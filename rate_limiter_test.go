package anymodel

import (
	"testing"
	"time"
)

func TestRateLimitTracker(t *testing.T) {
	t.Run("not rate-limited by default", func(t *testing.T) {
		tracker := NewRateLimitTracker()
		if tracker.IsRateLimited("openai") {
			t.Error("should not be rate-limited by default")
		}
	})

	t.Run("tracks rate limit from Record", func(t *testing.T) {
		tracker := NewRateLimitTracker()
		tracker.Record("openai", 5*time.Second)
		if !tracker.IsRateLimited("openai") {
			t.Error("should be rate-limited after Record")
		}
	})

	t.Run("different providers are independent", func(t *testing.T) {
		tracker := NewRateLimitTracker()
		tracker.Record("openai", 5*time.Second)
		if tracker.IsRateLimited("anthropic") {
			t.Error("anthropic should not be rate-limited")
		}
	})

	t.Run("clears rate limit", func(t *testing.T) {
		tracker := NewRateLimitTracker()
		tracker.Record("openai", 5*time.Second)
		tracker.Clear("openai")
		if tracker.IsRateLimited("openai") {
			t.Error("should not be rate-limited after Clear")
		}
	})

	t.Run("expires after retry duration", func(t *testing.T) {
		tracker := NewRateLimitTracker()
		tracker.Record("openai", 1*time.Millisecond)
		time.Sleep(5 * time.Millisecond)
		if tracker.IsRateLimited("openai") {
			t.Error("rate limit should have expired")
		}
	})
}
