package anymodel

import (
	"sync"
	"time"
)

// RateLimitState tracks rate limit info for a provider.
type RateLimitState struct {
	Provider    string
	RetryAfter  time.Duration
	LastUpdated time.Time
}

// RateLimitTracker tracks per-provider rate limits.
type RateLimitTracker struct {
	mu    sync.RWMutex
	state map[string]*RateLimitState
}

// NewRateLimitTracker creates a new tracker.
func NewRateLimitTracker() *RateLimitTracker {
	return &RateLimitTracker{state: make(map[string]*RateLimitState)}
}

// Record records a rate limit event for a provider.
func (t *RateLimitTracker) Record(provider string, retryAfter time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state[provider] = &RateLimitState{
		Provider:    provider,
		RetryAfter:  retryAfter,
		LastUpdated: time.Now(),
	}
}

// IsRateLimited returns true if the provider is currently rate limited.
func (t *RateLimitTracker) IsRateLimited(provider string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	s, ok := t.state[provider]
	if !ok {
		return false
	}
	return time.Since(s.LastUpdated) < s.RetryAfter
}

// Clear removes rate limit state for a provider.
func (t *RateLimitTracker) Clear(provider string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.state, provider)
}
