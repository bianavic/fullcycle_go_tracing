package api

import (
	"log/slog"
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func NewRouter(h *Handler, logger *slog.Logger, otelOpts ...otelhttp.Option) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /weather", h.PostWeather)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	opts := []otelhttp.Option{
		otelhttp.WithFilter(func(r *http.Request) bool { return r.URL.Path != healthPath }),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			if _, pattern := mux.Handler(r); pattern != "" {
				return pattern
			}
			return r.Method + " unmatched"
		}),
	}
	opts = append(opts, otelOpts...)

	return otelhttp.NewHandler(RequestID(logger)(mux), "http.server", opts...)
}
