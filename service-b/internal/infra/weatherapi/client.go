package weatherapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"service-b/internal/domain"
)

const (
	DefaultBaseURL = "https://api.weatherapi.com/v1/current.json"

	spanName     = "lookup-temperature"
	maxBodyBytes = 64 << 10
)

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
	tracer  trace.Tracer
}

func NewClient(baseURL, apiKey string, httpClient *http.Client, tracer trace.Tracer) *Client {
	return &Client{baseURL: baseURL, apiKey: apiKey, http: httpClient, tracer: tracer}
}

type response struct {
	Current struct {
		TempC *float64 `json:"temp_c"`
	} `json:"current"`
}

func (c *Client) CurrentTempC(ctx context.Context, city string) (float64, error) {
	ctx, span := c.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()
	span.SetAttributes(attribute.String("city", city))

	temp, err := c.fetch(ctx, span, city)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "weather lookup failed")
		return 0, err
	}
	span.SetAttributes(attribute.Float64("temp_c", temp))
	return temp, nil
}

func (c *Client) fetch(ctx context.Context, span trace.Span, city string) (float64, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return 0, fmt.Errorf("%w: weatherapi: invalid base url", domain.ErrUpstream)
	}
	q := u.Query()
	q.Set("key", c.apiKey)
	q.Set("q", city)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, fmt.Errorf("%w: weatherapi: build request", domain.ErrUpstream)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%w: weatherapi: request failed: %w", domain.ErrUpstream, withoutURL(err))
	}
	defer func() { _ = resp.Body.Close() }()
	span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("%w: weatherapi: unexpected status %d", domain.ErrUpstream, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return 0, fmt.Errorf("%w: weatherapi: read body: %w", domain.ErrUpstream, withoutURL(err))
	}
	if len(body) > maxBodyBytes {
		return 0, fmt.Errorf("%w: weatherapi: response exceeds %d bytes", domain.ErrUpstream, maxBodyBytes)
	}

	var r response
	if err := json.Unmarshal(body, &r); err != nil {
		return 0, fmt.Errorf("%w: weatherapi: decode body: %w", domain.ErrUpstream, err)
	}
	if r.Current.TempC == nil {
		return 0, fmt.Errorf("%w: weatherapi: response has no current.temp_c", domain.ErrUpstream)
	}
	return *r.Current.TempC, nil
}

func withoutURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
