package requestid

import (
	"context"
	"testing"
)

func TestWithFromContextRoundTrip(t *testing.T) {
	ctx := With(context.Background(), "abc-123")
	if got := FromContext(ctx); got != "abc-123" {
		t.Errorf("FromContext = %q, want %q", got, "abc-123")
	}
}

func TestFromContextEmptyContext(t *testing.T) {
	if got := FromContext(context.Background()); got != "" {
		t.Errorf("FromContext(empty) = %q, want \"\"", got)
	}
}

func TestFromContextNonStringValue(t *testing.T) {
	// A non-string value stored under the key must yield "" rather than panic.
	ctx := context.WithValue(context.Background(), requestIDKey, 42)
	if got := FromContext(ctx); got != "" {
		t.Errorf("FromContext(non-string) = %q, want \"\"", got)
	}
}
