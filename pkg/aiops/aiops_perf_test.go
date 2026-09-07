package aiops

import (
	"math"
	"math/rand"
	"sync"
	"testing"
)

// 2026 Competitive Baseline: Datadog Watchdog
//   Watchdog detects anomalies via batch statistical analysis on stored metrics.
//   Each analysis cycle processes full history window from TSDB. Latency: 30-60s cycle.
//
// Our Innovation: Sliding window online detection + root cause pattern cache.
//   - Online: each new data point updates running stats in O(1) (no TSDB query)
//   - Root cause cache: identical failure signatures return cached diagnosis instantly

type SlidingWindowDetector struct {
	mu      sync.Mutex
	window  []float64
	maxSize int
	sum     float64
	sumSq   float64
	count   int
}

func NewSlidingWindowDetector(windowSize int) *SlidingWindowDetector {
	return &SlidingWindowDetector{window: make([]float64, 0, windowSize), maxSize: windowSize}
}

// Ingest adds a data point and returns true if anomalous (>3 sigma).
// Complexity: O(1) amortized (ring buffer update + running stats).
func (d *SlidingWindowDetector) Ingest(value float64) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.window) >= d.maxSize {
		old := d.window[0]
		d.window = d.window[1:]
		d.sum -= old
		d.sumSq -= old * old
		d.count--
	}
	d.window = append(d.window, value)
	d.sum += value
	d.sumSq += value * value
	d.count++
	if d.count < 10 {
		return false
	}
	mean := d.sum / float64(d.count)
	variance := d.sumSq/float64(d.count) - mean*mean
	stddev := math.Sqrt(math.Abs(variance))
	return math.Abs(value-mean) > 3*stddev
}

type RootCauseCache struct {
	mu    sync.RWMutex
	cache map[uint64]string
}

func NewRootCauseCache() *RootCauseCache {
	return &RootCauseCache{cache: make(map[uint64]string, 256)}
}

func (rc *RootCauseCache) Lookup(signatureHash uint64) (string, bool) {
	rc.mu.RLock()
	v, ok := rc.cache[signatureHash]
	rc.mu.RUnlock()
	return v, ok
}

func (rc *RootCauseCache) Store(signatureHash uint64, diagnosis string) {
	rc.mu.Lock()
	rc.cache[signatureHash] = diagnosis
	rc.mu.Unlock()
}

func BenchmarkAIOps_OnlineDetect(b *testing.B) {
	det := NewSlidingWindowDetector(1000)
	for i := 0; i < 500; i++ {
		det.Ingest(rand.Float64() * 100)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		det.Ingest(rand.Float64() * 100)
	}
}

func BenchmarkAIOps_BatchDetect_Simulated(b *testing.B) {
	// Baseline: scan full 1000-point window each cycle (Datadog Watchdog style)
	window := make([]float64, 1000)
	for i := range window {
		window[i] = rand.Float64() * 100
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var sum, sumSq float64
		for _, v := range window {
			sum += v
			sumSq += v * v
		}
		mean := sum / 1000
		std := math.Sqrt(sumSq/1000 - mean*mean)
		_ = std
	}
}

func BenchmarkAIOps_RootCauseCache_Hit(b *testing.B) {
	cache := NewRootCauseCache()
	cache.Store(12345, "CPU throttling due to cgroup limit")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Lookup(12345)
	}
}

func TestAIOps_OnlineVsBatch(t *testing.T) {
	det := NewSlidingWindowDetector(1000)
	for i := 0; i < 500; i++ {
		det.Ingest(rand.Float64() * 100)
	}
	onlineResult := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			det.Ingest(rand.Float64() * 100)
		}
	})
	window := make([]float64, 1000)
	for i := range window { window[i] = rand.Float64() * 100 }
	batchResult := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			var sum, sumSq float64
			for _, v := range window { sum += v; sumSq += v * v }
			_ = math.Sqrt(sumSq/1000 - (sum/1000)*(sum/1000))
		}
	})
	t.Logf("Online O(1): %d ns/op", onlineResult.NsPerOp())
	t.Logf("Batch O(N):  %d ns/op", batchResult.NsPerOp())
	t.Logf("Speedup: %.1fx", float64(batchResult.NsPerOp())/float64(onlineResult.NsPerOp()))
}
