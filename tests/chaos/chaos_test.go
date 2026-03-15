// Package chaos provides chaos engineering tests for CloudAI Fusion.
// Implements Chaos Mesh-style fault injection scenarios to verify system
// resilience under adverse conditions: network partitions, pod failures,
// resource pressure, and cascading failure scenarios.
//
// Run with: go test ./tests/chaos/ -v -timeout=300s
// Skip in CI: go test ./tests/chaos/ -short
package chaos

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
	"github.com/cloudai-fusion/cloudai-fusion/pkg/edge"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/resilience"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/testutil"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/wasm"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// Chaos Mesh Integration — CRD Definitions
// ============================================================================

// ChaosMeshExperiment defines a Chaos Mesh experiment CRD
type ChaosMeshExperiment struct {
	APIVersion string                 `json:"apiVersion"`
	Kind       string                 `json:"kind"` // NetworkChaos, PodChaos, StressChaos, IOChaos
	Metadata   map[string]interface{} `json:"metadata"`
	Spec       map[string]interface{} `json:"spec"`
}

// NewNetworkChaos creates a network partition experiment
func NewNetworkChaos(name, namespace string, action string, durationSec int) *ChaosMeshExperiment {
	return &ChaosMeshExperiment{
		APIVersion: "chaos-mesh.org/v1alpha1",
		Kind:       "NetworkChaos",
		Metadata: map[string]interface{}{
			"name":      name,
			"namespace": namespace,
			"labels": map[string]string{
				"app.kubernetes.io/managed-by": "cloudai-fusion",
				"chaos.cloudai-fusion.io/type": "network",
			},
		},
		Spec: map[string]interface{}{
			"action":   action, // partition, loss, delay, duplicate, corrupt
			"mode":     "all",
			"duration": fmt.Sprintf("%ds", durationSec),
			"selector": map[string]interface{}{
				"namespaces":     []string{namespace},
				"labelSelectors": map[string]string{"app": "cloudai-fusion"},
			},
			"direction": "both",
		},
	}
}

// NewPodChaos creates a pod failure injection experiment
func NewPodChaos(name, namespace string, action string, durationSec int) *ChaosMeshExperiment {
	return &ChaosMeshExperiment{
		APIVersion: "chaos-mesh.org/v1alpha1",
		Kind:       "PodChaos",
		Metadata: map[string]interface{}{
			"name":      name,
			"namespace": namespace,
			"labels": map[string]string{
				"app.kubernetes.io/managed-by": "cloudai-fusion",
				"chaos.cloudai-fusion.io/type": "pod",
			},
		},
		Spec: map[string]interface{}{
			"action":   action, // pod-kill, pod-failure, container-kill
			"mode":     "one",
			"duration": fmt.Sprintf("%ds", durationSec),
			"selector": map[string]interface{}{
				"namespaces": []string{namespace},
			},
		},
	}
}

// NewStressChaos creates a resource stress experiment
func NewStressChaos(name, namespace string, cpuWorkers int, memoryMB int, durationSec int) *ChaosMeshExperiment {
	return &ChaosMeshExperiment{
		APIVersion: "chaos-mesh.org/v1alpha1",
		Kind:       "StressChaos",
		Metadata: map[string]interface{}{
			"name":      name,
			"namespace": namespace,
			"labels": map[string]string{
				"app.kubernetes.io/managed-by": "cloudai-fusion",
				"chaos.cloudai-fusion.io/type": "stress",
			},
		},
		Spec: map[string]interface{}{
			"mode":     "all",
			"duration": fmt.Sprintf("%ds", durationSec),
			"stressors": map[string]interface{}{
				"cpu": map[string]interface{}{
					"workers": cpuWorkers,
					"load":    80,
				},
				"memory": map[string]interface{}{
					"workers": 1,
					"size":    fmt.Sprintf("%dMB", memoryMB),
				},
			},
			"selector": map[string]interface{}{
				"namespaces": []string{namespace},
			},
		},
	}
}

// NewIOChaos creates an I/O fault injection experiment
func NewIOChaos(name, namespace string, action string, durationSec int) *ChaosMeshExperiment {
	return &ChaosMeshExperiment{
		APIVersion: "chaos-mesh.org/v1alpha1",
		Kind:       "IOChaos",
		Metadata: map[string]interface{}{
			"name":      name,
			"namespace": namespace,
			"labels": map[string]string{
				"app.kubernetes.io/managed-by": "cloudai-fusion",
				"chaos.cloudai-fusion.io/type": "io",
			},
		},
		Spec: map[string]interface{}{
			"action":   action, // latency, fault, attrOverride
			"mode":     "all",
			"duration": fmt.Sprintf("%ds", durationSec),
			"delay":    "100ms",
			"selector": map[string]interface{}{
				"namespaces": []string{namespace},
			},
		},
	}
}

// ============================================================================
// Test: Chaos Mesh CRD Generation
// ============================================================================

func TestChaosMesh_CRDGeneration(t *testing.T) {
	tests := []struct {
		name     string
		chaos    *ChaosMeshExperiment
		expKind  string
	}{
		{
			name:    "network_partition",
			chaos:   NewNetworkChaos("net-partition-test", "cloudai-fusion", "partition", 30),
			expKind: "NetworkChaos",
		},
		{
			name:    "pod_kill",
			chaos:   NewPodChaos("pod-kill-test", "cloudai-fusion", "pod-kill", 60),
			expKind: "PodChaos",
		},
		{
			name:    "cpu_stress",
			chaos:   NewStressChaos("stress-test", "cloudai-fusion", 4, 256, 120),
			expKind: "StressChaos",
		},
		{
			name:    "io_latency",
			chaos:   NewIOChaos("io-latency-test", "cloudai-fusion", "latency", 60),
			expKind: "IOChaos",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.chaos.Kind != tt.expKind {
				t.Errorf("expected kind %s, got %s", tt.expKind, tt.chaos.Kind)
			}
			if tt.chaos.APIVersion != "chaos-mesh.org/v1alpha1" {
				t.Errorf("unexpected API version: %s", tt.chaos.APIVersion)
			}

			// Verify JSON serialization
			data, err := json.Marshal(tt.chaos)
			if err != nil {
				t.Fatalf("failed to marshal chaos CRD: %v", err)
			}
			if len(data) == 0 {
				t.Error("serialized CRD should not be empty")
			}

			// Verify labels
			labels := tt.chaos.Metadata["labels"].(map[string]string)
			if labels["app.kubernetes.io/managed-by"] != "cloudai-fusion" {
				t.Error("missing managed-by label")
			}
		})
	}
}

// ============================================================================
// Test: Network Partition Resilience
// Simulate intermittent API failures and verify system recovers
// ============================================================================

func TestChaos_NetworkPartition(t *testing.T) {
	var failureCount int64
	var successCount int64

	// Create a mock server that simulates intermittent failures
	failRate := 0.3 // 30% failure rate
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rand.Float64() < failRate {
			atomic.AddInt64(&failureCount, 1)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		atomic.AddInt64(&successCount, 1)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	// Send 100 requests concurrently
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/health", nil)
			client := &http.Client{Timeout: 2 * time.Second}
			resp, err := client.Do(req)
			if err == nil {
				resp.Body.Close()
			}
		}()
	}
	wg.Wait()

	total := atomic.LoadInt64(&successCount) + atomic.LoadInt64(&failureCount)
	if total < 90 { // at least 90% of requests should complete
		t.Errorf("expected at least 90 completed requests, got %d", total)
	}

	successRate := float64(atomic.LoadInt64(&successCount)) / float64(total) * 100
	t.Logf("Network partition test: success=%.1f%%, failure=%.1f%%, total=%d",
		successRate, 100-successRate, total)
}

// ============================================================================
// Test: Circuit Breaker Under Chaos
// ============================================================================

func TestChaos_CircuitBreakerResilience(t *testing.T) {
	cb := resilience.NewCircuitBreaker(resilience.CircuitBreakerConfig{
		FailureThreshold:   3,
		SuccessThreshold:   2,
		Timeout:            200 * time.Millisecond,
		MaxHalfOpenRequests: 2,
	})

	// Phase 1: Induce failures to trip the breaker
	for i := 0; i < 5; i++ {
		_ = cb.Execute(func() error {
			return fmt.Errorf("simulated failure %d", i)
		})
	}

	// Circuit should be open now
	err := cb.Execute(func() error { return nil })
	if err == nil {
		t.Error("circuit breaker should be open after 5 failures")
	}

	// Phase 2: Wait for reset timeout
	time.Sleep(250 * time.Millisecond)

	// Phase 3: Half-open — next call should pass
	err = cb.Execute(func() error { return nil })
	if err != nil {
		t.Errorf("circuit breaker should allow call in half-open state: %v", err)
	}
}

// ============================================================================
// Test: Cache Resilience Under Pressure
// ============================================================================

func TestChaos_CacheUnderPressure(t *testing.T) {
	c := cache.NewMemoryCache(cache.DefaultConfig(), logrus.New())
	ctx := context.Background()

	// Hammer the cache with concurrent writes and reads
	var wg sync.WaitGroup
	const goroutines = 50
	const opsPerGoroutine = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("chaos-key-%d-%d", id, j)
				c.Set(ctx, key, []byte(fmt.Sprintf("value-%d", j)), 5*time.Second)
				if val, _ := c.Get(ctx, key); val != nil {
					_ = val // use it to prevent optimization
				}
			}
		}(i)
	}
	wg.Wait()

	// Cache should still be functional
	c.Set(ctx, "post-chaos", []byte("alive"), time.Minute)
	if val, _ := c.Get(ctx, "post-chaos"); val == nil {
		t.Error("cache should be functional after pressure test")
	}
}

// ============================================================================
// Test: Scheduler Resilience Under Load
// ============================================================================

func TestChaos_SchedulerUnderLoad(t *testing.T) {
	engine, err := scheduler.NewEngine(scheduler.EngineConfig{
		SchedulingInterval: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go engine.Run(ctx)
	time.Sleep(50 * time.Millisecond)

	// Submit 50 workloads rapidly
	for i := 0; i < 50; i++ {
		wl := &scheduler.Workload{
			ID:       common.NewUUID(),
			Name:     fmt.Sprintf("chaos-workload-%d", i),
			Type:     common.WorkloadTypeInference,
			Status:   common.WorkloadStatusQueued,
			Priority: rand.Intn(10) + 1,
			ResourceRequest: common.ResourceRequest{
				GPUCount: rand.Intn(4) + 1,
			},
			QueuedAt: common.NowUTC(),
		}
		engine.SubmitWorkload(wl)
	}

	// Let scheduler process
	time.Sleep(500 * time.Millisecond)

	stats := engine.GetStats()
	if stats == nil {
		t.Fatal("engine stats should not be nil")
	}

	engine.Stop()
	t.Logf("Scheduler chaos test: stats=%+v", stats)
}

// ============================================================================
// Test: API Under Concurrent Chaos (mixed load + failures)
// ============================================================================

func TestChaos_APIMixedLoad(t *testing.T) {
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

	// Mix of valid and invalid requests
	requests := []struct {
		method string
		path   string
		body   string
		valid  bool
	}{
		{"GET", "/healthz", "", true},
		{"GET", "/api/v1/clusters", "", true},
		{"POST", "/api/v1/auth/login", `{"username":"admin","password":"pass"}`, true},
		{"GET", "/nonexistent", "", false},
		{"POST", "/api/v1/clusters", `invalid-json`, false},
		{"GET", "/api/v1/security/policies", "", true},
		{"GET", "/api/v1/wasm/modules", "", true},
		{"GET", "/api/v1/edge/topology", "", true},
		{"DELETE", "/api/v1/clusters/nonexistent", "", true},
	}

	var wg sync.WaitGroup
	var totalOK, totalErr int64

	for round := 0; round < 20; round++ {
		for _, req := range requests {
			wg.Add(1)
			go func(method, path, body string, valid bool) {
				defer wg.Done()
				var r *http.Request
				if body != "" {
					r = httptest.NewRequest(method, path, strings.NewReader(body))
					r.Header.Set("Content-Type", "application/json")
				} else {
					r = httptest.NewRequest(method, path, nil)
				}
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				if w.Code >= 200 && w.Code < 500 {
					atomic.AddInt64(&totalOK, 1)
				} else {
					atomic.AddInt64(&totalErr, 1)
				}
			}(req.method, req.path, req.body, req.valid)
		}
	}

	wg.Wait()

	total := atomic.LoadInt64(&totalOK) + atomic.LoadInt64(&totalErr)
	t.Logf("API chaos test: total=%d, ok=%d, server_errors=%d",
		total, atomic.LoadInt64(&totalOK), atomic.LoadInt64(&totalErr))

	// No server errors (5xx) should occur
	if atomic.LoadInt64(&totalErr) > 0 {
		t.Errorf("got %d server errors under chaos load", atomic.LoadInt64(&totalErr))
	}
}

// ============================================================================
// Test: Edge Subsystem Resilience (disconnection simulation)
// ============================================================================

func TestChaos_EdgeDisconnection(t *testing.T) {
	hub := edge.NewEdgeCollabHub(edge.DefaultEdgeCollabConfig(), logrus.New())
	if hub == nil {
		t.Fatal("EdgeCollabHub should not be nil")
	}

	nodeID := "chaos-edge-node-1"

	// Simulate disconnect
	hub.Autonomy.NotifyDisconnect(nodeID)

	state := hub.Autonomy.GetNodeAutonomyState(nodeID)
	if state.State != edge.AutonomyActive {
		t.Errorf("expected autonomous mode after disconnect, got %s", state.State)
	}

	// Buffer messages during disconnect
	for i := 0; i < 20; i++ {
		hub.Autonomy.BufferMessage(nodeID, &edge.ForwardMessage{
			ID:        common.NewUUID(),
			NodeID:    nodeID,
			Type:      "metrics",
			Payload:   map[string]interface{}{"seq": i},
			Priority:  i % 3,
			SizeBytes: 128,
		})
	}

	state = hub.Autonomy.GetNodeAutonomyState(nodeID)
	if state.PendingForwards == 0 {
		t.Error("should have buffered messages during disconnect")
	}

	// Reconnect
	forwarded, err := hub.Autonomy.NotifyReconnect(nodeID)
	if err != nil {
		t.Fatalf("reconnect failed: %v", err)
	}
	if len(forwarded) == 0 {
		t.Error("should have drained forward buffer on reconnect")
	}

	state = hub.Autonomy.GetNodeAutonomyState(nodeID)
	if state.State == edge.AutonomyActive {
		t.Error("should not be in autonomous mode after reconnect")
	}
}

// ============================================================================
// Test: WASM Sandbox Isolation Under Chaos
// ============================================================================

func TestChaos_WasmSandboxIsolation(t *testing.T) {
	profiles := wasm.PredefinedSandboxProfiles()

	// Verify that minimal profile cannot escalate to privileged
	minimal := profiles["minimal"]
	privileged := profiles["privileged"]

	if minimal.MemoryLimitMB >= privileged.MemoryLimitMB {
		t.Error("minimal memory limit should be less than privileged")
	}
	if minimal.MaxNetworkConns >= privileged.MaxNetworkConns {
		t.Error("minimal network limit should be less than privileged")
	}
	if len(minimal.AllowedCapabilities) >= len(privileged.AllowedCapabilities) {
		t.Error("minimal capabilities should be fewer than privileged")
	}
}

// ============================================================================
// Test: Policy Engine Under Malformed Input
// ============================================================================

func TestChaos_PolicyEngineMalformedInput(t *testing.T) {
	pe := plugin.NewPolicyEngine()

	// Register a template
	templates := plugin.PredefinedConstraintTemplates()
	for _, tmpl := range templates {
		_ = pe.RegisterTemplate(tmpl)
	}

	// Create constraint
	_ = pe.CreateConstraint(&plugin.PolicyConstraint{
		Name:              "test-constraint",
		TemplateName:      "require-resource-limits",
		EnforcementAction: "deny",
	})

	// Feed malformed resources
	malformedInputs := []json.RawMessage{
		json.RawMessage(`{}`),
		json.RawMessage(`null`),
		json.RawMessage(`{"spec":null}`),
		json.RawMessage(`{"spec":{"containers":[]}}`),
		json.RawMessage(`{"spec":{"containers":[{"name":""}]}}`),
		json.RawMessage(`[]`),
	}

	for i, input := range malformedInputs {
		t.Run(fmt.Sprintf("malformed_%d", i), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("policy engine panicked on malformed input: %v", r)
				}
			}()

			kind := plugin.ResourceKind{Kind: "Pod", Group: ""}
			_ = pe.EvaluatePolicy(context.Background(), input, kind, "default")
		})
	}
}

// ============================================================================
// Test: Admission Chain Under Rapid Registration/Unregistration
// ============================================================================

func TestChaos_AdmissionChainRapidChanges(t *testing.T) {
	chain := plugin.NewAdmissionChain()

	var wg sync.WaitGroup

	// Rapidly register and unregister webhooks concurrently
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			name := fmt.Sprintf("chaos-webhook-%d", id)
			_ = chain.RegisterWebhook(&plugin.AdmissionWebhookConfig{
				Name:     name,
				Phase:    plugin.PhaseValidation,
				URL:      fmt.Sprintf("https://webhook-%d.example.com/validate", id),
				Priority: id * 10,
			})
			// Small delay then unregister
			time.Sleep(time.Duration(rand.Intn(10)) * time.Millisecond)
			_ = chain.UnregisterWebhook(name)
		}(i)
	}

	wg.Wait()

	// Chain should still be functional
	stats := chain.GetStats()
	if stats == nil {
		t.Error("admission chain stats should not be nil after chaos")
	}
}
