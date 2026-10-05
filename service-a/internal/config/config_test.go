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
	cfg, err := Load(env(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want 8080", cfg.Port)
	}
	if cfg.ServiceBURL != "http://service-b:8081" {
		t.Errorf("ServiceBURL = %q", cfg.ServiceBURL)
	}
	// Must outlast service-b's two sequential upstream calls (5s each by default).
	if cfg.HTTPClientTimeout != 15*time.Second {
		t.Errorf("HTTPClientTimeout = %v, want 15s", cfg.HTTPClientTimeout)
	}
}

func TestLoad_Overrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"PORT":                "9090",
		"SERVICE_B_URL":       "https://service-b.example.com",
		"HTTP_CLIENT_TIMEOUT": "750ms",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != "9090" || cfg.ServiceBURL != "https://service-b.example.com" || cfg.HTTPClientTimeout != 750*time.Millisecond {
		t.Errorf("overrides not applied: %+v", cfg)
	}
}

func TestLoad_Invalid(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"port not numeric", map[string]string{"PORT": "abc"}, "PORT"},
		{"port out of range", map[string]string{"PORT": "70000"}, "PORT"},
		{"port zero", map[string]string{"PORT": "0"}, "PORT"},
		{"service b url without scheme", map[string]string{"SERVICE_B_URL": "service-b:8081"}, "SERVICE_B_URL"},
		{"service b url wrong scheme", map[string]string{"SERVICE_B_URL": "ftp://x"}, "SERVICE_B_URL"},
		{"service b url empty host", map[string]string{"SERVICE_B_URL": "http://"}, "SERVICE_B_URL"},
		{"bad timeout", map[string]string{"HTTP_CLIENT_TIMEOUT": "soon"}, "HTTP_CLIENT_TIMEOUT"},
		{"non-positive timeout", map[string]string{"HTTP_CLIENT_TIMEOUT": "0s"}, "HTTP_CLIENT_TIMEOUT"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(env(tc.env))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want it to mention %s", err, tc.wantErr)
			}
		})
	}
}

func TestLoad_ReportsAllProblemsAtOnce(t *testing.T) {
	_, err := Load(env(map[string]string{"PORT": "abc", "SERVICE_B_URL": "x", "HTTP_CLIENT_TIMEOUT": "x"}))
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"PORT", "SERVICE_B_URL", "HTTP_CLIENT_TIMEOUT"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %s", err, want)
		}
	}
}
