package api

import (
	"log/slog"
	"net/http"
)

func NewRouter(h *Handler, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /weather", h.PostWeather)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return RequestID(logger)(mux)
}
