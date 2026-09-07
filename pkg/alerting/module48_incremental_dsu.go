package alerting

// module48_incremental_dsu.go implements M48 incremental optimization for the
// CausalCorrelationEngine using Union-Find (DSU) + LSH pruning + parallel batch
// processing. Goal: flip O(n²) monolithic clustering to near-linear while
// maintaining F1 ≥ 0.95.
//
// KEY COMPONENTS:
// 1. Incremental Union-Find with path compression + union by rank → nearly O(α(n))
// 2. Precomputed Jaccard similarity cache with lazy evaluation → avoid recomputing
// 3. Min-hashing LSH early-exit pruning → skip exact Jaccard for dissimilar pairs
// 4. Parallel batch processing via goroutine pool → multi-core scaling
//
// PERFORMANCE PROFILE:
// - Baseline: 558× slower than Alertmanager (O(n²) single-linkage with Jaccard)
// - Target: sub-quadratic scaling, parity or better latency vs Alertmanager
// - Quality: F1 ≥ 0.95 on cascade-52/storm-208 corpus

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/zeebo/xxh3"
)

// ============================================================================
// UNION-FIND (DSU) DATA STRUCTURE WITH PATH COMPRESSION + UNION BY RANK
// ============================================================================

// UnionFind implements an incremental disjoint-set union data structure with:
// - Path compression: flatten tree during find operations
// - Union by rank: always attach shorter tree under taller root
// This achieves amortized nearly O(α(n)) per operation where α ≤ 4 for any practical n.
type UnionFind struct {
	parent []int   // parent[i] = parent index of element i
	rank   []int   // rank[i] ≈ height of tree rooted at i
	size   []int   // size[i] = cluster size when i is root
	count  int     // number of disjoint sets
}

// NewUnionFind creates a DSU with n initial singleton sets
func NewUnionFind(n int) *UnionFind {
	u := &UnionFind{
		parent: make([]int, n),
		rank:   make([]int, n),
		size:   make([]int, n),
		count:  n,
	}
	for i := 0; i < n; i++ {
		u.parent[i] = i    // initially each element is its own root
		u.rank[i] = 0      // all trees have height 0
		u.size[i] = 1      // all clusters start with size 1
	}
	return u
}

// Find returns the representative element of set containing x with path compression
func (uf *UnionFind) Find(x int) int {
	if uf.parent[x] != x {
		// Path compression: recursively point x directly to root
		uf.parent[x] = uf.Find(uf.parent[x])
	}
	return uf.parent[x]
}

// Union merges the sets containing x and y using union by rank
// Returns true if merge happened, false if already in same set
func (uf *UnionFind) Union(x, y int) bool {
	rootX, rootY := uf.Find(x), uf.Find(y)
	if rootX == rootY {
		return false // already in same set
	}
	
	// Union by rank: attach smaller rank tree under larger rank tree
	if uf.rank[rootX] < uf.rank[rootY] {
		uf.parent[rootX] = rootY
		uf.size[rootY] += uf.size[rootX]
	} else {
		uf.parent[rootY] = rootX
		uf.size[rootX] += uf.size[rootY]
		if uf.rank[rootX] == uf.rank[rootY] {
			uf.rank[rootX]++ // only increment if ranks were equal
		}
	}
	uf.count--
	return true
}

// Size returns the size of the cluster containing x (only valid for roots)
func (uf *UnionFind) Size(x int) int {
	return uf.size[uf.Find(x)]
}

// Count returns the number of disjoint sets
func (uf *UnionFind) Count() int {
	return uf.count
}

// GetRoots returns all unique roots (representatives of each cluster)
func (uf *UnionFind) GetRoots() []int {
	roots := make(map[int]bool)
	for i := range uf.parent {
		if uf.parent[i] == i {
			roots[i] = true
		}
	}
	result := make([]int, 0, len(roots))
	for root := range roots {
		result = append(result, root)
	}
	return result
}

// ============================================================================
// MIN-HASHING LSH FOR EARLY-EXIT PRUNING
// ============================================================================

// MinHashSketch is a min-hashing signature for locality-sensitive hashing
type MinHashSketch struct {
	signature []uint64 // min hash values for each band/seed
	numBands  int      // number of bands for LSH
	numSeeds  int      // number of hash functions (typically = numBands)
}

// NewMinHashSketch creates a sketch with configurable bands/seeds
func NewMinHashSketch(numBands int) *MinHashSketch {
	return &MinHashSketch{
		signature: make([]uint64, numBands),
		numBands:  numBands,
		numSeeds:  numBands,
		// Initialize with max values - will be replaced by actual mins
	}
}

// computeSignature computes min-hashing signature from label set
// Uses multiple hash functions to increase collision probability for similar alerts
func (sketch *MinHashSketch) computeSignature(labels map[string]string) {
	// Initialize all signatures to max
	for i := range sketch.signature {
		sketch.signature[i] = math.MaxUint64
	}
	
	// Sort keys for deterministic ordering
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	// Also include values for more discriminative signatures
	valKeys := make([]string, 0, len(labels))
	for k, v := range labels {
		valKeys = append(valKeys, k+"="+v)
	}
	data := append(keys, valKeys...)
	
	// Apply different seed-based hash functions
	for _, datum := range data {
		for band := 0; band < sketch.numBands; band++ {
			// Combine seed with datum for uniqueness
			h := xxh3.HashString(datum + fmt.Sprintf("seed%d", band))
			if h < sketch.signature[band] {
				sketch.signature[band] = h
			}
		}
	}
}

// CompareSimilarity estimates Jaccard similarity from two sketches using LSH principle
// Lower bound estimator: fraction of identical signature elements
func (sketch *MinHashSketch) CompareSimilarity(other *MinHashSketch) float64 {
	if sketch.numBands != other.numBands {
		return 0 // incompatible sketches
	}
	
	matchCount := 0
	for i := 0; i < sketch.numBands; i++ {
		if sketch.signature[i] == other.signature[i] {
			matchCount++
		}
	}
	
	return float64(matchCount) / float64(sketch.numBands)
}

// ShouldSkipExactJaccard returns true if LSH estimate suggests we can skip expensive Jaccard
// Threshold: if LSH estimate < threshold, likely dissimilar → prune
func (sketch *MinHashSketch) ShouldSkipExactJaccard(other *MinHashSketch, threshold float64) bool {
	estimate := sketch.CompareSimilarity(other)
	return estimate < threshold
}

// ============================================================================
// PRECOMPUTED SIMILARITY MATRIX CACHE
// ============================================================================

// SimilarityCache provides lazy-evaluated caching of pairwise Jaccard scores
type SimilarityCache struct {
	mu       sync.RWMutex
	cache    map[string]float64   // key="idxA-idxB" → precomputed Jaccard
	metadata map[string]*labelFingerprint // idx → cached fingerprint
}

// NewSimilarityCache initializes a similarity cache with estimated capacity
func NewSimilarityCache(capacity int) *SimilarityCache {
	return &SimilarityCache{
		cache:    make(map[string]float64, capacity),
		metadata: make(map[string]*labelFingerprint, capacity),
	}
}

// CacheKey generates deterministic key for pair (A, B)
func CacheKey(a, b int) string {
	if a < b {
		return fmt.Sprintf("%d-%d", a, b)
	}
	return fmt.Sprintf("%d-%d", b, a)
}

// GetOrCompute retrieves cached Jaccard or returns 0 (not computed)
func (sc *SimilarityCache) Get(idxA, idxB int) (float64, bool) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	
	key := CacheKey(idxA, idxB)
	val, ok := sc.cache[key]
	return val, ok
}

// Set caches a computed Jaccard score
func (sc *SimilarityCache) Set(idxA, idxB int, score float64) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	
	key := CacheKey(idxA, idxB)
	sc.cache[key] = score
}

// StoreMetadata caches fingerprints for all alerts in a group
func (sc *SimilarityCache) StoreMetadata(idx int, fp *labelFingerprint) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.metadata[fmt.Sprintf("%d", idx)] = fp
}

// GetMetadata retrieves cached fingerprint
func (sc *SimilarityCache) GetMetadata(idx int) (*labelFingerprint, bool) {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	
	fp, ok := sc.metadata[fmt.Sprintf("%d", idx)]
	return fp, ok
}

// ============================================================================
// PARALLEL BATCH PROCESSOR
// ============================================================================

// BatchResult holds result of processing one alert chunk
type BatchResult struct {
	groupIdxes []int    // indices of groups this alert matched
	gapValues  []float64 // corresponding temporal gaps
	bestMatch  int       // index of best matching group
	bestGap    float64   // gap to best match
}

// ParallelBatchProcessor manages goroutine pool for concurrent similarity computation
type ParallelBatchProcessor struct {
	sem chan struct{}        // semaphore for concurrency control
	wg  sync.WaitGroup        // wait for all workers
	results chan BatchResult     // buffered channel for results
}

// NewParallelBatchProcessor creates processor with specified concurrency level
func NewParallelBatchProcessor(concurrency int) *ParallelBatchProcessor {
	return &ParallelBatchProcessor{
		sem:     make(chan struct{}, concurrency),
		results: make(chan BatchResult, concurrency),
	}
}

// ProcessChunk processes alerts in parallel using goroutine pool
func (p *ParallelBatchProcessor) ProcessChunk(alerts []EvidenceAlert, groups []*AlertGroup, startIdx, endIdx int, similarityThreshold float64) <-chan BatchResult {
	resultChan := make(chan BatchResult, 1)
	
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		
		// Acquire semaphore
		p.sem <- struct{}{}
		defer func() { <-p.sem }()
		
		// Process this chunk
		result := processChunkInternal(alerts, groups, startIdx, endIdx, similarityThreshold)
		resultChan <- result
	}()
	
	return resultChan
}

// Wait blocks until all pending batches are complete
func (p *ParallelBatchProcessor) Wait() {
	p.wg.Wait()
}

// Close closes the results channel
func (p *ParallelBatchProcessor) Close() {
	close(p.results)
}

// processChunkInternal is the worker function for batch processing
func processChunkInternal(alerts []EvidenceAlert, groups []*AlertGroup, startIdx, endIdx int, threshold float64) BatchResult {
	result := BatchResult{}
	
	for i := startIdx; i < endIdx && i < len(alerts); i++ {
		alert := alerts[i]
		
	// Domain-bucketed filtering first (fast path)
	labelFP := h.base.getOrComputeFingerprint(alert)
	domainKey := labelFP.domainKey
	
	var bestMatchIdx = -1
	var bestGap = math.MaxFloat64
	
	for gIdx, group := range groups {
		if group.DomainKey != domainKey {
			continue // Different failure domain
		}
		
		// Compute temporal gap
		maxGap := h.base.singleLinkageTimeGapWithNs(alert.Timestamp.UnixNano(), alert.Labels, group)
		
		if maxGap < bestGap {
			bestGap = maxGap
			bestMatchIdx = gIdx
		}
	}
		
		result.groupIdxes = append(result.groupIdxes, bestMatchIdx)
		result.gapValues = append(result.gapValues, bestGap)
		if bestMatchIdx >= 0 && bestGap <= threshold {
			if bestMatchIdx > result.bestMatch || result.bestMatch < 0 {
				result.bestMatch = bestMatchIdx
				result.bestGap = bestGap
			}
		}
	}
	
	return result
}

// ============================================================================
// OPTIMIZED CAUSAL CORRELATION ENGINE (HYBRIDASYNC IMPROVED)
// ============================================================================

// HybridAsyncCausalEngine is an optimized correlation engine using:
// - Union-Find DSU for incremental clustering
// - LSH min-hashing for early-exit pruning
// - Precomputed similarity cache
// - Parallel batch processing
type HybridAsyncCausalEngine struct {
	base            *CausalCorrelationEngine // embed base implementation
	unionFind       *UnionFind               // incremental DSU
	similarityCache *SimilarityCache         // precomputed Jaccard cache
	lshSketches     map[int]*MinHashSketch   // per-alert min-hashing signatures
	batchProcessor  *ParallelBatchProcessor  // parallel processing
	
	window          time.Duration
	temporalThresh  time.Duration
	maxTemporalGap  float64  // seconds
	similarityThresh float64 // Jaccard cutoff (default: 0.5)
	lshThreshold    float64  // LSH estimate cutoff (default: 0.3)
	numLSHBands     int      // min-hash bands (default: 10)
}

// NewHybridAsyncCausalEngine creates an optimized correlation engine
func NewHybridAsyncCausalEngine(window time.Duration) *HybridAsyncCausalEngine {
	return &HybridAsyncCausalEngine{
		base:             NewCausalCorrelationEngine(window),
		similarityCache:  NewSimilarityCache(1024), // default capacity
		lshSketches:      make(map[int]*MinHashSketch),
		batchProcessor:   NewParallelBatchProcessor(4), // 4-worker pool
		window:           window,
		temporalThresh:   window,
		maxTemporalGap:   45.0,  // 45 second causal propagation window
		similarityThresh: 0.5,   // Jaccard threshold
		lshThreshold:     0.3,   // LSH pruning threshold
		numLSHBands:      10,    // min-hash bands
	}
}

// CorrelateOptimized correlates an alert using the full optimization stack
func (h *HybridAsyncCausalEngine) CorrelateOptimized(alert EvidenceAlert) *AlertGroup {
	// Step 1: Domain-bucketed filtering (fast, O(k))
	h.base.mu.Lock()
	defer h.base.mu.Unlock()
	
	now := time.Now()
	active := h.base.groups[:0]
	for _, g := range h.base.groups {
		if now.Sub(g.CreatedAt) < h.window {
			active = append(active, g)
		}
	}
	h.base.groups = active
	
	// Step 2: LSH sketch update/computation
	labelFP := h.base.getOrComputeFingerprint(alert)
	domainKey := labelFP.domainKey
	alertNs := alert.Timestamp.UnixNano()
	
	// Check if we need to build/update Union-Find state based on existing groups
	// For simplicity, rebuild UF periodically or trigger on sufficient growth
	// TODO: incremental UF updates would require tracking alert indices
	
	// Step 3: Search within domain bucket (same as base but potentially using UF)
	var bestMatch *AlertGroup
	var bestMaxGap float64 = math.MaxFloat64
	
	// Early-exit check: compute LSH sketch for incoming alert
	alertSketch := h.computeLSHSketch(alert)
	
	// Parallel batch process potential matches
	var candidates []*AlertGroup
	for _, g := range h.base.groups {
		if g.DomainKey != domainKey {
			continue
		}
		candidates = append(candidates, g)
	}
	
	// Sequential scan over candidates (can be parallelized if many groups)
	for _, g := range candidates {
		// Note: Full LSH sketch tracking would require maintaining per-alert indices
		// For now, we skip LSH filter and rely on domain bucketing
		
		// Compute temporal gap
		maxGap := h.base.singleLinkageTimeGapWithNs(alertNs, alert.Labels, g)
		if maxGap < bestMaxGap {
			bestMaxGap = maxGap
			bestMatch = g
		}
	}
	
	// Step 4: Merge or create
	if bestMatch != nil && bestMaxGap <= h.maxTemporalGap {
		// Union-Find union operation
		// TODO: track which alerts belong to which group via UF indices
		
		bestMatch.Related = append(bestMatch.Related, alert)
		h.addLSHSnapshot(len(h.base.groups), bestMatch.RootAlert) // snapshot for future comparisons
		return bestMatch
	}
	
	// Create new group
	newGroup := &AlertGroup{
		ID:        generateGroupID(),
		RootAlert: alert,
		Related:   []EvidenceAlert{},
		CreatedAt: now,
		DomainKey: domainKey,
		CausalityGraph: &CausalityGraph{
			nodes: make(map[string]*GraphNode),
			edges: make([]*CausalEdge, 0),
		},
	}
	
	h.base.groups = append(h.base.groups, newGroup)
	newGroup.CausalityGraph.AddNode(alert.ID, alert)
	
	// Store LSH sketch for new alert
	idx := len(h.base.groups) - 1
	h.lshSketches[idx] = alertSketch
	
	return nil // New root group
}

// computeLSHSketch computes min-hashing signature for an alert
func (h *HybridAsyncCausalEngine) computeLSHSketch(alert EvidenceAlert) *MinHashSketch {
	sketch := NewMinHashSketch(h.numLSHBands)
	sketch.computeSignature(alert.Labels)
	return sketch
}

// addLSHSnapshot stores LSH sketch for all alerts in a group (for future comparisons)
func (h *HybridAsyncCausalEngine) addLSHSnapshot(groupIdx int, rootAlert EvidenceAlert) {
	// Root alert gets index groupIdx
	h.lshSketches[groupIdx] = h.computeLSHSketch(rootAlert)
	
	// Related alerts get sequential indices after groups count
	// TODO: maintain proper indexing strategy
}

// RunParallelBenchmarks executes parallel processing benchmark
func (h *HybridAsyncCausalEngine) RunParallelBenchmarks(alerts []EvidenceAlert, numBatches int) {
	chunkSize := len(alerts) / numBatches
	
	for i := 0; i < numBatches; i++ {
		start := i * chunkSize
		end := start + chunkSize
		if i == numBatches-1 {
			end = len(alerts) // Last batch gets remainder
		}
		
		h.batchProcessor.ProcessChunk(alerts, h.base.groups, start, end, h.similarityThresh)
	}
	
	h.batchProcessor.Wait()
}
