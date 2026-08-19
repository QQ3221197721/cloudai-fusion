package pipeline

import (
	"math"
	"sort"
	"testing"
)

// buildDiamondDAG constructs a canonical diamond graph with known critical path:
//
//	A(3) → B(2) → D(1)
//	A(3) → C(4) → D(1)
//
// Earliest finishes: A=3, B=5, C=7, D=8 (makespan=8).
// Critical path: A → C → D (slack 0); B has slack 2.
func buildDiamondDAG() ([]DAGTask, [][2]string) {
	tasks := []DAGTask{
		{ID: "A", Duration: 3, MemoryMB: 100, BandwidthInMBPS: 10, BandwidthOutMBPS: 10},
		{ID: "B", Duration: 2, MemoryMB: 200, BandwidthInMBPS: 20, BandwidthOutMBPS: 20},
		{ID: "C", Duration: 4, MemoryMB: 150, BandwidthInMBPS: 15, BandwidthOutMBPS: 15},
		{ID: "D", Duration: 1, MemoryMB: 100, BandwidthInMBPS: 5, BandwidthOutMBPS: 5},
	}
	deps := [][2]string{{"A", "B"}, {"A", "C"}, {"B", "D"}, {"C", "D"}}
	return tasks, deps
}

// TestTopologicalSort_ValidOrdering verifies parents always precede children.
func TestTopologicalSort_ValidOrdering(t *testing.T) {
	tasks, deps := buildDiamondDAG()
	dag := NewDAG(tasks, deps)

	order, ok := dag.TopologicalSort()
	if !ok {
		t.Fatal("expected valid topological sort")
	}
	if len(order) != 4 {
		t.Fatalf("expected 4 nodes, got %d", len(order))
	}

	pos := make(map[string]int)
	for i, id := range order {
		pos[id] = i
	}
	// A must come before B, C, D; B and C before D.
	for _, edge := range deps {
		if pos[edge[0]] >= pos[edge[1]] {
			t.Errorf("dependency violated: %s(pos %d) must precede %s(pos %d)",
				edge[0], pos[edge[0]], edge[1], pos[edge[1]])
		}
	}
}

// TestTopologicalSort_CycleDetection verifies cycles are reported as invalid.
func TestTopologicalSort_CycleDetection(t *testing.T) {
	tasks := []DAGTask{
		{ID: "X", Duration: 1}, {ID: "Y", Duration: 1}, {ID: "Z", Duration: 1},
	}
	deps := [][2]string{{"X", "Y"}, {"Y", "Z"}, {"Z", "X"}} // cycle
	dag := NewDAG(tasks, deps)

	_, ok := dag.TopologicalSort()
	if ok {
		t.Error("expected cycle to be detected (valid=false)")
	}
}

// TestFindCriticalPath_DiamondGraph verifies makespan and critical-node set.
func TestFindCriticalPath_DiamondGraph(t *testing.T) {
	tasks, deps := buildDiamondDAG()
	dag := NewDAG(tasks, deps)

	critical, makespan, ef, ls := dag.FindCriticalPath()

	if math.Abs(makespan-8.0) > epsilon {
		t.Errorf("expected makespan 8, got %.2f", makespan)
	}
	// Earliest finishes must match the hand-computed values.
	expectEF := map[string]float64{"A": 3, "B": 5, "C": 7, "D": 8}
	for id, want := range expectEF {
		if math.Abs(ef[id]-want) > epsilon {
			t.Errorf("EF[%s] = %.2f, want %.2f", id, ef[id], want)
		}
	}
	// Critical path = {A, C, D}; B has slack and must NOT be critical.
	sort.Strings(critical)
	want := []string{"A", "C", "D"}
	if len(critical) != len(want) {
		t.Fatalf("expected %v critical nodes, got %v", want, critical)
	}
	for i := range want {
		if critical[i] != want[i] {
			t.Errorf("critical[%d] = %s, want %s (full=%v)", i, critical[i], want[i], critical)
		}
	}
	// Slack on B must be positive.
	slackB := (ls["B"] + 2) - ef["B"]
	if slackB <= epsilon {
		t.Errorf("expected B to have positive slack, got %.2f", slackB)
	}
}

// TestFindCriticalPath_LinearChain verifies a chain's critical path is the whole chain.
func TestFindCriticalPath_LinearChain(t *testing.T) {
	tasks := []DAGTask{
		{ID: "n1", Duration: 2}, {ID: "n2", Duration: 3}, {ID: "n3", Duration: 5},
	}
	deps := [][2]string{{"n1", "n2"}, {"n2", "n3"}}
	dag := NewDAG(tasks, deps)

	critical, makespan, _, _ := dag.FindCriticalPath()
	if math.Abs(makespan-10.0) > epsilon {
		t.Errorf("expected makespan 10, got %.2f", makespan)
	}
	if len(critical) != 3 {
		t.Errorf("expected entire chain critical, got %v", critical)
	}
}

// TestOptimizePartition_RespectsConstraints verifies partitioning honors memory/bandwidth caps.
func TestOptimizePartition_RespectsConstraints(t *testing.T) {
	tasks, deps := buildDiamondDAG()

	req := PartitionRequest{
		TotalBandwidthMBPS: 100,
		TotalMemoryMB:      300, // A+C = 250 fits; adding B (200) would exceed
		NodeCount:          4,
	}
	plan := OptimizePartition(tasks, deps, req)

	if len(plan.Stages) == 0 {
		t.Fatal("expected at least one stage")
	}

	// Verify no stage exceeds memory or node-count constraints.
	dag := NewDAG(tasks, deps)
	for si, stage := range plan.Stages {
		if len(stage) > req.NodeCount {
			t.Errorf("stage %d has %d tasks, exceeds node count %d", si, len(stage), req.NodeCount)
		}
		mem := 0.0
		for _, id := range stage {
			mem += dag.tasks[id].MemoryMB
		}
		if mem > req.TotalMemoryMB+epsilon {
			t.Errorf("stage %d memory %.0f exceeds cap %.0f", si, mem, req.TotalMemoryMB)
		}
	}

	// All tasks must appear exactly once across stages.
	seen := make(map[string]int)
	for _, stage := range plan.Stages {
		for _, id := range stage {
			seen[id]++
		}
	}
	if len(seen) != len(tasks) {
		t.Errorf("expected all %d tasks scheduled, got %d distinct", len(tasks), len(seen))
	}
	for id, c := range seen {
		if c != 1 {
			t.Errorf("task %s scheduled %d times", id, c)
		}
	}

	if plan.Throughput <= 0 {
		t.Error("expected positive throughput")
	}
	t.Logf("plan: stages=%d throughput=%.4f util=%.3f criticalStage=%.2f",
		len(plan.Stages), plan.Throughput, plan.Utilization, plan.CriticalStageLength)
}

// TestFindOptimalCheckpoints_HighFailureRate verifies more checkpoints under high failure risk.
func TestFindOptimalCheckpoints_HighFailureRate(t *testing.T) {
	stages := [][]string{{"s1"}, {"s2"}, {"s3"}, {"s4"}}
	durations := []float64{10, 10, 10, 10}

	// High failure rate + low overhead → checkpoints beneficial.
	highRisk := CheckpointConfig{TaskFailureRate: 0.5, CheckpointOverhead: 0.5, RPLimit: 5}
	cpHigh := FindOptimalCheckpoints(stages, durations, highRisk)
	if len(cpHigh) != len(stages) {
		t.Fatalf("expected %d checkpoint flags, got %d", len(stages), len(cpHigh))
	}
	countHigh := 0
	for _, c := range cpHigh {
		if c {
			countHigh++
		}
	}

	// Very low failure rate → fewer checkpoints needed.
	lowRisk := CheckpointConfig{TaskFailureRate: 0.001, CheckpointOverhead: 5.0, RPLimit: 1000}
	cpLow := FindOptimalCheckpoints(stages, durations, lowRisk)
	countLow := 0
	for _, c := range cpLow {
		if c {
			countLow++
		}
	}

	t.Logf("checkpoints: highRisk=%d lowRisk=%d", countHigh, countLow)
	if countHigh < countLow {
		t.Errorf("expected high-risk config to place >= checkpoints than low-risk (high=%d low=%d)", countHigh, countLow)
	}
}

// TestFindOptimalCheckpoints_EmptyInput verifies graceful handling of empty stages.
func TestFindOptimalCheckpoints_EmptyInput(t *testing.T) {
	if cp := FindOptimalCheckpoints(nil, nil, CheckpointConfig{}); cp != nil {
		t.Errorf("expected nil for empty input, got %v", cp)
	}
}

// TestOptimizePartition_EmptyGraph verifies empty task list returns empty plan.
func TestOptimizePartition_EmptyGraph(t *testing.T) {
	plan := OptimizePartition(nil, nil, PartitionRequest{TotalMemoryMB: 100, TotalBandwidthMBPS: 100, NodeCount: 2})
	if len(plan.Stages) != 0 {
		t.Errorf("expected empty plan for empty graph, got %d stages", len(plan.Stages))
	}
}
