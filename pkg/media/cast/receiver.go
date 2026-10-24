package cast

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"
)

const DefaultMediaReceiver = "CC1AD845"

type Transport interface {
	Play() error
	Pause() error
	Stop() error
}

type Seeking interface {
	Seek(to time.Duration) error
	Elapsed() time.Duration
}

type Controls interface {
	Transport
	Seeking
}

type Playing interface {
	Controls
	Load(m Media, at time.Duration, play bool) error
}

type Stepping interface {
	Step(by int) error
}

type Receiver struct {
	Name string

	Player Playing

	Volume func(level float64, muted bool)

	Describe func(app string) AppConfig
	Launched func(app App)
	Stopped  func(app string)

	Loaded func(m Media)

	Unspoken func(namespace, kind string)

	Credentials func() *Credentials

	CRL func() []byte

	Challenged func(c Challenge, answered bool)

	Available func(app string) bool

	Identity func() Device

	Eureka func() Eureka

	Setup func(kind string, data json.RawMessage)

	volume Volume

	protocols map[string]Protocol

	app *Application

	media   *Media
	state   string
	idle    string
	session int

	senders map[string]bool

	sessions int

	appMedia appPlayer
}

type appPlayer struct {
	control  Controls
	commands int
	custom   json.RawMessage
	told     string
}

type Published struct {
	Media    Media
	State    string
	Commands int
	Custom   json.RawMessage

	Answer  *Message
	Request int
}

func (r *Receiver) Publish(control Controls, p *Published) []Message {
	if r.app == nil || control == nil {
		return nil
	}
	if p == nil {
		if r.appMedia.control != control || r.session == 0 {
			return nil
		}
		out := r.cancelled()
		r.media, r.session, r.appMedia = nil, 0, appPlayer{}
		return out
	}
	told, _ := json.Marshal(struct {
		Media    Media
		State    string
		Commands int
		Custom   json.RawMessage
	}{p.Media, p.State, p.Commands, p.Custom})
	if p.Answer == nil && r.appMedia.control == control && r.appMedia.told == string(told) &&
		r.session != 0 {
		return nil
	}
	if r.media == nil || r.media.ContentID != p.Media.ContentID || r.session == 0 {
		var b [4]byte
		rand.Read(b[:])
		r.session = int(binary.BigEndian.Uint32(b[:])>>2) + 1
	}
	m := p.Media
	r.media, r.state, r.idle = &m, p.State, ""
	r.appMedia = appPlayer{
		control:  control,
		commands: p.Commands,
		custom:   p.Custom,
		told:     string(told),
	}
	if p.Answer != nil {
		return r.report(*p.Answer, p.Request)
	}
	return []Message{r.mediaBroadcast(r.full())}
}

func (r *Receiver) control() Controls {
	if r.appMedia.control != nil {
		return r.appMedia.control
	}
	return r.Player
}

func NewReceiver(name string) *Receiver {
	return &Receiver{
		Name:    name,
		state:   StateIdle,
		senders: map[string]bool{},
		volume: Volume{
			Level:        ptr(1.0),
			Muted:        ptr(false),
			StepInterval: 0.05,
			ControlType:  "attenuation",
		},
	}
}

func ptr[T any](v T) *T { return &v }

func (r *Receiver) Status() ReceiverStatus {
	s := ReceiverStatus{Volume: r.volume, IsActiveInput: true}
	if r.app != nil {
		s.Applications = []Application{*r.app}
	}
	return s
}

func (r *Receiver) Running() string {
	if r.app == nil {
		return ""
	}
	return r.app.AppID
}

func (r *Receiver) State() string { return r.state }

func (r *Receiver) Media() *Media { return r.media }

func (r *Receiver) Receive(m Message) ([]Message, error) {
	if o := r.owner(); o != nil && m.Destination == r.app.TransportID &&
		m.Namespace != NSHeartbeat {
		var out []Message
		if m.Namespace == NSConnection {
			out, _ = r.connection(m)
		}
		more, err := o.Receive(r.app, m)
		return append(out, more...), err
	}

	switch m.Namespace {
	case NSConnection:
		return r.connection(m)
	case NSHeartbeat:
		return r.heartbeat(m)
	case NSReceiver:
		return r.receiver(m)
	case NSMedia:
		return r.mediaNamespace(m)
	case NSDeviceAuth:
		return r.deviceAuth(m)
	case NSSetup:
		return r.setup(m)
	case NSDiscovery:
		return r.discovery(m)
	}

	if p := r.protocol(m.Namespace); p != nil {
		out, err := p.Receive(r.app, m)
		if !errors.Is(err, ErrUnspoken) {
			return out, err
		}
	}

	if r.Unspoken != nil {
		kind := "binary"
		if len(m.Binary) == 0 {
			if h, err := Kind(m.Payload); err == nil {
				kind = h.Type
			}
		}
		r.Unspoken(m.Namespace, kind)
	}
	return nil, nil
}

func (r *Receiver) deviceAuth(m Message) ([]Message, error) {
	c, err := ParseChallenge(m.Binary)
	if err != nil {
		return nil, err
	}

	var creds *Credentials
	if r.Credentials != nil {
		creds = r.Credentials()
	}

	var body []byte
	switch {
	case creds == nil:
		body = Refuse(AuthInternal)
	case c.Algorithm != PKCS1v15:
		body = Refuse(AuthNoAlgorithm)
	default:
		var crl []byte
		if r.CRL != nil {
			crl = r.CRL()
		}
		body = Answer(creds, c.Hash, crl)
	}
	if r.Challenged != nil {
		r.Challenged(c, creds != nil && c.Algorithm == PKCS1v15)
	}

	return []Message{{
		Source:      m.Destination,
		Destination: m.Source,
		Namespace:   NSDeviceAuth,
		Binary:      body,
	}}, nil
}

func reply(to Message, namespace, body string) Message { return Reply(to, namespace, body) }

func Reply(to Message, namespace, body string) Message {
	return Message{
		Source:      to.Destination,
		Destination: to.Source,
		Namespace:   namespace,
		Payload:     body,
	}
}

// CONNECT has no reply in the protocol.
func (r *Receiver) connection(m Message) ([]Message, error) {
	h, err := Kind(m.Payload)
	if err != nil {
		return nil, err
	}

	switch h.Type {
	case TypeConnect:
		r.senders[m.Source] = true
		if r.app != nil && m.Destination == r.app.TransportID && r.session != 0 {
			return []Message{reply(m, NSMedia, MediaStatusPayload(0, r.full()...))}, nil
		}
	case TypeClose:
		delete(r.senders, m.Source)
	}
	return nil, nil
}

func (r *Receiver) heartbeat(m Message) ([]Message, error) {
	h, err := Kind(m.Payload)
	if err != nil {
		return nil, err
	}
	if h.Type != TypePing {
		return nil, nil
	}
	return []Message{reply(m, NSHeartbeat, Pong())}, nil
}

func (r *Receiver) receiver(m Message) ([]Message, error) {
	h, err := Kind(m.Payload)
	if err != nil {
		return nil, err
	}

	switch h.Type {
	case TypeGetStatus:
		return []Message{reply(m, NSReceiver, Status(h.RequestID, r.Status()))}, nil

	case TypeLaunch:
		return r.launch(m, h)

	case TypeStop:
		return r.stop(m, h)

	case TypeSetVolume:
		return r.setVolume(m, h)

	case TypeGetAppAvailability:
		return r.availability(m)
	}

	return []Message{reply(m, NSReceiver, Invalid(h.RequestID, ReasonInvalidCommand))}, nil
}

// LAUNCH is answered with RECEIVER_STATUS, not a type of its own.
func (r *Receiver) launch(m Message, h Header) ([]Message, error) {
	req, err := ParseLaunch(m.Payload)
	if err != nil {
		return []Message{reply(m, NSReceiver, Invalid(h.RequestID, ReasonInvalidParams))}, nil
	}

	// Music Assistant launches C35B0678 and raises on a refusal.

	if r.app == nil || r.app.AppID != req.AppID {
		r.release(IdleInterrupted)
		r.start(req.AppID)
	}

	return r.announce(m, h.RequestID), nil
}

func sessionID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func (r *Receiver) start(app string) {
	id := sessionID()
	known := Lookup(app)
	if r.Describe != nil {
		c := r.Describe(app)
		known.Name, known.Icon = c.Name, c.Icon
	}
	if known.Name == "" {
		known.Name = app
	}

	r.app = &Application{
		AppID:          app,
		UniversalAppID: app,
		AppType:        "WEB",
		IconURL:        known.Icon,
		SessionID:      id,

		TransportID: id,

		DisplayName: known.Name,
		StatusText:  known.Name,
		Namespaces:  r.namespaces(known),
	}
	r.bracket(known, true)

	if r.Launched != nil {
		r.Launched(known)
	}
}

func (r *Receiver) stop(m Message, h Header) ([]Message, error) {
	req, err := ParseStop(m.Payload)
	if err != nil {
		return []Message{reply(m, NSReceiver, Invalid(h.RequestID, ReasonInvalidParams))}, nil
	}

	if req.SessionID != "" && (r.app == nil || r.app.SessionID != req.SessionID) {
		return []Message{reply(m, NSReceiver, Invalid(h.RequestID, ReasonInvalidParams))}, nil
	}

	out := r.cancelled()
	r.release(IdleCancelled)
	return append(out, r.announce(m, h.RequestID)...), nil
}

func (r *Receiver) End() []Message {
	if r.app == nil {
		return nil
	}
	out := r.cancelled()
	r.release(IdleCancelled)
	return append(out, r.broadcast())
}

func (r *Receiver) broadcast() Message {
	return Message{
		Source:      ReceiverID,
		Destination: Broadcast,
		Namespace:   NSReceiver,
		Payload:     Status(0, r.Status()),
	}
}

func (r *Receiver) mediaBroadcast(status []MediaStatus) Message {
	return Message{
		Source:      r.app.TransportID,
		Destination: Broadcast,
		Namespace:   NSMedia,
		Payload:     MediaBroadcastPayload(status...),
	}
}

func (r *Receiver) cancelled() []Message {
	if r.session == 0 || r.app == nil {
		return nil
	}
	r.state, r.idle = StateIdle, IdleCancelled
	return []Message{r.mediaBroadcast(r.playing())}
}

func (r *Receiver) release(why string) {
	if r.app == nil {
		return
	}

	if r.Player != nil {
		_ = r.Player.Stop()
	}
	r.appMedia = appPlayer{}

	app := r.app.AppID
	r.app = nil
	r.media = nil
	r.state = StateIdle
	r.idle = why
	r.session = 0
	r.bracket(Lookup(app), false)

	if r.Stopped != nil {
		r.Stopped(app)
	}
}

func (r *Receiver) setVolume(m Message, h Header) ([]Message, error) {
	req, err := ParseSetVolume(m.Payload)
	if err != nil {
		return []Message{reply(m, NSReceiver, Invalid(h.RequestID, ReasonInvalidParams))}, nil
	}
	r.applyVolume(req)
	return r.announce(m, h.RequestID), nil
}

func (r *Receiver) applyVolume(req SetVolumeRequest) {
	if req.Volume.Level != nil {
		r.volume.Level = ptr(*req.Volume.Level)
	}
	if req.Volume.Muted != nil {
		r.volume.Muted = ptr(*req.Volume.Muted)
	}

	if r.Volume != nil {
		r.Volume(*r.volume.Level, *r.volume.Muted)
	}
}

func (r *Receiver) streamVolume(m Message, h Header) ([]Message, error) {
	req, err := ParseSetVolume(m.Payload)
	if err != nil {
		return []Message{reply(m, NSMedia, Invalid(h.RequestID, ReasonInvalidParams))}, nil
	}
	r.applyVolume(req)
	return append(r.report(m, h.RequestID), r.broadcast()), nil
}

func (r *Receiver) Report(level float64, muted bool) []Message {
	if *r.volume.Level == level && *r.volume.Muted == muted {
		return nil
	}
	r.volume.Level, r.volume.Muted = ptr(level), ptr(muted)
	return []Message{r.broadcast()}
}

func (r *Receiver) announce(m Message, request int) []Message {
	return []Message{reply(m, NSReceiver, Status(request, r.Status())), r.broadcast()}
}

func (r *Receiver) mediaNamespace(m Message) ([]Message, error) {
	h, err := Kind(m.Payload)
	if err != nil {
		return nil, err
	}

	if r.app == nil && h.Type != TypeGetStatus {
		return []Message{reply(m, NSMedia, Invalid(h.RequestID, ReasonInvalidCommand))}, nil
	}

	switch h.Type {
	case TypeLoad:
		return r.load(m, h)
	case TypeGetStatus:
		return []Message{reply(m, NSMedia, MediaStatusPayload(h.RequestID, r.full()...))}, nil
	case TypePlay,
		TypePause,
		TypeStopMedia,
		TypeSeek,
		TypeQueueNext,
		TypeQueuePrev,
		TypeQueueUpdate:
		return r.transport(m, h)
	case TypeSetVolume:
		return r.streamVolume(m, h)
	}

	return []Message{reply(m, NSMedia, Invalid(h.RequestID, ReasonInvalidCommand))}, nil
}

func (r *Receiver) load(m Message, h Header) ([]Message, error) {
	if l := r.loader(); l != nil {
		return l.Load(r.app, m)
	}
	req, err := ParseLoad(m.Payload)
	if err != nil {
		return []Message{reply(m, NSMedia, LoadFailed(h.RequestID))}, nil
	}
	if r.Loaded != nil {
		r.Loaded(req.Media)
	}

	r.sessions++
	r.session = r.sessions
	r.media = &req.Media
	r.idle = ""

	at := seconds(req.CurrentTime)

	if r.Player != nil {
		if err := r.Player.Load(req.Media, at, req.Starts()); err != nil {
			r.media = nil
			r.session = 0
			r.state = StateIdle
			r.idle = IdleError
			return []Message{reply(m, NSMedia, LoadFailed(h.RequestID))},
				fmt.Errorf("cast: loading %q: %w", req.Media.URL(), err)
		}
	}

	r.state = StatePlaying
	if !req.Starts() {
		r.state = StatePaused
	}

	return r.report(m, h.RequestID), nil
}

func (r *Receiver) transport(m Message, h Header) ([]Message, error) {
	req, err := ParseMediaRequest(m.Payload)
	if err != nil {
		return []Message{reply(m, NSMedia, Invalid(h.RequestID, ReasonInvalidParams))}, nil
	}

	if req.MediaSessionID != 0 && req.MediaSessionID != r.session {
		return []Message{reply(m, NSMedia, Invalid(h.RequestID, ReasonInvalidParams))}, nil
	}

	var act error
	switch h.Type {
	case TypePlay:
		r.state = StatePlaying
		if ctl := r.control(); ctl != nil {
			act = ctl.Play()
		}

	case TypePause:
		r.state = StatePaused
		if ctl := r.control(); ctl != nil {
			act = ctl.Pause()
		}

	case TypeStopMedia:
		if ctl := r.control(); ctl != nil {
			act = ctl.Stop()
		}
		r.media = nil
		r.session = 0
		r.state = StateIdle
		r.idle = IdleCancelled

	case TypeSeek:
		if (r.media != nil && r.media.Streaming()) || req.CurrentTime < 0 ||
			math.IsNaN(req.CurrentTime) ||
			math.IsInf(req.CurrentTime, 0) {
			return []Message{reply(m, NSMedia, Invalid(h.RequestID, ReasonInvalidParams))}, nil
		}
		if ctl := r.control(); ctl != nil {
			act = ctl.Seek(req.Seek())
		}
		switch req.ResumeState {
		case ResumePlay:
			r.state = StatePlaying
		case ResumePause:
			r.state = StatePaused
		}

	case TypeQueueNext, TypeQueuePrev, TypeQueueUpdate:
		by := map[string]int{TypeQueueNext: 1, TypeQueuePrev: -1}[h.Type]
		if h.Type == TypeQueueUpdate {
			by = req.Jump
		}
		step, ok := r.control().(Stepping)
		if !ok || by == 0 {
			return []Message{reply(m, NSMedia, Invalid(h.RequestID, ReasonInvalidCommand))}, nil
		}
		act = step.Step(by)
	}

	if act != nil {
		return []Message{reply(m, NSMedia, Invalid(h.RequestID, ReasonInvalidCommand))},
			fmt.Errorf("cast: %s: %w", h.Type, act)
	}
	return r.report(m, h.RequestID), nil
}

func (r *Receiver) playing() []MediaStatus {
	if r.session == 0 {
		return nil
	}

	s := MediaStatus{
		MediaSessionID:         r.session,
		PlaybackRate:           1,
		PlayerState:            r.state,
		SupportedMediaCommands: Speaker,
		Volume:                 r.volume,
		IdleReason:             r.idle,
	}
	if r.media != nil {
		s.MediaCategory = r.media.Category()
		if r.media.Streaming() {
			s.SupportedMediaCommands &^= CommandSeek
		}
	}

	if r.appMedia.control != nil {
		s.SupportedMediaCommands, s.CustomData, s.MediaCategory = r.appMedia.commands, r.appMedia.custom, ""
	}
	if ctl := r.control(); ctl != nil {
		s.CurrentTime = asSeconds(ctl.Elapsed())
	}
	return []MediaStatus{s}
}

func (r *Receiver) full() []MediaStatus {
	full := r.playing()
	if len(full) > 0 {
		full[0].Media = r.media
	}
	return full
}

func (r *Receiver) report(m Message, request int) []Message {
	full := r.full()
	return []Message{
		reply(m, NSMedia, MediaStatusPayload(request, full...)),
		r.mediaBroadcast(full),
	}
}

func (r *Receiver) Finished() []Message {
	if r.session == 0 {
		return nil
	}

	r.state = StateIdle
	r.idle = IdleFinished
	r.media = nil

	status := r.playing()
	r.session = 0
	if r.app == nil {
		return nil
	}
	return []Message{r.mediaBroadcast(status)}
}

func (r *Receiver) Done() []Message {
	if r.appMedia.control == nil && r.Player != nil && r.session != 0 {
		return r.Finished()
	}
	return r.End()
}

func (r *Receiver) Forget() {
	r.release(IdleCancelled)
	clear(r.senders)
	r.idle = ""
}
