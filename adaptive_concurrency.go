package anymodel

import (
	"math"
	"strconv"
	"sync"
	"time"
)

// AdaptiveConcurrencyOptions configures the adaptive concurrency controller.
type AdaptiveConcurrencyOptions struct {
	Initial        int     // Starting concurrency. Default: 5
	Min            int     // Minimum concurrency floor. Default: 1
	Max            int     // Maximum concurrency ceiling. Default: 500
	DecreaseFactor float64 // Multiplicative decrease factor on 429. Default: 0.5
}

// AdaptiveConcurrencyController implements TCP-style slow-start + AIMD
// concurrency control for batch request processing.
//
// Phase 1 (slow-start): doubles concurrency each window until a 429 or
// header-driven backoff sets a threshold.
//
// Phase 2 (congestion avoidance / AIMD): additive increase (+1 per window)
// after the first throttle.
//
// On 429: multiplicative decrease (halve), set threshold to pre-throttle / 2.
type AdaptiveConcurrencyController struct {
	mu             sync.Mutex
	current        float64
	min            int
	max            int
	decreaseFactor float64
	pauseUntil     int64 // unix millis
	successCount   int
	ssthresh       float64 // Infinity = still in slow-start phase
}

// NewAdaptiveConcurrencyController creates a new controller with the given options.
// Nil options use defaults: initial=5, min=1, max=500, decreaseFactor=0.5.
func NewAdaptiveConcurrencyController(opts *AdaptiveConcurrencyOptions) *AdaptiveConcurrencyController {
	initial := 5
	minC := 1
	maxC := 500
	df := 0.5

	if opts != nil {
		if opts.Initial > 0 {
			initial = opts.Initial
		}
		if opts.Min > 0 {
			minC = opts.Min
		}
		if opts.Max > 0 {
			maxC = opts.Max
		}
		if opts.DecreaseFactor > 0 {
			df = opts.DecreaseFactor
		}
	}

	return &AdaptiveConcurrencyController{
		current:        float64(initial),
		min:            minC,
		max:            maxC,
		decreaseFactor: df,
		ssthresh:       math.Inf(1),
	}
}

// MaxConcurrency returns the current allowed concurrency level.
func (c *AdaptiveConcurrencyController) MaxConcurrency() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return int(math.Floor(c.current))
}

// RecordSuccess records a successful response. Optionally pass response meta
// to allow header-driven proactive adjustment.
func (c *AdaptiveConcurrencyController) RecordSuccess(meta *ResponseMeta) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.successCount++

	if c.successCount >= int(c.current) {
		if c.current < c.ssthresh {
			// Slow-start phase: double each window
			c.current = math.Min(c.current*2, float64(c.max))
		} else {
			// Congestion avoidance: additive increase (+1 per window)
			c.current = math.Min(c.current+1, float64(c.max))
		}
		c.successCount = 0
	}

	// Proactive backoff from rate-limit headers
	if meta != nil && meta.Headers != nil {
		remaining := meta.Headers["x-ratelimit-remaining-requests"]
		if remaining == "" {
			remaining = meta.Headers["anthropic-ratelimit-requests-remaining"]
		}
		if remaining != "" {
			if remainingNum, err := strconv.Atoi(remaining); err == nil {
				if remainingNum < int(c.current) {
					c.ssthresh = math.Max(float64(c.min), float64(remainingNum))
					c.current = math.Max(float64(c.min), float64(remainingNum))
					c.successCount = 0
				}
			}
		}
	}
}

// RecordThrottle records a rate-limit (429) response. Halves concurrency,
// sets the slow-start threshold, and optionally pauses for retryAfterMs.
func (c *AdaptiveConcurrencyController) RecordThrottle(retryAfterMs int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ssthresh = math.Max(float64(c.min), math.Floor(c.current*c.decreaseFactor))
	c.current = math.Max(float64(c.min), math.Floor(c.current*c.decreaseFactor))
	c.successCount = 0

	if retryAfterMs > 0 {
		c.pauseUntil = time.Now().UnixMilli() + retryAfterMs
	}
}

// GetDelay returns milliseconds to wait before sending the next request (0 if none).
func (c *AdaptiveConcurrencyController) GetDelay() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	delay := c.pauseUntil - time.Now().UnixMilli()
	if delay < 0 {
		return 0
	}
	return delay
}
