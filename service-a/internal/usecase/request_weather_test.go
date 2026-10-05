package usecase

import (
	"bianavic/fullcycle_go_tracing/internal/domain"
	"context"
	"errors"
	"testing"
)

type fakeGateway struct {
	out   Weather
	err   error
	calls int
	gotIn domain.CEP
}

func (f *fakeGateway) GetWeather(_ context.Context, cep domain.CEP) (Weather, error) {
	f.calls++
	f.gotIn = cep
	return f.out, f.err
}

func mustCEP(t *testing.T, s string) domain.CEP {
	t.Helper()
	c, err := domain.NewCEP(s)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestExecute_ForwardsCEPAndReturnsWeather(t *testing.T) {
	want := Weather{City: "São Paulo", TempC: 28.5, TempF: 83.3, TempK: 301.5}
	gw := &fakeGateway{out: want}

	got, err := NewRequestWeather(gw).Execute(context.Background(), mustCEP(t, "01310100"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("Execute() = %+v, want %+v", got, want)
	}
	if gw.gotIn.String() != "01310100" {
		t.Errorf("gateway received %q, want 01310100", gw.gotIn)
	}
}

func TestExecute_ZeroCEPIsInvalidAndSkipsGateway(t *testing.T) {
	gw := &fakeGateway{}

	_, err := NewRequestWeather(gw).Execute(context.Background(), domain.CEP{})

	if !errors.Is(err, domain.ErrInvalidZipcode) {
		t.Fatalf("error = %v, want ErrInvalidZipcode", err)
	}
	if gw.calls != 0 {
		t.Error("gateway must not be called")
	}
}

func TestExecute_GatewayErrorsPropagate(t *testing.T) {
	for _, target := range []error{domain.ErrInvalidZipcode, domain.ErrZipcodeNotFound, domain.ErrUpstream} {
		t.Run(target.Error(), func(t *testing.T) {
			gw := &fakeGateway{err: target}

			_, err := NewRequestWeather(gw).Execute(context.Background(), mustCEP(t, "01310100"))

			if !errors.Is(err, target) {
				t.Fatalf("error = %v, want it to wrap %v", err, target)
			}
		})
	}
}
