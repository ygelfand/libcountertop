package thermostat

func Samples(n int, unit string) []State {
	celsius := unit == "°C"
	t := func(f float64) *float64 {
		if celsius {
			c := float64(int((f-32)/1.8*2+0.5)) / 2
			return &c
		}
		return &f
	}
	limits := Limits{
		Modes:    []Mode{Heat, Cool, HeatCool, Off},
		FanModes: []string{"auto", "low", "high", "on"},
		Presets:  []string{"none", "away", "eco", "sleep"},
		Min:      *t(45),
		Max:      *t(95),
		Step:     1,
		features: featureTarget | featureRange,
	}
	if celsius {
		limits.Step = 0.5
	}
	humid := func(h float64) *float64 { return &h }
	all := []State{
		{
			Entity:   "climate.rehearsal_1",
			Name:     "Living room",
			Mode:     Heat,
			Action:   Heating,
			Fan:      "auto",
			Preset:   "none",
			Current:  t(70),
			Target:   t(72),
			Humidity: humid(42),
			Limits:   limits,
		},
		{
			Entity:   "climate.rehearsal_2",
			Name:     "Bedroom",
			Mode:     Cool,
			Action:   Idle,
			Fan:      "auto",
			Preset:   "eco",
			Current:  t(71),
			Target:   t(68),
			Humidity: humid(48),
			Limits:   limits,
		},
		{
			Entity:   "climate.rehearsal_3",
			Name:     "Office",
			Mode:     HeatCool,
			Action:   Cooling,
			Fan:      "low",
			Preset:   "none",
			Current:  t(75),
			Low:      t(68),
			High:     t(74),
			Humidity: humid(45),
			Limits:   limits,
		},
		{
			Entity: "climate.rehearsal_4", Name: "Basement", Mode: Off, Action: Stopped,
			Current: t(64), Target: t(65), Humidity: humid(55), Limits: limits,
		},
	}
	return all[:min(max(n, 1), len(all))]
}
