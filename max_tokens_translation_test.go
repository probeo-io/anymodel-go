package anymodel

import (
	"encoding/json"
	"testing"
)

func TestUsesMaxCompletionTokens(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"gpt-4o", true},
		{"gpt-4o-mini", true},
		{"gpt-4o-2024-08-06", true},
		{"o1", true},
		{"o3", true},
		{"o4-mini", true},
		{"gpt-5-mini", true},
		{"gpt-4-turbo", false},
		{"gpt-3.5-turbo", false},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			if got := usesMaxCompletionTokens(tt.model); got != tt.want {
				t.Errorf("usesMaxCompletionTokens(%q) = %v, want %v", tt.model, got, tt.want)
			}
		})
	}
}

func TestOpenAIBuildBodyMaxTokensTranslation(t *testing.T) {
	maxTok := 100

	tests := []struct {
		name      string
		model     string
		wantField string // "max_completion_tokens" or "max_tokens"
	}{
		{"gpt-4o uses max_completion_tokens", "gpt-4o", "max_completion_tokens"},
		{"gpt-4o-mini uses max_completion_tokens", "gpt-4o-mini", "max_completion_tokens"},
		{"o1 uses max_completion_tokens", "o1", "max_completion_tokens"},
		{"o3 uses max_completion_tokens", "o3", "max_completion_tokens"},
		{"o4-mini uses max_completion_tokens", "o4-mini", "max_completion_tokens"},
		{"gpt-5-mini uses max_completion_tokens", "gpt-5-mini", "max_completion_tokens"},
		{"gpt-4-turbo uses max_tokens", "gpt-4-turbo", "max_tokens"},
		{"gpt-3.5-turbo uses max_tokens", "gpt-3.5-turbo", "max_tokens"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewOpenAIAdapter("test-key", "")
			req := ChatCompletionRequest{
				Model:     tt.model,
				Messages:  []Message{{Role: RoleUser, Content: "hi"}},
				MaxTokens: &maxTok,
			}
			body := a.buildBody(req)
			if _, ok := body[tt.wantField]; !ok {
				t.Errorf("expected body to have %q, got keys: %v", tt.wantField, keys(body))
			}
			// Ensure the OTHER field is not present
			otherField := "max_tokens"
			if tt.wantField == "max_tokens" {
				otherField = "max_completion_tokens"
			}
			if _, ok := body[otherField]; ok {
				t.Errorf("body should not have %q", otherField)
			}
		})
	}
}

func TestOpenAIBuildBodyOmitsMaxTokensWhenNil(t *testing.T) {
	a := NewOpenAIAdapter("test-key", "")
	req := ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}
	body := a.buildBody(req)
	if _, ok := body["max_tokens"]; ok {
		t.Error("max_tokens should be omitted when nil")
	}
	if _, ok := body["max_completion_tokens"]; ok {
		t.Error("max_completion_tokens should be omitted when nil")
	}
}

func TestAnthropicAlwaysUsesMaxTokens(t *testing.T) {
	a := NewAnthropicAdapter("test-key")
	req := ChatCompletionRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	}
	body := a.buildBody(req)
	if _, ok := body["max_tokens"]; !ok {
		t.Error("anthropic body should always have max_tokens")
	}
	if _, ok := body["max_completion_tokens"]; ok {
		t.Error("anthropic body should not have max_completion_tokens")
	}
}

func TestGoogleAlwaysUsesMaxOutputTokens(t *testing.T) {
	maxTok := 100
	a := NewGoogleAdapter("test-key")
	req := ChatCompletionRequest{
		Model:     "gemini-2.5-pro",
		Messages:  []Message{{Role: RoleUser, Content: "hi"}},
		MaxTokens: &maxTok,
	}
	body := a.buildBody(req)
	genConfig, ok := body["generationConfig"].(map[string]any)
	if !ok {
		t.Fatal("expected generationConfig in body")
	}
	if _, ok := genConfig["maxOutputTokens"]; !ok {
		t.Error("google body should use maxOutputTokens")
	}
	if _, ok := genConfig["max_tokens"]; ok {
		t.Error("google body should not have max_tokens")
	}
	if _, ok := genConfig["max_completion_tokens"]; ok {
		t.Error("google body should not have max_completion_tokens")
	}
}

func TestOpenAIBatchJSONLMaxTokensTranslation(t *testing.T) {
	maxTok := 100

	tests := []struct {
		name      string
		model     string
		wantField string
	}{
		{"gpt-4o batch uses max_completion_tokens", "gpt-4o", "max_completion_tokens"},
		{"gpt-4-turbo batch uses max_tokens", "gpt-4-turbo", "max_tokens"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewOpenAIBatchAdapter("test-key")
			jsonl := a.buildJSONL(tt.model, []BatchRequestItem{
				{CustomID: "req-1", Messages: []Message{{Role: RoleUser, Content: "hi"}}, MaxTokens: &maxTok},
			})
			var line struct {
				Body map[string]any `json:"body"`
			}
			if err := json.Unmarshal([]byte(jsonl), &line); err != nil {
				t.Fatalf("failed to parse JSONL: %v", err)
			}
			if _, ok := line.Body[tt.wantField]; !ok {
				t.Errorf("expected body to have %q", tt.wantField)
			}
		})
	}
}

func keys(m map[string]any) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
