package api

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"go.opentelemetry.io/otel/trace"

	"service-b/internal/observability/requestid"
)

const (
	requestIDHeader = "X-Request-Id"
	healthPath      = "/healthz"
)

var safeRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// RequestID assigns an ID to every request, exposes it as a response header and
// logs one structured line per request. service-b is internal, so it reuses the
// X-Request-Id propagated by service-a, but only when it is a short token of
// safe characters; anything else is replaced by a fresh UUID. Health checks
// are not logged.
func RequestID(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get(requestIDHeader)
			if !safeRequestID.MatchString(id) {
				id = newRequestID()
			}
			w.Header().Set(requestIDHeader, id)

			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()

			next.ServeHTTP(sw, r.WithContext(requestid.With(r.Context(), id)))

			if r.URL.Path == healthPath {
				return
			}
			attrs := []any{
				"request_id", id,
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
			}
			if sc := trace.SpanContextFromContext(r.Context()); sc.HasTraceID() {
				attrs = append(attrs, "trace_id", sc.TraceID().String())
			}
			logger.InfoContext(r.Context(), "request", attrs...)
		})
	}
}

// newRequestID returns an RFC 4122 v4 UUID without an external dependency.
func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

func (sw *statusWriter) Unwrap() http.ResponseWriter { return sw.ResponseWriter }
