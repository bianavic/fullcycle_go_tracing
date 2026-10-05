package weatherapi

import (
	"net/http"
	"strings"
	"testing"
)

const testKey = "SECRET-KEY-123"

func respond(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func TestCurrentTempC_Success(t *testing.T) {
}

func TestCurrentTempC_ZeroAndNegativeAreValid(t *testing.T) {
}

func TestCurrentTempC_Errors(t *testing.T) {
}

func TestCurrentTempC_NetworkErrorsDoNotLeakKey(t *testing.T) {
	t.Run("connection refused", func(t *testing.T) {
	})

	t.Run("timeout", func(t *testing.T) {
	})

	t.Run("cancelled context", func(t *testing.T) {
	})
}

func TestCurrentTempC_InvalidBaseURLDoesNotLeakKey(t *testing.T) {
}

func TestCurrentTempC_Span(t *testing.T) {
	t.Run("success", func(t *testing.T) {
	})

	t.Run("failure marks the span and never records the key", func(t *testing.T) {
	})
}

func assertNoSecret(t *testing.T, s string) {
	t.Helper()
	if strings.Contains(s, testKey) || strings.Contains(strings.ToLower(s), "key=") {
		t.Errorf("secret leaked in %q", s)
	}
}
