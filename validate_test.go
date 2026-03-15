package anymodel

import "testing"

func TestValidateRequest(t *testing.T) {
	t.Run("valid request", func(t *testing.T) {
		req := &ChatCompletionRequest{
			Model:    "openai/gpt-4o",
			Messages: []Message{{Role: RoleUser, Content: "hello"}},
		}
		if err := ValidateRequest(req); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("missing model", func(t *testing.T) {
		req := &ChatCompletionRequest{Messages: []Message{{Role: RoleUser, Content: "hello"}}}
		if err := ValidateRequest(req); err == nil {
			t.Error("expected error")
		}
	})

	t.Run("empty messages", func(t *testing.T) {
		req := &ChatCompletionRequest{Model: "openai/gpt-4o"}
		if err := ValidateRequest(req); err == nil {
			t.Error("expected error")
		}
	})

	t.Run("invalid temperature", func(t *testing.T) {
		temp := 3.0
		req := &ChatCompletionRequest{
			Model: "openai/gpt-4o", Messages: []Message{{Role: RoleUser, Content: "hello"}},
			Temperature: &temp,
		}
		if err := ValidateRequest(req); err == nil {
			t.Error("expected error")
		}
	})

	t.Run("too many stop sequences", func(t *testing.T) {
		req := &ChatCompletionRequest{
			Model: "openai/gpt-4o", Messages: []Message{{Role: RoleUser, Content: "hello"}},
			Stop: []string{"a", "b", "c", "d", "e"},
		}
		if err := ValidateRequest(req); err == nil {
			t.Error("expected error")
		}
	})

	t.Run("fallback with models", func(t *testing.T) {
		req := &ChatCompletionRequest{
			Models: []string{"openai/gpt-4o", "anthropic/claude-sonnet-4-6"},
			Route: "fallback", Messages: []Message{{Role: RoleUser, Content: "hello"}},
		}
		if err := ValidateRequest(req); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("models without fallback route", func(t *testing.T) {
		req := &ChatCompletionRequest{
			Models: []string{"openai/gpt-4o"}, Messages: []Message{{Role: RoleUser, Content: "hello"}},
		}
		if err := ValidateRequest(req); err == nil {
			t.Error("expected error")
		}
	})
}
