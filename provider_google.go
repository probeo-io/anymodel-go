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

var googleSupportedParams = map[string]bool{
	"temperature": true, "max_tokens": true, "top_p": true, "top_k": true,
	"stop": true, "stream": true, "tools": true, "tool_choice": true,
	"response_format": true,
}

const geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta"

// GoogleAdapter implements the Adapter interface for Google Gemini.
type GoogleAdapter struct {
	apiKey string
	client *http.Client
}

// NewGoogleAdapter creates a Google Gemini provider adapter.
func NewGoogleAdapter(apiKey string) *GoogleAdapter {
	return &GoogleAdapter{apiKey: apiKey, client: &http.Client{Timeout: GetDefaultHTTPTimeout()}}
}

func (a *GoogleAdapter) Name() string                        { return "google" }
func (a *GoogleAdapter) SupportsParameter(param string) bool { return googleSupportedParams[param] }
func (a *GoogleAdapter) SupportsBatch() bool                 { return true }

func (a *GoogleAdapter) translateMessages(messages []Message) (system string, contents []map[string]any) {
	for _, m := range messages {
		switch m.Role {
		case RoleSystem:
			if system != "" {
				system += "\n\n"
			}
			system += m.Content
		case RoleUser:
			contents = append(contents, map[string]any{
				"role": "user", "parts": []map[string]any{{"text": m.Content}},
			})
		case RoleAssistant:
			if len(m.ToolCalls) > 0 {
				var parts []map[string]any
				if m.Content != "" {
					parts = append(parts, map[string]any{"text": m.Content})
				}
				for _, tc := range m.ToolCalls {
					var args any
					json.Unmarshal([]byte(tc.Function.Arguments), &args)
					parts = append(parts, map[string]any{"functionCall": map[string]any{"name": tc.Function.Name, "args": args}})
				}
				contents = append(contents, map[string]any{"role": "model", "parts": parts})
			} else {
				contents = append(contents, map[string]any{
					"role": "model", "parts": []map[string]any{{"text": m.Content}},
				})
			}
		case RoleTool:
			contents = append(contents, map[string]any{
				"role": "user", "parts": []map[string]any{{
					"functionResponse": map[string]any{"name": m.Name, "response": map[string]any{"result": m.Content}},
				}},
			})
		}
	}
	return
}

func (a *GoogleAdapter) buildBody(req ChatCompletionRequest) map[string]any {
	system, contents := a.translateMessages(req.Messages)
	body := map[string]any{"contents": contents}
	if system != "" {
		body["systemInstruction"] = map[string]any{"parts": []map[string]any{{"text": system}}}
	}
	genConfig := map[string]any{}
	if req.Temperature != nil {
		genConfig["temperature"] = *req.Temperature
	}
	if req.MaxTokens != nil {
		genConfig["maxOutputTokens"] = *req.MaxTokens
	}
	if req.TopP != nil {
		genConfig["topP"] = *req.TopP
	}
	if req.TopK != nil {
		genConfig["topK"] = *req.TopK
	}
	if len(req.Stop) > 0 {
		genConfig["stopSequences"] = req.Stop
	}
	if req.ResponseFormat != nil && req.ResponseFormat.Type == "json_object" {
		genConfig["responseMimeType"] = "application/json"
	}
	if len(genConfig) > 0 {
		body["generationConfig"] = genConfig
	}
	if len(req.Tools) > 0 {
		var decls []map[string]any
		for _, t := range req.Tools {
			decls = append(decls, map[string]any{
				"name": t.Function.Name, "description": t.Function.Description, "parameters": t.Function.Parameters,
			})
		}
		body["tools"] = []map[string]any{{"functionDeclarations": decls}}
	}
	return body
}

func (a *GoogleAdapter) geminiURL(model string, stream bool) string {
	method := "generateContent"
	if stream {
		method = "streamGenerateContent"
	}
	u := fmt.Sprintf("%s/models/%s:%s?key=%s", geminiBaseURL, model, method, a.apiKey)
	if stream {
		u += "&alt=sse"
	}
	return u
}

func (a *GoogleAdapter) mapError(resp *http.Response, body []byte) error {
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
		return NewError(401, msg, map[string]any{"provider_name": "google"})
	case code == 429:
		return NewError(429, msg, map[string]any{"provider_name": "google"})
	case code >= 500:
		return NewError(502, msg, map[string]any{"provider_name": "google"})
	default:
		return NewError(code, msg, map[string]any{"provider_name": "google"})
	}
}

func (a *GoogleAdapter) translateResponse(respBody []byte, model string) (*ChatCompletion, error) {
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
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, NewError(502, "failed to parse google response", nil)
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
				msg.Content = part.Text
			}
		}
		switch raw.Candidates[0].FinishReason {
		case "MAX_TOKENS":
			finishReason = FinishLength
		case "SAFETY":
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
			PromptTokens: raw.UsageMetadata.PromptTokenCount,
			CompletionTokens: raw.UsageMetadata.CandidatesTokenCount,
			TotalTokens: raw.UsageMetadata.TotalTokenCount,
		},
	}, nil
}

// extractGoogleRateLimitHeaders extracts rate-limit headers from a Google response.
func extractGoogleRateLimitHeaders(resp *http.Response) map[string]string {
	headers := make(map[string]string)
	keys := []string{
		"x-ratelimit-remaining-requests",
		"x-ratelimit-remaining-tokens",
		"retry-after",
	}
	for _, k := range keys {
		if v := resp.Header.Get(k); v != "" {
			headers[k] = v
		}
	}
	return headers
}

// SendRequestWithMeta sends a request and returns the completion with response metadata.
func (a *GoogleAdapter) SendRequestWithMeta(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionWithMeta, error) {
	body := a.buildBody(req)
	data, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.geminiURL(req.Model, false), bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, NewError(502, fmt.Sprintf("google request failed: %v", err), map[string]any{"provider_name": "google"})
	}
	defer resp.Body.Close()

	meta := ResponseMeta{Headers: extractGoogleRateLimitHeaders(resp)}

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, a.mapError(resp, respBody)
	}
	completion, err := a.translateResponse(respBody, req.Model)
	if err != nil {
		return nil, err
	}
	return &ChatCompletionWithMeta{Completion: completion, Meta: meta}, nil
}

func (a *GoogleAdapter) SendRequest(ctx context.Context, req ChatCompletionRequest) (*ChatCompletion, error) {
	body := a.buildBody(req)
	data, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.geminiURL(req.Model, false), bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, NewError(502, fmt.Sprintf("google request failed: %v", err), map[string]any{"provider_name": "google"})
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, a.mapError(resp, respBody)
	}
	return a.translateResponse(respBody, req.Model)
}

func (a *GoogleAdapter) SendStreamingRequest(ctx context.Context, req ChatCompletionRequest) (<-chan ChatCompletionChunk, <-chan error) {
	chunkCh := make(chan ChatCompletionChunk, 32)
	errCh := make(chan error, 1)

	go func() {
		defer close(chunkCh)
		defer close(errCh)

		body := a.buildBody(req)
		data, _ := json.Marshal(body)

		httpReq, err := http.NewRequestWithContext(ctx, "POST", a.geminiURL(req.Model, true), bytes.NewReader(data))
		if err != nil {
			errCh <- err
			return
		}
		httpReq.Header.Set("Content-Type", "application/json")

		resp, err := a.client.Do(httpReq)
		if err != nil {
			errCh <- NewError(502, fmt.Sprintf("google stream failed: %v", err), map[string]any{"provider_name": "google"})
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			respBody, _ := io.ReadAll(resp.Body)
			errCh <- a.mapError(resp, respBody)
			return
		}

		genID := GenerateID("gen")
		model := "google/" + req.Model
		for event := range ParseSSE(resp.Body) {
			if event.Data == "" {
				continue
			}
			var raw struct {
				Candidates []struct {
					Content struct {
						Parts []struct{ Text string `json:"text"` } `json:"parts"`
					} `json:"content"`
				} `json:"candidates"`
			}
			if json.Unmarshal([]byte(event.Data), &raw) != nil || len(raw.Candidates) == 0 {
				continue
			}
			for _, part := range raw.Candidates[0].Content.Parts {
				chunkCh <- ChatCompletionChunk{
					ID: genID, Object: "chat.completion.chunk",
					Created: time.Now().Unix(), Model: model,
					Choices: []ChunkChoice{{Index: 0, Delta: ChunkDelta{Content: part.Text}}},
				}
			}
		}
	}()

	return chunkCh, errCh
}

func (a *GoogleAdapter) ListModels(ctx context.Context) ([]ModelInfo, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/models?key=%s", geminiBaseURL, a.apiKey), nil)
	if err != nil {
		return googleFallbackModels(), nil
	}
	resp, err := a.client.Do(httpReq)
	if err != nil {
		return googleFallbackModels(), nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return googleFallbackModels(), nil
	}

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Models []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
			Description string `json:"description"`
		} `json:"models"`
	}
	if json.Unmarshal(body, &result) != nil {
		return googleFallbackModels(), nil
	}

	var models []ModelInfo
	for _, m := range result.Models {
		id := strings.TrimPrefix(m.Name, "models/")
		if !strings.Contains(id, "gemini") {
			continue
		}
		models = append(models, ModelInfo{ID: "google/" + id, Name: m.DisplayName, Description: m.Description})
	}
	if len(models) == 0 {
		return googleFallbackModels(), nil
	}
	return models, nil
}

func googleFallbackModels() []ModelInfo {
	ids := []string{"gemini-2.5-pro", "gemini-2.5-flash", "gemini-2.0-flash", "gemini-2.0-flash-lite", "gemini-1.5-pro", "gemini-1.5-flash"}
	models := make([]ModelInfo, len(ids))
	for i, id := range ids {
		models[i] = ModelInfo{ID: "google/" + id, Name: id}
	}
	return models
}
