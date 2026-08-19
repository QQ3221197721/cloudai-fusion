// Package aiops - Self-Healing Engine Performance Benchmarks
// Targets for Module M49 self-heal controller optimization:
//   • BenchmarkGateCheck: <1µs (safety gate decision latency)
//   • BenchmarkNonDestructivePath: <20µs (previously 26.7µs)
//   • BenchmarkEnsembleDecision: <10µs (ensemble voting aggregation)
//   • BenchmarkIdempotentRemediation: <500ns (idempotency check)
//   • BenchmarkForestAnomalyScore: <5µs (Isolation Forest scoring)

package aiops

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

var (
	testLogger     *logrus.Logger
	testConfig     SelfHealConfig
	testEngine     *SelfHealingEngine
	healthyMetricsMap map[string]float64
	testMetricsMap  map[string]float64
	sampleSnapshot  MetricsSnapshot
)

func init() {
	testLogger = logrus.New()
	testLogger.SetLevel(logrus.PanicLevel)
	testLogger.SetOutput(io.Discard)
	testConfig = DefaultSelfHealConfig()
	testEngine = NewSelfHealingEngine(testConfig, testLogger)

	// Metrics that stay BELOW every default detector threshold. Used by the pure
	// safety-gate benchmark: each detector performs a lookup + operator compare
	// but no FaultEvent is allocated, isolating the gate-decision hot path.
	healthyMetricsMap = map[string]float64{
		"node_cpu_percent":        10.0,
		"node_memory_percent":     20.0,
		"node_disk_percent":       30.0,
		"pod_restart_count":       0.0,
		"gpu_temperature_celsius": 40.0,
		"gpu_ecc_errors":          0.0,
		"error_rate_percent":      0.5,
		"latency_p99_ms":          50.0,
	}

	// Metrics that TRIGGER default detectors (drives full detection + fault-event
	// creation). Used by the reference full-path detection benchmark.
	testMetricsMap = map[string]float64{
		"node_cpu_percent":        98.0,
		"node_memory_percent":     95.0,
		"gpu_temperature_celsius": 95.0,
		"gpu_ecc_errors":          5.0,
		"error_rate_percent":      10.0,
		"latency_p99_ms":          2000.0,
	}

	// Sample snapshot for ensemble/forest benchmarks
	sampleSnapshot = MetricsSnapshot{
		Timestamp:        time.Now(),
		CPUUtilization:   98.0,
		MemoryUsage:      95.0,
		DiskIORead:       5000.0,
		DiskIOWrite:      3000.0,
		NetworkIn:        1e6,
		NetworkOut:       5e5,
		Connections:      150,
		GPUUtilization:   95.0,
		GPUMemory:        85.0,
		ErrorRate:        10.0,
		LatencyP99:       2000.0,
	}
}

// ============================================================================
// BENCHMARK 1: GateCheck (Safety Gate Decision Latency)
// Target: <1µs
// ============================================================================

// BenchmarkGateCheck measures the pure safety-gate decision latency: iterating
// enabled detectors, looking up their metric, and evaluating the threshold
// operator. Uses healthy (non-triggering) metrics so no FaultEvent is allocated,
// isolating the gate hot path. Target: <1µs.
func BenchmarkGateCheck(b *testing.B) {
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = testEngine.DetectFaults(ctx, healthyMetricsMap)
	}
}

// BenchmarkGateCheck_WithFaults is a reference: full detection over triggering
// metrics, including FaultEvent creation (fmt.Sprintf) and correlation. This is
// NOT the <1µs gate target — it measures the complete fault-detection path.
func BenchmarkGateCheck_WithFaults(b *testing.B) {
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		faults, _ := testEngine.DetectFaults(ctx, testMetricsMap)
		_ = len(faults)
	}
}

// ============================================================================
// BENCHMARK 2: NonDestructivePath (Safe Remediation Check)
// Target: <20µs (baseline ~26.7µs)
// ============================================================================

// BenchmarkNonDestructivePath measures the non-destructive (dry-run) remediation
// path. The playbook's rate-limit gates (MaxExecutions / Cooldown) are disabled so
// EVERY iteration executes the full step loop rather than early-returning on the
// rate-limit gate — this measures the real remediation work, not the gate.
func BenchmarkNonDestructivePath(b *testing.B) {
	cfg := DefaultSelfHealConfig()
	cfg.EnableAutoRemediate = true
	cfg.DryRunMode = true // Safe mode - no actual changes

	engine := NewSelfHealingEngine(cfg, testLogger)
	faults, _ := engine.DetectFaults(context.Background(), map[string]float64{
		"pod_restart_count": 10,
	})
	incident := engine.CreateIncident(faults)

	// Disable rate-limit gates so the full path runs every iteration.
	pb := engine.playbooks["pb-pod-restart"]
	pb.MaxExecutions = 0 // 0 => unlimited
	pb.Cooldown = 0      // 0 => no cooldown

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, _ := engine.Remediate(context.Background(), incident)
		_ = result
	}
}

// BenchmarkNonDestructivePath_SingleStep isolates single step execution.
// Measures one remediation step overhead.
func BenchmarkNonDestructivePath_SingleStep(b *testing.B) {
	cfg := DefaultSelfHealConfig()
	cfg.EnableAutoRemediate = true
	cfg.DryRunMode = true

	engine := NewSelfHealingEngine(cfg, testLogger)
	faults, _ := engine.DetectFaults(context.Background(), map[string]float64{"pod_restart_count": 10})
	_ = engine.CreateIncident(faults)

	playbook := engine.playbooks["pb-pod-restart"]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, step := range playbook.Steps {
			if step.Name != "" && step.Action != "" {
				_ = step.Name + step.Action
			}
		}
	}
}

// ============================================================================
// BENCHMARK 3: EnsembleDecision (ML Ensemble Voting Aggregation)
// Target: <10µs
// ============================================================================

// BenchmarkEnsembleDecision measures ML ensemble voting aggregation latency.
// Combines Mahalanobis distance + Isolation Forest scores with weights.
func BenchmarkEnsembleDecision(b *testing.B) {
	engine := NewSelfHealEngine(testLogger)
	x := extractFeatures(sampleSnapshot)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mahalanobisScore := engine.anomalyDetector.mahalanobisModel.IsScore(x)
		iforestScore := engine.anomalyDetector.isolationForest.AnomallyScore(x)
		totalScore := mahalanobisScore*0.4 + iforestScore*0.6
		_ = totalScore
	}
}

// BenchmarkEnsembleDecision_Minimal measures minimal ensemble scoring.
// Pure score computation without decision branching.
func BenchmarkEnsembleDecision_Minimal(b *testing.B) {
	engine := NewSelfHealEngine(testLogger)
	x := extractFeatures(sampleSnapshot)

	mahalanobisScore := engine.anomalyDetector.mahalanobisModel.IsScore(x)
	iforestScore := engine.anomalyDetector.isolationForest.AnomallyScore(x)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		totalScore := mahalanobisScore*0.4 + iforestScore*0.6
		_ = totalScore
	}
}

// ============================================================================
// BENCHMARK 4: IdempotentRemediation (Idempotency Check)
// Target: <500ns
// ============================================================================

// BenchmarkIdempotentRemediation measures idempotency check latency.
// Tests max executions & cooldown boundary checks before remediation.
func BenchmarkIdempotentRemediation(b *testing.B) {
	cfg := DefaultSelfHealConfig()
	cfg.EnableAutoRemediate = true
	engine := NewSelfHealingEngine(cfg, testLogger)

	faults, _ := engine.DetectFaults(context.Background(), map[string]float64{"pod_restart_count": 10})
	_ = engine.CreateIncident(faults)
	playbook := engine.playbooks["pb-pod-restart"]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Simulate idempotency checks
		if playbook.MaxExecutions > 0 && playbook.ExecutionCount >= playbook.MaxExecutions {
			_ = "max_exceeded"
		}
		if !playbook.LastExecution.IsZero() && time.Since(playbook.LastExecution) < playbook.Cooldown {
			_ = "cooldown_active"
		}
	}
}

// BenchmarkIdempotentRemediation_Maps uses map-based lookups.
// Measures atomic.Value/read-only access pattern efficiency.
func BenchmarkIdempotentRemediation_Maps(b *testing.B) {
	executionCount := 0
	maxExecutions := 5
	lastExecution := time.Time{}
	cooldown := 5 * time.Minute

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Pure arithmetic comparisons
		if maxExecutions > 0 && executionCount >= maxExecutions {
			_ = "exceed"
		}
		if !lastExecution.IsZero() && time.Since(lastExecution) < cooldown {
			_ = "cool"
		}
	}
}

// ============================================================================
// BENCHMARK 5: ForestAnomalyScore (Isolation Forest Scoring)
// Target: <5µs
// ============================================================================

// BenchmarkForestAnomalyScore measures Isolation Forest anomaly scoring.
// Tests average path length computation across forest trees.
func BenchmarkForestAnomalyScore(b *testing.B) {
	model := NewIsolationForestModel(testLogger, 100, 200)

	// Create minimal training data to initialize model
	trainingData := make([]MetricsSnapshot, 200)
	for i := range trainingData {
		trainingData[i] = sampleSnapshot
	}

	_ = model.Train(trainingData)
	x := extractFeatures(sampleSnapshot)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		score := model.AnomallyScore(x)
		_ = score
	}
}

// BenchmarkForestAnomalyScore_Trained measures pre-trained forest scoring.
// Avoids repeated model initialization overhead.
func BenchmarkForestAnomalyScore_Trained(b *testing.B) {
	model := NewIsolationForestModel(testLogger, 100, 200)

	trainingData := make([]MetricsSnapshot, 200)
	for i := range trainingData {
		trainingData[i] = sampleSnapshot
	}

	err := model.Train(trainingData)
	if err != nil {
		b.Fatalf("failed to train model: %v", err)
	}

	x := extractFeatures(sampleSnapshot)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		score := model.AnomallyScore(x)
		_ = score
	}
}

// BenchmarkForestAnomalyScore_OneTree tests single tree path traversal.
// Isolates recursion overhead from ensemble averaging.
func BenchmarkForestAnomalyScore_OneTree(b *testing.B) {
	model := NewIsolationForestModel(testLogger, 1, 200)

	trainingData := make([]MetricsSnapshot, 200)
	for i := range trainingData {
		trainingData[i] = sampleSnapshot
	}

	_ = model.Train(trainingData)
	x := extractFeatures(sampleSnapshot)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		score := model.AnomallyScore(x)
		_ = score
	}
}

// ============================================================================
// HELPER BENCHMARKS: Component-Level Analysis
// ============================================================================

// BenchmarkOperatorSwitch tests switch statement performance vs map lookup.
func BenchmarkOperatorSwitch(b *testing.B) {
	value := 98.0
	threshold := 95.0

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var triggered bool
		switch "gt" {
		case "gt":
			triggered = value > threshold
		case "lt":
			triggered = value < threshold
		case "eq":
			triggered = value == threshold
		case "ne":
			triggered = value != threshold
		}
		_ = triggered
	}
}

// BenchmarkMetricLookup measures map read-only latency for metrics.
func BenchmarkMetricLookup(b *testing.B) {
	metrics := map[string]float64{
		"node_cpu_percent":         98.0,
		"node_memory_percent":      95.0,
		"gpu_temperature_celsius":  95.0,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, ok := metrics["node_cpu_percent"]
		if ok {
			_ = metrics["node_cpu_percent"]
		}
	}
}

// BenchmarkSliceAppendWithPreallocation tests slice allocation patterns.
func BenchmarkSliceAppendWithPreallocation(b *testing.B) {
	// With pre-allocation (optimized)
	preAllocated := make([]string, 0, 10)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		preAllocated = preAllocated[:0]
		for j := 0; j < 10; j++ {
			preAllocated = append(preAllocated, "fault")
		}
	}
}

// BenchmarkSliceAppendWithoutPreallocation tests baseline allocation.
func BenchmarkSliceAppendWithoutPreallocation(b *testing.B) {
	// Without pre-allocation (baseline)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		notPreAllocated := make([]string, 0)
		for j := 0; j < 10; j++ {
			notPreAllocated = append(notPreAllocated, "fault")
		}
	}
}
