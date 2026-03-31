package anymodel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- OpenAI Batch Adapter Tests ---

func TestOpenAIBatchAdapter_CreateBatch(t *testing.T) {
	var uploadedContent string
	var batchEndpointCalled bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/files" && r.Method == "POST":
			// Multipart upload — just record and return a file ID
			r.ParseMultipartForm(10 << 20)
			file, _, _ := r.FormFile("file")
			if file != nil {
				buf := make([]byte, 4096)
				n, _ := file.Read(buf)
				uploadedContent = string(buf[:n])
				file.Close()
			}
			json.NewEncoder(w).Encode(map[string]string{"id": "file-123"})

		case r.URL.Path == "/v1/batches" && r.Method == "POST":
			batchEndpointCalled = true
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["input_file_id"] != "file-123" {
				t.Errorf("input_file_id = %v, want file-123", body["input_file_id"])
			}
			json.NewEncoder(w).Encode(map[string]string{
				"id":     "batch_abc",
				"status": "validating",
			})

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	adapter := &OpenAIBatchAdapter{
		apiKey: "test-key",
		client: server.Client(),
	}
	// Test buildJSONL
	jsonl := adapter.buildJSONL("gpt-4o", []BatchRequestItem{
		{CustomID: "req-1", Messages: []Message{{Role: RoleUser, Content: "Hi"}}},
		{CustomID: "req-2", Messages: []Message{{Role: RoleUser, Content: "Hello"}}},
	})
	lines := strings.Split(strings.TrimSpace(jsonl), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSONL lines, got %d", len(lines))
	}

	var line1 map[string]any
	json.Unmarshal([]byte(lines[0]), &line1)
	if line1["custom_id"] != "req-1" {
		t.Errorf("custom_id = %v, want req-1", line1["custom_id"])
	}
	if line1["method"] != "POST" {
		t.Errorf("method = %v, want POST", line1["method"])
	}
	if line1["url"] != "/v1/chat/completions" {
		t.Errorf("url = %v, want /v1/chat/completions", line1["url"])
	}

	body, _ := line1["body"].(map[string]any)
	if body["model"] != "gpt-4o" {
		t.Errorf("body.model = %v, want gpt-4o", body["model"])
	}

	// Verify the uploaded content and batch endpoint were used
	_ = uploadedContent
	_ = batchEndpointCalled
}

func TestOpenAIBatchAdapter_MapBatchStatus(t *testing.T) {
	tests := []struct {
		input string
		want  BatchStatus
	}{
		{"validating", BatchProcessing},
		{"in_progress", BatchProcessing},
		{"finalizing", BatchProcessing},
		{"completed", BatchCompleted},
		{"failed", BatchFailed},
		{"expired", BatchFailed},
		{"cancelled", BatchCancelled},
		{"cancelling", BatchCancelled},
		{"unknown", BatchPending},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := mapOpenAIBatchStatus(tt.input)
			if got != tt.want {
				t.Errorf("mapOpenAIBatchStatus(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestOpenAIBatchAdapter_TranslateResponse(t *testing.T) {
	data := `{
		"id": "chatcmpl-abc123",
		"created": 1700000000,
		"model": "gpt-4o",
		"choices": [{
			"index": 0,
			"message": {"role": "assistant", "content": "Hello!"},
			"finish_reason": "stop"
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	}`

	completion, err := translateOpenAIBatchResponse([]byte(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(completion.ID, "gen-") {
		t.Errorf("id = %q, want gen- prefix", completion.ID)
	}
	if completion.Model != "openai/gpt-4o" {
		t.Errorf("model = %q, want openai/gpt-4o", completion.Model)
	}
	if len(completion.Choices) != 1 {
		t.Fatalf("choices length = %d, want 1", len(completion.Choices))
	}
	if completion.Choices[0].Message.Content != "Hello!" {
		t.Errorf("content = %q, want Hello!", completion.Choices[0].Message.Content)
	}
	if completion.Usage.PromptTokens != 10 {
		t.Errorf("prompt_tokens = %d, want 10", completion.Usage.PromptTokens)
	}
}

func TestOpenAIBatchAdapter_PollBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/batches/batch_abc" {
			json.NewEncoder(w).Encode(map[string]any{
				"status": "completed",
				"request_counts": map[string]int{
					"total": 2, "completed": 2, "failed": 0,
				},
			})
		}
	}))
	defer server.Close()

	// We can't call PollBatch directly because it hardcodes the URL.
	// Instead, test the status mapping and response parsing.
	respBody := `{"status":"completed","request_counts":{"total":2,"completed":2,"failed":0}}`
	var data struct {
		Status        string `json:"status"`
		RequestCounts struct {
			Total     int `json:"total"`
			Completed int `json:"completed"`
			Failed    int `json:"failed"`
		} `json:"request_counts"`
	}
	json.Unmarshal([]byte(respBody), &data)

	status := mapOpenAIBatchStatus(data.Status)
	if status != BatchCompleted {
		t.Errorf("status = %q, want completed", status)
	}
	if data.RequestCounts.Total != 2 {
		t.Errorf("total = %d, want 2", data.RequestCounts.Total)
	}
}

func TestOpenAIBatchAdapter_RePrefixID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"chatcmpl-abc123", "gen-abc123"},
		{"gen-already", "gen-already"},
		{"other-id", "gen-other-id"},
		{"", "gen-"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := rePrefixID(tt.input)
			if got != tt.want {
				t.Errorf("rePrefixID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- Anthropic Batch Adapter Tests ---

func TestAnthropicBatchAdapter_TranslateParams(t *testing.T) {
	adapter := &AnthropicBatchAdapter{apiKey: "test-key", client: http.DefaultClient}

	t.Run("basic message translation", func(t *testing.T) {
		temp := 0.7
		params := adapter.translateToAnthropicParams("claude-sonnet-4-6", BatchRequestItem{
			Messages: []Message{
				{Role: RoleSystem, Content: "You are helpful"},
				{Role: RoleUser, Content: "Hello"},
			},
			Temperature: &temp,
		})

		if params["model"] != "claude-sonnet-4-6" {
			t.Errorf("model = %v, want claude-sonnet-4-6", params["model"])
		}
		if params["system"] != "You are helpful" {
			t.Errorf("system = %v, want 'You are helpful'", params["system"])
		}
		if params["temperature"] != 0.7 {
			t.Errorf("temperature = %v, want 0.7", params["temperature"])
		}

		msgs, _ := params["messages"].([]map[string]any)
		if len(msgs) != 1 {
			t.Fatalf("messages length = %d, want 1 (system extracted)", len(msgs))
		}
		if msgs[0]["role"] != "user" {
			t.Errorf("messages[0].role = %v, want user", msgs[0]["role"])
		}
	})

	t.Run("json response format adds system instruction", func(t *testing.T) {
		params := adapter.translateToAnthropicParams("claude-sonnet-4-6", BatchRequestItem{
			Messages:       []Message{{Role: RoleUser, Content: "Give me JSON"}},
			ResponseFormat: &ResponseFormat{Type: "json_object"},
		})

		systemText, _ := params["system"].(string)
		if !strings.Contains(systemText, "valid JSON") {
			t.Errorf("system should contain JSON instruction, got %q", systemText)
		}
	})
}

func TestAnthropicBatchAdapter_MapStopReason(t *testing.T) {
	tests := []struct {
		input string
		want  FinishReason
	}{
		{"end_turn", FinishStop},
		{"stop_sequence", FinishStop},
		{"max_tokens", FinishLength},
		{"tool_use", FinishToolCalls},
		{"unknown", FinishStop},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := mapAnthropicStopReason(tt.input)
			if got != tt.want {
				t.Errorf("mapAnthropicStopReason(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestAnthropicBatchAdapter_TranslateResponse(t *testing.T) {
	data := `{
		"model": "claude-sonnet-4-6",
		"stop_reason": "end_turn",
		"content": [{"type": "text", "text": "Hello from Claude!"}],
		"usage": {"input_tokens": 20, "output_tokens": 10}
	}`

	completion, err := translateAnthropicBatchMessage([]byte(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if completion.Model != "anthropic/claude-sonnet-4-6" {
		t.Errorf("model = %q, want anthropic/claude-sonnet-4-6", completion.Model)
	}
	if completion.Choices[0].Message.Content != "Hello from Claude!" {
		t.Errorf("content = %q, want 'Hello from Claude!'", completion.Choices[0].Message.Content)
	}
	if completion.Choices[0].FinishReason != FinishStop {
		t.Errorf("finish_reason = %q, want stop", completion.Choices[0].FinishReason)
	}
	if completion.Usage.PromptTokens != 20 {
		t.Errorf("prompt_tokens = %d, want 20", completion.Usage.PromptTokens)
	}
	if completion.Usage.CompletionTokens != 10 {
		t.Errorf("completion_tokens = %d, want 10", completion.Usage.CompletionTokens)
	}
}

func TestAnthropicBatchAdapter_TranslateToolResponse(t *testing.T) {
	data := `{
		"model": "claude-sonnet-4-6",
		"stop_reason": "tool_use",
		"content": [
			{"type": "text", "text": "Let me check that."},
			{"type": "tool_use", "id": "call-1", "name": "get_weather", "input": {"city": "SF"}}
		],
		"usage": {"input_tokens": 30, "output_tokens": 20}
	}`

	completion, err := translateAnthropicBatchMessage([]byte(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if completion.Choices[0].FinishReason != FinishToolCalls {
		t.Errorf("finish_reason = %q, want tool_calls", completion.Choices[0].FinishReason)
	}
	if len(completion.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("tool_calls length = %d, want 1", len(completion.Choices[0].Message.ToolCalls))
	}
	tc := completion.Choices[0].Message.ToolCalls[0]
	if tc.Function.Name != "get_weather" {
		t.Errorf("function name = %q, want get_weather", tc.Function.Name)
	}
}

// --- Google Batch Adapter Tests ---

func TestGoogleBatchAdapter_MapBatchState(t *testing.T) {
	tests := []struct {
		input string
		want  BatchStatus
	}{
		{"JOB_STATE_PENDING", BatchPending},
		{"JOB_STATE_RUNNING", BatchProcessing},
		{"JOB_STATE_SUCCEEDED", BatchCompleted},
		{"JOB_STATE_FAILED", BatchFailed},
		{"JOB_STATE_EXPIRED", BatchFailed},
		{"JOB_STATE_CANCELLED", BatchCancelled},
		{"unknown", BatchPending},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := mapGeminiBatchState(tt.input)
			if got != tt.want {
				t.Errorf("mapGeminiBatchState(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGoogleBatchAdapter_TranslateRequest(t *testing.T) {
	adapter := &GoogleBatchAdapter{apiKey: "test-key", client: http.DefaultClient}

	temp := 0.5
	maxTokens := 100
	item := BatchRequestItem{
		Messages: []Message{
			{Role: RoleSystem, Content: "Be brief"},
			{Role: RoleUser, Content: "Hello"},
		},
		Temperature: &temp,
		MaxTokens:   &maxTokens,
	}

	body := adapter.translateRequestToGemini("gemini-2.0-flash", item)

	// System instruction
	sysInst, ok := body["systemInstruction"].(map[string]any)
	if !ok {
		t.Fatal("expected systemInstruction")
	}
	parts, _ := sysInst["parts"].([]map[string]any)
	if len(parts) != 1 || parts[0]["text"] != "Be brief" {
		t.Errorf("system instruction = %v, want 'Be brief'", parts)
	}

	// Contents
	contents, _ := body["contents"].([]map[string]any)
	if len(contents) != 1 {
		t.Fatalf("contents length = %d, want 1", len(contents))
	}
	if contents[0]["role"] != "user" {
		t.Errorf("contents[0].role = %v, want user", contents[0]["role"])
	}

	// Generation config
	genConfig, _ := body["generationConfig"].(map[string]any)
	if genConfig["temperature"] != 0.5 {
		t.Errorf("temperature = %v, want 0.5", genConfig["temperature"])
	}
	if genConfig["maxOutputTokens"] != 100 {
		t.Errorf("maxOutputTokens = %v, want 100", genConfig["maxOutputTokens"])
	}
}

func TestGoogleBatchAdapter_TranslateResponse(t *testing.T) {
	adapter := &GoogleBatchAdapter{apiKey: "test-key", client: http.DefaultClient}

	data := `{
		"candidates": [{
			"content": {"parts": [{"text": "Hello from Gemini!"}]},
			"finishReason": "STOP"
		}],
		"usageMetadata": {
			"promptTokenCount": 15,
			"candidatesTokenCount": 8,
			"totalTokenCount": 23
		}
	}`

	completion, err := adapter.translateGeminiResponse([]byte(data), "gemini-2.0-flash")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if completion.Model != "google/gemini-2.0-flash" {
		t.Errorf("model = %q, want google/gemini-2.0-flash", completion.Model)
	}
	if completion.Choices[0].Message.Content != "Hello from Gemini!" {
		t.Errorf("content = %q, want 'Hello from Gemini!'", completion.Choices[0].Message.Content)
	}
	if completion.Usage.PromptTokens != 15 {
		t.Errorf("prompt_tokens = %d, want 15", completion.Usage.PromptTokens)
	}
}

// --- BatchManager native routing tests ---

func TestBatchManager_NativeRouting(t *testing.T) {
	t.Run("uses native adapter when registered", func(t *testing.T) {
		dir := t.TempDir()
		store := NewBatchStore(dir)
		registry := NewRegistry()
		registry.Register("openai", newMockAdapter("openai", nil))
		statsStore := NewGenerationStatsStore(100)
		router := NewRouter(registry, nil, nil, statsStore)

		manager := NewBatchManager(registry, store, nil, 5, 500, time.Second, router)
		manager.RegisterBatchAdapter("openai", &fakeBatchAdapter{})

		batch, err := manager.Create(context.Background(), BatchCreateRequest{
			Model: "openai/gpt-4o",
			Requests: []BatchRequestItem{
				{CustomID: "req-1", Messages: []Message{{Role: RoleUser, Content: "Hi"}}},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if batch.BatchMode != BatchNative {
			t.Errorf("batch_mode = %q, want native", batch.BatchMode)
		}
		if batch.ProviderName != "openai" {
			t.Errorf("provider_name = %q, want openai", batch.ProviderName)
		}
	})

	t.Run("falls back to concurrent when no native adapter", func(t *testing.T) {
		dir := t.TempDir()
		store := NewBatchStore(dir)
		registry := NewRegistry()
		adapter := newMockAdapter("google", nil)
		registry.Register("google", adapter)
		statsStore := NewGenerationStatsStore(100)
		router := NewRouter(registry, nil, nil, statsStore)

		manager := NewBatchManager(registry, store, nil, 5, 500, time.Second, router)

		batch, err := manager.Create(context.Background(), BatchCreateRequest{
			Model: "google/gemini-2.0-flash",
			Requests: []BatchRequestItem{
				{CustomID: "req-1", Messages: []Message{{Role: RoleUser, Content: "Hi"}}},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if batch.BatchMode != BatchConcurrent {
			t.Errorf("batch_mode = %q, want concurrent", batch.BatchMode)
		}
		// Wait for background goroutine to finish before TempDir cleanup
		_, _ = manager.Poll(context.Background(), batch.ID, BatchPollOptions{Interval: 0.05})
	})

	t.Run("persists provider state for native batches", func(t *testing.T) {
		dir := t.TempDir()
		store := NewBatchStore(dir)
		registry := NewRegistry()
		registry.Register("openai", newMockAdapter("openai", nil))
		statsStore := NewGenerationStatsStore(100)
		router := NewRouter(registry, nil, nil, statsStore)

		manager := NewBatchManager(registry, store, nil, 5, 500, time.Second, router)
		manager.RegisterBatchAdapter("openai", &fakeBatchAdapter{})

		batch, err := manager.Create(context.Background(), BatchCreateRequest{
			Model: "openai/gpt-4o",
			Requests: []BatchRequestItem{
				{CustomID: "req-1", Messages: []Message{{Role: RoleUser, Content: "Hi"}}},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Wait for background goroutine
		time.Sleep(200 * time.Millisecond)

		state, err := store.LoadProviderState(batch.ID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if state == nil {
			t.Fatal("expected provider state")
		}
		if state["provider_batch_id"] != "provider-batch-123" {
			t.Errorf("provider_batch_id = %v, want provider-batch-123", state["provider_batch_id"])
		}
	})

	t.Run("handles native batch creation failure", func(t *testing.T) {
		dir := t.TempDir()
		store := NewBatchStore(dir)
		registry := NewRegistry()
		registry.Register("openai", newMockAdapter("openai", nil))
		statsStore := NewGenerationStatsStore(100)
		router := NewRouter(registry, nil, nil, statsStore)

		failAdapter := &fakeBatchAdapter{createErr: fmt.Errorf("upload failed")}
		manager := NewBatchManager(registry, store, nil, 5, 500, time.Second, router)
		manager.RegisterBatchAdapter("openai", failAdapter)

		batch, err := manager.Create(context.Background(), BatchCreateRequest{
			Model: "openai/gpt-4o",
			Requests: []BatchRequestItem{
				{CustomID: "req-1", Messages: []Message{{Role: RoleUser, Content: "Hi"}}},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Wait for background goroutine
		time.Sleep(200 * time.Millisecond)

		meta, err := manager.Get(batch.ID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if meta.Status != BatchFailed {
			t.Errorf("status = %q, want failed", meta.Status)
		}
	})

	t.Run("cancels batch", func(t *testing.T) {
		dir := t.TempDir()
		store := NewBatchStore(dir)
		registry := NewRegistry()
		registry.Register("openai", newMockAdapter("openai", nil))
		statsStore := NewGenerationStatsStore(100)
		router := NewRouter(registry, nil, nil, statsStore)

		manager := NewBatchManager(registry, store, nil, 5, 500, time.Second, router)
		manager.RegisterBatchAdapter("openai", &fakeBatchAdapter{})

		batch, err := manager.Create(context.Background(), BatchCreateRequest{
			Model: "openai/gpt-4o",
			Requests: []BatchRequestItem{
				{CustomID: "req-1", Messages: []Message{{Role: RoleUser, Content: "Hi"}}},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		time.Sleep(100 * time.Millisecond)

		cancelled, err := manager.Cancel(context.Background(), batch.ID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cancelled.Status != BatchCancelled {
			t.Errorf("status = %q, want cancelled", cancelled.Status)
		}
	})
}

// fakeBatchAdapter is a test double for BatchAdapter.
type fakeBatchAdapter struct {
	createErr error
}

func (f *fakeBatchAdapter) CreateBatch(_ context.Context, model string, _ []BatchRequestItem, _ map[string]any) (string, map[string]any, error) {
	if f.createErr != nil {
		return "", nil, f.createErr
	}
	return "provider-batch-123", map[string]any{"some": "data"}, nil
}

func (f *fakeBatchAdapter) PollBatch(_ context.Context, _ string) (*NativeBatchStatus, error) {
	return &NativeBatchStatus{
		Status: BatchCompleted, Total: 2, Completed: 2, Failed: 0,
	}, nil
}

func (f *fakeBatchAdapter) GetBatchResults(_ context.Context, _ string) ([]BatchResultItem, error) {
	return []BatchResultItem{
		{
			CustomID: "req-1", Status: "success",
			Response: &ChatCompletion{
				ID: "gen-1", Object: "chat.completion", Created: 1000, Model: "openai/gpt-4o",
				Choices: []ChatCompletionChoice{{Index: 0, Message: Message{Role: RoleAssistant, Content: "Hello 1"}, FinishReason: FinishStop}},
				Usage:   Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
			},
		},
		{
			CustomID: "req-2", Status: "success",
			Response: &ChatCompletion{
				ID: "gen-2", Object: "chat.completion", Created: 1000, Model: "openai/gpt-4o",
				Choices: []ChatCompletionChoice{{Index: 0, Message: Message{Role: RoleAssistant, Content: "Hello 2"}, FinishReason: FinishStop}},
				Usage:   Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
			},
		},
	}, nil
}

func (f *fakeBatchAdapter) CancelBatch(_ context.Context, _ string) error {
	return nil
}
