package serviceb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"service-a/internal/domain"
	"service-a/internal/observability/requestid"
	"service-a/internal/usecase"
)

const (
	requestIDHeader = "X-Request-Id"
	maxBodyBytes    = 64 << 10
)

// Client calls service-b. Trace-context propagation is the job of the
// http.Client's transport (otelhttp, wired in main), not of this adapter.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient builds a Client. The http.Client carries the request timeout.
func NewClient(baseURL string, httpClient *http.Client) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: httpClient}
}

type weatherResponse struct {
	City  string  `json:"city"`
	TempC float64 `json:"temp_C"`
	TempF float64 `json:"temp_F"`
	TempK float64 `json:"temp_K"`
}

// GetWeather posts cep to service-b.
func (c *Client) GetWeather(ctx context.Context, cep domain.CEP) (usecase.Weather, error) {
	payload, err := json.Marshal(struct {
		CEP string `json:"cep"`
	}{CEP: cep.String()})
	if err != nil {
		return usecase.Weather{}, fmt.Errorf("%w: service-b: encode request: %w", domain.ErrUpstream, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/weather", bytes.NewReader(payload))
	if err != nil {
		return usecase.Weather{}, fmt.Errorf("%w: service-b: build request: %w", domain.ErrUpstream, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if id := requestid.FromContext(ctx); id != "" {
		req.Header.Set(requestIDHeader, id)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return usecase.Weather{}, fmt.Errorf("%w: service-b: request failed: %w", domain.ErrUpstream, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return usecase.Weather{}, domain.ErrZipcodeNotFound
	case http.StatusUnprocessableEntity:
		return usecase.Weather{}, domain.ErrInvalidZipcode
	default:
		return usecase.Weather{}, fmt.Errorf("%w: service-b: unexpected status %d", domain.ErrUpstream, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return usecase.Weather{}, fmt.Errorf("%w: service-b: read body: %w", domain.ErrUpstream, err)
	}
	if len(body) > maxBodyBytes {
		return usecase.Weather{}, fmt.Errorf("%w: service-b: response exceeds %d bytes", domain.ErrUpstream, maxBodyBytes)
	}

	var r weatherResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return usecase.Weather{}, fmt.Errorf("%w: service-b: decode body: %w", domain.ErrUpstream, err)
	}
	if r.City == "" {
		return usecase.Weather{}, fmt.Errorf("%w: service-b: response has no city", domain.ErrUpstream)
	}
	return usecase.Weather{City: r.City, TempC: r.TempC, TempF: r.TempF, TempK: r.TempK}, nil
}
