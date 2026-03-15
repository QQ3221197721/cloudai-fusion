package scheduler

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// Benchmark: GPU Scheduling Pipeline (end-to-end)
// ============================================================================

func BenchmarkGPUSchedulingPipeline(b *testing.B) {
	engine, _ := NewEngine(EngineConfig{
		SchedulingInterval: time.Second,
	})

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		wl := &Workload{
			ID:       fmt.Sprintf("bench-%d", i),
			Name:     "gpu-bench-job",
			Type:     common.WorkloadTypeTraining,
			Status:   common.WorkloadStatusQueued,
			Priority: 5,
			ResourceRequest: common.ResourceRequest{
				GPUCount: 4,
				GPUType:  common.GPUTypeNvidiaA100,
			},
			QueuedAt: common.NowUTC(),
		}
		engine.SubmitWorkload(wl)
		engine.schedulingCycle(context.Background())
	}
}

// ============================================================================
// Benchmark: GPU Sharing Manager (MIG/MPS operations)
// ============================================================================

func BenchmarkGPUSharingGetStates(b *testing.B) {
	mgr := NewGPUSharingManager(GPUSharingConfig{})

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		mgr.GetGPUSharingStates()
	}
}

func BenchmarkGPUSharingMemoryInit(b *testing.B) {
	mgr := NewGPUSharingManager(GPUSharingConfig{})

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		mgr.InitGPUMemoryState(i%8, 81920)
	}
}

func BenchmarkGPUSharingMemoryAllocate(b *testing.B) {
	mgr := NewGPUSharingManager(GPUSharingConfig{})
	// Init 8 GPUs
	for g := 0; g < 8; g++ {
		mgr.InitGPUMemoryState(g, 81920) // 80 GiB A100
	}
	policy := DefaultMemoryOversellPolicy()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		mgr.AllocateMemoryIsolationGroup(i%8, fmt.Sprintf("wl-%d", i), 4096, i%10, policy)
	}
}

// ============================================================================
// Benchmark: Elastic Inference Manager
// ============================================================================

func BenchmarkElasticInferenceCreateEndpoint(b *testing.B) {
	eim := NewElasticInferenceManager(DefaultElasticInferenceConfig())

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		eim.CreateEndpoint(
			fmt.Sprintf("ep-%d", i),
			"llama2-70b",
			"v1",
			"nvidia-a100",
			2,
		)
	}
}

func BenchmarkElasticInferenceEvaluateScaling(b *testing.B) {
	eim := NewElasticInferenceManager(DefaultElasticInferenceConfig())
	// Create endpoint with metrics
	ep, _ := eim.CreateEndpoint("bench-ep", "gpt-3.5", "v1", "nvidia-a100", 2)
	eim.UpdateEndpointMetrics(ep.ID, &InferenceMetrics{
		RequestsPerSec: 500,
		AvgLatencyMs:   20,
		P99LatencyMs:   100,
		GPUUtilization: 70,
		QueueDepth:     5,
		CurrentMode:    InferenceModeAdaptive,
	})

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		eim.EvaluateScaling(context.Background())
	}
}

// ============================================================================
// Benchmark: Predictive Scaling
// ============================================================================

func BenchmarkPredictiveScalingRecordAndForecast(b *testing.B) {
	ps := NewPredictiveScaler(DefaultPredictiveScalingConfig())

	// Seed history
	for i := 0; i < 100; i++ {
		ps.RecordDataPoint(LoadDataPoint{
			Timestamp:       time.Now().Add(-time.Duration(100-i) * time.Minute),
			GPUUtilization:  40 + float64(i%40),
			QueueDepth:      i % 20,
			ActiveWorkloads: 10 + i%10,
		})
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ps.GenerateForecast()
	}
}

func BenchmarkPredictiveScalingRecommendation(b *testing.B) {
	ps := NewPredictiveScaler(DefaultPredictiveScalingConfig())
	for i := 0; i < 100; i++ {
		ps.RecordDataPoint(LoadDataPoint{
			Timestamp:       time.Now().Add(-time.Duration(100-i) * time.Minute),
			GPUUtilization:  40 + float64(i%40),
			QueueDepth:      i % 20,
			ActiveWorkloads: 10 + i%10,
		})
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ps.GetScalingRecommendation(16, 64)
	}
}

// ============================================================================
// Benchmark: Cost Optimizer
// ============================================================================

func BenchmarkCostOptimizerSelectPricing(b *testing.B) {
	co := NewCostOptimizer(DefaultCostOptimizerConfig())

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		co.SelectOptimalPricing("training", "nvidia-a100", 4, 8.0, 5, true)
	}
}

func BenchmarkCostOptimizerGenerateReport(b *testing.B) {
	co := NewCostOptimizer(DefaultCostOptimizerConfig())

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		co.GenerateCostReport()
	}
}

// ============================================================================
// Benchmark: Federation Scheduler
// ============================================================================

func BenchmarkFederationSelectCluster(b *testing.B) {
	fs := NewFederationScheduler(DefaultFederationConfig())
	// Register test clusters
	for i := 0; i < 5; i++ {
		fs.RegisterCluster(&FederatedCluster{
			Name:     fmt.Sprintf("cluster-%d", i),
			Provider: "aws",
			Region:   "us-east-1",
			Capacity: FederatedCapacity{
				TotalGPUs:     64,
				AvailableGPUs: 32 - i*5,
				GPUTypes:      map[string]int{"nvidia-a100": 32},
			},
			CostMultiplier: 1.0 + float64(i)*0.1,
		})
	}
	wl := &Workload{
		ID:   "bench-wl",
		Name: "bench-workload",
		Type: common.WorkloadTypeTraining,
		ResourceRequest: common.ResourceRequest{GPUCount: 4},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		fs.SelectCluster(context.Background(), wl, nil)
	}
}

// ============================================================================
// Benchmark: Queue Manager
// ============================================================================

func BenchmarkQueueManagerEnqueue(b *testing.B) {
	qm := NewQueueManager(DefaultQueueManagerConfig(), 64, 384000, 3*1024*1024*1024*1024)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		wl := &Workload{
			ID:       fmt.Sprintf("wl-%d", i),
			Name:     fmt.Sprintf("bench-wl-%d", i),
			Type:     common.WorkloadTypeInference,
			Priority: i % 10,
			ResourceRequest: common.ResourceRequest{GPUCount: 1},
		}
		qm.Enqueue(wl, "default", PriorityLevel(i%10)*100)
	}
}

func BenchmarkQueueManagerDequeue(b *testing.B) {
	qm := NewQueueManager(DefaultQueueManagerConfig(), 64, 384000, 3*1024*1024*1024*1024)
	for i := 0; i < 1000; i++ {
		wl := &Workload{
			ID:       fmt.Sprintf("wl-%d", i),
			Name:     fmt.Sprintf("bench-wl-%d", i),
			Type:     common.WorkloadTypeInference,
			Priority: i % 10,
			ResourceRequest: common.ResourceRequest{GPUCount: 1},
		}
		qm.Enqueue(wl, "default", PriorityLevel(i%10)*100)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		qm.Dequeue()
	}
}

// ============================================================================
// Benchmark: Workload Submission Throughput
// ============================================================================

func BenchmarkWorkloadSubmission(b *testing.B) {
	engine, _ := NewEngine(EngineConfig{
		SchedulingInterval: time.Hour, // don't auto-schedule during bench
	})

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		wl := &Workload{
			ID:       fmt.Sprintf("wl-%d", i),
			Name:     "bench-workload",
			Type:     common.WorkloadTypeInference,
			Status:   common.WorkloadStatusQueued,
			Priority: i % 10,
			ResourceRequest: common.ResourceRequest{
				GPUCount: 1,
			},
			QueuedAt: common.NowUTC(),
		}
		engine.SubmitWorkload(wl)
	}
}

// ============================================================================
// Benchmark: Scheduling Decision (single cycle)
// ============================================================================

func BenchmarkSchedulingCycleSingle(b *testing.B) {
	engine, _ := NewEngine(EngineConfig{})

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		wl := &Workload{
			ID:       fmt.Sprintf("wl-%d", i),
			Name:     "cycle-bench",
			Type:     common.WorkloadTypeTraining,
			Status:   common.WorkloadStatusQueued,
			Priority: 5,
			ResourceRequest: common.ResourceRequest{GPUCount: 2},
			QueuedAt: common.NowUTC(),
		}
		engine.SubmitWorkload(wl)
		engine.schedulingCycle(context.Background())
	}
}
