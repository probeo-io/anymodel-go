package anymodel

import (
	"context"
	"time"
)

// Router handles request routing, transforms, fallback, and retry logic.
type Router struct {
	registry    *Registry
	aliases     map[string]string
	defaults    *DefaultsConfig
	statsStore  *GenerationStatsStore
	rateLimiter *RateLimitTracker
}

// NewRouter creates a new router.
func NewRouter(registry *Registry, aliases map[string]string, defaults *DefaultsConfig, statsStore *GenerationStatsStore) *Router {
	return &Router{
		registry:    registry,
		aliases:     aliases,
		defaults:    defaults,
		statsStore:  statsStore,
		rateLimiter: NewRateLimitTracker(),
	}
}

func (r *Router) applyDefaults(req *ChatCompletionRequest) {
	if r.defaults == nil {
		return
	}
	if req.Temperature == nil && r.defaults.Temperature != nil {
		req.Temperature = r.defaults.Temperature
	}
	if req.MaxTokens == nil && r.defaults.MaxTokens != nil {
		req.MaxTokens = r.defaults.MaxTokens
	}
	if len(req.Transforms) == 0 && len(r.defaults.Transforms) > 0 {
		req.Transforms = r.defaults.Transforms
	}
}

func (r *Router) stripUnsupported(req ChatCompletionRequest, adapter Adapter) ChatCompletionRequest {
	strippable := []struct {
		param string
		clear func(*ChatCompletionRequest)
	}{
		{"temperature", func(r *ChatCompletionRequest) { r.Temperature = nil }},
		{"max_tokens", func(r *ChatCompletionRequest) { r.MaxTokens = nil }},
		{"top_p", func(r *ChatCompletionRequest) { r.TopP = nil }},
		{"top_k", func(r *ChatCompletionRequest) { r.TopK = nil }},
		{"frequency_penalty", func(r *ChatCompletionRequest) { r.FrequencyPenalty = nil }},
		{"presence_penalty", func(r *ChatCompletionRequest) { r.PresencePenalty = nil }},
		{"repetition_penalty", func(r *ChatCompletionRequest) { r.RepetitionPenalty = nil }},
		{"seed", func(r *ChatCompletionRequest) { r.Seed = nil }},
		{"stop", func(r *ChatCompletionRequest) { r.Stop = nil }},
		{"logprobs", func(r *ChatCompletionRequest) { r.Logprobs = nil }},
		{"top_logprobs", func(r *ChatCompletionRequest) { r.TopLogprobs = nil }},
		{"response_format", func(r *ChatCompletionRequest) { r.ResponseFormat = nil }},
		{"tools", func(r *ChatCompletionRequest) { r.Tools = nil }},
		{"tool_choice", func(r *ChatCompletionRequest) { r.ToolChoice = nil }},
		{"user", func(r *ChatCompletionRequest) { r.User = "" }},
	}
	for _, s := range strippable {
		if !adapter.SupportsParameter(s.param) {
			s.clear(&req)
		}
	}
	return req
}

// Complete sends a non-streaming completion request.
func (r *Router) Complete(ctx context.Context, req ChatCompletionRequest) (*ChatCompletion, error) {
	if err := ValidateRequest(&req); err != nil {
		return nil, err
	}
	r.applyDefaults(&req)
	if len(req.Transforms) > 0 {
		req.Messages = ApplyTransforms(req.Transforms, req.Messages, 128000)
	}
	if len(req.Models) > 0 && req.Route == "fallback" {
		return r.completeWithFallback(ctx, req)
	}

	parsed, err := ParseModelString(req.Model, r.aliases)
	if err != nil {
		return nil, err
	}
	adapter, err := r.registry.Get(parsed.Provider)
	if err != nil {
		return nil, err
	}

	stripped := r.stripUnsupported(req, adapter)
	stripped.Model = parsed.Model

	retries := 2
	if r.defaults != nil && r.defaults.Retries != nil {
		retries = *r.defaults.Retries
	}

	start := time.Now()
	result, err := WithRetry(ctx, RetryOptions{
		MaxRetries: retries, BaseDelay: 500 * time.Millisecond, MaxDelay: 10 * time.Second,
	}, func() (*ChatCompletion, error) {
		return adapter.SendRequest(ctx, stripped)
	})

	if err != nil {
		if e, ok := err.(*Error); ok && e.Code == 429 {
			r.rateLimiter.Record(parsed.Provider, 60*time.Second)
		}
		return nil, err
	}

	elapsed := time.Since(start)
	r.statsStore.Record(GenerationStats{
		ID: result.ID, Model: result.Model, ProviderName: parsed.Provider,
		TokensPrompt: result.Usage.PromptTokens, TokensCompletion: result.Usage.CompletionTokens,
		Latency: elapsed.Seconds(), GenerationTime: elapsed.Seconds(),
		CreatedAt: time.Now().Format(time.RFC3339),
		FinishReason: result.Choices[0].FinishReason, Streamed: false,
	})

	return result, nil
}

func (r *Router) completeWithFallback(ctx context.Context, req ChatCompletionRequest) (*ChatCompletion, error) {
	models := r.applyProviderPreferences(req.Models, req.Provider)
	var lastErr error
	for _, model := range models {
		parsed, err := ParseModelString(model, r.aliases)
		if err != nil {
			lastErr = err
			continue
		}
		if r.rateLimiter.IsRateLimited(parsed.Provider) {
			continue
		}
		adapter, err := r.registry.Get(parsed.Provider)
		if err != nil {
			lastErr = err
			continue
		}
		stripped := r.stripUnsupported(req, adapter)
		stripped.Model = parsed.Model
		stripped.Models = nil
		stripped.Route = ""

		result, err := adapter.SendRequest(ctx, stripped)
		if err != nil {
			lastErr = err
			if e, ok := err.(*Error); ok && e.Code == 429 {
				r.rateLimiter.Record(parsed.Provider, 60*time.Second)
			}
			continue
		}
		return result, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, NewError(502, "all fallback models failed", nil)
}

func (r *Router) applyProviderPreferences(models []string, prefs *ProviderPreferences) []string {
	if prefs == nil {
		return models
	}
	if len(prefs.Only) > 0 {
		onlySet := make(map[string]bool)
		for _, p := range prefs.Only {
			onlySet[p] = true
		}
		var filtered []string
		for _, m := range models {
			if p, err := ParseModelString(m, nil); err == nil && onlySet[p.Provider] {
				filtered = append(filtered, m)
			}
		}
		models = filtered
	}
	if len(prefs.Ignore) > 0 {
		ignoreSet := make(map[string]bool)
		for _, p := range prefs.Ignore {
			ignoreSet[p] = true
		}
		var filtered []string
		for _, m := range models {
			if p, err := ParseModelString(m, nil); err == nil && !ignoreSet[p.Provider] {
				filtered = append(filtered, m)
			}
		}
		models = filtered
	}
	if len(prefs.Order) > 0 {
		orderMap := make(map[string]int)
		for i, p := range prefs.Order {
			orderMap[p] = i
		}
		var ordered, rest []string
		for _, m := range models {
			if p, err := ParseModelString(m, nil); err == nil {
				if _, ok := orderMap[p.Provider]; ok {
					ordered = append(ordered, m)
				} else {
					rest = append(rest, m)
				}
			}
		}
		models = append(ordered, rest...)
	}
	return models
}

// Stream sends a streaming completion request.
func (r *Router) Stream(ctx context.Context, req ChatCompletionRequest) (<-chan ChatCompletionChunk, <-chan error, error) {
	if err := ValidateRequest(&req); err != nil {
		return nil, nil, err
	}
	r.applyDefaults(&req)
	if len(req.Transforms) > 0 {
		req.Messages = ApplyTransforms(req.Transforms, req.Messages, 128000)
	}
	parsed, err := ParseModelString(req.Model, r.aliases)
	if err != nil {
		return nil, nil, err
	}
	adapter, err := r.registry.Get(parsed.Provider)
	if err != nil {
		return nil, nil, err
	}
	stripped := r.stripUnsupported(req, adapter)
	stripped.Model = parsed.Model
	chunkCh, errCh := adapter.SendStreamingRequest(ctx, stripped)
	return chunkCh, errCh, nil
}
