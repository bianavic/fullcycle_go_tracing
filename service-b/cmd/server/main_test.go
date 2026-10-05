package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
)

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return strings.TrimPrefix(l.Addr().String(), "127.0.0.1:")
}

// upstreams fakes ViaCEP, WeatherAPI and the OTLP collector with one server.
func upstreams(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/ws/01310100/json"):
			_, _ = w.Write([]byte(`{"localidade":"São Paulo"}`))
		case strings.HasPrefix(r.URL.Path, "/current"):
			_, _ = w.Write([]byte(`{"current":{"temp_c":28.5}}`))
		case r.URL.Path == "/v1/traces":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// isolateOTel keeps run()'s global provider/propagator out of other tests and
// points the exporter at the fake collector.
func isolateOTel(t *testing.T, collectorURL string) {
	t.Helper()
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", collectorURL)
}

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func waitReady(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:gosec,noctx // test helper polling a local server
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server at %s never became ready", url)
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestRun_ServesWeatherAndShutsDownGracefully(t *testing.T) {
	up := upstreams(t)
	isolateOTel(t, up.URL)
	port := freePort(t)
	env := envFrom(map[string]string{
		"PORT":                port,
		"WEATHER_API_KEY":     "test-key",
		"VIACEP_BASE_URL":     up.URL + "/ws",
		"WEATHERAPI_BASE_URL": up.URL + "/current",
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, quietLogger(), env) }()
	base := "http://127.0.0.1:" + port
	waitReady(t, base+"/healthz")

	resp, err := http.Post(base+"/weather", "application/json", strings.NewReader(`{"cep":"01310100"}`)) //nolint:gosec,noctx // local test server
	if err != nil {
		t.Fatalf("POST /weather: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK ||
		string(body) != `{"city":"São Paulo","temp_C":28.5,"temp_F":83.3,"temp_K":301.5}`+"\n" {
		t.Fatalf("response = %d %s, want the wired ViaCEP + WeatherAPI result", resp.StatusCode, body)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v on graceful shutdown, want nil", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return after the context was cancelled")
	}
	if c, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 500*time.Millisecond); err == nil {
		_ = c.Close()
		t.Error("the port is still accepting connections after shutdown")
	}
}

func TestRun_InvalidConfigFailsBeforeListening(t *testing.T) {
	// No WEATHER_API_KEY.
	err := run(context.Background(), quietLogger(), envFrom(map[string]string{"PORT": freePort(t)}))
	if err == nil || !strings.Contains(err.Error(), "WEATHER_API_KEY") {
		t.Fatalf("error = %v, want a config error naming WEATHER_API_KEY", err)
	}
}

func TestRun_PortInUseReturnsError(t *testing.T) {
	up := upstreams(t)
	isolateOTel(t, up.URL)
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	port := strings.TrimPrefix(l.Addr().String(), "[::]:")

	err = run(context.Background(), quietLogger(), envFrom(map[string]string{
		"PORT": port, "WEATHER_API_KEY": "k",
		"VIACEP_BASE_URL": up.URL, "WEATHERAPI_BASE_URL": up.URL,
	}))
	if err == nil {
		t.Fatal("expected a listen error when the port is taken")
	}
}
