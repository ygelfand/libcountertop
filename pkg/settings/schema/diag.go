package schema

type Diag struct {
	Interval int `json:"interval"`
	MinCores int `json:"min_cores,omitempty"`

	LowMemory  bool `json:"low_memory,omitempty"`
	PageMemory int  `json:"page_memory,omitempty"`
}

const DefaultDiagInterval = 300

var PageMemorySteps = []int{128, 192, 256, 384, 512, 0}

func DefaultDiag() Diag { return Diag{Interval: DefaultDiagInterval} }
