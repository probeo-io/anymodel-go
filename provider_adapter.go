package anymodel

import (
	"context"
	"fmt"
	"sync"
)

// Adapter is the interface every provider must implement.
type Adapter interface {
	Name() string
	SendRequest(ctx context.Context, req ChatCompletionRequest) (*ChatCompletion, error)
	SendStreamingRequest(ctx context.Context, req ChatCompletionRequest) (<-chan ChatCompletionChunk, <-chan error)
	ListModels(ctx context.Context) ([]ModelInfo, error)
	SupportsParameter(param string) bool
	SupportsBatch() bool
}

// AdapterWithMeta extends Adapter with the ability to return response metadata
// (e.g., rate-limit headers) alongside the completion.
type AdapterWithMeta interface {
	Adapter
	SendRequestWithMeta(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionWithMeta, error)
}

// NativeBatchStatus is the status returned by a provider's native batch API.
type NativeBatchStatus struct {
	Status    BatchStatus
	Total     int
	Completed int
	Failed    int
}

// BatchAdapter is the interface for providers with native batch support.
type BatchAdapter interface {
	CreateBatch(ctx context.Context, model string, requests []BatchRequestItem, options map[string]any) (providerBatchID string, metadata map[string]any, err error)
	PollBatch(ctx context.Context, providerBatchID string) (*NativeBatchStatus, error)
	GetBatchResults(ctx context.Context, providerBatchID string) ([]BatchResultItem, error)
	CancelBatch(ctx context.Context, providerBatchID string) error
}

// Registry holds registered provider adapters.
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
}

// NewRegistry creates a new empty provider registry.
func NewRegistry() *Registry {
	return &Registry{adapters: make(map[string]Adapter)}
}

// Register adds a provider adapter.
func (r *Registry) Register(slug string, adapter Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[slug] = adapter
}

// Get returns the adapter for the given slug.
func (r *Registry) Get(slug string) (Adapter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[slug]
	if !ok {
		return nil, NewError(400, fmt.Sprintf("unknown provider: %s", slug), nil)
	}
	return a, nil
}

// Has returns true if the given slug is registered.
func (r *Registry) Has(slug string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.adapters[slug]
	return ok
}

// Slugs returns all registered provider slugs.
func (r *Registry) Slugs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	slugs := make([]string, 0, len(r.adapters))
	for s := range r.adapters {
		slugs = append(slugs, s)
	}
	return slugs
}

// All returns all registered adapters.
func (r *Registry) All() []Adapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	all := make([]Adapter, 0, len(r.adapters))
	for _, a := range r.adapters {
		all = append(all, a)
	}
	return all
}
