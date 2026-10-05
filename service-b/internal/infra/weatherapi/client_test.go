package weatherapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"

	"service-b/internal/domain"
)

const testKey = "SECRET-KEY-123"

func newClient(t *testing.T, h http.HandlerFunc) (*Client, *tracetest.SpanRecorder) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	return NewClient(srv.URL, testKey, srv.Client(), tp.Tracer("test")), rec
}

func respond(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func TestCurrentTempC_Success(t *testing.T) {
	var gotQuery map[string]string
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = map[string]string{"key": r.URL.Query().Get("key"), "q": r.URL.Query().Get("q")}
		_, _ = w.Write([]byte(`{"location":{"name":"São Paulo"},"current":{"temp_c":28.5}}`))
	})

	got, err := c.CurrentTempC(context.Background(), "São Paulo & Co=1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 28.5 {
		t.Errorf("temp = %v, want 28.5", got)
	}
	if gotQuery["key"] != testKey {
		t.Errorf("key param = %q, want the configured key", gotQuery["key"])
	}
	if gotQuery["q"] != "São Paulo & Co=1" {
		t.Errorf("q param = %q, want city to be query-escaped and round-trip intact", gotQuery["q"])
	}
}

func TestCurrentTempC_ZeroAndNegativeAreValid(t *testing.T) {
	for _, body := range []string{`{"current":{"temp_c":0}}`, `{"current":{"temp_c":-12.3}}`} {
		c, _ := newClient(t, respond(200, body))
		if _, err := c.CurrentTempC(context.Background(), "Oslo"); err != nil {
			t.Errorf("body %s: unexpected error %v", body, err)
		}
	}
}

func TestCurrentTempC_Errors(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"api error with message", respond(400, `{"error":{"code":1006,"message":"No matching location found."}}`)},
		{"401 invalid key", respond(401, `{"error":{"code":2006,"message":"API key is invalid."}}`)},
		{"500 without body", respond(500, ``)},
		{"429", respond(429, `{"error":{"code":2007,"message":"quota"}}`)},
		{"malformed json", respond(200, `{not json}`)},
		{"missing current.temp_c", respond(200, `{"current":{}}`)},
		{"missing current", respond(200, `{}`)},
		{"oversized body", respond(200, `{"current":{"temp_c":1},"x":"`+strings.Repeat("a", maxBodyBytes)+`"}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newClient(t, tc.handler)
			_, err := c.CurrentTempC(context.Background(), "Oslo")
			if !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("error = %v, want ErrUpstream", err)
			}
			assertNoSecret(t, err.Error())
		})
	}
}

// The API key travels in the query string, and net/http embeds the full URL in
// transport errors. None of that may reach the returned error.
func TestCurrentTempC_NetworkErrorsDoNotLeakKey(t *testing.T) {
	t.Run("connection refused", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		url := srv.URL
		srv.Close()

		c := NewClient(url, testKey, &http.Client{Timeout: time.Second}, noop.NewTracerProvider().Tracer("t"))
		_, err := c.CurrentTempC(context.Background(), "Oslo")
		if !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("error = %v, want ErrUpstream", err)
		}
		assertNoSecret(t, err.Error())
	})

	t.Run("timeout", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
		}))
		defer srv.Close()

		c := NewClient(srv.URL, testKey, &http.Client{Timeout: 50 * time.Millisecond}, noop.NewTracerProvider().Tracer("t"))
		_, err := c.CurrentTempC(context.Background(), "Oslo")
		if !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("error = %v, want ErrUpstream", err)
		}
		assertNoSecret(t, err.Error())
	})

	t.Run("cancelled context", func(t *testing.T) {
		c, _ := newClient(t, respond(200, `{"current":{"temp_c":1}}`))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := c.CurrentTempC(ctx, "Oslo")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
		assertNoSecret(t, err.Error())
	})
}

func TestCurrentTempC_InvalidBaseURLDoesNotLeakKey(t *testing.T) {
	c := NewClient("http://bad host/\x7f", testKey, http.DefaultClient, noop.NewTracerProvider().Tracer("t"))
	_, err := c.CurrentTempC(context.Background(), "Oslo")
	if !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("error = %v, want ErrUpstream", err)
	}
	assertNoSecret(t, err.Error())
}

func TestCurrentTempC_Span(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		c, rec := newClient(t, respond(200, `{"current":{"temp_c":28.5}}`))
		_, _ = c.CurrentTempC(context.Background(), "São Paulo")

		span := onlySpan(t, rec)
		if span.Name() != "lookup-temperature" {
			t.Errorf("span name = %q, want lookup-temperature", span.Name())
		}
		var gotCity string
		var gotTemp float64
		for _, kv := range span.Attributes() {
			switch kv.Key {
			case "city":
				gotCity = kv.Value.AsString()
			case "temp_c":
				gotTemp = kv.Value.AsFloat64()
			}
		}
		if gotCity != "São Paulo" || gotTemp != 28.5 {
			t.Errorf("attributes city=%q temp_c=%v, want São Paulo / 28.5", gotCity, gotTemp)
		}
		if span.Status().Code == codes.Error {
			t.Error("successful lookup must not set error status")
		}
	})

	t.Run("failure marks the span and never records the key", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		url := srv.URL
		srv.Close()
		rec := tracetest.NewSpanRecorder()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
		defer func() { _ = tp.Shutdown(context.Background()) }()

		c := NewClient(url, testKey, &http.Client{Timeout: time.Second}, tp.Tracer("t"))
		_, _ = c.CurrentTempC(context.Background(), "Oslo")

		span := onlySpan(t, rec)
		if span.Status().Code != codes.Error {
			t.Errorf("status = %v, want Error", span.Status().Code)
		}
		assertNoSecret(t, fmt.Sprint(span.Attributes(), span.Events(), span.Status()))
	})
}

func onlySpan(t *testing.T, rec *tracetest.SpanRecorder) sdktrace.ReadOnlySpan {
	t.Helper()
	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}
	return spans[0]
}

func assertNoSecret(t *testing.T, s string) {
	t.Helper()
	if strings.Contains(s, testKey) || strings.Contains(strings.ToLower(s), "key=") {
		t.Errorf("secret leaked in %q", s)
	}
}
