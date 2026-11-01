package icons

import (
	_ "embed"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/image/font/sfnt"
)

//go:embed mdi.ttf
var MDI []byte

//go:embed homeassistant.svg
var HomeAssistant string

const Family = "Material Design Icons"

const (
	Wifi1      = 0xF091F
	Wifi2      = 0xF0922
	Wifi3      = 0xF0925
	Wifi4      = 0xF0928
	WifiOff    = 0xF092E
	LinkOff    = 0xF0338
	Battery    = 0xF0079
	BatteryLow = 0xF0083
)

func Codepoint(hex string) rune {
	if hex == "" || len(hex) > 6 {
		return 0
	}
	v, err := strconv.ParseUint(hex, 16, 32)
	if err != nil || v < firstCode || v > lastCode {
		return 0
	}
	return rune(v)
}

func Glyph(r rune) string {
	if r == 0 {
		return ""
	}
	return string(r)
}

const (
	firstCode = 0xF0000
	lastCode  = 0xF1FFF
)

var named = sync.OnceValue(func() map[string]rune {
	out := map[string]rune{}
	f, err := sfnt.Parse(MDI)
	if err != nil {
		return out
	}
	var b sfnt.Buffer
	for r := rune(firstCode); r <= lastCode; r++ {
		x, err := f.GlyphIndex(&b, r)
		if err != nil || x == 0 {
			continue
		}
		if name, err := f.GlyphName(&b, x); err == nil && name != "" {
			out[name] = r
		}
	}
	return out
})

func Named(name string) rune {
	name = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(name)), "mdi:")
	if r, ok := named()[name]; ok {
		return r
	}
	return Codepoint(strings.ToUpper(name))
}
