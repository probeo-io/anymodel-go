package anymodel

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBatchStore(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "anymodel-test-store")
	defer os.RemoveAll(dir)

	store := NewBatchStore(dir)
	batchID := "batch-test-123"

	t.Run("create and get meta", func(t *testing.T) {
		batch := BatchObject{
			ID: batchID, Object: "batch", Status: BatchPending,
			Model: "openai/gpt-4o", ProviderName: "openai",
			BatchMode: BatchConcurrent, Total: 3, CreatedAt: "2025-01-01T00:00:00Z",
		}
		if err := store.Create(batch); err != nil {
			t.Fatalf("create: %v", err)
		}
		meta, err := store.GetMeta(batchID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if meta == nil || meta.ID != batchID || meta.Total != 3 {
			t.Error("metadata mismatch")
		}
	})

	t.Run("append and get results", func(t *testing.T) {
		r := BatchResultItem{CustomID: "req-1", Status: "success", Response: &ChatCompletion{
			ID: "gen-1", Object: "chat.completion", Model: "openai/gpt-4o",
			Choices: []ChatCompletionChoice{{Message: Message{Content: "hi"}}},
			Usage:   Usage{PromptTokens: 5, CompletionTokens: 1, TotalTokens: 6},
		}}
		if err := store.AppendResult(batchID, r); err != nil {
			t.Fatalf("append: %v", err)
		}
		results, err := store.GetResults(batchID)
		if err != nil {
			t.Fatalf("get results: %v", err)
		}
		if len(results) != 1 || results[0].CustomID != "req-1" {
			t.Error("results mismatch")
		}
	})

	t.Run("list batches", func(t *testing.T) {
		ids, err := store.ListBatches()
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(ids) != 1 {
			t.Errorf("expected 1 batch, got %d", len(ids))
		}
	})

	t.Run("nonexistent batch", func(t *testing.T) {
		meta, err := store.GetMeta("batch-nope")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if meta != nil {
			t.Error("expected nil")
		}
	})
}
