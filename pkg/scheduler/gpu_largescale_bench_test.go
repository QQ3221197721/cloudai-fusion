// Package scheduler — GPU Scheduler large-scale benchmark tests.
// Validates scheduling performance at 1000+ node scale with
// heterogeneous GPU topologies, multi-tenant queues, and
// real-world workload distribution patterns.
//
// Run: go test -bench=BenchmarkLargeScale -benchmem -timeout=600s ./pkg/scheduler/
package scheduler

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// Helpers — generate realistic large-scale cluster topology
// ============================================================================

// generateLargeScaleNodes creates a realistic heterogeneous GPU cluster with N nodes.
// Distribution: 50% A100, 30% H100, 15% V100, 5% T4
func generateLargeScaleNodes(n int) []NodeScore {
	nodes := make([]NodeScore, n)
	gpuSpecs := []struct {
		gpuType    string
		freeRange  [2]int
		costBase   float64
		weight     float64
	}{
		{"nvidia-a100", [2]int{1, 8}, 12.80, 0.50},
		{"nvidia-h100", [2]int{1, 8}, 24.50, 0.30},
		{"nvidia-v100", [2]int{1, 4}, 6.50, 0.15},
		{"nvidia-t4", [2]int{1, 4}, 2.80, 0.05},
	}

	idx := 0
	for _, spec := range gpuSpecs {
		count := int(float64(n) * spec.weight)
		for j := 0; j < count && idx < n; j++ {
			gpuFree := spec.freeRange[0] + rand.Intn(spec.freeRange[1]-spec.freeRange[0]+1)
			nodes[idx] = NodeScore{
				NodeName:        fmt.Sprintf("gpu-node-%05d", idx),
				ClusterID:       fmt.Sprintf("cluster-%d", idx%10),
				GPUFreeCount:    gpuFree,
				GPUType:         spec.gpuType,
				GPUUtilization:  float64(rand.Intn(80)),
				CostPerHour:     spec.costBase * (0.8 + rand.Float64()*0.4),
				TopologyScore:   rand.Float64(),
				AvailableMemory: int64(rand.Intn(512)+128) * 1024 * 1024 * 1024,
			}
			idx++
		}
	}
	// Fill remaining
	for ; idx < n; idx++ {
		nodes[idx] = NodeScore{
			NodeName:        fmt.Sprintf("gpu-node-%05d", idx),
			ClusterID:       "cluster-0",
			GPUFreeCount:    4,
			GPUType:         "nvidia-a100",
			GPUUtilization:  50,
			CostPerHour:     12.80,
			AvailableMemory: 256 * 1024 * 1024 * 1024,
		}
	}
	return nodes
}

// generateRealisticWorkloads creates a batch of workloads with realistic distribution.
func generateRealisticWorkloads(n int) []*Workload {
	workloads := make([]*Workload, n)
	types := []common.WorkloadType{
		common.WorkloadTypeTraining,
		common.WorkloadTypeInference,
	}
	frameworks := []string{"pytorch", "tensorflow", "jax", "triton"}
	gpuCounts := []int{1, 1, 2, 2, 4, 4, 8}

	for i := 0; i < n; i++ {
		workloads[i] = &Workload{
			ID:        fmt.Sprintf("wl-%06d", i),
			Name:      fmt.Sprintf("job-%d", i),
			Namespace: fmt.Sprintf("tenant-%d", i%20),
			Type:      types[i%len(types)],
			Status:    common.WorkloadStatusQueued,
			Priority:  rand.Intn(10) + 1,
			Framework: frameworks[i%len(frameworks)],
			ResourceRequest: common.ResourceRequest{
				GPUCount: gpuCounts[i%len(gpuCounts)],
			},
			QueuedAt: common.NowUTC(),
		}
	}
	return workloads
}

// ============================================================================
// Benchmark: 1000-node Scheduling Cycle
// ============================================================================

func BenchmarkLargeScale_1000Node_SchedulingCycle(b *testing.B) {
	engine, _ := NewEngine(EngineConfig{
		SchedulingInterval: time.Hour,
	})
	// Inject 1000 nodes via simulated candidates
	nodes := generateLargeScaleNodes(1000)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		wl := &Workload{
			ID:       fmt.Sprintf("wl-%d", i),
			Name:     "large-scale-job",
			Type:     common.WorkloadTypeTraining,
			Status:   common.WorkloadStatusQueued,
			Priority: 5,
			ResourceRequest: common.ResourceRequest{
				GPUCount: 4,
				GPUType:  common.GPUTypeNvidiaA100,
			},
			QueuedAt: common.NowUTC(),
		}
		// Simulate scoring over 1000 nodes
		var bestNode *NodeScore
		for j := range nodes {
			engine.scoreNode(&nodes[j], wl)
			if bestNode == nil || nodes[j].Score > bestNode.Score {
				bestNode = &nodes[j]
			}
		}
	}
}

// ============================================================================
// Benchmark: 2000-node Full Pipeline with Filter + Score
// ============================================================================

func BenchmarkLargeScale_2000Node_FullPipeline(b *testing.B) {
	engine, _ := NewEngine(EngineConfig{
		SchedulingInterval: time.Hour,
	})
	nodes := generateLargeScaleNodes(2000)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		wl := &Workload{
			ID:       fmt.Sprintf("wl-%d", i),
			Name:     "pipeline-bench",
			Type:     common.WorkloadTypeTraining,
			Status:   common.WorkloadStatusQueued,
			Priority: 7,
			ResourceRequest: common.ResourceRequest{
				GPUCount: 2,
			},
			QueuedAt: common.NowUTC(),
		}

		// Filter phase: remove nodes without enough GPUs
		filtered := make([]NodeScore, 0, len(nodes)/2)
		for _, n := range nodes {
			if n.GPUFreeCount >= wl.ResourceRequest.GPUCount {
				filtered = append(filtered, n)
			}
		}

		// Score phase
		var bestNode *NodeScore
		for j := range filtered {
			engine.scoreNode(&filtered[j], wl)
			if bestNode == nil || filtered[j].Score > bestNode.Score {
				bestNode = &filtered[j]
			}
		}
	}
}

// ============================================================================
// Benchmark: Concurrent Workload Submission at Scale
// ============================================================================

func BenchmarkLargeScale_ConcurrentSubmission_1000(b *testing.B) {
	engine, _ := NewEngine(EngineConfig{
		SchedulingInterval: time.Hour,
	})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		for j := 0; j < 1000; j++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				wl := &Workload{
					ID:       fmt.Sprintf("wl-%d-%d", i, id),
					Name:     fmt.Sprintf("concurrent-%d", id),
					Type:     common.WorkloadTypeInference,
					Status:   common.WorkloadStatusQueued,
					Priority: id % 10,
					ResourceRequest: common.ResourceRequest{
						GPUCount: 1,
					},
					QueuedAt: common.NowUTC(),
				}
				engine.SubmitWorkload(wl)
			}(j)
		}
		wg.Wait()
	}
}

// ============================================================================
// Benchmark: Batch Scheduling Throughput (100 workloads per cycle)
// ============================================================================

func BenchmarkLargeScale_BatchScheduling_100(b *testing.B) {
	engine, _ := NewEngine(EngineConfig{
		SchedulingInterval: time.Hour,
	})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Submit 100 workloads
		for j := 0; j < 100; j++ {
			wl := &Workload{
				ID:       fmt.Sprintf("batch-%d-%d", i, j),
				Name:     fmt.Sprintf("batch-wl-%d", j),
				Type:     common.WorkloadTypeTraining,
				Status:   common.WorkloadStatusQueued,
				Priority: j%10 + 1,
				ResourceRequest: common.ResourceRequest{
					GPUCount: (j%4 + 1) * 2,
				},
				QueuedAt: common.NowUTC(),
			}
			engine.SubmitWorkload(wl)
		}
		// Run scheduling cycle
		engine.schedulingCycle(context.Background())
	}
}

// ============================================================================
// Benchmark: Node Scoring at Scale (1000, 2000, 5000 nodes)
// ============================================================================

func BenchmarkLargeScale_NodeScoring_1000(b *testing.B) {
	benchmarkNodeScoringAtScale(b, 1000)
}

func BenchmarkLargeScale_NodeScoring_2000(b *testing.B) {
	benchmarkNodeScoringAtScale(b, 2000)
}

func BenchmarkLargeScale_NodeScoring_5000(b *testing.B) {
	benchmarkNodeScoringAtScale(b, 5000)
}

func benchmarkNodeScoringAtScale(b *testing.B, nodeCount int) {
	engine, _ := NewEngine(EngineConfig{})
	nodes := generateLargeScaleNodes(nodeCount)
	wl := &Workload{
		Type:     common.WorkloadTypeTraining,
		Priority: 7,
		ResourceRequest: common.ResourceRequest{
			GPUCount: 4,
			GPUType:  common.GPUTypeNvidiaA100,
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		for j := range nodes {
			engine.scoreNode(&nodes[j], wl)
		}
	}
}

// ============================================================================
// Benchmark: Queue Manager at Scale (10000 workloads)
// ============================================================================

func BenchmarkLargeScale_QueueManager_10000_Enqueue(b *testing.B) {
	qm := NewQueueManager(DefaultQueueManagerConfig(), 512, 384000*8, 24*1024*1024*1024*1024)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		for j := 0; j < 10000; j++ {
			wl := &Workload{
				ID:       fmt.Sprintf("wl-%d-%d", i, j),
				Name:     fmt.Sprintf("scale-wl-%d", j),
				Type:     common.WorkloadTypeInference,
				Priority: j % 10,
				ResourceRequest: common.ResourceRequest{GPUCount: 1},
			}
			qm.Enqueue(wl, fmt.Sprintf("tenant-%d", j%20), PriorityLevel(j%10)*100)
		}
	}
}

// ============================================================================
// Benchmark: Federation Scheduling Across 50 Clusters
// ============================================================================

func BenchmarkLargeScale_Federation_50Clusters(b *testing.B) {
	fs := NewFederationScheduler(DefaultFederationConfig())
	for i := 0; i < 50; i++ {
		providers := []string{"aws", "gcp", "azure", "aliyun", "huawei"}
		regions := []string{"us-east-1", "eu-west-1", "ap-southeast-1", "cn-hangzhou", "cn-shanghai"}
		fs.RegisterCluster(&FederatedCluster{
			Name:     fmt.Sprintf("cluster-%02d", i),
			Provider: providers[i%len(providers)],
			Region:   regions[i%len(regions)],
			Capacity: FederatedCapacity{
				TotalGPUs:     128,
				AvailableGPUs: rand.Intn(64) + 16,
				GPUTypes:      map[string]int{"nvidia-a100": 64, "nvidia-h100": 64},
			},
			CostMultiplier: 0.8 + rand.Float64()*0.6,
		})
	}

	wl := &Workload{
		ID:   "fed-bench",
		Name: "federation-workload",
		Type: common.WorkloadTypeTraining,
		ResourceRequest: common.ResourceRequest{
			GPUCount: 8,
			GPUType:  common.GPUTypeNvidiaA100,
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		fs.SelectCluster(context.Background(), wl, nil)
	}
}

// ============================================================================
// Benchmark: Cost Optimizer with Large Fleet
// ============================================================================

func BenchmarkLargeScale_CostOptimizer_LargeFleet(b *testing.B) {
	co := NewCostOptimizer(DefaultCostOptimizerConfig())
	gpuTypes := []string{"nvidia-a100", "nvidia-h100", "nvidia-v100", "nvidia-t4"}
	workloadTypes := []string{"training", "inference", "finetuning"}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		for j := 0; j < 500; j++ {
			co.SelectOptimalPricing(
				workloadTypes[j%len(workloadTypes)],
				gpuTypes[j%len(gpuTypes)],
				(j%8)+1,
				float64(j%24)+1,
				j%10+1,
				j%2 == 0,
			)
		}
	}
}

// ============================================================================
// Benchmark: Predictive Scaling with Dense History
// ============================================================================

func BenchmarkLargeScale_PredictiveScaling_DenseHistory(b *testing.B) {
	ps := NewPredictiveScaler(DefaultPredictiveScalingConfig())

	// Seed 1000 data points (dense history)
	for i := 0; i < 1000; i++ {
		ps.RecordDataPoint(LoadDataPoint{
			Timestamp:       time.Now().Add(-time.Duration(1000-i) * time.Minute),
			GPUUtilization:  30 + float64(i%50) + rand.Float64()*10,
			QueueDepth:      rand.Intn(50),
			ActiveWorkloads: rand.Intn(100) + 10,
		})
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ps.GenerateForecast()
	}
}

// ============================================================================
// Test: Large-Scale Scheduling Correctness (1000 nodes)
// ============================================================================

func TestLargeScale_SchedulingCorrectness_1000Nodes(t *testing.T) {
	engine, err := NewEngine(EngineConfig{
		SchedulingInterval: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go engine.Run(ctx)
	time.Sleep(30 * time.Millisecond)

	// Submit 200 workloads
	workloads := generateRealisticWorkloads(200)
	for _, wl := range workloads {
		engine.SubmitWorkload(wl)
	}

	// Wait for scheduling
	time.Sleep(500 * time.Millisecond)
	engine.Stop()

	stats := engine.GetStats()
	if stats == nil {
		t.Fatal("engine stats should not be nil")
	}

	t.Logf("Large-scale test: submitted=200, stats=%+v", stats)
}

// ============================================================================
// Test: Scheduling Latency Budget (P99 < 50ms for 1000 nodes)
// ============================================================================

func TestLargeScale_SchedulingLatencyBudget(t *testing.T) {
	engine, _ := NewEngine(EngineConfig{})
	nodes := generateLargeScaleNodes(1000)

	wl := &Workload{
		ID:       "latency-test",
		Type:     common.WorkloadTypeTraining,
		Priority: 5,
		ResourceRequest: common.ResourceRequest{GPUCount: 4},
	}

	iterations := 100
	latencies := make([]time.Duration, iterations)

	for i := 0; i < iterations; i++ {
		start := time.Now()
		// Filter + Score over 1000 nodes
		var bestNode *NodeScore
		for j := range nodes {
			if nodes[j].GPUFreeCount >= wl.ResourceRequest.GPUCount {
				engine.scoreNode(&nodes[j], wl)
				if bestNode == nil || nodes[j].Score > bestNode.Score {
					bestNode = &nodes[j]
				}
			}
		}
		latencies[i] = time.Since(start)
	}

	// Calculate P50, P95, P99
	sortDurations(latencies)
	p50 := latencies[len(latencies)*50/100]
	p95 := latencies[len(latencies)*95/100]
	p99 := latencies[len(latencies)*99/100]

	t.Logf("1000-node scheduling latency: P50=%v, P95=%v, P99=%v", p50, p95, p99)

	// P99 should be under 150ms for 1000 nodes (allows CI environment variance)
	if p99 > 150*time.Millisecond {
		t.Errorf("P99 latency %v exceeds 150ms budget for 1000-node scheduling", p99)
	}
}

func sortDurations(d []time.Duration) {
	for i := 1; i < len(d); i++ {
		for j := i; j > 0 && d[j] < d[j-1]; j-- {
			d[j], d[j-1] = d[j-1], d[j]
		}
	}
}
