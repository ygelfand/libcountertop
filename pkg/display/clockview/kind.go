package clockview

import "github.com/ygelfand/libcountertop/pkg/settings/schema"

type Face struct {
	Name      schema.Face
	Draw      View
	Ticks     bool
	Countdown schema.Face
}

var Faces = []Face{
	{Name: "plain", Draw: Plain},
	{Name: "stack", Draw: Stack},
	{Name: "cards", Draw: Cards},
	{Name: "flip", Draw: Flip},
	{Name: "analog", Draw: Analog(false), Countdown: "analog-seconds"},
	{Name: "analog-seconds", Draw: Analog(true), Ticks: true},
	{Name: "roman", Draw: Roman, Countdown: "analog-seconds"},
	{Name: "railway", Draw: Railway, Ticks: true},
	{Name: "octagon", Draw: Octagon, Ticks: true},
	{Name: "atomic", Draw: Atomic, Ticks: true},
	{Name: "spoken", Draw: Spoken},
	{Name: "math", Draw: Math, Ticks: true},
	{Name: "smiley", Draw: Smiley, Ticks: true},
	{Name: "neon", Draw: Neon},
	{Name: "bars", Draw: Bars},
	{Name: "reel", Draw: Reel},
	{Name: "binary", Draw: Binary, Ticks: true},
	{Name: "segments", Draw: Segments},
	{Name: "nixie", Draw: Nixie},
	{Name: "words", Draw: Words, Countdown: schema.DefaultFace},
	{Name: "overlap", Draw: Overlap},
}

func Of(name schema.Face) (Face, bool) {
	for _, f := range Faces {
		if f.Name == name {
			return f, true
		}
	}
	return Face{}, false
}

func Names() []schema.Face {
	names := make([]schema.Face, len(Faces))
	for i, f := range Faces {
		names[i] = f.Name
	}
	return names
}

func Ticks(name schema.Face) bool {
	f, _ := Of(name)
	return f.Ticks
}
