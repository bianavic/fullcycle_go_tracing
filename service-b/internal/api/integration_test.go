package api_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"service-b/internal/api"
	"service-b/internal/infra/viacep"
	"service-b/internal/infra/weatherapi"
	"service-b/internal/usecase"
)

// These tests wire the real router, handler, use case and HTTP adapters
// together and fake only the two external APIs.

const (
	apiKey     = "integration-secret-key"
	traceID    = "4bf92f3577b34da6a3ce929d0e0e4736"
	parentSpan = "00f067aa0ba902b7"
)

type upstream struct {
	handler http.HandlerFunc
	calls   atomic.Int32
	lastKey atomic.Value // string
}

func (u *upstream) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.calls.Add(1)
		u.lastKey.Store(r.URL.Query().Get("key"))
		u.handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func reply(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

type stack struct {
	handler http.Handler
	spans   *tracetest.SpanRecorder
	viacep  *upstream
	weather *upstream
}

func newStack(t *testing.T, viaCEP, weather http.HandlerFunc, clientTimeout time.Duration) *stack {
	t.Helper()
	s := &stack{viacep: &upstream{handler: viaCEP}, weather: &upstream{handler: weather}}
	viaSrv, weatherSrv := s.viacep.server(t), s.weather.server(t)

	s.spans = tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(s.spans))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	prop := propagation.TraceContext{}

	viaHTTP := &http.Client{
		Timeout:   clientTimeout,
		Transport: otelhttp.NewTransport(http.DefaultTransport, otelhttp.WithTracerProvider(tp), otelhttp.WithPropagators(prop)),
	}
	weatherHTTP := &http.Client{Timeout: clientTimeout}
	tracer := tp.Tracer("integration")

	uc := usecase.NewGetWeatherByCEP(
		viacep.NewClient(viaSrv.URL, viaHTTP, tracer),
		weatherapi.NewClient(weatherSrv.URL, apiKey, weatherHTTP, tracer),
	)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s.handler = api.NewRouter(api.NewHandler(uc, logger), logger,
		otelhttp.WithTracerProvider(tp), otelhttp.WithPropagators(prop))
	return s
}

func (s *stack) post(body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/weather", strings.NewReader(body))
	req.Header.Set("traceparent", "00-"+traceID+"-"+parentSpan+"-01")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	return rec
}

func (s *stack) span(t *testing.T, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, sp := range s.spans.Ended() {
		if sp.Name() == name {
			return sp
		}
	}
	t.Fatalf("no span named %q; have %v", name, s.spanNames())
	return nil
}

func (s *stack) spanNames() []string {
	var names []string
	for _, sp := range s.spans.Ended() {
		names = append(names, sp.Name())
	}
	return names
}

func okViaCEP() http.HandlerFunc  { return reply(200, `{"localidade":"São Paulo"}`) }
func okWeather() http.HandlerFunc { return reply(200, `{"current":{"temp_c":28.5}}`) }

func TestIntegration_Success(t *testing.T) {
	s := newStack(t, okViaCEP(), okWeather(), time.Second)

	rec := s.post(`{"cep":"01310100"}`)

	if rec.Code != 200 || rec.Body.String() != `{"city":"São Paulo","temp_C":28.5,"temp_F":83.3,"temp_K":301.5}`+"\n" {
		t.Fatalf("response = %d %q", rec.Code, rec.Body)
	}
	if got := s.weather.lastKey.Load(); got != apiKey {
		t.Errorf("WeatherAPI received key %v, want the configured key", got)
	}
}

func TestIntegration_SpanTree(t *testing.T) {
	s := newStack(t, okViaCEP(), okWeather(), time.Second)
	s.post(`{"cep":"01310100"}`)

	server := s.span(t, "POST /weather")
	lookupCEP := s.span(t, "lookup-cep")
	lookupTemp := s.span(t, "lookup-temperature")
	viaHTTP := s.span(t, "HTTP GET")

	if got := server.Parent().SpanID().String(); got != parentSpan {
		t.Errorf("server span parent = %s, want the incoming span %s", got, parentSpan)
	}
	for name, sp := range map[string]sdktrace.ReadOnlySpan{
		"server": server, "lookup-cep": lookupCEP, "lookup-temperature": lookupTemp, "viacep http": viaHTTP,
	} {
		if got := sp.SpanContext().TraceID().String(); got != traceID {
			t.Errorf("%s span is in trace %s, want %s", name, got, traceID)
		}
	}
	wantParent := map[string]sdktrace.ReadOnlySpan{
		"lookup-cep → server":         lookupCEP,
		"lookup-temperature → server": lookupTemp,
	}
	for name, child := range wantParent {
		if child.Parent().SpanID() != server.SpanContext().SpanID() {
			t.Errorf("%s: parent mismatch", name)
		}
	}
	if viaHTTP.Parent().SpanID() != lookupCEP.SpanContext().SpanID() {
		t.Error("the ViaCEP client span must hang under lookup-cep")
	}
	if n := len(s.spans.Ended()); n != 4 {
		t.Errorf("got %d spans %v, want exactly 4 (WeatherAPI is not auto-instrumented)", n, s.spanNames())
	}
}

func TestIntegration_APIKeyNeverReachesTelemetryOrClients(t *testing.T) {
	scenarios := map[string]*stack{
		"success":      newStack(t, okViaCEP(), okWeather(), time.Second),
		"weather 500":  newStack(t, okViaCEP(), reply(500, "key="+apiKey), time.Second),
		"weather 401":  newStack(t, okViaCEP(), reply(401, `{"error":{"message":"invalid key `+apiKey+`"}}`), time.Second),
		"weather slow": newStack(t, okViaCEP(), func(http.ResponseWriter, *http.Request) { time.Sleep(300 * time.Millisecond) }, 40*time.Millisecond),
	}
	for name, s := range scenarios {
		t.Run(name, func(t *testing.T) {
			rec := s.post(`{"cep":"01310100"}`)

			var dump strings.Builder
			dump.WriteString(rec.Body.String())
			for _, sp := range s.spans.Ended() {
				fmt.Fprint(&dump, sp.Name(), sp.Attributes(), sp.Events(), sp.Status(), sp.Links())
			}
			if strings.Contains(dump.String(), apiKey) {
				t.Errorf("the API key leaked into the response or the spans:\n%s", dump.String())
			}
		})
	}
}

func TestIntegration_FailureModes(t *testing.T) {
	cases := []struct {
		name        string
		viaCEP      http.HandlerFunc
		weather     http.HandlerFunc
		timeout     time.Duration
		wantStatus  int
		wantBody    string
		wantWeather int32
		failedSpan  string // span expected to carry an Error status ("" = none)
	}{
		{"cep not found", reply(200, `{"erro":"true"}`), okWeather(), time.Second, 404, `{"message":"can not find zipcode"}`, 0, ""},
		{"viacep 404", reply(404, ``), okWeather(), time.Second, 404, `{"message":"can not find zipcode"}`, 0, ""},
		{"viacep 500", reply(500, `oops`), okWeather(), time.Second, 502, `{"message":"upstream service unavailable"}`, 0, "lookup-cep"},
		{"viacep garbage", reply(200, `<html>`), okWeather(), time.Second, 502, `{"message":"upstream service unavailable"}`, 0, "lookup-cep"},
		{"viacep timeout", func(http.ResponseWriter, *http.Request) { time.Sleep(300 * time.Millisecond) }, okWeather(), 40 * time.Millisecond, 502, `{"message":"upstream service unavailable"}`, 0, "lookup-cep"},
		{"weather 500", okViaCEP(), reply(500, `oops`), time.Second, 502, `{"message":"upstream service unavailable"}`, 1, "lookup-temperature"},
		{"weather unknown city", okViaCEP(), reply(400, `{"error":{"code":1006,"message":"No matching location found."}}`), time.Second, 502, `{"message":"upstream service unavailable"}`, 1, "lookup-temperature"},
		{"weather missing field", okViaCEP(), reply(200, `{"current":{}}`), time.Second, 502, `{"message":"upstream service unavailable"}`, 1, "lookup-temperature"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newStack(t, tc.viaCEP, tc.weather, tc.timeout)

			rec := s.post(`{"cep":"01310100"}`)

			if rec.Code != tc.wantStatus || strings.TrimSpace(rec.Body.String()) != tc.wantBody {
				t.Errorf("response = %d %s, want %d %s", rec.Code, strings.TrimSpace(rec.Body.String()), tc.wantStatus, tc.wantBody)
			}
			if got := s.weather.calls.Load(); got != tc.wantWeather {
				t.Errorf("WeatherAPI called %d times, want %d", got, tc.wantWeather)
			}
			for _, sp := range s.spans.Ended() {
				isErr := sp.Status().Code == codes.Error
				if sp.Name() == tc.failedSpan && !isErr {
					t.Errorf("span %q should be marked as an error", sp.Name())
				}
				if sp.Name() != tc.failedSpan && (sp.Name() == "lookup-cep" || sp.Name() == "lookup-temperature") && isErr {
					t.Errorf("span %q should not be marked as an error", sp.Name())
				}
			}
		})
	}
}

func TestIntegration_InvalidInputNeverReachesUpstreams(t *testing.T) {
	for _, body := range []string{`{"cep":"123"}`, `{"cep":"29902-555"}`, `{"cep":29902555}`, `{}`, `nope`} {
		s := newStack(t, okViaCEP(), okWeather(), time.Second)

		rec := s.post(body)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d, want 422", body, rec.Code)
		}
		if s.viacep.calls.Load() != 0 || s.weather.calls.Load() != 0 {
			t.Errorf("%s: an upstream was called for invalid input", body)
		}
	}
}
