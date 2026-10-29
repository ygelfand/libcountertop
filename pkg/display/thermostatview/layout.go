package thermostatview

type arrows int

const (
	sides arrows = iota
	stacked
	above
)

type spots struct {
	head, status, number, facts, modes, up, down, body rect
}

type plan struct {
	arrows arrows
	head   float32
	modes  float32
	left   float32
	right  float32
	margin float32
	arrow  float32
}

func (f *face) modeBar(width, height float32) rect {
	u := f.u()
	m := u * 0.05
	limit := float32(0.15)
	switch {
	case !f.wide():
		width, height, limit = max(width, 0.92), max(height, 0.17), 0.12
	case u < 800:
		width, height, limit = max(width, 0.84), max(height*1.35, 0.15), 0.18
	}
	h := min(u*height, f.vh*limit)
	w := f.vw * width
	return rect{(f.vw - w) / 2, f.vh - m - h, w, h}
}

func (f *face) spots(p plan) spots {
	u := f.u()
	m := u * 0.05
	if p.margin > 0 {
		m = u * p.margin
	}
	var s spots
	top := m
	if p.head > 0 {
		hh := u * p.head
		s.head = rect{m, m, max(f.vw-2*m-f.reserve, 1), hh}
		top = m + hh + m*0.5
	} else if f.reserve > 0 {
		top = m + u*0.06
	}
	bottom := f.vh - m
	if p.modes > 0 {
		mh := min(u*p.modes, f.vh*0.2)
		s.modes = rect{m + f.vw*p.left, f.vh - m - mh, f.vw - 2*m - f.vw*(p.left+p.right), mh}
		bottom = s.modes.y - m*0.6
	}
	body := rect{m + f.vw*p.left, top, f.vw - 2*m - f.vw*(p.left+p.right), max(bottom-top, 1)}
	s.body = body
	a := min(body.h*0.3, u*0.19)
	if p.arrow > 0 {
		a = min(body.h*0.42, u*p.arrow)
	}
	arrowsAt := p.arrows
	if arrowsAt == stacked && f.tall() {
		arrowsAt = sides
	}
	mid := body
	switch arrowsAt {
	case sides:
		s.up = rect{body.x, body.cy() - a/2, a, a}
		s.down = rect{body.x + body.w - a, body.cy() - a/2, a, a}
		mid = rect{body.x + a + m*0.4, body.y, body.w - 2*a - m*0.8, body.h}
	case stacked:
		g := a * 0.25
		s.up = rect{body.x + body.w - a, body.cy() - a - g/2, a, a}
		s.down = rect{body.x + body.w - a, body.cy() + g/2, a, a}
		mid = rect{body.x, body.y, body.w - a - m, body.h}
	case above:
		ah := min(a*0.8, body.h*0.2)
		s.up = rect{body.cx() - ah*0.6, body.y, ah * 1.2, ah}
		s.down = rect{body.cx() - ah*0.6, body.y + body.h - ah, ah * 1.2, ah}
		mid = rect{body.x, body.y + ah, body.w, body.h - 2*ah}
	}
	sh := mid.h * 0.12
	s.status = rect{mid.x, mid.y, mid.w, sh}
	s.facts = rect{mid.x, mid.y + mid.h - sh, mid.w, sh}
	s.number = rect{mid.x, mid.y + sh, mid.w, mid.h - 2*sh}
	return s
}
