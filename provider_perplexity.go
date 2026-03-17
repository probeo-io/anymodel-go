package anymodel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

var perplexitySupportedParams = map[string]bool{
	"temperature": true, "max_tokens": true, "top_p": true,
	"frequency_penalty": true, "presence_penalty": true,
	"stream": true, "stop": true, "response_format": true,
	"tools": true, "tool_choice": true,
}

// PerplexityAdapter implements the Adapter interface for Perplexity.
type PerplexityAdapter struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewPerplexityAdapter creates a Perplexity provider adapter.
func NewPerplexityAdapter(apiKey string) *PerplexityAdapter {
	return &PerplexityAdapter{
		apiKey:  apiKey,
		baseURL: "https://api.perplexity.ai",
		client:  &http.Client{Timeout: GetDefaultHTTPTimeout()},
	}
}

func (a *PerplexityAdapter) Name() string                        { return "perplexity" }
func (a *PerplexityAdapter) SupportsParameter(param string) bool { return perplexitySupportedParams[param] }
func (a *PerplexityAdapter) SupportsBatch() bool                 { return false }

func (a *PerplexityAdapter) buildBody(req ChatCompletionRequest) map[string]any {
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
	if req.ResponseFormat != nil {
		body["response_format"] = req.ResponseFormat
	}
	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
	}
	if req.ToolChoice != nil {
		body["tool_choice"] = req.ToolChoice
	}
	return body
}

func (a *PerplexityAdapter) doRequest(ctx context.Context, body map[string]any) (*http.Response, error) {
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

func (a *PerplexityAdapter) mapError(resp *http.Response, body []byte) error {
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
		return NewError(401, msg, map[string]any{"provider_name": "perplexity"})
	case code == 429:
		return NewError(429, msg, map[string]any{"provider_name": "perplexity"})
	case code >= 500:
		return NewError(502, msg, map[string]any{"provider_name": "perplexity"})
	default:
		return NewError(code, msg, map[string]any{"provider_name": "perplexity"})
	}
}

func (a *PerplexityAdapter) SendRequest(ctx context.Context, req ChatCompletionRequest) (*ChatCompletion, error) {
	body := a.buildBody(req)
	delete(body, "stream")

	resp, err := a.doRequest(ctx, body)
	if err != nil {
		return nil, NewError(502, fmt.Sprintf("perplexity request failed: %v", err), map[string]any{"provider_name": "perplexity"})
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, NewError(502, "failed to read response", map[string]any{"provider_name": "perplexity"})
	}
	if resp.StatusCode != 200 {
		return nil, a.mapError(resp, respBody)
	}

	var completion ChatCompletion
	if err := json.Unmarshal(respBody, &completion); err != nil {
		return nil, NewError(502, "failed to parse response", map[string]any{"provider_name": "perplexity"})
	}

	if strings.HasPrefix(completion.ID, "chatcmpl-") {
		completion.ID = "gen-" + completion.ID[9:]
	}
	completion.Model = "perplexity/" + completion.Model
	return &completion, nil
}

func (a *PerplexityAdapter) SendStreamingRequest(ctx context.Context, req ChatCompletionRequest) (<-chan ChatCompletionChunk, <-chan error) {
	chunkCh := make(chan ChatCompletionChunk, 32)
	errCh := make(chan error, 1)

	go func() {
		defer close(chunkCh)
		defer close(errCh)

		body := a.buildBody(req)
		body["stream"] = true

		resp, err := a.doRequest(ctx, body)
		if err != nil {
			errCh <- NewError(502, fmt.Sprintf("perplexity stream failed: %v", err), map[string]any{"provider_name": "perplexity"})
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
			chunk.Model = "perplexity/" + chunk.Model
			chunkCh <- chunk
		}
	}()

	return chunkCh, errCh
}

func (a *PerplexityAdapter) ListModels(_ context.Context) ([]ModelInfo, error) {
	return perplexityModels(), nil
}

func perplexityModels() []ModelInfo {
	type modelDef struct {
		id        string
		name      string
		context   int
		maxOutput int
	}
	defs := []modelDef{
		{"sonar", "Sonar", 128000, 4096},
		{"sonar-pro", "Sonar Pro", 200000, 8192},
		{"sonar-reasoning", "Sonar Reasoning", 128000, 8192},
		{"sonar-reasoning-pro", "Sonar Reasoning Pro", 128000, 16384},
		{"sonar-deep-research", "Sonar Deep Research", 128000, 16384},
		{"r1-1776", "R1 1776", 128000, 16384},
	}
	models := make([]ModelInfo, len(defs))
	for i, d := range defs {
		models[i] = ModelInfo{
			ID:            "perplexity/" + d.id,
			Name:          d.name,
			Created:       0,
			ContextLength: d.context,
		}
	}
	return models
}
