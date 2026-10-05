package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"service-b/internal/domain"
	"service-b/internal/observability/requestid"
	"service-b/internal/usecase"
)

const maxBodyBytes = 1 << 10

type WeatherUseCase interface {
	Execute(ctx context.Context, cep domain.CEP) (usecase.Output, error)
}

type weatherRequest struct {
	CEP string `json:"cep"`
}

type weatherResponse struct {
	City  string  `json:"city"`
	TempC float64 `json:"temp_C"`
	TempF float64 `json:"temp_F"`
	TempK float64 `json:"temp_K"`
}

type errorResponse struct {
	Message string `json:"message"`
}

var errBodyTooLarge = errors.New("request body too large")

type Handler struct {
	uc     WeatherUseCase
	logger *slog.Logger
}

func NewHandler(uc WeatherUseCase, logger *slog.Logger) *Handler {
	return &Handler{uc: uc, logger: logger}
}

func (h *Handler) PostWeather(w http.ResponseWriter, r *http.Request) {
	cep, err := decodeCEP(w, r)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	out, err := h.uc.Execute(r.Context(), cep)
	if err != nil {
		h.writeError(w, r, err)
		return
	}

	h.writeJSON(w, r, http.StatusOK, weatherResponse{
		City:  out.City,
		TempC: out.Temperature.C,
		TempF: out.Temperature.F,
		TempK: out.Temperature.K,
	})
}

func decodeCEP(w http.ResponseWriter, r *http.Request) (domain.CEP, error) {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()

	var req weatherRequest
	if err := dec.Decode(&req); err != nil {
		return domain.CEP{}, classifyDecodeError(err)
	}
	// Reject trailing data such as a second JSON value.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return domain.CEP{}, classifyDecodeError(err)
	}
	return domain.NewCEP(req.CEP)
}

func classifyDecodeError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return errBodyTooLarge
	}
	return domain.ErrInvalidZipcode
}

func (h *Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errBodyTooLarge):
		h.writeJSON(w, r, http.StatusRequestEntityTooLarge, errorResponse{"request body too large"})
	case errors.Is(err, domain.ErrInvalidZipcode):
		h.writeJSON(w, r, http.StatusUnprocessableEntity, errorResponse{"invalid zipcode"})
	case errors.Is(err, domain.ErrZipcodeNotFound):
		h.writeJSON(w, r, http.StatusNotFound, errorResponse{"can not find zipcode"})
	case errors.Is(err, domain.ErrUpstream):
		h.logger.WarnContext(r.Context(), "upstream failure", "error", err, "request_id", requestid.FromContext(r.Context()))
		h.writeJSON(w, r, http.StatusBadGateway, errorResponse{"upstream service unavailable"})
	default:
		h.logger.ErrorContext(r.Context(), "unexpected error", "error", err, "request_id", requestid.FromContext(r.Context()))
		h.writeJSON(w, r, http.StatusInternalServerError, errorResponse{"internal server error"})
	}
}

func (h *Handler) writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.logger.ErrorContext(r.Context(), "write response", "error", err, "request_id", requestid.FromContext(r.Context()))
	}
}
