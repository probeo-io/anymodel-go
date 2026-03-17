package anymodel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const anthropicBatchDefaultMaxTokens = 4096

// AnthropicBatchAdapter implements the BatchAdapter interface for Anthropic.
type AnthropicBatchAdapter struct {
	apiKey string
	client *http.Client
}

// NewAnthropicBatchAdapter creates a new Anthropic batch adapter.
func NewAnthropicBatchAdapter(apiKey string) *AnthropicBatchAdapter {
	return &AnthropicBatchAdapter{
		apiKey: apiKey,
		client: &http.Client{Timeout: GetDefaultHTTPTimeout()},
	}
}

func (a *AnthropicBatchAdapter) apiRequest(ctx context.Context, method, path string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(data)
	}

	url := "https://api.anthropic.com/v1" + path
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, NewError(502, fmt.Sprintf("anthropic batch request failed: %v", err), map[string]any{"provider_name": "anthropic"})
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		var parsed struct {
			Error struct{ Message string } `json:"error"`
		}
		msg := string(respBody)
		if json.Unmarshal(respBody, &parsed) == nil && parsed.Error.Message != "" {
			msg = parsed.Error.Message
		}
		code := resp.StatusCode
		if code >= 500 {
			code = 502
		}
		return nil, NewError(code, msg, map[string]any{"provider_name": "anthropic"})
	}

	return respBody, nil
}

func (a *AnthropicBatchAdapter) translateToAnthropicParams(model string, req BatchRequestItem) map[string]any {
	var userMax *int
	if req.MaxTokens != nil {
		userMax = req.MaxTokens
	} else {
		v := anthropicBatchDefaultMaxTokens
		userMax = &v
	}
	maxTokens := ResolveMaxTokens(model, req.Messages, userMax)

	params := map[string]any{
		"model":      model,
		"max_tokens": maxTokens,
	}

	// Extract system messages
	var systemParts []string
	var nonSystemMessages []map[string]any

	for _, m := range req.Messages {
		if m.Role == RoleSystem {
			if m.Content != "" {
				systemParts = append(systemParts, m.Content)
			}
			continue
		}

		msg := map[string]any{
			"role": string(m.Role),
		}

		// Map tool role to user with tool_result content block
		if m.Role == RoleTool {
			msg["role"] = "user"
			msg["content"] = []map[string]any{{
				"type":        "tool_result",
				"tool_use_id": m.ToolCallID,
				"content":     m.Content,
			}}
		} else {
			msg["content"] = m.Content
		}

		nonSystemMessages = append(nonSystemMessages, msg)
	}

	// Handle response_format - prepend JSON instruction to system
	if req.ResponseFormat != nil && (req.ResponseFormat.Type == "json_object" || req.ResponseFormat.Type == "json_schema") {
		jsonInstruction := "Respond with valid JSON only. Do not include any text outside the JSON object."
		systemParts = append([]string{jsonInstruction}, systemParts...)
	}

	if len(systemParts) > 0 {
		params["system"] = strings.Join(systemParts, "\n")
	}

	params["messages"] = nonSystemMessages

	if req.Temperature != nil {
		params["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		params["top_p"] = *req.TopP
	}
	if req.TopK != nil {
		params["top_k"] = *req.TopK
	}
	if len(req.Stop) > 0 {
		params["stop_sequences"] = req.Stop
	}

	// Map tools
	if len(req.Tools) > 0 {
		var tools []map[string]any
		for _, t := range req.Tools {
			tool := map[string]any{
				"name":        t.Function.Name,
				"description": t.Function.Description,
			}
			if t.Function.Parameters != nil {
				tool["input_schema"] = t.Function.Parameters
			} else {
				tool["input_schema"] = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			tools = append(tools, tool)
		}
		params["tools"] = tools

		// Map tool_choice
		if req.ToolChoice != nil {
			switch tc := req.ToolChoice.(type) {
			case string:
				switch tc {
				case "auto":
					params["tool_choice"] = map[string]any{"type": "auto"}
				case "required":
					params["tool_choice"] = map[string]any{"type": "any"}
				case "none":
					delete(params, "tools")
				}
			case map[string]any:
				if fn, ok := tc["function"].(map[string]any); ok {
					if name, ok := fn["name"].(string); ok {
						params["tool_choice"] = map[string]any{"type": "tool", "name": name}
					}
				}
			}
		}
	}

	return params
}

func mapAnthropicStopReason(reason string) FinishReason {
	switch reason {
	case "end_turn", "stop_sequence":
		return FinishStop
	case "max_tokens":
		return FinishLength
	case "tool_use":
		return FinishToolCalls
	default:
		return FinishStop
	}
}

func translateAnthropicBatchMessage(data []byte) (*ChatCompletion, error) {
	var msg struct {
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
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, NewError(502, "failed to parse anthropic batch response", nil)
	}

	message := Message{Role: RoleAssistant}
	var toolCalls []ToolCall

	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			message.Content += block.Text
		case "tool_use":
			toolCalls = append(toolCalls, ToolCall{
				ID:   block.ID,
				Type: "function",
				Function: ToolCallFunction{
					Name:      block.Name,
					Arguments: string(block.Input),
				},
			})
		}
	}

	if len(toolCalls) > 0 {
		message.ToolCalls = toolCalls
	}

	return &ChatCompletion{
		ID:      GenerateID("gen"),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   "anthropic/" + msg.Model,
		Choices: []ChatCompletionChoice{{
			Index:        0,
			Message:      message,
			FinishReason: mapAnthropicStopReason(msg.StopReason),
		}},
		Usage: Usage{
			PromptTokens:     msg.Usage.InputTokens,
			CompletionTokens: msg.Usage.OutputTokens,
			TotalTokens:      msg.Usage.InputTokens + msg.Usage.OutputTokens,
		},
	}, nil
}

// CreateBatch submits a batch to the Anthropic message batches API.
func (a *AnthropicBatchAdapter) CreateBatch(ctx context.Context, model string, requests []BatchRequestItem, options map[string]any) (string, map[string]any, error) {
	batchRequests := make([]map[string]any, len(requests))
	for i, req := range requests {
		customID := req.CustomID
		if customID == "" {
			customID = fmt.Sprintf("request-%d", i)
		}
		batchRequests[i] = map[string]any{
			"custom_id": customID,
			"params":    a.translateToAnthropicParams(model, req),
		}
	}

	respBody, err := a.apiRequest(ctx, "POST", "/messages/batches", map[string]any{
		"requests": batchRequests,
	})
	if err != nil {
		return "", nil, err
	}

	var result struct {
		ID        string `json:"id"`
		Type      string `json:"type"`
		CreatedAt string `json:"created_at"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", nil, NewError(502, "failed to parse anthropic batch response", map[string]any{"provider_name": "anthropic"})
	}

	return result.ID, map[string]any{
		"anthropic_type": result.Type,
		"created_at":     result.CreatedAt,
	}, nil
}

// PollBatch checks batch status.
func (a *AnthropicBatchAdapter) PollBatch(ctx context.Context, providerBatchID string) (*NativeBatchStatus, error) {
	respBody, err := a.apiRequest(ctx, "GET", "/messages/batches/"+providerBatchID, nil)
	if err != nil {
		return nil, err
	}

	var data struct {
		ProcessingStatus  string `json:"processing_status"`
		CancelInitiatedAt string `json:"cancel_initiated_at"`
		RequestCounts     struct {
			Processing int `json:"processing"`
			Succeeded  int `json:"succeeded"`
			Errored    int `json:"errored"`
			Canceled   int `json:"canceled"`
			Expired    int `json:"expired"`
		} `json:"request_counts"`
	}
	if err := json.Unmarshal(respBody, &data); err != nil {
		return nil, NewError(502, "failed to parse anthropic poll response", nil)
	}

	counts := data.RequestCounts
	total := counts.Processing + counts.Succeeded + counts.Errored + counts.Canceled + counts.Expired
	failed := counts.Errored + counts.Expired + counts.Canceled

	var status BatchStatus
	if data.ProcessingStatus == "ended" {
		if counts.Succeeded == 0 && (counts.Errored > 0 || counts.Expired > 0 || counts.Canceled > 0) {
			status = BatchFailed
		} else if data.CancelInitiatedAt != "" {
			status = BatchCancelled
		} else {
			status = BatchCompleted
		}
	} else {
		status = BatchProcessing
	}

	return &NativeBatchStatus{
		Status:    status,
		Total:     total,
		Completed: counts.Succeeded,
		Failed:    failed,
	}, nil
}

// GetBatchResults downloads batch results.
func (a *AnthropicBatchAdapter) GetBatchResults(ctx context.Context, providerBatchID string) ([]BatchResultItem, error) {
	respBody, err := a.apiRequest(ctx, "GET", "/messages/batches/"+providerBatchID+"/results", nil)
	if err != nil {
		return nil, err
	}

	var results []BatchResultItem

	for _, line := range strings.Split(strings.TrimSpace(string(respBody)), "\n") {
		if line == "" {
			continue
		}

		var item struct {
			CustomID string `json:"custom_id"`
			Result   struct {
				Type    string          `json:"type"`
				Message json.RawMessage `json:"message"`
				Error   *struct {
					Message string `json:"message"`
				} `json:"error"`
			} `json:"result"`
		}
		if json.Unmarshal([]byte(line), &item) != nil {
			continue
		}

		if item.Result.Type == "succeeded" {
			completion, err := translateAnthropicBatchMessage(item.Result.Message)
			if err != nil {
				results = append(results, BatchResultItem{
					CustomID: item.CustomID, Status: "error",
					Error: &BatchError{Code: 502, Message: err.Error()},
				})
				continue
			}
			results = append(results, BatchResultItem{
				CustomID: item.CustomID, Status: "success", Response: completion,
			})
		} else {
			errorType := item.Result.Type
			if errorType == "" {
				errorType = "unknown"
			}
			msg := fmt.Sprintf("Batch item %s", errorType)
			if item.Result.Error != nil && item.Result.Error.Message != "" {
				msg = item.Result.Error.Message
			}
			code := 500
			if errorType == "expired" {
				code = 408
			}
			results = append(results, BatchResultItem{
				CustomID: item.CustomID, Status: "error",
				Error: &BatchError{Code: code, Message: msg},
			})
		}
	}

	return results, nil
}

// CancelBatch cancels a batch.
func (a *AnthropicBatchAdapter) CancelBatch(ctx context.Context, providerBatchID string) error {
	_, err := a.apiRequest(ctx, "POST", "/messages/batches/"+providerBatchID+"/cancel", nil)
	return err
}
