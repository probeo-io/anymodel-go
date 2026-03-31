package anymodel

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupBatchCostTest(t *testing.T) (*BatchManager, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "batches")
	store := NewBatchStore(dir)
	registry := NewRegistry()
	statsStore := NewGenerationStatsStore(100)
	router := NewRouter(registry, nil, nil, statsStore)
	mgr := NewBatchManager(registry, store, nil, 5, 500, 5*time.Second, router)
	return mgr, dir
}

func TestBatchCostConcurrentWithoutFlex(t *testing.T) {
	mgr, _ := setupBatchCostTest(t)
	store := mgr.GetStore()

	batch := BatchObject{
		ID: "batch-cost-1", Object: "batch", Status: BatchCompleted,
		Model: "openai/gpt-4o", ProviderName: "openai",
		BatchMode: BatchConcurrent, ServiceTier: "",
		Total: 1, Completed: 1, CreatedAt: time.Now().Format(time.RFC3339),
	}
	if err := store.Create(batch); err != nil {
		t.Fatal(err)
	}
	store.AppendResult(batch.ID, BatchResultItem{
		CustomID: "req-1", Status: "success",
		Response: &ChatCompletion{
			ID: "gen-1", Object: "chat.completion", Model: "openai/gpt-4o",
			Usage: Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
			Choices: []ChatCompletionChoice{{Index: 0, FinishReason: FinishStop}},
		},
	})

	results, err := mgr.Results(batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Concurrent without flex = full price (discount = 1.0)
	fullCost := CalculateCost("openai/gpt-4o", 100, 50)
	if fullCost == 0 {
		t.Skip("no pricing data for openai/gpt-4o, skipping cost assertion")
	}
	if results.UsageSummary.EstimatedCost != fullCost {
		t.Errorf("estimated_cost = %f, want %f (full price)", results.UsageSummary.EstimatedCost, fullCost)
	}
}

func TestBatchCostConcurrentWithFlex(t *testing.T) {
	mgr, _ := setupBatchCostTest(t)
	store := mgr.GetStore()

	batch := BatchObject{
		ID: "batch-cost-2", Object: "batch", Status: BatchCompleted,
		Model: "openai/gpt-4o", ProviderName: "openai",
		BatchMode: BatchConcurrent, ServiceTier: "flex",
		Total: 1, Completed: 1, CreatedAt: time.Now().Format(time.RFC3339),
	}
	if err := store.Create(batch); err != nil {
		t.Fatal(err)
	}
	store.AppendResult(batch.ID, BatchResultItem{
		CustomID: "req-1", Status: "success",
		Response: &ChatCompletion{
			ID: "gen-1", Object: "chat.completion", Model: "openai/gpt-4o",
			Usage: Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
			Choices: []ChatCompletionChoice{{Index: 0, FinishReason: FinishStop}},
		},
	})

	results, err := mgr.Results(batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	fullCost := CalculateCost("openai/gpt-4o", 100, 50)
	if fullCost == 0 {
		t.Skip("no pricing data for openai/gpt-4o, skipping cost assertion")
	}
	expected := fullCost * 0.5
	if results.UsageSummary.EstimatedCost != expected {
		t.Errorf("estimated_cost = %f, want %f (50%% discount)", results.UsageSummary.EstimatedCost, expected)
	}
}

func TestBatchCostConcurrentWithAuto(t *testing.T) {
	mgr, _ := setupBatchCostTest(t)
	store := mgr.GetStore()

	batch := BatchObject{
		ID: "batch-cost-3", Object: "batch", Status: BatchCompleted,
		Model: "openai/gpt-4o", ProviderName: "openai",
		BatchMode: BatchConcurrent, ServiceTier: "auto",
		Total: 1, Completed: 1, CreatedAt: time.Now().Format(time.RFC3339),
	}
	if err := store.Create(batch); err != nil {
		t.Fatal(err)
	}
	store.AppendResult(batch.ID, BatchResultItem{
		CustomID: "req-1", Status: "success",
		Response: &ChatCompletion{
			ID: "gen-1", Object: "chat.completion", Model: "openai/gpt-4o",
			Usage: Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
			Choices: []ChatCompletionChoice{{Index: 0, FinishReason: FinishStop}},
		},
	})

	results, err := mgr.Results(batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	// "auto" is not "flex", so full price
	fullCost := CalculateCost("openai/gpt-4o", 100, 50)
	if fullCost == 0 {
		t.Skip("no pricing data for openai/gpt-4o, skipping cost assertion")
	}
	if results.UsageSummary.EstimatedCost != fullCost {
		t.Errorf("estimated_cost = %f, want %f (full price for auto)", results.UsageSummary.EstimatedCost, fullCost)
	}
}

func TestBatchServiceTierPersisted(t *testing.T) {
	mgr, dir := setupBatchCostTest(t)
	store := mgr.GetStore()

	batch := BatchObject{
		ID: "batch-st-1", Object: "batch", Status: BatchCompleted,
		Model: "openai/gpt-4o", ProviderName: "openai",
		BatchMode: BatchConcurrent, ServiceTier: "flex",
		Total: 0, CreatedAt: time.Now().Format(time.RFC3339),
	}
	if err := store.Create(batch); err != nil {
		t.Fatal(err)
	}

	// Verify file was written with service_tier
	data, err := os.ReadFile(filepath.Join(dir, "batch-st-1", "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); !contains(got, `"service_tier"`) {
		t.Error("meta.json should contain service_tier field")
	}

	// Read back
	got, err := store.GetMeta("batch-st-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ServiceTier != "flex" {
		t.Errorf("ServiceTier = %q, want \"flex\"", got.ServiceTier)
	}
}

func TestBatchServiceTierDefaultsToEmpty(t *testing.T) {
	mgr, _ := setupBatchCostTest(t)
	store := mgr.GetStore()

	batch := BatchObject{
		ID: "batch-st-2", Object: "batch", Status: BatchCompleted,
		Model: "openai/gpt-4o", ProviderName: "openai",
		BatchMode: BatchConcurrent,
		Total: 0, CreatedAt: time.Now().Format(time.RFC3339),
	}
	if err := store.Create(batch); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetMeta("batch-st-2")
	if err != nil {
		t.Fatal(err)
	}
	if got.ServiceTier != "" {
		t.Errorf("ServiceTier = %q, want empty", got.ServiceTier)
	}
}

func TestBatchServiceTierFallsBackToFirstRequestItem(t *testing.T) {
	mgr, _ := setupBatchCostTest(t)
	store := mgr.GetStore()

	// Simulate what Create() does: resolve service_tier from first request item
	req := BatchCreateRequest{
		Model: "openai/gpt-4o",
		Requests: []BatchRequestItem{
			{CustomID: "req-1", Messages: []Message{{Role: RoleUser, Content: "hi"}}, ServiceTier: "flex"},
			{CustomID: "req-2", Messages: []Message{{Role: RoleUser, Content: "bye"}}},
		},
	}

	// Resolve service_tier the same way Create() does
	serviceTier := ""
	if req.Options != nil && req.Options.ServiceTier != "" {
		serviceTier = req.Options.ServiceTier
	} else if len(req.Requests) > 0 && req.Requests[0].ServiceTier != "" {
		serviceTier = req.Requests[0].ServiceTier
	}

	batch := BatchObject{
		ID: "batch-st-3", Object: "batch", Status: BatchCompleted,
		Model: req.Model, ProviderName: "openai",
		BatchMode: BatchConcurrent, ServiceTier: serviceTier,
		Total: len(req.Requests), CreatedAt: time.Now().Format(time.RFC3339),
	}
	if err := store.Create(batch); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetMeta("batch-st-3")
	if err != nil {
		t.Fatal(err)
	}
	if got.ServiceTier != "flex" {
		t.Errorf("ServiceTier = %q, want \"flex\" (from first request item)", got.ServiceTier)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
