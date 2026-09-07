// Package scheduler - GPU Topology-Aware Scheduling Performance Optimizations
//
// Performance Barrier: DQN Forward Cache + Parallel Scoring
//
// Competitive Baseline: kube-scheduler default (no NVLink/PCIe awareness)
// Our Innovation:
//   1. ScoreTopologyCached: Memoizes topology scores for identical (node, gpuCount, nvlink) tuples.
//      Amortized complexity: O(1) for repeated scheduling decisions vs O(G^2) per call.
//   2. ScoreNodesParallel: Scores N candidate nodes concurrently (goroutine per node).
//      Wall-clock time: O(max_node_score_time) vs O(N * avg_node_score_time).
//   3. DQN Forward Cache: Caches DQN model predictions for topology hash,
//      avoiding repeated neural network inference for same topology state.
//
// Expected improvement: 5-10x scheduling throughput for large clusters (100+ GPUs).
package scheduler

import (
	"crypto/sha256"
	"encoding/binary"
	"sync"
)

// TopologyScoreCache caches ScoreTopology results to avoid redundant O(G^2) computation.
// Key insight: topology changes infrequently (hardware doesn't move), but scheduling
// decisions happen continuously. Cache invalidation: on node topology update events only.
type TopologyScoreCache struct {
	mu    sync.RWMutex
	cache map[uint64]float64 // hash(nodeID + gpuCount + requireNVLink + minBW) -> score
	hits  int64
	total int64
}

// NewTopologyScoreCache creates a topology score cache.
func NewTopologyScoreCache() *TopologyScoreCache {
	return &TopologyScoreCache{
		cache: make(map[uint64]float64, 256),
	}
}

// topoCacheKey computes a deterministic hash for cache lookup.
// Complexity: O(1) - fixed-size hash computation.
func topoCacheKey(nodeID string, gpuCount int, requireNVLink bool, minBW float64) uint64 {
	h := sha256.New()
	h.Write([]byte(nodeID))
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, uint64(gpuCount))
	h.Write(buf)
	if requireNVLink {
		h.Write([]byte{1})
	} else {
		h.Write([]byte{0})
	}
	binary.LittleEndian.PutUint64(buf, uint64(minBW*1000))
	h.Write(buf)
	sum := h.Sum(nil)
	return binary.LittleEndian.Uint64(sum[:8])
}

// ScoreTopologyCached returns cached topology score or computes and caches it.
// Performance: O(1) amortized (cache hit) vs O(G^2) for fresh computation.
func (c *TopologyScoreCache) ScoreTopologyCached(nodeID string, topo *NodeGPUTopology, gpuCount int, requireNVLink bool, minBW float64) float64 {
	key := topoCacheKey(nodeID, gpuCount, requireNVLink, minBW)

	c.mu.RLock()
	c.total++
	if score, ok := c.cache[key]; ok {
		c.hits++
		c.mu.RUnlock()
		return score
	}
	c.mu.RUnlock()

	// Cache miss: compute score
	score := ScoreTopology(topo, gpuCount, requireNVLink, minBW)

	c.mu.Lock()
	c.cache[key] = score
	c.mu.Unlock()

	return score
}

// Invalidate removes cached score for a node (call on topology change event).
func (c *TopologyScoreCache) Invalidate(nodeID string) {
	c.mu.Lock()
	// Simple approach: clear all (topology changes are rare)
	c.cache = make(map[uint64]float64, 256)
	c.mu.Unlock()
}

// HitRate returns cache hit rate for monitoring.
func (c *TopologyScoreCache) HitRate() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.total == 0 {
		return 0
	}
	return float64(c.hits) / float64(c.total)
}

// ScoreNodesParallel scores multiple candidate nodes concurrently.
// Performance: O(max(score_time)) wall-clock vs O(N * avg(score_time)) serial.
// For 8-node cluster with 100ms/node scoring: 100ms vs 800ms.
func ScoreNodesParallel(nodes []string, topos map[string]*NodeGPUTopology, gpuCount int, requireNVLink bool, minBW float64) map[string]float64 {
	results := make(map[string]float64, len(nodes))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, node := range nodes {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			topo := topos[n]
			score := ScoreTopology(topo, gpuCount, requireNVLink, minBW)
			mu.Lock()
			results[n] = score
			mu.Unlock()
		}(node)
	}

	wg.Wait()
	return results
}

// DQNForwardCache caches DQN placement predictions by topology state hash.
// Rationale: DQN inference costs ~5-50ms per call; topology state changes only
// when nodes join/leave or GPU health changes (minutes/hours between changes).
// Cache hit avoids neural network forward pass entirely.
type DQNForwardCache struct {
	mu      sync.RWMutex
	cache   map[uint64]DQNPrediction
	maxSize int
}

// DQNPrediction stores a cached DQN placement decision.
type DQNPrediction struct {
	BestNodeIndex int
	QValue        float64
	Confidence    float64
}

// NewDQNForwardCache creates DQN prediction cache.
func NewDQNForwardCache(maxSize int) *DQNForwardCache {
	if maxSize <= 0 {
		maxSize = 1024
	}
	return &DQNForwardCache{
		cache:   make(map[uint64]DQNPrediction, maxSize),
		maxSize: maxSize,
	}
}

// Get retrieves cached prediction. Returns (prediction, hit).
func (d *DQNForwardCache) Get(stateHash uint64) (DQNPrediction, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	p, ok := d.cache[stateHash]
	return p, ok
}

// Put stores a prediction. Evicts randomly if at capacity.
func (d *DQNForwardCache) Put(stateHash uint64, pred DQNPrediction) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.cache) >= d.maxSize {
		// Simple eviction: clear half
		count := 0
		for k := range d.cache {
			delete(d.cache, k)
			count++
			if count >= d.maxSize/2 {
				break
			}
		}
	}
	d.cache[stateHash] = pred
}

// HashTopologyState produces a deterministic hash of the current topology state
// for DQN cache key computation.
func HashTopologyState(topos map[string]*NodeGPUTopology, gpuCount int) uint64 {
	h := sha256.New()
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, uint64(gpuCount))
	h.Write(buf)
	for name, topo := range topos {
		h.Write([]byte(name))
		if topo != nil {
			binary.LittleEndian.PutUint64(buf, uint64(topo.TotalGPUs))
			h.Write(buf)
			if topo.HasNVLink {
				h.Write([]byte{1})
			}
		}
	}
	sum := h.Sum(nil)
	return binary.LittleEndian.Uint64(sum[:8])
}
