package anymodel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// OpenAIBatchAdapter implements the BatchAdapter interface for OpenAI.
type OpenAIBatchAdapter struct {
	apiKey string
	client *http.Client
}

// NewOpenAIBatchAdapter creates a new OpenAI batch adapter.
func NewOpenAIBatchAdapter(apiKey string) *OpenAIBatchAdapter {
	return &OpenAIBatchAdapter{
		apiKey: apiKey,
		client: &http.Client{Timeout: GetDefaultHTTPTimeout()},
	}
}

func (a *OpenAIBatchAdapter) apiRequest(ctx context.Context, method, path string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(data)
	}

	url := "https://api.openai.com/v1" + path
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+a.apiKey)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, NewError(502, fmt.Sprintf("openai batch request failed: %v", err), map[string]any{"provider_name": "openai"})
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
		return nil, NewError(code, msg, map[string]any{"provider_name": "openai"})
	}

	return respBody, nil
}

func (a *OpenAIBatchAdapter) apiRequestRaw(ctx context.Context, method, url string, body io.Reader, contentType string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Authorization", "Bearer "+a.apiKey)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, NewError(502, fmt.Sprintf("openai batch request failed: %v", err), map[string]any{"provider_name": "openai"})
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
		return nil, NewError(code, msg, map[string]any{"provider_name": "openai"})
	}

	return respBody, nil
}

func (a *OpenAIBatchAdapter) buildJSONL(model string, requests []BatchRequestItem) string {
	var lines []string
	for _, req := range requests {
		body := map[string]any{
			"model":    model,
			"messages": req.Messages,
		}
		if req.MaxTokens != nil {
			body["max_tokens"] = *req.MaxTokens
		} else {
			body["max_tokens"] = ResolveMaxTokens(model, req.Messages, nil)
		}
		if req.Temperature != nil {
			body["temperature"] = *req.Temperature
		}
		if req.TopP != nil {
			body["top_p"] = *req.TopP
		}
		if len(req.Stop) > 0 {
			body["stop"] = req.Stop
		}
		if req.ResponseFormat != nil {
			body["response_format"] = req.ResponseFormat
		}
		if len(req.Tools) > 0 {
			body["tools"] = req.Tools
		}
		if req.ToolChoice != nil {
			body["tool_choice"] = req.ToolChoice
		}

		line := map[string]any{
			"custom_id": req.CustomID,
			"method":    "POST",
			"url":       "/v1/chat/completions",
			"body":      body,
		}
		data, _ := json.Marshal(line)
		lines = append(lines, string(data))
	}
	return strings.Join(lines, "\n")
}

func rePrefixID(id string) string {
	if id != "" && strings.HasPrefix(id, "chatcmpl-") {
		return "gen-" + id[9:]
	}
	if strings.HasPrefix(id, "gen-") {
		return id
	}
	return "gen-" + id
}

func translateOpenAIBatchResponse(data []byte) (*ChatCompletion, error) {
	var body struct {
		ID      string                 `json:"id"`
		Created int64                  `json:"created"`
		Model   string                 `json:"model"`
		Choices []ChatCompletionChoice `json:"choices"`
		Usage   Usage                  `json:"usage"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, NewError(502, "failed to parse openai batch response", nil)
	}

	id := body.ID
	if id == "" {
		id = GenerateID("gen")
	} else {
		id = rePrefixID(id)
	}

	created := body.Created
	if created == 0 {
		created = time.Now().Unix()
	}

	return &ChatCompletion{
		ID:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   "openai/" + body.Model,
		Choices: body.Choices,
		Usage:   body.Usage,
	}, nil
}

func mapOpenAIBatchStatus(status string) BatchStatus {
	switch status {
	case "validating", "finalizing", "in_progress":
		return BatchProcessing
	case "completed":
		return BatchCompleted
	case "failed", "expired":
		return BatchFailed
	case "cancelled", "cancelling":
		return BatchCancelled
	default:
		return BatchPending
	}
}

// CreateBatch submits a batch to the OpenAI batch API.
func (a *OpenAIBatchAdapter) CreateBatch(ctx context.Context, model string, requests []BatchRequestItem, options map[string]any) (string, map[string]any, error) {
	// 1. Build JSONL content
	jsonlContent := a.buildJSONL(model, requests)

	// 2. Upload file via multipart
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	_ = writer.WriteField("purpose", "batch")
	part, err := writer.CreateFormFile("file", "batch_input.jsonl")
	if err != nil {
		return "", nil, err
	}
	_, _ = part.Write([]byte(jsonlContent))
	_ = writer.Close()

	uploadResp, err := a.apiRequestRaw(ctx, "POST", "https://api.openai.com/v1/files", &buf, writer.FormDataContentType())
	if err != nil {
		return "", nil, err
	}

	var fileData struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(uploadResp, &fileData); err != nil {
		return "", nil, NewError(502, "failed to parse openai file upload response", map[string]any{"provider_name": "openai"})
	}

	// 3. Create batch
	batchBody := map[string]any{
		"input_file_id":     fileData.ID,
		"endpoint":          "/v1/chat/completions",
		"completion_window": "24h",
	}
	if options != nil {
		if meta, ok := options["metadata"]; ok {
			batchBody["metadata"] = meta
		}
	}

	batchResp, err := a.apiRequest(ctx, "POST", "/batches", batchBody)
	if err != nil {
		return "", nil, err
	}

	var batchData struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(batchResp, &batchData); err != nil {
		return "", nil, NewError(502, "failed to parse openai batch response", map[string]any{"provider_name": "openai"})
	}

	return batchData.ID, map[string]any{
		"input_file_id": fileData.ID,
		"openai_status": batchData.Status,
	}, nil
}

// PollBatch checks batch status.
func (a *OpenAIBatchAdapter) PollBatch(ctx context.Context, providerBatchID string) (*NativeBatchStatus, error) {
	respBody, err := a.apiRequest(ctx, "GET", "/batches/"+providerBatchID, nil)
	if err != nil {
		return nil, err
	}

	var data struct {
		Status        string `json:"status"`
		RequestCounts struct {
			Total     int `json:"total"`
			Completed int `json:"completed"`
			Failed    int `json:"failed"`
		} `json:"request_counts"`
	}
	if err := json.Unmarshal(respBody, &data); err != nil {
		return nil, NewError(502, "failed to parse openai poll response", nil)
	}

	return &NativeBatchStatus{
		Status:    mapOpenAIBatchStatus(data.Status),
		Total:     data.RequestCounts.Total,
		Completed: data.RequestCounts.Completed,
		Failed:    data.RequestCounts.Failed,
	}, nil
}

// GetBatchResults downloads batch results.
func (a *OpenAIBatchAdapter) GetBatchResults(ctx context.Context, providerBatchID string) ([]BatchResultItem, error) {
	// Get batch to find output file
	respBody, err := a.apiRequest(ctx, "GET", "/batches/"+providerBatchID, nil)
	if err != nil {
		return nil, err
	}

	var batchData struct {
		OutputFileID string `json:"output_file_id"`
		ErrorFileID  string `json:"error_file_id"`
	}
	if err := json.Unmarshal(respBody, &batchData); err != nil {
		return nil, NewError(502, "failed to parse openai batch data", nil)
	}

	var results []BatchResultItem
	seenIDs := map[string]bool{}

	// Download output file
	if batchData.OutputFileID != "" {
		outputBody, err := a.apiRequest(ctx, "GET", "/files/"+batchData.OutputFileID+"/content", nil)
		if err != nil {
			return nil, err
		}

		for _, line := range strings.Split(strings.TrimSpace(string(outputBody)), "\n") {
			if line == "" {
				continue
			}
			var item struct {
				CustomID string `json:"custom_id"`
				Response struct {
					StatusCode int             `json:"status_code"`
					Body       json.RawMessage `json:"body"`
				} `json:"response"`
				Error *struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal([]byte(line), &item) != nil {
				continue
			}

			seenIDs[item.CustomID] = true

			if item.Response.StatusCode == 200 {
				completion, err := translateOpenAIBatchResponse(item.Response.Body)
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
				code := item.Response.StatusCode
				if code == 0 {
					code = 500
				}
				msg := "Unknown error"
				if item.Error != nil && item.Error.Message != "" {
					msg = item.Error.Message
				} else {
					// Try to extract error from response body
					var bodyErr struct {
						Error struct{ Message string } `json:"error"`
					}
					if json.Unmarshal(item.Response.Body, &bodyErr) == nil && bodyErr.Error.Message != "" {
						msg = bodyErr.Error.Message
					}
				}
				results = append(results, BatchResultItem{
					CustomID: item.CustomID, Status: "error",
					Error: &BatchError{Code: code, Message: msg},
				})
			}
		}
	}

	// Download error file
	if batchData.ErrorFileID != "" {
		errorBody, err := a.apiRequest(ctx, "GET", "/files/"+batchData.ErrorFileID+"/content", nil)
		if err == nil {
			for _, line := range strings.Split(strings.TrimSpace(string(errorBody)), "\n") {
				if line == "" {
					continue
				}
				var item struct {
					CustomID string `json:"custom_id"`
					Response struct {
						StatusCode int `json:"status_code"`
					} `json:"response"`
					Error *struct {
						Message string `json:"message"`
					} `json:"error"`
				}
				if json.Unmarshal([]byte(line), &item) != nil {
					continue
				}
				// Only add if not already in results
				if seenIDs[item.CustomID] {
					continue
				}
				seenIDs[item.CustomID] = true
				code := item.Response.StatusCode
				if code == 0 {
					code = 500
				}
				msg := "Batch item error"
				if item.Error != nil && item.Error.Message != "" {
					msg = item.Error.Message
				}
				results = append(results, BatchResultItem{
					CustomID: item.CustomID, Status: "error",
					Error: &BatchError{Code: code, Message: msg},
				})
			}
		}
	}

	return results, nil
}

// CancelBatch cancels a batch.
func (a *OpenAIBatchAdapter) CancelBatch(ctx context.Context, providerBatchID string) error {
	_, err := a.apiRequest(ctx, "POST", "/batches/"+providerBatchID+"/cancel", nil)
	return err
}
