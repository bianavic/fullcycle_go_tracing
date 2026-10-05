package weatherapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"bianavic/fullcycle_go_tracing/internal/domain"
)

const (
	DefaultBaseURL = "https://api.weatherapi.com/v1/current.json"

	spanName     = "lookup-temperature"
	maxBodyBytes = 64 << 10
)

// Client queries WeatherAPI for the current temperature of a city.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func NewClient(baseURL, apiKey string, httpClient *http.Client) *Client {
	return &Client{baseURL: baseURL, apiKey: apiKey, http: httpClient}
}

type response struct {
	Current struct {
		TempC *float64 `json:"temp_c"`
	} `json:"current"`
}

func (c *Client) CurrentTempC(ctx context.Context, city string) (float64, error) {

	temp, err := c.fetch(ctx, city)
	if err != nil {
		return 0, err
	}
	return temp, nil
}

func (c *Client) fetch(ctx context.Context, city string) (float64, error) {
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
