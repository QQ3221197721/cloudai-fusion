package quantile

import (
	"fmt"
	"math"
	"strconv"
)

// GKWrapper provides a GK (Greedy-KSketch-style) interface using Greenwald-Khanna algorithm.
// This is the standard bounded-memory sketch with epsilon rank-error guarantees.
// Theory: Greenwald & Khannna 2001 - O((1/eps) log(eps*n)) memory with eps*error guarantee.
// Unlike P² which has NO theoretical bounds, GK provides formal accuracy guarantees.
// Memory is higher (~24 bytes per tuple + compression overhead) but error is provably bounded.
type GKWrapper struct {
	inner   *GKSummary
	epsilon float64 // accuracy parameter (same as epsilon in GK)
}

// NewGKWrapper creates a new Greedy-KSketch wrapper using GK algorithm.
// Epsilon controls the trade-off: smaller = more accurate but more memory.
// Typical values: 0.01 (1% rank error), 0.005 (0.5% error).
// Comparison to P²: GK guarantees |estimated_rank - true_rank| <= epsilon*n
// P² has no such bound, making GK safer for regulated environments requiring formal proofs.
func NewGKWrapper(epsilon float64) *GKWrapper {
	if epsilon <= 0 {
		return &GKWrapper{
			inner:   NewGKExact(),
			epsilon: 0,
		}
	}
	return &GKWrapper{
		inner:   NewGKSummary(epsilon),
		epsilon: epsilon,
	}
}

// Insert adds a single sample to the GK sketch (P² uses Add, we maintain interface consistency).
func (g *GKWrapper) Insert(value float64) error {
	if math.IsNaN(value) {
		return nil // Skip NaN, consistent with P² behavior
	}
	g.inner.Add(value)
	return nil
}

// Quantile returns estimated q-quantile value (q in [0,1]).
// Returns NaN if sketch is empty.
// Guaranteed: true_rank(Quantile(q)) ∈ [ceil(q*n) - eps*n, ceil(q*n) + eps*n]
func (g *GKWrapper) Quantile(q float64) float64 {
	return g.inner.Quantile(q)
}

// SizeBytes reports current memory usage in bytes.
// Each summary tuple is ~24 bytes (float64 + 2 int32s).
// In bounded mode: ~2/epsilon tuples max, so memory caps at O((1/epsilon) * 24).
func (g *GKWrapper) SizeBytes() int {
	return g.inner.SizeBytes()
}

// Count returns total samples ingested.
func (g *GKWrapper) Count() int {
	return g.inner.Count()
}

// Type returns algorithm identifier.
func (g *GKWrapper) Type() string {
	if g.epsilon == 0 {
		return "GK-Exact"
	}
	return fmt.Sprintf("GK(ε=%.4f)", g.epsilon)
}

// Name implements Sketch interface for GKWrapper.
func (g *GKWrapper) Name() string {
	return g.Type()
}

// Add implements Sketch interface by calling Insert.
func (g *GKWrapper) Add(x float64) {
	g.Insert(x)
}

// FlushBuffer is a no-op for GK since it doesn't buffer.
func (g *GKWrapper) FlushBuffer() {}

// CompressionRatio estimates how much the summary has compressed relative to raw data.
// Returns ratio of original size (8 bytes per sample) to current sketch size.
// Higher = better compression. Exact mode returns 1.0 (no compression).
func (g *GKWrapper) CompressionRatio() float64 {
	if g.inner.Count() == 0 {
		return 1.0
	}
	rawSize := float64(g.inner.Count()) * 8
	actualSize := float64(g.SizeBytes())
	if actualSize == 0 {
		return 1.0
	}
	return rawSize / actualSize
}

// TDigestWrapper wraps t-digest centroid-based clustering approach (Dunning & Ertl 2019).
// Uses arcsin scale function k(q) ∝ arcsin(2q−1) to concentrate resolution in tails.
// Strengths: excellent tail accuracy (p99/p999), supports merging sketches.
// Weaknesses: slower than P²/GK due to centroid management, not adversarial-robust.
// Memory: ~delta centroids × 16 bytes (mean + weight per centroid).
type TDigestWrapper struct {
	inner      *TDigest
	compression int // delta parameter: higher = more centroids = better accuracy
}

// NewTDigestWrapper creates a new t-digest wrapper.
// Compression (δ) typically ranges from 100-1000.
// - δ=100: ~160KB for 1M samples (low accuracy, fast)
// - δ=1000: ~1.6MB for 1M samples (high accuracy, slower)
// Comparison to P²: t-digest has O(delta) memory vs P²'s fixed ~100 bytes,
// but provides better tail estimates due to arcsin concentration.
func NewTDigestWrapper(compression int) *TDigestWrapper {
	if compression < 20 {
		compression = 20
	}
	return &TDigestWrapper{
		inner:      NewTDigest(float64(compression)),
		compression: compression,
	}
}

// Insert adds a single sample to t-digest.
// Internal buffering merges points into centroids periodically.
func (t *TDigestWrapper) Insert(value float64) error {
	if math.IsNaN(value) {
		return nil
	}
	t.inner.Add(value)
	return nil
}

// Quantile returns estimated q-quantile by walking centroid CDF.
// Best accuracy: extreme quantiles (p99, p999) due to arcsin scale.
// Weaker accuracy: median/body percentiles where centroids merge aggressively.
func (t *TDigestWrapper) Quantile(q float64) float64 {
	return t.inner.Quantile(q)
}

// SizeBytes reports memory footprint of centroids + buffer.
// Approximate: delta × 16 bytes (centroid mean+weight) + buffer_size × 8 bytes.
func (t *TDigestWrapper) SizeBytes() int {
	return t.inner.SizeBytes()
}

// Count returns total weighted count of samples.
// Note: t-digest weights centroids, so this may differ slightly from actual inserted count.
func (t *TDigestWrapper) Count() int {
	return t.inner.Count()
}

// Type returns algorithm identifier for benchmarks.
func (t *TDigestWrapper) Type() string {
	return fmt.Sprintf("TDigest(δ=%d)", t.compression)
}

// Name implements Sketch interface for TDigestWrapper.
func (t *TDigestWrapper) Name() string {
	return t.Type()
}

// Add implements Sketch interface by calling Insert.
func (t *TDigestWrapper) Add(x float64) {
	t.Insert(x)
}

// FlushBuffer flushes any internal buffers for P² wrappers.
func (t *TDigestWrapper) FlushBuffer() {}

// Count returns total weighted count of samples.

// CentroidCount returns number of active centroids currently stored.
// This grows until saturation at approximately delta centroids.
func (t *TDigestWrapper) CentroidCount() int {
	t.inner.flush()
	return len(t.inner.centroids)
}

// P2Wrapper provides a standardized interface for P² algorithm using NewP2 constructor.
// Allows P² to be treated identically to GK/t-digest in benchmarks.
type P2Wrapper struct {
	inner    *P2Sketch
	targetQs []float64
}

// NewP2Wrapper creates P² wrapper tracking specific quantiles (e.g., 0.5, 0.9, 0.99).
// Memory: ~100 bytes per quantile tracked + 8KB default buffer.
// Speed: fastest of all sketches - O(1) per insert with zero allocations after init.
// Accuracy: excellent on typical streams (lognormal, gamma), no formal error bounds.
// Use case: AIOps latency monitoring where speed > theoretical guarantees.
func NewP2Wrapper(targetQs ...float64) *P2Wrapper {
	if len(targetQs) == 0 {
		targetQs = []float64{0.5, 0.9, 0.99}
	}
	return &P2Wrapper{
		inner:    NewP2(targetQs...),
		targetQs: targetQs,
	}
}

// Insert aliases P² Add method for interface consistency.
func (p *P2Wrapper) Insert(value float64) error {
	p.inner.Add(value)
	return nil
}

// Quantile returns closest tracked quantile or interpolated estimate.
// Matches nearest configured target if requested q not exactly tracked.
func (p *P2Wrapper) Quantile(q float64) float64 {
	return p.inner.Quantile(q)
}

// SizeBytes reports P² memory usage.
// Fixed cost: 100 bytes per tracked quantile + 8KB buffer.
// Does NOT grow with stream length (true O(1) memory).
func (p *P2Wrapper) SizeBytes() int {
	return p.inner.SizeBytes()
}

// Count returns total samples ingested.
func (p *P2Wrapper) Count() int {
	return p.inner.Count()
}

// Name implements Sketch interface for P2Wrapper.
func (p *P2Wrapper) Name() string {
	return p.Type()
}

// Add implements Sketch interface by calling Insert.
func (p *P2Wrapper) Add(x float64) {
	p.Insert(x)
}

// FlushBuffer flushes any internal buffers for P² wrappers.
func (p *P2Wrapper) FlushBuffer() {
	p.inner.FlushBuffer()
}
// Type returns algorithm identifier.
func (p *P2Wrapper) Type() string {
	qStr := ""
	for i, q := range p.targetQs {
		if i > 0 {
			qStr += ","
		}
		qStr += strconv.FormatFloat(q, 'f', 2, 64)
	}
	return fmt.Sprintf("P²([%s])", qStr)
}

// BenchmarkResult captures performance metrics from FLIP benchmark runs.
// Used to compare accuracy, memory, and speed across algorithms.
type BenchmarkResult struct {
	Algorithm     string        // e.g., "P²", "GK(ε=0.01)", "TDigest(δ=1000)"
	Accuracy      float64       // Relative error at p50/p90/p99 (avg)
	MemoryMB      float64       // Memory usage in MB
	Allocations   int           // Heap allocations per operation (from -benchmem)
	NsPerOp       uint64        // Nanoseconds per Insert operation
	Distribution  string        // Dataset type (lognormal, uniform, heavy_tail, adversarial)
	Samples       int           // Number of samples processed
	P50Error      float64       // Absolute error at p50
	P90Error      float64       // Absolute error at p90
	P99Error      float64       // Absolute error at p99
	MedianValue   float64       // True median of dataset (ground truth)
	P99Value      float64       // True p99 of dataset (ground truth)
}

// ResultSummary converts benchmark results to human-readable format.
// Designed for printing in test output or generating reports.
func (r *BenchmarkResult) ResultSummary() string {
	return fmt.Sprintf(`Benchmark Result: %s (%s)
  Accuracy: %.4f%% avg relative error
    ├─ p50 error: %.4f%%
    ├─ p90 error: %.4f%%
    └─ p99 error: %.4f%%
  Memory: %.2f MB (%.0f KB)
  Performance: %d ns/op (%.2f M ops/sec)
  Allocations: %d allocs/op
  Ground Truth: median=%.2f, p99=%.2f`,
		r.Algorithm, r.Distribution,
		r.Accuracy*100, r.P50Error*100, r.P90Error*100, r.P99Error*100,
		r.MemoryMB, r.MemoryMB*1024,
		r.NsPerOp, float64(1e9)/float64(r.NsPerOp)/1e6,
		r.Allocations,
		r.MedianValue, r.P99Value)
}

// CompareAgainst computes comparative metrics against a reference result.
// Returns speedup factor, memory ratio, and accuracy improvement.
// Positive speedup = faster than reference, negative = slower.
func (r *BenchmarkResult) CompareAgainst(reference BenchmarkResult) map[string]float64 {
	speedup := float64(reference.NsPerOp) / float64(r.NsPerOp)
	memoryRatio := r.MemoryMB / reference.MemoryMB
	accuracyImprovement := reference.Accuracy - r.Accuracy // positive = better
	
	return map[string]float64{
		"speedup":         speedup,
		"memory_ratio":    memoryRatio,
		"accuracy_diff":   accuracyImprovement,
		"percent_faster":  (speedup - 1) * 100,
		"percent_memory":  (memoryRatio - 1) * 100,
	}
}
