package domain

import (
	"math"
	"testing"
)

func TestFromCelsius(t *testing.T) {
	cases := []struct {
		name string
		in   float64
		want Temperature
	}{
		{"freezing point", 0, Temperature{C: 0, F: 32, K: 273}},
		{"boiling point", 100, Temperature{C: 100, F: 212, K: 373}},
		{"fahrenheit equals celsius", -40, Temperature{C: -40, F: -40, K: 233}},
		{"spec example", 28.5, Temperature{C: 28.5, F: 83.3, K: 301.5}},
		{"rounds every value to one decimal", 21.34, Temperature{C: 21.3, F: 70.4, K: 294.3}},
		{"negative with decimals", -5.5, Temperature{C: -5.5, F: 22.1, K: 267.5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FromCelsius(tc.in); got != tc.want {
				t.Errorf("FromCelsius(%v) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestFromCelsius_KelvinUsesIntegerOffset(t *testing.T) {
	got := FromCelsius(25)
	if got.K != 298 {
		t.Errorf("K = %v, want 298 (C + 273)", got.K)
	}
}

func TestFromCelsius_NoNegativeZero(t *testing.T) {
	got := FromCelsius(-0.04)
	if got.C != 0 || math.Signbit(got.C) {
		t.Errorf("C = %v (signbit=%v), want +0", got.C, math.Signbit(got.C))
	}
}
