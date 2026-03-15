// Package middleware provides HTTP middleware components for the CloudAI Fusion API.
// Includes rate limiting, request validation, and other cross-cutting concerns.
package middleware

import (
	"fmt"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"
)

// RateLimitConfig configures the rate limiter middleware.
type RateLimitConfig struct {
	// RequestsPerSecond is the sustained rate per client IP.
	RequestsPerSecond float64
	// BurstSize is the maximum burst allowed beyond the sustained rate.
	BurstSize int
	// CleanupInterval controls how often expired limiters are purged.
	CleanupInterval time.Duration
	// MaxAge is how long an idle limiter is kept before cleanup.
	MaxAge time.Duration
}

// DefaultRateLimitConfig returns sensible defaults for API rate limiting.
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerSecond: 50,
		BurstSize:         100,
		CleanupInterval:   5 * time.Minute,
		MaxAge:            10 * time.Minute,
	}
}

type clientLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiter manages per-client token-bucket rate limiters.
type RateLimiter struct {
	clients map[string]*clientLimiter
	mu      sync.Mutex
	config  RateLimitConfig
}

// NewRateLimiter creates a new RateLimiter and starts a background cleanup goroutine.
func NewRateLimiter(cfg RateLimitConfig) *RateLimiter {
	rl := &RateLimiter{
		clients: make(map[string]*clientLimiter),
		config:  cfg,
	}
	go rl.cleanup()
	return rl
}

// getLimiter returns the rate.Limiter for a given key, creating one if needed.
func (rl *RateLimiter) getLimiter(key string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cl, ok := rl.clients[key]
	if !ok {
		limiter := rate.NewLimiter(rate.Limit(rl.config.RequestsPerSecond), rl.config.BurstSize)
		rl.clients[key] = &clientLimiter{limiter: limiter, lastSeen: time.Now()}
		return limiter
	}
	cl.lastSeen = time.Now()
	return cl.limiter
}

// cleanup periodically removes idle client limiters.
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(rl.config.CleanupInterval)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		for key, cl := range rl.clients {
			if time.Since(cl.lastSeen) > rl.config.MaxAge {
				delete(rl.clients, key)
			}
		}
		rl.mu.Unlock()
	}
}

// Middleware returns a Gin middleware that enforces per-IP rate limiting.
// When the rate limit is exceeded, it responds with a 429 Too Many Requests
// error including Retry-After and X-RateLimit-* headers.
func (rl *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.ClientIP()
		limiter := rl.getLimiter(key)

		if !limiter.Allow() {
			retryAfter := time.Second / time.Duration(rl.config.RequestsPerSecond)
			c.Header("Retry-After", fmt.Sprintf("%.0f", retryAfter.Seconds()))
			c.Header("X-RateLimit-Limit", fmt.Sprintf("%.0f", rl.config.RequestsPerSecond))
			c.Header("X-RateLimit-Remaining", "0")

			apperrors.RespondError(c, apperrors.ErrRateLimited)
			c.Abort()
			return
		}

		// Set rate limit headers for successful requests
		c.Header("X-RateLimit-Limit", fmt.Sprintf("%.0f", rl.config.RequestsPerSecond))
		c.Next()
	}
}

// GlobalRateLimit creates a simpler global (not per-client) rate limiter.
func GlobalRateLimit(rps float64, burst int) gin.HandlerFunc {
	limiter := rate.NewLimiter(rate.Limit(rps), burst)
	return func(c *gin.Context) {
		if !limiter.Allow() {
			c.Header("Retry-After", "1")
			apperrors.RespondError(c, apperrors.ErrRateLimited)
			c.Abort()
			return
		}
		c.Next()
	}
}

// EndpointRateLimit returns a middleware for rate-limiting specific endpoints
// (e.g., login, register) more aggressively.
func EndpointRateLimit(rps float64, burst int) gin.HandlerFunc {
	rl := NewRateLimiter(RateLimitConfig{
		RequestsPerSecond: rps,
		BurstSize:         burst,
		CleanupInterval:   5 * time.Minute,
		MaxAge:            10 * time.Minute,
	})
	return rl.Middleware()
}

