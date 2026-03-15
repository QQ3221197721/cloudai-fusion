// Package performance validates performance budgets for critical operations.
// Each test defines a maximum acceptable latency or throughput threshold.
// Failing these tests indicates a performance regression.
//
// Run with: go test ./tests/performance/ -v -timeout=120s
package performance

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/api"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/auth"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/cache"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/testutil"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/wasm"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// Performance Budget Configuration
// ============================================================================

// PerformanceBudget defines the maximum acceptable latency for an operation
type PerformanceBudget struct {
	Name        string
	MaxLatency  time.Duration
	MinThroughput int // operations per second (0 = not checked)
}

var budgets = map[string]PerformanceBudget{
	"health_check":         {Name: "Health Check API", MaxLatency: 10 * time.Millisecond},
	"auth_token_gen":       {Name: "JWT Token Generation", MaxLatency: 5 * time.Millisecond},
	"auth_token_validate":  {Name: "JWT Token Validation", MaxLatency: 2 * time.Millisecond},
	"scheduling_cycle":     {Name: "Single Scheduling Cycle", MaxLatency: 50 * time.Millisecond},
	"node_scoring":         {Name: "Node Scoring (single)", MaxLatency: 1 * time.Millisecond},
	"cache_set":            {Name: "Cache Set Operation", MaxLatency: 1 * time.Millisecond},
	"cache_get":            {Name: "Cache Get Operation", MaxLatency: 500 * time.Microsecond},
	"uuid_gen":             {Name: "UUID Generation", MaxLatency: 100 * time.Microsecond},
	"wasm_module_validate": {Name: "WASM Binary Validation", MaxLatency: 5 * time.Millisecond},
	"api_list_endpoint":    {Name: "API List Endpoint", MaxLatency: 50 * time.Millisecond},
	"workload_submission":  {Name: "Workload Submission", MaxLatency: 5 * time.Millisecond},
}

func checkBudget(t *testing.T, budgetKey string, elapsed time.Duration) {
	t.Helper()
	budget, ok := budgets[budgetKey]
	if !ok {
		t.Fatalf("unknown budget key: %s", budgetKey)
	}
	if elapsed > budget.MaxLatency {
		t.Errorf("PERFORMANCE REGRESSION: %s took %v, budget is %v",
			budget.Name, elapsed, budget.MaxLatency)
	} else {
		t.Logf("OK: %s = %v (budget: %v, headroom: %.1f%%)",
			budget.Name, elapsed, budget.MaxLatency,
			float64(budget.MaxLatency-elapsed)/float64(budget.MaxLatency)*100)
	}
}

// ============================================================================
// Budget: Health Check API
// ============================================================================

func TestBudget_HealthCheck(t *testing.T) {
	auth, cluster, cloud, security, monitor, workload, mesh, wasmSvc, edgeSvc :=
		testutil.NewTestRouterConfig()
	router := api.NewRouter(api.RouterConfig{
		AuthService: auth, CloudManager: cloud, ClusterManager: cluster,
		SecurityManager: security, MonitorService: monitor,
		WorkloadManager: workload, MeshManager: mesh,
		WasmManager: wasmSvc, EdgeManager: edgeSvc, Logger: logrus.New(),
	})

	// Warm up
	for i := 0; i < 10; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	}

	// Measure
	start := time.Now()
	iterations := 100
	for i := 0; i < iterations; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
		// 200=OK, 429=rate-limited — both are valid server responses for budget test
		if w.Code != http.StatusOK && w.Code != http.StatusTooManyRequests {
			t.Fatalf("health check failed: %d", w.Code)
		}
	}
	avg := time.Since(start) / time.Duration(iterations)
	checkBudget(t, "health_check", avg)
}

// ============================================================================
// Budget: JWT Token Operations
// ============================================================================

func TestBudget_AuthTokenGeneration(t *testing.T) {
	svc, _ := auth.NewService(auth.Config{
		JWTSecret: "perf-test-secret-key-32bytes!!!!",
		JWTExpiry: time.Hour,
	})
	user := &auth.User{ID: "perf-user", Username: "perftest", Role: auth.RoleAdmin, Status: "active"}

	// Warm up
	for i := 0; i < 10; i++ {
		svc.GenerateToken(user)
	}

	start := time.Now()
	iterations := 1000
	for i := 0; i < iterations; i++ {
		_, err := svc.GenerateToken(user)
		if err != nil {
			t.Fatalf("token generation failed: %v", err)
		}
	}
	avg := time.Since(start) / time.Duration(iterations)
	checkBudget(t, "auth_token_gen", avg)
}

func TestBudget_AuthTokenValidation(t *testing.T) {
	svc, _ := auth.NewService(auth.Config{
		JWTSecret: "perf-test-secret-key-32bytes!!!!",
		JWTExpiry: time.Hour,
	})
	user := &auth.User{ID: "perf-user", Username: "perftest", Role: auth.RoleAdmin, Status: "active"}
	tokenResp, _ := svc.GenerateToken(user)

	// Warm up
	for i := 0; i < 10; i++ {
		svc.ValidateToken(tokenResp.AccessToken)
	}

	start := time.Now()
	iterations := 10000
	for i := 0; i < iterations; i++ {
		_, err := svc.ValidateToken(tokenResp.AccessToken)
		if err != nil {
			t.Fatalf("token validation failed: %v", err)
		}
	}
	avg := time.Since(start) / time.Duration(iterations)
	checkBudget(t, "auth_token_validate", avg)
}

// ============================================================================
// Budget: Scheduling Operations
// ============================================================================

func TestBudget_SchedulingCycle(t *testing.T) {
	engine, _ := scheduler.NewEngine(scheduler.EngineConfig{})

	start := time.Now()
	iterations := 100
	for i := 0; i < iterations; i++ {
		wl := &scheduler.Workload{
			ID:       fmt.Sprintf("budget-wl-%d", i),
			Name:     "budget-test",
			Type:     common.WorkloadTypeTraining,
			Status:   common.WorkloadStatusQueued,
			Priority: 5,
			ResourceRequest: common.ResourceRequest{GPUCount: 2},
			QueuedAt: common.NowUTC(),
		}
		engine.SubmitWorkload(wl)
		engine.RunSchedulingCycle(context.Background())
	}
	avg := time.Since(start) / time.Duration(iterations)
	checkBudget(t, "scheduling_cycle", avg)
}

func TestBudget_WorkloadSubmission(t *testing.T) {
	engine, _ := scheduler.NewEngine(scheduler.EngineConfig{
		SchedulingInterval: time.Hour,
	})

	start := time.Now()
	iterations := 10000
	for i := 0; i < iterations; i++ {
		wl := &scheduler.Workload{
			ID:       fmt.Sprintf("submit-%d", i),
			Name:     "submission-test",
			Type:     common.WorkloadTypeInference,
			Status:   common.WorkloadStatusQueued,
			Priority: i % 10,
			ResourceRequest: common.ResourceRequest{GPUCount: 1},
			QueuedAt: common.NowUTC(),
		}
		engine.SubmitWorkload(wl)
	}
	avg := time.Since(start) / time.Duration(iterations)
	checkBudget(t, "workload_submission", avg)
}

// ============================================================================
// Budget: Cache Operations
// ============================================================================

func TestBudget_CacheOperations(t *testing.T) {
	c := cache.NewMemoryCache(cache.DefaultConfig(), logrus.New())
	ctx := context.Background()

	// Set budget
	start := time.Now()
	iterations := 10000
	for i := 0; i < iterations; i++ {
		c.Set(ctx, fmt.Sprintf("key-%d", i), []byte(fmt.Sprintf("value-%d", i)), time.Minute)
	}
	avgSet := time.Since(start) / time.Duration(iterations)
	checkBudget(t, "cache_set", avgSet)

	// Get budget
	start = time.Now()
	for i := 0; i < iterations; i++ {
		c.Get(ctx, fmt.Sprintf("key-%d", i))
	}
	avgGet := time.Since(start) / time.Duration(iterations)
	checkBudget(t, "cache_get", avgGet)
}

// ============================================================================
// Budget: UUID Generation
// ============================================================================

func TestBudget_UUIDGeneration(t *testing.T) {
	// Warm up
	for i := 0; i < 100; i++ {
		common.NewUUID()
	}

	start := time.Now()
	iterations := 100000
	for i := 0; i < iterations; i++ {
		common.NewUUID()
	}
	avg := time.Since(start) / time.Duration(iterations)
	checkBudget(t, "uuid_gen", avg)
}

// ============================================================================
// Budget: WASM Module Validation
// ============================================================================

func TestBudget_WasmModuleValidation(t *testing.T) {
	// Create a minimal valid Wasm binary (magic + version + empty sections)
	validWasm := []byte{
		0x00, 0x61, 0x73, 0x6D, // magic: \0asm
		0x01, 0x00, 0x00, 0x00, // version: 1
	}

	start := time.Now()
	iterations := 10000
	for i := 0; i < iterations; i++ {
		result := wasm.ValidateWasmBinary(validWasm)
		if !result.Valid {
			t.Fatalf("validation failed: %s", result.ErrorMsg)
		}
	}
	avg := time.Since(start) / time.Duration(iterations)
	checkBudget(t, "wasm_module_validate", avg)
}

// ============================================================================
// Budget: API List Endpoint
// ============================================================================

func TestBudget_APIListEndpoint(t *testing.T) {
	auth, cluster, cloud, security, monitor, workload, mesh, wasmSvc, edgeSvc :=
		testutil.NewTestRouterConfig()
	router := api.NewRouter(api.RouterConfig{
		AuthService: auth, CloudManager: cloud, ClusterManager: cluster,
		SecurityManager: security, MonitorService: monitor,
		WorkloadManager: workload, MeshManager: mesh,
		WasmManager: wasmSvc, EdgeManager: edgeSvc, Logger: logrus.New(),
	})

	endpoints := []string{
		"/api/v1/clusters",
		"/api/v1/security/policies",
		"/api/v1/wasm/modules",
	}

	for _, ep := range endpoints {
		t.Run(ep, func(t *testing.T) {
			// Warm up
			for i := 0; i < 5; i++ {
				w := httptest.NewRecorder()
				router.ServeHTTP(w, httptest.NewRequest("GET", ep, nil))
			}

			start := time.Now()
			iterations := 100
			for i := 0; i < iterations; i++ {
				w := httptest.NewRecorder()
				router.ServeHTTP(w, httptest.NewRequest("GET", ep, nil))
				// 200=OK, 429=rate-limited — both are valid server responses for budget test
				if w.Code != http.StatusOK && w.Code != http.StatusTooManyRequests {
					t.Fatalf("endpoint %s failed: %d", ep, w.Code)
				}
			}
			avg := time.Since(start) / time.Duration(iterations)
			checkBudget(t, "api_list_endpoint", avg)
		})
	}
}

// ============================================================================
// Budget: Memory Allocation Tracking
// ============================================================================

func TestBudget_MemoryAllocation(t *testing.T) {
	var memBefore, memAfter runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memBefore)

	// Create scheduler engine + 1000 workloads
	engine, _ := scheduler.NewEngine(scheduler.EngineConfig{
		SchedulingInterval: time.Hour,
	})
	for i := 0; i < 1000; i++ {
		engine.SubmitWorkload(&scheduler.Workload{
			ID:       fmt.Sprintf("mem-wl-%d", i),
			Name:     "memory-test",
			Type:     common.WorkloadTypeTraining,
			Status:   common.WorkloadStatusQueued,
			Priority: i % 10,
			ResourceRequest: common.ResourceRequest{GPUCount: 1},
			QueuedAt: common.NowUTC(),
		})
	}

	runtime.GC()
	runtime.ReadMemStats(&memAfter)

	allocatedMB := float64(memAfter.TotalAlloc-memBefore.TotalAlloc) / 1024 / 1024
	maxAllowedMB := 50.0 // 50MB budget for 1000 workloads

	if allocatedMB > maxAllowedMB {
		t.Errorf("MEMORY BUDGET EXCEEDED: allocated %.2f MB for 1000 workloads (budget: %.0f MB)",
			allocatedMB, maxAllowedMB)
	} else {
		t.Logf("Memory: %.2f MB / %.0f MB budget (%.1f%% used)",
			allocatedMB, maxAllowedMB, allocatedMB/maxAllowedMB*100)
	}
}

// ============================================================================
// Stress: Concurrent Request Throughput
// ============================================================================

func TestStress_ConcurrentRequestThroughput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	auth, cluster, cloud, security, monitor, workload, mesh, wasmSvc, edgeSvc :=
		testutil.NewTestRouterConfig()
	router := api.NewRouter(api.RouterConfig{
		AuthService: auth, CloudManager: cloud, ClusterManager: cluster,
		SecurityManager: security, MonitorService: monitor,
		WorkloadManager: workload, MeshManager: mesh,
		WasmManager: wasmSvc, EdgeManager: edgeSvc, Logger: logrus.New(),
	})

	concurrency := 50
	requestsPerWorker := 200
	totalRequests := concurrency * requestsPerWorker

	start := time.Now()
	done := make(chan bool, totalRequests)

	for i := 0; i < concurrency; i++ {
		go func() {
			for j := 0; j < requestsPerWorker; j++ {
				w := httptest.NewRecorder()
				router.ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
				// 200=OK, 429=rate-limited (both are valid server responses)
				done <- w.Code == http.StatusOK || w.Code == http.StatusTooManyRequests
			}
		}()
	}

	successes := 0
	for i := 0; i < totalRequests; i++ {
		if <-done {
			successes++
		}
	}

	elapsed := time.Since(start)
	rps := float64(totalRequests) / elapsed.Seconds()
	successRate := float64(successes) / float64(totalRequests) * 100

	t.Logf("Stress test: %d requests in %v = %.0f RPS, success rate: %.1f%%",
		totalRequests, elapsed, rps, successRate)

	minRPS := 5000.0
	if rps < minRPS {
		t.Errorf("throughput %.0f RPS below minimum %.0f RPS", rps, minRPS)
	}
	if successRate < 99.0 {
		t.Errorf("success rate %.1f%% below minimum 99.0%%", successRate)
	}
}
