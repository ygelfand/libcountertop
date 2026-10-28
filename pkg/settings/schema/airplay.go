package schema

type AirPlay struct {
	Enabled bool             `json:"enabled"`
	Audio   AirPlayAudioMode `json:"audio_mode"`
}

type AirPlayAudioMode string

const (
	AirPlayInstant AirPlayAudioMode = "instant"
	AirPlaySynced  AirPlayAudioMode = "synced"
)

func AirPlayAudioModes() []AirPlayAudioMode {
	return []AirPlayAudioMode{AirPlayInstant, AirPlaySynced}
}

func (m AirPlayAudioMode) Synced() bool { return m == AirPlaySynced }

func DefaultAirPlay() AirPlay { return AirPlay{Audio: AirPlayInstant} }
