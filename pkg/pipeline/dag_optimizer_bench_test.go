package pipeline

import (
	"fmt"
	"testing"
)

// buildLayeredDAG builds a wide layered DAG with `layers` layers of `width` tasks each.
// Every task in layer L depends on every task in layer L-1 (dense fan-in/out) to stress
// the critical-path and partitioning algorithms.
func buildLayeredDAG(layers, width int) ([]DAGTask, [][2]string) {
	tasks := make([]DAGTask, 0, layers*width)
	deps := make([][2]string, 0)
	for l := 0; l < layers; l++ {
		for w := 0; w < width; w++ {
			id := fmt.Sprintf("L%d_%d", l, w)
			tasks = append(tasks, DAGTask{
				ID: id, Duration: float64(1 + (l+w)%5),
				MemoryMB: float64(50 + (w%3)*25),
				BandwidthInMBPS: float64(5 + w%4), BandwidthOutMBPS: float64(5 + l%4),
			})
			if l > 0 {
				for pw := 0; pw < width; pw++ {
					deps = append(deps, [2]string{fmt.Sprintf("L%d_%d", l-1, pw), id})
				}
			}
		}
	}
	return tasks, deps
}

// BenchmarkTopologicalSort measures topo-sort cost on a 5x8 (40-node) dense DAG.
func BenchmarkTopologicalSort(b *testing.B) {
	tasks, deps := buildLayeredDAG(5, 8)
	dag := NewDAG(tasks, deps)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := dag.TopologicalSort(); !ok {
			b.Fatal("expected valid sort")
		}
	}
}

// BenchmarkFindCriticalPath measures critical-path computation on a 5x8 dense DAG.
func BenchmarkFindCriticalPath(b *testing.B) {
	tasks, deps := buildLayeredDAG(5, 8)
	dag := NewDAG(tasks, deps)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, makespan, _, _ := dag.FindCriticalPath()
		if makespan <= 0 {
			b.Fatal("expected positive makespan")
		}
	}
}

// BenchmarkOptimizePartition measures end-to-end partitioning latency (build + sort + pack).
func BenchmarkOptimizePartition(b *testing.B) {
	tasks, deps := buildLayeredDAG(5, 8)
	req := PartitionRequest{TotalBandwidthMBPS: 500, TotalMemoryMB: 400, NodeCount: 8}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		plan := OptimizePartition(tasks, deps, req)
		if len(plan.Stages) == 0 {
			b.Fatal("expected non-empty partition plan")
		}
	}
}

// BenchmarkFindOptimalCheckpoints measures checkpoint placement over a 20-stage pipeline.
func BenchmarkFindOptimalCheckpoints(b *testing.B) {
	n := 20
	stages := make([][]string, n)
	durations := make([]float64, n)
	for i := 0; i < n; i++ {
		stages[i] = []string{fmt.Sprintf("s%d", i)}
		durations[i] = float64(5 + i%7)
	}
	cfg := CheckpointConfig{TaskFailureRate: 0.1, CheckpointOverhead: 1.0, RPLimit: 10}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cp := FindOptimalCheckpoints(stages, durations, cfg)
		if len(cp) != n {
			b.Fatalf("expected %d flags, got %d", n, len(cp))
		}
	}
}
