package anymodel

import (
	"strings"
	"testing"
)

// Tests for GenerateID — parseModelString and validateRequest are tested
// in model_parser_test.go and validate_test.go respectively.

func TestGenerateID(t *testing.T) {
	t.Run("generates prefixed IDs", func(t *testing.T) {
		id := GenerateID("gen")
		if !strings.HasPrefix(id, "gen-") {
			t.Errorf("id = %q, want prefix 'gen-'", id)
		}
	})

	t.Run("generates unique IDs", func(t *testing.T) {
		seen := make(map[string]bool, 100)
		for i := 0; i < 100; i++ {
			id := GenerateID("gen")
			if seen[id] {
				t.Fatalf("duplicate id: %s", id)
			}
			seen[id] = true
		}
	})

	t.Run("supports custom prefix", func(t *testing.T) {
		id := GenerateID("batch")
		if !strings.HasPrefix(id, "batch-") {
			t.Errorf("id = %q, want prefix 'batch-'", id)
		}
	})

	t.Run("has sufficient randomness", func(t *testing.T) {
		id := GenerateID("gen")
		random := id[len("gen-"):]
		if len(random) < 16 {
			t.Errorf("random part length = %d, want >= 16", len(random))
		}
	})
}

func TestEstimateTokenCount(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{"empty string", "", 0},
		{"short text", "hi", 1},
		{"medium text", "Hello, how are you doing today?", 7},
		{"long text", strings.Repeat("word ", 100), 125},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateTokenCount(tt.text)
			if got != tt.want {
				t.Errorf("EstimateTokenCount(%q) = %d, want %d", tt.text, got, tt.want)
			}
		})
	}
}

func TestResolveMaxTokens(t *testing.T) {
	t.Run("returns user value when provided", func(t *testing.T) {
		userMax := 1000
		got := ResolveMaxTokens("openai/gpt-4o", nil, &userMax)
		if got != 1000 {
			t.Errorf("got = %d, want 1000", got)
		}
	})

	t.Run("computes default when user value is nil", func(t *testing.T) {
		msgs := []Message{{Role: RoleUser, Content: "Hello"}}
		got := ResolveMaxTokens("openai/gpt-4o", msgs, nil)
		if got <= 0 {
			t.Errorf("got = %d, want > 0", got)
		}
	})

	t.Run("returns at least 1", func(t *testing.T) {
		// Very long input that would exceed context
		longContent := strings.Repeat("a", 1000000)
		msgs := []Message{{Role: RoleUser, Content: longContent}}
		got := ResolveMaxTokens("openai/gpt-4o", msgs, nil)
		if got < 1 {
			t.Errorf("got = %d, want >= 1", got)
		}
	})
}

// parseModelString additional edge cases (supplements model_parser_test.go)
func TestParseModelStringMultiSlash(t *testing.T) {
	t.Run("handles models with slashes in name", func(t *testing.T) {
		result, err := ParseModelString("custom/meta-llama/llama-3.3-70b", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Provider != "custom" {
			t.Errorf("provider = %q, want %q", result.Provider, "custom")
		}
		if result.Model != "meta-llama/llama-3.3-70b" {
			t.Errorf("model = %q, want %q", result.Model, "meta-llama/llama-3.3-70b")
		}
	})
}

// validateRequest additional edge cases (supplements validate_test.go)
func TestValidateRequestAdditional(t *testing.T) {
	t.Run("throws on invalid top_p", func(t *testing.T) {
		topP := 1.5
		req := &ChatCompletionRequest{
			Model:    "openai/gpt-4o",
			Messages: []Message{{Role: RoleUser, Content: "hello"}},
			TopP:     &topP,
		}
		if err := ValidateRequest(req); err == nil {
			t.Error("expected error for invalid top_p")
		}
	})

	t.Run("throws on negative temperature", func(t *testing.T) {
		temp := -1.0
		req := &ChatCompletionRequest{
			Model:       "openai/gpt-4o",
			Messages:    []Message{{Role: RoleUser, Content: "hello"}},
			Temperature: &temp,
		}
		if err := ValidateRequest(req); err == nil {
			t.Error("expected error for negative temperature")
		}
	})

	t.Run("throws on top_logprobs without logprobs", func(t *testing.T) {
		topLogprobs := 5
		req := &ChatCompletionRequest{
			Model:       "openai/gpt-4o",
			Messages:    []Message{{Role: RoleUser, Content: "hello"}},
			TopLogprobs: &topLogprobs,
		}
		if err := ValidateRequest(req); err == nil {
			t.Error("expected error for top_logprobs without logprobs")
		}
	})

	t.Run("allows top_logprobs with logprobs", func(t *testing.T) {
		logprobs := true
		topLogprobs := 5
		req := &ChatCompletionRequest{
			Model:       "openai/gpt-4o",
			Messages:    []Message{{Role: RoleUser, Content: "hello"}},
			Logprobs:    &logprobs,
			TopLogprobs: &topLogprobs,
		}
		if err := ValidateRequest(req); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}
