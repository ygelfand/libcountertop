package schema

import (
	"fmt"
	"maps"
	"slices"

	"github.com/ygelfand/libcountertop/pkg/audio/ducking"
	"github.com/ygelfand/libcountertop/pkg/settings/storage"
)

type APIWriter struct{ s storage.Section[API] }

func NewAPIWriter(s storage.Section[API]) APIWriter { return APIWriter{s: s} }

func (w APIWriter) Adopted(v bool) error {
	return w.s.Update(func(c *API) { c.Adopted = v })
}

type BluetoothWriter struct{ s storage.Section[Bluetooth] }

func NewBluetoothWriter(s storage.Section[Bluetooth]) BluetoothWriter {
	return BluetoothWriter{s: s}
}

func (w BluetoothWriter) Proxy(v bool) error {
	return w.s.Update(func(c *Bluetooth) { c.Proxy = v })
}

func (w BluetoothWriter) Speaker(v bool) error {
	return w.s.Update(func(c *Bluetooth) { c.Speaker = v })
}

type CallWriter struct{ s storage.Section[Call] }

func NewCallWriter(s storage.Section[Call]) CallWriter { return CallWriter{s: s} }

func (w CallWriter) Stream(v CallStream) error {
	return w.s.Update(func(c *Call) { c.Stream = v })
}

func (w CallWriter) AutoVideo(v bool) error {
	return w.s.Update(func(c *Call) { c.AutoVideo = v })
}

func (w CallWriter) Incoming(v bool) error {
	return w.s.Update(func(c *Call) { c.Incoming = v })
}

func (w CallWriter) AutoAnswer(v bool) error {
	return w.s.Update(func(c *Call) { c.AutoAnswer = v })
}

func (w CallWriter) PauseWake(v bool) error {
	return w.s.Update(func(c *Call) { c.PauseWake = v })
}

type CameraWriter struct{ s storage.Section[Camera] }

func NewCameraWriter(s storage.Section[Camera]) CameraWriter { return CameraWriter{s: s} }

func (w CameraWriter) Set(name, value string) error {
	return w.Put(map[string]string{name: value})
}

func (w CameraWriter) Put(values map[string]string) error {
	return w.s.Update(func(c *Camera) {
		settings := maps.Clone(c.Settings)
		if settings == nil {
			settings = map[string]string{}
		}
		for name, value := range values {
			if value == "" {
				delete(settings, name)
			} else {
				settings[name] = value
			}
		}
		c.Settings = settings
	})
}

func (w CameraWriter) Clear() error {
	return w.s.Update(func(c *Camera) { c.Settings = map[string]string{} })
}

type CastWriter struct{ s storage.Section[Cast] }

func NewCastWriter(s storage.Section[Cast]) CastWriter { return CastWriter{s: s} }

func (w CastWriter) Oracle(url string) error {
	return w.s.Update(func(c *Cast) { c.Oracle = url })
}

func (w CastWriter) Bundle(url string) error {
	return w.s.Update(func(c *Cast) { c.Bundle = url })
}

func (w CastWriter) Screens(device, music, video string) error {
	return w.s.Update(func(c *Cast) {
		c.YouTube.Device, c.YouTube.Music, c.YouTube.Video = device, music, video
	})
}

type ClockWriter struct{ s storage.Section[Clock] }

func NewClockWriter(s storage.Section[Clock]) ClockWriter { return ClockWriter{s: s} }

func (w ClockWriter) Face(v Face) error {
	return w.s.Update(func(c *Clock) { c.Face = v })
}

func (w ClockWriter) Date(v bool) error {
	return w.s.Update(func(c *Clock) { c.Date = v })
}

func (w ClockWriter) Position(v Position) error {
	return w.s.Update(func(c *Clock) { c.Position = v })
}

func (w ClockWriter) Align(v Align) error {
	return w.s.Update(func(c *Clock) { c.Align = v })
}

func (w ClockWriter) Size(v Size) error {
	return w.s.Update(func(c *Clock) { c.Size = v })
}

func (w ClockWriter) Ink(v Ink) error {
	return w.s.Update(func(c *Clock) { c.Ink = v })
}

type FeedbackWriter struct{ s storage.Section[Feedback] }

func NewFeedbackWriter(s storage.Section[Feedback]) FeedbackWriter {
	return FeedbackWriter{s: s}
}

func (w FeedbackWriter) Chime(v Chime) error {
	return w.s.Update(func(c *Feedback) { c.Chime = v })
}

type HomeWriter struct{ s storage.Section[Home] }

func NewHomeWriter(s storage.Section[Home]) HomeWriter { return HomeWriter{s: s} }

func (w HomeWriter) Control(key string, on bool) error {
	return w.s.Update(func(h *Home) {
		control := maps.Clone(h.Control)
		if control == nil {
			control = map[string]bool{}
		}
		if on {
			control[key] = true
		} else {
			delete(control, key)
		}
		h.Control = control
	})
}

func (w HomeWriter) Group(key string, n int) error {
	return w.s.Update(func(h *Home) {
		group := maps.Clone(h.Group)
		if group == nil {
			group = map[string]int{}
		}
		group[key] = min(max(n, 0), HomeGroupMost)
		h.Group = group
	})
}

func (w HomeWriter) Enabled(v bool) error {
	return w.s.Update(func(h *Home) { h.Enabled = v })
}

func (w HomeWriter) Combine(v bool) error {
	return w.s.Update(func(h *Home) { h.Combine = v })
}

func (w HomeWriter) Pick(key string, p HomePick) error {
	return w.s.Update(func(h *Home) {
		picks := maps.Clone(h.Picks)
		if picks == nil {
			picks = map[string]HomePick{}
		}
		if p.Empty() {
			delete(picks, key)
		} else {
			picks[key] = p.Clone()
		}
		h.Picks = picks
	})
}

type IdleWriter struct{ s storage.Section[Idle] }

func NewIdleWriter(s storage.Section[Idle]) IdleWriter { return IdleWriter{s: s} }

func (w IdleWriter) After(v Delay) error {
	return w.s.Update(func(c *Idle) { c.After = v })
}

func (w IdleWriter) Media(v Delay) error {
	return w.s.Update(func(c *Idle) { c.Media = v })
}

func (w IdleWriter) Face(v Face) error {
	return w.s.Update(func(c *Idle) { c.Face = v })
}

func (w IdleWriter) Position(v Position) error {
	return w.s.Update(func(c *Idle) { c.Position = v })
}

func (w IdleWriter) Align(v Align) error {
	return w.s.Update(func(c *Idle) { c.Align = v })
}

func (w IdleWriter) Size(v Size) error {
	return w.s.Update(func(c *Idle) { c.Size = v })
}

func (w IdleWriter) Visual(slot int, v IdleVisual) error {
	return w.s.Update(func(c *Idle) {
		if slot == 0 {
			c.First = v
		} else {
			c.Second = v
		}
	})
}

type MediaWriter struct{ s storage.Section[Media] }

func NewMediaWriter(s storage.Section[Media]) MediaWriter { return MediaWriter{s: s} }

func (w MediaWriter) DuckDB(db float64) error {
	if !ducking.Valid(db) {
		return fmt.Errorf(
			"config: ducking must be between %d and %d dB",
			ducking.MinimumDB,
			ducking.MaximumDB,
		)
	}
	return w.s.Update(func(c *Media) { c.DuckDB = db })
}

type NetworkWriter struct{ s storage.Section[Network] }

func NewNetworkWriter(s storage.Section[Network]) NetworkWriter { return NetworkWriter{s: s} }

func (w NetworkWriter) Verify(v bool) error {
	return w.s.Update(func(c *Network) { c.Verify = v })
}

func (w NetworkWriter) Address(v string) error {
	return w.s.Update(func(c *Network) { c.Address = v })
}

type PosterWriter struct{ s storage.Section[Poster] }

func NewPosterWriter(s storage.Section[Poster]) PosterWriter { return PosterWriter{s: s} }

func (w PosterWriter) Enabled(v bool) error {
	return w.s.Update(func(c *Poster) { c.Enabled = v })
}

func (w PosterWriter) Every(v PosterEvery) error {
	return w.s.Update(func(c *Poster) { c.Every = v })
}

func (w PosterWriter) Last(v string) error {
	return w.s.Update(func(c *Poster) { c.Last = v })
}

type LockWriter struct{ s storage.Section[Lock] }

func NewLockWriter(s storage.Section[Lock]) LockWriter { return LockWriter{s: s} }

func (w LockWriter) All(v Lock) error {
	return w.s.Update(func(c *Lock) { *c = v })
}

type RTSPWriter struct{ s storage.Section[RTSP] }

func NewRTSPWriter(s storage.Section[RTSP]) RTSPWriter { return RTSPWriter{s: s} }

func (w RTSPWriter) All(v RTSP) error {
	return w.s.Update(func(c *RTSP) { *c = v })
}

type ScreenWriter struct{ s storage.Section[Screen] }

func NewScreenWriter(s storage.Section[Screen]) ScreenWriter { return ScreenWriter{s: s} }

func (w ScreenWriter) All(v Screen) error {
	return w.s.Update(func(c *Screen) { *c = v })
}

func (w ScreenWriter) Marks(v bool) error {
	return w.s.Update(func(c *Screen) { c.Marks = v })
}

func (w ScreenWriter) Backlight(v int) error {
	return w.s.Update(func(c *Screen) { c.Backlight = v })
}

func (w ScreenWriter) Mode(v ScreenMode) error {
	return w.s.Update(func(c *Screen) { c.Mode = v })
}

func (w ScreenWriter) Theme(v string) error {
	return w.s.Update(func(c *Screen) { c.Theme = v })
}

func (w ScreenWriter) Style(v string) error {
	return w.s.Update(func(c *Screen) { c.Style = v })
}

func (w ScreenWriter) Size(v string) error {
	return w.s.Update(func(c *Screen) { c.Size = v })
}

func (w ScreenWriter) Hours(v HourFormat) error {
	return w.s.Update(func(c *Screen) { c.Hours = v })
}

func (w ScreenWriter) Keyboard(v KeyboardSize) error {
	return w.s.Update(func(c *Screen) { c.Keyboard = v })
}

func (w ScreenWriter) Drawer(v Edge) error {
	return w.s.Update(func(c *Screen) { c.Drawer = v })
}

func (w ScreenWriter) Volume(v Edge) error {
	return w.s.Update(func(c *Screen) { c.Volume = v })
}

func (w ScreenWriter) Alert(v AlertStyle) error {
	return w.s.Update(func(c *Screen) { c.Alert = v })
}

func (w ScreenWriter) Timer(v TimerStyle) error {
	return w.s.Update(func(c *Screen) { c.Timer = v })
}

func (w ScreenWriter) Language(v string) error {
	return w.s.Update(func(c *Screen) { c.Language = v })
}

type SendspinWriter struct{ s storage.Section[Sendspin] }

func NewSendspinWriter(s storage.Section[Sendspin]) SendspinWriter {
	return SendspinWriter{s: s}
}

func (w SendspinWriter) Enabled(v bool) error {
	return w.s.Update(func(c *Sendspin) { c.Enabled = v })
}

type TimeWriter struct{ s storage.Section[Time] }

func NewTimeWriter(s storage.Section[Time]) TimeWriter { return TimeWriter{s: s} }

func (w TimeWriter) Home(v string) error {
	return w.s.Update(func(c *Time) { c.Home = v })
}

func (w TimeWriter) Chosen(v string) error {
	return w.s.Update(func(c *Time) { c.Chosen = v })
}

type UpdateWriter struct{ s storage.Section[Update] }

func NewUpdateWriter(s storage.Section[Update]) UpdateWriter { return UpdateWriter{s: s} }

func (w UpdateWriter) Channel(v string) error {
	return w.s.Update(func(c *Update) { c.Channel = v })
}

func (w UpdateWriter) LastVersion(v string) error {
	return w.s.Update(func(c *Update) { c.LastVersion = v })
}

type VisualWriter struct{ s storage.Section[Visual] }

func NewVisualWriter(s storage.Section[Visual]) VisualWriter { return VisualWriter{s: s} }

func (w VisualWriter) Kind(v string) error {
	return w.s.Update(func(c *Visual) { c.Kind = v })
}

func (w VisualWriter) MaxFPS(v int) error {
	return w.s.Update(func(c *Visual) { c.MaxFPS = v })
}

func (w VisualWriter) Seed(v int) error {
	return w.s.Update(func(c *Visual) { c.Seed = v })
}

func (w VisualWriter) Label(v string) error {
	return w.s.Update(func(c *Visual) { c.Label = v })
}

type StopWriter struct{ s storage.Section[Wake] }

func NewStopWriter(s storage.Section[Wake]) StopWriter { return StopWriter{s: s} }

func (w StopWriter) Threshold(v float64) error {
	return w.s.Update(func(c *Wake) { c.Stop.Threshold = v })
}

type PryonWriter struct{ s storage.Section[Wake] }

func NewPryonWriter(s storage.Section[Wake]) PryonWriter { return PryonWriter{s: s} }

type GeminiWriter struct{ s storage.Section[Wake] }

func NewGeminiWriter(s storage.Section[Wake]) GeminiWriter { return GeminiWriter{s: s} }

func (w GeminiWriter) On(v bool) error {
	return w.s.Update(func(c *Wake) { c.Gemini = v })
}

func (w PryonWriter) Set(v Wake) error {
	return w.s.Update(func(c *Wake) {
		c.Pryon = v.Pryon
		c.Stop.Pryon = v.Stop.Pryon
	})
}

type WakeWriter struct {
	s    storage.Section[Wake]
	slot int
}

func NewWakeWriter(s storage.Section[Wake], slot int) WakeWriter {
	return WakeWriter{s: s, slot: slot}
}

func (w WakeWriter) ID(v string) error {
	return w.word(func(word *WakeWord) { word.ID = v })
}

func (w WakeWriter) Threshold(v float64) error {
	return w.word(func(word *WakeWord) { word.Threshold = v })
}

func (w WakeWriter) Tone(v Chime) error {
	return w.word(func(word *WakeWord) { word.Tone = v })
}

func (w WakeWriter) Effect(v string) error {
	return w.word(func(word *WakeWord) { word.Effect = v })
}

func (w WakeWriter) ThinkingEffect(v string) error {
	return w.word(func(word *WakeWord) { word.ThinkingEffect = v })
}

func (w WakeWriter) ReplyingEffect(v string) error {
	return w.word(func(word *WakeWord) { word.ReplyingEffect = v })
}

func (w WakeWriter) Delivery(v Delivery) error {
	return w.word(func(word *WakeWord) { word.Delivery = v })
}

func (w WakeWriter) FollowUp(seconds int) error {
	return w.word(func(word *WakeWord) { word.FollowUp = seconds })
}

func (w WakeWriter) Buffer(ms int) error {
	return w.word(func(word *WakeWord) { word.Buffer = ms })
}

func (w WakeWriter) MaxListen(seconds int) error {
	return w.word(func(word *WakeWord) { word.MaxListen = seconds })
}

func (w WakeWriter) MaxThink(seconds int) error {
	return w.word(func(word *WakeWord) { word.MaxThink = seconds })
}

func (w WakeWriter) Recordings(count int) error {
	return w.word(func(word *WakeWord) { word.Recordings = count })
}

func (w WakeWriter) Place(v LookPlace) error {
	return w.word(func(word *WakeWord) { word.Look.Place = v })
}

func (w WakeWriter) Visual(s Stage, kind string) error {
	return w.word(func(word *WakeWord) {
		switch s {
		case StageListening:
			word.Look.Listening = kind
		case StageWaiting:
			word.Look.Waiting = kind
		case StageResponding:
			word.Look.Responding = kind
		}
	})
}

func (w WakeWriter) Apply(f func(*WakeWord)) error { return w.word(f) }

func (w WakeWriter) word(f func(*WakeWord)) error {
	if w.slot < 0 {
		return fmt.Errorf("config: wake slot %d", w.slot)
	}
	return w.s.Update(func(c *Wake) {
		words := slices.Clone(c.Words)
		for len(words) <= w.slot {
			words = append(words, DefaultWakeWord())
		}
		f(&words[w.slot])
		c.Words = words
	})
}

type WeatherWriter struct{ s storage.Section[Weather] }

func NewWeatherWriter(s storage.Section[Weather]) WeatherWriter { return WeatherWriter{s: s} }

func (w WeatherWriter) Entity(v string) error {
	return w.s.Update(func(c *Weather) { c.Entity = v })
}

func (w WeatherWriter) Look(v WeatherLook) error {
	return w.s.Update(func(c *Weather) { c.Look = v })
}

func (w WeatherWriter) Dashboard(v bool) error {
	return w.s.Update(func(c *Weather) { c.Dashboard = v })
}

func (w WeatherWriter) Idle(v bool) error {
	return w.s.Update(func(c *Weather) { c.Idle = v })
}

func (w WeatherWriter) Alerts(v bool) error {
	return w.s.Update(func(c *Weather) { c.Alerts = v })
}

func (w WeatherWriter) RadarSource(v RadarSource) error {
	return w.s.Update(func(c *Weather) { c.RadarSource = v })
}

func (w WeatherWriter) RadarRange(v RadarRange) error {
	return w.s.Update(func(c *Weather) { c.RadarRange = v })
}

func (w WeatherWriter) Animate(v bool) error {
	return w.s.Update(func(c *Weather) { c.Animate = v })
}

func (w WeatherWriter) Themed(v bool) error {
	return w.s.Update(func(c *Weather) { c.Themed = v })
}

func (w WeatherWriter) Position(v Position) error {
	return w.s.Update(func(c *Weather) { c.Position = v })
}

func (w WeatherWriter) Align(v Align) error {
	return w.s.Update(func(c *Weather) { c.Align = v })
}

func (w WeatherWriter) Size(v Size) error {
	return w.s.Update(func(c *Weather) { c.Size = v })
}

func (w WeatherWriter) Saver(change func(*WeatherLayout)) error {
	return w.s.Update(func(c *Weather) { change(c.SeedSaver()) })
}
