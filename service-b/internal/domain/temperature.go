package domain

import "math"

type Temperature struct {
	C float64
	F float64
	K float64
}

// FromCelsius converts c using the challenge formulas
// F = C × 1.8 + 32 and K = C + 273, rounding each value to one decimal place.
func FromCelsius(c float64) Temperature {
	return Temperature{
		C: round1(c),
		F: round1(c*1.8 + 32),
		K: round1(c + 273),
	}
}

// round1 rounds to one decimal place and normalizes -0 to +0 so that the
// JSON output never contains "-0".
func round1(v float64) float64 {
	r := math.Round(v*10) / 10
	if r == 0 {
		return 0
	}
	return r
}
