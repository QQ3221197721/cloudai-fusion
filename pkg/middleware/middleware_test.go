package middleware

import (
	"testing"
	"time"
)

func TestDefaultRateLimitConfig(t *testing.T) {
	cfg := DefaultRateLimitConfig()
	if cfg.RequestsPerSecond != 50 {
		t.Errorf("RequestsPerSecond = %f, want 50", cfg.RequestsPerSecond)
	}
	if cfg.BurstSize != 100 {
		t.Errorf("BurstSize = %d, want 100", cfg.BurstSize)
	}
	if cfg.CleanupInterval != 5*time.Minute {
		t.Errorf("CleanupInterval = %v, want 5m", cfg.CleanupInterval)
	}
	if cfg.MaxAge != 10*time.Minute {
		t.Errorf("MaxAge = %v, want 10m", cfg.MaxAge)
	}
}

func TestRateLimiter_GetLimiter(t *testing.T) {
	rl := &RateLimiter{
		clients: make(map[string]*clientLimiter),
		config:  DefaultRateLimitConfig(),
	}

	// First call creates a new limiter
	l1 := rl.getLimiter("10.0.0.1")
	if l1 == nil {
		t.Fatal("getLimiter should return non-nil")
	}

	// Second call returns same limiter
	l2 := rl.getLimiter("10.0.0.1")
	if l1 != l2 {
		t.Error("same key should return same limiter")
	}

	// Different key returns different limiter
	l3 := rl.getLimiter("10.0.0.2")
	if l1 == l3 {
		t.Error("different key should return different limiter")
	}
}

func TestRateLimiter_Allow(t *testing.T) {
	rl := &RateLimiter{
		clients: make(map[string]*clientLimiter),
		config: RateLimitConfig{
			RequestsPerSecond: 2,
			BurstSize:         2,
			CleanupInterval:   time.Hour,
			MaxAge:            time.Hour,
		},
	}

	limiter := rl.getLimiter("client-1")

	// First 2 requests should be allowed (burst=2)
	if !limiter.Allow() {
		t.Error("first request should be allowed")
	}
	if !limiter.Allow() {
		t.Error("second request should be allowed (within burst)")
	}
	// Third immediate request should be denied (burst exhausted)
	if limiter.Allow() {
		t.Error("third immediate request should be denied")
	}
}

func TestNewRateLimiter(t *testing.T) {
	rl := NewRateLimiter(DefaultRateLimitConfig())
	if rl == nil {
		t.Fatal("NewRateLimiter should return non-nil")
	}
	if rl.clients == nil {
		t.Error("clients map should be initialized")
	}
}
