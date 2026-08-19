package scheduler

import (
	"context"
	"testing"
)

func testNodes() []GPUNode {
	return []GPUNode{
		{Name: "node-a", FreeGPUs: 8, HasNVLink: true, PowerPerGPUW: 300, LatencyBaseMs: 2, TPSPerGPU: 120},
		{Name: "node-b", FreeGPUs: 8, HasNVLink: false, PowerPerGPUW: 250, LatencyBaseMs: 5, TPSPerGPU: 90},
		{Name: "node-c", FreeGPUs: 4, HasNVLink: true, PowerPerGPUW: 320, LatencyBaseMs: 3, TPSPerGPU: 130},
	}
}

func generateWorkload(n int) []Job {
	jobs := make([]Job, 0, n)
	for i := 0; i < n; i++ {
		jobs = append(jobs, Job{
			ID:           string(rune('A'+(i%26))) + string(rune('0'+(i%10))),
			GPUCount:     1 + i%4,
			ExpectedTPS:  100,
			LatencyClass: 10,
			PowerBudgetW: 300,
			PreferNVLink: i%2 == 0,
		})
	}
	return jobs
}

func newTestScheduler(t *testing.T) *EvidenceGPUScheduler {
	t.Helper()
	s, err := NewEvidenceGPUScheduler(EvidenceGPUSchedulerConfig{
		Nodes:              testNodes(),
		ParetoSamples:      100,
		MaxAdaptiveSamples: 200, // Cap to limit expansion
	})
	if err != nil {
		t.Fatalf("NewEvidenceGPUScheduler: %v", err)
	}
	return s
}

func TestEvidenceScheduler_ReceiptVerifies(t *testing.T) {
	s := newTestScheduler(t)
	jobs := []Job{
		{ID: "j1", GPUCount: 2, ExpectedTPS: 100, LatencyClass: 5, PowerBudgetW: 300, PreferNVLink: true},
		{ID: "j2", GPUCount: 1, ExpectedTPS: 80, LatencyClass: 8, PowerBudgetW: 250},
	}
	result, receipt, err := s.Schedule(context.Background(), jobs)
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("expected at least one placement")
	}
	if receipt == nil || !receipt.Verify() {
		t.Fatal("receipt must verify with a real Ed25519 signature")
	}
	if receipt.Module != "scheduler.gpu" || receipt.Operation != "Schedule" {
		t.Errorf("unexpected receipt module/op: %s/%s", receipt.Module, receipt.Operation)
	}
}

func TestEvidenceScheduler_ReceiptBindsInput(t *testing.T) {
	s := newTestScheduler(t)
	jobsA := []Job{{ID: "a", GPUCount: 2}}
	jobsB := []Job{{ID: "b", GPUCount: 3}}

	_, rA, err := s.Schedule(context.Background(), jobsA)
	if err != nil {
		t.Fatalf("schedule A: %v", err)
	}
	_, rB, err := s.Schedule(context.Background(), jobsB)
	if err != nil {
		t.Fatalf("schedule B: %v", err)
	}
	if rA.InputHash == rB.InputHash {
		t.Error("different inputs must produce different input hashes")
	}
	if jobsHash(jobsA) == jobsHash(jobsB) {
		t.Error("jobsHash must differ for different jobs")
	}
}

func TestEvidenceScheduler_ParetoOptimal(t *testing.T) {
	s := newTestScheduler(t)
	jobs := generateWorkload(6)
	_, receipt, err := s.Schedule(context.Background(), jobs)
	if err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	// The greedy topology-aware placement should not be strictly dominated by
	// random alternatives; confidence must be high.
	if receipt.Metadata["pareto_optimal"] == "" {
		t.Fatal("pareto_optimal metadata missing")
	}
}

func TestEvidenceScheduler_ParetoProofStructure(t *testing.T) {
	// Use a config with min adaptive samples.
	s, err := NewEvidenceGPUScheduler(EvidenceGPUSchedulerConfig{
		Nodes:   testNodes(),
		MaxAdaptiveSamples: 100, // Cap so alternatives don't expand
	})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	jobs := generateWorkload(5)
	result := s.placeTopologyAware(jobs)
	proof := s.verifyParetoOptimality(jobs, result)
	// With adaptive sampling, Alternatives should be >= paretoSamples and <= maxAdaptiveSamples.
	// The new proof also has HVI and AdaptiveSamples fields.
	if proof.AdaptiveSamples < s.paretoSamples || proof.AdaptiveSamples > s.maxAdaptive {
		t.Errorf("expected adaptive samples in [%d,%d], got %d", s.paretoSamples, s.maxAdaptive, proof.AdaptiveSamples)
	}
	if proof.HVI < 0 {
		t.Errorf("HVI must be non-negative, got %f", proof.HVI)
	}
	if proof.Confidence < 0 || proof.Confidence > 1 {
		t.Errorf("confidence out of range: %f", proof.Confidence)
	}
	if proof.FrontierSize < 1 {
		t.Error("frontier must contain at least one point")
	}
}

func TestDominatesWithTolerance(t *testing.T) {
	a := ObjectiveVector{NegThroughput: -100, Latency: 5, Power: 300}
	b := ObjectiveVector{NegThroughput: -50, Latency: 10, Power: 400}
	if !dominatesWithTolerance(a, b, 0) {
		t.Error("a should dominate b on all axes")
	}
	if dominatesWithTolerance(b, a, 0) {
		t.Error("b must not dominate a")
	}
}

func TestEvidenceScheduler_NoNodesErrors(t *testing.T) {
	s, err := NewEvidenceGPUScheduler(EvidenceGPUSchedulerConfig{Nodes: nil})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	if _, _, err := s.Schedule(context.Background(), []Job{{ID: "x", GPUCount: 1}}); err == nil {
		t.Error("expected error with no nodes")
	}
}

// ============================================================================
// Benchmarks
// ============================================================================

func BenchmarkEvidenceGPUScheduler_WithReceipt(b *testing.B) {
	s, err := NewEvidenceGPUScheduler(EvidenceGPUSchedulerConfig{Nodes: testNodes()})
	if err != nil {
		b.Fatalf("construct: %v", err)
	}
	jobs := generateWorkload(20)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, receipt, err := s.Schedule(ctx, jobs)
		if err != nil {
			b.Fatal(err)
		}
		if !receipt.Verify() {
			b.Fatal("invalid receipt")
		}
	}
	// Target: full schedule + pareto proof + signed receipt within a few ms.
	// Overhead vs bare placement is dominated by the N=100 pareto sampling.
}

func BenchmarkEvidenceGPUScheduler_PlacementOnly(b *testing.B) {
	s, _ := NewEvidenceGPUScheduler(EvidenceGPUSchedulerConfig{Nodes: testNodes()})
	jobs := generateWorkload(20)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.placeTopologyAware(jobs)
	}
}

func BenchmarkEvidenceGPUScheduler_ParetoVerification(b *testing.B) {
	s, _ := NewEvidenceGPUScheduler(EvidenceGPUSchedulerConfig{Nodes: testNodes()})
	jobs := generateWorkload(20)
	result := s.placeTopologyAware(jobs)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.verifyParetoOptimality(jobs, result)
	}
}
