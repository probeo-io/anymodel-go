package anymodel

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// BatchManager handles batch lifecycle.
type BatchManager struct {
	store          *BatchStore
	registry       *Registry
	router         *Router
	batchAdapters  map[string]BatchAdapter
	aliases        map[string]string
	concurrency    int
	concurrencyMax int
	useAdaptive    bool
	pollInterval   time.Duration
	mu             sync.Mutex
}

// NewBatchManager creates a new batch manager.
// When concurrency is set to -1 (representing "auto"), adaptive concurrency is enabled.
func NewBatchManager(registry *Registry, store *BatchStore, aliases map[string]string, concurrency int, concurrencyMax int, pollInterval time.Duration, router *Router) *BatchManager {
	useAdaptive := false
	if concurrency <= 0 {
		concurrency = 5
		useAdaptive = true
	}
	if concurrencyMax <= 0 {
		concurrencyMax = 500
	}
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	return &BatchManager{
		store:          store,
		registry:       registry,
		router:         router,
		batchAdapters:  make(map[string]BatchAdapter),
		aliases:        aliases,
		concurrency:    concurrency,
		concurrencyMax: concurrencyMax,
		useAdaptive:    useAdaptive,
		pollInterval:   pollInterval,
	}
}

// GetStore returns the batch store.
func (m *BatchManager) GetStore() *BatchStore {
	return m.store
}

// RegisterBatchAdapter registers a native batch adapter for a provider.
func (m *BatchManager) RegisterBatchAdapter(providerName string, adapter BatchAdapter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.batchAdapters[providerName] = adapter
}

// Create creates a new batch.
func (m *BatchManager) Create(ctx context.Context, req BatchCreateRequest) (*BatchObject, error) {
	parsed, err := ParseModelString(req.Model, m.aliases)
	if err != nil {
		return nil, err
	}

	batchID := GenerateID("batch")
	now := time.Now().Format(time.RFC3339)

	mode := BatchConcurrent
	if req.BatchMode != "concurrent" {
		m.mu.Lock()
		_, hasNative := m.batchAdapters[parsed.Provider]
		m.mu.Unlock()
		if hasNative {
			mode = BatchNative
		}
	}

	// Resolve service_tier from request-level options or individual items
	serviceTier := ""
	if req.Options != nil && req.Options.ServiceTier != "" {
		serviceTier = req.Options.ServiceTier
	} else if len(req.Requests) > 0 && req.Requests[0].ServiceTier != "" {
		serviceTier = req.Requests[0].ServiceTier
	}

	batch := BatchObject{
		ID: batchID, Object: "batch", Status: BatchPending,
		Model: req.Model, ProviderName: parsed.Provider,
		BatchMode: mode, ServiceTier: serviceTier,
		Total: len(req.Requests),
		CreatedAt: now,
	}

	if err := m.store.Create(batch); err != nil {
		return nil, err
	}
	if err := m.store.SaveRequests(batchID, req.Requests); err != nil {
		return nil, err
	}

	if mode == BatchNative {
		go m.processNativeBatch(context.Background(), batchID, req, parsed.Provider)
	} else {
		go m.processConcurrentBatch(context.Background(), batchID, req.Model, req.Options, parsed)
	}

	return &batch, nil
}

// CreateAndPoll creates a batch and waits for completion.
func (m *BatchManager) CreateAndPoll(ctx context.Context, req BatchCreateRequest, opts BatchPollOptions) (*BatchResults, error) {
	batch, err := m.Create(ctx, req)
	if err != nil {
		return nil, err
	}
	return m.Poll(ctx, batch.ID, opts)
}

// Poll waits for a batch to complete.
func (m *BatchManager) Poll(ctx context.Context, batchID string, opts BatchPollOptions) (*BatchResults, error) {
	interval := m.pollInterval
	if opts.Interval > 0 {
		interval = time.Duration(opts.Interval * float64(time.Second))
	}

	var deadline <-chan time.Time
	if opts.Timeout > 0 {
		timer := time.NewTimer(time.Duration(opts.Timeout * float64(time.Second)))
		defer timer.Stop()
		deadline = timer.C
	}

	logEnabled := opts.LogToConsole || shouldLogPoll()

	for {
		batch, err := m.store.GetMeta(batchID)
		if err != nil {
			return nil, err
		}
		if batch == nil {
			return nil, NewError(404, "batch not found: "+batchID, nil)
		}
		if logEnabled {
			fmt.Printf("[anymodel][batch.poll] id=%s status=%s mode=%s progress=%d/%d failed=%d\n",
				batch.ID, batch.Status, batch.BatchMode, batch.Completed, batch.Total, batch.Failed)
		}
		if opts.OnProgress != nil {
			opts.OnProgress(batch)
		}
		if batch.Status == BatchCompleted || batch.Status == BatchFailed {
			return m.Results(batchID)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline:
			return nil, NewError(408, "batch poll timed out", nil)
		case <-time.After(interval):
		}
	}
}

// Get returns batch metadata.
func (m *BatchManager) Get(batchID string) (*BatchObject, error) {
	return m.store.GetMeta(batchID)
}

// List returns all batches.
func (m *BatchManager) List() ([]BatchObject, error) {
	ids, err := m.store.ListBatches()
	if err != nil {
		return nil, err
	}
	var batches []BatchObject
	for _, id := range ids {
		batch, err := m.store.GetMeta(id)
		if err != nil || batch == nil {
			continue
		}
		batches = append(batches, *batch)
	}
	return batches, nil
}

// Results retrieves batch results with usage summary.
func (m *BatchManager) Results(batchID string) (*BatchResults, error) {
	batch, err := m.store.GetMeta(batchID)
	if err != nil || batch == nil {
		return nil, NewError(404, "batch not found: "+batchID, nil)
	}
	results, err := m.store.GetResults(batchID)
	if err != nil {
		return nil, err
	}
	var totalPrompt, totalCompletion int
	for _, r := range results {
		if r.Response != nil {
			totalPrompt += r.Response.Usage.PromptTokens
			totalCompletion += r.Response.Usage.CompletionTokens
		}
	}
	estimatedCost := CalculateCost(batch.Model, totalPrompt, totalCompletion)
	// Native batch APIs are 50% off; concurrent with flex is also 50% off
	var batchDiscount float64
	if batch.BatchMode == BatchNative {
		batchDiscount = 0.5
	} else if batch.ServiceTier == "flex" {
		batchDiscount = 0.5
	} else {
		batchDiscount = 1.0
	}
	estimatedCost *= batchDiscount

	return &BatchResults{
		ID: batchID, Status: batch.Status, Results: results,
		UsageSummary: BatchUsageSummary{
			TotalPromptTokens: totalPrompt, TotalCompletionTokens: totalCompletion,
			EstimatedCost: estimatedCost,
		},
	}, nil
}

// Cancel cancels a batch.
func (m *BatchManager) Cancel(ctx context.Context, batchID string) (*BatchObject, error) {
	batch, err := m.store.GetMeta(batchID)
	if err != nil || batch == nil {
		return nil, NewError(404, "batch not found: "+batchID, nil)
	}
	batch.Status = BatchCancelled
	now := time.Now().Format(time.RFC3339)
	batch.CompletedAt = &now
	if err := m.store.UpdateMeta(*batch); err != nil {
		return nil, err
	}
	return batch, nil
}

func (m *BatchManager) processNativeBatch(ctx context.Context, batchID string, req BatchCreateRequest, providerName string) {
	m.mu.Lock()
	adapter, ok := m.batchAdapters[providerName]
	m.mu.Unlock()
	if !ok {
		return
	}

	providerBatchID, metadata, err := adapter.CreateBatch(ctx, req.Model, req.Requests, nil)
	if err != nil {
		m.failBatch(batchID)
		return
	}

	state := map[string]any{"provider_batch_id": providerBatchID}
	for k, v := range metadata {
		state[k] = v
	}
	m.store.SaveProviderState(batchID, state)

	if batch, _ := m.store.GetMeta(batchID); batch != nil {
		batch.Status = BatchProcessing
		m.store.UpdateMeta(*batch)
	}
}

func (m *BatchManager) processConcurrentBatch(ctx context.Context, batchID string, model string, options *BatchCreateOptions, parsed *ParsedModel) {
	if batch, _ := m.store.GetMeta(batchID); batch != nil {
		batch.Status = BatchProcessing
		m.store.UpdateMeta(*batch)
	}

	// Set up adaptive concurrency controller if enabled
	var controller *AdaptiveConcurrencyController
	if m.useAdaptive {
		controller = NewAdaptiveConcurrencyController(&AdaptiveConcurrencyOptions{
			Initial: m.concurrency,
			Max:     m.concurrencyMax,
		})
	}

	getLimit := func() int {
		if controller != nil {
			return controller.MaxConcurrency()
		}
		return m.concurrency
	}

	// Stream requests from disk instead of holding all in memory
	itemCh := make(chan BatchRequestItem, m.concurrency)
	if err := m.store.StreamRequests(batchID, itemCh); err != nil {
		m.failBatch(batchID)
		return
	}

	sem := make(chan struct{}, getLimit())
	var wg sync.WaitGroup
	var completed, failed int
	var mu sync.Mutex

	for item := range itemCh {
		// Check for cancellation
		if batch, _ := m.store.GetMeta(batchID); batch != nil && batch.Status == BatchCancelled {
			break
		}

		// Respect rate-limit pause from the controller
		if controller != nil {
			if delay := controller.GetDelay(); delay > 0 {
				time.Sleep(time.Duration(delay) * time.Millisecond)
			}
		}

		wg.Add(1)
		sem <- struct{}{}

		go func(item BatchRequestItem) {
			defer wg.Done()
			defer func() { <-sem }()

			completionReq := ChatCompletionRequest{
				Model: model, Messages: item.Messages,
			}
			if item.MaxTokens != nil {
				completionReq.MaxTokens = item.MaxTokens
			} else if options != nil && options.MaxTokens != nil {
				completionReq.MaxTokens = options.MaxTokens
			}
			if item.Temperature != nil {
				completionReq.Temperature = item.Temperature
			} else if options != nil && options.Temperature != nil {
				completionReq.Temperature = options.Temperature
			}
			if item.ServiceTier != "" {
				completionReq.ServiceTier = item.ServiceTier
			} else if options != nil && options.ServiceTier != "" {
				completionReq.ServiceTier = options.ServiceTier
			}

			var resultItem BatchResultItem

			if m.router != nil && controller != nil {
				// Use CompleteWithMeta for adaptive concurrency
				withMeta, err := m.router.CompleteWithMeta(ctx, completionReq)
				if err != nil {
					if e, ok := err.(*Error); ok && e.Code == 429 {
						controller.RecordThrottle(0)
					}
					resultItem = BatchResultItem{
						CustomID: item.CustomID, Status: "error",
						Error: &BatchError{Code: 500, Message: err.Error()},
					}
				} else {
					controller.RecordSuccess(&withMeta.Meta)
					resultItem = BatchResultItem{
						CustomID: item.CustomID, Status: "success", Response: withMeta.Completion,
					}
				}
			} else {
				// Fallback to direct adapter call
				adapter, err := m.registry.Get(parsed.Provider)
				if err != nil {
					resultItem = BatchResultItem{
						CustomID: item.CustomID, Status: "error",
						Error: &BatchError{Code: 500, Message: err.Error()},
					}
				} else {
					result, err := adapter.SendRequest(ctx, completionReq)
					if err != nil {
						resultItem = BatchResultItem{
							CustomID: item.CustomID, Status: "error",
							Error: &BatchError{Code: 500, Message: err.Error()},
						}
					} else {
						resultItem = BatchResultItem{
							CustomID: item.CustomID, Status: "success", Response: result,
						}
					}
				}
			}

			mu.Lock()
			defer mu.Unlock()

			if resultItem.Status == "success" {
				completed++
			} else {
				failed++
			}
			m.store.AppendResult(batchID, resultItem)

			if batch, _ := m.store.GetMeta(batchID); batch != nil {
				batch.Completed = completed
				batch.Failed = failed
				m.store.UpdateMeta(*batch)
			}
		}(item)
	}

	wg.Wait()

	if batch, _ := m.store.GetMeta(batchID); batch != nil {
		if failed > 0 && completed == 0 {
			batch.Status = BatchFailed
		} else {
			batch.Status = BatchCompleted
		}
		now := time.Now().Format(time.RFC3339)
		batch.CompletedAt = &now
		batch.Completed = completed
		batch.Failed = failed
		m.store.UpdateMeta(*batch)
	}
}

func shouldLogPoll() bool {
	v := strings.ToLower(os.Getenv("ANYMODEL_BATCH_POLL_LOG"))
	return v == "1" || v == "true" || v == "yes"
}

func (m *BatchManager) failBatch(batchID string) {
	if batch, _ := m.store.GetMeta(batchID); batch != nil {
		batch.Status = BatchFailed
		now := time.Now().Format(time.RFC3339)
		batch.CompletedAt = &now
		m.store.UpdateMeta(*batch)
	}
}
