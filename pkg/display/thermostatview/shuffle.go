package thermostatview

import (
	"time"
	"unicode"
)

const shuffleFor = 700 * time.Millisecond

const shuffleGlyphs = "0123456789#%&@$?"

type shuffle struct {
	text string
	at   time.Time
}

func (v *View) settle(key, text string, now time.Time) (string, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	s, ok := v.shuffles[key]
	switch {
	case !ok:
		v.shuffles[key] = shuffle{text: text}
		return text, false
	case s.text != text:
		s = shuffle{text: text, at: now}
		v.shuffles[key] = s
	}
	gone := now.Sub(s.at)
	if gone >= shuffleFor {
		return text, false
	}
	runes := []rune(text)
	settled := int(float64(len(runes)) * float64(gone) / float64(shuffleFor))
	tick := now.UnixMilli() / 45
	for i := settled; i < len(runes); i++ {
		if unicode.IsDigit(runes[i]) {
			runes[i] = rune(shuffleGlyphs[(tick*7+int64(i)*13)%int64(len(shuffleGlyphs))])
		}
	}
	return string(runes), true
}
