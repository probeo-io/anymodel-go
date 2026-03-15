package anymodel

import "context"

// CustomAdapter wraps OpenAIAdapter for any OpenAI-compatible endpoint.
type CustomAdapter struct {
	*OpenAIAdapter
	providerName string
	staticModels []string
}

// NewCustomAdapter creates a custom OpenAI-compatible provider adapter.
func NewCustomAdapter(name, baseURL, apiKey string, models []string) *CustomAdapter {
	adapter := NewOpenAIAdapter(apiKey, baseURL)
	adapter.SetName(name)
	return &CustomAdapter{
		OpenAIAdapter: adapter,
		providerName:  name,
		staticModels:  models,
	}
}

func (a *CustomAdapter) Name() string       { return a.providerName }
func (a *CustomAdapter) SupportsBatch() bool { return false }

func (a *CustomAdapter) ListModels(ctx context.Context) ([]ModelInfo, error) {
	if len(a.staticModels) > 0 {
		models := make([]ModelInfo, len(a.staticModels))
		for i, id := range a.staticModels {
			models[i] = ModelInfo{ID: a.providerName + "/" + id, Name: id}
		}
		return models, nil
	}
	return a.OpenAIAdapter.ListModels(ctx)
}
