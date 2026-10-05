package domain

import "math"

type Temperature struct {
	C float64
	F float64
	K float64
}

func FromCelsius(c float64) Temperature {
	return Temperature{
		C: round1(c),
		F: round1(c*1.8 + 32),
		K: round1(c + 273),
	}
}

func round1(v float64) float64 {
	r := math.Round(v*10) / 10
	if r == 0 {
		return 0
	}
	return r
}
