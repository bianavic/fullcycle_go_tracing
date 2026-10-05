package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"service-b/internal/domain"
	"service-b/internal/usecase"
)

type fakeUseCase struct {
	out   usecase.Output
	err   error
	calls int
	gotIn domain.CEP
}

func (f *fakeUseCase) Execute(_ context.Context, cep domain.CEP) (usecase.Output, error) {
	f.calls++
	f.gotIn = cep
	return f.out, f.err
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newServer(uc WeatherUseCase) http.Handler {
	return NewRouter(NewHandler(uc, discardLogger()), discardLogger())
}

func post(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/weather", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("response is not JSON: %v (%q)", err, rec.Body.String())
	}
	return m
}

func TestPostWeather_Success(t *testing.T) {
	uc := &fakeUseCase{out: usecase.Output{
		City:        "São Paulo",
		Temperature: domain.Temperature{C: 28.5, F: 83.3, K: 301.5},
	}}

	rec := post(t, newServer(uc), `{"cep":"01310100"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	got := decodeMap(t, rec)
	want := map[string]any{"city": "São Paulo", "temp_C": 28.5, "temp_F": 83.3, "temp_K": 301.5}
	if len(got) != len(want) {
		t.Errorf("response has fields %v, want exactly %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	if uc.gotIn.String() != "01310100" {
		t.Errorf("use case received %q, want 01310100", uc.gotIn)
	}
}

func TestPostWeather_ZeroValuesAreNotOmitted(t *testing.T) {
	uc := &fakeUseCase{out: usecase.Output{City: "Oslo", Temperature: domain.Temperature{C: 0, F: 32, K: 273}}}
	got := decodeMap(t, post(t, newServer(uc), `{"cep":"01310100"}`))
	if v, ok := got["temp_C"]; !ok || v != 0.0 {
		t.Errorf("temp_C = %v (present=%v), want 0 present", v, ok)
	}
}

func TestPostWeather_InvalidBodyIs422AndSkipsUseCase(t *testing.T) {
	cases := []struct{ name, body string }{
		{"too short", `{"cep":"123"}`},
		{"too long", `{"cep":"299025555"}`},
		{"hyphenated", `{"cep":"29902-555"}`},
		{"letters", `{"cep":"ABCDEFGH"}`},
		{"number instead of string", `{"cep":29902555}`},
		{"null", `{"cep":null}`},
		{"boolean", `{"cep":true}`},
		{"array", `{"cep":["29902555"]}`},
		{"object", `{"cep":{"v":"29902555"}}`},
		{"empty string", `{"cep":""}`},
		{"missing field", `{}`},
		{"unknown field", `{"cep":"29902555","extra":1}`},
		{"empty body", ``},
		{"not json", `cep=29902555`},
		{"truncated json", `{"cep":"29902`},
		{"top-level string", `"29902555"`},
		{"top-level array", `[]`},
		{"trailing second object", `{"cep":"29902555"}{"cep":"29902555"}`},
		{"trailing garbage", `{"cep":"29902555"} x`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uc := &fakeUseCase{}
			rec := post(t, newServer(uc), tc.body)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422", rec.Code)
			}
			if got := decodeMap(t, rec); got["message"] != "invalid zipcode" || len(got) != 1 {
				t.Errorf("body = %v, want only {message: invalid zipcode}", got)
			}
			if uc.calls != 0 {
				t.Error("use case must not run for an invalid request")
			}
		})
	}
}

func TestPostWeather_BodyTooLargeIs413(t *testing.T) {
	uc := &fakeUseCase{}
	body := `{"cep":"` + strings.Repeat("1", maxBodyBytes+10) + `"}`

	rec := post(t, newServer(uc), body)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if got := decodeMap(t, rec); got["message"] != "request body too large" {
		t.Errorf("message = %v", got["message"])
	}
	if uc.calls != 0 {
		t.Error("use case must not run")
	}
}

func TestPostWeather_UseCaseErrorMapping(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantMsg    string
	}{
		{"not found", domain.ErrZipcodeNotFound, 404, "can not find zipcode"},
		{"wrapped not found", fmt.Errorf("find city: %w", domain.ErrZipcodeNotFound), 404, "can not find zipcode"},
		{"invalid", domain.ErrInvalidZipcode, 422, "invalid zipcode"},
		{"upstream", fmt.Errorf("%w: viacep: status 500 secret-detail", domain.ErrUpstream), 502, "upstream service unavailable"},
		{"unexpected", errors.New("db password is hunter2"), 500, "internal server error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := post(t, newServer(&fakeUseCase{err: tc.err}), `{"cep":"01310100"}`)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			got := decodeMap(t, rec)
			if got["message"] != tc.wantMsg || len(got) != 1 {
				t.Errorf("body = %v, want only {message: %q}", got, tc.wantMsg)
			}
			if body := rec.Body.String(); strings.Contains(body, "secret-detail") || strings.Contains(body, "hunter2") {
				t.Errorf("internal error details leaked to the client: %s", body)
			}
		})
	}
}

func TestRouter_MethodsAndPaths(t *testing.T) {
	h := newServer(&fakeUseCase{})

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(method+" /weather", func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(method, "/weather", nil))
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("status = %d, want 405", rec.Code)
			}
			if allow := rec.Header().Get("Allow"); !strings.Contains(allow, "POST") {
				t.Errorf("Allow = %q, want it to list POST", allow)
			}
		})
	}

	t.Run("unknown path", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/other", nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("healthz", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", rec.Code)
		}
	})
}
