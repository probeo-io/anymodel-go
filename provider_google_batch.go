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

// GoogleBatchAdapter implements the BatchAdapter interface for Google Gemini.
type GoogleBatchAdapter struct {
	apiKey string
	client *http.Client
}

// NewGoogleBatchAdapter creates a new Google Gemini batch adapter.
func NewGoogleBatchAdapter(apiKey string) *GoogleBatchAdapter {
	return &GoogleBatchAdapter{
		apiKey: apiKey,
		client: &http.Client{Timeout: GetDefaultHTTPTimeout()},
	}
}

func (a *GoogleBatchAdapter) apiRequest(ctx context.Context, method, path string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(data)
	}

	url := geminiBaseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", a.apiKey)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, NewError(502, fmt.Sprintf("google batch request failed: %v", err), map[string]any{"provider_name": "google"})
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
		return nil, NewError(code, msg, map[string]any{"provider_name": "google"})
	}

	return respBody, nil
}

func (a *GoogleBatchAdapter) translateRequestToGemini(model string, item BatchRequestItem) map[string]any {
	body := map[string]any{}

	// Separate system and non-system messages
	var systemText string
	var contents []map[string]any

	for _, m := range item.Messages {
		switch m.Role {
		case RoleSystem:
			if systemText != "" {
				systemText += "\n"
			}
			systemText += m.Content
		case RoleUser:
			contents = append(contents, map[string]any{
				"role": "user", "parts": []map[string]any{{"text": m.Content}},
			})
		case RoleAssistant:
			contents = append(contents, map[string]any{
				"role": "model", "parts": []map[string]any{{"text": m.Content}},
			})
		}
	}

	if systemText != "" {
		body["systemInstruction"] = map[string]any{"parts": []map[string]any{{"text": systemText}}}
	}
	body["contents"] = contents

	// Generation config
	genConfig := map[string]any{}
	if item.Temperature != nil {
		genConfig["temperature"] = *item.Temperature
	}
	if item.MaxTokens != nil {
		genConfig["maxOutputTokens"] = *item.MaxTokens
	} else {
		genConfig["maxOutputTokens"] = ResolveMaxTokens("google/"+model, item.Messages, nil)
	}
	if item.TopP != nil {
		genConfig["topP"] = *item.TopP
	}
	if item.TopK != nil {
		genConfig["topK"] = *item.TopK
	}
	if len(item.Stop) > 0 {
		genConfig["stopSequences"] = item.Stop
	}
	if item.ResponseFormat != nil && item.ResponseFormat.Type == "json_object" {
		genConfig["responseMimeType"] = "application/json"
	}
	if len(genConfig) > 0 {
		body["generationConfig"] = genConfig
	}

	// Tools
	if len(item.Tools) > 0 {
		var decls []map[string]any
		for _, t := range item.Tools {
			decls = append(decls, map[string]any{
				"name":        t.Function.Name,
				"description": t.Function.Description,
				"parameters":  t.Function.Parameters,
			})
		}
		body["tools"] = []map[string]any{{"functionDeclarations": decls}}
	}

	return body
}

func (a *GoogleBatchAdapter) translateGeminiResponse(data []byte, model string) (*ChatCompletion, error) {
	// Reuse the same response structure as the regular adapter
	var raw struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         string `json:"text,omitempty"`
					FunctionCall *struct {
						Name string         `json:"name"`
						Args map[string]any `json:"args"`
					} `json:"functionCall,omitempty"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, NewError(502, "failed to parse google batch response", nil)
	}

	msg := Message{Role: RoleAssistant}
	var toolCalls []ToolCall
	finishReason := FinishStop

	if len(raw.Candidates) > 0 {
		for _, part := range raw.Candidates[0].Content.Parts {
			if part.FunctionCall != nil {
				argsJSON, _ := json.Marshal(part.FunctionCall.Args)
				toolCalls = append(toolCalls, ToolCall{
					ID: GenerateID("call"), Type: "function",
					Function: ToolCallFunction{Name: part.FunctionCall.Name, Arguments: string(argsJSON)},
				})
			} else {
				msg.Content += part.Text
			}
		}
		switch raw.Candidates[0].FinishReason {
		case "MAX_TOKENS":
			finishReason = FinishLength
		case "SAFETY", "RECITATION":
			finishReason = FinishContentFilter
		}
	}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
		finishReason = FinishToolCalls
	}

	return &ChatCompletion{
		ID: GenerateID("gen"), Object: "chat.completion",
		Created: time.Now().Unix(), Model: "google/" + model,
		Choices: []ChatCompletionChoice{{Index: 0, Message: msg, FinishReason: finishReason}},
		Usage: Usage{
			PromptTokens:     raw.UsageMetadata.PromptTokenCount,
			CompletionTokens: raw.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      raw.UsageMetadata.TotalTokenCount,
		},
	}, nil
}

func mapGeminiBatchState(state string) BatchStatus {
	switch state {
	case "JOB_STATE_PENDING":
		return BatchPending
	case "JOB_STATE_RUNNING":
		return BatchProcessing
	case "JOB_STATE_SUCCEEDED":
		return BatchCompleted
	case "JOB_STATE_FAILED", "JOB_STATE_EXPIRED":
		return BatchFailed
	case "JOB_STATE_CANCELLED":
		return BatchCancelled
	default:
		return BatchPending
	}
}

// CreateBatch submits a batch to Gemini's batchGenerateContent endpoint.
func (a *GoogleBatchAdapter) CreateBatch(ctx context.Context, model string, requests []BatchRequestItem, options map[string]any) (string, map[string]any, error) {
	batchRequests := make([]map[string]any, len(requests))
	for i, req := range requests {
		customID := req.CustomID
		if customID == "" {
			customID = fmt.Sprintf("request-%d", i)
		}
		batchRequests[i] = map[string]any{
			"request":  a.translateRequestToGemini(model, req),
			"metadata": map[string]any{"key": customID},
		}
	}

	respBody, err := a.apiRequest(ctx, "POST", fmt.Sprintf("/models/%s:batchGenerateContent", model), map[string]any{
		"batch": map[string]any{
			"display_name": fmt.Sprintf("anymodel-batch-%d", time.Now().Unix()),
			"input_config": map[string]any{
				"requests": map[string]any{
					"requests": batchRequests,
				},
			},
		},
	})
	if err != nil {
		return "", nil, err
	}

	var result struct {
		Name  string `json:"name"`
		Batch struct {
			Name string `json:"name"`
		} `json:"batch"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", nil, NewError(502, "failed to parse google batch response", map[string]any{"provider_name": "google"})
	}

	batchName := result.Name
	if batchName == "" {
		batchName = result.Batch.Name
	}
	if batchName == "" {
		return "", nil, NewError(502, "no batch name in Google response", map[string]any{"provider_name": "google"})
	}

	return batchName, map[string]any{
		"model":          model,
		"total_requests": len(requests),
	}, nil
}

// PollBatch checks batch status.
func (a *GoogleBatchAdapter) PollBatch(ctx context.Context, providerBatchID string) (*NativeBatchStatus, error) {
	respBody, err := a.apiRequest(ctx, "GET", "/"+providerBatchID, nil)
	if err != nil {
		return nil, err
	}

	var data struct {
		State          string `json:"state"`
		TotalCount     int    `json:"totalCount"`
		SucceededCount int    `json:"succeededCount"`
		FailedCount    int    `json:"failedCount"`
		Metadata       struct {
			TotalRequests int `json:"total_requests"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(respBody, &data); err != nil {
		return nil, NewError(502, "failed to parse google poll response", nil)
	}

	state := data.State
	if state == "" {
		state = "JOB_STATE_PENDING"
	}

	total := data.TotalCount
	if total == 0 {
		total = data.Metadata.TotalRequests
	}
	if total == 0 {
		total = data.SucceededCount + data.FailedCount
	}

	return &NativeBatchStatus{
		Status:    mapGeminiBatchState(state),
		Total:     total,
		Completed: data.SucceededCount,
		Failed:    data.FailedCount,
	}, nil
}

// GetBatchResults downloads batch results.
func (a *GoogleBatchAdapter) GetBatchResults(ctx context.Context, providerBatchID string) ([]BatchResultItem, error) {
	respBody, err := a.apiRequest(ctx, "GET", "/"+providerBatchID, nil)
	if err != nil {
		return nil, err
	}

	var batchData struct {
		Response struct {
			InlinedResponses []json.RawMessage `json:"inlinedResponses"`
			ResponsesFileName string           `json:"responsesFileName"`
		} `json:"response"`
		OutputConfig struct {
			FileName string `json:"file_name"`
		} `json:"outputConfig"`
		Metadata struct {
			Model string `json:"model"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(respBody, &batchData); err != nil {
		return nil, NewError(502, "failed to parse google batch data", nil)
	}

	model := batchData.Metadata.Model
	if model == "" {
		model = "unknown"
	}

	var results []BatchResultItem

	// Check for inline responses
	if len(batchData.Response.InlinedResponses) > 0 {
		for i, raw := range batchData.Response.InlinedResponses {
			var item struct {
				Response json.RawMessage `json:"response"`
				Error    *struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
				Metadata struct {
					Key string `json:"key"`
				} `json:"metadata"`
			}
			if json.Unmarshal(raw, &item) != nil {
				continue
			}

			customID := item.Metadata.Key
			if customID == "" {
				customID = fmt.Sprintf("request-%d", i)
			}

			if item.Response != nil {
				completion, err := a.translateGeminiResponse(item.Response, model)
				if err != nil {
					results = append(results, BatchResultItem{
						CustomID: customID, Status: "error",
						Error: &BatchError{Code: 502, Message: err.Error()},
					})
					continue
				}
				results = append(results, BatchResultItem{
					CustomID: customID, Status: "success", Response: completion,
				})
			} else if item.Error != nil {
				code := item.Error.Code
				if code == 0 {
					code = 500
				}
				msg := item.Error.Message
				if msg == "" {
					msg = "Batch item failed"
				}
				results = append(results, BatchResultItem{
					CustomID: customID, Status: "error",
					Error: &BatchError{Code: code, Message: msg},
				})
			}
		}
		return results, nil
	}

	// Check for file-based results
	responsesFile := batchData.Response.ResponsesFileName
	if responsesFile == "" {
		responsesFile = batchData.OutputConfig.FileName
	}

	if responsesFile != "" {
		downloadURL := fmt.Sprintf("%s/%s:download?alt=media", geminiBaseURL, responsesFile)
		req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("x-goog-api-key", a.apiKey)

		resp, err := a.client.Do(req)
		if err != nil {
			return nil, NewError(502, "failed to download batch results file", map[string]any{"provider_name": "google"})
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			return nil, NewError(502, "failed to download batch results file", map[string]any{"provider_name": "google"})
		}

		body, _ := io.ReadAll(resp.Body)
		for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
			if line == "" {
				continue
			}
			var item struct {
				Key      string          `json:"key"`
				Response json.RawMessage `json:"response"`
				Error    *struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
				Metadata struct {
					Key string `json:"key"`
				} `json:"metadata"`
			}
			if json.Unmarshal([]byte(line), &item) != nil {
				continue
			}

			customID := item.Key
			if customID == "" {
				customID = item.Metadata.Key
			}
			if customID == "" {
				customID = fmt.Sprintf("request-%d", len(results))
			}

			if item.Response != nil {
				completion, err := a.translateGeminiResponse(item.Response, model)
				if err != nil {
					results = append(results, BatchResultItem{
						CustomID: customID, Status: "error",
						Error: &BatchError{Code: 502, Message: err.Error()},
					})
					continue
				}
				results = append(results, BatchResultItem{
					CustomID: customID, Status: "success", Response: completion,
				})
			} else if item.Error != nil {
				code := item.Error.Code
				if code == 0 {
					code = 500
				}
				msg := item.Error.Message
				if msg == "" {
					msg = "Batch item failed"
				}
				results = append(results, BatchResultItem{
					CustomID: customID, Status: "error",
					Error: &BatchError{Code: code, Message: msg},
				})
			}
		}
	}

	return results, nil
}

// CancelBatch cancels a batch.
func (a *GoogleBatchAdapter) CancelBatch(ctx context.Context, providerBatchID string) error {
	_, err := a.apiRequest(ctx, "POST", "/"+providerBatchID+":cancel", nil)
	return err
}
