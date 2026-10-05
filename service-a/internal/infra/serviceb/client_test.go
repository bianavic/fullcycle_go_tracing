package serviceb

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"service-a/internal/domain"
	"service-a/internal/observability/requestid"
	"service-a/internal/usecase"
)

const okBody = `{"city":"São Paulo","temp_C":28.5,"temp_F":83.3,"temp_K":301.5}`

func mustCEP(t *testing.T, s string) domain.CEP {
	t.Helper()
	c, err := domain.NewCEP(s)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func newClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, srv.Client())
}

func respond(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func TestGetWeather_SendsPostJSON(t *testing.T) {
	var method, path, ctype string
	var payload map[string]any
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path, ctype = r.Method, r.URL.Path, r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&payload)
		_, _ = w.Write([]byte(okBody))
	})

	got, err := c.GetWeather(context.Background(), mustCEP(t, "01310100"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := usecase.Weather{City: "São Paulo", TempC: 28.5, TempF: 83.3, TempK: 301.5}
	if got != want {
		t.Errorf("GetWeather() = %+v, want %+v", got, want)
	}
	if method != http.MethodPost || path != "/weather" {
		t.Errorf("request = %s %s, want POST /weather", method, path)
	}
	if !strings.HasPrefix(ctype, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ctype)
	}
	if len(payload) != 1 || payload["cep"] != "01310100" {
		t.Errorf("payload = %v, want {cep: 01310100}", payload)
	}
}

func TestGetWeather_BaseURLTrailingSlash(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(okBody))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL+"/", srv.Client()).GetWeather(context.Background(), mustCEP(t, "01310100"))
	if err != nil || path != "/weather" {
		t.Errorf("err = %v, path = %q, want /weather", err, path)
	}
}

func TestGetWeather_ForwardsRequestID(t *testing.T) {
	var got string
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Request-Id")
		_, _ = w.Write([]byte(okBody))
	})

	ctx := requestid.With(context.Background(), "req-123")
	if _, err := c.GetWeather(ctx, mustCEP(t, "01310100")); err != nil {
		t.Fatal(err)
	}
	if got != "req-123" {
		t.Errorf("X-Request-Id = %q, want req-123", got)
	}
}

func TestGetWeather_OmitsRequestIDWhenAbsent(t *testing.T) {
	var present bool
	c := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, present = r.Header["X-Request-Id"]
		_, _ = w.Write([]byte(okBody))
	})
	if _, err := c.GetWeather(context.Background(), mustCEP(t, "01310100")); err != nil {
		t.Fatal(err)
	}
	if present {
		t.Error("X-Request-Id must not be sent when the context has none")
	}
}

func TestGetWeather_ErrorMapping(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    error
	}{
		{"404", respond(404, `{"message":"can not find zipcode"}`), domain.ErrZipcodeNotFound},
		{"422", respond(422, `{"message":"invalid zipcode"}`), domain.ErrInvalidZipcode},
		{"500", respond(500, `boom`), domain.ErrUpstream},
		{"502", respond(502, `{"message":"upstream service unavailable"}`), domain.ErrUpstream},
		{"503", respond(503, ``), domain.ErrUpstream},
		{"413", respond(413, `{"message":"request body too large"}`), domain.ErrUpstream},
		{"405", respond(405, ``), domain.ErrUpstream},
		{"200 malformed json", respond(200, `{not json}`), domain.ErrUpstream},
		{"200 empty body", respond(200, ``), domain.ErrUpstream},
		{"200 without city", respond(200, `{"temp_C":1,"temp_F":2,"temp_K":3}`), domain.ErrUpstream},
		{"200 oversized", respond(200, `{"city":"`+strings.Repeat("a", maxBodyBytes)+`"}`), domain.ErrUpstream},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newClient(t, tc.handler).GetWeather(context.Background(), mustCEP(t, "01310100"))
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestGetWeather_ServerDownIsUpstream(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	_, err := NewClient(url, &http.Client{Timeout: time.Second}).GetWeather(context.Background(), mustCEP(t, "01310100"))
	if !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("error = %v, want ErrUpstream", err)
	}
}

func TestGetWeather_TimeoutIsUpstream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, &http.Client{Timeout: 50 * time.Millisecond}).GetWeather(context.Background(), mustCEP(t, "01310100"))
	if !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("error = %v, want ErrUpstream", err)
	}
}

func TestGetWeather_ContextCancelled(t *testing.T) {
	c := newClient(t, respond(200, okBody))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.GetWeather(ctx, mustCEP(t, "01310100"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestGetWeather_InvalidBaseURLIsUpstream(t *testing.T) {
	_, err := NewClient("http://bad host/\x7f", http.DefaultClient).GetWeather(context.Background(), mustCEP(t, "01310100"))
	if !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("error = %v, want ErrUpstream", err)
	}
}

func TestGetWeather_PropagatesTraceContext(t *testing.T) {
	var traceparent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceparent = r.Header.Get("traceparent")
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(okBody))
	}))
	defer srv.Close()

	tp := sdktrace.NewTracerProvider()
	defer func() { _ = tp.Shutdown(context.Background()) }()
	httpClient := &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport,
		otelhttp.WithTracerProvider(tp),
		otelhttp.WithPropagators(propagation.TraceContext{}),
	)}
	ctx, span := tp.Tracer("test").Start(context.Background(), "root")
	defer span.End()

	if _, err := NewClient(srv.URL, httpClient).GetWeather(ctx, mustCEP(t, "01310100")); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(traceparent, span.SpanContext().TraceID().String()) {
		t.Errorf("traceparent = %q, want it to carry trace id %s", traceparent, span.SpanContext().TraceID())
	}
}
