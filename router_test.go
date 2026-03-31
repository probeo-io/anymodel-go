package anymodel

import (
	"context"
	"testing"
)

// mockAdapter is a minimal Adapter for testing the Router.
type mockAdapter struct {
	name            string
	supportedParams map[string]bool
	lastRequest     ChatCompletionRequest
}

func newMockAdapter(name string, params []string) *mockAdapter {
	m := &mockAdapter{
		name:            name,
		supportedParams: make(map[string]bool),
	}
	for _, p := range params {
		m.supportedParams[p] = true
	}
	return m
}

func (a *mockAdapter) Name() string { return a.name }

func (a *mockAdapter) SendRequest(_ context.Context, req ChatCompletionRequest) (*ChatCompletion, error) {
	a.lastRequest = req
	return &ChatCompletion{
		ID:      GenerateID("gen"),
		Object:  "chat.completion",
		Created: 1000,
		Model:   a.name + "/" + req.Model,
		Choices: []ChatCompletionChoice{{
			Index:        0,
			Message:      Message{Role: RoleAssistant, Content: "Hello"},
			FinishReason: FinishStop,
		}},
		Usage: Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}, nil
}

func (a *mockAdapter) SendStreamingRequest(_ context.Context, _ ChatCompletionRequest) (<-chan ChatCompletionChunk, <-chan error) {
	ch := make(chan ChatCompletionChunk)
	errCh := make(chan error)
	close(ch)
	close(errCh)
	return ch, errCh
}

func (a *mockAdapter) ListModels(_ context.Context) ([]ModelInfo, error) {
	return nil, nil
}

func (a *mockAdapter) SupportsParameter(param string) bool {
	return a.supportedParams[param]
}

func (a *mockAdapter) SupportsBatch() bool { return false }

func TestRouter(t *testing.T) {
	t.Run("strips unsupported parameters before sending", func(t *testing.T) {
		registry := NewRegistry()
		adapter := newMockAdapter("test", []string{"temperature", "max_tokens", "top_p", "stop", "tools", "tool_choice"})
		registry.Register("test", adapter)

		statsStore := NewGenerationStatsStore(100)
		router := NewRouter(registry, nil, nil, statsStore)

		topK := 40
		seed := 42
		freq := 0.5
		temp := 0.7
		req := ChatCompletionRequest{
			Model:            "test/some-model",
			Messages:         []Message{{Role: RoleUser, Content: "Hello"}},
			Temperature:      &temp,
			TopK:             &topK,
			Seed:             &seed,
			FrequencyPenalty: &freq,
		}

		_, err := router.Complete(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		sent := adapter.lastRequest
		if sent.Temperature == nil || *sent.Temperature != 0.7 {
			t.Error("temperature should be preserved")
		}
		if sent.TopK != nil {
			t.Error("top_k should be stripped")
		}
		if sent.Seed != nil {
			t.Error("seed should be stripped")
		}
		if sent.FrequencyPenalty != nil {
			t.Error("frequency_penalty should be stripped")
		}
	})

	t.Run("resolves aliases", func(t *testing.T) {
		registry := NewRegistry()
		adapter := newMockAdapter("anthropic", []string{"temperature"})
		registry.Register("anthropic", adapter)

		aliases := map[string]string{"smart": "anthropic/claude-sonnet-4-6"}
		statsStore := NewGenerationStatsStore(100)
		router := NewRouter(registry, aliases, nil, statsStore)

		_, err := router.Complete(context.Background(), ChatCompletionRequest{
			Model:    "smart",
			Messages: []Message{{Role: RoleUser, Content: "Hi"}},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if adapter.lastRequest.Model != "claude-sonnet-4-6" {
			t.Errorf("model = %q, want %q", adapter.lastRequest.Model, "claude-sonnet-4-6")
		}
	})

	t.Run("throws on missing provider", func(t *testing.T) {
		registry := NewRegistry()
		statsStore := NewGenerationStatsStore(100)
		router := NewRouter(registry, nil, nil, statsStore)

		_, err := router.Complete(context.Background(), ChatCompletionRequest{
			Model:    "unknown/model",
			Messages: []Message{{Role: RoleUser, Content: "Hi"}},
		})
		if err == nil {
			t.Fatal("expected error for unknown provider")
		}
		e, ok := err.(*Error)
		if !ok {
			t.Fatalf("expected *Error, got %T", err)
		}
		if e.Code != 400 {
			t.Errorf("error code = %d, want 400", e.Code)
		}
	})

	t.Run("applies default temperature", func(t *testing.T) {
		registry := NewRegistry()
		adapter := newMockAdapter("test", []string{"temperature"})
		registry.Register("test", adapter)

		temp := 0.3
		defaults := &DefaultsConfig{Temperature: &temp}
		statsStore := NewGenerationStatsStore(100)
		router := NewRouter(registry, nil, defaults, statsStore)

		_, err := router.Complete(context.Background(), ChatCompletionRequest{
			Model:    "test/model",
			Messages: []Message{{Role: RoleUser, Content: "Hi"}},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if adapter.lastRequest.Temperature == nil || *adapter.lastRequest.Temperature != 0.3 {
			t.Error("default temperature not applied")
		}
	})

	t.Run("request temperature overrides default", func(t *testing.T) {
		registry := NewRegistry()
		adapter := newMockAdapter("test", []string{"temperature"})
		registry.Register("test", adapter)

		defaultTemp := 0.3
		defaults := &DefaultsConfig{Temperature: &defaultTemp}
		statsStore := NewGenerationStatsStore(100)
		router := NewRouter(registry, nil, defaults, statsStore)

		reqTemp := 0.9
		_, err := router.Complete(context.Background(), ChatCompletionRequest{
			Model:       "test/model",
			Messages:    []Message{{Role: RoleUser, Content: "Hi"}},
			Temperature: &reqTemp,
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if adapter.lastRequest.Temperature == nil || *adapter.lastRequest.Temperature != 0.9 {
			t.Error("request temperature should override default")
		}
	})
}
