package viacep

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"bianavic/fullcycle_go_tracing/internal/domain"
)

const (
	// DefaultBaseURL is the public ViaCEP endpoint.
	DefaultBaseURL = "https://viacep.com.br/ws"

	spanName     = "lookup-cep"
	maxBodyBytes = 64 << 10
)

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string, httpClient *http.Client) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: httpClient}
}

type response struct {
	Localidade string   `json:"localidade"`
	Erro       erroFlag `json:"erro"`
}

// erroFlag accepts both `true` and `"true"`: ViaCEP has returned both forms
// for unknown CEPs.
type erroFlag bool

func (e *erroFlag) UnmarshalJSON(b []byte) error {
	*e = strings.Trim(string(b), `"`) == "true"
	return nil
}

func (c *Client) FindCityByCEP(ctx context.Context, cep domain.CEP) (string, error) {

	city, err := c.lookup(ctx, cep)
	switch {
	case err == nil:
	case errors.Is(err, domain.ErrZipcodeNotFound):
	default:
		return "", fmt.Errorf("%w: viacep: %w", domain.ErrUpstream, err)
	}
	return city, err
}

func (c *Client) lookup(ctx context.Context, cep domain.CEP) (string, error) {
	// cep is a validated 8-digit value, so it is safe to place in the path.
	endpoint := c.baseURL + "/" + url.PathEscape(cep.String()) + "/json/"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("%w: viacep: build request: %w", domain.ErrUpstream, err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: viacep: request failed: %w", domain.ErrUpstream, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return "", domain.ErrZipcodeNotFound
	case http.StatusBadRequest:
		return "", domain.ErrInvalidZipcode
	default:
		return "", fmt.Errorf("%w: viacep: unexpected status %d", domain.ErrUpstream, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return "", fmt.Errorf("%w: viacep: read body: %w", domain.ErrUpstream, err)
	}
	if len(body) > maxBodyBytes {
		return "", fmt.Errorf("%w: viacep: response exceeds %d bytes", domain.ErrUpstream, maxBodyBytes)
	}

	var r response
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("%w: viacep: decode body: %w", domain.ErrUpstream, err)
	}
	if bool(r.Erro) || r.Localidade == "" {
		return "", domain.ErrZipcodeNotFound
	}
	return r.Localidade, nil
}
