package vad

type Moment int

const (
	Began Moment = iota + 1
	Speaking
	Ended
)

type Event struct {
	Moment  Moment
	Samples []int16
}

const (
	speechAbove = 0.5
	quietBelow  = 0.35
	startRun    = 3
	quietRun    = 14
	preRoll     = 25
)

type Gate struct {
	d       *Detector
	pending []int16
	lead    [][]int16
	open    bool
	voiced  int
	quiet   int
}

func NewGate() (*Gate, error) {
	d, err := New()
	if err != nil {
		return nil, err
	}
	return &Gate{d: d, pending: make([]int16, 0, Chunk)}, nil
}

func (g *Gate) Open() bool { return g.open }

func (g *Gate) Reset() {
	g.d.Reset()
	g.pending = g.pending[:0]
	g.lead = g.lead[:0]
	g.open = false
	g.voiced, g.quiet = 0, 0
}

func (g *Gate) Feed(samples []int16) []Event {
	var out []Event
	for len(samples) > 0 {
		n := min(Chunk-len(g.pending), len(samples))
		g.pending = append(g.pending, samples[:n]...)
		samples = samples[n:]
		if len(g.pending) < Chunk {
			break
		}
		chunk := append([]int16(nil), g.pending...)
		g.pending = g.pending[:0]
		out = append(out, g.step(chunk)...)
	}
	return out
}

func (g *Gate) step(chunk []int16) []Event {
	p, err := g.d.Speech(chunk)
	if err != nil {
		return nil
	}
	if !g.open {
		g.lead = append(g.lead, chunk)
		if len(g.lead) > preRoll {
			g.lead = g.lead[1:]
		}
		if p >= speechAbove {
			g.voiced++
		} else {
			g.voiced = 0
		}
		if g.voiced < startRun {
			return nil
		}
		g.open, g.quiet = true, 0
		lead := make([]int16, 0, len(g.lead)*Chunk)
		for _, c := range g.lead {
			lead = append(lead, c...)
		}
		g.lead = g.lead[:0]
		return []Event{{Moment: Began}, {Moment: Speaking, Samples: lead}}
	}
	if p < quietBelow {
		g.quiet++
	} else if p >= speechAbove {
		g.quiet = 0
	}
	out := []Event{{Moment: Speaking, Samples: chunk}}
	if g.quiet >= quietRun {
		g.open, g.voiced, g.quiet = false, 0, 0
		out = append(out, Event{Moment: Ended})
	}
	return out
}
