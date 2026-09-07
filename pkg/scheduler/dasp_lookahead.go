package scheduler

import (
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// DemandCache caches GPU classifications to avoid repeated O(N×C) work across placement decisions
// 
// Key optimization: When the demand distribution remains stable, reuse cached GPU classifications
// instead of recomputing them. This reduces per-placement cost from O(N×C) to O(N).
type DemandCache struct {
	mu sync.RWMutex
	
	// lastDistribution holds the demand map that produced the current classification
	lastDistribution map[string]float64
	
	// lastClassification contains the computed GPU classes for cached state
	lastClassification []GPUClass
	
	// timestamp marks when this cache entry became valid
	timestamp time.Time
	
	// ttl is the maximum validity window for cache entries
	ttl time.Duration
	
	// placementCount tracks how many placements have used this cache since refresh
	placementCount int
	
	// maxPlacements forces refresh after N placements even if distribution appears unchanged
	maxPlacements int
	
	// version increments on every actual recomputation for audit trails
	version int64
	
	// totalReclaims counts how many times cache was stale but reusable
	totalReclaims int64
	
	// totalForcedRefreshes counts forced evictions due to TTL/maxPlacements
	totalForcedRefreshes int64
}

const (
	// defaultCacheTTL defines how long a classification remains fresh under stable conditions
	defaultCacheTTL = 1 * time.Second
	
	// defaultMaxPlacements bounds cache lifetime by placement count to prevent stale state
	defaultMaxPlacements = 100
	
	// distributionEqThreshold allows small numerical drift in distribution floats
	distributionEqThreshold = 1e-9
)

// NewDemandCache creates a fresh cache with sensible defaults for production workloads
func NewDemandCache() *DemandCache {
	return &DemandCache{
		lastDistribution: make(map[string]float64),
		lastClassification: make([]GPUClass, 0),
		ttl:              defaultCacheTTL,
		maxPlacements:    defaultMaxPlacements,
		version:          0,
		totalReclaims:    0,
		totalForcedRefreshes: 0,
	}
}

// GetOrCreate returns cached GPU classification if still valid, otherwise recomputes it
//
// Performance characteristics:
// - Cache hit (fresh + stable dist): O(N) copy operation
// - Cache miss (stale OR unstable dist): O(N×C) full reclassification
//
// Thread safety: RLock during read, full Lock during write
func (dc *DemandCache) GetOrCreate(gpus []GPUTopology, dist map[string]float64) ([]GPUClass, error) {
	dc.mu.RLock()
	isStale := dc.isExpired() || dc.placementCount >= dc.maxPlacements
	dc.mu.RUnlock()
	
	if !isStale && distributionNearlyEquals(dc.lastDistribution, dist) {
		// ===== CACHE HIT: O(N) copy instead of O(N×C) =====
		// Return cloned copy to prevent external mutation attacks on cache internals
		dc.mu.RLock()
		result := make([]GPUClass, len(dc.lastClassification))
		copy(result, dc.lastClassification)
		dc.mu.RUnlock()
		
		dc.mu.Lock()
		dc.placementCount++
		dc.mu.Unlock()
		
		return result, nil
	}
	
	// ===== CACHE MISS: Need expensive O(N×C) reclassification =====
	dc.mu.Lock()
	defer dc.mu.Unlock()
	
	// Extract large zone threshold from distribution (keys starting with "large")
	var largeZoneThreshold int
	for key := range dist {
		if len(key) >= 5 && key[:5] == "large" {
			largeZoneThreshold++
		}
	}
	if largeZoneThreshold == 0 {
		largeZoneThreshold = 1 // Default: first GPU in large zone
	}
	
	result := classifyAllGPUs(gpus, largeZoneThreshold, dist)
	
	// Atomically update cache state
	dc.lastClassification = result
	dc.lastDistribution = copyDistribution(dist)
	dc.timestamp = time.Now()
	dc.placementCount = 1
	atomic.AddInt64(&dc.version, 1)
	
	return result, nil
}

// IsFresh reports whether current cache entry can be reused without checking distribution
// Used by callers who know distribution hasn't changed and want to skip comparison overhead
func (dc *DemandCache) IsFresh() bool {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	return !dc.isExpired() && dc.placementCount < dc.maxPlacements
}

// Reset clears all cached data immediately (useful after topology changes or failures)
func (dc *DemandCache) Reset() {
	dc.mu.Lock()
	dc.placementCount = 0
	// Keep capacity but zero out length for GC-friendly reclaim
	dc.lastClassification = dc.lastClassification[:0]
	dc.lastDistribution = clearMap(dc.lastDistribution)
	dc.timestamp = time.Time{}
	dc.mu.Unlock()
	
	// Increment fake version so next GetOrCreate knows we intentionally invalidated
	atomic.AddInt64(&dc.totalForcedRefreshes, 1)
}

// Stats exposes cache performance metrics for monitoring/debugging
func (dc *DemandCache) Stats() DemandCacheStats {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	
	cacheAge := time.Since(dc.timestamp)
	cacheHitWindow := !dc.isExpired() && dc.placementCount < dc.maxPlacements
	
	return DemandCacheStats{
		Version:            atomic.LoadInt64(&dc.version),
		CacheAge:           cacheAge,
		IsFresh:            cacheHitWindow,
		PlacementsSinceRefresh: dc.placementCount,
		TTLSeconds:         dc.ttl.Seconds(),
		MaxPlacements:      dc.maxPlacements,
		TotalReclaims:      atomic.LoadInt64(&dc.totalReclaims),
		TotalForcedRefreshes: atomic.LoadInt64(&dc.totalForcedRefreshes),
	}
}

// isExpired checks if cache has exceeded its TTL (internal lock assumed held)
func (dc *DemandCache) isExpired() bool {
	if dc.timestamp.IsZero() {
		return true // Never initialized
	}
	return time.Since(dc.timestamp) > dc.ttl
}

// distributionNearlyEquals compares two demand maps with float tolerance
// Handles edge cases: missing keys → 0.0, small differences within epsilon
func distributionNearlyEquals(a, b map[string]float64) bool {
	// Fast path: same reference means identical pointer
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	
	// Different sizes → must compare all keys
	if len(a) != len(b) {
		return false
	}
	
	for k, va := range a {
		vb, exists := b[k]
		if !exists {
			// Key missing in b → check if va ≈ 0.0
			if math.Abs(va) > distributionEqThreshold {
				return false
			}
			continue
		}
		
		diff := math.Abs(va - vb)
		if diff > distributionEqThreshold {
			return false
		}
	}
	
	return true
}

// copyDistribution creates an independent clone of a demand map
func copyDistribution(src map[string]float64) map[string]float64 {
	dst := make(map[string]float64, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// clearMap empties a map while preserving its underlying array capacity
func clearMap(m map[string]float64) map[string]float64 {
	// Zero out values then reset length
	for k := range m {
		m[k] = 0.0
	}
	
	result := make(map[string]float64, len(m))
	return result
}

// DemandCacheStats exposes internal cache metrics for observability
type DemandCacheStats struct {
	Version            int64
	CacheAge           time.Duration
	IsFresh            bool
	PlacementsSinceRefresh int
	TTLSeconds         float64
	MaxPlacements      int
	TotalReclaims      int64
	TotalForcedRefreshes   int64
}

// classifyAllGPUs categorizes each GPU into zones/classes based on capacity and workload distribution
func classifyAllGPUs(gpus []GPUTopology, largeZoneThreshold int, dist map[string]float64) []GPUClass {
	classifications := make([]GPUClass, len(gpus))

	for i, gpu := range gpus {
		gpuState := gpu.State
		var freeSlices int
		var hasLargeCapacity bool

		// 获取 GPU 状态（无锁版本）
		if gpuState != nil && len(gpuState.Slices) > 0 {
			freeSlices = countFreeSlicesUnlocked(gpuState)
			hasLargeCapacity = hasContiguousRegionUnlocked(gpuState, 7) ||
			                   hasContiguousRegionUnlocked(gpuState, 4) ||
			                   hasContiguousRegionUnlocked(gpuState, 3)
		}

		if freeSlices == totalSlices {
			classifications[i] = ClassClean
		} else if freeSlices == 0 {
			classifications[i] = ClassFull
		} else if !isInLargeZone(i, largeZoneThreshold) && !hasLargeCapacity {
			classifications[i] = ClassSmallOnly
		} else if isInLargeZone(i, largeZoneThreshold) && hasLargeCapacity {
			classifications[i] = ClassLargeCap
		} else {
			classifications[i] = ClassClean
		}
	}

	return classifications
}

// isInLargeZone determines if GPU at index belongs to large-zone partition
func isInLargeZone(idx, largeZoneThreshold int) bool {
	return idx < largeZoneThreshold
}
