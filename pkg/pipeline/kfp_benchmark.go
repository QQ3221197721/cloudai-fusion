// Package pipeline — head-to-head: M18 ML Pipeline Designer vs Kubeflow Pipelines (KFP).
//
// WHAT IS ACTUALLY MEASURED (no fabrication):
//
//   KFP side  — testdata/kfp_compile_bench.py invokes KFP v2's REAL Compiler
//               (github.com/kubeflow/pipelines, pip pkg `kfp` 2.17.0) to compile
//               an N-component DSL pipeline into IR YAML. Cluster-free, local.
//               Emits per-iteration compile latency (ms) + median/stddev as JSON.
//
//   M18 side  — measured here in Go. The fair analog of "DAG compile/publish" is
//               Create()+Publish(): validate N stages → persist JSON atomically →
//               write a signed, hash-chained Ed25519 attestation via pkg/evidence.
//               That is M18's real spec→persisted-signed-artifact path, mirroring
//               KFP's spec→IR-YAML path. We ALSO report the pure in-memory DAG
//               optimizer cost (critical path + partition) separately.
//
// HONEST ASYMMETRY (stated, not hidden):
//   - KFP compiles to a portable, cluster-submittable IR (protobuf→YAML). M18
//     persists a local JSON + cryptographic attestation; it does NOT emit a
//     Kubernetes-submittable artifact. These are different deliverables of similar
//     intent ("turn a pipeline spec into an executable, durable plan").
//   - Runtime scheduler throughput and real CPU/GPU utilization require a live
//     Kubeflow cluster (KFP) — UNAVAILABLE in this environment (no kube-apiserver,
//     no Docker). Those numbers are therefore NOT reported. Claiming them would be
//     fabrication. See TestKFPHeadToHead which records this limitation explicitly.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/experiment"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/training"
)

// kfpResult mirrors the JSON emitted by testdata/kfp_compile_bench.py.
type kfpResult struct {
	System            string    `json:"system"`
	KFPVersion        string    `json:"kfp_version"`
	TaskCount         int       `json:"task_count"`
	Iterations        int       `json:"iterations"`
	CompileLatencyMs  []float64 `json:"compile_latency_ms"`
	MedianMs          float64   `json:"median_ms"`
	StdDevMs          float64   `json:"stddev_ms"`
	CompiledYAMLBytes int       `json:"compiled_yaml_bytes"`
	Error             string    `json:"error,omitempty"`
}

// median returns the median of a copy of xs (xs is not mutated).
func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 0 {
		return (s[n/2-1] + s[n/2]) / 2
	}
	return s[n/2]
}

// stddev returns the sample standard deviation of xs.
func stddev(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	var mean float64
	for _, v := range xs {
		mean += v
	}
	mean /= float64(len(xs))
	var ss float64
	for _, v := range xs {
		ss += (v - mean) * (v - mean)
	}
	return math.Sqrt(ss / float64(len(xs)-1))
}

// buildM18Designer wires a designer with a real signed ledger (matches bench_test.go).
func buildM18Designer(tb testing.TB) *FSDesigner {
	tb.Helper()
	tmp := tb.TempDir()

	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		tb.Fatalf("generate signer: %v", err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{
		Store:    evidence.NewMemoryStore(),
		Signer:   signer,
		Anchorer: evidence.NewSimulatedAnchorer(),
	})
	if err != nil {
		tb.Fatalf("build ledger: %v", err)
	}
	orch, err := training.NewFSOrchestrator(tmp, ledger)
	if err != nil {
		tb.Fatalf("training orchestrator: %v", err)
	}
	tracker, err := experiment.NewFSTracker(tmp, ledger)
	if err != nil {
		tb.Fatalf("experiment tracker: %v", err)
	}
	cost := scheduler.NewDefaultCostOptimizer(nil)
	d, err := NewFSDesigner(tmp, ledger, Deps{Train: orch, Exp: tracker, Cost: cost})
	if err != nil {
		tb.Fatalf("pipeline designer: %v", err)
	}
	return d
}

// nStageInput builds a CreateInput with n notify stages (isolates designer
// persist+attest overhead; notify has no external deps).
func nStageInput(name string, n int) CreateInput {
	stages := make([]Stage, n)
	for i := 0; i < n; i++ {
		stages[i] = Stage{Name: fmt.Sprintf("stage-%d", i), Type: StageNotify}
	}
	return CreateInput{
		Name:    name,
		Stages:  stages,
		Params:  map[string]string{"epochs": "50", "batch": "32"},
		Trigger: Trigger{Type: TriggerManual},
		Actor:   "bench-runner",
	}
}

// measureM18CompilePublish measures Create()+Publish() latency (ms) for an
// n-stage pipeline over `iters` iterations. This is M18's spec→persisted-signed
// artifact path — the honest analog of KFP's spec→IR-YAML compile.
func measureM18CompilePublish(tb testing.TB, n, iters int) []float64 {
	d := buildM18Designer(tb)
	ctx := context.Background()
	out := make([]float64, 0, iters)
	for i := 0; i < iters; i++ {
		start := time.Now()
		p, err := d.Create(ctx, nStageInput(fmt.Sprintf("bench-%d-%d", n, i), n))
		if err != nil {
			tb.Fatalf("create: %v", err)
		}
		if err := d.Publish(ctx, p.ID); err != nil {
			tb.Fatalf("publish: %v", err)
		}
		out = append(out, float64(time.Since(start))/1e6)
	}
	return out
}

// measureM18Optimize measures the pure in-memory DAG optimizer (critical path +
// partition) latency (ms) for an n-node linear DAG over `iters` iterations.
func measureM18Optimize(n, iters int) []float64 {
	tasks := make([]DAGTask, n)
	deps := make([][2]string, 0, n-1)
	for i := 0; i < n; i++ {
		tasks[i] = DAGTask{ID: fmt.Sprintf("task-%d", i), Duration: float64(i%5+1) * 0.5, MemoryMB: float64(i%3+1) * 512}
		if i > 0 {
			deps = append(deps, [2]string{fmt.Sprintf("task-%d", i-1), fmt.Sprintf("task-%d", i)})
		}
	}
	req := PartitionRequest{TotalBandwidthMBPS: 1000, TotalMemoryMB: 16384, NodeCount: 8}
	out := make([]float64, 0, iters)
	for i := 0; i < iters; i++ {
		start := time.Now()
		dag := NewDAG(tasks, deps)
		_, _, _, _ = dag.FindCriticalPath()
		_ = OptimizePartition(tasks, deps, req)
		out = append(out, float64(time.Since(start))/1e6)
	}
	return out
}

// runKFPCompile shells out to the real KFP compiler benchmark for n tasks.
// Returns (result, ok). ok=false means KFP is unavailable / failed — the caller
// MUST NOT fabricate numbers in that case.
func runKFPCompile(n, iters int) (kfpResult, bool) {
	// testdata sits next to this source file at test time (CWD = package dir).
	script := filepath.Join("testdata", "kfp_compile_bench.py")
	if _, err := os.Stat(script); err != nil {
		return kfpResult{Error: "script not found: " + err.Error()}, false
	}

	var res kfpResult
	for _, py := range []string{"python", "python3"} {
		cmd := exec.Command(py, script, fmt.Sprint(n), fmt.Sprint(iters))
		out, err := cmd.Output()
		if err != nil {
			res.Error = fmt.Sprintf("%s failed: %v", py, err)
			continue
		}
		if jerr := json.Unmarshal(out, &res); jerr != nil {
			res.Error = "bad JSON: " + jerr.Error()
			continue
		}
		if res.Error != "" || len(res.CompileLatencyMs) == 0 {
			continue
		}
		return res, true
	}
	return res, false
}

// ---------------------------------------------------------------------------
// Go benchmarks for the M18 side (captured cleanly via `go test -bench -json`).
// ---------------------------------------------------------------------------

func benchM18CompilePublish(b *testing.B, n int) {
	d := buildM18Designer(b)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p, err := d.Create(ctx, nStageInput(fmt.Sprintf("b-%d-%d", n, i), n))
		if err != nil {
			b.Fatalf("create: %v", err)
		}
		if err := d.Publish(ctx, p.ID); err != nil {
			b.Fatalf("publish: %v", err)
		}
	}
}

func BenchmarkM18CompilePublish_Small(b *testing.B)   { benchM18CompilePublish(b, 10) }
func BenchmarkM18CompilePublish_Complex(b *testing.B) { benchM18CompilePublish(b, 50) }

func benchM18Optimize(b *testing.B, n int) {
	tasks := make([]DAGTask, n)
	deps := make([][2]string, 0, n-1)
	for i := 0; i < n; i++ {
		tasks[i] = DAGTask{ID: fmt.Sprintf("task-%d", i), Duration: float64(i%5+1) * 0.5, MemoryMB: float64(i%3+1) * 512}
		if i > 0 {
			deps = append(deps, [2]string{fmt.Sprintf("task-%d", i-1), fmt.Sprintf("task-%d", i)})
		}
	}
	req := PartitionRequest{TotalBandwidthMBPS: 1000, TotalMemoryMB: 16384, NodeCount: 8}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dag := NewDAG(tasks, deps)
		_, _, _, _ = dag.FindCriticalPath()
		_ = OptimizePartition(tasks, deps, req)
	}
}

func BenchmarkM18Optimize_Small(b *testing.B)   { benchM18Optimize(b, 10) }
func BenchmarkM18Optimize_Complex(b *testing.B) { benchM18Optimize(b, 50) }
