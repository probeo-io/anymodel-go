package anymodel

import "strings"

// PricingEntry holds per-token pricing in USD for a model.
type PricingEntry struct {
	Prompt     float64 // USD per token (prompt/input)
	Completion float64 // USD per token (completion/output)
}

// GetModelPricing returns pricing for a model ID.
// It first tries an exact match, then falls back to prefix matching
// (e.g., "openai/gpt-4o-2024-08-06" matches "openai/gpt-4o").
func GetModelPricing(modelID string) *PricingEntry {
	// Exact match.
	if entry, ok := modelPricing[modelID]; ok {
		return &entry
	}

	// Prefix match: find the longest prefix that matches.
	var bestKey string
	for key := range modelPricing {
		if strings.HasPrefix(modelID, key) && len(key) > len(bestKey) {
			bestKey = key
		}
	}
	if bestKey != "" {
		entry := modelPricing[bestKey]
		return &entry
	}

	return nil
}

// CalculateCost estimates the cost in USD for a generation.
func CalculateCost(modelID string, promptTokens, completionTokens int) float64 {
	entry := GetModelPricing(modelID)
	if entry == nil {
		return 0
	}
	return entry.Prompt*float64(promptTokens) + entry.Completion*float64(completionTokens)
}
