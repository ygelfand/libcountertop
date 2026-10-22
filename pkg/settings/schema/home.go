package schema

import (
	"slices"

	"github.com/ygelfand/libcountertop/pkg/say"
)

type Home struct {
	Enabled bool                `json:"enabled"`
	Mode    HomeMode            `json:"mode"`
	Picks   map[string]HomePick `json:"picks"`
	Control map[string]bool     `json:"control"`
	Combine bool                `json:"combine"`
	Group   map[string]int      `json:"group"`
	Tessera TesseraGrid         `json:"tessera"`
	Return  bool                `json:"tessera_return"`
	Wide    bool                `json:"tessera_landscape"`
	Tall    bool                `json:"tessera_portrait"`
	Themed  bool                `json:"tessera_themed"`
	Turn    TesseraTurn         `json:"tessera_turn"`
	Bottom  TesseraBottom       `json:"tessera_bottom"`
	Follow  bool                `json:"tessera_follow"`
}

type TesseraBottom string

const (
	BottomDock TesseraBottom = "dock"
	BottomHome TesseraBottom = "home"
	BottomOff  TesseraBottom = "off"
)

func (b TesseraBottom) Label() string { return say.T("tessera.bottom." + string(b)) }

func TesseraBottoms() []TesseraBottom { return []TesseraBottom{BottomDock, BottomHome, BottomOff} }

type TesseraTurn string

const (
	TurnNone   TesseraTurn = "none"
	TurnSlide  TesseraTurn = "slide"
	TurnCover  TesseraTurn = "cover"
	TurnReveal TesseraTurn = "reveal"
	TurnDeck   TesseraTurn = "deck"
	TurnZoom   TesseraTurn = "zoom"
	TurnFade   TesseraTurn = "fade"
)

func (t TesseraTurn) Label() string { return say.T("tessera.turn." + string(t)) }

func TesseraTurns() []TesseraTurn {
	return []TesseraTurn{TurnNone, TurnSlide, TurnCover, TurnReveal, TurnDeck, TurnZoom, TurnFade}
}

const (
	HomeGroupDefault = 2
	HomeGroupMost    = 5
)

type HomeMode string

const (
	HomeNative   HomeMode = "native"
	HomeTessera  HomeMode = "tessera"
	HomeFirmware HomeMode = "firmware"
)

func (m HomeMode) Label() string {
	switch m {
	case HomeNative:
		return say.T("mode.native")
	case HomeTessera:
		return say.T("mode.tessera_native")
	case HomeFirmware:
		return say.T("mode.firmware")
	}
	return string(m)
}

func (m HomeMode) Tessera() bool { return m == HomeTessera || m == HomeFirmware }

func HomeModes() []HomeMode { return []HomeMode{HomeNative, HomeTessera, HomeFirmware} }

type SaverMode string

const (
	SaverNative  SaverMode = "native"
	SaverTessera SaverMode = "tessera"
)

func (m SaverMode) Label() string {
	switch m {
	case SaverNative:
		return say.T("mode.native")
	case SaverTessera:
		return say.T("mode.tessera")
	}
	return string(m)
}

func SaverModes() []SaverMode { return []SaverMode{SaverNative, SaverTessera} }

type TesseraGrid struct {
	Columns int `json:"columns"`
	Rows    int `json:"rows"`
}

const (
	TesseraMost  = 12
	TesseraCells = 64
)

type HomePick struct {
	All      bool     `json:"all"`
	Labels   []string `json:"labels"`
	Areas    []string `json:"areas"`
	Entities []string `json:"entities"`
}

func (p HomePick) Empty() bool {
	return !p.All && len(p.Labels) == 0 && len(p.Areas) == 0 && len(p.Entities) == 0
}

func (p HomePick) Clone() HomePick {
	p.Labels = slices.Clone(p.Labels)
	p.Areas = slices.Clone(p.Areas)
	p.Entities = slices.Clone(p.Entities)
	return p
}

func DefaultHome() Home {
	return Home{
		Enabled: true,
		Mode:    HomeNative,
		Wide:    true,
		Tall:    true,
		Themed:  true,
		Turn:    TurnSlide,
		Bottom:  BottomDock,
		Follow:  true,
	}
}

func (h Home) Picked(key string) HomePick { return h.Picks[key] }

func (h Home) Grouped(key string) int {
	if n, ok := h.Group[key]; ok {
		return n
	}
	return HomeGroupDefault
}

func (h Home) Speaks() HomeMode {
	if h.Mode.Tessera() {
		return h.Mode
	}
	return HomeNative
}

func (g TesseraGrid) Or(fallback TesseraGrid) TesseraGrid {
	if g.Columns <= 0 {
		g.Columns = fallback.Columns
	}
	if g.Rows <= 0 {
		g.Rows = fallback.Rows
	}
	g.Columns = min(max(g.Columns, 1), TesseraMost)
	g.Rows = min(max(g.Rows, 1), TesseraMost, TesseraCells/g.Columns)
	return g
}
