package domain

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"invalid zipcode", ErrInvalidZipcode, "invalid zipcode"},
		{"zipcode not found", ErrZipcodeNotFound, "can not find zipcode"},
		{"upstream", ErrUpstream, "upstream service unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Error() != tc.want {
				t.Errorf("Error() = %q, want %q", tc.err.Error(), tc.want)
			}
		})
	}
}

func TestErrors_AreDistinctAndWrappable(t *testing.T) {
	wrapped := fmt.Errorf("lookup failed: %w", ErrZipcodeNotFound)
	if !errors.Is(wrapped, ErrZipcodeNotFound) {
		t.Error("wrapped error should match ErrZipcodeNotFound")
	}
	if errors.Is(wrapped, ErrInvalidZipcode) || errors.Is(wrapped, ErrUpstream) {
		t.Error("errors must not match each other")
	}
}
