package anymodel

import (
	"testing"
)

func TestError(t *testing.T) {
	t.Run("create and message", func(t *testing.T) {
		err := NewError(429, "rate limited", map[string]any{"provider_name": "openai"})
		if err.Code != 429 {
			t.Errorf("code = %d, want 429", err.Code)
		}
		if err.Error() != "anymodel error 429: rate limited" {
			t.Errorf("unexpected error string: %s", err.Error())
		}
		if err.Metadata["provider_name"] != "openai" {
			t.Error("metadata not set")
		}
	})

	t.Run("to map", func(t *testing.T) {
		err := NewError(400, "bad request", nil)
		m := err.ToMap()
		errMap, ok := m["error"].(map[string]any)
		if !ok {
			t.Fatal("expected error map")
		}
		if errMap["code"] != 400 {
			t.Errorf("code = %v, want 400", errMap["code"])
		}
	})

	t.Run("nil metadata defaults to empty", func(t *testing.T) {
		err := NewError(500, "internal", nil)
		if err.Metadata == nil {
			t.Error("metadata should default to empty map")
		}
	})
}
