package anymodel

import (
	"context"
	"testing"
	"time"
)

func TestWithRetry(t *testing.T) {
	opts := RetryOptions{
		MaxRetries: 3,
		BaseDelay:  1 * time.Millisecond,
		MaxDelay:   10 * time.Millisecond,
	}

	t.Run("returns on first success", func(t *testing.T) {
		calls := 0
		result, err := WithRetry(context.Background(), opts, func() (string, error) {
			calls++
			return "ok", nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "ok" {
			t.Errorf("result = %q, want %q", result, "ok")
		}
		if calls != 1 {
			t.Errorf("calls = %d, want 1", calls)
		}
	})

	t.Run("retries on 429 and succeeds", func(t *testing.T) {
		calls := 0
		result, err := WithRetry(context.Background(), RetryOptions{
			MaxRetries: 2, BaseDelay: 1 * time.Millisecond, MaxDelay: 10 * time.Millisecond,
		}, func() (string, error) {
			calls++
			if calls == 1 {
				return "", NewError(429, "Rate limited", nil)
			}
			return "ok", nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "ok" {
			t.Errorf("result = %q, want %q", result, "ok")
		}
		if calls != 2 {
			t.Errorf("calls = %d, want 2", calls)
		}
	})

	t.Run("retries on 502 and succeeds", func(t *testing.T) {
		calls := 0
		result, err := WithRetry(context.Background(), RetryOptions{
			MaxRetries: 2, BaseDelay: 1 * time.Millisecond, MaxDelay: 10 * time.Millisecond,
		}, func() (string, error) {
			calls++
			if calls == 1 {
				return "", NewError(502, "Bad gateway", nil)
			}
			return "ok", nil
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "ok" {
			t.Errorf("result = %q, want %q", result, "ok")
		}
		if calls != 2 {
			t.Errorf("calls = %d, want 2", calls)
		}
	})

	t.Run("does not retry on 400 (non-retryable)", func(t *testing.T) {
		calls := 0
		_, err := WithRetry(context.Background(), opts, func() (string, error) {
			calls++
			return "", NewError(400, "Bad request", nil)
		})
		if err == nil {
			t.Fatal("expected error")
		}
		if err.Error() != "anymodel error 400: Bad request" {
			t.Errorf("unexpected error: %v", err)
		}
		if calls != 1 {
			t.Errorf("calls = %d, want 1", calls)
		}
	})

	t.Run("throws after max retries exhausted", func(t *testing.T) {
		calls := 0
		_, err := WithRetry(context.Background(), RetryOptions{
			MaxRetries: 2, BaseDelay: 1 * time.Millisecond, MaxDelay: 10 * time.Millisecond,
		}, func() (string, error) {
			calls++
			return "", NewError(429, "Rate limited", nil)
		})
		if err == nil {
			t.Fatal("expected error")
		}
		if calls != 3 { // initial + 2 retries
			t.Errorf("calls = %d, want 3", calls)
		}
	})

	t.Run("does not retry non-Error", func(t *testing.T) {
		calls := 0
		_, err := WithRetry(context.Background(), opts, func() (string, error) {
			calls++
			return "", &customErr{msg: "network error"}
		})
		if err == nil {
			t.Fatal("expected error")
		}
		if calls != 1 {
			t.Errorf("calls = %d, want 1", calls)
		}
	})

	t.Run("retries on 503 and 529", func(t *testing.T) {
		for _, code := range []int{503, 529} {
			t.Run("code_"+string(rune('0'+code/100))+string(rune('0'+code%100/10))+string(rune('0'+code%10)), func(t *testing.T) {
				calls := 0
				result, err := WithRetry(context.Background(), RetryOptions{
					MaxRetries: 2, BaseDelay: 1 * time.Millisecond, MaxDelay: 10 * time.Millisecond,
				}, func() (string, error) {
					calls++
					if calls == 1 {
						return "", NewError(code, "error", nil)
					}
					return "ok", nil
				})
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if result != "ok" {
					t.Errorf("result = %q, want %q", result, "ok")
				}
				if calls != 2 {
					t.Errorf("calls = %d, want 2", calls)
				}
			})
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		go func() {
			time.Sleep(5 * time.Millisecond)
			cancel()
		}()
		_, err := WithRetry(ctx, RetryOptions{
			MaxRetries: 100, BaseDelay: 50 * time.Millisecond, MaxDelay: 100 * time.Millisecond,
		}, func() (string, error) {
			calls++
			return "", NewError(429, "Rate limited", nil)
		})
		if err == nil {
			t.Fatal("expected error from context cancellation")
		}
	})
}

type customErr struct {
	msg string
}

func (e *customErr) Error() string { return e.msg }
