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

var openaiSupportedParams = map[string]bool{
	"temperature": true, "max_tokens": true, "top_p": true,
	"frequency_penalty": true, "presence_penalty": true,
	"seed": true, "stop": true, "stream": true,
	"logprobs": true, "top_logprobs": true,
	"response_format": true, "tools": true, "tool_choice": true, "user": true,
	"service_tier": true,
}

// OpenAIAdapter implements the Adapter interface for OpenAI.
type OpenAIAdapter struct {
	apiKey  string
	baseURL string
	client  *http.Client
	name    string
}

// NewOpenAIAdapter creates an OpenAI provider adapter.
func NewOpenAIAdapter(apiKey string, baseURL string) *OpenAIAdapter {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &OpenAIAdapter{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{}, // timeout controlled per-request via context
		name:    "openai",
	}
}

func (a *OpenAIAdapter) Name() string                        { return a.name }
func (a *OpenAIAdapter) SupportsParameter(param string) bool { return openaiSupportedParams[param] }
func (a *OpenAIAdapter) SupportsBatch() bool                 { return true }

// SetName overrides the provider name (used by CustomAdapter).
func (a *OpenAIAdapter) SetName(n string) { a.name = n }

func (a *OpenAIAdapter) buildBody(req ChatCompletionRequest) map[string]any {
	body := map[string]any{"model": req.Model, "messages": req.Messages}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.MaxTokens != nil {
		body["max_tokens"] = *req.MaxTokens
	}
	if req.TopP != nil {
		body["top_p"] = *req.TopP
	}
	if req.FrequencyPenalty != nil {
		body["frequency_penalty"] = *req.FrequencyPenalty
	}
	if req.PresencePenalty != nil {
		body["presence_penalty"] = *req.PresencePenalty
	}
	if req.Seed != nil {
		body["seed"] = *req.Seed
	}
	if len(req.Stop) > 0 {
		if len(req.Stop) == 1 {
			body["stop"] = req.Stop[0]
		} else {
			body["stop"] = req.Stop
		}
	}
	if req.Stream {
		body["stream"] = true
	}
	if req.Logprobs != nil {
		body["logprobs"] = *req.Logprobs
	}
	if req.TopLogprobs != nil {
		body["top_logprobs"] = *req.TopLogprobs
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
	if req.User != "" {
		body["user"] = req.User
	}
	if req.ServiceTier != "" {
		body["service_tier"] = req.ServiceTier
	}
	return body
}

func (a *OpenAIAdapter) doRequest(ctx context.Context, body map[string]any, timeout time.Duration) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)
	return a.client.Do(httpReq)
}

func (a *OpenAIAdapter) requestTimeout(req ChatCompletionRequest) time.Duration {
	if req.ServiceTier == "flex" {
		return GetFlexHTTPTimeout()
	}
	return GetDefaultHTTPTimeout()
}

func (a *OpenAIAdapter) mapError(resp *http.Response, body []byte) error {
	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	msg := string(body)
	if json.Unmarshal(body, &parsed) == nil && parsed.Error.Message != "" {
		msg = parsed.Error.Message
	}
	code := resp.StatusCode
	switch {
	case code == 401 || code == 403:
		return NewError(401, msg, map[string]any{"provider_name": a.name})
	case code == 429:
		return NewError(429, msg, map[string]any{"provider_name": a.name})
	case code >= 500:
		return NewError(502, msg, map[string]any{"provider_name": a.name})
	default:
		return NewError(code, msg, map[string]any{"provider_name": a.name})
	}
}

func (a *OpenAIAdapter) SendRequest(ctx context.Context, req ChatCompletionRequest) (*ChatCompletion, error) {
	body := a.buildBody(req)
	delete(body, "stream")

	resp, err := a.doRequest(ctx, body, a.requestTimeout(req))
	if err != nil {
		return nil, NewError(502, fmt.Sprintf("%s request failed: %v", a.name, err), map[string]any{"provider_name": a.name})
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, NewError(502, "failed to read response", map[string]any{"provider_name": a.name})
	}
	if resp.StatusCode != 200 {
		return nil, a.mapError(resp, respBody)
	}

	var completion ChatCompletion
	if err := json.Unmarshal(respBody, &completion); err != nil {
		return nil, NewError(502, "failed to parse response", map[string]any{"provider_name": a.name})
	}

	if strings.HasPrefix(completion.ID, "chatcmpl-") {
		completion.ID = "gen-" + completion.ID[9:]
	}
	completion.Model = a.name + "/" + completion.Model
	return &completion, nil
}

func (a *OpenAIAdapter) SendStreamingRequest(ctx context.Context, req ChatCompletionRequest) (<-chan ChatCompletionChunk, <-chan error) {
	chunkCh := make(chan ChatCompletionChunk, 32)
	errCh := make(chan error, 1)

	go func() {
		defer close(chunkCh)
		defer close(errCh)

		body := a.buildBody(req)
		body["stream"] = true

		resp, err := a.doRequest(ctx, body, a.requestTimeout(req))
		if err != nil {
			errCh <- NewError(502, fmt.Sprintf("%s stream failed: %v", a.name, err), map[string]any{"provider_name": a.name})
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			respBody, _ := io.ReadAll(resp.Body)
			errCh <- a.mapError(resp, respBody)
			return
		}

		for event := range ParseSSE(resp.Body) {
			if event.Data == "[DONE]" {
				break
			}
			var chunk ChatCompletionChunk
			if err := json.Unmarshal([]byte(event.Data), &chunk); err != nil {
				continue
			}
			if strings.HasPrefix(chunk.ID, "chatcmpl-") {
				chunk.ID = "gen-" + chunk.ID[9:]
			}
			chunk.Model = a.name + "/" + chunk.Model
			chunkCh <- chunk
		}
	}()

	return chunkCh, errCh
}

func (a *OpenAIAdapter) ListModels(ctx context.Context) ([]ModelInfo, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET", a.baseURL+"/models", nil)
	if err != nil {
		return openAIFallbackModels(a.name), nil
	}
	httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return openAIFallbackModels(a.name), nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return openAIFallbackModels(a.name), nil
	}

	var result struct {
		Data []struct {
			ID      string `json:"id"`
			Created int64  `json:"created"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &result) != nil {
		return openAIFallbackModels(a.name), nil
	}

	chatPrefixes := []string{"gpt-", "o1", "o3", "o4", "chatgpt-"}
	var models []ModelInfo
	for _, m := range result.Data {
		isChat := false
		for _, p := range chatPrefixes {
			if strings.HasPrefix(m.ID, p) {
				isChat = true
				break
			}
		}
		if !isChat {
			continue
		}
		models = append(models, ModelInfo{ID: a.name + "/" + m.ID, Name: m.ID, Created: m.Created})
	}
	if len(models) == 0 {
		return openAIFallbackModels(a.name), nil
	}
	return models, nil
}

func openAIFallbackModels(prefix string) []ModelInfo {
	ids := []string{"gpt-4o", "gpt-4o-mini", "gpt-4-turbo", "o1", "o3-mini"}
	models := make([]ModelInfo, len(ids))
	for i, id := range ids {
		models[i] = ModelInfo{ID: prefix + "/" + id, Name: id, Created: time.Now().Unix()}
	}
	return models
}
