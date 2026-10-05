package domain

import (
	"errors"
	"testing"
)

func TestNewCEP(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"eight digits", "29902555", false},
		{"leading zeros", "01310100", false},
		{"empty", "", true},
		{"seven digits", "2990255", true},
		{"nine digits", "299025555", true},
		{"hyphenated", "29902-555", true},
		{"letters", "ABCDEFGH", true},
		{"digits with trailing letter", "2990255a", true},
		{"leading space", " 2990255", true},
		{"trailing newline", "29902555\n", true},
		{"embedded space", "299 2555", true},
		{"fullwidth unicode digits", "２９９０２５５５", true},
		{"arabic-indic digits", "٢٩٩٠٢٥٥٥", true},
		{"sql injection", "1' OR '1'", true},
		{"path traversal", "../../x1", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewCEP(tc.in)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidZipcode) {
					t.Fatalf("NewCEP(%q) error = %v, want ErrInvalidZipcode", tc.in, err)
				}
				if got != (CEP{}) {
					t.Errorf("NewCEP(%q) = %v, want zero value on error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewCEP(%q) unexpected error: %v", tc.in, err)
			}
			if got.String() != tc.in {
				t.Errorf("String() = %q, want %q", got.String(), tc.in)
			}
		})
	}
}

func TestCEP_ZeroValueIsEmpty(t *testing.T) {
	var c CEP
	if c.String() != "" {
		t.Errorf("zero CEP String() = %q, want empty", c.String())
	}
}
