package anymodel

import (
	"context"
	"math"
	"math/rand"
	"time"
)

// RetryOptions controls retry behavior.
type RetryOptions struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

// DefaultRetryOptions returns sensible defaults.
func DefaultRetryOptions() RetryOptions {
	return RetryOptions{
		MaxRetries: 2,
		BaseDelay:  500 * time.Millisecond,
		MaxDelay:   10 * time.Second,
	}
}

func isRetryable(err error) bool {
	if e, ok := err.(*Error); ok {
		switch e.Code {
		case 429, 502, 503, 529:
			return true
		}
	}
	return false
}

// WithRetry executes fn with exponential backoff on retryable errors.
func WithRetry[T any](ctx context.Context, opts RetryOptions, fn func() (T, error)) (T, error) {
	var zero T
	var lastErr error

	for attempt := 0; attempt <= opts.MaxRetries; attempt++ {
		result, err := fn()
		if err == nil {
			return result, nil
		}
		lastErr = err

		if !isRetryable(err) || attempt == opts.MaxRetries {
			return zero, err
		}

		delay := time.Duration(float64(opts.BaseDelay) * math.Pow(2, float64(attempt)))
		if delay > opts.MaxDelay {
			delay = opts.MaxDelay
		}
		jitter := time.Duration(float64(delay) * (0.8 + 0.4*rand.Float64()))

		select {
		case <-ctx.Done():
			return zero, ctx.Err()
		case <-time.After(jitter):
		}
	}

	return zero, lastErr
}
