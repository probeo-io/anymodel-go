package anymodel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

var anthropicSupportedParams = map[string]bool{
	"temperature": true, "max_tokens": true, "top_p": true, "top_k": true,
	"stop": true, "stream": true, "tools": true, "tool_choice": true,
	"response_format": true,
}

const anthropicAPIVersion = "2023-06-01"

// AnthropicAdapter implements the Adapter interface for Anthropic.
type AnthropicAdapter struct {
	apiKey string
	client *http.Client
}

// NewAnthropicAdapter creates an Anthropic provider adapter.
func NewAnthropicAdapter(apiKey string) *AnthropicAdapter {
	return &AnthropicAdapter{apiKey: apiKey, client: &http.Client{Timeout: GetDefaultHTTPTimeout()}}
}

func (a *AnthropicAdapter) Name() string                        { return "anthropic" }
func (a *AnthropicAdapter) SupportsParameter(param string) bool { return anthropicSupportedParams[param] }
func (a *AnthropicAdapter) SupportsBatch() bool                 { return true }

func (a *AnthropicAdapter) extractSystem(messages []Message) (string, []Message) {
	var system string
	var filtered []Message
	for _, m := range messages {
		if m.Role == RoleSystem {
			if system != "" {
				system += "\n\n"
			}
			system += m.Content
		} else {
			filtered = append(filtered, m)
		}
	}
	return system, filtered
}

func (a *AnthropicAdapter) translateMessages(messages []Message) []map[string]any {
	var result []map[string]any
	for _, m := range messages {
		msg := map[string]any{"role": string(m.Role)}
		if m.Role == RoleTool {
			msg["role"] = "user"
			msg["content"] = []map[string]any{{
				"type": "tool_result", "tool_use_id": m.ToolCallID, "content": m.Content,
			}}
		} else if len(m.ToolCalls) > 0 {
			var content []map[string]any
			if m.Content != "" {
				content = append(content, map[string]any{"type": "text", "text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				var args any
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
					args = map[string]any{}
				}
				content = append(content, map[string]any{
					"type": "tool_use", "id": tc.ID, "name": tc.Function.Name, "input": args,
				})
			}
			msg["content"] = content
		} else {
			msg["content"] = m.Content
		}
		result = append(result, msg)
	}
	return result
}

func (a *AnthropicAdapter) translateTools(tools []Tool) []map[string]any {
	var result []map[string]any
	for _, t := range tools {
		result = append(result, map[string]any{
			"name": t.Function.Name, "description": t.Function.Description,
			"input_schema": t.Function.Parameters,
		})
	}
	return result
}

func (a *AnthropicAdapter) translateToolChoice(choice any) any {
	switch v := choice.(type) {
	case string:
		switch v {
		case "auto":
			return map[string]any{"type": "auto"}
		case "required":
			return map[string]any{"type": "any"}
		}
	case map[string]any:
		if fn, ok := v["function"].(map[string]any); ok {
			return map[string]any{"type": "tool", "name": fn["name"]}
		}
	}
	return nil
}

func (a *AnthropicAdapter) buildBody(req ChatCompletionRequest) map[string]any {
	system, messages := a.extractSystem(req.Messages)
	body := map[string]any{"model": req.Model, "messages": a.translateMessages(messages)}
	if system != "" {
		body["system"] = system
	}
	maxTokens := 4096
	if req.MaxTokens != nil {
		maxTokens = *req.MaxTokens
	}
	body["max_tokens"] = maxTokens
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		body["top_p"] = *req.TopP
	}
	if req.TopK != nil {
		body["top_k"] = *req.TopK
	}
	if len(req.Stop) > 0 {
		body["stop_sequences"] = req.Stop
	}
	if req.Stream {
		body["stream"] = true
	}
	if len(req.Tools) > 0 {
		body["tools"] = a.translateTools(req.Tools)
		if req.ToolChoice != nil {
			if tc := a.translateToolChoice(req.ToolChoice); tc != nil {
				body["tool_choice"] = tc
			}
		}
	}
	return body
}

func (a *AnthropicAdapter) doRequest(ctx context.Context, body map[string]any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)
	return a.client.Do(httpReq)
}

func (a *AnthropicAdapter) mapFinishReason(sr string) FinishReason {
	switch sr {
	case "end_turn":
		return FinishStop
	case "max_tokens":
		return FinishLength
	case "tool_use":
		return FinishToolCalls
	default:
		return FinishStop
	}
}

func (a *AnthropicAdapter) mapError(resp *http.Response, body []byte) error {
	var parsed struct {
		Error struct{ Message string } `json:"error"`
	}
	msg := string(body)
	if json.Unmarshal(body, &parsed) == nil && parsed.Error.Message != "" {
		msg = parsed.Error.Message
	}
	code := resp.StatusCode
	switch {
	case code == 401 || code == 403:
		return NewError(401, msg, map[string]any{"provider_name": "anthropic"})
	case code == 429:
		return NewError(429, msg, map[string]any{"provider_name": "anthropic"})
	case code == 529 || code >= 500:
		return NewError(502, msg, map[string]any{"provider_name": "anthropic"})
	default:
		return NewError(code, msg, map[string]any{"provider_name": "anthropic"})
	}
}

func (a *AnthropicAdapter) translateResponse(respBody []byte) (*ChatCompletion, error) {
	var raw struct {
		ID         string `json:"id"`
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text,omitempty"`
			ID    string          `json:"id,omitempty"`
			Name  string          `json:"name,omitempty"`
			Input json.RawMessage `json:"input,omitempty"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, NewError(502, "failed to parse anthropic response", nil)
	}

	msg := Message{Role: RoleAssistant}
	var toolCalls []ToolCall
	for _, block := range raw.Content {
		switch block.Type {
		case "text":
			msg.Content = block.Text
		case "tool_use":
			toolCalls = append(toolCalls, ToolCall{
				ID: block.ID, Type: "function",
				Function: ToolCallFunction{Name: block.Name, Arguments: string(block.Input)},
			})
		}
	}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}

	return &ChatCompletion{
		ID: "gen-" + raw.ID, Object: "chat.completion",
		Created: time.Now().Unix(), Model: "anthropic/" + raw.Model,
		Choices: []ChatCompletionChoice{{
			Index: 0, Message: msg, FinishReason: a.mapFinishReason(raw.StopReason),
		}},
		Usage: Usage{
			PromptTokens: raw.Usage.InputTokens, CompletionTokens: raw.Usage.OutputTokens,
			TotalTokens: raw.Usage.InputTokens + raw.Usage.OutputTokens,
		},
	}, nil
}

// extractAnthropicRateLimitHeaders extracts and normalizes Anthropic rate-limit headers.
func extractAnthropicRateLimitHeaders(resp *http.Response) map[string]string {
	headers := make(map[string]string)
	mapping := map[string]string{
		"anthropic-ratelimit-requests-remaining": "x-ratelimit-remaining-requests",
		"anthropic-ratelimit-tokens-remaining":   "x-ratelimit-remaining-tokens",
		"retry-after":                            "retry-after",
	}
	for src, dst := range mapping {
		if v := resp.Header.Get(src); v != "" {
			headers[dst] = v
		}
	}
	return headers
}

// SendRequestWithMeta sends a request and returns the completion with response metadata.
func (a *AnthropicAdapter) SendRequestWithMeta(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionWithMeta, error) {
	body := a.buildBody(req)
	delete(body, "stream")

	resp, err := a.doRequest(ctx, body)
	if err != nil {
		return nil, NewError(502, fmt.Sprintf("anthropic request failed: %v", err), map[string]any{"provider_name": "anthropic"})
	}
	defer resp.Body.Close()

	meta := ResponseMeta{Headers: extractAnthropicRateLimitHeaders(resp)}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, NewError(502, "failed to read response", map[string]any{"provider_name": "anthropic"})
	}
	if resp.StatusCode != 200 {
		return nil, a.mapError(resp, respBody)
	}
	completion, err := a.translateResponse(respBody)
	if err != nil {
		return nil, err
	}
	return &ChatCompletionWithMeta{Completion: completion, Meta: meta}, nil
}

func (a *AnthropicAdapter) SendRequest(ctx context.Context, req ChatCompletionRequest) (*ChatCompletion, error) {
	body := a.buildBody(req)
	delete(body, "stream")

	resp, err := a.doRequest(ctx, body)
	if err != nil {
		return nil, NewError(502, fmt.Sprintf("anthropic request failed: %v", err), map[string]any{"provider_name": "anthropic"})
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, NewError(502, "failed to read response", map[string]any{"provider_name": "anthropic"})
	}
	if resp.StatusCode != 200 {
		return nil, a.mapError(resp, respBody)
	}
	return a.translateResponse(respBody)
}

func (a *AnthropicAdapter) SendStreamingRequest(ctx context.Context, req ChatCompletionRequest) (<-chan ChatCompletionChunk, <-chan error) {
	chunkCh := make(chan ChatCompletionChunk, 32)
	errCh := make(chan error, 1)

	go func() {
		defer close(chunkCh)
		defer close(errCh)

		body := a.buildBody(req)
		body["stream"] = true

		resp, err := a.doRequest(ctx, body)
		if err != nil {
			errCh <- NewError(502, fmt.Sprintf("anthropic stream failed: %v", err), map[string]any{"provider_name": "anthropic"})
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			respBody, _ := io.ReadAll(resp.Body)
			errCh <- a.mapError(resp, respBody)
			return
		}

		var genID, model string
		for event := range ParseSSE(resp.Body) {
			switch event.Event {
			case "message_start":
				var ms struct {
					Message struct {
						ID    string `json:"id"`
						Model string `json:"model"`
					} `json:"message"`
				}
				if json.Unmarshal([]byte(event.Data), &ms) == nil {
					genID = "gen-" + ms.Message.ID
					model = "anthropic/" + ms.Message.Model
				}
			case "content_block_delta":
				var delta struct {
					Delta struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"delta"`
				}
				if json.Unmarshal([]byte(event.Data), &delta) == nil && delta.Delta.Type == "text_delta" {
					chunkCh <- ChatCompletionChunk{
						ID: genID, Object: "chat.completion.chunk",
						Created: time.Now().Unix(), Model: model,
						Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Content: delta.Delta.Text}}},
					}
				}
			case "message_delta":
				var md struct {
					Delta struct{ StopReason string `json:"stop_reason"` } `json:"delta"`
				}
				if json.Unmarshal([]byte(event.Data), &md) == nil {
					fr := a.mapFinishReason(md.Delta.StopReason)
					chunkCh <- ChatCompletionChunk{
						ID: genID, Object: "chat.completion.chunk",
						Created: time.Now().Unix(), Model: model,
						Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{}, FinishReason: &fr}},
					}
				}
			}
		}
	}()

	return chunkCh, errCh
}

func (a *AnthropicAdapter) ListModels(ctx context.Context) ([]ModelInfo, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", "https://api.anthropic.com/v1/models", nil)
	if err != nil {
		return anthropicFallbackModels(), nil
	}
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return anthropicFallbackModels(), nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return anthropicFallbackModels(), nil
	}

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &result) != nil {
		return anthropicFallbackModels(), nil
	}
	var models []ModelInfo
	for _, m := range result.Data {
		models = append(models, ModelInfo{ID: "anthropic/" + m.ID, Name: m.DisplayName})
	}
	if len(models) == 0 {
		return anthropicFallbackModels(), nil
	}
	return models, nil
}

func anthropicFallbackModels() []ModelInfo {
	ids := []string{"claude-opus-4-6", "claude-sonnet-4-6", "claude-haiku-4-5",
		"claude-3-5-sonnet-20241022", "claude-3-5-haiku-20241022"}
	models := make([]ModelInfo, len(ids))
	for i, id := range ids {
		models[i] = ModelInfo{ID: "anthropic/" + id, Name: id}
	}
	return models
}
