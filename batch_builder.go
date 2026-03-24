package anymodel

import (
	"context"
	"fmt"
)

// BatchBuilderConfig configures a BatchBuilder.
type BatchBuilderConfig struct {
	Model       string   `json:"model"`
	System      string   `json:"system,omitempty"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	TopK        *int     `json:"top_k,omitempty"`
	Stop        []string `json:"stop,omitempty"`
	ServiceTier string   `json:"service_tier,omitempty"`
	BatchMode   string   `json:"batch_mode,omitempty"` // "native", "concurrent", or "" for auto
}

// BatchBuilderSuccessItem represents a successfully completed batch item.
type BatchBuilderSuccessItem struct {
	ID      string
	Content string
	Usage   struct {
		PromptTokens     int
		CompletionTokens int
	}
	Cost float64
	Raw  *ChatCompletion
}

// BatchBuilderFailedItem represents a failed batch item.
type BatchBuilderFailedItem struct {
	ID     string
	Prompt interface{} // string or []Message
	Error  struct {
		Code     int
		Message  string
		Provider string
	}
	Retryable bool
}

// BatchBuilderResults holds the transformed results from a batch.
type BatchBuilderResults struct {
	ID        string
	Succeeded []BatchBuilderSuccessItem
	Failed    []BatchBuilderFailedItem
	Usage     BatchUsageSummary
}

// BatchBuilder provides a fluent API for building and executing batches.
type BatchBuilder struct {
	batchID   string
	config    BatchBuilderConfig
	store     *BatchStore
	manager   *BatchManager
	count     int
	submitted bool
}

// NewBatchBuilder creates a new BatchBuilder.
func NewBatchBuilder(config BatchBuilderConfig, store *BatchStore, manager *BatchManager) *BatchBuilder {
	return &BatchBuilder{
		batchID: GenerateID("batch"),
		config:  config,
		store:   store,
		manager: manager,
	}
}

// ID returns the batch ID.
func (b *BatchBuilder) ID() string {
	return b.batchID
}

// Size returns the number of items added.
func (b *BatchBuilder) Size() int {
	return b.count
}

// Add adds a string prompt to the batch.
func (b *BatchBuilder) Add(content string) *BatchBuilder {
	var messages []Message

	if b.config.System != "" {
		messages = append(messages, Message{
			Role:    RoleSystem,
			Content: b.config.System,
		})
	}

	messages = append(messages, Message{
		Role:    RoleUser,
		Content: content,
	})

	return b.AddMessages(messages)
}

// AddMessages adds a multi-turn message sequence to the batch.
func (b *BatchBuilder) AddMessages(messages []Message) *BatchBuilder {
	customID := fmt.Sprintf("req-%06d", b.count)

	item := BatchRequestItem{
		CustomID:    customID,
		Messages:    messages,
		MaxTokens:   b.config.MaxTokens,
		Temperature: b.config.Temperature,
		TopP:        b.config.TopP,
		TopK:        b.config.TopK,
		Stop:        b.config.Stop,
		ServiceTier: b.config.ServiceTier,
	}

	_ = b.store.AppendRequest(b.batchID, item)
	b.count++

	return b
}

// Submit submits the batch for processing.
func (b *BatchBuilder) Submit(ctx context.Context) (string, error) {
	if b.submitted {
		return b.batchID, nil
	}

	// Read all requests back from disk
	ch := make(chan BatchRequestItem, b.count)
	if err := b.store.StreamRequests(b.batchID, ch); err != nil {
		return "", err
	}
	var requests []BatchRequestItem
	for item := range ch {
		requests = append(requests, item)
	}

	req := BatchCreateRequest{
		Model:     b.config.Model,
		Requests:  requests,
		BatchMode: b.config.BatchMode,
	}

	batch, err := b.manager.Create(ctx, req)
	if err != nil {
		return "", err
	}

	b.batchID = batch.ID
	b.submitted = true
	return b.batchID, nil
}

// Poll waits for the batch to complete and returns transformed results.
func (b *BatchBuilder) Poll(ctx context.Context, opts BatchPollOptions) (*BatchBuilderResults, error) {
	results, err := b.manager.Poll(ctx, b.batchID, opts)
	if err != nil {
		return nil, err
	}
	return b.transformResults(results)
}

// GetResults returns transformed results if the batch is complete.
func (b *BatchBuilder) GetResults() (*BatchBuilderResults, error) {
	results, err := b.manager.Results(b.batchID)
	if err != nil {
		return nil, err
	}
	return b.transformResults(results)
}

// Retry creates a new BatchBuilder pre-loaded with the failed items.
func (b *BatchBuilder) Retry(failed []BatchBuilderFailedItem) *BatchBuilder {
	nb := NewBatchBuilder(b.config, b.store, b.manager)

	for _, f := range failed {
		switch prompt := f.Prompt.(type) {
		case string:
			nb.Add(prompt)
		case []Message:
			nb.AddMessages(prompt)
		case []interface{}:
			// Handle JSON-deserialized message arrays
			var msgs []Message
			for _, m := range prompt {
				if mm, ok := m.(map[string]interface{}); ok {
					msg := Message{}
					if role, ok := mm["role"].(string); ok {
						msg.Role = Role(role)
					}
					if content, ok := mm["content"].(string); ok {
						msg.Content = content
					}
					msgs = append(msgs, msg)
				}
			}
			if len(msgs) > 0 {
				nb.AddMessages(msgs)
			}
		}
	}

	return nb
}

// Cancel cancels the batch.
func (b *BatchBuilder) Cancel(ctx context.Context) error {
	_, err := b.manager.Cancel(ctx, b.batchID)
	return err
}

// transformResults converts BatchResults into BatchBuilderResults.
func (b *BatchBuilder) transformResults(results *BatchResults) (*BatchBuilderResults, error) {
	// Load original prompts for failed items
	promptMap := make(map[string]interface{})
	ch := make(chan BatchRequestItem, 100)
	if err := b.store.StreamRequests(b.batchID, ch); err == nil {
		for item := range ch {
			// Store original prompt: if only one user message (possibly with system), store as string
			var userMessages []Message
			for _, m := range item.Messages {
				if m.Role != RoleSystem {
					userMessages = append(userMessages, m)
				}
			}
			if len(userMessages) == 1 && userMessages[0].Role == RoleUser {
				promptMap[item.CustomID] = userMessages[0].Content
			} else {
				promptMap[item.CustomID] = item.Messages
			}
		}
	}

	out := &BatchBuilderResults{
		ID:    results.ID,
		Usage: results.UsageSummary,
	}

	retryableCodes := map[int]bool{
		408: true, 429: true, 500: true, 502: true, 503: true, 529: true,
	}

	for _, r := range results.Results {
		if r.Status == "success" && r.Response != nil {
			item := BatchBuilderSuccessItem{
				ID:  r.CustomID,
				Raw: r.Response,
			}
			if len(r.Response.Choices) > 0 {
				item.Content = r.Response.Choices[0].Message.Content
			}
			item.Usage.PromptTokens = r.Response.Usage.PromptTokens
			item.Usage.CompletionTokens = r.Response.Usage.CompletionTokens
			item.Cost = CalculateCost(b.config.Model, r.Response.Usage.PromptTokens, r.Response.Usage.CompletionTokens)
			out.Succeeded = append(out.Succeeded, item)
		} else {
			item := BatchBuilderFailedItem{
				ID:    r.CustomID,
				Prompt: promptMap[r.CustomID],
			}
			if r.Error != nil {
				item.Error.Code = r.Error.Code
				item.Error.Message = r.Error.Message
				item.Retryable = retryableCodes[r.Error.Code]
			}
			out.Failed = append(out.Failed, item)
		}
	}

	return out, nil
}
