package rtsp

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ygelfand/libcountertop/pkg/network/digest"
)

const (
	dialWait    = 10 * time.Second
	replyWait   = 10 * time.Second
	keepEvery   = 25 * time.Second
	backchannel = "www.onvif.org/ver20/backchannel"
	clientAgent = "Countertop"
)

var (
	ErrNoVideo     = errors.New("rtsp: the stream offers no H.264 or H.265 video")
	ErrNoBackAudio = errors.New("rtsp: the stream offers no backchannel audio")
	ErrRefused     = errors.New("rtsp: wrong user or password")
)

type Codec int

const (
	H264 Codec = iota + 1
	H265
)

type Unit struct {
	Codec Codec
	Data  []byte
	PTS   time.Duration
	Key   bool
}

type Sound struct {
	Codec  string
	Rate   int
	Config []byte
	Data   []byte
}

type Options struct {
	User        string
	Password    string
	Video       func(Unit)
	Audio       func(Sound)
	Backchannel bool
}

type Client struct {
	o     Options
	base  *url.URL
	conn  net.Conn
	r     *bufio.Reader
	media []media

	mu       sync.Mutex
	cseq     int
	session  string
	auth     string
	back     *media
	backSeq  uint16
	backTS   uint32
	backSSRC uint32
}

type reply struct {
	status  int
	headers textproto.MIMEHeader
	body    string
}

func Dial(ctx context.Context, rawURL string, o Options) (*Client, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.User != nil {
		if o.User == "" {
			o.User = u.User.Username()
		}
		if p, ok := u.User.Password(); ok && o.Password == "" {
			o.Password = p
		}
		u.User = nil
	}
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), "554")
	}
	d := net.Dialer{Timeout: dialWait}
	conn, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, err
	}
	c := &Client{
		o:        o,
		base:     u,
		conn:     conn,
		r:        bufio.NewReader(conn),
		backSSRC: uint32(time.Now().UnixNano()),
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	if err := c.start(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) start() error {
	var extra []string
	if c.o.Backchannel {
		extra = append(extra, "Require: "+backchannel)
	}
	desc, err := c.call("DESCRIBE", c.base.String(), append(extra, "Accept: application/sdp")...)
	if err != nil {
		return err
	}
	if b := desc.headers.Get("Content-Base"); b != "" {
		if u, err := url.Parse(b); err == nil {
			u.User = nil
			c.base = u
		}
	}
	c.media = parseSDP(desc.body)
	channel := 0
	var video bool
	for i := range c.media {
		m := &c.media[i]
		switch {
		case m.kind == "video" && m.codec() != 0 && !video:
			video = true
		case m.kind == "audio" && m.send && c.o.Backchannel && c.back == nil:
			c.back = m
		case m.kind == "audio" && !m.send && c.o.Audio != nil:
		default:
			continue
		}
		m.channel = channel
		m.depack = &depacketizer{codec: m.codec()}
		if _, err := c.call(
			"SETUP",
			c.control(m),
			append(
				extra,
				fmt.Sprintf("Transport: RTP/AVP/TCP;unicast;interleaved=%d-%d", channel, channel+1),
			)...); err != nil {
			return fmt.Errorf("rtsp: setup %s: %w", m.kind, err)
		}
		channel += 2
	}
	if !video && c.o.Video != nil {
		return ErrNoVideo
	}
	if c.o.Backchannel && c.back == nil {
		return ErrNoBackAudio
	}
	_, err = c.call("PLAY", c.base.String(), append(extra, "Range: npt=0.000-")...)
	return err
}

func (c *Client) control(m *media) string {
	if m.control == "" || m.control == "*" {
		return c.base.String()
	}
	if strings.HasPrefix(m.control, "rtsp://") || strings.HasPrefix(m.control, "rtsps://") {
		u, err := url.Parse(m.control)
		if err == nil {
			u.User = nil
			return u.String()
		}
		return m.control
	}
	return strings.TrimSuffix(c.base.String(), "/") + "/" + m.control
}

func (c *Client) Run(ctx context.Context) error {
	stop := context.AfterFunc(ctx, func() { _ = c.conn.SetDeadline(time.Now()) })
	defer stop()
	done := make(chan struct{})
	defer close(done)
	go c.keepalive(done)
	for {
		if err := c.readOne(); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
	}
}

func (c *Client) keepalive(done <-chan struct{}) {
	t := time.NewTicker(keepEvery)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			_ = c.send("GET_PARAMETER", c.base.String())
		}
	}
}

func (c *Client) Close() error {
	_ = c.send("TEARDOWN", c.base.String())
	return c.conn.Close()
}

func (c *Client) BackRate() int {
	if c.back == nil {
		return 0
	}
	return c.back.rate
}

func (c *Client) BackCodec() string {
	if c.back == nil {
		return ""
	}
	return c.back.encoding
}

func (c *Client) SendBack(payload []byte, samples int) error {
	c.mu.Lock()
	m := c.back
	if m == nil {
		c.mu.Unlock()
		return ErrNoBackAudio
	}
	pkt := make([]byte, 12+len(payload))
	pkt[0] = 0x80
	pkt[1] = byte(m.payload) | 0x80
	binary.BigEndian.PutUint16(pkt[2:], c.backSeq)
	binary.BigEndian.PutUint32(pkt[4:], c.backTS)
	binary.BigEndian.PutUint32(pkt[8:], c.backSSRC)
	copy(pkt[12:], payload)
	c.backSeq++
	c.backTS += uint32(samples)
	channel := m.channel
	c.mu.Unlock()
	frame := make([]byte, 4+len(pkt))
	frame[0], frame[1] = '$', byte(channel)
	binary.BigEndian.PutUint16(frame[2:], uint16(len(pkt)))
	copy(frame[4:], pkt)
	return c.write(frame)
}

func (c *Client) write(b []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.conn.Write(b)
	return err
}

func (c *Client) call(method, uri string, headers ...string) (reply, error) {
	for attempt := 0; ; attempt++ {
		if err := c.send(method, uri, headers...); err != nil {
			return reply{}, err
		}
		_ = c.conn.SetReadDeadline(time.Now().Add(replyWait))
		r, err := c.readReply()
		_ = c.conn.SetReadDeadline(time.Time{})
		if err != nil {
			return reply{}, err
		}
		if r.status == 401 && attempt == 0 && c.o.User != "" {
			c.mu.Lock()
			c.auth = pickChallenge(r.headers.Values("WWW-Authenticate"))
			c.mu.Unlock()
			continue
		}
		switch {
		case r.status == 401:
			return r, ErrRefused
		case r.status != 200:
			return r, fmt.Errorf("rtsp: %s answered %d", method, r.status)
		}
		if s := r.headers.Get("Session"); s != "" {
			id, _, _ := strings.Cut(s, ";")
			c.mu.Lock()
			c.session = strings.TrimSpace(id)
			c.mu.Unlock()
		}
		return r, nil
	}
}

func pickChallenge(values []string) string {
	for _, v := range values {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(v)), "digest") {
			return v
		}
	}
	if len(values) > 0 {
		return values[0]
	}
	return ""
}

func (c *Client) send(method, uri string, headers ...string) error {
	c.mu.Lock()
	c.cseq++
	var b strings.Builder
	fmt.Fprintf(
		&b,
		"%s %s RTSP/1.0\r\nCSeq: %d\r\nUser-Agent: %s\r\n",
		method,
		uri,
		c.cseq,
		clientAgent,
	)
	if c.session != "" {
		fmt.Fprintf(&b, "Session: %s\r\n", c.session)
	}
	if c.auth != "" {
		fmt.Fprintf(
			&b,
			"Authorization: %s\r\n",
			digest.Answer(c.auth, method, uri, c.o.User, c.o.Password),
		)
	}
	for _, h := range headers {
		b.WriteString(h)
		b.WriteString("\r\n")
	}
	b.WriteString("\r\n")
	_, err := io.WriteString(c.conn, b.String())
	c.mu.Unlock()
	return err
}

func (c *Client) readReply() (reply, error) {
	for {
		first, err := c.r.Peek(1)
		if err != nil {
			return reply{}, err
		}
		if first[0] == '$' {
			if err := c.readPacket(); err != nil {
				return reply{}, err
			}
			continue
		}
		return c.readResponse()
	}
}

func (c *Client) readOne() error {
	first, err := c.r.Peek(1)
	if err != nil {
		return err
	}
	if first[0] == '$' {
		return c.readPacket()
	}
	_, err = c.readResponse()
	return err
}

func (c *Client) readResponse() (reply, error) {
	tp := textproto.NewReader(c.r)
	line, err := tp.ReadLine()
	if err != nil {
		return reply{}, err
	}
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 2 || !strings.HasPrefix(parts[0], "RTSP/") {
		return reply{}, fmt.Errorf("rtsp: unexpected line %q", line)
	}
	status, err := strconv.Atoi(parts[1])
	if err != nil {
		return reply{}, fmt.Errorf("rtsp: unexpected status %q", parts[1])
	}
	headers, err := tp.ReadMIMEHeader()
	if err != nil && !errors.Is(err, io.EOF) {
		return reply{}, err
	}
	r := reply{status: status, headers: headers}
	if n, _ := strconv.Atoi(headers.Get("Content-Length")); n > 0 {
		body := make([]byte, n)
		if _, err := io.ReadFull(c.r, body); err != nil {
			return reply{}, err
		}
		r.body = string(body)
	}
	return r, nil
}

func (c *Client) readPacket() error {
	var head [4]byte
	if _, err := io.ReadFull(c.r, head[:]); err != nil {
		return err
	}
	pkt := make([]byte, binary.BigEndian.Uint16(head[2:]))
	if _, err := io.ReadFull(c.r, pkt); err != nil {
		return err
	}
	channel := int(head[1])
	for i := range c.media {
		m := &c.media[i]
		if m.depack == nil || m.channel != channel {
			continue
		}
		c.deliver(m, pkt)
		return nil
	}
	return nil
}

func (c *Client) deliver(m *media, pkt []byte) {
	payload, marker, ts, ok := rtpPayload(pkt)
	if !ok {
		return
	}
	switch m.kind {
	case "video":
		if c.o.Video == nil {
			return
		}
		au, key, done := m.depack.push(payload, marker)
		if !done {
			return
		}
		c.o.Video(Unit{Codec: m.depack.codec, Data: au, PTS: m.clock(ts), Key: key})
	case "audio":
		if c.o.Audio == nil || m.send {
			return
		}
		c.o.Audio(Sound{Codec: m.encoding, Rate: m.rate, Config: m.config, Data: payload})
	}
}

func rtpPayload(pkt []byte) ([]byte, bool, uint32, bool) {
	if len(pkt) < 12 || pkt[0]>>6 != 2 {
		return nil, false, 0, false
	}
	marker := pkt[1]&0x80 != 0
	ts := binary.BigEndian.Uint32(pkt[4:])
	at := 12 + 4*int(pkt[0]&0x0f)
	if pkt[0]&0x10 != 0 {
		if len(pkt) < at+4 {
			return nil, false, 0, false
		}
		at += 4 + 4*int(binary.BigEndian.Uint16(pkt[at+2:]))
	}
	end := len(pkt)
	if pkt[0]&0x20 != 0 && end > at {
		end -= int(pkt[end-1])
	}
	if at > end {
		return nil, false, 0, false
	}
	return pkt[at:end], marker, ts, true
}
