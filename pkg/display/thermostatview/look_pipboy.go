package thermostatview

import (
	"strings"
	"time"

	gogui "github.com/go-gui-org/go-gui/gui"

	kit "github.com/ygelfand/libcountertop/pkg/display/panelkit"
	"github.com/ygelfand/libcountertop/pkg/display/style"
	"github.com/ygelfand/libcountertop/pkg/display/visual"
	"github.com/ygelfand/libcountertop/pkg/say"
)

func pipGreen(f *face) (gogui.View, skin) { return pipLook(f, visual.PipBoy3) }

func pipAmber(f *face) (gogui.View, skin) { return pipLook(f, visual.PipBoyNV) }

func tinted(t [3]float64, k float64) gogui.Color {
	c := func(v float64) uint8 { return uint8(min(max(v*k*255, 0), 255)) }
	return gogui.RGB(c(t[0]), c(t[1]), c(t[2]))
}

type pipRow struct {
	label, value string
}

func pipLook(f *face, screen visual.Kind) (gogui.View, skin) {
	crt, _ := visual.CRTOf(screen)
	lit, dim, faint, back := tinted(crt.Tint, 1), tinted(crt.Tint, 0.5), tinted(crt.Tint, 0.22),
		tinted(crt.Tint, 0.05)
	mono := kit.Type{Family: style.MonoFamily, Upper: true}
	sk := f.wear(skin{
		back: back, panel: back, ink: lit, muted: dim, accent: lit, accentInk: back,
		heat: lit, cool: lit, edge: lit, edgeWide: max(1.5, f.u()*0.004),
		text: mono, digits: kit.Type{Family: style.MonoFamily},
	})
	u := f.u()
	m := u * 0.045
	line := max(1.5, u*0.004)

	tabH := u * 0.075
	tabs := rect{m, m, f.vw - 2*m - f.reserve, tabH}
	subs := rect{m, tabs.y + tabs.h + u*0.012, f.vw - 2*m, u * 0.06}
	foot := rect{m, f.vh - m - u*0.065, f.vw - 2*m, u * 0.065}
	body := rect{m, subs.y + subs.h + u*0.03, f.vw - 2*m, foot.y - subs.y - subs.h - u*0.06}

	rows := []pipRow{{say.T("thermostat.pip.target"), f.number(f.setpoint()) + "°"}}
	if f.st.Current != nil {
		rows = append(rows, pipRow{say.T("thermostat.pip.inside"), f.number(f.st.Current) + "°"})
	}
	if f.v.o.Outside != nil {
		if t, ok := f.v.o.Outside(); ok {
			rows = append(rows, pipRow{say.T("thermostat.pip.outside"), f.number(&t) + "°"})
		}
	}
	if h := f.humidity(); h != "" {
		rows = append(rows, pipRow{say.T("thermostat.humidity"), h})
	}
	if f.st.Fan != "" {
		rows = append(rows, pipRow{say.T("thermostat.fan"), strings.ToUpper(f.st.Fan)})
	}

	var list, detail rect
	if f.wide() {
		list = rect{body.x, body.y, body.w * 0.44, body.h}
		detail = rect{list.x + list.w + u*0.04, body.y, body.w - list.w - u*0.04, body.h}
	} else {
		detail = rect{body.x, body.y, body.w, body.h * 0.55}
		list = rect{body.x, detail.y + detail.h + u*0.03, body.w, body.h - detail.h - u*0.03}
	}
	rowH := min(u*0.075, list.h/float32(max(len(rows), 1)))
	if most := int(list.h / rowH); most < len(rows) {
		rows = rows[:max(most, 1)]
	}

	names := []string{"stat", "temp", "data", "map", "radio"}
	tabW := tabs.w / float32(len(names))
	chosenTab := 1
	cursor := f.breath(1600 * time.Millisecond)

	views := []gogui.View{
		f.canvas(
			"pip-frame",
			rect{0, 0, f.vw, f.vh},
			kit.Version(f.vw, f.vh, float32(crt.Tint[0]), float32(len(rows)), cursor),
			func(dc *gogui.DrawContext) {
				base := tabs.y + tabs.h
				x0 := tabs.x + tabW*float32(chosenTab) + tabW*0.08
				x1 := x0 + tabW*0.84
				rise := tabs.h * 0.55
				dc.Line(tabs.x, base, x0, base, lit, line)
				dc.Line(x0, base, x0, base-rise, lit, line)
				dc.Line(x0, base-rise, x0+tabW*0.06, base-rise, lit, line)
				dc.Line(x1, base, x1, base-rise, lit, line)
				dc.Line(x1-tabW*0.06, base-rise, x1, base-rise, lit, line)
				dc.Line(x1, base, tabs.x+tabs.w, base, lit, line)
				dc.Line(tabs.x, base, tabs.x, base+tabs.h*0.25, lit, line)
				dc.Line(tabs.x+tabs.w, base, tabs.x+tabs.w, base+tabs.h*0.25, lit, line)
				hl := rect{list.x, list.y, list.w, rowH}
				dc.FilledRect(hl.x, hl.y, hl.w, hl.h, lit)
				dc.FilledRect(
					hl.x-u*0.012,
					hl.y+rowH*0.3,
					u*0.006,
					rowH*0.4,
					lit.WithOpacity(0.4+0.6*cursor),
				)
				dc.Rect(detail.x, detail.y, detail.w, detail.h, faint, line)
				dc.Line(
					detail.x,
					detail.y+detail.h*0.18,
					detail.x+detail.w,
					detail.y+detail.h*0.18,
					faint,
					line,
				)
				dc.FilledRect(foot.x, foot.y, foot.w, foot.h, faint.WithOpacity(0.7))
			},
		),
	}
	for i, n := range names {
		r := rect{tabs.x + tabW*float32(i), tabs.y, tabW, tabs.h * 0.8}
		views = append(
			views,
			f.centered(sk, r, say.T("thermostat.pip."+n), ink(lit, i == chosenTab, dim), true),
		)
	}
	views = append(views, subs.place(pipModes(f, sk, subs, lit, dim)))
	for i, row := range rows {
		r := rect{list.x + u*0.02, list.y + rowH*float32(i), list.w - u*0.04, rowH}
		c := ink(back, i == 0, lit)
		views = append(
			views,
			f.line(sk, rect{r.x, r.y + rowH*0.15, r.w * 0.6, rowH * 0.7}, row.label, c, true),
			f.right(
				sk,
				rect{r.x + r.w*0.6, r.y + rowH*0.15, r.w * 0.4, rowH * 0.7},
				row.value,
				c,
				true,
			),
		)
	}
	head := rect{detail.x + u*0.02, detail.y, detail.w - u*0.04, detail.h * 0.18}
	views = append(views, f.line(sk, head.inset(head.h*0.18), f.name()+" · "+f.status(), lit, true))
	a := min(detail.h*0.28, u*0.13)
	numArea := rect{detail.x, detail.y + detail.h*0.2, detail.w - a - u*0.04, detail.h * 0.78}
	views = append(
		views,
		numArea.place(f.reading(sk, numArea.inset(u*0.01), digits{ink: lit, letter: true})),
	)
	up := rect{detail.x + detail.w - a - u*0.02, numArea.cy() - a*1.1, a, a}
	down := rect{up.x, numArea.cy() + a*0.1, a, a}
	key := func(id string, r rect, steps int, label string) gogui.View {
		run := f.step(steps)
		c := ink(lit, run != nil || f.still, dim)
		return r.place(f.button(btn{
			id: id, r: r, edge: c, edgeWide: line, run: run,
			content: []gogui.View{mono.Fitted(f.w, label, r.w*0.7, r.h*0.6, c, true)},
		}))
	}
	views = append(views, key("up", up, 1, "+"), key("down", down, -1, "-"))
	footParts := []string{}
	if f.st.Current != nil {
		footParts = append(footParts, say.T("thermostat.pip.inside")+" "+f.number(f.st.Current)+"°")
	}
	if h := f.humidity(); h != "" {
		footParts = append(footParts, say.T("thermostat.humidity")+" "+h)
	}
	footParts = append(footParts, f.clock())
	cell := foot.w / float32(len(footParts))
	for i, p := range footParts {
		r := rect{foot.x + cell*float32(i), foot.y, cell, foot.h}
		views = append(views, f.centered(sk, r.inset(foot.h*0.18), p, lit, true))
	}
	return f.facade(sk, views...), sk
}

func pipModes(f *face, sk skin, r rect, lit, dim gogui.Color) gogui.View {
	cs := f.choices()
	if len(cs) == 0 {
		return kit.Row(r.w, r.h, 0, gogui.HAlignLeft)
	}
	cw := r.w / float32(len(cs))
	keys := make([]gogui.View, 0, len(cs))
	for _, c := range cs {
		b := btn{
			content: []gogui.View{
				sk.text.Fitted(
					f.w,
					c.label,
					cw*0.86,
					r.h*0.62,
					ink(lit, c.current, dim.WithOpacity(0.75)),
					c.current,
				),
			},
		}
		b.id, b.r, b.run = "mode-"+string(c.mode), rect{w: cw, h: r.h}, c.run
		keys = append(keys, f.button(b))
	}
	return kit.Row(r.w, r.h, 0, gogui.HAlignLeft, keys...)
}
