package ha

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var ErrDrainTimeout = errors.New("connection drain timeout exceeded")

// ConnectionDrainer manages graceful connection draining during shutdown
type ConnectionDrainer struct {
	activeConns  int64
	draining     bool
	mu           sync.RWMutex
	drainTimeout time.Duration
	log          func(format string, args ...interface{})
}

// NewConnectionDrainer creates a new connection drainer with specified timeout
func NewConnectionDrainer(timeout time.Duration) *ConnectionDrainer {
	if timeout < 1*time.Second || timeout > 300*time.Second {
		timeout = 30 * time.Second // Default 30s timeout
	}

	return &ConnectionDrainer{
		drainTimeout: timeout,
		log:          func(format string, args ...interface{}) {},
	}
}

// SetLogger sets custom logging function
func (cd *ConnectionDrainer) SetLogger(log func(format string, args ...interface{})) {
	cd.log = log
}

// StartDrain initiates the draining process - stops accepting new connections
func (cd *ConnectionDrainer) StartDrain(ctx context.Context) error {
	cd.mu.Lock()
	if cd.draining {
		cd.mu.Unlock()
		return nil // Already draining
	}
	cd.draining = true
	cd.mu.Unlock()

	cd.log("Starting connection drain...")
	
	// Wait for existing connections to complete
	select {
	case <-time.After(cd.drainTimeout):
		current := atomic.LoadInt64(&cd.activeConns)
		if current > 0 {
			cd.log("Warning: %d connections still active after drain timeout", current)
			return ErrDrainTimeout
		}
		cd.log("All connections drained successfully")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TrackConnection increments active connection counter
func (cd *ConnectionDrainer) TrackConnection() {
	atomic.AddInt64(&cd.activeConns, 1)
}

// ReleaseConnection decrements active connection counter
func (cd *ConnectionDrainer) ReleaseConnection() {
	atomic.AddInt64(&cd.activeConns, -1)
	
	cd.mu.RLock()
	if !cd.draining {
		cd.mu.RUnlock()
		return
	}
	cd.mu.RUnlock()
}

// WaitForDrain blocks until all connections are released or timeout
func (cd *ConnectionDrainer) WaitForDrain(timeout time.Duration) error {
	endTime := time.Now().Add(timeout)
	
	for time.Now().Before(endTime) {
		if atomic.LoadInt64(&cd.activeConns) == 0 {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	
	return ErrDrainTimeout
}

// IsActive returns whether we're currently accepting connections
func (cd *ConnectionDrainer) IsActive() bool {
	cd.mu.RLock()
	defer cd.mu.RUnlock()
	return !cd.draining
}

// Stats returns current draining statistics
func (cd *ConnectionDrainer) Stats() map[string]interface{} {
	cd.mu.RLock()
	defer cd.mu.RUnlock()

	return map[string]interface{}{
		"active_connections": atomic.LoadInt64(&cd.activeConns),
		"draining":           cd.draining,
		"timeout":            cd.drainTimeout.String(),
	}
}

// SetDraining manually sets draining mode (for testing/cleanup)
func (cd *ConnectionDrainer) SetDraining(drain bool) {
	cd.mu.Lock()
	cd.draining = drain
	cd.mu.Unlock()
}
