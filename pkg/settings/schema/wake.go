package schema

type Wake struct {
	Words []WakeWord `json:"words"`

	Stop Stop `json:"stop"`

	Pryon bool `json:"pryon"`

	Gemini bool `json:"gemini"`
}

type Stop struct {
	Threshold float64 `json:"threshold"`

	Pryon bool `json:"pryon"`
}

const (
	StopOff = 1.0

	DefaultStopThreshold = 0.7
)

func DefaultStop() Stop { return Stop{Threshold: DefaultStopThreshold} }

func DefaultWake() Wake { return Wake{Stop: DefaultStop()} }

func (s Stop) Listening() bool { return s.Threshold < StopOff }

type WakeWord struct {
	ID string `json:"id"`

	Threshold float64 `json:"threshold"`

	Tone           Chime  `json:"tone"`
	Effect         string `json:"effect,omitempty"`
	ThinkingEffect string `json:"thinking_effect,omitempty"`
	ReplyingEffect string `json:"replying_effect,omitempty"`

	Delivery Delivery `json:"delivery"`

	FollowUp int `json:"follow_up"`

	Buffer int `json:"buffer"`

	MaxListen int `json:"max_listen"`
	MaxThink  int `json:"max_think"`

	Recordings int `json:"recordings"`

	Look Look `json:"look"`
}

const (
	DefaultThreshold = 0.85
	DefaultEffect    = "Pulse"
	EffectNone       = "None"
	DefaultTone      = ChimeChirp
	DefaultDelivery  = DeliveryWhole

	DefaultMaxListen = 15
	DefaultMaxThink  = 90

	DefaultFollowUp = 0

	// Home Assistant streams TTS 384 ms ahead of playback.
	DefaultBuffer = 650
)

func DefaultWakeWord() WakeWord {
	return WakeWord{
		Threshold: DefaultThreshold,
		Tone:      DefaultTone,
		Effect:    DefaultEffect,
		Delivery:  DefaultDelivery,
		FollowUp:  DefaultFollowUp,
		Buffer:    DefaultBuffer,
		MaxListen: DefaultMaxListen,
		MaxThink:  DefaultMaxThink,
		Look:      Look{Place: LookPanel},
	}
}

func (w Wake) Slot(n int) WakeWord {
	if n < 0 || n >= len(w.Words) {
		return DefaultWakeWord()
	}
	return w.Words[n]
}

func (w Wake) Slots(n int) []WakeWord {
	out := make([]WakeWord, n)
	for i := range out {
		out[i] = w.Slot(i)
	}
	return out
}

func (w Wake) IDs(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = w.Slot(i).ID
	}
	return out
}
