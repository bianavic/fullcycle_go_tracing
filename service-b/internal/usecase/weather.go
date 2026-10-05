package usecase

import (
	"context"
	"fmt"

	"bianavic/fullcycle_go_tracing/internal/domain"
)

type LocationProvider interface {
	FindCityByCEP(ctx context.Context, cep domain.CEP) (string, error)
}

type WeatherProvider interface {
	CurrentTempC(ctx context.Context, city string) (float64, error)
}

type Output struct {
	City        string
	Temperature domain.Temperature
}

type GetWeatherByCEP struct {
	location LocationProvider
	weather  WeatherProvider
}

func NewGetWeatherByCEP(location LocationProvider, weather WeatherProvider) *GetWeatherByCEP {
	return &GetWeatherByCEP{location: location, weather: weather}
}

func (u *GetWeatherByCEP) Execute(ctx context.Context, cep domain.CEP) (Output, error) {
	if cep == (domain.CEP{}) {
		return Output{}, domain.ErrInvalidZipcode
	}

	city, err := u.location.FindCityByCEP(ctx, cep)
	if err != nil {
		return Output{}, fmt.Errorf("find city by cep: %w", err)
	}

	tempC, err := u.weather.CurrentTempC(ctx, city)
	if err != nil {
		return Output{}, fmt.Errorf("get current temperature: %w", err)
	}

	return Output{City: city, Temperature: domain.FromCelsius(tempC)}, nil
}
