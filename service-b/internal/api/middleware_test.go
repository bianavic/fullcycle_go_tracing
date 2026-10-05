package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"service-b/internal/observability/requestid"
)

func logRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		out = append(out, m)
	}
	return out
}

func runMiddleware(t *testing.T, req *http.Request, next http.HandlerFunc) (*httptest.ResponseRecorder, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	rec := httptest.NewRecorder()
	RequestID(logger)(next).ServeHTTP(rec, req)
	return rec, &buf
}

func TestRequestID_GeneratesWhenAbsent(t *testing.T) {
	var inCtx string
	rec, _ := runMiddleware(t, httptest.NewRequest(http.MethodGet, "/x", nil), func(_ http.ResponseWriter, r *http.Request) {
		inCtx = requestid.FromContext(r.Context())
	})

	got := rec.Header().Get("X-Request-Id")
	if got == "" || got != inCtx {
		t.Errorf("header = %q, context = %q; want equal and non-empty", got, inCtx)
	}
	if len(got) != 36 || got[14] != '4' {
		t.Errorf("generated id %q is not a UUIDv4", got)
	}
}

func TestRequestID_ReusesValidCallerID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Request-Id", "client-supplied-correlation-id")

	rec, _ := runMiddleware(t, req, func(http.ResponseWriter, *http.Request) {})

	if got := rec.Header().Get("X-Request-Id"); got != "client-supplied-correlation-id" {
		t.Errorf("X-Request-Id = %q, want the caller's id", got)
	}
}

func TestRequestID_ReplacesUnsafeCallerID(t *testing.T) {
	cases := map[string]string{
		"newline injection": "abc\nINJECTED",
		"spaces":            "has space",
		"too long":          strings.Repeat("a", 65),
		"html":              "<script>",
	}
	for name, id := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			req.Header["X-Request-Id"] = []string{id}

			rec, _ := runMiddleware(t, req, func(http.ResponseWriter, *http.Request) {})

			if got := rec.Header().Get("X-Request-Id"); got == id || len(got) != 36 {
				t.Errorf("X-Request-Id = %q, want a freshly generated UUID", got)
			}
		})
	}
}

func TestRequestID_LogsOneStructuredLine(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/weather", nil)
	_, buf := runMiddleware(t, req, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })

	recs := logRecords(t, buf)
	if len(recs) != 1 {
		t.Fatalf("got %d log lines, want 1", len(recs))
	}
	r := recs[0]
	if r["method"] != "POST" || r["path"] != "/weather" || r["status"] != float64(404) {
		t.Errorf("unexpected record: %v", r)
	}
	if id, _ := r["request_id"].(string); id == "" {
		t.Error("log line must carry request_id")
	}
	if _, ok := r["trace_id"]; ok {
		t.Error("trace_id must be absent when there is no active span")
	}
}

func TestRequestID_LogsTraceIDFromContext(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	defer func() { _ = tp.Shutdown(context.Background()) }()
	ctx, span := tp.Tracer("t").Start(context.Background(), "server")
	defer span.End()

	req := httptest.NewRequest(http.MethodGet, "/x", nil).WithContext(ctx)
	_, buf := runMiddleware(t, req, func(http.ResponseWriter, *http.Request) {})

	if got := logRecords(t, buf)[0]["trace_id"]; got != span.SpanContext().TraceID().String() {
		t.Errorf("trace_id = %v, want %s", got, span.SpanContext().TraceID())
	}
}

func TestRequestID_DefaultStatusIs200(t *testing.T) {
	_, buf := runMiddleware(t, httptest.NewRequest(http.MethodGet, "/x", nil), func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	if got := logRecords(t, buf)[0]["status"]; got != float64(200) {
		t.Errorf("status = %v, want 200", got)
	}
}

func TestRequestID_DoesNotLogHealthChecks(t *testing.T) {
	_, buf := runMiddleware(t, httptest.NewRequest(http.MethodGet, "/healthz", nil), func(http.ResponseWriter, *http.Request) {})
	if buf.Len() != 0 {
		t.Errorf("health checks must not be logged, got %q", buf.String())
	}
}

func TestRequestID_StatusWriterUnwraps(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	var flushErr error
	rec, _ := runMiddleware(t, req, func(w http.ResponseWriter, _ *http.Request) {
		flushErr = http.NewResponseController(w).Flush()
	})

	if flushErr != nil || !rec.Flushed {
		t.Errorf("Flush through the middleware: err=%v flushed=%v, want nil/true", flushErr, rec.Flushed)
	}
}
