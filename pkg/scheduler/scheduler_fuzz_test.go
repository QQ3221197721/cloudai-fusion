package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// Fuzz: Workload JSON round-trip
// ============================================================================

func FuzzWorkloadJSON(f *testing.F) {
	f.Add("wl-1", "training-job", "default", "training", 5, 4, "nvidia-a100")
	f.Add("", "", "", "", 0, 0, "")
	f.Add("wl-special", "test-name", "ns", "inference", -1, -1, "unknown")
	f.Add("wl-max", "x", "x", "batch", 2147483647, 2147483647, "nvidia-h100")

	f.Fuzz(func(t *testing.T, id, name, ns, wlType string, priority, gpuCount int, gpuType string) {
		wl := &Workload{
			ID:        id,
			Name:      name,
			Namespace: ns,
			Type:      common.WorkloadType(wlType),
			Priority:  priority,
			ResourceRequest: common.ResourceRequest{
				GPUCount: gpuCount,
				GPUType:  common.GPUType(gpuType),
			},
			QueuedAt: common.NowUTC(),
		}

		// Marshal should not panic
		data, err := json.Marshal(wl)
		if err != nil {
			// Invalid UTF-8 can cause Marshal errors — acceptable
			t.Skipf("Marshal: %v", err)
		}

		var decoded Workload
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}

		if decoded.Priority != wl.Priority {
			t.Errorf("Priority mismatch: %d vs %d", decoded.Priority, wl.Priority)
		}
	})
}

// ============================================================================
// Fuzz: SubmitWorkload (should never panic on any input)
// ============================================================================

func FuzzSubmitWorkload(f *testing.F) {
	f.Add("wl-1", "job", "training", 5, 4)
	f.Add("", "", "", 0, 0)
	f.Add("x", "x", "inference", -1, -1)
	f.Add("\x00", "\xff\xfe", "batch", 2147483647, 2147483647)

	f.Fuzz(func(t *testing.T, id, name, wlType string, priority, gpuCount int) {
		engine, err := NewEngine(EngineConfig{
			SchedulingInterval: time.Hour,
		})
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}

		wl := &Workload{
			ID:       id,
			Name:     name,
			Type:     common.WorkloadType(wlType),
			Priority: priority,
			ResourceRequest: common.ResourceRequest{
				GPUCount: gpuCount,
			},
		}

		// Should never panic
		engine.SubmitWorkload(wl)

		stats := engine.GetStats()
		if stats == nil {
			t.Error("GetStats returned nil")
		}
		qLen, ok := stats["queue_length"].(int)
		if !ok || qLen < 1 {
			t.Errorf("expected queue_length >= 1, got %v", stats["queue_length"])
		}
	})
}

// ============================================================================
// Fuzz: GPU Sharing - InitGPUMemoryState (boundary conditions)
// ============================================================================

func FuzzGPUSharingInitMemory(f *testing.F) {
	f.Add(0, 81920)
	f.Add(7, 40960)
	f.Add(-1, 0)
	f.Add(9999, -1)
	f.Add(0, 2147483647)

	f.Fuzz(func(t *testing.T, gpuIdx int, totalMB int) {
		mgr := NewGPUSharingManager(GPUSharingConfig{})
		// Should never panic
		mgr.InitGPUMemoryState(gpuIdx, totalMB)
		// GetGPUSharingStates should still work
		mgr.GetGPUSharingStates()
	})
}

// ============================================================================
// Fuzz: GPU Sharing - AllocateMemoryIsolationGroup
// ============================================================================

func FuzzGPUSharingAllocate(f *testing.F) {
	f.Add(0, "wl-123456", 4096, 5)
	f.Add(0, "12345678", 0, 0)
	f.Add(-1, "xxxxxxxx", -1, -1)
	f.Add(7, "big12345", 999999, 100)

	f.Fuzz(func(t *testing.T, gpuIdx int, workloadID string, requestMB int, priority int) {
		if len(workloadID) < 8 {
			t.Skip("workloadID too short for AllocateMemoryIsolationGroup")
		}
		mgr := NewGPUSharingManager(GPUSharingConfig{})
		mgr.InitGPUMemoryState(0, 81920)
		policy := DefaultMemoryOversellPolicy()

		// Should never panic
		mgr.AllocateMemoryIsolationGroup(gpuIdx, workloadID, requestMB, priority, policy)
	})
}

// ============================================================================
// Fuzz: Predictive Scaling - RecordDataPoint (should never panic)
// ============================================================================

func FuzzPredictiveScalingRecord(f *testing.F) {
	f.Add(50.0, 10, 5)
	f.Add(0.0, 0, 0)
	f.Add(-1.0, -1, -1)
	f.Add(100.0, 999999, 999999)

	f.Fuzz(func(t *testing.T, gpuUtil float64, queueDepth, activeWorkloads int) {
		if math.IsNaN(gpuUtil) || math.IsInf(gpuUtil, 0) {
			t.Skip("skip NaN/Inf")
		}

		ps := NewPredictiveScaler(DefaultPredictiveScalingConfig())
		// Should never panic
		ps.RecordDataPoint(LoadDataPoint{
			Timestamp:       time.Now(),
			GPUUtilization:  gpuUtil,
			QueueDepth:      queueDepth,
			ActiveWorkloads: activeWorkloads,
		})
		// Forecast on single data point should not panic
		ps.GenerateForecast()
	})
}

// ============================================================================
// Fuzz: Cost Optimizer - SelectOptimalPricing (boundary inputs)
// ============================================================================

func FuzzCostOptimizerPricing(f *testing.F) {
	f.Add("training", "nvidia-a100", 4, 8.0, 5, true)
	f.Add("", "", 0, 0.0, 0, false)
	f.Add("inference", "amd-mi300x", -1, -1.0, -1, true)
	f.Add("batch", "nvidia-h200", 2147483647, 99999.99, 2147483647, false)

	f.Fuzz(func(t *testing.T, workloadType, gpuType string, gpuCount int, maxCost float64, priority int, spotOK bool) {
		if math.IsNaN(maxCost) || math.IsInf(maxCost, 0) {
			t.Skip("skip NaN/Inf")
		}

		co := NewCostOptimizer(DefaultCostOptimizerConfig())
		// Should never panic
		co.SelectOptimalPricing(workloadType, gpuType, gpuCount, maxCost, priority, spotOK)
	})
}

// ============================================================================
// Fuzz: Queue Manager - Enqueue/Dequeue consistency
// ============================================================================

func FuzzQueueManagerEnqueueDequeue(f *testing.F) {
	f.Add("wl-1", "default", int64(500), 5)
	f.Add("", "", int64(0), 0)
	f.Add("x", "high-priority", int64(9999), -1)
	f.Add("\x00", "queue\xff", int64(-1), 999)

	f.Fuzz(func(t *testing.T, wlID, queue string, priority int64, gpuCount int) {
		qm := NewQueueManager(DefaultQueueManagerConfig(), 64, 384000, 3*1024*1024*1024*1024)

		wl := &Workload{
			ID:       wlID,
			Name:     "fuzz-workload",
			Type:     common.WorkloadTypeInference,
			Priority: int(priority % 100),
			ResourceRequest: common.ResourceRequest{
				GPUCount: gpuCount % 16,
			},
		}

		// Should never panic
		qm.Enqueue(wl, queue, PriorityLevel(priority))

		// Dequeue should not panic (may return nil if queue logic rejects)
		qm.Dequeue()
	})
}

// ============================================================================
// Fuzz: Federation - SelectCluster with fuzzed workload
// ============================================================================

func FuzzFederationSelectCluster(f *testing.F) {
	f.Add("wl-1", "training", 4, "nvidia-a100")
	f.Add("", "", 0, "")
	f.Add("x", "inference", -1, "invalid-gpu")

	f.Fuzz(func(t *testing.T, wlID, wlType string, gpuCount int, gpuType string) {
		fs := NewFederationScheduler(DefaultFederationConfig())
		fs.RegisterCluster(&FederatedCluster{
			Name:     "test-cluster",
			Provider: "aws",
			Region:   "us-east-1",
			Capacity: FederatedCapacity{
				TotalGPUs:     64,
				AvailableGPUs: 32,
				GPUTypes:      map[string]int{"nvidia-a100": 32},
			},
			CostMultiplier: 1.0,
		})

		wl := &Workload{
			ID:   wlID,
			Type: common.WorkloadType(wlType),
			ResourceRequest: common.ResourceRequest{
				GPUCount: gpuCount,
				GPUType:  common.GPUType(gpuType),
			},
		}

		// Should never panic
		fs.SelectCluster(context.Background(), wl, nil)
	})
}

// ============================================================================
// Fuzz: Scheduling Cycle (should never panic on any queue state)
// ============================================================================

func FuzzSchedulingCycle(f *testing.F) {
	f.Add(1, "training", 5, 2)
	f.Add(10, "inference", 0, 0)
	f.Add(0, "batch", -1, -1)

	f.Fuzz(func(t *testing.T, numWorkloads int, wlType string, priority, gpuCount int) {
		if numWorkloads < 0 {
			numWorkloads = 0
		}
		if numWorkloads > 50 {
			numWorkloads = 50
		}

		engine, err := NewEngine(EngineConfig{})
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}

		for i := 0; i < numWorkloads; i++ {
			engine.SubmitWorkload(&Workload{
				ID:       fmt.Sprintf("fuzz-%d", i),
				Name:     "fuzz-wl",
				Type:     common.WorkloadType(wlType),
				Status:   common.WorkloadStatusQueued,
				Priority: priority,
				ResourceRequest: common.ResourceRequest{
					GPUCount: gpuCount % 16,
				},
				QueuedAt: common.NowUTC(),
			})
		}

		// Should never panic
		engine.RunSchedulingCycle(context.Background())
	})
}
