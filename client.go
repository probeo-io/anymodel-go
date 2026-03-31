package anymodel

import (
	"context"
	"time"
)

// Client is the main anymodel client.
type Client struct {
	Chat       *ChatNamespace
	Models     *ModelsNamespace
	Generation *GenerationNamespace
	Batches    *BatchNamespace

	config     *Config
	router     *Router
	registry   *Registry
	statsStore *GenerationStatsStore
	batchMgr   *BatchManager
}

// New creates a new AnyModel client.
func New(cfg *Config) *Client {
	resolved := ResolveConfig(cfg)
	registry := NewRegistry()
	statsStore := NewGenerationStatsStore(1000)

	aliases := resolved.Aliases
	if aliases == nil {
		aliases = map[string]string{}
	}

	if resolved.Defaults != nil && resolved.Defaults.Timeout != nil {
		SetDefaultHTTPTimeout(time.Duration(*resolved.Defaults.Timeout * float64(time.Second)))
	}

	router := NewRouter(registry, aliases, resolved.Defaults, statsStore)
	registerProviders(resolved, registry)

	batchDir := ".anymodel/batches"
	batchConcurrency := 5
	batchConcurrencyMax := 500
	batchPollInterval := 5 * time.Second
	if resolved.Batch != nil {
		if resolved.Batch.Dir != "" {
			batchDir = resolved.Batch.Dir
		}
		if resolved.Batch.ConcurrencyFallback > 0 {
			batchConcurrency = resolved.Batch.ConcurrencyFallback
		}
		if resolved.Batch.ConcurrencyMax > 0 {
			batchConcurrencyMax = resolved.Batch.ConcurrencyMax
		}
		if resolved.Batch.PollInterval > 0 {
			batchPollInterval = time.Duration(resolved.Batch.PollInterval * float64(time.Second))
		}
	}

	store := NewBatchStore(batchDir)
	batchMgr := NewBatchManager(registry, store, aliases, batchConcurrency, batchConcurrencyMax, batchPollInterval, router)

	// Register native batch adapters
	registerBatchAdapters(resolved, batchMgr)

	c := &Client{
		config: resolved, router: router, registry: registry,
		statsStore: statsStore, batchMgr: batchMgr,
	}

	c.Chat = &ChatNamespace{Completions: &CompletionsNamespace{router: router}}
	c.Models = &ModelsNamespace{registry: registry}
	c.Generation = &GenerationNamespace{store: statsStore}
	c.Batches = &BatchNamespace{mgr: batchMgr}

	return c
}

// Registry returns the provider registry.
func (c *Client) Registry() *Registry {
	return c.registry
}

func registerProviders(cfg *Config, registry *Registry) {
	if cfg.OpenAI != nil && cfg.OpenAI.APIKey != "" {
		registry.Register("openai", NewOpenAIAdapter(cfg.OpenAI.APIKey, ""))
	}
	if cfg.Anthropic != nil && cfg.Anthropic.APIKey != "" {
		registry.Register("anthropic", NewAnthropicAdapter(cfg.Anthropic.APIKey))
	}
	if cfg.Google != nil && cfg.Google.APIKey != "" {
		registry.Register("google", NewGoogleAdapter(cfg.Google.APIKey))
	}
	if cfg.Perplexity != nil && cfg.Perplexity.APIKey != "" {
		registry.Register("perplexity", NewPerplexityAdapter(cfg.Perplexity.APIKey))
	}

	builtInConfigs := map[string]*ProviderConfig{
		"mistral": cfg.Mistral, "groq": cfg.Groq, "deepseek": cfg.DeepSeek,
		"xai": cfg.XAI, "together": cfg.Together, "fireworks": cfg.Fireworks,
	}
	for slug, pc := range builtInConfigs {
		if pc != nil && pc.APIKey != "" {
			if baseURL, ok := BuiltInProviders[slug]; ok {
				registry.Register(slug, NewCustomAdapter(slug, baseURL, pc.APIKey, nil))
			}
		}
	}

	if cfg.Ollama != nil {
		baseURL := "http://localhost:11434/v1"
		if cfg.Ollama.BaseURL != "" {
			baseURL = cfg.Ollama.BaseURL
		}
		registry.Register("ollama", NewCustomAdapter("ollama", baseURL, "", nil))
	}

	for name, custom := range cfg.Custom {
		registry.Register(name, NewCustomAdapter(name, custom.BaseURL, custom.APIKey, custom.Models))
	}
}

func registerBatchAdapters(cfg *Config, batchMgr *BatchManager) {
	if cfg.OpenAI != nil && cfg.OpenAI.APIKey != "" {
		batchMgr.RegisterBatchAdapter("openai", NewOpenAIBatchAdapter(cfg.OpenAI.APIKey))
	}
	if cfg.Anthropic != nil && cfg.Anthropic.APIKey != "" {
		batchMgr.RegisterBatchAdapter("anthropic", NewAnthropicBatchAdapter(cfg.Anthropic.APIKey))
	}
	if cfg.Google != nil && cfg.Google.APIKey != "" {
		batchMgr.RegisterBatchAdapter("google", NewGoogleBatchAdapter(cfg.Google.APIKey))
	}
}

// ── Namespaces ──────────────────────────────────────────────────────────────

// ChatNamespace provides chat-related operations.
type ChatNamespace struct {
	Completions *CompletionsNamespace
}

// CompletionsNamespace provides chat completion operations.
type CompletionsNamespace struct {
	router *Router
}

// Create sends a chat completion request.
func (n *CompletionsNamespace) Create(ctx context.Context, req ChatCompletionRequest) (*ChatCompletion, error) {
	return n.router.Complete(ctx, req)
}

// CreateStream sends a streaming chat completion request.
func (n *CompletionsNamespace) CreateStream(ctx context.Context, req ChatCompletionRequest) (<-chan ChatCompletionChunk, <-chan error, error) {
	return n.router.Stream(ctx, req)
}

// ModelsNamespace provides model listing.
type ModelsNamespace struct {
	registry *Registry
}

// List returns available models, optionally filtered by provider.
func (n *ModelsNamespace) List(ctx context.Context, provider string) ([]ModelInfo, error) {
	var allModels []ModelInfo
	for _, adapter := range n.registry.All() {
		if provider != "" && adapter.Name() != provider {
			continue
		}
		models, err := adapter.ListModels(ctx)
		if err != nil {
			continue
		}
		allModels = append(allModels, models...)
	}
	return allModels, nil
}

// GenerationNamespace provides generation stats.
type GenerationNamespace struct {
	store *GenerationStatsStore
}

// Get returns stats for a generation by ID.
func (n *GenerationNamespace) Get(id string) *GenerationStats {
	return n.store.Get(id)
}

// List returns recent generation stats.
func (n *GenerationNamespace) List(limit int) []GenerationStats {
	return n.store.List(limit)
}

// BatchNamespace provides batch operations.
type BatchNamespace struct {
	mgr *BatchManager
}

// Create creates a new batch.
func (n *BatchNamespace) Create(ctx context.Context, req BatchCreateRequest) (*BatchObject, error) {
	return n.mgr.Create(ctx, req)
}

// CreateAndPoll creates a batch and waits for completion.
func (n *BatchNamespace) CreateAndPoll(ctx context.Context, req BatchCreateRequest, opts BatchPollOptions) (*BatchResults, error) {
	return n.mgr.CreateAndPoll(ctx, req, opts)
}

// Poll waits for a batch to complete.
func (n *BatchNamespace) Poll(ctx context.Context, batchID string, opts BatchPollOptions) (*BatchResults, error) {
	return n.mgr.Poll(ctx, batchID, opts)
}

// Get returns batch metadata.
func (n *BatchNamespace) Get(batchID string) (*BatchObject, error) {
	return n.mgr.Get(batchID)
}

// List returns all batches.
func (n *BatchNamespace) List() ([]BatchObject, error) {
	return n.mgr.List()
}

// Results retrieves batch results.
func (n *BatchNamespace) Results(batchID string) (*BatchResults, error) {
	return n.mgr.Results(batchID)
}

// Cancel cancels a batch.
func (n *BatchNamespace) Cancel(ctx context.Context, batchID string) (*BatchObject, error) {
	return n.mgr.Cancel(ctx, batchID)
}

// Open creates a new BatchBuilder for fluent batch construction.
func (n *BatchNamespace) Open(config BatchBuilderConfig) *BatchBuilder {
	return NewBatchBuilder(config, n.mgr.GetStore(), n.mgr)
}
