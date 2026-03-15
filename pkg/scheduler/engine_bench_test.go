package scheduler

import (
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// Benchmark: Node Scoring (hot path for scheduling)
// ============================================================================

func BenchmarkScoreNode(b *testing.B) {
	engine, _ := NewEngine(EngineConfig{})
	workload := &Workload{
		Type: common.WorkloadTypeTraining,
		ResourceRequest: common.ResourceRequest{
			GPUCount: 4,
			GPUType:  common.GPUTypeNvidiaA100,
		},
		Priority: 7,
	}
	node := NodeScore{
		NodeName:       "bench-node",
		GPUFreeCount:   8,
		GPUType:        "nvidia-a100",
		GPUUtilization: 45.0,
		CostPerHour:    12.0,
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		engine.scoreNode(&node, workload)
	}
}

// ============================================================================
// Benchmark: RL Optimizer
// ============================================================================

func BenchmarkEncodeState(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		EncodeState("training", 4, 7, 55.0)
	}
}

func BenchmarkSelectAction(b *testing.B) {
	rl := NewRLOptimizer(RLOptimizerConfig{ExplorationRate: 0.0})
	state := EncodeState("training", 4, 7, 55.0)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rl.SelectAction(state)
	}
}

func BenchmarkUpdateQValue(b *testing.B) {
	rl := NewRLOptimizer(RLOptimizerConfig{})
	state := EncodeState("training", 2, 5, 50.0)
	nextState := EncodeState("training", 2, 5, 60.0)
	action := SchedulingAction{
		NodePreference:   "balanced",
		GPUShareMode:     "exclusive",
		PreemptionChoice: "allow",
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rl.UpdateQValue(state, action, 0.8, nextState)
	}
}

// ============================================================================
// Benchmark: ScoreTopology
// ============================================================================

func BenchmarkScoreTopology(b *testing.B) {
	topo := &NodeGPUTopology{
		TotalGPUs: 8,
		HasNVLink: true,
		NVLinks: []NVLinkConnection{
			{GPU1Index: 0, GPU2Index: 1, LinkType: "NV12", BandwidthGB: 600},
			{GPU1Index: 2, GPU2Index: 3, LinkType: "NV12", BandwidthGB: 600},
		},
		NUMANodes: map[int][]int{0: {0, 1, 2, 3}, 1: {4, 5, 6, 7}},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ScoreTopology(topo, 4, true, 300.0)
	}
}

// ============================================================================
// Benchmark: K8s Quantity Parsing
// ============================================================================

func BenchmarkParseK8sQuantity(b *testing.B) {
	inputs := []string{"128Gi", "32000Mi", "1024Ki", "1Ti", ""}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		parseK8sQuantity(inputs[i%len(inputs)])
	}
}

// ============================================================================
// Benchmark: Candidate Node Generation (fallback path)
// ============================================================================

func BenchmarkGetCandidateNodes(b *testing.B) {
	engine, _ := NewEngine(EngineConfig{})
	workload := &Workload{
		ResourceRequest: common.ResourceRequest{GPUCount: 2},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		engine.getCandidateNodes(context.Background(), workload)
	}
}
