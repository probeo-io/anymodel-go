package anymodel

import (
	"testing"
	"time"
)

func TestAdaptiveConcurrencyController(t *testing.T) {
	t.Run("starts at configured initial concurrency", func(t *testing.T) {
		c := NewAdaptiveConcurrencyController(&AdaptiveConcurrencyOptions{Initial: 10})
		if got := c.MaxConcurrency(); got != 10 {
			t.Errorf("MaxConcurrency() = %d, want 10", got)
		}
	})

	t.Run("defaults to initial=5 when no options provided", func(t *testing.T) {
		c := NewAdaptiveConcurrencyController(nil)
		if got := c.MaxConcurrency(); got != 5 {
			t.Errorf("MaxConcurrency() = %d, want 5", got)
		}
	})

	t.Run("slow-start doubles concurrency after first window", func(t *testing.T) {
		c := NewAdaptiveConcurrencyController(&AdaptiveConcurrencyOptions{Initial: 5})
		for i := 0; i < 5; i++ {
			c.RecordSuccess(nil)
		}
		if got := c.MaxConcurrency(); got != 10 {
			t.Errorf("MaxConcurrency() = %d, want 10", got)
		}
	})

	t.Run("slow-start keeps doubling each window", func(t *testing.T) {
		c := NewAdaptiveConcurrencyController(&AdaptiveConcurrencyOptions{Initial: 5})
		// Window 1: 5 successes -> 10
		for i := 0; i < 5; i++ {
			c.RecordSuccess(nil)
		}
		if got := c.MaxConcurrency(); got != 10 {
			t.Errorf("after window 1: MaxConcurrency() = %d, want 10", got)
		}
		// Window 2: 10 successes -> 20
		for i := 0; i < 10; i++ {
			c.RecordSuccess(nil)
		}
		if got := c.MaxConcurrency(); got != 20 {
			t.Errorf("after window 2: MaxConcurrency() = %d, want 20", got)
		}
		// Window 3: 20 successes -> 40
		for i := 0; i < 20; i++ {
			c.RecordSuccess(nil)
		}
		if got := c.MaxConcurrency(); got != 40 {
			t.Errorf("after window 3: MaxConcurrency() = %d, want 40", got)
		}
	})

	t.Run("slow-start reaches high concurrency quickly", func(t *testing.T) {
		c := NewAdaptiveConcurrencyController(&AdaptiveConcurrencyOptions{Initial: 5})
		// 5 -> 10 -> 20 -> 40 -> 80 -> 160
		total := 0
		for window := 5; window <= 80; window *= 2 {
			for i := 0; i < window; i++ {
				c.RecordSuccess(nil)
			}
			total += window
		}
		if got := c.MaxConcurrency(); got != 160 {
			t.Errorf("MaxConcurrency() = %d, want 160", got)
		}
		if total != 5+10+20+40+80 {
			t.Errorf("total = %d, want 155", total)
		}
	})

	t.Run("does not increase before a full window completes", func(t *testing.T) {
		c := NewAdaptiveConcurrencyController(&AdaptiveConcurrencyOptions{Initial: 5})
		for i := 0; i < 4; i++ {
			c.RecordSuccess(nil)
		}
		if got := c.MaxConcurrency(); got != 5 {
			t.Errorf("MaxConcurrency() = %d, want 5", got)
		}
	})

	t.Run("switches to additive increase after first throttle", func(t *testing.T) {
		c := NewAdaptiveConcurrencyController(&AdaptiveConcurrencyOptions{Initial: 10})
		c.RecordThrottle(0)
		if got := c.MaxConcurrency(); got != 5 {
			t.Errorf("after throttle: MaxConcurrency() = %d, want 5", got)
		}
		// Now in congestion avoidance: +1 per window, not doubling
		for i := 0; i < 5; i++ {
			c.RecordSuccess(nil)
		}
		if got := c.MaxConcurrency(); got != 6 {
			t.Errorf("after first CA window: MaxConcurrency() = %d, want 6", got)
		}
		for i := 0; i < 6; i++ {
			c.RecordSuccess(nil)
		}
		if got := c.MaxConcurrency(); got != 7 {
			t.Errorf("after second CA window: MaxConcurrency() = %d, want 7", got)
		}
	})

	t.Run("multiplicative decrease halves concurrency on throttle", func(t *testing.T) {
		c := NewAdaptiveConcurrencyController(&AdaptiveConcurrencyOptions{Initial: 10})
		c.RecordThrottle(0)
		if got := c.MaxConcurrency(); got != 5 {
			t.Errorf("MaxConcurrency() = %d, want 5", got)
		}
	})

	t.Run("respects minimum floor on throttle", func(t *testing.T) {
		c := NewAdaptiveConcurrencyController(&AdaptiveConcurrencyOptions{Initial: 1})
		c.RecordThrottle(0)
		if got := c.MaxConcurrency(); got != 1 {
			t.Errorf("MaxConcurrency() = %d, want 1 (min)", got)
		}
	})

	t.Run("respects maximum ceiling during slow-start", func(t *testing.T) {
		c := NewAdaptiveConcurrencyController(&AdaptiveConcurrencyOptions{Initial: 8, Max: 10})
		// 8 successes -> would double to 16, but clamped to 10
		for i := 0; i < 8; i++ {
			c.RecordSuccess(nil)
		}
		if got := c.MaxConcurrency(); got != 10 {
			t.Errorf("MaxConcurrency() = %d, want 10", got)
		}
		// Fill another window, should stay clamped
		for i := 0; i < 10; i++ {
			c.RecordSuccess(nil)
		}
		if got := c.MaxConcurrency(); got != 10 {
			t.Errorf("MaxConcurrency() = %d, want 10 (clamped)", got)
		}
	})

	t.Run("proactive backoff clamps to remaining-requests from headers", func(t *testing.T) {
		c := NewAdaptiveConcurrencyController(&AdaptiveConcurrencyOptions{Initial: 20})
		c.RecordSuccess(&ResponseMeta{
			Headers: map[string]string{"x-ratelimit-remaining-requests": "3"},
		})
		if got := c.MaxConcurrency(); got != 3 {
			t.Errorf("MaxConcurrency() = %d, want 3", got)
		}
	})

	t.Run("proactive backoff switches to congestion avoidance", func(t *testing.T) {
		c := NewAdaptiveConcurrencyController(&AdaptiveConcurrencyOptions{Initial: 20})
		c.RecordSuccess(&ResponseMeta{
			Headers: map[string]string{"x-ratelimit-remaining-requests": "10"},
		})
		if got := c.MaxConcurrency(); got != 10 {
			t.Errorf("MaxConcurrency() = %d, want 10", got)
		}
		// Should now be additive (+1), not doubling
		for i := 0; i < 10; i++ {
			c.RecordSuccess(nil)
		}
		if got := c.MaxConcurrency(); got != 11 {
			t.Errorf("MaxConcurrency() = %d, want 11", got)
		}
	})

	t.Run("does not reduce below min even with low remaining-requests", func(t *testing.T) {
		c := NewAdaptiveConcurrencyController(&AdaptiveConcurrencyOptions{Initial: 5, Min: 2})
		c.RecordSuccess(&ResponseMeta{
			Headers: map[string]string{"x-ratelimit-remaining-requests": "0"},
		})
		if got := c.MaxConcurrency(); got != 2 {
			t.Errorf("MaxConcurrency() = %d, want 2 (min)", got)
		}
	})

	t.Run("ignores remaining-requests when it exceeds current concurrency", func(t *testing.T) {
		c := NewAdaptiveConcurrencyController(&AdaptiveConcurrencyOptions{Initial: 5})
		c.RecordSuccess(&ResponseMeta{
			Headers: map[string]string{"x-ratelimit-remaining-requests": "1000"},
		})
		if got := c.MaxConcurrency(); got != 5 {
			t.Errorf("MaxConcurrency() = %d, want 5", got)
		}
	})

	t.Run("normalizes anthropic header names for proactive backoff", func(t *testing.T) {
		c := NewAdaptiveConcurrencyController(&AdaptiveConcurrencyOptions{Initial: 20})
		c.RecordSuccess(&ResponseMeta{
			Headers: map[string]string{
				"anthropic-ratelimit-requests-remaining": "5",
				"x-ratelimit-remaining-requests":         "5",
			},
		})
		if got := c.MaxConcurrency(); got != 5 {
			t.Errorf("MaxConcurrency() = %d, want 5", got)
		}
	})

	t.Run("sets retry-after delay on throttle", func(t *testing.T) {
		c := NewAdaptiveConcurrencyController(&AdaptiveConcurrencyOptions{Initial: 5})
		c.RecordThrottle(5000)
		delay := c.GetDelay()
		if delay <= 4900 || delay > 5000 {
			t.Errorf("GetDelay() = %d, want between 4901 and 5000", delay)
		}
	})

	t.Run("returns 0 delay when no throttle has occurred", func(t *testing.T) {
		c := NewAdaptiveConcurrencyController(nil)
		if got := c.GetDelay(); got != 0 {
			t.Errorf("GetDelay() = %d, want 0", got)
		}
	})

	t.Run("delay expires after waiting", func(t *testing.T) {
		c := NewAdaptiveConcurrencyController(&AdaptiveConcurrencyOptions{Initial: 5})
		c.RecordThrottle(50)
		if got := c.GetDelay(); got <= 0 {
			t.Error("expected positive delay immediately after throttle")
		}
		time.Sleep(60 * time.Millisecond)
		if got := c.GetDelay(); got != 0 {
			t.Errorf("GetDelay() = %d, want 0 after expiry", got)
		}
	})

	t.Run("resets success counter on throttle", func(t *testing.T) {
		c := NewAdaptiveConcurrencyController(&AdaptiveConcurrencyOptions{Initial: 5})
		for i := 0; i < 3; i++ {
			c.RecordSuccess(nil)
		}
		c.RecordThrottle(0)
		if got := c.MaxConcurrency(); got != 2 {
			t.Errorf("after throttle: MaxConcurrency() = %d, want 2", got)
		}
		// Now in congestion avoidance: 2 successes -> 3
		c.RecordSuccess(nil)
		if got := c.MaxConcurrency(); got != 2 {
			t.Errorf("after 1 success: MaxConcurrency() = %d, want 2", got)
		}
		c.RecordSuccess(nil)
		if got := c.MaxConcurrency(); got != 3 {
			t.Errorf("after 2 successes: MaxConcurrency() = %d, want 3", got)
		}
	})

	t.Run("custom decrease factor", func(t *testing.T) {
		c := NewAdaptiveConcurrencyController(&AdaptiveConcurrencyOptions{Initial: 10, DecreaseFactor: 0.75})
		c.RecordThrottle(0)
		if got := c.MaxConcurrency(); got != 7 {
			t.Errorf("MaxConcurrency() = %d, want 7", got)
		}
	})

	t.Run("slow-start resumes up to ssthresh after throttle recovery", func(t *testing.T) {
		c := NewAdaptiveConcurrencyController(&AdaptiveConcurrencyOptions{Initial: 20})
		// Ramp to 40 via slow-start
		for i := 0; i < 20; i++ {
			c.RecordSuccess(nil)
		}
		if got := c.MaxConcurrency(); got != 40 {
			t.Errorf("after ramp: MaxConcurrency() = %d, want 40", got)
		}
		// Throttle: current = 20, ssthresh = 20
		c.RecordThrottle(0)
		if got := c.MaxConcurrency(); got != 20 {
			t.Errorf("after throttle: MaxConcurrency() = %d, want 20", got)
		}
		// Next window: current (20) >= ssthresh (20), so additive increase
		for i := 0; i < 20; i++ {
			c.RecordSuccess(nil)
		}
		if got := c.MaxConcurrency(); got != 21 {
			t.Errorf("after recovery window: MaxConcurrency() = %d, want 21", got)
		}
	})
}
