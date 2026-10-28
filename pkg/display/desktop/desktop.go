package desktop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"

	"github.com/ygelfand/libcountertop/pkg/display/geometry"
	"github.com/ygelfand/libcountertop/pkg/display/panel"
	"github.com/ygelfand/libcountertop/pkg/display/surface"
	"github.com/ygelfand/libcountertop/pkg/input/textentry"
	"github.com/ygelfand/libcountertop/pkg/input/touch"
)

const (
	lease       = 30 * time.Second
	idleTimeout = 2 * time.Minute
)

type capture interface {
	Read() (surface.Encoded, error)
	Keyframe() error
	Close() error
}

type Server struct {
	mu       sync.Mutex
	current  *session
	open     func() (capture, error)
	size     func() (int, int)
	turn     func() geometry.Orientation
	deliver  func(touch.Contact)
	keyboard *textentry.Controller
}

type session struct {
	server    *Server
	id        string
	pc        *webrtc.PeerConnection
	track     *webrtc.TrackLocalStaticSample
	sender    *webrtc.RTPSender
	ctx       context.Context
	cancel    context.CancelFunc
	until     time.Time
	input     pointer
	connected sync.Once
	ready     chan struct{}
	workers   sync.WaitGroup
	turn      geometry.Orientation
}

func New(d *panel.Driver, deliver func(touch.Contact), keyboard *textentry.Controller) *Server {
	return &Server{
		open: func() (capture, error) {
			c := d.Helper()
			if c == nil {
				return nil, fmt.Errorf("screen capture unavailable")
			}
			w, h := d.Native()
			return c.Record(w, h, int(d.Orientation())/90)
		},
		size:     func() (int, int) { w, h := d.Native(); return d.Orientation().Size(w, h) },
		deliver:  deliver,
		keyboard: keyboard,
		turn:     d.Orientation,
	}
}

func (s *Server) orientation() geometry.Orientation {
	if s.turn == nil {
		return geometry.Rotate0
	}
	return s.turn()
}

func local(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !local(net.ParseIP(host)) {
		http.Error(w, "connect from the local network", http.StatusForbidden)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host || u.Scheme != "https" {
			http.Error(w, "use the device web interface", http.StatusForbidden)
			return
		}
	}
	id := strings.Trim(r.URL.Path, "/")
	if id == "" && r.Method == http.MethodPost {
		s.offer(w, r)
		return
	}
	s.mu.Lock()
	current := s.current
	if current == nil || id != current.id {
		s.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodPost:
		current.until = time.Now().Add(lease)
	case http.MethodDelete:
		current.cancel()
	default:
		s.mu.Unlock()
		w.Header().Set("Allow", "POST, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) offer(w http.ResponseWriter, r *http.Request) {
	var offer webrtc.SessionDescription
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&offer) != nil ||
		offer.Type != webrtc.SDPTypeOffer ||
		offer.SDP == "" {
		http.Error(w, "send a WebRTC offer", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current != nil {
		http.Error(w, "remote desktop is in use", http.StatusConflict)
		return
	}
	x, y := s.size()
	if x <= 0 || y <= 0 {
		http.Error(w, "screen unavailable", http.StatusServiceUnavailable)
		return
	}
	sess, err := s.create(x, y)
	if err != nil {
		slog.Warn("remote desktop", "err", err)
		http.Error(w, "remote desktop unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	answer, err := sess.answer(ctx, offer)
	if err != nil {
		sess.cancel()
		_ = sess.pc.Close()
		http.Error(w, "WebRTC negotiation failed", http.StatusBadRequest)
		return
	}
	s.current = sess
	go sess.watch()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(struct {
		ID  string                    `json:"id"`
		SDP webrtc.SessionDescription `json:"answer"`
	}{sess.id, answer}); err != nil {
		sess.cancel()
	}
}

func (s *Server) create(w, h int) (*session, error) {
	engine := &webrtc.MediaEngine{}
	codec := webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000,
		SDPFmtpLine:  "level-asymmetry-allowed=1;packetization-mode=1;profile-level-id=42e01f",
		RTCPFeedback: []webrtc.RTCPFeedback{{Type: "nack"}, {Type: "nack", Parameter: "pli"}}}
	if err := engine.RegisterCodec(
		webrtc.RTPCodecParameters{RTPCodecCapability: codec, PayloadType: 102},
		webrtc.RTPCodecTypeVideo,
	); err != nil {
		return nil, err
	}
	registry := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(engine, registry); err != nil {
		return nil, err
	}
	settings := webrtc.SettingEngine{}
	settings.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	settings.SetIPFilter(local)
	settings.SetIncludeLoopbackCandidate(true)
	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(engine),
		webrtc.WithInterceptorRegistry(registry),
		webrtc.WithSettingEngine(settings),
	)
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, err
	}
	track, err := webrtc.NewTrackLocalStaticSample(codec, "screen", "desktop")
	if err != nil {
		_ = pc.Close()
		return nil, err
	}
	sender, err := pc.AddTrack(track)
	if err != nil {
		_ = pc.Close()
		return nil, err
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		_ = pc.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	sess := &session{
		server: s,
		id:     hex.EncodeToString(token[:]),
		pc:     pc,
		track:  track,
		ctx:    ctx,
		cancel: cancel,
		until: time.Now().
			Add(lease),
		input: pointer{width: w, height: h, deliver: s.deliver, last: time.Now()},
		ready: make(chan struct{}),
		turn:  s.orientation(),
	}
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		if dc.Label() != "input" || !dc.Ordered() || dc.MaxRetransmits() != nil ||
			dc.MaxPacketLifeTime() != nil {
			_ = dc.Close()
			return
		}
		dc.OnMessage(func(m webrtc.DataChannelMessage) {
			if m.IsString && ctx.Err() == nil {
				if err := sess.receive(m.Data); err != nil {
					message, marshalErr := json.Marshal(struct {
						Error string `json:"error"`
					}{err.Error()})
					if marshalErr != nil || dc.SendText(string(message)) != nil {
						cancel()
					}
				}
			}
		})
		dc.OnClose(cancel)
	})
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateConnected:
			sess.connected.Do(func() { close(sess.ready) })
		case webrtc.PeerConnectionStateDisconnected,
			webrtc.PeerConnectionStateFailed,
			webrtc.PeerConnectionStateClosed:
			cancel()
		}
	})
	sess.sender = sender
	return sess, nil
}

func (s *session) answer(
	ctx context.Context,
	offer webrtc.SessionDescription,
) (webrtc.SessionDescription, error) {
	if err := s.pc.SetRemoteDescription(offer); err != nil {
		return webrtc.SessionDescription{}, err
	}
	answer, err := s.pc.CreateAnswer(nil)
	if err != nil {
		return answer, err
	}
	gathered := webrtc.GatheringCompletePromise(s.pc)
	if err := s.pc.SetLocalDescription(answer); err != nil {
		return answer, err
	}
	select {
	case <-ctx.Done():
		return answer, ctx.Err()
	case <-gathered:
		return *s.pc.LocalDescription(), nil
	}
}

func (s *session) watch() {
	defer func() {
		s.cancel()
		s.input.release()
		_ = s.pc.Close()
		s.workers.Wait()
		s.server.mu.Lock()
		if s.server.current == s {
			s.server.current = nil
		}
		s.server.mu.Unlock()
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	ready := s.ready
	for {
		select {
		case <-ready:
			ready = nil
			s.workers.Go(func() { s.stream(s.sender) })
		case <-s.ctx.Done():
			return
		case now := <-ticker.C:
			s.server.mu.Lock()
			expired := now.After(s.until)
			s.server.mu.Unlock()
			w, h := s.server.size()
			if expired || s.input.idle(now) || w != s.input.width || h != s.input.height ||
				s.turn != s.server.orientation() {
				return
			}
		}
	}
}

func (s *session) stream(sender *webrtc.RTPSender) {
	defer s.cancel()
	stream, err := s.server.open()
	if err != nil {
		slog.Warn("remote desktop capture", "err", err)
		return
	}
	stop := context.AfterFunc(s.ctx, func() { _ = stream.Close() })
	defer stop()
	defer stream.Close()
	go func() {
		for {
			packets, _, err := sender.ReadRTCP()
			if err != nil {
				return
			}
			for _, packet := range packets {
				if _, ok := packet.(*rtcp.PictureLossIndication); ok {
					if err := stream.Keyframe(); err != nil {
						return
					}
				}
			}
		}
	}()
	var config []byte
	var previous time.Duration
	for s.ctx.Err() == nil {
		frame, err := stream.Read()
		if err != nil {
			return
		}
		if frame.Flags&2 != 0 {
			config = frame.Data
			continue
		}
		if frame.Flags&1 != 0 {
			frame.Data = append(append([]byte(nil), config...), frame.Data...)
		}
		duration := frame.At - previous
		if previous == 0 || duration <= 0 || duration > time.Second {
			duration = time.Second / 15
		}
		previous = frame.At
		if err := s.track.WriteSample(
			media.Sample{Data: frame.Data, Duration: duration},
		); err != nil {
			return
		}
	}
}
