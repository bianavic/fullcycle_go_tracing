package api

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const (
	requestIDHeader = "X-Request-Id"
	healthPath      = "/healthz"
)


func RequestID(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := newRequestID()
			w.Header().Set(requestIDHeader, id)

			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()

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
			if sc := r.Context(); sc != nil {
				attrs = append(attrs, "trace_id", sc)
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

// statusWriter records the status code so the middleware can log it.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (sw *statusWriter) Unwrap() http.ResponseWriter { return sw.ResponseWriter }
