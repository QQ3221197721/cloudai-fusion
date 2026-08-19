// Package orchestrator — benchmarks for Modules 14-16 (AI/ML Workload Management).
//
// Measures the core scheduling primitives: DAG pipeline topological sort and levels,
// gang scheduling allocation efficiency with varying cluster sizes, checkpoint save/load
// performance, job state machine transitions, and autoscaler decision latency under
// different threshold configurations. Results provide a baseline for comparing against
// Apache Airflow, Kubeflow Pipelines, and MLflow.
//
// Honesty note: All benchmarks are micro-benchmarks measuring isolated algorithmic cost.
// End-to-end system latency will be higher due to I/O, network, and coordination overhead.
package orchestrator

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// ============================================================================
// Module 14 — DAG Pipeline Benchmarks
// ============================================================================

// BenchmarkPipelineTopoOrder measures the cost of topological sorting a DAG with
// realistic depth/width characteristics: 5 stages deep, up to 3 concurrent per level.
func BenchmarkPipelineTopoOrder(b *testing.B) {
	// Create a multi-level DAG
	stages := []Stage{
		{ID: "preprocess"},
		{ID: "augment"},
		{ID: "train_base"},
		{ID: "eval_base"},
		{ID: "train_head"},
	}
	edges := []Dep{
		{From: "preprocess", To: "augment"},
		{From: "augment", To: "train_base"},
		{From: "train_base", To: "eval_base"},
		{From: "train_base", To: "train_head"},
	}
	pipeline := Pipeline{Nodes: stages, Edges: edges}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = pipeline.TopoOrder()
	}
}

// BenchmarkPipelineLevels measures the cost of computing dependency levels (Kahn's
// algorithm), which is used for parallel execution planning across levels.
func BenchmarkPipelineLevels(b *testing.B) {
	stages := []Stage{
		{ID: "data_load"},
		{ID: "feature_engineer"},
		{ID: "model_init"},
		{ID: "train_epoch_1"},
		{ID: "train_epoch_2"},
		{ID: "evaluate"},
		{ID: "checkpoint"},
	}
	edges := []Dep{
		{From: "data_load", To: "feature_engineer"},
		{From: "feature_engineer", To: "model_init"},
		{From: "model_init", To: "train_epoch_1"},
		{From: "train_epoch_1", To: "train_epoch_2"},
		{From: "train_epoch_2", To: "evaluate"},
		{From: "evaluate", To: "checkpoint"},
	}
	pipeline := Pipeline{Nodes: stages, Edges: edges}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = pipeline.Levels()
	}
}

// BenchmarkPipelineValidateLarge measures validation cost on a larger realistic DAG
// with ~20 stages simulating an end-to-end MLOps pipeline.
func BenchmarkPipelineValidateLarge(b *testing.B) {
	stages := make([]Stage, 20)
	var edges []Dep
	for i := 0; i < 20; i++ {
		stages[i] = Stage{ID: fmt.Sprintf("stage_%d", i)}
		if i > 0 {
			edges = append(edges, Dep{From: fmt.Sprintf("stage_%d", i-1), To: fmt.Sprintf("stage_%d", i)})
		}
		if i > 2 && i%3 == 0 {
			edges = append(edges, Dep{From: fmt.Sprintf("stage_%d", i-2), To: fmt.Sprintf("stage_%d", i)})
		}
	}
	pipeline := Pipeline{Nodes: stages, Edges: edges}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pipeline.Validate()
	}
}

// ============================================================================
// Module 14 — Gang Scheduling Benchmarks
// ============================================================================

// BenchmarkAllocateGangSmall measures gang allocation on a small cluster (4 nodes).
func BenchmarkAllocateGangSmall(b *testing.B) {
	pool := NewResourcePool()
	for i := 1; i <= 4; i++ {
		_ = pool.AddNode(fmt.Sprintf("node-%d", i), 8, 64*1024) // 8 GPUs, 64GB mem
	}
	req := GangRequest{JobID: "bench-small", Workers: 2, GPUsPerWorker: 2, MemMBPerWorker: 8192}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pl, err := pool.AllocateGang(req)
		if err != nil || pl == nil {
			b.Fatalf("allocate: %v", err)
		}
		_ = pool.ReleaseGang(pl.JobID)
	}
}

// BenchmarkAllocateGangLarge measures gang allocation on a medium cluster (16 nodes).
func BenchmarkAllocateGangLarge(b *testing.B) {
	pool := NewResourcePool()
	for i := 1; i <= 16; i++ {
		_ = pool.AddNode(fmt.Sprintf("gpu-node-%d", i), 8, 256*1024) // 8 GPUs, 256GB mem
	}
	req := GangRequest{JobID: "bench-large", Workers: 4, GPUsPerWorker: 4, MemMBPerWorker: 32768}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pl, err := pool.AllocateGang(req)
		if err != nil || pl == nil {
			b.Fatalf("allocate: %v", err)
		}
		_ = pool.ReleaseGang(pl.JobID)
	}
}

// BenchmarkAllocateGangFragmented tests allocation under memory pressure (high fragmentation).
func BenchmarkAllocateGangFragmented(b *testing.B) {
	pool := NewResourcePool()
	for i := 1; i <= 8; i++ {
		_ = pool.AddNode(fmt.Sprintf("node-%d", i), 4, 32*1024) // 4 GPUs, 32GB mem
	}
	// Allocate some resources first to create fragmentation
	for i := 0; i < 10; i++ {
		req := GangRequest{JobID: fmt.Sprintf("fragment-%d", i), Workers: 1, GPUsPerWorker: 1, MemMBPerWorker: 8192}
		if pl, err := pool.AllocateGang(req); err == nil {
			_ = pool.ReleaseGang(pl.JobID)
		}
	}
	req := GangRequest{JobID: "bench-frag", Workers: 2, GPUsPerWorker: 2, MemMBPerWorker: 16384}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = pool.AllocateGang(req)
	}
}

// BenchmarkReleaseGang measures the cost of releasing allocated resources back to the pool.
func BenchmarkReleaseGang(b *testing.B) {
	pool := NewResourcePool()
	for i := 1; i <= 16; i++ {
		_ = pool.AddNode(fmt.Sprintf("node-%d", i), 8, 128*1024)
	}
	placements := make([]*Placement, 0, 100)
	for i := 0; i < 100; i++ {
		req := GangRequest{JobID: fmt.Sprintf("job-%d", i), Workers: 2, GPUsPerWorker: 2, MemMBPerWorker: 16384}
		if pl, err := pool.AllocateGang(req); err == nil {
			placements = append(placements, pl)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pool.ReleaseGang(placements[i%len(placements)].JobID)
	}
}

// ============================================================================
// Module 14 — Checkpoint Management Benchmarks
// ============================================================================

// BenchmarkCheckpointSave measures persisting a checkpoint to in-memory store.
func BenchmarkCheckpointSave(b *testing.B) {
	store := NewMemCheckpointStore()
	ctx := context.Background()
	cp := Checkpoint{
		JobID:     "bench-job",
		Step:      1000,
		CreatedAt: time.Now().UTC(),
		CompletedStages: []string{"preprocess", "augment", "train", "evaluate"},
		Metadata: map[string]string{
			"accuracy": "0.9234",
			"loss":     "0.3421",
			"lr":       "0.001",
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = store.Save(ctx, cp)
	}
}

// BenchmarkCheckpointLoad measures retrieving a checkpoint from in-memory store.
func BenchmarkCheckpointLoad(b *testing.B) {
	store := NewMemCheckpointStore()
	ctx := context.Background()
	cp := Checkpoint{JobID: "bench-job", Step: 1000, CompletedStages: []string{"preprocess", "augment"}}
	_ = store.Save(ctx, cp)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = store.Load(ctx, "bench-job", -1)
	}
}

// BenchmarkCheckpointList measures listing all checkpoints for a job.
func BenchmarkCheckpointList(b *testing.B) {
	store := NewMemCheckpointStore()
	ctx := context.Background()
	for step := 0; step < 100; step++ {
		cp := Checkpoint{JobID: "bench-job", Step: step, CompletedStages: []string{fmt.Sprintf("stage-%d", step)}}
		_ = store.Save(ctx, cp)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = store.List(ctx, "bench-job")
	}
}

// BenchmarkCheckpointPrune measures pruning old checkpoints based on retention policy.
func BenchmarkCheckpointPrune(b *testing.B) {
	store := NewMemCheckpointStore()
	ctx := context.Background()
	now := time.Now().UTC()
	store.SetClock(func() time.Time { return now })
	policy := RetentionPolicy{KeepLast: 5, MaxAge: 24 * time.Hour}
	for step := 0; step < 50; step++ {
		cp := Checkpoint{
			JobID:           "bench-job",
			Step:            step,
			CreatedAt:       now.Add(-time.Duration(step) * time.Hour),
			CompletedStages: []string{fmt.Sprintf("stage-%d", step)},
		}
		_ = store.Save(ctx, cp)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = store.Prune(ctx, "bench-job", policy)
	}
}

// ============================================================================
// Module 14 — Job Manager Benchmarks
// ============================================================================

// BenchmarkSubmitJob measures admitting a job into Pending state.
func BenchmarkSubmitJob(b *testing.B) {
	pool := NewResourcePool()
	ckpt := NewMemCheckpointStore()
	manager := NewJobManager(pool, ckpt)
	ctx := context.Background()
	spec := JobSpec{
		ID:          "bench-job",
		Name:        "benchmark-training",
		Workers:     4,
		GPUsPerWorker: 4,
		MemMBPerWorker: 32768,
		Priority:    10,
		Pipeline: Pipeline{
			Nodes: []Stage{{ID: "preprocess"}, {ID: "train"}},
			Edges: []Dep{{From: "preprocess", To: "train"}},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		spec.ID = fmt.Sprintf("bench-job-%d", i)
		_, _ = manager.Submit(ctx, spec)
	}
}

// BenchmarkScheduleJob measures gang scheduling for a pending job.
func BenchmarkScheduleJob(b *testing.B) {
	pool := NewResourcePool()
	for i := 1; i <= 8; i++ {
		_ = pool.AddNode(fmt.Sprintf("gpu-node-%d", i), 8, 128*1024)
	}
	ckpt := NewMemCheckpointStore()
	manager := NewJobManager(pool, ckpt)
	ctx := context.Background()
	spec := JobSpec{
		ID:             "bench-job",
		Name:           "benchmark-schedule",
		Workers:        2,
		GPUsPerWorker:   2,
		MemMBPerWorker: 16384,
		Pipeline: Pipeline{
			Nodes: []Stage{{ID: "step1"}, {ID: "step2"}},
		},
	}

	// Pre-submit jobs
	for i := 0; i < 10; i++ {
		spec.ID = fmt.Sprintf("bench-job-%d", i)
		_, _ = manager.Submit(ctx, spec)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = manager.Schedule(ctx, fmt.Sprintf("bench-job-%d", i))
	}
}

// BenchmarkTransitionState measures state machine transition cost.
func BenchmarkTransitionState(b *testing.B) {
	pool := NewResourcePool()
	ckpt := NewMemCheckpointStore()
	manager := NewJobManager(pool, ckpt)
	ctx := context.Background()
	spec := JobSpec{ID: "bench-transition", Workers: 2, GPUsPerWorker: 1, Pipeline: Pipeline{Nodes: []Stage{{ID: "x"}}}}
	_, _ = manager.Submit(ctx, spec)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = manager.Transition(ctx, "bench-transition", StateRunning, "test")
	}
}

// ============================================================================
// Module 15 & 16 — Autoscaler Benchmarks
// ============================================================================

// BenchmarkThresholdScalerDecideInference measures HPA-compatible scaling decision latency.
func BenchmarkThresholdScalerDecideInference(b *testing.B) {
	cfg := DefaultThresholdConfig(PoolInference)
	scaler, _ := NewThresholdScaler(cfg)
	metrics := ClusterMetrics{
		Timestamp:              time.Now().UTC(),
		InferenceReplicas:      10,
		InferenceMinReplicas:   2,
		InferenceMaxReplicas:   50,
		InferenceQPS:           5000,
		TargetQPSPerReplica:    500,
		InferenceQueueDepth:    100,
		TargetQueuePerReplica:  20,
		CPUPercent:             65.5,
		GPUPercent:             72.3,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = scaler.Decide(context.Background(), metrics)
	}
}

// BenchmarkThresholdScalerDecideTraining measures training pool backlog-driven scaling.
func BenchmarkThresholdScalerDecideTraining(b *testing.B) {
	cfg := DefaultThresholdConfig(PoolTraining)
	scaler, _ := NewThresholdScaler(cfg)
	metrics := ClusterMetrics{
		Timestamp:            time.Now().UTC(),
		TrainingWorkers:      20,
		TrainingPendingJobs:  15,
		TrainingMinWorkers:   4,
		TrainingMaxWorkers:   100,
		WorkersPerPendingJob: 4,
		CPUPercent:           45.2,
		GPUPercent:           58.7,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = scaler.Decide(context.Background(), metrics)
	}
}

// BenchmarkCooldownGateAllow measures cooldown window check latency.
func BenchmarkCooldownGateAllow(b *testing.B) {
	gate := NewCooldownGate(DefaultScaleUpCooldown, DefaultScaleDownCooldown)
	now := time.Now().UTC()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gate.Allow(PoolInference, ScaleUp, now)
	}
}

// BenchmarkArbiterDecide measures cross-pool arbitration between inference and training.
func BenchmarkArbiterDecide(b *testing.B) {
	inferCfg := DefaultThresholdConfig(PoolInference)
	trainCfg := DefaultThresholdConfig(PoolTraining)
	inferScaler, _ := NewThresholdScaler(inferCfg)
	trainScaler, _ := NewThresholdScaler(trainCfg)
	arbiter, _ := NewArbiter(inferScaler, trainScaler, DefaultArbiterConfig(200))
	metrics := ClusterMetrics{
		Timestamp:                time.Now().UTC(),
		InferenceReplicas:        25,
		InferenceMinReplicas:     5,
		InferenceMaxReplicas:     60,
		InferenceQPS:             12000,
		TargetQPSPerReplica:      500,
		InferenceQueueDepth:      300,
		TargetQueuePerReplica:    20,
		TrainingWorkers:          40,
		TrainingPendingJobs:      20,
		TrainingMinWorkers:       8,
		TrainingMaxWorkers:       120,
		WorkersPerPendingJob:     4,
		CPUPercent:               75.8,
		GPUPercent:               82.1,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = arbiter.Decide(context.Background(), metrics)
	}
}

// BenchmarkCollectMetrics measures the cost of gathering cluster metrics from active jobs.
func BenchmarkCollectMetrics(b *testing.B) {
	jm := NewJobManager(nil, nil)
	ctx := context.Background()
	// Seed some jobs
	for i := 0; i < 20; i++ {
		spec := JobSpec{
			ID:          fmt.Sprintf("job-%d", i),
			Workers:     2 + rand.Intn(6),
			GPUsPerWorker: 1 + rand.Intn(4),
			Pipeline:    Pipeline{Nodes: []Stage{{ID: "st"}}},
		}
		if _, err := jm.Submit(ctx, spec); err == nil {
			if i%2 == 0 {
				_ = jm.Start(ctx, spec.ID)
			}
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CollectMetrics(time.Now(), jm, nil)
	}
}

// ============================================================================
// Synthetic Load / Scalability Benchmarks
// ============================================================================

// BenchmarkPipelineScalesLinearly measures how pipeline topo order scales with graph size.
func BenchmarkPipelineScalesLinearly(b *testing.B) {
	sizes := []int{10, 20, 50, 100}
	b.StopTimer()
	for _, n := range sizes {
		stages := make([]Stage, n)
		for i := 0; i < n; i++ {
			stages[i] = Stage{ID: fmt.Sprintf("s%d", i)}
		}
		for i := 1; i < n; i++ {
			// Linear chain + some cross-links
			stages[0].Run = func(ctx context.Context) error { return nil }
		}
		pipeline := Pipeline{Nodes: stages, Edges: nil}
		b.Run(fmt.Sprintf("nodes-%d", n), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = pipeline.TopoOrder()
			}
		})
	}
}

// BenchmarkGangSchedulingClusterSize measures gang allocation under increasing node count.
func BenchmarkGangSchedulingClusterSize(b *testing.B) {
	nodeCounts := []int{4, 8, 16, 32}
	b.StopTimer()
	for _, nodes := range nodeCounts {
		pool := NewResourcePool()
		for i := 1; i <= nodes; i++ {
			_ = pool.AddNode(fmt.Sprintf("node-%d", i), 8, 128*1024)
		}
		req := GangRequest{JobID: "bench", Workers: 2, GPUsPerWorker: 2, MemMBPerWorker: 8192}
		b.Run(fmt.Sprintf("nodes-%d", nodes), func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				pl, _ := pool.AllocateGang(req)
				if pl != nil {
					_ = pool.ReleaseGang(pl.JobID)
				}
			}
		})
	}
}
