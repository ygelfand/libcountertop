package webreceiver

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ygelfand/libcountertop/pkg/media/cast"
	"github.com/ygelfand/libcountertop/pkg/media/urlplayer"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
)

const (
	settle    = 2 * time.Second
	ticks     = 500 * time.Millisecond
	openWait  = 30 * time.Second
	openCheck = 100 * time.Millisecond
)

type order struct {
	Op      string  `json:"op"`
	URL     string  `json:"url"`
	Start   float64 `json:"start"`
	T       float64 `json:"t"`
	From    float64 `json:"from"`
	Until   float64 `json:"until"`
	Lane    int     `json:"lane"`
	Seq     int     `json:"seq"`
	Type    string  `json:"type"`
	Offset  float64 `json:"offset"`
	Title   string  `json:"title"`
	Artist  string  `json:"artist"`
	Album   string  `json:"album"`
	Session string  `json:"session"`
	Init    string  `json:"init"`
	Name    string  `json:"name"`

	Fields map[string]string `json:"fields"`
}

type report struct {
	Event    string       `json:"ev"`
	T        *float64     `json:"t,omitempty"`
	Duration float64      `json:"duration,omitempty"`
	W        int          `json:"w,omitempty"`
	H        int          `json:"h,omitempty"`
	Message  string       `json:"message,omitempty"`
	Lane     int          `json:"lane,omitempty"`
	Seq      int          `json:"seq,omitempty"`
	Buffered [][2]float64 `json:"buffered,omitempty"`
	Session  string       `json:"session,omitempty"`
	Data     []byte       `json:"data,omitempty"`
	Kids     [][]byte     `json:"kids,omitempty"`
	Frames   int64        `json:"frames,omitempty"`
}

type bridge struct {
	r *Receiver

	mu      sync.Mutex
	conn    *websocket.Conn
	loads   int
	playing bool
	began   bool
	feed    *urlplayer.Feed
	told    bool
	settled bool
	held    *held
}

type held struct {
	feed *urlplayer.Feed
	at   time.Duration
	play bool
}

func (b *bridge) serve(w http.ResponseWriter, req *http.Request) {
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	c, err := up.Upgrade(w, req, nil)
	if err != nil {
		return
	}
	b.mu.Lock()
	old := b.conn
	b.conn = c
	b.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	safe.Go("cast page clock", func() { b.clock(ctx) })
	defer func() {
		b.mu.Lock()
		if b.conn == c {
			b.conn = nil
		}
		b.mu.Unlock()
		_ = c.Close()
	}()
	for {
		kind, data, err := c.ReadMessage()
		if err != nil {
			return
		}
		var o order
		var payload []byte
		if kind == websocket.BinaryMessage {
			if len(data) < 4 {
				continue
			}
			n := int(binary.BigEndian.Uint32(data))
			if 4+n > len(data) {
				continue
			}
			data, payload = data[4:4+n], data[4+n:]
		}
		if json.Unmarshal(data, &o) != nil {
			continue
		}
		b.handle(o, payload)
	}
}

func (b *bridge) failed(err error) {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		b.tell(b.at("ended"))
		return
	}
	b.tell(report{Event: "error", Message: err.Error()})
}

func (b *bridge) handle(o order, payload []byte) {
	if o.Op == "lounge" {
		b.r.trace("the cast page told the lounge", "message", o.Name, "fields", o.Fields)
		if o.Name != "onStateChange" {
			return
		}
		if pr, ok := program(o.Fields); ok && b.r.player != nil {
			b.r.player.Program(pr)
		}
		return
	}
	if o.Op == "close" {
		b.r.closed()
		return
	}
	p := b.r.player
	if p == nil {
		b.tell(report{Event: "error", Message: "this device has no player"})
		return
	}
	if o.Op != "append" {
		b.r.trace("the cast page ordered", "op", o.Op, "t", o.T, "from", o.From, "start", o.Start,
			"lane", o.Lane, "type", o.Type, "title", o.Title, "url", o.URL)
	}
	switch o.Op {
	case "load", "source":
		b.r.unpictured()
		b.r.snapshotElements(o.Op)
	case "stop":
		b.r.snapshotElements(o.Op)
	}
	if b.fed(o, payload) || b.keyed(o, payload) || b.holding(o) {
		return
	}
	switch o.Op {
	case "load":
		b.mu.Lock()
		b.loads++
		load := b.loads
		b.playing, b.began, b.feed, b.held = false, false, nil, nil
		b.mu.Unlock()
		at := time.Duration(o.Start * float64(time.Second))
		if err := p.Load(cast.Media{ContentID: o.URL}, at, false); err != nil {
			slog.Info("the cast page asked for something unplayable", "err", err)
			b.tell(report{Event: "error", Message: err.Error()})
			return
		}
		safe.Go("cast page open", func() { b.opened(load) })
	case "play":
		if p.Play() == nil {
			b.setPlaying(true)
			b.started()
		}
	case "pause":
		if p.Pause() == nil {
			b.setPlaying(false)
			b.tell(b.at("pause"))
		}
	case "seek":
		if p.Seek(time.Duration(o.T*float64(time.Second))) == nil {
			b.mu.Lock()
			b.began = false
			b.mu.Unlock()
			b.tell(b.at("seeked"))
			b.started()
		}
	case "page":
		b.r.mu.Lock()
		b.r.url = o.URL
		b.r.mu.Unlock()
		b.r.sponsor.page(o.URL)
	case "describe":
		md := cast.Metadata{Title: o.Title, Artist: o.Artist, AlbumName: o.Album}
		if o.URL != "" {
			md.Images = []cast.Image{{URL: o.URL}}
		}
		p.Describe(&md)
	case "stop":
		b.mu.Lock()
		b.playing, b.began, b.feed, b.held = false, false, nil, nil
		b.mu.Unlock()
		p.Release()
	}
}

func program(fields map[string]string) (urlplayer.Program, bool) {
	var secs [4]time.Duration
	for i, k := range []string{"currentTime", "duration", "seekableStartTime", "seekableEndTime"} {
		v, err := strconv.ParseFloat(fields[k], 64)
		if err != nil {
			return urlplayer.Program{}, false
		}
		secs[i] = time.Duration(v * float64(time.Second))
	}
	pr := urlplayer.Program{At: secs[0], Length: secs[1], Earliest: secs[2], Edge: secs[3]}
	if pr.Length <= 0 || pr.Edge <= pr.Earliest {
		return urlplayer.Program{}, false
	}
	return pr, true
}

func (b *bridge) fed(o order, payload []byte) bool {
	b.mu.Lock()
	f := b.feed
	b.mu.Unlock()
	switch o.Op {
	case "source":
		f = urlplayer.NewFeed()
		f.Protect(b.r.keys.fork)
		b.mu.Lock()
		b.loads++
		load := b.loads
		b.feed, b.told, b.playing, b.began, b.held = f, false, false, false, nil
		first := !b.settled
		b.settled = true
		if first {
			b.held = &held{feed: f, at: seconds(o.Start)}
		}
		b.mu.Unlock()
		if first {
			time.AfterFunc(settle, func() { b.release(load) })
			return true
		}
		if err := b.r.player.LoadFeed(f, seconds(o.Start), false); err != nil {
			b.tell(report{Event: "error", Message: err.Error()})
		}
		return true
	case "add", "drop", "abort", "append", "remove", "duration", "end":
	default:
		return false
	}
	if f == nil {
		return true
	}
	switch o.Op {
	case "add":
		f.Add(o.Lane, o.Type)
	case "drop":
		f.Drop(o.Lane)
	case "abort":
		f.Abort(o.Lane)
	case "duration":
		f.SetDuration(seconds(o.T))
	case "end":
		f.End()
	case "append", "remove":
		var ranges []urlplayer.Range
		var err error
		if o.Op == "append" {
			ranges, err = f.Append(o.Lane, payload, seconds(o.Offset))
		} else {
			ranges, err = f.Remove(o.Lane, seconds(o.Start), seconds(o.Until))
		}
		r := report{Event: "done", Lane: o.Lane, Seq: o.Seq, Buffered: [][2]float64{}}
		for _, rg := range ranges {
			r.Buffered = append(r.Buffered, [2]float64{rg.Start.Seconds(), rg.End.Seconds()})
		}
		if err != nil {
			slog.Info("the cast page appended something unplayable", "lane", o.Lane, "err", err)
			r.Message = err.Error()
		}
		slog.Debug(
			"the cast page's media is buffered",
			"op",
			o.Op,
			"lane",
			o.Lane,
			"bytes",
			len(payload),
			"ranges",
			r.Buffered,
		)
		b.tell(r)
		if boxes := f.InitData(o.Lane); len(boxes) > 0 {
			slog.Debug(
				"the cast page's media carries init data",
				"lane",
				o.Lane,
				"boxes",
				len(boxes),
			)
			b.tell(report{Event: "encrypted", Lane: o.Lane, Data: bytes.Join(boxes, nil)})
		}
		b.metaOnce(f)
	}
	return true
}

func (b *bridge) holding(o order) bool {
	b.mu.Lock()
	h := b.held
	if h == nil {
		b.mu.Unlock()
		return false
	}
	switch o.Op {
	case "play":
		h.play = true
	case "pause":
		h.play = false
	case "seek":
		h.at = time.Duration(o.T * float64(time.Second))
	default:
		b.mu.Unlock()
		return false
	}
	b.mu.Unlock()
	if o.Op == "seek" {
		t := o.T
		b.tell(report{Event: "seeked", T: &t})
	}
	return true
}

func (b *bridge) release(load int) {
	b.mu.Lock()
	h := b.held
	if b.loads != load || h == nil {
		b.mu.Unlock()
		return
	}
	b.held = nil
	b.mu.Unlock()
	if err := b.r.player.LoadFeed(h.feed, h.at, h.play); err != nil {
		b.tell(report{Event: "error", Message: err.Error()})
		return
	}
	if h.play {
		b.setPlaying(true)
	}
}

func (b *bridge) forget() {
	b.mu.Lock()
	b.loads++
	b.settled, b.held = false, nil
	b.mu.Unlock()
}

func (b *bridge) keyed(o order, payload []byte) bool {
	k := b.r.keys
	r := report{Event: "keydone", Seq: o.Seq, Session: o.Session}
	switch o.Op {
	case "cert":
		k.setCert(payload)
	case "request":
		challenge, kids, err := k.request(o.Session, o.Init, payload)
		if err != nil {
			slog.Info("the cast page's key request failed", "init", o.Init, "err", err)
			r.Message = err.Error()
		}
		r.Data, r.Kids = challenge, kids
	case "update":
		kids, err := k.update(o.Session, payload)
		if err != nil {
			slog.Info("the cast page's license was refused", "err", err)
			r.Message = err.Error()
		}
		r.Kids = kids
	case "release":
		k.close(o.Session)
	default:
		return false
	}
	b.tell(r)
	return true
}

func (b *bridge) metaOnce(f *urlplayer.Feed) {
	o, ok := f.Opened()
	if !ok {
		return
	}
	b.mu.Lock()
	first := !b.told && b.feed == f
	b.told = true
	b.mu.Unlock()
	if first {
		b.tell(report{Event: "meta", W: o.Width, H: o.Height})
	}
}

func (b *bridge) opened(load int) {
	deadline := time.Now().Add(openWait)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		current := b.loads == load
		b.mu.Unlock()
		if !current {
			return
		}
		if s, ok := b.r.player.Shown(); ok {
			b.tell(report{Event: "meta", Duration: s.Length.Seconds(), W: s.Width, H: s.Height})
			return
		}
		time.Sleep(openCheck)
	}
}

func (b *bridge) clock(ctx context.Context) {
	tick := time.NewTicker(ticks)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		b.mu.Lock()
		playing, began := b.playing, b.began
		b.mu.Unlock()
		switch {
		case playing && began && !b.r.player.Ready():
			b.stalled()
		case playing && began:
			b.tell(b.at("time"))
		case playing:
			b.started()
		}
	}
}

func (b *bridge) stalled() {
	b.mu.Lock()
	was := b.began
	b.began = false
	b.mu.Unlock()
	if was {
		b.tell(b.at("waiting"))
	}
}

func (b *bridge) started() {
	if !b.r.player.Ready() {
		return
	}
	b.mu.Lock()
	first := b.playing && !b.began
	b.began = b.began || b.playing
	b.mu.Unlock()
	if first {
		b.tell(b.at("playing"))
	}
}

func (b *bridge) at(event string) report {
	t := b.r.player.Elapsed().Seconds()
	return report{Event: event, T: &t, Frames: b.r.player.Frames()}
}

func (b *bridge) setPlaying(on bool) {
	b.mu.Lock()
	b.playing = on
	b.began = b.began && on
	b.mu.Unlock()
}

func (b *bridge) tell(r report) {
	if r.Event == "ended" {
		b.setPlaying(false)
	}
	if r.T != nil {
		slog.Debug("the cast page was told", "ev", r.Event, "t", *r.T, "frames", r.Frames)
	} else if r.Event != "done" && r.Event != "keydone" {
		b.r.trace(
			"the cast page was told",
			"ev",
			r.Event,
			"duration",
			r.Duration,
			"w",
			r.W,
			"h",
			r.H,
			"message",
			r.Message,
		)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn == nil {
		return
	}
	if err := b.conn.WriteJSON(r); err != nil {
		slog.Debug("the cast page missed a media report", "err", err)
	}
}

func displayType(t string) (string, int, int, int) {
	kind, params, err := mime.ParseMediaType(t)
	if err != nil {
		return t, 0, 0, 0
	}
	number := func(k string) int {
		v, _ := strconv.ParseFloat(params[k], 64)
		return int(math.Ceil(v))
	}
	kept := map[string]string{}
	if c := params["codecs"]; c != "" {
		kept["codecs"] = c
	}
	return mime.FormatMediaType(kind, kept), number("width"), number("height"), number("framerate")
}
