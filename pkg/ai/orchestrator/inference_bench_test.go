// Package orchestrator — Benchmark for Module 15 Inference Mesh.
//
// These benchmarks isolate the M15 inference-serving primitives — endpoint deploy
// (Register), canary routing (Router.Pick), replica scaling (ScaleTo), and native GPU
// memory pooling (MemoryPool block-level lease) — from the DAG/gang/checkpoint
// primitives already covered in orchestrator_bench_test.go.
//
// Responsibility boundary (verified against inference.go): M15 owns model serving,
// traffic routing and GPU memory isolation. It does NOT emit trace spans (that is
// M47's distributed tracing) nor perform tail-sampling optimization (that is M18's
// trace optimizer). There is no overlap: the Mesh/Router/MemoryPool types here carry
// no span/context or sampling logic.
//
// Honesty note: All figures are isolated micro-benchmarks of in-memory algorithmic
// cost. Real inference latency is dominated by model execution on the GPU, which these
// numbers deliberately exclude — they measure only the control-plane overhead the mesh
// adds around each request.
//
// Naming: every benchmark here is prefixed Benchmark Inference* to avoid collision with
// the existing Benchmark{AllocateGang,ReleaseGang,ScheduleJob,...} functions in
// orchestrator_bench_test.go.
package orchestrator

import (
	"context"
	"fmt"
	"testing"
)

// ============================================================================
// Benchmark #1: InferenceDeploy — Endpoint registration (Mesh.Register)
// ============================================================================

// BenchmarkInferenceDeploy measures the cost of deploying (registering) a model
// endpoint into the mesh. With MinReplicas=0 no GPU allocation happens, so this
// isolates the validation + registry-insert cost of a deploy.
// Target: <1ms per Op.
func BenchmarkInferenceDeploy(b *testing.B) {
	ctx := context.Background()
	mesh := NewMesh(nil, NewRouter(1), nil)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ep := Endpoint{
			Name:        fmt.Sprintf("ep-%d", i),
			Model:       "resnet50",
			Version:     "v1",
			MinReplicas: 0,
			MaxReplicas: 8,
			TargetQPS:   100,
			ModelSizeMB: 512,
		}
		if err := mesh.Register(ctx, ep); err != nil {
			b.Fatalf("Register failed: %v", err)
		}
	}
}

// BenchmarkInferenceDeployWithReplicas measures the fuller deploy path that also
// reserves GPU memory for MinReplicas resident replicas. This shows the added cost of
// the memory-pool allocation on the deploy hot path.
func BenchmarkInferenceDeployWithReplicas(b *testing.B) {
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		pool := NewMemoryPool()
		_ = pool.AddGPU("gpu-0", 80000) // 80 GB card
		mesh := NewMesh(pool, NewRouter(1), nil)
		b.StartTimer()

		ep := Endpoint{
			Name:        "ep",
			Model:       "resnet50",
			Version:     "v1",
			MinReplicas: 2,
			MaxReplicas: 8,
			TargetQPS:   100,
			ModelSizeMB: 512,
		}
		if err := mesh.Register(ctx, ep); err != nil {
			b.Fatalf("Register failed: %v", err)
		}
	}
}

// ============================================================================
// Benchmark #2: InferenceRoute — Canary weighted pick (Router.Pick)
// ============================================================================

// BenchmarkInferenceRoute measures a single canary routing decision: given a model
// with a weighted version split, pick the version that serves one request.
// Target: <100ns per Op.
func BenchmarkInferenceRoute(b *testing.B) {
	router := NewRouter(42)
	if err := router.SetRoute("resnet50", []VersionWeight{
		{Version: "v1", Endpoint: "ep-v1", Weight: 90},
		{Version: "v2", Endpoint: "ep-v2", Weight: 10}, // canary
	}); err != nil {
		b.Fatalf("SetRoute failed: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := router.Pick("resnet50"); err != nil {
			b.Fatalf("Pick failed: %v", err)
		}
	}
}

// BenchmarkInferenceRouteDeterministic measures the deterministic PickAt path, which
// removes the RNG draw and isolates the cumulative-weight bucket walk.
func BenchmarkInferenceRouteDeterministic(b *testing.B) {
	router := NewRouter(42)
	if err := router.SetRoute("resnet50", []VersionWeight{
		{Version: "v1", Endpoint: "ep-v1", Weight: 90},
		{Version: "v2", Endpoint: "ep-v2", Weight: 10},
	}); err != nil {
		b.Fatalf("SetRoute failed: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := router.PickAt("resnet50", i%100); err != nil {
			b.Fatalf("PickAt failed: %v", err)
		}
	}
}

// ============================================================================
// Benchmark #3: InferenceScaleTo — Replica scale up/down (Mesh.ScaleTo)
// ============================================================================

// BenchmarkInferenceScaleTo measures the cost of driving an endpoint's replica count,
// including the per-replica GPU memory reserve/release against the pool. Each iteration
// scales up to 4 replicas then back to 0, so both the allocate and release paths are
// exercised.
// Target: <50µs per Op.
func BenchmarkInferenceScaleTo(b *testing.B) {
	ctx := context.Background()
	pool := NewMemoryPool()
	_ = pool.AddGPU("gpu-0", 80000)
	_ = pool.AddGPU("gpu-1", 80000)
	mesh := NewMesh(pool, NewRouter(1), nil)

	ep := Endpoint{
		Name:        "svc",
		Model:       "llama",
		Version:     "v1",
		MinReplicas: 0,
		MaxReplicas: 8,
		TargetQPS:   100,
		ModelSizeMB: 4096,
	}
	if err := mesh.Register(ctx, ep); err != nil {
		b.Fatalf("Register failed: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := mesh.ScaleTo(ctx, "svc", 4); err != nil {
			b.Fatalf("ScaleTo up failed: %v", err)
		}
		if _, err := mesh.ScaleTo(ctx, "svc", 0); err != nil {
			b.Fatalf("ScaleTo down failed: %v", err)
		}
	}
}

// BenchmarkInferenceScaleUpOnly measures a single scale-up step (allocate path only),
// resetting the endpoint outside the timed region each iteration.
func BenchmarkInferenceScaleUpOnly(b *testing.B) {
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		pool := NewMemoryPool()
		_ = pool.AddGPU("gpu-0", 80000)
		mesh := NewMesh(pool, NewRouter(1), nil)
		_ = mesh.Register(ctx, Endpoint{
			Name: "svc", Model: "llama", Version: "v1",
			MinReplicas: 0, MaxReplicas: 8, TargetQPS: 100, ModelSizeMB: 4096,
		})
		b.StartTimer()

		if _, err := mesh.ScaleTo(ctx, "svc", 4); err != nil {
			b.Fatalf("ScaleTo failed: %v", err)
		}
	}
}

// ============================================================================
// Benchmark #4: InferenceMemoryPool — GPU block-level lease (MemoryPool)
// ============================================================================

// BenchmarkInferenceMemoryPool measures the native GPU memory pool's block-level lease
// cycle: best-fit Allocate followed by Release. This is the primitive that keeps several
// models co-resident on one card while preserving large contiguous blocks elsewhere.
func BenchmarkInferenceMemoryPool(b *testing.B) {
	pool := NewMemoryPool()
	_ = pool.AddGPU("gpu-0", 80000)
	_ = pool.AddGPU("gpu-1", 80000)
	_ = pool.AddGPU("gpu-2", 80000)
	_ = pool.AddGPU("gpu-3", 80000)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lease, err := pool.Allocate("lease-x", 4096)
		if err != nil {
			b.Fatalf("Allocate failed: %v", err)
		}
		if err := pool.Release(lease.LeaseID); err != nil {
			b.Fatalf("Release failed: %v", err)
		}
	}
}

// BenchmarkInferenceMemoryPoolStats measures the per-GPU accounting snapshot cost,
// which operators poll to diagnose fragmentation.
func BenchmarkInferenceMemoryPoolStats(b *testing.B) {
	pool := NewMemoryPool()
	for g := 0; g < 8; g++ {
		_ = pool.AddGPU(fmt.Sprintf("gpu-%d", g), 80000)
		_, _ = pool.Allocate(fmt.Sprintf("resident-%d", g), 8192)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pool.Stats()
	}
}

// BenchmarkInferenceMemoryPoolFragmented measures the failure/diagnosis path: an
// allocation that cannot be placed returns a *FragmentationError with a per-GPU report.
// The pool is filled so every card has some free memory but none can host the request.
func BenchmarkInferenceMemoryPoolFragmented(b *testing.B) {
	pool := NewMemoryPool()
	// Two 10GB cards, each already holding 7GB → 3GB free each (6GB total, but no single
	// card can host a 4GB request): a genuine fragmentation scenario.
	_ = pool.AddGPU("gpu-0", 10000)
	_ = pool.AddGPU("gpu-1", 10000)
	_, _ = pool.Allocate("pin-0", 7000)
	_, _ = pool.Allocate("pin-1", 7000)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := pool.Allocate("frag", 4000)
		if err == nil {
			b.Fatal("expected fragmentation error, got nil")
		}
	}
}
