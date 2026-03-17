package anymodel

import (
	"sync"
	"time"
)

var (
	defaultHTTPTimeout = 120 * time.Second // 2 minutes
	flexHTTPTimeout    = 600 * time.Second // 10 minutes
	timeoutMu          sync.RWMutex
)

// SetDefaultHTTPTimeout sets the default HTTP client timeout for all provider adapters.
func SetDefaultHTTPTimeout(d time.Duration) {
	timeoutMu.Lock()
	defer timeoutMu.Unlock()
	defaultHTTPTimeout = d
}

// GetDefaultHTTPTimeout returns the current default HTTP client timeout.
func GetDefaultHTTPTimeout() time.Duration {
	timeoutMu.RLock()
	defer timeoutMu.RUnlock()
	return defaultHTTPTimeout
}

// SetFlexHTTPTimeout sets the HTTP timeout for flex/async requests.
func SetFlexHTTPTimeout(d time.Duration) {
	timeoutMu.Lock()
	defer timeoutMu.Unlock()
	flexHTTPTimeout = d
}

// GetFlexHTTPTimeout returns the current flex request timeout.
func GetFlexHTTPTimeout() time.Duration {
	timeoutMu.RLock()
	defer timeoutMu.RUnlock()
	return flexHTTPTimeout
}
