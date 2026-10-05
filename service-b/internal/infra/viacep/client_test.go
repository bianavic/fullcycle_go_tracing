package viacep

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"

	"service-b/internal/domain"
)

func newClient(t *testing.T, h http.HandlerFunc) (*Client, *tracetest.SpanRecorder) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	return NewClient(srv.URL, srv.Client(), tp.Tracer("test")), rec
}

func mustCEP(t *testing.T, s string) domain.CEP {
	t.Helper()
	c, err := domain.NewCEP(s)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func respond(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func TestFindCityByCEP_Success(t *testing.T) {
	var gotPath string
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"cep":"01310-100","localidade":"São Paulo","logradouro":"Avenida Paulista"}`))
	})

	city, err := c.FindCityByCEP(context.Background(), mustCEP(t, "01310100"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if city != "São Paulo" {
		t.Errorf("city = %q, want São Paulo", city)
	}
	if gotPath != "/01310100/json/" {
		t.Errorf("path = %q, want /01310100/json/", gotPath)
	}
}

func TestFindCityByCEP_ErrorMapping(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    error
	}{
		{"erro as string", respond(200, `{"erro":"true"}`), domain.ErrZipcodeNotFound},
		{"erro as bool", respond(200, `{"erro":true}`), domain.ErrZipcodeNotFound},
		{"empty city", respond(200, `{"localidade":"","logradouro":"Av"}`), domain.ErrZipcodeNotFound},
		{"http 404", respond(404, ``), domain.ErrZipcodeNotFound},
		{"http 400", respond(400, `<html>Bad Request</html>`), domain.ErrInvalidZipcode},
		{"http 500", respond(500, `oops`), domain.ErrUpstream},
		{"http 429", respond(429, `slow down`), domain.ErrUpstream},
		{"malformed json", respond(200, `{not json}`), domain.ErrUpstream},
		{"truncated json", respond(200, `{"localidade":"São`), domain.ErrUpstream},
		{"empty body", respond(200, ``), domain.ErrUpstream},
		{"oversized body", respond(200, `{"localidade":"`+strings.Repeat("a", maxBodyBytes)+`"}`), domain.ErrUpstream},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newClient(t, tc.handler)
			_, err := c.FindCityByCEP(context.Background(), mustCEP(t, "01310100"))
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestFindCityByCEP_ServerDownIsUpstream(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	c := NewClient(url, &http.Client{Timeout: time.Second}, noop.NewTracerProvider().Tracer("test"))
	_, err := c.FindCityByCEP(context.Background(), mustCEP(t, "01310100"))
	if !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("error = %v, want ErrUpstream", err)
	}
}

func TestFindCityByCEP_TimeoutIsUpstream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, &http.Client{Timeout: 50 * time.Millisecond}, noop.NewTracerProvider().Tracer("test"))
	_, err := c.FindCityByCEP(context.Background(), mustCEP(t, "01310100"))
	if !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("error = %v, want ErrUpstream", err)
	}
}

func TestFindCityByCEP_ContextCancelled(t *testing.T) {
	c, _ := newClient(t, respond(200, `{"localidade":"X"}`))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.FindCityByCEP(ctx, mustCEP(t, "01310100"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestFindCityByCEP_Span(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		c, rec := newClient(t, respond(200, `{"localidade":"São Paulo"}`))
		_, _ = c.FindCityByCEP(context.Background(), mustCEP(t, "01310100"))

		span := onlySpan(t, rec)
		if span.Name() != "lookup-cep" {
			t.Errorf("span name = %q, want lookup-cep", span.Name())
		}
		wantAttr(t, span.Attributes(), "cep", "01310100")
		wantAttr(t, span.Attributes(), "city", "São Paulo")
		if span.Status().Code == codes.Error {
			t.Error("successful lookup must not set error status")
		}
	})

	t.Run("not found is not a span error", func(t *testing.T) {
		c, rec := newClient(t, respond(200, `{"erro":"true"}`))
		_, _ = c.FindCityByCEP(context.Background(), mustCEP(t, "99999999"))

		span := onlySpan(t, rec)
		if span.Status().Code == codes.Error {
			t.Error("a business not-found must not mark the span as error")
		}
		if len(span.Events()) != 0 {
			t.Errorf("unexpected span events: %v", span.Events())
		}
	})

	t.Run("upstream failure marks the span", func(t *testing.T) {
		c, rec := newClient(t, respond(500, `oops`))
		_, _ = c.FindCityByCEP(context.Background(), mustCEP(t, "01310100"))

		span := onlySpan(t, rec)
		if span.Status().Code != codes.Error {
			t.Errorf("status = %v, want Error", span.Status().Code)
		}
		if len(span.Events()) == 0 {
			t.Error("expected the error to be recorded on the span")
		}
	})

	t.Run("span is a child of the caller", func(t *testing.T) {
		c, rec := newClient(t, respond(200, `{"localidade":"X"}`))
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
		defer func() { _ = tp.Shutdown(context.Background()) }()
		ctx, parent := tp.Tracer("test").Start(context.Background(), "parent")

		_, _ = c.FindCityByCEP(ctx, mustCEP(t, "01310100"))
		parent.End()

		var child sdktrace.ReadOnlySpan
		for _, s := range rec.Ended() {
			if s.Name() == "lookup-cep" {
				child = s
			}
		}
		if child == nil || child.Parent().SpanID() != parent.SpanContext().SpanID() {
			t.Fatal("lookup-cep must be a child of the span in the caller's context")
		}
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

func wantAttr(t *testing.T, attrs []attribute.KeyValue, key, want string) {
	t.Helper()
	for _, kv := range attrs {
		if string(kv.Key) == key {
			if kv.Value.AsString() != want {
				t.Errorf("attribute %s = %q, want %q", key, kv.Value.AsString(), want)
			}
			return
		}
	}
	t.Errorf("attribute %s missing", key)
}
