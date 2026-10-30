package style

import (
	_ "embed"

	gogui "github.com/go-gui-org/go-gui/gui"
)

const (
	SerifFamily = "PT Serif"
	LightFamily = "Lato Light"
)

var (
	//go:embed ptserif.ttf
	serifTTF []byte
	//go:embed latolight.ttf
	lightTTF []byte
)

func init() {
	gogui.RegisterAppFontBytes(serifTTF)
	gogui.RegisterAppFontBytes(lightTTF)
}
