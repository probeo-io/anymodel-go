package anymodel

import (
	"encoding/json"
	"strings"
)

// EstimateTokenCount estimates the number of tokens in a string using a
// rough heuristic of ~4 characters per token.
func EstimateTokenCount(text string) int {
	n := len(text) / 4
	if n == 0 && len(text) > 0 {
		n = 1
	}
	return n
}

type modelLimits struct {
	contextLength      int
	maxCompletionTokens int
}

var knownModelLimits = map[string]modelLimits{
	// OpenAI
	"gpt-4o":        {128000, 16384},
	"gpt-4o-mini":   {128000, 16384},
	"gpt-4-turbo":   {128000, 4096},
	"gpt-3.5-turbo": {16385, 4096},
	"o1":            {200000, 100000},
	"o3":            {200000, 100000},
	"o4-mini":       {200000, 100000},
	"gpt-5-mini":    {1047576, 65536},

	// Anthropic
	"claude-opus-4":    {200000, 32768},
	"claude-sonnet-4":  {200000, 16384},
	"claude-haiku-4":   {200000, 8192},
	"claude-3.5-sonnet": {200000, 8192},
	"claude-3-opus":    {200000, 4096},

	// Google
	"gemini-2.5-pro":   {1048576, 65536},
	"gemini-2.5-flash": {1048576, 65536},
	"gemini-2.0-flash": {1048576, 65536},
	"gemini-1.5-pro":   {2097152, 8192},
	"gemini-1.5-flash": {1048576, 8192},
}

var defaultModelLimits = modelLimits{128000, 4096}

// getModelLimits returns the context length and max completion tokens for a
// model. It strips any "provider/" prefix and uses prefix matching against
// the known models table.
func getModelLimits(model string) modelLimits {
	// Strip provider prefix (e.g. "openai/gpt-4o" -> "gpt-4o")
	if idx := strings.Index(model, "/"); idx >= 0 {
		model = model[idx+1:]
	}

	// Exact match first
	if lim, ok := knownModelLimits[model]; ok {
		return lim
	}

	// Prefix match (e.g. "gpt-4o-2024-08-06" matches "gpt-4o")
	for prefix, lim := range knownModelLimits {
		if strings.HasPrefix(model, prefix) {
			return lim
		}
	}

	return defaultModelLimits
}

// ResolveMaxTokens determines the max_tokens value to use for a request.
// If userMaxTokens is non-nil it is returned directly. Otherwise the function
// estimates input token usage from the messages, applies a 5% safety margin,
// and returns the smaller of the model's max completion tokens and the
// remaining context window (clamped to at least 1).
func ResolveMaxTokens(model string, messages []Message, userMaxTokens *int) int {
	if userMaxTokens != nil {
		return *userMaxTokens
	}

	// Estimate input tokens by marshalling messages to JSON.
	data, _ := json.Marshal(messages)
	estimatedInput := len(data) / 4

	// Add 5% safety margin.
	estimatedWithMargin := int(float64(estimatedInput) * 1.05)

	limits := getModelLimits(model)

	remaining := limits.contextLength - estimatedWithMargin
	if remaining < 1 {
		remaining = 1
	}

	maxTokens := limits.maxCompletionTokens
	if remaining < maxTokens {
		maxTokens = remaining
	}
	if maxTokens < 1 {
		maxTokens = 1
	}

	return maxTokens
}
