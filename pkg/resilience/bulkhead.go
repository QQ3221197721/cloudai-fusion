package resilience

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"
)

var ErrBulkheadFull = errors.New("bulkhead full - no available slots")
var ErrRateLimited = errors.New("rate limit exceeded")

// Bulkhead provides circuit breaker pattern for resource isolation
type Bulkhead struct {
	sem      chan struct{}
	maxConc  int
	mu       sync.RWMutex
	closed   bool
	log      func(format string, args ...interface{})
}

// NewBulkhead creates a new bulkhead with specified concurrency limit
func NewBulkhead(maxConcurrency int) *Bulkhead {
	if maxConcurrency < 1 {
		maxConcurrency = 10
	}

	return &Bulkhead{
		sem:     make(chan struct{}, maxConcurrency),
		maxConc: maxConcurrency,
		log:     func(format string, args ...interface{}) {},
	}
}

// Execute acquires a slot, executes the function, and releases the slot
func (b *Bulkhead) Execute(ctx context.Context, fn func() error) error {
	b.mu.RLock()
	isClosed := b.closed
	b.mu.RUnlock()

	if isClosed {
		return errors.New("bulkhead is closed")
	}

	select {
	case b.sem <- struct{}{}:
		defer func() { <-b.sem }()
		
		return fn()
	case <-ctx.Done():
		return ctx.Err()
	default:
		return ErrBulkheadFull
	}
}

// SetClosed marks the bulkhead as closed (prevents new executions)
func (b *Bulkhead) SetClosed(closed bool) {
	b.mu.Lock()
	b.closed = closed
	b.mu.Unlock()
}

// IsOpen returns whether the bulkhead is accepting new work
func (b *Bulkhead) IsOpen() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return !b.closed
}

// Stats returns bulkhead statistics
func (b *Bulkhead) Stats() map[string]interface{} {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return map[string]interface{}{
		"max_concurrency": b.maxConc,
		"available_slots": len(b.sem),
		"closed":          b.closed,
	}
}

// TokenBucketLimiter implements token bucket algorithm for rate limiting
type TokenBucketLimiter struct {
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	mu         sync.Mutex
	lastRefill time.Time
	log        func(format string, args ...interface{})
}

// NewTokenBucketLimiter creates a new token bucket rate limiter
func NewTokenBucketLimiter(maxTokens, refillRate float64) *TokenBucketLimiter {
	if maxTokens < 1 || refillRate < 0 {
		maxTokens = 100
		refillRate = 10 // Default 10 tokens/second
	}

	return &TokenBucketLimiter{
		tokens:     maxTokens,
		maxTokens:  maxTokens,
		refillRate: refillRate,
		lastRefill: time.Now(),
		log:        func(format string, args ...interface{}) {},
	}
}

// Allow checks if one request is allowed
func (l *TokenBucketLimiter) Allow() bool {
	return l.AllowN(1)
}

// AllowN checks if n requests are allowed
func (l *TokenBucketLimiter) AllowN(n int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	delta := now.Sub(l.lastRefill).Seconds()
	
	// Refill tokens based on elapsed time
	l.tokens = math.Min(l.maxTokens, l.tokens + delta*l.refillRate)
	l.lastRefill = now
	
	// Check if we have enough tokens
	if l.tokens >= float64(n) {
		l.tokens -= float64(n)
		return true
	}
	
	return false
}

// WaitForAllow waits until a request is allowed or context expires
func (l *TokenBucketLimiter) WaitForAllow(ctx context.Context) error {
	for {
		if l.Allow() {
			return nil
		}
		
		select {
		case <-time.After(10 * time.Millisecond):
			continue
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// WaitTimeEstimate returns how long to wait before next token becomes available
func (l *TokenBucketLimiter) WaitTimeEstimate() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.tokens >= 1 {
		return 0
	}

	needed := 1 - l.tokens
	seconds := needed / l.refillRate
	return time.Duration(seconds * float64(time.Second))
}

// Stats returns rate limiter statistics
func (l *TokenBucketLimiter) Stats() map[string]interface{} {
	l.mu.Lock()
	defer l.mu.Unlock()

	return map[string]interface{}{
		"tokens_available": l.tokens,
		"max_tokens":       l.maxTokens,
		"refill_rate":      l.refillRate,
	}
}
