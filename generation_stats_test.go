package anymodel

import "testing"

func TestGenerationStatsStore(t *testing.T) {
	t.Run("records and retrieves a generation", func(t *testing.T) {
		store := NewGenerationStatsStore(100)
		store.Record(GenerationStats{
			ID:               "gen-abc123",
			Model:            "anthropic/claude-sonnet-4-6",
			ProviderName:     "anthropic",
			TokensPrompt:     100,
			TokensCompletion: 50,
			Latency:          1.5,
			FinishReason:     FinishStop,
			Streamed:         false,
		})

		stats := store.Get("gen-abc123")
		if stats == nil {
			t.Fatal("expected stats, got nil")
		}
		if stats.ID != "gen-abc123" {
			t.Errorf("id = %q, want %q", stats.ID, "gen-abc123")
		}
		if stats.Model != "anthropic/claude-sonnet-4-6" {
			t.Errorf("model = %q, want %q", stats.Model, "anthropic/claude-sonnet-4-6")
		}
		if stats.ProviderName != "anthropic" {
			t.Errorf("provider = %q, want %q", stats.ProviderName, "anthropic")
		}
		if stats.TokensPrompt != 100 {
			t.Errorf("tokens_prompt = %d, want 100", stats.TokensPrompt)
		}
		if stats.TokensCompletion != 50 {
			t.Errorf("tokens_completion = %d, want 50", stats.TokensCompletion)
		}
		if stats.Latency != 1.5 {
			t.Errorf("latency = %f, want 1.5", stats.Latency)
		}
		if stats.Streamed != false {
			t.Error("streamed should be false")
		}
	})

	t.Run("returns nil for unknown id", func(t *testing.T) {
		store := NewGenerationStatsStore(100)
		if store.Get("nonexistent") != nil {
			t.Error("expected nil for unknown id")
		}
	})

	t.Run("lists recent generations", func(t *testing.T) {
		store := NewGenerationStatsStore(100)
		for i := 0; i < 5; i++ {
			store.Record(GenerationStats{
				ID:    "gen-" + string(rune('0'+i)),
				Model: "openai/gpt-4o",
			})
		}

		list := store.List(3)
		if len(list) != 3 {
			t.Fatalf("list length = %d, want 3", len(list))
		}
		// Most recent last in the Go implementation (entries are appended)
		if list[2].ID != "gen-4" {
			t.Errorf("last entry id = %q, want %q", list[2].ID, "gen-4")
		}
	})

	t.Run("evicts oldest when at capacity", func(t *testing.T) {
		store := NewGenerationStatsStore(3)
		for i := 0; i < 5; i++ {
			store.Record(GenerationStats{
				ID:    "gen-" + string(rune('0'+i)),
				Model: "openai/gpt-4o",
			})
		}

		if store.Get("gen-0") != nil {
			t.Error("gen-0 should have been evicted")
		}
		if store.Get("gen-1") != nil {
			t.Error("gen-1 should have been evicted")
		}
		if store.Get("gen-4") == nil {
			t.Error("gen-4 should still exist")
		}
	})

	t.Run("list returns all when limit exceeds count", func(t *testing.T) {
		store := NewGenerationStatsStore(100)
		store.Record(GenerationStats{ID: "gen-only", Model: "openai/gpt-4o"})

		list := store.List(10)
		if len(list) != 1 {
			t.Errorf("list length = %d, want 1", len(list))
		}
	})
}
