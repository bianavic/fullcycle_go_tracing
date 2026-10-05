package usecase

import (
	"context"
	"fmt"

	"bianavic/fullcycle_go_tracing/internal/domain"
)

type Weather struct {
	City  string
	TempC float64
	TempF float64
	TempK float64
}

// WeatherGateway abstracts service-b.
type WeatherGateway interface {
	GetWeather(ctx context.Context, cep domain.CEP) (Weather, error)
}

// RequestWeather forwards an already validated CEP to service-b.
type RequestWeather struct {
	gateway WeatherGateway
}

func NewRequestWeather(gateway WeatherGateway) *RequestWeather {
	return &RequestWeather{gateway: gateway}
}

func (u *RequestWeather) Execute(ctx context.Context, cep domain.CEP) (Weather, error) {
	if cep == (domain.CEP{}) {
		return Weather{}, domain.ErrInvalidZipcode
	}
	w, err := u.gateway.GetWeather(ctx, cep)
	if err != nil {
		return Weather{}, fmt.Errorf("get weather from service-b: %w", err)
	}
	return w, nil
}
