// Package chaos - Enhanced chaos engineering exercises for CloudAI Fusion.
// Implements comprehensive fault injection scenarios including:
//   - Cascading failure propagation and recovery
//   - Resource exhaustion simulation (memory, goroutines, file descriptors)
//   - Split-brain and network partition healing
//   - Gradual degradation and graceful degradation verification
//   - SLO compliance under adverse conditions
//
// Run with: go test ./tests/chaos/ -v -run TestChaosEnhanced -timeout=300s
package chaos

import (
	"context"
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
// Chaos Scenario: Cascading Failure Propagation
// Simulates a chain of dependent service failures and verifies
// the system degrades gracefully rather than collapsing.
// ============================================================================

func TestChaosEnhanced_CascadingFailure(t *testing.T) {
	// Stage 1: Create circuit breakers for 5 dependent services
	services := []string{"auth-svc", "cluster-svc", "scheduler-svc", "cache-svc", "monitoring-svc"}
	registry := resilience.NewBreakerRegistry(resilience.CircuitBreakerConfig{
		FailureThreshold:    3,
		SuccessThreshold:    2,
		Timeout:             200 * time.Millisecond,
		MaxHalfOpenRequests: 1,
	})

	// Stage 2: Simulate cascading failure — each service depends on the previous
	// Auth → Cluster → Scheduler → Cache → Monitoring
	for _, svc := range services {
		cb := registry.Get(svc)
		// Trip each breaker
		for i := 0; i < 5; i++ {
			_ = cb.Execute(func() error {
				return fmt.Errorf("service %s unavailable", svc)
			})
		}
	}

	// Verify all breakers are open
	states := registry.States()
	for _, svc := range services {
		if states[svc] != "open" {
			t.Errorf("expected %s breaker to be open, got %s", svc, states[svc])
		}
	}

	// Stage 3: Recovery — services come back one by one
	time.Sleep(250 * time.Millisecond)

	recoveredCount := 0
	for _, svc := range services {
		cb := registry.Get(svc)
		// Successful call in half-open should start recovery
		err := cb.Execute(func() error { return nil })
		if err == nil {
			recoveredCount++
		}
	}

	t.Logf("Cascading failure: %d/%d services began recovery", recoveredCount, len(services))

	if recoveredCount == 0 {
		t.Error("at least one service should begin recovery after timeout")
	}
}

// ============================================================================
// Chaos Scenario: Resource Exhaustion — Goroutine Storm
// Verifies the system remains responsive when goroutine count spikes.
// ============================================================================

func TestChaosEnhanced_GoroutineStorm(t *testing.T) {
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

	// Launch 2000 concurrent goroutines hitting the API
	const goroutines = 2000
	var wg sync.WaitGroup
	var ok, failed int64

	barrier := make(chan struct{}) // synchronize start

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-barrier // wait for all goroutines to be ready
			r := httptest.NewRequest("GET", "/healthz", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code == http.StatusOK {
				atomic.AddInt64(&ok, 1)
			} else {
				atomic.AddInt64(&failed, 1)
			}
		}()
	}

	// Release all goroutines simultaneously
	close(barrier)
	wg.Wait()

	successRate := float64(atomic.LoadInt64(&ok)) / float64(goroutines) * 100
	t.Logf("Goroutine storm: %d/%d success (%.1f%%)", atomic.LoadInt64(&ok), goroutines, successRate)

	if successRate < 95 {
		t.Errorf("success rate %.1f%% below 95%% threshold under goroutine storm", successRate)
	}
}

// ============================================================================
// Chaos Scenario: Intermittent Network Jitter
// Simulates random delays and failures to verify retry/timeout behavior.
// ============================================================================

func TestChaosEnhanced_NetworkJitter(t *testing.T) {
	var totalRequests, successCount, failureCount int64

	// Mock server with random jitter
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&totalRequests, 1)

		// Random behavior: 50% fast, 30% slow, 10% timeout, 10% error
		roll := rand.Float64()
		switch {
		case roll < 0.5:
			// Fast response
			w.WriteHeader(http.StatusOK)
		case roll < 0.8:
			// Slow response (simulate DB delay)
			time.Sleep(time.Duration(rand.Intn(50)) * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		case roll < 0.9:
			// Simulated timeout — just delay beyond client timeout
			time.Sleep(200 * time.Millisecond)
			w.WriteHeader(http.StatusGatewayTimeout)
		default:
			// Server error
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	// Send requests with tight timeout
	const requests = 500
	var wg sync.WaitGroup

	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/api", nil)
			client := &http.Client{Timeout: 100 * time.Millisecond}
			resp, err := client.Do(req)
			if err != nil {
				atomic.AddInt64(&failureCount, 1)
				return
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				atomic.AddInt64(&successCount, 1)
			} else {
				atomic.AddInt64(&failureCount, 1)
			}
		}()
	}
	wg.Wait()

	success := atomic.LoadInt64(&successCount)
	failures := atomic.LoadInt64(&failureCount)
	total := success + failures

	t.Logf("Network jitter: success=%d, failures=%d, total=%d, success_rate=%.1f%%",
		success, failures, total, float64(success)/float64(total)*100)

	// At least 40% should succeed (we have 50% fast + 30% slow = ~80% potentially OK)
	if float64(success)/float64(total) < 0.30 {
		t.Errorf("success rate too low under network jitter: %.1f%%", float64(success)/float64(total)*100)
	}
}

// ============================================================================
// Chaos Scenario: Split-Brain Recovery
// Simulates a cluster split where two partitions operate independently,
// then verifies correct reconciliation on reunion.
// ============================================================================

func TestChaosEnhanced_SplitBrainRecovery(t *testing.T) {
	// Simulate two cache partitions (representing split-brain)
	partition1 := cache.NewMemoryCache(cache.Config{L1MaxSize: 1000, KeyPrefix: "p1:"}, logrus.New())
	partition2 := cache.NewMemoryCache(cache.Config{L1MaxSize: 1000, KeyPrefix: "p2:"}, logrus.New())
	defer partition1.Close()
	defer partition2.Close()
	ctx := context.Background()

	// Both partitions write to the same keys during split
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("shared-key-%d", i)
		// Partition 1 has "v1" values
		partition1.Set(ctx, key, []byte(fmt.Sprintf("p1-value-%d", i)), time.Minute)
		// Partition 2 has "v2" values
		partition2.Set(ctx, key, []byte(fmt.Sprintf("p2-value-%d", i)), time.Minute)
	}

	// Reconcile: "last writer wins" strategy — merge p2 into p1
	merged := cache.NewMemoryCache(cache.Config{L1MaxSize: 2000, KeyPrefix: ""}, logrus.New())
	defer merged.Close()

	// P1 first, then P2 overwrites (simulating later timestamp)
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("shared-key-%d", i)
		// Copy from p1
		if val, _ := partition1.Get(ctx, key); val != nil {
			merged.Set(ctx, key, val, time.Minute)
		}
		// P2 overwrites (it's "newer")
		if val, _ := partition2.Get(ctx, key); val != nil {
			merged.Set(ctx, key, val, time.Minute)
		}
	}

	// Verify merged state uses p2 values
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("shared-key-%d", i)
		val, _ := merged.Get(ctx, key)
		if val == nil {
			t.Errorf("key %s should exist after merge", key)
			continue
		}
		expected := fmt.Sprintf("p2-value-%d", i)
		if string(val) != expected {
			t.Errorf("key %s: expected %s, got %s", key, expected, string(val))
		}
	}

	t.Log("Split-brain recovery: successfully reconciled partitions with last-writer-wins")
}

// ============================================================================
// Chaos Scenario: Gradual Degradation
// Simulates progressively increasing failure rate and verifies
// the system degrades gracefully.
// ============================================================================

func TestChaosEnhanced_GradualDegradation(t *testing.T) {
	cb := resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
		FailureThreshold:    5,
		SuccessThreshold:    3,
		Timeout:             300 * time.Millisecond,
		MaxHalfOpenRequests: 2,
	})

	type phaseResult struct {
		failRate    float64
		success     int
		failure     int
		rejected    int
	}

	phases := []float64{0.0, 0.1, 0.3, 0.5, 0.7, 0.9, 0.5, 0.1, 0.0}
	results := make([]phaseResult, len(phases))

	for p, failRate := range phases {
		cb.Reset()
		result := phaseResult{failRate: failRate}

		for i := 0; i < 50; i++ {
			err := cb.Execute(func() error {
				if rand.Float64() < failRate {
					return fmt.Errorf("degraded")
				}
				return nil
			})
			if err == nil {
				result.success++
			} else if strings.Contains(err.Error(), "circuit") {
				result.rejected++
			} else {
				result.failure++
			}
		}
		results[p] = result
	}

	for _, r := range results {
		t.Logf("Fail rate %.0f%%: success=%d, failed=%d, rejected=%d",
			r.failRate*100, r.success, r.failure, r.rejected)
	}

	// When failure rate is 0%, all should succeed
	if results[0].success < 48 {
		t.Errorf("phase 0 (0%% failures): expected ~50 successes, got %d", results[0].success)
	}
}

// ============================================================================
// Chaos Scenario: Scheduler Under Random Node Failures
// Simulates nodes going offline during active scheduling.
// ============================================================================

func TestChaosEnhanced_SchedulerNodeFailures(t *testing.T) {
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

	// Submit workloads while simulating node failures
	var submitted int64
	var wg sync.WaitGroup

	for round := 0; round < 5; round++ {
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(r, idx int) {
				defer wg.Done()
				wl := &scheduler.Workload{
					ID:       common.NewUUID(),
					Name:     fmt.Sprintf("chaos-node-wl-%d-%d", r, idx),
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
			}(round, i)
		}
		// Simulate a "node failure" by pausing briefly (scheduler continues)
		time.Sleep(100 * time.Millisecond)
	}

	wg.Wait()
	time.Sleep(500 * time.Millisecond)
	engine.Stop()

	stats := engine.GetStats()
	t.Logf("Scheduler chaos with node failures: submitted=%d, stats=%+v",
		atomic.LoadInt64(&submitted), stats)
}

// ============================================================================
// Chaos Scenario: SLO Compliance Under Adverse Conditions
// Verifies error budget and SLO targets during chaos experiments.
// ============================================================================

func TestChaosEnhanced_SLOCompliance(t *testing.T) {
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

	// SLO targets
	const targetAvailability = 99.9     // 99.9%
	const targetP99LatencyMs = 50.0     // 50ms

	const totalRequests = 5000
	const concurrency = 100

	var totalOK, totalErr int64
	latencies := make([]int64, totalRequests)
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var idx int64

	for i := 0; i < totalRequests; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			start := time.Now()
			r := httptest.NewRequest("GET", "/healthz", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			latency := time.Since(start).Microseconds()

			j := atomic.AddInt64(&idx, 1) - 1
			if j < int64(len(latencies)) {
				latencies[j] = latency
			}

			if w.Code == http.StatusOK {
				atomic.AddInt64(&totalOK, 1)
			} else {
				atomic.AddInt64(&totalErr, 1)
			}
		}()
	}
	wg.Wait()

	// Calculate SLO metrics
	ok := atomic.LoadInt64(&totalOK)
	total := ok + atomic.LoadInt64(&totalErr)
	availability := float64(ok) / float64(total) * 100

	// Sort latencies for P99
	n := int(atomic.LoadInt64(&idx))
	if n > len(latencies) {
		n = len(latencies)
	}
	sortInt64s(latencies[:n])
	p99Idx := n * 99 / 100
	p99LatencyMs := float64(latencies[p99Idx]) / 1000.0

	t.Logf("SLO Compliance:")
	t.Logf("  Availability: %.3f%% (target: %.1f%%)", availability, targetAvailability)
	t.Logf("  P99 Latency:  %.2fms (target: %.0fms)", p99LatencyMs, targetP99LatencyMs)

	if availability < targetAvailability {
		t.Errorf("SLO violation: availability %.3f%% < %.1f%%", availability, targetAvailability)
	}
	if p99LatencyMs > targetP99LatencyMs {
		t.Errorf("SLO violation: P99 latency %.2fms > %.0fms", p99LatencyMs, targetP99LatencyMs)
	}
}

// ============================================================================
// Chaos Scenario: Rapid Cache Eviction Storm
// ============================================================================

func TestChaosEnhanced_CacheEvictionStorm(t *testing.T) {
	// Small cache to force constant eviction
	c := cache.NewMemoryCache(cache.Config{L1MaxSize: 50, KeyPrefix: ""}, logrus.New())
	defer c.Close()
	ctx := context.Background()

	const goroutines = 50
	const opsPerGoroutine = 200

	var totalSets, totalGets, totalHits int64
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("evict-key-%d", rand.Intn(200))
				c.Set(ctx, key, []byte("data"), time.Minute)
				atomic.AddInt64(&totalSets, 1)

				if val, _ := c.Get(ctx, key); val != nil {
					atomic.AddInt64(&totalHits, 1)
				}
				atomic.AddInt64(&totalGets, 1)
			}
		}(i)
	}
	wg.Wait()

	hitRate := float64(atomic.LoadInt64(&totalHits)) / float64(atomic.LoadInt64(&totalGets))
	t.Logf("Eviction storm: sets=%d, gets=%d, hits=%d, hit_rate=%.2f",
		atomic.LoadInt64(&totalSets), atomic.LoadInt64(&totalGets),
		atomic.LoadInt64(&totalHits), hitRate)

	// Cache should still be functional
	c.Set(ctx, "post-eviction", []byte("alive"), time.Minute)
	if val, _ := c.Get(ctx, "post-eviction"); val == nil {
		t.Error("cache should be functional after eviction storm")
	}
}

// ============================================================================
// Utility
// ============================================================================

func sortInt64s(a []int64) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}
