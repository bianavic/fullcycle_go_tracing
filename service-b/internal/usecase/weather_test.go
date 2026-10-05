package usecase

import (
	"context"
	"errors"
	"testing"

	"bianavic/fullcycle_go_tracing/internal/domain"
)

type fakeLocation struct {
	city  string
	err   error
	calls int
	gotIn domain.CEP
}

func (f *fakeLocation) FindCityByCEP(_ context.Context, cep domain.CEP) (string, error) {
	f.calls++
	f.gotIn = cep
	return f.city, f.err
}

type fakeWeather struct {
	tempC float64
	err   error
	calls int
	gotIn string
}

func (f *fakeWeather) CurrentTempC(_ context.Context, city string) (float64, error) {
	f.calls++
	f.gotIn = city
	return f.tempC, f.err
}

func mustCEP(t *testing.T, s string) domain.CEP {
	t.Helper()
	c, err := domain.NewCEP(s)
	if err != nil {
		t.Fatalf("NewCEP(%q): %v", s, err)
	}
	return c
}

func TestExecute_Success(t *testing.T) {
	loc := &fakeLocation{city: "São Paulo"}
	w := &fakeWeather{tempC: 28.5}
	uc := NewGetWeatherByCEP(loc, w)

	got, err := uc.Execute(context.Background(), mustCEP(t, "01310100"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := Output{City: "São Paulo", Temperature: domain.Temperature{C: 28.5, F: 83.3, K: 301.5}}
	if got != want {
		t.Errorf("Execute() = %+v, want %+v", got, want)
	}
	if loc.gotIn.String() != "01310100" {
		t.Errorf("location received CEP %q, want 01310100", loc.gotIn)
	}
	if w.gotIn != "São Paulo" {
		t.Errorf("weather received city %q, want São Paulo", w.gotIn)
	}
}

func TestExecute_ZeroCEPIsInvalidAndSkipsProviders(t *testing.T) {
	loc, w := &fakeLocation{}, &fakeWeather{}
	uc := NewGetWeatherByCEP(loc, w)

	_, err := uc.Execute(context.Background(), domain.CEP{})
	if !errors.Is(err, domain.ErrInvalidZipcode) {
		t.Fatalf("error = %v, want ErrInvalidZipcode", err)
	}
	if loc.calls != 0 || w.calls != 0 {
		t.Errorf("providers called (location=%d weather=%d), want none", loc.calls, w.calls)
	}
}

func TestExecute_LocationErrorsPropagateAndSkipWeather(t *testing.T) {
	boom := errors.New("boom")
	cases := []struct {
		name string
		err  error
	}{
		{"not found", domain.ErrZipcodeNotFound},
		{"upstream", domain.ErrUpstream},
		{"unexpected", boom},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			loc, w := &fakeLocation{err: tc.err}, &fakeWeather{}
			uc := NewGetWeatherByCEP(loc, w)

			_, err := uc.Execute(context.Background(), mustCEP(t, "01310100"))
			if !errors.Is(err, tc.err) {
				t.Fatalf("error = %v, want it to wrap %v", err, tc.err)
			}
			if w.calls != 0 {
				t.Error("weather provider must not be called when the location lookup fails")
			}
		})
	}
}

func TestExecute_WeatherErrorPropagates(t *testing.T) {
	loc := &fakeLocation{city: "Vitória"}
	w := &fakeWeather{err: domain.ErrUpstream}
	uc := NewGetWeatherByCEP(loc, w)

	_, err := uc.Execute(context.Background(), mustCEP(t, "29902555"))
	if !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("error = %v, want ErrUpstream", err)
	}
}
