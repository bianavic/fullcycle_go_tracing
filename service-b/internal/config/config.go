// Package config loads and validates service-b settings from the environment.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"service-b/internal/infra/viacep"
	"service-b/internal/infra/weatherapi"
)

// Config holds every runtime setting of service-b. OpenTelemetry settings are
// read by the OTel SDK itself from the standard OTEL_* variables.
type Config struct {
	Port              string
	WeatherAPIKey     string
	ViaCEPBaseURL     string
	WeatherAPIBaseURL string
	HTTPClientTimeout time.Duration
}

func Load(getenv func(string) string) (Config, error) {
	get := func(key, def string) string {
		if v := getenv(key); v != "" {
			return v
		}
		return def
	}

	cfg := Config{
		Port:              get("PORT", "8081"),
		WeatherAPIKey:     getenv("WEATHER_API_KEY"),
		ViaCEPBaseURL:     get("VIACEP_BASE_URL", viacep.DefaultBaseURL),
		WeatherAPIBaseURL: get("WEATHERAPI_BASE_URL", weatherapi.DefaultBaseURL),
	}

	var errs []error
	if cfg.WeatherAPIKey == "" {
		errs = append(errs, errors.New("WEATHER_API_KEY is required"))
	}
	if p, err := strconv.Atoi(cfg.Port); err != nil || p < 1 || p > 65535 {
		errs = append(errs, errors.New("PORT must be an integer between 1 and 65535"))
	}
	if err := validateHTTPURL(cfg.ViaCEPBaseURL); err != nil {
		errs = append(errs, fmt.Errorf("VIACEP_BASE_URL: %w", err))
	}
	if err := validateHTTPURL(cfg.WeatherAPIBaseURL); err != nil {
		errs = append(errs, fmt.Errorf("WEATHERAPI_BASE_URL: %w", err))
	}

	timeout, err := time.ParseDuration(get("HTTP_CLIENT_TIMEOUT", "5s"))
	if err != nil || timeout <= 0 {
		errs = append(errs, errors.New("HTTP_CLIENT_TIMEOUT must be a positive duration such as 5s"))
	}
	cfg.HTTPClientTimeout = timeout

	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) String() string {
	return fmt.Sprintf("port=%s viacep=%s weatherapi=%s timeout=%s weather_api_key=[redacted]",
		c.Port, c.ViaCEPBaseURL, c.WeatherAPIBaseURL, c.HTTPClientTimeout)
}

func validateHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("must be an absolute http(s) URL")
	}
	return nil
}
