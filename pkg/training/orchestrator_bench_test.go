// Benchmarks for Module 14 gang scheduling performance.
//
// Measures:
//   - JobSubmissionLatency: end-to-end Submit (validation + ID + one Ed25519 receipt) — the sub-100ms hot path.
//   - GangAdmissionDecisionsPerSec: all-or-nothing capacity-fit throughput via TryAdmit.
//   - AdmissionFitCheckPerOp: single-reservation decision in microseconds.
package training

import (
	"testing"
	"time"
)

func benchGangScheduler(b *testing.B, cap ClusterCapacity) *GangScheduler {
	b.Helper()
	s, err := NewGangScheduler(cap, testSigner(b))
	if err != nil {
		b.Fatalf("new scheduler: %v", err)
	}
	// Freeze time for determinism
	s.SetClock(func() time.Time { return time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC) })
	return s
}

func validSpecForBenchmark(name string) GangJobSpec {
	return GangJobSpec{
		Name:       name,
		Image:      "pytorch:2.3",
		Replicas:   4,
		Priority:   10,
		MinMembers: 4, // strict gang
		Resources: ResourceRequest{GPUs: 2, CPUCores: 8, MemoryGB: 32},
		Command:    "torchrun --nproc_per_node=2 train.py",
		Queue:      "research",
	}
}

// BenchmarkJobSubmissionLatency measures the real cost of submitting a new gang-scheduled job
// from validation to signing the first receipt (state ""→pending). This is the sub-100ms hot path
// targeting <100ms versus a K8s API server round-trip (~150ms) or Volcano podgroup controller.
//
// Kubeflow MPIJob/PipelineJob admission goes through K8s API server + etcd write plus reconcile loop,
// so we expect microsecond-scale numbers here. We measure pure Go with one signature over ~120 bytes.
func BenchmarkJobSubmissionLatency(b *testing.B) {
	spec := validSpecForBenchmark("submit")
	s := bigScheduler(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		job, err := s.Submit(spec)
		if err != nil {
			b.Fatalf("submit: %v", err)
		}
		_ = job
	}
}

// BenchmarkGangAdmissionDecisionsPerSec measures how many all-or-nothing admission decisions
// can be made per second across gangs waiting in a queue. Each decision calls TryAdmit which
// computes a perfect-fit check against the current capacity ledger.
func BenchmarkGangAdmissionDecisionsPerSec(b *testing.B) {
	// Small capacity so every gang fails admission → fast rejection without state mutation
	s := benchGangScheduler(b, ClusterCapacity{GPUs: 7, CPUCores: 512, MemoryGB: 1024})
	spec := validSpecForBenchmark("admit") // needs 8 GPU but only 7 available → instant reject
	b.ReportAllocs()
	b.ResetTimer()
	var res AdmissionResult
	for i := 0; i < b.N; i++ {
		res = s.TryAdmit(spec)
		_ = res.Admitted
	}
}

// BenchmarkAdmissionFitCheckPerOp measures the single-reservation decision cost after creating
// one job upfront (so we measure just the capacity-fit logic). For an admitting gang, this
// includes adding to allocated; for a rejected one, it's purely a comparison.
func BenchmarkAdmissionFitCheckPerOp(b *testing.B) {
	s := bigScheduler(b)
	job, err := s.Submit(validSpecForBenchmark("fitcheck"))
	if err != nil {
		b.Fatalf("submit: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Admit(job.ID)
	}
}

// BenchmarkTryAdmit_Fit reports admission when resources are plentiful (fast-path).
func BenchmarkTryAdmit_Fit(b *testing.B) {
	s := bigScheduler(b)
	spec := validSpecForBenchmark("fit")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res := s.TryAdmit(spec)
		_ = res.Admitted
	}
}

// BenchmarkTryAdmit_NoFit reports admission when resources are scarce (rejection path).
func BenchmarkTryAdmit_NoFit(b *testing.B) {
	s := benchGangScheduler(b, ClusterCapacity{GPUs: 7, CPUCores: 512, MemoryGB: 1024})
	spec := validSpecForBenchmark("nofit") // needs 8 GPUs
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res := s.TryAdmit(spec)
		_ = res.Shortfall.GPUs
	}
}

// BenchmarkEndToEnd_HappyPath measures full happy-path throughput: submit → admit → start → succeed.
func BenchmarkEndToEnd_HappyPath(b *testing.B) {
	s := bigScheduler(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		job, _ := s.Submit(validSpecForBenchmark("endtoend"))
		s.Admit(job.ID)
		s.Start(job.ID)
		s.Succeed(job.ID)
	}
}

// BenchmarkEndToEnd_RejectPath measures full flow where the gang cannot fit (no resources reserved).
func BenchmarkEndToEnd_RejectPath(b *testing.B) {
	s := benchGangScheduler(b, ClusterCapacity{GPUs: 7, CPUCores: 512, MemoryGB: 1024})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		job, _ := s.Submit(validSpecForBenchmark("rejectpath"))
		s.Admit(job.ID) // returns non-admitted, no reservation
		_ = job
	}
}
