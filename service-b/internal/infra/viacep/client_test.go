package viacep

import (
	"testing"
)

func TestFindCityByCEP_Success(t *testing.T) {
}

func TestFindCityByCEP_ErrorMapping(t *testing.T) {
}

func TestFindCityByCEP_ServerDownIsUpstream(t *testing.T) {
}

func TestFindCityByCEP_TimeoutIsUpstream(t *testing.T) {
}

func TestFindCityByCEP_ContextCancelled(t *testing.T) {
}

func TestFindCityByCEP_Span(t *testing.T) {
	t.Run("success", func(t *testing.T) {
	})

	t.Run("not found is not a span error", func(t *testing.T) {
	})

	t.Run("upstream failure marks the span", func(t *testing.T) {
	})

	t.Run("span is a child of the caller", func(t *testing.T) {
	})
}
