package schema

type Diag struct {
	Interval int `json:"interval"`
	MinCores int `json:"min_cores,omitempty"`
}

const DefaultDiagInterval = 300

func DefaultDiag() Diag { return Diag{Interval: DefaultDiagInterval} }
