// Package performance provides stress tests, load tests, and performance
// budget tests for CloudAI Fusion. Validates system behavior under extreme
// load conditions: 10000+ concurrent requests, sustained throughput, and
// 72-hour stability simulation.
//
// Run: go test ./tests/performance/ -v -timeout=600s
// Run stress only: go test ./tests/performance/ -run TestStress_ -timeout=120s
// Run stability: go test ./tests/performance/ -run TestStability_ -timeout=600s
package performance

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/api"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/cache"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/resilience"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/testutil"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// Stress Test: 10,000 Concurrent API Requests
// ============================================================================

func TestStress_10000_ConcurrentRequests(t *testing.T) {
	auth, cluster, cloud, security, monitor, workload, mesh, wasmSvc, edgeSvc :=
		testutil.NewTestRouterConfig()
	router := api.NewRouter(api.RouterConfig{
		AuthService:     auth,
		CloudManager:    cloud,
		ClusterManager:  cluster,
		SecurityManager: security,
		MonitorService:  monitor,
		WorkloadManager: workload,
		MeshManager:     mesh,
		WasmManager:     wasmSvc,
		EdgeManager:     edgeSvc,
		Logger:          logrus.New(),
	})

	const totalRequests = 10000
	const concurrency = 500

	endpoints := []struct {
		method string
		path   string
		body   string
	}{
		{"GET", "/healthz", ""},
		{"GET", "/readyz", ""},
		{"GET", "/version", ""},
		{"GET", "/api/v1/clusters", ""},
		{"GET", "/api/v1/security/policies", ""},
		{"GET", "/api/v1/monitoring/alerts/rules", ""},
		{"GET", "/api/v1/wasm/modules", ""},
		{"GET", "/api/v1/edge/topology", ""},
		{"POST", "/api/v1/auth/login", `{"username":"admin","password":"Admin123!"}`},
	}

	var (
		totalOK      int64
		totalErr     int64
		totalLatency int64
		maxLatency   int64
	)

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	start := time.Now()

	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int) {
			defer wg.Done()
			defer func() { <-sem }()

			ep := endpoints[idx%len(endpoints)]
			reqStart := time.Now()

			var r *http.Request
			if ep.body != "" {
				r = httptest.NewRequest(ep.method, ep.path, strings.NewReader(ep.body))
				r.Header.Set("Content-Type", "application/json")
			} else {
				r = httptest.NewRequest(ep.method, ep.path, nil)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)

			latency := time.Since(reqStart).Microseconds()
			atomic.AddInt64(&totalLatency, latency)

			// Track max latency
			for {
				old := atomic.LoadInt64(&maxLatency)
				if latency <= old || atomic.CompareAndSwapInt64(&maxLatency, old, latency) {
					break
				}
			}

			if w.Code >= 200 && w.Code < 500 {
				atomic.AddInt64(&totalOK, 1)
			} else {
				atomic.AddInt64(&totalErr, 1)
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	ok := atomic.LoadInt64(&totalOK)
	errs := atomic.LoadInt64(&totalErr)
	total := ok + errs
	avgLatencyUs := float64(atomic.LoadInt64(&totalLatency)) / float64(total)
	maxLatencyMs := float64(atomic.LoadInt64(&maxLatency)) / 1000.0
	rps := float64(total) / elapsed.Seconds()

	t.Logf("=== Stress Test: 10,000 Concurrent Requests ===")
	t.Logf("  Total requests:    %d", total)
	t.Logf("  Successful:        %d (%.1f%%)", ok, float64(ok)/float64(total)*100)
	t.Logf("  Server errors:     %d", errs)
	t.Logf("  Elapsed:           %v", elapsed)
	t.Logf("  Throughput:        %.0f req/s", rps)
	t.Logf("  Avg latency:       %.2f ms", avgLatencyUs/1000)
	t.Logf("  Max latency:       %.2f ms", maxLatencyMs)
	t.Logf("  Concurrency:       %d", concurrency)

	// Assertions
	if errs > 0 {
		t.Errorf("expected 0 server errors, got %d", errs)
	}
	if total < totalRequests*90/100 {
		t.Errorf("expected at least %d completed requests, got %d", totalRequests*90/100, total)
	}
}

// ============================================================================
// Stress Test: 20,000 Concurrent Requests (Higher Scale)
// ============================================================================

func TestStress_20000_BurstRequests(t *testing.T) {
	auth, cluster, cloud, security, monitor, workload, mesh, wasmSvc, edgeSvc :=
		testutil.NewTestRouterConfig()
	router := api.NewRouter(api.RouterConfig{
		AuthService:     auth,
		CloudManager:    cloud,
		ClusterManager:  cluster,
		SecurityManager: security,
		MonitorService:  monitor,
		WorkloadManager: workload,
		MeshManager:     mesh,
		WasmManager:     wasmSvc,
		EdgeManager:     edgeSvc,
		Logger:          logrus.New(),
	})

	const totalRequests = 20000
	const concurrency = 1000

	var totalOK, totalErr int64
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			r := httptest.NewRequest("GET", "/healthz", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)

			if w.Code == http.StatusOK {
				atomic.AddInt64(&totalOK, 1)
			} else {
				atomic.AddInt64(&totalErr, 1)
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	ok := atomic.LoadInt64(&totalOK)
	rps := float64(ok+atomic.LoadInt64(&totalErr)) / elapsed.Seconds()

	t.Logf("20K burst: ok=%d, err=%d, elapsed=%v, rps=%.0f", ok, atomic.LoadInt64(&totalErr), elapsed, rps)

	if atomic.LoadInt64(&totalErr) > 0 {
		t.Errorf("expected 0 server errors under burst load")
	}
}

// ============================================================================
// Stress Test: Cache Under 10,000 Concurrent Operations
// ============================================================================

func TestStress_Cache_10000_ConcurrentOps(t *testing.T) {
	c := cache.NewMemoryCache(cache.Config{L1MaxSize: 50000, KeyPrefix: "stress:"}, logrus.New())
	defer c.Close()
	ctx := context.Background()

	const goroutines = 500
	const opsPerGoroutine = 200

	var totalOps int64
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				val := []byte(fmt.Sprintf("value-%d", j))

				// Mixed operations: 60% read, 30% write, 10% delete
				op := rand.Intn(10)
				switch {
				case op < 6:
					c.Get(ctx, key)
				case op < 9:
					c.Set(ctx, key, val, 5*time.Minute)
				default:
					c.Delete(ctx, key)
				}
				atomic.AddInt64(&totalOps, 1)
			}
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	ops := atomic.LoadInt64(&totalOps)
	opsPerSec := float64(ops) / elapsed.Seconds()

	stats := c.Stats()
	t.Logf("Cache stress: ops=%d, elapsed=%v, ops/s=%.0f, hit_rate=%.2f",
		ops, elapsed, opsPerSec, stats.HitRate)

	// Cache should remain functional
	c.Set(ctx, "post-stress", []byte("alive"), time.Minute)
	val, _ := c.Get(ctx, "post-stress")
	if val == nil {
		t.Error("cache should be functional after stress test")
	}
}

// ============================================================================
// Stress Test: Sharded Cache Under Load
// ============================================================================

func TestStress_ShardedCache_HighConcurrency(t *testing.T) {
	sc := cache.NewShardedCache(cache.DefaultShardedCacheConfig(), logrus.New())
	defer sc.Close()
	ctx := context.Background()

	const goroutines = 200
	const opsPerGoroutine = 500

	var totalOps int64
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("shard-key-%d-%d", id, j%100)
				val := []byte(fmt.Sprintf("shard-value-%d", j))
				sc.Set(ctx, key, val, 5*time.Minute)
				sc.Get(ctx, key)
				atomic.AddInt64(&totalOps, 2)
			}
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	ops := atomic.LoadInt64(&totalOps)
	stats := sc.Stats()

	t.Logf("Sharded cache stress: ops=%d, elapsed=%v, ops/s=%.0f, hit_rate=%.2f, keys=%d",
		ops, elapsed, float64(ops)/elapsed.Seconds(), stats.HitRate, stats.KeyCount)

	if stats.HitRate < 0.3 {
		t.Errorf("sharded cache hit rate %.2f is too low under stress", stats.HitRate)
	}
}

// ============================================================================
// Stress Test: Scheduler Under Sustained Load
// ============================================================================

func TestStress_Scheduler_SustainedLoad(t *testing.T) {
	engine, err := scheduler.NewEngine(scheduler.EngineConfig{
		SchedulingInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go engine.Run(ctx)
	time.Sleep(30 * time.Millisecond)

	// Submit 500 workloads in bursts of 50
	var submitted int64
	for batch := 0; batch < 10; batch++ {
		for i := 0; i < 50; i++ {
			wl := &scheduler.Workload{
				ID:       common.NewUUID(),
				Name:     fmt.Sprintf("stress-wl-%d-%d", batch, i),
				Type:     common.WorkloadTypeInference,
				Status:   common.WorkloadStatusQueued,
				Priority: rand.Intn(10) + 1,
				ResourceRequest: common.ResourceRequest{
					GPUCount: rand.Intn(4) + 1,
				},
				QueuedAt: common.NowUTC(),
			}
			engine.SubmitWorkload(wl)
			atomic.AddInt64(&submitted, 1)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Wait for processing
	time.Sleep(1 * time.Second)
	engine.Stop()

	stats := engine.GetStats()
	t.Logf("Scheduler stress: submitted=%d, stats=%+v", atomic.LoadInt64(&submitted), stats)
}

// ============================================================================
// Stress Test: Circuit Breaker Under Concurrent Load
// ============================================================================

func TestStress_CircuitBreaker_ConcurrentExecution(t *testing.T) {
	cb := resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
		FailureThreshold:    10,
		SuccessThreshold:    3,
		Timeout:             100 * time.Millisecond,
		MaxHalfOpenRequests: 3,
	})

	const goroutines = 100
	const callsPerGoroutine = 100

	var totalSuccess, totalRejected, totalFailed int64
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				err := cb.Execute(func() error {
					if rand.Float64() < 0.1 { // 10% failure
						return fmt.Errorf("simulated failure")
					}
					return nil
				})
				if err == nil {
					atomic.AddInt64(&totalSuccess, 1)
				} else if err.Error() == "circuit breaker is open" {
					atomic.AddInt64(&totalRejected, 1)
				} else {
					atomic.AddInt64(&totalFailed, 1)
				}
			}
		}(i)
	}
	wg.Wait()

	t.Logf("Circuit breaker stress: success=%d, rejected=%d, failed=%d, state=%s",
		atomic.LoadInt64(&totalSuccess),
		atomic.LoadInt64(&totalRejected),
		atomic.LoadInt64(&totalFailed),
		cb.State().String(),
	)
}

// ============================================================================
// Stress Test: Bloom Filter Under Concurrent Access
// ============================================================================

func TestStress_BloomFilter_ConcurrentOps(t *testing.T) {
	bf := cache.NewBloomFilter(100000, 0.01)

	const goroutines = 100
	const opsPerGoroutine = 1000

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("bf-key-%d-%d", id, j)
				bf.Add(key)
				bf.MayContain(key)
			}
		}(i)
	}
	wg.Wait()

	t.Logf("Bloom filter: count=%d, estimated_fp_rate=%.6f", bf.Count(), bf.EstimatedFPRate())

	// False positive rate should be reasonable
	if bf.EstimatedFPRate() > 0.05 {
		t.Errorf("bloom filter FP rate %.4f exceeds 5%%", bf.EstimatedFPRate())
	}
}

// ============================================================================
// Stability Test: Simulated 72-Hour Soak Test (compressed to ~30s)
// ============================================================================

func TestStability_72h_CompressedSoak(t *testing.T) {
	auth, cluster, cloud, security, monitor, workload, mesh, wasmSvc, edgeSvc :=
		testutil.NewTestRouterConfig()
	router := api.NewRouter(api.RouterConfig{
		AuthService:     auth,
		CloudManager:    cloud,
		ClusterManager:  cluster,
		SecurityManager: security,
		MonitorService:  monitor,
		WorkloadManager: workload,
		MeshManager:     mesh,
		WasmManager:     wasmSvc,
		EdgeManager:     edgeSvc,
		Logger:          logrus.New(),
	})

	// Simulate 72 hours of varying load compressed into ~30 seconds.
	// Each "hour" is ~400ms with varying request intensity.
	const simulatedHours = 72
	const msPerHour = 400

	type hourlyStats struct {
		Hour       int
		Requests   int64
		Errors     int64
		AvgLatency float64
	}

	allStats := make([]hourlyStats, simulatedHours)

	for hour := 0; hour < simulatedHours; hour++ {
		// Simulate diurnal load pattern (peak at hour 12-18, trough at 0-6)
		hourOfDay := hour % 24
		var loadFactor float64
		switch {
		case hourOfDay >= 0 && hourOfDay < 6:
			loadFactor = 0.2 // night low
		case hourOfDay >= 6 && hourOfDay < 12:
			loadFactor = 0.6 // morning ramp
		case hourOfDay >= 12 && hourOfDay < 18:
			loadFactor = 1.0 // peak
		default:
			loadFactor = 0.4 // evening decline
		}

		requestsThisHour := int(50 * loadFactor)
		if requestsThisHour < 5 {
			requestsThisHour = 5
		}

		var hourOK, hourErr, hourLatency int64
		var wg sync.WaitGroup

		for i := 0; i < requestsThisHour; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				start := time.Now()
				paths := []string{"/healthz", "/api/v1/clusters", "/api/v1/security/policies"}
				r := httptest.NewRequest("GET", paths[rand.Intn(len(paths))], nil)
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				latency := time.Since(start).Microseconds()
				atomic.AddInt64(&hourLatency, latency)

				if w.Code >= 200 && w.Code < 500 {
					atomic.AddInt64(&hourOK, 1)
				} else {
					atomic.AddInt64(&hourErr, 1)
				}
			}()
		}
		wg.Wait()

		ok := atomic.LoadInt64(&hourOK)
		errs := atomic.LoadInt64(&hourErr)
		total := ok + errs
		avgLat := float64(0)
		if total > 0 {
			avgLat = float64(atomic.LoadInt64(&hourLatency)) / float64(total) / 1000
		}

		allStats[hour] = hourlyStats{
			Hour:       hour,
			Requests:   total,
			Errors:     errs,
			AvgLatency: avgLat,
		}

		// Brief pause between hours
		time.Sleep(time.Duration(msPerHour/10) * time.Millisecond)
	}

	// Analyze results
	var totalReqs, totalErrs int64
	var maxLatency float64
	var errorHours int

	for _, s := range allStats {
		totalReqs += s.Requests
		totalErrs += s.Errors
		if s.AvgLatency > maxLatency {
			maxLatency = s.AvgLatency
		}
		if s.Errors > 0 {
			errorHours++
		}
	}

	t.Logf("=== 72-Hour Stability Test (Compressed) ===")
	t.Logf("  Simulated hours:   %d", simulatedHours)
	t.Logf("  Total requests:    %d", totalReqs)
	t.Logf("  Total errors:      %d", totalErrs)
	t.Logf("  Error hours:       %d/%d", errorHours, simulatedHours)
	t.Logf("  Max avg latency:   %.2f ms", maxLatency)

	// Stability assertions
	if totalErrs > 0 {
		t.Errorf("expected 0 errors over 72h soak, got %d across %d hours", totalErrs, errorHours)
	}
}

// ============================================================================
// Stability Test: Memory Leak Detection (simulated)
// ============================================================================

func TestStability_MemoryLeak_Detection(t *testing.T) {
	c := cache.NewMemoryCache(cache.Config{L1MaxSize: 1000, KeyPrefix: "leak:"}, logrus.New())
	defer c.Close()
	ctx := context.Background()

	// Run multiple cycles of add/remove to detect if memory grows unboundedly
	for cycle := 0; cycle < 50; cycle++ {
		// Add 500 keys
		for i := 0; i < 500; i++ {
			key := fmt.Sprintf("cycle-%d-key-%d", cycle, i)
			c.Set(ctx, key, []byte(fmt.Sprintf("value-%d", i)), 100*time.Millisecond)
		}

		// Wait for expiration
		time.Sleep(150 * time.Millisecond)
	}

	// After all cycles, key count should be bounded by maxSize
	stats := c.Stats()
	t.Logf("Memory leak test: keys=%d (max=1000)", stats.KeyCount)

	if stats.KeyCount > 1000 {
		t.Errorf("cache has %d keys, exceeding max size 1000 — possible memory leak", stats.KeyCount)
	}
}

// ============================================================================
// Performance Budget: API Response Time
// ============================================================================

func TestPerformanceBudget_APILatency(t *testing.T) {
	auth, cluster, cloud, security, monitor, workload, mesh, wasmSvc, edgeSvc :=
		testutil.NewTestRouterConfig()
	router := api.NewRouter(api.RouterConfig{
		AuthService:     auth,
		CloudManager:    cloud,
		ClusterManager:  cluster,
		SecurityManager: security,
		MonitorService:  monitor,
		WorkloadManager: workload,
		MeshManager:     mesh,
		WasmManager:     wasmSvc,
		EdgeManager:     edgeSvc,
		Logger:          logrus.New(),
	})

	budgets := []struct {
		name      string
		method    string
		path      string
		body      string
		maxP99Ms  float64
	}{
		{"health_check", "GET", "/healthz", "", 5},
		{"version", "GET", "/version", "", 5},
		{"list_clusters", "GET", "/api/v1/clusters", "", 20},
		{"login", "POST", "/api/v1/auth/login", `{"username":"admin","password":"Admin123!"}`, 50},
		{"security_policies", "GET", "/api/v1/security/policies", "", 20},
		{"wasm_modules", "GET", "/api/v1/wasm/modules", "", 20},
		{"edge_topology", "GET", "/api/v1/edge/topology", "", 20},
	}

	for _, budget := range budgets {
		t.Run(budget.name, func(t *testing.T) {
			const iterations = 200
			latencies := make([]time.Duration, iterations)

			for i := 0; i < iterations; i++ {
				var r *http.Request
				if budget.body != "" {
					r = httptest.NewRequest(budget.method, budget.path, strings.NewReader(budget.body))
					r.Header.Set("Content-Type", "application/json")
				} else {
					r = httptest.NewRequest(budget.method, budget.path, nil)
				}
				w := httptest.NewRecorder()

				start := time.Now()
				router.ServeHTTP(w, r)
				latencies[i] = time.Since(start)
			}

			// Sort for percentiles
			sortDurations(latencies)
			p50 := latencies[iterations*50/100]
			p95 := latencies[iterations*95/100]
			p99 := latencies[iterations*99/100]

			t.Logf("%s: P50=%v, P95=%v, P99=%v (budget: P99<%vms)",
				budget.name, p50, p95, p99, budget.maxP99Ms)

			p99Ms := float64(p99.Microseconds()) / 1000
			if p99Ms > budget.maxP99Ms {
				t.Errorf("P99 latency %.2fms exceeds budget of %.0fms", p99Ms, budget.maxP99Ms)
			}
		})
	}
}

// ============================================================================
// Performance Budget: Cache Operations Throughput
// ============================================================================

func TestPerformanceBudget_CacheThroughput(t *testing.T) {
	c := cache.NewMemoryCache(cache.Config{L1MaxSize: 100000, KeyPrefix: "perf:"}, logrus.New())
	defer c.Close()
	ctx := context.Background()

	// Measure write throughput
	const ops = 100000
	start := time.Now()
	for i := 0; i < ops; i++ {
		c.Set(ctx, fmt.Sprintf("key-%d", i), []byte(fmt.Sprintf("value-%d", i)), 5*time.Minute)
	}
	writeElapsed := time.Since(start)
	writeOpsPerSec := float64(ops) / writeElapsed.Seconds()

	// Measure read throughput
	start = time.Now()
	for i := 0; i < ops; i++ {
		c.Get(ctx, fmt.Sprintf("key-%d", i))
	}
	readElapsed := time.Since(start)
	readOpsPerSec := float64(ops) / readElapsed.Seconds()

	t.Logf("Cache throughput: write=%.0f ops/s, read=%.0f ops/s", writeOpsPerSec, readOpsPerSec)

	// Minimum throughput requirements
	if writeOpsPerSec < 100000 {
		t.Errorf("cache write throughput %.0f ops/s below 100K minimum", writeOpsPerSec)
	}
	if readOpsPerSec < 200000 {
		t.Errorf("cache read throughput %.0f ops/s below 200K minimum", readOpsPerSec)
	}
}

// ============================================================================
// JSON Serialization Performance
// ============================================================================

func TestPerformanceBudget_JSONSerialization(t *testing.T) {
	payload := map[string]interface{}{
		"id":        "uuid-12345",
		"name":      "test-workload",
		"namespace": "production",
		"type":      "training",
		"status":    "running",
		"priority":  7,
		"gpu_count": 4,
		"metrics": map[string]interface{}{
			"gpu_utilization": 85.5,
			"memory_usage":    72.3,
			"throughput":      1234.56,
		},
	}

	const iterations = 10000
	start := time.Now()
	for i := 0; i < iterations; i++ {
		data, _ := json.Marshal(payload)
		var result map[string]interface{}
		json.Unmarshal(data, &result)
	}
	elapsed := time.Since(start)
	opsPerSec := float64(iterations) / elapsed.Seconds()

	t.Logf("JSON round-trip: %.0f ops/s (elapsed=%v)", opsPerSec, elapsed)
}

// ============================================================================
// Utility
// ============================================================================

func sortDurations(d []time.Duration) {
	for i := 1; i < len(d); i++ {
		for j := i; j > 0 && d[j] < d[j-1]; j-- {
			d[j], d[j-1] = d[j-1], d[j]
		}
	}
}
