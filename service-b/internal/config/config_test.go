package config

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load(env(map[string]string{"WEATHER_API_KEY": "k"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != "8081" {
		t.Errorf("Port = %q, want 8081", cfg.Port)
	}
	if cfg.ViaCEPBaseURL != "https://viacep.com.br/ws" {
		t.Errorf("ViaCEPBaseURL = %q", cfg.ViaCEPBaseURL)
	}
	if cfg.WeatherAPIBaseURL != "https://api.weatherapi.com/v1/current.json" {
		t.Errorf("WeatherAPIBaseURL = %q", cfg.WeatherAPIBaseURL)
	}
	if cfg.HTTPClientTimeout != 5*time.Second {
		t.Errorf("HTTPClientTimeout = %v, want 5s", cfg.HTTPClientTimeout)
	}
	if cfg.WeatherAPIKey != "k" {
		t.Errorf("WeatherAPIKey not loaded")
	}
}

func TestLoad_Overrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"WEATHER_API_KEY":     "k",
		"PORT":                "9090",
		"VIACEP_BASE_URL":     "http://localhost:1234/ws",
		"WEATHERAPI_BASE_URL": "http://localhost:5678/current",
		"HTTP_CLIENT_TIMEOUT": "750ms",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != "9090" || cfg.ViaCEPBaseURL != "http://localhost:1234/ws" ||
		cfg.WeatherAPIBaseURL != "http://localhost:5678/current" || cfg.HTTPClientTimeout != 750*time.Millisecond {
		t.Errorf("overrides not applied: %+v", cfg)
	}
}

func TestLoad_Invalid(t *testing.T) {
	const secret = "TOP-SECRET-KEY"
	cases := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"missing api key", map[string]string{}, "WEATHER_API_KEY"},
		{"port not numeric", map[string]string{"WEATHER_API_KEY": secret, "PORT": "abc"}, "PORT"},
		{"port out of range", map[string]string{"WEATHER_API_KEY": secret, "PORT": "70000"}, "PORT"},
		{"port zero", map[string]string{"WEATHER_API_KEY": secret, "PORT": "0"}, "PORT"},
		{"bad timeout", map[string]string{"WEATHER_API_KEY": secret, "HTTP_CLIENT_TIMEOUT": "soon"}, "HTTP_CLIENT_TIMEOUT"},
		{"non-positive timeout", map[string]string{"WEATHER_API_KEY": secret, "HTTP_CLIENT_TIMEOUT": "0s"}, "HTTP_CLIENT_TIMEOUT"},
		{"viacep url without scheme", map[string]string{"WEATHER_API_KEY": secret, "VIACEP_BASE_URL": "viacep.com.br"}, "VIACEP_BASE_URL"},
		{"viacep url wrong scheme", map[string]string{"WEATHER_API_KEY": secret, "VIACEP_BASE_URL": "ftp://x"}, "VIACEP_BASE_URL"},
		{"weatherapi url invalid", map[string]string{"WEATHER_API_KEY": secret, "WEATHERAPI_BASE_URL": "http://"}, "WEATHERAPI_BASE_URL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(env(tc.env))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q should mention %s", err, tc.wantErr)
			}
			if strings.Contains(err.Error(), secret) {
				t.Errorf("error leaks the API key: %q", err)
			}
		})
	}
}

func TestLoad_ReportsAllProblemsAtOnce(t *testing.T) {
	_, err := Load(env(map[string]string{"PORT": "abc", "HTTP_CLIENT_TIMEOUT": "x"}))
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"WEATHER_API_KEY", "PORT", "HTTP_CLIENT_TIMEOUT"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %s", err, want)
		}
	}
}

func TestConfig_StringRedactsKey(t *testing.T) {
	cfg := Config{Port: "1", WeatherAPIKey: "TOP-SECRET-KEY"}
	if strings.Contains(cfg.String(), "TOP-SECRET-KEY") {
		t.Errorf("String() leaks the key: %s", cfg.String())
	}
}
