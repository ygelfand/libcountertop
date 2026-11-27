package turn

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"sync/atomic"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"
	"github.com/ygelfand/go-esphome-device/api"

	"github.com/ygelfand/libcountertop/pkg/assistant/activity"
	"github.com/ygelfand/libcountertop/pkg/hook"
	"github.com/ygelfand/libcountertop/pkg/inference/wake"
	"github.com/ygelfand/libcountertop/pkg/runtime/safe"
)

// Home Assistant's reply to satellites that lost the wake race: "Duplicate wake-up detected for Glados".
const errDuplicate = "duplicate_wake_up_detected"

// Home Assistant's reply when nothing was said after the wake word: "No text recognized".
const errNoText = "stt-no-text-recognized"

type phase = Phase

const (
	phaseIdle      = Idle
	phaseListening = Listening
	phaseThinking  = Thinking
	phaseReplying  = Replying
)

type eventKind int

const (
	evStart eventKind = iota
	evHeard
	evReplyText
	evReplyURL
	evStreamStart
	evStreamAudio
	evStreamEnd
	evPlayed
	evRunEnd
	evError
	evCancel
	evTimeout
	evPending
	evPlaying
	evContinue
	evSpeaking
	evAccepted
	evListen
	evRelease
	evVoice
	evRejected
)

type event struct {
	replyID uint64
	kind    eventKind
	slot    int
	text    string
	url     string
	audio   []byte
	code    string
	msg     string
	at      time.Time
	woke    wake.Detection
}

type Conversation struct {
	replyID uint64
	options Dependencies
	Word    func(int) (string, bool)
	Ended   hook.Hook[struct{}]
	vs      *esphome.VoiceSatellite
	source  Microphone
	speaker Speaker

	sound  Sound
	player interface{ Sounding(bool) }
	log    *activity.Log

	events chan event

	out chan func() error

	quietUntil atomic.Int64

	visible atomic.Int32

	phase phase

	slot int

	stopAudio func()
	deadline  *time.Timer
	gate      *gate

	holding bool

	followUp bool

	turn *activity.Turn

	// Home Assistant's events carry no run identifier.
	pending *nextTurn
	grace   *time.Timer

	reply reply

	said   string
	answer string
}

const graceStart = 750 * time.Millisecond

type nextTurn struct {
	slot     int
	followUp bool
	heard    time.Time
	woke     wake.Detection
}

type reply struct {
	url    string
	stream *Reply
}

func NewConversation(vs *esphome.VoiceSatellite, options Dependencies) *Conversation {
	if options.Models == nil {
		options.Models = func() []wake.Model { return nil }
	}
	if options.Presentation == nil {
		options.Presentation = &Presentation{}
	}
	c := &Conversation{
		options: options,
		Word:    options.Word,
		vs:      vs,
		source:  options.Source,
		speaker: options.Speaker,
		sound:   options.Sound,
		player:  options.Player,
		log:     options.Log,
		events:  make(chan event, 32),
		out:     make(chan func() error, outDepth),
	}

	vs.OnPipelineEvent = c.pipeline
	vs.OnTTSAudio = c.tts
	vs.OnAnnounce = c.announce
	vs.OnStartRequestAccepted = func(port uint32, failed bool) {
		if failed {
			slog.Error("home assistant refused the turn")
			c.post(event{kind: evError, code: "refused"})
			return
		}
		if port != 0 {
			slog.Warn("home assistant asked for udp audio", "port", port)
		}
	}
	return c
}

const (
	outDepth  = 64
	voiceRate = 16000
	preRoll   = 250 * time.Millisecond
	// ESPHome's default wake_word_audio.buffer_duration.
	wakeWordAudio = 1500 * time.Millisecond
	enrolling     = 1e6
)

func (c *Conversation) send(what string, fn func() error) {
	select {
	case c.out <- fn:
	default:
		slog.Warn("dropped a message to home assistant", "what", what)
	}
}

func (c *Conversation) sendAudio(b []byte) {
	frame := append([]byte(nil), b...)
	select {
	case c.out <- func() error { return c.vs.SendAudio(frame) }:
	default:
	}
}

func (c *Conversation) sends(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case fn := <-c.out:
			if err := fn(); err != nil {
				slog.Error("sending to home assistant failed", "err", err)
				for len(c.out) > 0 {
					<-c.out
				}
				c.post(event{kind: evError, code: "send", msg: err.Error()})
			}
		}
	}
}

func (c *Conversation) post(e event) {
	select {
	case c.events <- e:
	default:
		slog.Warn("Conversation event dropped", "kind", e.kind, "phase", c.Phase())
	}
}

func (c *Conversation) Phase() phase { return phase(c.visible.Load()) }

func (c *Conversation) Run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	c.options.Presentation.Begin()
	defer c.options.Presentation.End()

	senderDone := make(chan struct{})
	safe.Go("voice sender", func() { defer close(senderDone); c.sends(ctx) })
	defer func() { cancel(); c.stopStreaming(); c.disarm(); c.clearPending(); <-senderDone }()

	for {
		var expired, waited <-chan time.Time
		if c.deadline != nil {
			expired = c.deadline.C
		}
		if c.grace != nil {
			waited = c.grace.C
		}

		select {
		case <-ctx.Done():
			c.handle(event{kind: evCancel})
			return
		case <-expired:
			c.handle(event{kind: evTimeout})
		case <-waited:
			c.handle(event{kind: evPending})
		case e := <-c.events:
			c.handle(e)
		}
	}
}

func (c *Conversation) handle(e event) {
	switch e.kind {
	case evStart:
		c.start(nextTurn{slot: e.slot, heard: e.at, woke: e.woke})

	case evSpeaking:
		if c.followUp && c.phase == phaseListening {
			c.arm(c.options.Words.MaxListen(c.slot))
		}

	case evAccepted:
		if c.phase == phaseListening && c.gate == nil {
			c.accept()
		}

	case evVoice:
		if c.gate == nil || !c.gate.heard.Equal(e.at) || c.phase != phaseListening {
			return
		}
		if e.text == "" {
			slog.Info("unrecognized voice, turn dropped", "slot", c.slot+1)
			c.clearPending()
			c.idle("unrecognized voice", activity.Cancelled)
			c.trouble()
			return
		}
		slog.Info("recognized voice, turn sent", "slot", c.slot+1, "speaker", e.text)
		c.send("start", c.gate.begin)
		c.gate.release()

	case evRejected:
		if c.phase != phaseListening {
			return
		}
		slog.Info("home assistant did not verify the wake word", "slot", c.slot+1)
		c.clearPending()
		c.idle("wake word not verified", activity.Cancelled)

	case evContinue:
		if c.phase != phaseIdle {
			slog.Debug("pipeline asked for an answer", "slot", c.slot+1)
			c.pending = &nextTurn{slot: c.slot, followUp: true}
		}

	case evHeard:
		if e.text != "" {
			slog.Info("heard", "slot", c.slot+1, "text", e.text)
			c.log.Heard(e.text)
			c.said = e.text
			c.show()
		}
		c.turn.Heard(e.text)
		if c.phase == phaseListening {
			c.think()
		}

	case evReplyText:
		slog.Info("replying", "slot", c.slot+1, "text", e.text)
		c.log.Replied(e.text)
		c.answer = e.text
		c.show()
		c.turn.Replying(e.text)
		if c.phase == phaseListening {
			c.think()
		}
		c.player.Sounding(true)

	case evStreamStart:
		if c.phase != phaseIdle && c.options.Words.Delivery(c.slot) == "stream" {
			c.speak("")
		}

	case evReplyURL:
		if c.phase == phaseIdle {
			return
		}
		if c.options.Words.Delivery(c.slot) == "stream" {
			return
		}
		c.speak(e.url)

	case evStreamAudio:
		if c.phase == phaseIdle || c.reply.url != "" {
			return
		}
		if c.phase != phaseReplying {
			c.speak("")
		}
		c.reply.stream.Send(e.audio)

	case evStreamEnd:
		if c.phase == phaseReplying && c.reply.stream != nil {
			c.reply.stream.Done()
		}

	case evPlayed:
		if e.replyID != c.replyID {
			return
		}
		c.reported(e.at)
		if c.phase == phaseReplying {
			slot := c.slot
			c.idle("spoken", activity.Completed)

			if c.pending == nil && c.options.Words.FollowUp(slot) > 0 {
				slog.Info(
					"listening again after the reply",
					"slot",
					slot+1,
					"for",
					c.options.Words.FollowUp(slot),
				)
				c.pending = &nextTurn{slot: slot, followUp: true}
			}
			c.startPending()
		}

	case evRunEnd:
		if c.phase == phaseIdle {
			c.startPending()
			return
		}
		if c.phase != phaseReplying {
			c.idle("ended", activity.Completed)
		}

	case evPending:
		c.startPending()

	case evPlaying:
		if e.replyID != c.replyID || c.phase != phaseReplying {
			return
		}
		c.disarm()

	case evError:
		if e.code == errDuplicate {
			slog.Info("another device answered first", "slot", c.slot+1, "message", e.msg)
			c.clearPending()
			c.idle("answered elsewhere", activity.Cancelled)
			c.options.Presentation.Elsewhere()
			return
		}
		if e.code == errNoText {
			slog.Info("nothing heard", "slot", c.slot+1, "message", e.msg)
			c.clearPending()
			c.idle("nothing heard", activity.Cancelled)
			return
		}

		slog.Error("pipeline error", "slot", c.slot+1, "code", e.code, "message", e.msg)
		c.clearPending()
		c.idle("failed", activity.Failed)
		c.trouble()

	case evCancel:
		c.clearPending()

		sounding := c.sound.Busy()
		c.sound.Silence()

		if c.phase == phaseIdle && !sounding {
			return
		}
		slog.Info("cancelled", "phase", c.phase, "slot", c.slot+1, "sounding", sounding)
		c.idle("cancelled", activity.Cancelled)
		c.options.Cancelled()

	case evListen:
		if c.phase == phaseIdle {
			c.options.Presentation.Show(Showing{Phase: phaseListening, Slot: c.slot})
			c.options.Presentation.Phase(c.slot, phaseListening)
		}

	case evRelease:
		if c.phase == phaseIdle {
			c.present()
		}

	case evTimeout:
		c.clearPending()

		if c.followUp && c.phase == phaseListening {
			slog.Info("nothing followed", "slot", c.slot+1)
			c.idle("nothing said", activity.Cancelled)
			return
		}

		switch c.phase {
		case phaseListening:
			slog.Warn(
				"gave up listening",
				"slot",
				c.slot+1,
				"after",
				c.options.Words.MaxListen(c.slot),
			)
		case phaseThinking:
			slog.Warn(
				"gave up waiting for an answer",
				"slot",
				c.slot+1,
				"after",
				c.options.Words.MaxThink(c.slot),
			)
		}
		c.idle("timed out", activity.Timeout)
		c.trouble()
	}
}

func (c *Conversation) muted() (bool, error) { return c.options.Muted() }

func (c *Conversation) start(n nextTurn) {
	slot := n.slot

	if muted, err := c.muted(); err == nil && muted {
		slog.Info("microphone muted, not starting a turn", "slot", slot+1)
		c.trouble()
		return
	}

	if c.phase == phaseListening {
		slog.Info("wake word ignored, still listening", "slot", slot+1)
		return
	}

	if c.phase != phaseIdle {
		slog.Info("Conversation interrupted", "was", c.phase, "from", c.slot+1, "slot", slot+1)
		c.sound.Silence()

		c.pending = &nextTurn{slot: slot}
		c.idle("interrupted", activity.Cancelled)

		c.grace = time.NewTimer(graceStart)
		return
	}
	c.clearPending()

	if !c.vs.Subscribed() {
		slog.Warn("no voice pipeline subscribed, ignoring wake", "slot", slot+1)
		c.options.Words.Chime(slot)
		c.trouble()
		return
	}

	phrase, ok := c.PhraseFor(slot)
	if slot > 0 && !ok {
		slog.Warn("no wake word in that slot, nothing to reach its pipeline with", "slot", slot+1)
		c.trouble()
		return
	}

	c.slot = slot
	c.followUp = n.followUp

	c.hold(true)

	c.quietUntil.Store(0)
	if !n.followUp {
		c.log.Woke(phrase)
	}
	c.turn = c.log.Begin(slot+1, phrase)
	c.options.Recorder.Opens(c.turn.ID(), slot)
	frames, unlisten := c.source.Listen("turn")
	begin, pre := c.opening(n, phrase)
	if c.recognizes(n) {
		c.gate = &gate{heard: n.heard, begin: begin, send: c.sendAudio}
	} else {
		c.send("start", begin)
	}

	c.said, c.answer = "", ""
	c.enter(phaseListening)
	c.turn.Listening()
	c.reply = reply{}

	c.arm(c.listenFor(n))
	c.startAudio(slot, n.heard, frames, unlisten, pre)
	if c.gate != nil {
		c.accept()
	}
	slog.Info("turn started", "slot", slot+1, "phrase", phrase, "follow_up", n.followUp,
		"recognized_only", c.gate != nil)
}

func (c *Conversation) recognizes(n nextTurn) bool {
	return c.options.Voices != nil && !n.followUp && !n.heard.IsZero() &&
		c.options.Words.KnownVoices(n.slot)
}

func (c *Conversation) accept() {
	c.present()
	c.options.Words.Chime(c.slot)
	if c.options.Words.Tones(c.slot) {
		c.quietUntil.Store(
			time.Now().
				Add(c.options.Words.ChimeLength(c.slot) + c.options.HardwareTail).
				UnixNano(),
		)
	}
}

func (c *Conversation) reached() bool { return c.gate == nil || c.gate.opened() }

func (c *Conversation) listenFor(n nextTurn) time.Duration {
	if n.followUp {
		if d := c.options.Words.FollowUp(n.slot); d > 0 {
			return d
		}
	}
	return c.options.Words.MaxListen(n.slot)
}

func (c *Conversation) think() {
	c.stopStreaming()
	c.enter(phaseThinking)
	c.arm(c.options.Words.MaxThink(c.slot))

}

func (c *Conversation) speak(url string) {
	c.replyID++
	id := c.replyID
	c.stopStreaming()
	c.enter(phaseReplying)
	c.reply = reply{url: url}

	c.player.Sounding(true)

	errand := func(ctx context.Context, p Speaker) error {
		return c.play(ctx, url, func() { c.post(event{kind: evPlaying, replyID: id}) })
	}
	if url == "" {
		s := NewReply(c.speaker.Splices(), c.speaker.Underruns(), c.options.Words.Buffer(c.slot))
		c.reply.stream = s
		errand = func(ctx context.Context, p Speaker) error {
			return s.Play(ctx, p, func() { c.post(event{kind: evPlaying, replyID: id}) })
		}
	}

	held := c.sound.Claim("reply", errand)
	safe.Go("reply", func() {
		<-held.Done()

		if err := held.Err(); err != nil {
			slog.Error("playing the reply failed", "url", url, "err", err)
		}
		if held.Stopped() {
			return
		}
		c.post(event{kind: evPlayed, at: held.Quiet(), replyID: id})
	})
}

func (c *Conversation) idle(why string, how activity.Outcome) {
	c.replyID++
	was := c.phase
	c.stopStreaming()
	c.disarm()

	if was != phaseIdle && c.reached() {
		c.send("stop", c.vs.StopTurn)
	}
	c.gate = nil

	kept := c.options.Recorder
	kept.Closes()
	c.turn.Records(kept.Seconds(c.turn.ID()))
	c.turn.Ends(how)
	c.turn = nil

	c.enter(phaseIdle)
	c.player.Sounding(false)
	c.reply = reply{}
	c.hold(c.pending != nil)

	if was != phaseIdle {
		slog.Info("turn ended", "was", was, "slot", c.slot+1, "why", why)
		c.Ended.Emit(struct{}{})
	}
}

func (c *Conversation) enter(p phase) {
	c.phase = p
	c.visible.Store(int32(p))
	c.options.Source.SetAdapting(p != phaseListening)
	if p != phaseListening {
		c.present()
	}
}

func (c *Conversation) present() {
	c.show()
	c.options.Presentation.Phase(c.slot, c.phase)
}

func (c *Conversation) show() {
	c.options.Presentation.Show(Showing{
		Phase: c.phase,
		Slot:  c.slot,
		Said:  c.said,
		Reply: c.answer,
	})
}

func (c *Conversation) startPending() {
	next := c.pending
	c.clearPending()

	if next != nil {
		c.start(*next)
	}
}

func (c *Conversation) clearPending() {
	c.pending = nil
	if c.grace != nil {
		c.grace.Stop()
		c.grace = nil
	}

	if c.phase == phaseIdle {
		c.hold(false)
	}
}

func (c *Conversation) hold(on bool) {
	if on == c.holding {
		return
	}
	c.holding = on

	c.options.Sound.Duck(on)
	c.log.Ducked(on)
}

func (c *Conversation) arm(d time.Duration) {
	c.disarm()
	c.deadline = time.NewTimer(d)
}

func (c *Conversation) disarm() {
	if c.deadline != nil {
		c.deadline.Stop()
		c.deadline = nil
	}
}

func (c *Conversation) trouble() {
	c.options.Failure()
}

func (c *Conversation) PhraseFor(slot int) (string, bool) {
	if c.Word == nil {
		return "", false
	}
	id, ok := c.Word(slot)
	if !ok {
		return "", false
	}
	if m, ok := wake.Find(c.options.Models(), id); ok {
		return m.Phrase, true
	}
	return id, true
}

func (c *Conversation) pipeline(e esphome.PipelineEvent) {
	switch e.Type {
	case api.VoiceAssistantEvent_VOICE_ASSISTANT_STT_START:
		c.post(event{kind: evAccepted})
	case api.VoiceAssistantEvent_VOICE_ASSISTANT_STT_END:
		c.post(event{kind: evHeard, text: e.Data["text"]})
	case api.VoiceAssistantEvent_VOICE_ASSISTANT_STT_VAD_START:
		c.post(event{kind: evSpeaking})
	case api.VoiceAssistantEvent_VOICE_ASSISTANT_STT_VAD_END:
		c.post(event{kind: evHeard})
	case api.VoiceAssistantEvent_VOICE_ASSISTANT_INTENT_PROGRESS:
		if e.Data["tts_start_streaming"] == "1" {
			slog.Debug("early tts streaming offered", "slot", c.slot+1)
		}

	case api.VoiceAssistantEvent_VOICE_ASSISTANT_INTENT_END:
		if e.Data["continue_conversation"] == "1" {
			c.post(event{kind: evContinue})
		}
	case api.VoiceAssistantEvent_VOICE_ASSISTANT_TTS_START:
		c.post(event{kind: evReplyText, text: e.Data["text"]})
	case api.VoiceAssistantEvent_VOICE_ASSISTANT_TTS_END:
		if url := e.Data["url"]; url != "" {
			c.post(event{kind: evReplyURL, url: url})
		}

	case api.VoiceAssistantEvent_VOICE_ASSISTANT_TTS_STREAM_START:
		c.post(event{kind: evStreamStart})
	case api.VoiceAssistantEvent_VOICE_ASSISTANT_TTS_STREAM_END:
		c.post(event{kind: evStreamEnd})
	case api.VoiceAssistantEvent_VOICE_ASSISTANT_RUN_END:
		c.post(event{kind: evRunEnd})
	case api.VoiceAssistantEvent_VOICE_ASSISTANT_ERROR:
		c.post(event{kind: evError, code: e.Data["code"], msg: e.Data["message"]})
	case api.VoiceAssistantEvent_VOICE_ASSISTANT_WAKE_WORD_REJECTED:
		c.post(event{kind: evRejected})
	}
}

func (c *Conversation) tts(data []byte, end bool) {
	if len(data) > 0 {
		c.post(event{kind: evStreamAudio, audio: data})
	}
	// TTS_STREAM_END marks the end of streamed audio; the per-message end flag is unreliable.
	_ = end
}

func (c *Conversation) Start(slot int) { c.post(event{kind: evStart, slot: slot}) }

func (c *Conversation) Heard(d wake.Detection) {
	slot := d.Slot
	at := time.Now()
	if c.options.Voices != nil && !c.Busy() && c.options.Voices.Claim(at) {
		if c.options.Arbiter != nil {
			phrase, _ := c.PhraseFor(slot)
			safe.Go("enrolment claim", func() { c.options.Arbiter.Decide(phrase, enrolling) })
		}
		return
	}
	if c.options.Arbiter == nil || c.Busy() {
		c.post(event{kind: evStart, slot: slot, at: at, woke: d})
		return
	}
	phrase, _ := c.PhraseFor(slot)
	score := c.source.Over(time.Second)
	slog.Info("wake score", "over_floor_db", score, "floor_dbfs", c.source.Floor(),
		"gain_db", c.source.Gain())
	safe.Go("wake arbitration", func() {
		if !c.options.Arbiter.Decide(phrase, score) {
			c.options.Presentation.Elsewhere()
			return
		}
		c.post(event{kind: evStart, slot: slot, at: at, woke: d})
	})
}

func (c *Conversation) Cancel() { c.post(event{kind: evCancel}) }

func (c *Conversation) Listen() (release func()) {
	c.post(event{kind: evListen})
	var once sync.Once
	return func() { once.Do(func() { c.post(event{kind: evRelease}) }) }
}

func (c *Conversation) Busy() bool { return c.Phase() != phaseIdle }

func audioSettings() *api.VoiceAssistantAudioSettings {
	return &api.VoiceAssistantAudioSettings{
		NoiseSuppressionLevel: 2,
		AutoGain:              31,
		VolumeMultiplier:      1,
	}
}

func (c *Conversation) opening(n nextTurn, phrase string) (func() error, []int16) {
	var since time.Duration
	if !n.heard.IsZero() {
		since = time.Since(n.heard)
	}
	plain := func() error { return c.vs.StartTurn(phrase, audioSettings()) }

	id, named := "", false
	if c.Word != nil {
		id, named = c.Word(n.slot)
	}
	if named && !n.heard.IsZero() && !n.followUp && c.vs.WakeWordAudio() {
		pre := c.source.Recent(wakeWordAudio + since)
		w := esphome.WakeWordAudio{
			ID:          id,
			Probability: float32(n.woke.Score),
			Cutoff:      float32(n.woke.Cutoff),
			Bytes:       beforeDetection(pre, since),
		}
		return func() error { return c.vs.StartTurnFromWakeWord(phrase, audioSettings(), w) }, pre
	}
	if c.options.Words.Tones(n.slot) {
		return plain, nil
	}
	return plain, c.source.Recent(preRoll + since)
}

func beforeDetection(pre []int16, since time.Duration) uint32 {
	after := min(int(since*voiceRate/time.Second), len(pre))
	return uint32(2 * (len(pre) - after))
}

func (c *Conversation) startAudio(
	slot int,
	heard time.Time,
	frames <-chan []int16,
	unlisten func(),
	pre []int16,
) {
	g := c.gate
	if c.options.Voices != nil && !heard.IsZero() {
		t := c.turn
		told := func(name, person string, score float64, err error) {
			if err == nil {
				t.Spoke(name, person, score)
			} else {
				t.Missed()
			}
			if g != nil {
				c.post(event{kind: evVoice, at: g.heard, text: name})
			}
		}
		if c.options.Voices.Spoke(heard, told) {
			t.Awaits()
		} else if g != nil {
			c.post(event{kind: evVoice, at: g.heard})
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	c.stopAudio = func() { cancel(); <-done }

	safe.Go("turn audio", func() {
		defer close(done)
		defer unlisten()
		c.stream(ctx, slot, frames, pre, g)
	})
}

func (c *Conversation) stopStreaming() {
	if c.stopAudio == nil {
		return
	}
	c.stopAudio()
	c.stopAudio = nil

	if c.reached() {
		c.send("end of audio", c.vs.EndAudio)
	}
}

func (c *Conversation) stream(
	ctx context.Context,
	slot int,
	frames <-chan []int16,
	pre []int16,
	g *gate,
) {
	deliver := c.sendAudio
	if g != nil {
		deliver = g.pass
	}

	buf := make([]byte, 0, 320*2)

	if len(pre) > 0 {
		for _, s := range pre {
			buf = append(buf, byte(s), byte(s>>8))
		}
		c.options.Recorder.Frame(buf)

		for b := buf; len(b) > 0; {
			n := min(len(b), releaseChunk)
			deliver(b[:n])
			b = b[n:]
		}
		slog.Debug("sent audio history", "ms", len(pre)*1000/voiceRate)
	}

	var held int

	var peak, samples int
	var energy float64
	defer func() {
		if samples == 0 {
			return
		}
		rms := math.Sqrt(energy / float64(samples))
		slog.Debug("sent audio",
			"slot", slot+1,
			"seconds", float64(samples)/float64(16000),
			"peak", peak,
			"peakdbfs", math.Round(20*math.Log10(max(float64(peak), 1)/32768)*10)/10,
			"rms", math.Round(rms),
			"rmsdbfs", math.Round(20*math.Log10(math.Max(rms, 1)/32768)*10)/10,
			"gaindb", math.Round(c.source.Gain()*10)/10,
			"clipped", c.source.Clipped())
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case frame, ok := <-frames:
			if !ok {
				return
			}

			if until := c.quietUntil.Load(); until != 0 {
				if time.Now().UnixNano() < until {
					held += len(frame)
					continue
				}
				if held > 0 {
					slog.Debug("held the tone back", "ms", held*1000/16000)
					held = 0
				}
			}

			buf = buf[:0]
			for _, s := range frame {
				buf = append(buf, byte(s), byte(s>>8))
				peak = max(peak, int(s), -int(s))
				energy += float64(s) * float64(s)
				samples++
			}
			c.options.Recorder.Frame(buf)

			deliver(buf)
		}
	}
}

func (c *Conversation) reported(dry time.Time) {
	s := c.reply.stream
	if s == nil || s.Stats().Bytes == 0 || s.Stats().PlayingAt.IsZero() {
		return
	}

	seconds := float64(s.Stats().Bytes) / float64(2*16000)
	took := dry.Sub(s.Stats().PlayingAt).Seconds()

	slog.Debug("reply played",
		"via", c.options.Words.Delivery(c.slot),
		"bytes", s.Stats().Bytes,
		"seconds", math.Round(seconds*100)/100,
		"took", math.Round(took*100)/100,
		"gap", math.Round(max(took-seconds, 0)*100)/100,
		"peak", s.Stats().Peak,
		"seams", c.speaker.Splices()-s.Stats().SplicesAt,
		"underruns", c.speaker.Underruns()-s.Stats().UnderrunsAt,
		"clipped", c.speaker.Clipped(),
		"dropped", dropped(c.source))
}
