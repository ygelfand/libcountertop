package thermostatview

import (
	"strings"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	kit "github.com/ygelfand/libcountertop/pkg/display/panelkit"
	"github.com/ygelfand/libcountertop/pkg/display/widgets"
)

func minimalLook(f *face) (gogui.View, skin) {
	pal := f.v.o.UI.Palette()
	sk := f.wear(skin{
		back:      widgets.Color(pal.Background),
		panel:     widgets.Color(pal.Surface),
		ink:       widgets.Color(pal.Text),
		muted:     widgets.Color(pal.Muted),
		accent:    widgets.Color(pal.Accent),
		accentInk: widgets.Color(pal.Contrast(pal.Accent)),
		heat:      widgets.Color(pal.Warning),
		cool:      widgets.Color(pal.Accent),
		radius:    f.u() * 0.04,
	})
	s := f.spots(plan{arrows: sides, head: 0.07, modes: 0.14})
	tone := f.tone(sk)
	side := min(s.number.w, s.number.h)
	ring := rect{s.number.x + (s.number.w-side)/2, s.number.y, side, side}
	views := []gogui.View{
		f.orbit("minimal-orbit", ring, tone),
		f.line(sk, s.head, f.name(), sk.muted, true),
		f.centered(sk, s.status, f.status(), tone, true),
		s.number.place(f.reading(sk, s.number, digits{ink: sk.ink, deg: tone})),
		f.centered(sk, s.facts, strings.Join(f.facts(), "   "), sk.muted, false),
		s.up.place(f.arrow(sk, "up", s.up, true, "", btn{fill: sk.panel, radius: s.up.w / 2})),
		s.down.place(
			f.arrow(sk, "down", s.down, false, "", btn{fill: sk.panel, radius: s.down.w / 2}),
		),
		s.modes.place(f.modes(s.modes, s.modes.h*0.2, func(c choice, r rect) btn {
			fill, ink := ink(sk.accent, c.current, sk.panel), ink(sk.accentInk, c.current, sk.ink)
			return btn{
				fill: fill, radius: r.h / 2, content: f.modeContent(sk, c, r, ink, true),
			}
		})),
	}
	return f.facade(sk, views...), sk
}

func editorialLook(f *face) (gogui.View, skin) {
	paper, black, orange := kit.Hex(0xf0eee9), kit.Hex(0x111111), kit.Hex(0xc8481e)
	blue := kit.Hex(0x2f5f9e)
	sk := f.wear(skin{
		back: paper, panel: paper, ink: black, muted: kit.Hex(0x55524c), accent: orange,
		accentInk: paper, heat: orange, cool: blue, edge: black, edgeWide: max(2, f.u()*0.005),
		text: kit.Type{}, digits: kit.Type{Thin: true},
	})
	u := f.u()
	m := u * 0.05
	tone := f.tone(sk)
	if tone == sk.muted {
		tone = black
	}
	head := rect{m, m, f.vw - 2*m - f.reserve, u * 0.06}
	rule := rect{m, head.y + head.h + u*0.02, f.vw - 2*m, max(1.5, u*0.004)}
	modes := rect{m, f.vh - m - min(u*0.13, f.vh*0.16), f.vw - 2*m, min(u*0.13, f.vh*0.16)}
	top := rule.y + rule.h + u*0.04
	bw := min(f.vw*0.24, (modes.y-top-u*0.05)*0.55)
	var up, down, num, status rect
	if f.tall() {
		bh := min(u*0.18, (modes.y-top)*0.22)
		up = rect{m, modes.y - u*0.04 - bh, (f.vw - 2*m - u*0.03) / 2, bh}
		down = rect{up.x + up.w + u*0.03, up.y, up.w, bh}
		status = rect{m, top, f.vw - 2*m, u * 0.07}
		num = rect{m, status.y + status.h, f.vw - 2*m, up.y - status.y - status.h - u*0.03}
	} else {
		colH := modes.y - top - u*0.05
		up = rect{f.vw - m - bw, top, bw, (colH - u*0.03) / 2}
		down = rect{up.x, up.y + up.h + u*0.03, bw, up.h}
		status = rect{m, top, up.x - 2*m, u * 0.07}
		num = rect{m - u*0.02, status.y + status.h, up.x - m, colH - status.h}
	}
	facts := strings.Join(f.facts(), "  ")
	views := []gogui.View{
		f.line(sk, rect{head.x, head.y, head.w * 0.45, head.h},
			f.name()+" · "+f.clock(), sk.muted, false),
		f.right(
			sk,
			rect{head.x + head.w*0.45, head.y, head.w * 0.55, head.h},
			facts,
			sk.muted,
			false,
		),
		rule.place(gogui.Row(gogui.ContainerCfg{
			Width: rule.w, Height: rule.h, Sizing: gogui.FixedFixed, Color: black,
		})),
		f.sweep("editorial-sweep", rect{rule.x, rule.y - rule.h, rule.w, rule.h * 3}, tone),
		f.line(sk, status, f.status(), tone, true),
		num.place(editorialNumber(f, sk, num, tone)),
		up.place(f.arrow(sk, "up", up, true, "mdi:plus", btn{
			fill: black, ink: paper, radius: u * 0.015,
		})),
		down.place(f.arrow(sk, "down", down, false, "mdi:minus", btn{
			fill: paper, ink: black, edge: black, edgeWide: sk.edgeWide, radius: u * 0.015,
		})),
		modes.place(f.modes(modes, 0, func(c choice, r rect) btn {
			fill, ink := ink(orange, c.current, paper), ink(paper, c.current, black)
			return btn{
				fill: fill, edge: black, edgeWide: max(1, sk.edgeWide*0.6),
				content: []gogui.View{sk.text.Fitted(f.w, c.label, r.w*0.8, r.h*0.34, ink, false)},
			}
		})),
	}
	return f.facade(sk, views...), sk
}

func editorialNumber(f *face, sk skin, r rect, tone gogui.Color) gogui.View {
	if f.st.Ranged() {
		return f.reading(sk, r, digits{ink: sk.ink, deg: tone})
	}
	text := f.number(f.setpoint())
	size := sk.digits.Fit(f.w, text+"o", r.w*0.95, r.h*1.05, false)
	b := f.breath(2600 * time.Millisecond)
	ring := size * (0.16 + 0.02*b)
	wide := f.w.TextWidth(text, sk.digits.Style(size, sk.ink, false))
	top := (r.h - size*1.15) / 2
	side := ring * 1.6
	return kit.Stack(r.w, r.h,
		kit.Place(0, top, r.w, size*1.2, sk.digits.Label(text, size, sk.ink, false)),
		kit.Place(wide+size*0.04+(size*0.29-side)/2, top+size*0.12+(size*0.29-side)/2, side, side,
			gogui.Row(gogui.ContainerCfg{
				Width: side, Height: side, Sizing: gogui.FixedFixed,
				ColorBorder: tone.WithOpacity(0.6 + 0.4*b), SizeBorder: gogui.BorderPx(ring * 0.42),
				Radius: gogui.RadiusPx(ring),
			})),
	)
}

func pillLook(f *face) (gogui.View, skin) {
	rust, light, white := kit.Hex(0xb34a22), kit.Hex(0xcb6a3c), kit.Hex(0xffffff)
	sk := f.wear(skin{
		back: rust, panel: light, ink: white, muted: white.WithOpacity(0.8), accent: white,
		accentInk: rust, heat: white, cool: white, text: kit.Type{}, digits: kit.Type{Thin: true},
	})
	u := f.u()
	m := u * 0.06
	var pill, right rect
	if f.tall() {
		pw := min(f.vw*0.36, u*0.4)
		pill = rect{(f.vw - pw) / 2, f.vh * 0.52, pw, f.vh*0.48 - m}
		top := m + f.reserve*0.3
		right = rect{m, top, f.vw - 2*m, pill.y - top - m}
	} else {
		pw := min(f.vw*0.24, (f.vh-2*m)*0.48)
		pill = rect{m + u*0.04, m, pw, f.vh - 2*m}
		right = rect{
			pill.x + pill.w + m*1.4,
			m + f.reserve*0.3,
			f.vw - pill.x - pill.w - m*2.4,
			f.vh - 2*m - f.reserve*0.3,
		}
	}
	half := pill.h / 2
	up := rect{pill.x, pill.y, pill.w, half}
	down := rect{pill.x, pill.y + half, pill.w, half}
	rh := right.h
	head := rect{right.x, right.y, right.w, rh * 0.09}
	status := rect{right.x, right.y + rh*0.18, right.w, rh * 0.09}
	num := rect{right.x - u*0.01, status.y + status.h, right.w, rh * 0.38}
	facts := rect{right.x, num.y + num.h + rh*0.02, right.w, rh * 0.075}
	modes := rect{right.x, facts.y + facts.h + rh*0.07, right.w, min(rh*0.2, right.w/4.6)}
	views := []gogui.View{
		f.halo(pill, pill.w/2, white),
		up.place(f.arrow(sk, "up", up, true, "mdi:plus", btn{
			fill: light, ink: white, radius: pill.w / 2,
		})),
		down.place(f.arrow(sk, "down", down, false, "mdi:minus", btn{
			fill: white, ink: rust, radius: pill.w / 2,
		})),
		f.line(sk, head, f.name()+" · "+f.clock(), white, false),
		f.line(sk, status, f.status(), white, true),
		num.place(f.readingLeft(sk, num, white)),
		f.line(sk, facts, strings.Join(f.facts(), " · "), white, false),
		modes.place(f.modes(modes, modes.h*0.25, func(c choice, r rect) btn {
			d := min(r.w, r.h)
			fill, ink := ink(white, c.current, gogui.RGBA(0, 0, 0, 0)), ink(rust, c.current, white)
			return btn{
				fill: fill, edge: white, edgeWide: max(2, d*0.03), radius: d / 2,
				content: []gogui.View{sk.text.Fitted(f.w, c.label, r.w*0.7, d*0.26, ink, false)},
			}
		})),
	}
	return f.facade(sk, views...), sk
}

func (f *face) readingLeft(sk skin, r rect, c gogui.Color) gogui.View {
	if f.st.Ranged() {
		return f.reading(sk, r, digits{ink: c})
	}
	text := f.number(f.setpoint()) + "°"
	size := sk.digits.Fit(f.w, text, r.w, r.h*1.1, false)
	return kit.Row(r.w, r.h, 0, gogui.HAlignLeft, sk.digits.Label(text, size, c, false))
}
