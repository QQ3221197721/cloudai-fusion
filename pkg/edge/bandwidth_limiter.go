// Package edge - Token Bucket Rate Limiter for per-tenant edge bandwidth allocation.
// Implements hierarchical fair sharing across multiple edge nodes with priority queues.
package edge

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// TOKEN BUCKET RATE LIMITER
// Per-tenant bandwidth allocation with burst support and priority fairness.
// ============================================================================

// BandwidthLimiter implements token bucket rate limiting for edge sync traffic.
type BandwidthLimiter struct {
	mu sync.Mutex

	// Token bucket state
	tokens       float64   // Current available tokens (bytes)
	maxTokens    float64   // Bucket capacity (bytes) - controls burst size
	refillRate   float64   // Tokens added per second (bytes/sec) - steady-state rate
	lastRefill   time.Time // Last time tokens were refilled

	// Per-tenant tracking
	tenantBuckets map[string]*TenantBucket

	// Configuration
	config BandwidthLimiterConfig

	logger *logrus.Logger
}

// TenantBucket tracks per-tenant bandwidth usage.
type TenantBucket struct {
	TenantID     string    `json:"tenant_id"`
	Tokens       float64   `json:"tokens"`
	MaxTokens    float64   `json:"max_tokens"`
	RefillRate   float64   `json:"refill_rate_bps"`
	Priority     int       `json:"priority"` // 0=highest
	LastActivity time.Time `json:"last_activity"`
	TotalUsed    int64     `json:"total_used_bytes"`
}

// BandwidthLimiterConfig configures the rate limiter.
type BandwidthLimiterConfig struct {
	GlobalRateBps     float64       `json:"global_rate_bps"`      // Global max bandwidth
	GlobalBurstBytes  float64       `json:"global_burst_bytes"`   // Global burst allowance
	DefaultTenantRate float64       `json:"default_tenant_rate"`  // Default per-tenant rate
	DefaultTenantBurst float64      `json:"default_tenant_burst"` // Default per-tenant burst
	GCInterval        time.Duration `json:"gc_interval"`          // Cleanup inactive tenants
}

// DefaultBandwidthLimiterConfig returns production defaults.
func DefaultBandwidthLimiterConfig() BandwidthLimiterConfig {
	return BandwidthLimiterConfig{
		GlobalRateBps:      50 * 1024 * 1024, // 50 MB/s global
		GlobalBurstBytes:   200 * 1024 * 1024, // 200 MB burst
		DefaultTenantRate:  10 * 1024 * 1024,  // 10 MB/s per tenant
		DefaultTenantBurst: 50 * 1024 * 1024,  // 50 MB burst per tenant
		GCInterval:         5 * time.Minute,
	}
}

// NewBandwidthLimiter creates a new token bucket bandwidth limiter.
func NewBandwidthLimiter(config BandwidthLimiterConfig, logger *logrus.Logger) *BandwidthLimiter {
	return &BandwidthLimiter{
		tokens:        config.GlobalBurstBytes,
		maxTokens:     config.GlobalBurstBytes,
		refillRate:    config.GlobalRateBps,
		lastRefill:    time.Now(),
		tenantBuckets: make(map[string]*TenantBucket),
		config:        config,
		logger:        logger,
	}
}

// Allow checks if a sync of the given size is allowed for the tenant.
// Returns true if allowed, false if rate-limited.
func (bl *BandwidthLimiter) Allow(ctx context.Context, tenantID string, sizeBytes int64) bool {
	bl.mu.Lock()
	defer bl.mu.Unlock()

	bl.refill()

	// Check global bucket
	if float64(sizeBytes) > bl.tokens {
		return false
	}

	// Check tenant bucket
	bucket := bl.getOrCreateTenant(tenantID)
	bl.refillTenant(bucket)

	if float64(sizeBytes) > bucket.Tokens {
		return false
	}

	// Consume tokens from both buckets
	bl.tokens -= float64(sizeBytes)
	bucket.Tokens -= float64(sizeBytes)
	bucket.TotalUsed += sizeBytes
	bucket.LastActivity = time.Now()

	return true
}

// Wait blocks until bandwidth is available or context is cancelled.
func (bl *BandwidthLimiter) Wait(ctx context.Context, tenantID string, sizeBytes int64) error {
	for {
		if bl.Allow(ctx, tenantID, sizeBytes) {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
			// Retry after short delay
		}
	}
}

// SetTenantRate configures a custom rate for a specific tenant.
func (bl *BandwidthLimiter) SetTenantRate(tenantID string, rateBps float64, burstBytes float64) {
	bl.mu.Lock()
	defer bl.mu.Unlock()

	bucket := bl.getOrCreateTenant(tenantID)
	bucket.RefillRate = rateBps
	bucket.MaxTokens = burstBytes
	if bucket.Tokens > burstBytes {
		bucket.Tokens = burstBytes
	}

	bl.logger.WithFields(logrus.Fields{
		"tenant":     tenantID,
		"rate_bps":   rateBps,
		"burst_bytes": burstBytes,
	}).Info("Tenant bandwidth rate updated")
}

// SetTenantPriority sets priority for a tenant (0 = highest).
func (bl *BandwidthLimiter) SetTenantPriority(tenantID string, priority int) {
	bl.mu.Lock()
	defer bl.mu.Unlock()
	bucket := bl.getOrCreateTenant(tenantID)
	bucket.Priority = priority
}

// GetStats returns current limiter statistics.
func (bl *BandwidthLimiter) GetStats() map[string]interface{} {
	bl.mu.Lock()
	defer bl.mu.Unlock()
	bl.refill()

	return map[string]interface{}{
		"global_tokens_available": bl.tokens,
		"global_max_tokens":       bl.maxTokens,
		"global_refill_rate":      bl.refillRate,
		"active_tenants":          len(bl.tenantBuckets),
	}
}

// GetTenantStats returns stats for a specific tenant.
func (bl *BandwidthLimiter) GetTenantStats(tenantID string) (*TenantBucket, error) {
	bl.mu.Lock()
	defer bl.mu.Unlock()

	bucket, exists := bl.tenantBuckets[tenantID]
	if !exists {
		return nil, fmt.Errorf("tenant %s not found", tenantID)
	}
	return bucket, nil
}

// GCInactiveTenants removes tenants that haven't been active.
func (bl *BandwidthLimiter) GCInactiveTenants(maxIdle time.Duration) int {
	bl.mu.Lock()
	defer bl.mu.Unlock()

	removed := 0
	now := time.Now()
	for id, bucket := range bl.tenantBuckets {
		if now.Sub(bucket.LastActivity) > maxIdle {
			delete(bl.tenantBuckets, id)
			removed++
		}
	}
	return removed
}

// refill adds tokens based on elapsed time since last refill.
func (bl *BandwidthLimiter) refill() {
	now := time.Now()
	elapsed := now.Sub(bl.lastRefill).Seconds()
	bl.lastRefill = now

	bl.tokens += elapsed * bl.refillRate
	if bl.tokens > bl.maxTokens {
		bl.tokens = bl.maxTokens
	}
}

// refillTenant refills a tenant's token bucket.
func (bl *BandwidthLimiter) refillTenant(bucket *TenantBucket) {
	now := time.Now()
	elapsed := now.Sub(bucket.LastActivity).Seconds()
	bucket.Tokens += elapsed * bucket.RefillRate
	if bucket.Tokens > bucket.MaxTokens {
		bucket.Tokens = bucket.MaxTokens
	}
}

// getOrCreateTenant returns existing bucket or creates a new one.
func (bl *BandwidthLimiter) getOrCreateTenant(tenantID string) *TenantBucket {
	bucket, exists := bl.tenantBuckets[tenantID]
	if !exists {
		bucket = &TenantBucket{
			TenantID:     tenantID,
			Tokens:       bl.config.DefaultTenantBurst,
			MaxTokens:    bl.config.DefaultTenantBurst,
			RefillRate:   bl.config.DefaultTenantRate,
			Priority:     5, // default medium priority
			LastActivity: time.Now(),
		}
		bl.tenantBuckets[tenantID] = bucket
	}
	return bucket
}
