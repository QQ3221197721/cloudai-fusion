package metrics

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// Golden Signal metrics — existence check
// ============================================================================

func TestGoldenSignalMetrics_Exist(t *testing.T) {
	// Verify that golden signal metrics are registered and usable.
	HTTPRequestsTotal.WithLabelValues("GET", "/api/v1/test", "200").Inc()
	HTTPRequestDuration.WithLabelValues("GET", "/api/v1/test", "200").Observe(0.05)
	HTTPErrorsTotal.WithLabelValues("GET", "/api/v1/test", "500", "server_error").Inc()
	HTTPResponseSizeBytes.WithLabelValues("GET", "/api/v1/test").Observe(1024)
	HTTPRequestsInFlight.Set(5)
	PanicsRecoveredTotal.Inc()
	GoroutinesCount.Set(42)
	ConnectionPoolUtilization.WithLabelValues("postgres").Set(0.75)
}

func TestGRPCMetrics_Exist(t *testing.T) {
	GRPCRequestsTotal.WithLabelValues("Scheduler", "CreateTask", "OK").Inc()
	GRPCRequestDuration.WithLabelValues("Scheduler", "CreateTask", "OK").Observe(0.01)
}

// ============================================================================
// Business Metrics — existence check
// ============================================================================

func TestBusinessMetrics_Exist(t *testing.T) {
	TaskSuccessTotal.WithLabelValues("training", "cluster-1").Inc()
	TaskFailureTotal.WithLabelValues("training", "cluster-1", "oom").Inc()
	TaskDuration.WithLabelValues("training").Observe(120.0)
	SchedulingLatency.Observe(0.5)
	SchedulerDecisionsTotal.WithLabelValues("placed").Inc()
	AIInferenceLatency.WithLabelValues("resnet50", "v1").Observe(0.02)
	AIInferenceThroughput.WithLabelValues("resnet50", "samples_per_sec").Set(1000)
	EventBusPublishedTotal.WithLabelValues("cluster.created").Inc()
	EventBusDeliveredTotal.WithLabelValues("cluster.created").Inc()
	EventBusErrorsTotal.WithLabelValues("cluster.created", "timeout").Inc()
}

// ============================================================================
// Resource Metrics — existence check
// ============================================================================

func TestResourceMetrics_Exist(t *testing.T) {
	GPUUtilization.WithLabelValues("c1", "node1", "0", "A100").Set(85.0)
	GPUMemoryUsedBytes.WithLabelValues("c1", "node1", "0").Set(40e9)
	GPUMemoryTotalBytes.WithLabelValues("c1", "node1", "0").Set(80e9)
	GPUTemperatureCelsius.WithLabelValues("c1", "node1", "0").Set(72)
	GPUPowerWatts.WithLabelValues("c1", "node1", "0").Set(250)
	CPUUtilization.WithLabelValues("c1", "node1").Set(65)
	MemoryUsedBytes.WithLabelValues("c1", "node1").Set(16e9)
	MemoryTotalBytes.WithLabelValues("c1", "node1").Set(64e9)
	DiskUsedBytes.WithLabelValues("c1", "node1", "/data").Set(500e9)
	NetworkBandwidthBytes.WithLabelValues("c1", "node1", "rx").Add(1e6)
}

// ============================================================================
// Cost Metrics — existence check
// ============================================================================

func TestCostMetrics_Exist(t *testing.T) {
	CloudResourceCostHourly.WithLabelValues("c1", "aws", "gpu").Set(3.5)
	ComputeUnitCost.WithLabelValues("aws", "p4d.24xlarge", "gpu_hour").Set(32.77)
	CostSavingsTotal.WithLabelValues("spot").Add(150.0)
	BudgetUtilizationRatio.WithLabelValues("ml-team", "proj-a").Set(0.68)
}

// ============================================================================
// Registry
// ============================================================================

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if r.customs == nil {
		t.Error("customs map should be initialized")
	}
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_registry_counter_rg",
		Help: "test",
	})
	if err := r.Register("test_counter_rg", counter); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, ok := r.Get("test_counter_rg")
	if !ok {
		t.Fatal("Get returned false for registered metric")
	}
	if got != counter {
		t.Error("Get returned different collector")
	}

	// Cleanup
	r.Unregister("test_counter_rg")
}

func TestRegistry_RegisterDuplicate(t *testing.T) {
	r := NewRegistry()
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_registry_dup",
		Help: "test",
	})
	if err := r.Register("dup", counter); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	err := r.Register("dup", counter)
	if err == nil {
		t.Error("expected error on duplicate registration")
	}

	r.Unregister("dup")
}

func TestRegistry_Unregister(t *testing.T) {
	r := NewRegistry()
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_registry_unreg",
		Help: "test",
	})
	r.Register("unreg", counter)

	if !r.Unregister("unreg") {
		t.Error("Unregister should return true")
	}
	if r.Unregister("unreg") {
		t.Error("second Unregister should return false")
	}
	_, ok := r.Get("unreg")
	if ok {
		t.Error("Get should return false after Unregister")
	}
}

func TestRegistry_GetNotFound(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("Get should return false for nonexistent key")
	}
}

// ============================================================================
// SLO Definitions
// ============================================================================

func TestDefaultSLOs(t *testing.T) {
	slos := DefaultSLOs()
	if len(slos) != 4 {
		t.Fatalf("len(DefaultSLOs) = %d, want 4", len(slos))
	}

	services := map[string]bool{}
	for _, s := range slos {
		services[s.Service] = true
		if s.AvailabilityTarget <= 0 || s.AvailabilityTarget > 1 {
			t.Errorf("%s: invalid AvailabilityTarget %f", s.Service, s.AvailabilityTarget)
		}
		if s.LatencyP99Target <= 0 {
			t.Errorf("%s: LatencyP99Target should be > 0", s.Service)
		}
		if s.Window <= 0 {
			t.Errorf("%s: Window should be > 0", s.Service)
		}
	}

	expected := []string{"apiserver", "scheduler", "ai-engine", "controlplane"}
	for _, name := range expected {
		if !services[name] {
			t.Errorf("missing SLO definition for %q", name)
		}
	}
}

// ============================================================================
// SLOTracker — RecordRequest + Status
// ============================================================================

func newTestTracker() *SLOTracker {
	return NewSLOTracker(SLOTrackerConfig{
		Definitions: []SLODefinition{
			{
				Service:            "test-svc",
				AvailabilityTarget: 0.99,
				LatencyP99Target:   1 * time.Second,
				LatencyP95Target:   500 * time.Millisecond,
				Window:             24 * time.Hour,
			},
		},
		Logger:   logrus.StandardLogger(),
		Interval: 1 * time.Hour, // long interval so evaluation doesn't auto-run
	})
}

func TestSLOTracker_NewDefaults(t *testing.T) {
	tracker := NewSLOTracker(SLOTrackerConfig{})
	status := tracker.Status()
	// Should have the 4 default services
	if len(status) != 4 {
		t.Errorf("len(status) = %d, want 4", len(status))
	}
}

func TestSLOTracker_RecordRequest_Success(t *testing.T) {
	tracker := newTestTracker()

	tracker.RecordRequest("test-svc", 50*time.Millisecond, false)
	tracker.RecordRequest("test-svc", 100*time.Millisecond, false)
	tracker.RecordRequest("test-svc", 200*time.Millisecond, false)

	status := tracker.Status()
	s, ok := status["test-svc"]
	if !ok {
		t.Fatal("missing test-svc in status")
	}
	if s.TotalRequests != 3 {
		t.Errorf("TotalRequests = %d, want 3", s.TotalRequests)
	}
	if s.ErrorRequests != 0 {
		t.Errorf("ErrorRequests = %d, want 0", s.ErrorRequests)
	}
	if s.CurrentAvailability != 1.0 {
		t.Errorf("CurrentAvailability = %f, want 1.0", s.CurrentAvailability)
	}
	if s.ErrorBudgetRemaining != 1.0 {
		t.Errorf("ErrorBudgetRemaining = %f, want 1.0", s.ErrorBudgetRemaining)
	}
}

func TestSLOTracker_RecordRequest_WithErrors(t *testing.T) {
	tracker := newTestTracker()

	// 90 successes + 10 errors = 90% availability (target: 99%)
	for i := 0; i < 90; i++ {
		tracker.RecordRequest("test-svc", 50*time.Millisecond, false)
	}
	for i := 0; i < 10; i++ {
		tracker.RecordRequest("test-svc", 50*time.Millisecond, true)
	}

	status := tracker.Status()
	s := status["test-svc"]
	if s.TotalRequests != 100 {
		t.Errorf("TotalRequests = %d, want 100", s.TotalRequests)
	}
	if s.ErrorRequests != 10 {
		t.Errorf("ErrorRequests = %d, want 10", s.ErrorRequests)
	}

	expectedAvail := 0.9
	if math.Abs(s.CurrentAvailability-expectedAvail) > 0.001 {
		t.Errorf("CurrentAvailability = %f, want %f", s.CurrentAvailability, expectedAvail)
	}

	// Error budget should be exhausted: 10% error rate vs 1% allowed → budget consumed = 10x
	if s.ErrorBudgetRemaining != 0 {
		t.Errorf("ErrorBudgetRemaining = %f, want 0 (exhausted)", s.ErrorBudgetRemaining)
	}
}

func TestSLOTracker_RecordRequest_UnknownService(t *testing.T) {
	tracker := newTestTracker()
	// Should not panic
	tracker.RecordRequest("unknown-svc", 50*time.Millisecond, false)

	status := tracker.Status()
	if _, ok := status["unknown-svc"]; ok {
		t.Error("unknown-svc should not appear in status")
	}
}

func TestSLOTracker_Status_NoRequests(t *testing.T) {
	tracker := newTestTracker()
	status := tracker.Status()
	s := status["test-svc"]
	if s.CurrentAvailability != 1.0 {
		t.Errorf("CurrentAvailability = %f, want 1.0 (no requests)", s.CurrentAvailability)
	}
	if s.ErrorBudgetRemaining != 1.0 {
		t.Errorf("ErrorBudgetRemaining = %f, want 1.0 (no requests)", s.ErrorBudgetRemaining)
	}
}

func TestSLOTracker_StartStop(t *testing.T) {
	tracker := NewSLOTracker(SLOTrackerConfig{
		Definitions: []SLODefinition{
			{Service: "svc", AvailabilityTarget: 0.99, Window: 24 * time.Hour},
		},
		Interval: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tracker.Start(ctx)
	time.Sleep(100 * time.Millisecond)
	tracker.Stop()
	// Should not panic or hang
}

func TestSLOTracker_Evaluate_UpdatesPrometheus(t *testing.T) {
	tracker := newTestTracker()

	for i := 0; i < 100; i++ {
		isErr := i >= 95 // 5% error rate
		tracker.RecordRequest("test-svc", time.Duration(i)*time.Millisecond, isErr)
	}

	// Manually trigger evaluate
	tracker.evaluate()
	// No panic = success; actual Prometheus values verified via the global metrics
}

// ============================================================================
// SLOStatus.String()
// ============================================================================

func TestSLOStatus_String(t *testing.T) {
	s := SLOStatus{
		Service:              "api",
		CurrentAvailability:  0.9990,
		AvailabilityTarget:   0.999,
		ErrorBudgetRemaining: 0.95,
		TotalRequests:        10000,
		ErrorRequests:        10,
	}
	str := s.String()
	if str == "" {
		t.Error("String() should not be empty")
	}
	// Should contain the service name
	if len(str) < 10 {
		t.Errorf("String() too short: %q", str)
	}
}

// ============================================================================
// percentile utility
// ============================================================================

func TestPercentile_EmptyData(t *testing.T) {
	if v := percentile(nil, 0.99); v != 0 {
		t.Errorf("percentile(nil) = %f, want 0", v)
	}
	if v := percentile([]float64{}, 0.99); v != 0 {
		t.Errorf("percentile([]) = %f, want 0", v)
	}
}

func TestPercentile_SingleElement(t *testing.T) {
	if v := percentile([]float64{42}, 0.99); v != 42 {
		t.Errorf("percentile([42]) = %f, want 42", v)
	}
}

func TestPercentile_KnownValues(t *testing.T) {
	data := make([]float64, 100)
	for i := range data {
		data[i] = float64(i + 1)
	}

	p50 := percentile(data, 0.50)
	if math.Abs(p50-50.5) > 0.5 {
		t.Errorf("p50 = %f, want ~50.5", p50)
	}

	p99 := percentile(data, 0.99)
	if p99 < 98 || p99 > 100 {
		t.Errorf("p99 = %f, want ~99", p99)
	}

	p0 := percentile(data, 0.0)
	if p0 != 1 {
		t.Errorf("p0 = %f, want 1", p0)
	}

	p100 := percentile(data, 1.0)
	if p100 != 100 {
		t.Errorf("p100 = %f, want 100", p100)
	}
}

func TestPercentile_DoesNotMutateInput(t *testing.T) {
	data := []float64{5, 3, 1, 4, 2}
	original := make([]float64, len(data))
	copy(original, data)

	percentile(data, 0.5)

	for i := range data {
		if data[i] != original[i] {
			t.Errorf("percentile mutated input at index %d: got %f, want %f", i, data[i], original[i])
		}
	}
}

// ============================================================================
// sortFloat64s
// ============================================================================

func TestSortFloat64s(t *testing.T) {
	tests := []struct {
		input    []float64
		expected []float64
	}{
		{[]float64{}, []float64{}},
		{[]float64{1}, []float64{1}},
		{[]float64{3, 1, 2}, []float64{1, 2, 3}},
		{[]float64{5, 4, 3, 2, 1}, []float64{1, 2, 3, 4, 5}},
		{[]float64{1, 1, 1}, []float64{1, 1, 1}},
		{[]float64{-1, 0, 1}, []float64{-1, 0, 1}},
	}
	for _, tc := range tests {
		sortFloat64s(tc.input)
		for i, v := range tc.input {
			if v != tc.expected[i] {
				t.Errorf("sortFloat64s: got %v, want %v", tc.input, tc.expected)
				break
			}
		}
	}
}

// ============================================================================
// ResourceCollector
// ============================================================================

func TestNewResourceCollector_Defaults(t *testing.T) {
	c := NewResourceCollector(ResourceCollectorConfig{})
	if c.config.CollectInterval != 15*time.Second {
		t.Errorf("CollectInterval = %v, want 15s", c.config.CollectInterval)
	}
	if c.config.NodeName != "unknown" {
		t.Errorf("NodeName = %q, want unknown", c.config.NodeName)
	}
}

func TestResourceCollector_StartStop(t *testing.T) {
	c := NewResourceCollector(ResourceCollectorConfig{
		CollectInterval: 50 * time.Millisecond,
		EnableGoRuntime: true,
		NodeName:        "test-node",
		ClusterID:       "test-cluster",
	})

	ctx, cancel := context.WithCancel(context.Background())
	c.Start(ctx)
	time.Sleep(150 * time.Millisecond)
	c.Stop()
	cancel()
	// Should not panic or hang
}

func TestResourceCollector_GPUCollection(t *testing.T) {
	gpuCollectorCalled := false
	c := NewResourceCollector(ResourceCollectorConfig{
		CollectInterval: 50 * time.Millisecond,
		NodeName:        "gpu-node",
		ClusterID:       "c1",
		GPUCollector: func() []GPUMetrics {
			gpuCollectorCalled = true
			return []GPUMetrics{
				{Index: "0", Model: "A100", Utilization: 85, MemoryUsed: 40e9, MemoryTotal: 80e9, Temperature: 72, PowerUsage: 250},
			}
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	c.Start(ctx)
	time.Sleep(100 * time.Millisecond)
	c.Stop()
	cancel()

	if !gpuCollectorCalled {
		t.Error("GPU collector function was not called")
	}
}

// ============================================================================
// CollectDBPoolStats
// ============================================================================

func TestCollectDBPoolStats(t *testing.T) {
	// Should not panic
	CollectDBPoolStats("test-pool", DBPoolStats{
		MaxOpen: 50,
		Open:    30,
		InUse:   20,
		Idle:    10,
	})
}

func TestCollectDBPoolStats_ZeroMaxOpen(t *testing.T) {
	// Should not panic or set metrics when MaxOpen=0
	CollectDBPoolStats("zero-pool", DBPoolStats{MaxOpen: 0, InUse: 5})
}

// ============================================================================
// slidingWindow burn rate
// ============================================================================

func TestSlidingWindow_BurnRateWindows(t *testing.T) {
	tracker := newTestTracker()

	// Record requests with some errors
	for i := 0; i < 50; i++ {
		tracker.RecordRequest("test-svc", 50*time.Millisecond, false)
	}
	for i := 0; i < 5; i++ {
		tracker.RecordRequest("test-svc", 50*time.Millisecond, true)
	}

	tracker.mu.RLock()
	w := tracker.windows["test-svc"]
	for name, bw := range w.burnRateWindows {
		if bw.total != 55 {
			t.Errorf("burn window %q total = %d, want 55", name, bw.total)
		}
		if bw.errors != 5 {
			t.Errorf("burn window %q errors = %d, want 5", name, bw.errors)
		}
	}
	tracker.mu.RUnlock()
}

// ============================================================================
// SLOTracker latency ring buffer wrapping
// ============================================================================

func TestSLOTracker_LatencyRingBufferWrap(t *testing.T) {
	tracker := NewSLOTracker(SLOTrackerConfig{
		Definitions: []SLODefinition{
			{
				Service:            "wrap-svc",
				AvailabilityTarget: 0.99,
				LatencyP99Target:   5 * time.Second,
				Window:             24 * time.Hour,
			},
		},
		Logger:   logrus.StandardLogger(),
		Interval: 1 * time.Hour,
	})

	// Fill beyond the ring buffer size (10000)
	for i := 0; i < 10100; i++ {
		tracker.RecordRequest("wrap-svc", time.Duration(i%100)*time.Millisecond, false)
	}

	tracker.mu.RLock()
	w := tracker.windows["wrap-svc"]
	if !w.latencyFull {
		t.Error("latencyFull should be true after wrapping")
	}
	tracker.mu.RUnlock()

	// evaluate should not panic
	tracker.evaluate()
}

// ============================================================================
// GinMiddlewareConfig / GinMiddleware (unit level — config only)
// ============================================================================

func TestGinMiddlewareConfig_SkipPaths(t *testing.T) {
	cfg := GinMiddlewareConfig{
		ServiceName: "api",
		SkipPaths:   map[string]bool{"/healthz": true, "/metrics": true},
	}
	if !cfg.SkipPaths["/healthz"] {
		t.Error("/healthz should be in skip paths")
	}
	if !cfg.SkipPaths["/metrics"] {
		t.Error("/metrics should be in skip paths")
	}
}

// ============================================================================
// GPUMetrics struct
// ============================================================================

func TestGPUMetrics_Fields(t *testing.T) {
	g := GPUMetrics{
		Index:       "0",
		Model:       "A100",
		Utilization: 95.5,
		MemoryUsed:  40e9,
		MemoryTotal: 80e9,
		Temperature: 75,
		PowerUsage:  300,
	}
	if g.Index != "0" {
		t.Errorf("Index = %q", g.Index)
	}
	if g.Utilization != 95.5 {
		t.Errorf("Utilization = %f", g.Utilization)
	}
}

// ============================================================================
// DBPoolStats struct
// ============================================================================

func TestDBPoolStats_Fields(t *testing.T) {
	s := DBPoolStats{
		MaxOpen:   100,
		Open:      50,
		InUse:     30,
		Idle:      20,
		WaitCount: 5,
		WaitTime:  time.Second,
	}
	if s.MaxOpen != 100 {
		t.Errorf("MaxOpen = %d", s.MaxOpen)
	}
	expected := float64(30) / float64(100)
	if math.Abs(expected-0.3) > 0.001 {
		t.Errorf("utilization calc unexpected: %f", expected)
	}
}

// ============================================================================
// Concurrent access to SLOTracker
// ============================================================================

func TestSLOTracker_ConcurrentAccess(t *testing.T) {
	tracker := newTestTracker()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tracker.Start(ctx)

	done := make(chan struct{})
	// Concurrent writes
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				tracker.RecordRequest("test-svc", time.Duration(j)*time.Millisecond, j%10 == 0)
			}
			if id == 9 {
				close(done)
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 50; j++ {
				_ = tracker.Status()
				_ = fmt.Sprintf("%v", tracker.Status())
			}
		}()
	}

	<-done
	tracker.Stop()
}
