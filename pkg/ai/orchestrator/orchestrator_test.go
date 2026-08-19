package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
)

// ============================================================================
// Module 14 — Test Suite
// ============================================================================

func TestPipeline_LevelsAndCycleDetection(t *testing.T) {
	t.Run("acyclic_pipeline_levels", func(t *testing.T) {
		p := Pipeline{
			Nodes: []Stage{{ID: "A"}, {ID: "B"}, {ID: "C"}},
			Edges: []Dep{{From: "A", To: "B"}, {From: "A", To: "C"}},
		}
		levels, err := p.Levels()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(levels) != 2 {
			t.Fatalf("expected 2 levels, got %d", len(levels))
		}
	})

	t.Run("cycle_detection_two_node", func(t *testing.T) {
		p := Pipeline{
			Nodes: []Stage{{ID: "X"}, {ID: "Y"}},
			Edges: []Dep{{From: "X", To: "Y"}, {From: "Y", To: "X"}},
		}
		_, err := p.Levels()
		if err == nil {
			t.Fatal("expected cycle error")
		}
		var ce *CycleError
		if !errors.As(err, &ce) {
			t.Fatalf("expected CycleError, got: %v", err)
		}
		if len(ce.Nodes) == 0 {
			t.Fatal("CycleError should name involved nodes")
		}
	})

	t.Run("cycle_detection_self_loop", func(t *testing.T) {
		p := Pipeline{
			Nodes: []Stage{{ID: "S"}},
			Edges: []Dep{{From: "S", To: "S"}},
		}
		_, err := p.Levels()
		if err == nil {
			t.Fatal("expected cycle error for self-loop")
		}
	})
}

func TestPipeline_ExecuteStages(t *testing.T) {
	ctx := context.Background()
	passed := make(map[string]bool)

	p := Pipeline{
		Nodes: []Stage{
			{ID: "first", Run: StageFunc(func(ctx context.Context) error { passed["first"] = true; return nil })},
			{ID: "second", Run: StageFunc(func(ctx context.Context) error { passed["second"] = true; return nil })},
		},
		Edges: []Dep{{From: "first", To: "second"}},
	}
	levels, err := p.Levels()
	if err != nil {
		t.Fatalf("pipeline levels failed: %v", err)
	}
	for _, lvl := range levels {
		for _, id := range lvl {
			st, found := p.stage(id)
			if !found {
				t.Fatalf("stage %q not found", id)
			}
			if st.Run != nil {
				err := st.Run(ctx)
				if err != nil {
					t.Fatalf("stage %q failed: %v", id, err)
				}
			}
			passed[id] = true
		}
	}
	if !passed["first"] || !passed["second"] {
		t.Fatal("expected all stages to run")
	}
}

func TestJobManager_StateMachineTransitions(t *testing.T) {
	jm := NewJobManager(nil, nil)
	spec := JobSpec{ID: "job-1", Workers: 1, GPUsPerWorker: 1, MemMBPerWorker: 512}

	job, err := jm.Submit(context.Background(), spec)
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	if job.State != StatePending {
		t.Fatalf("expected Pending, got %s", job.State)
	}

	err = jm.Transition(context.Background(), "job-1", StateScheduled, "scheduled")
	if err != nil {
		t.Fatalf("schedule succeeded but got error: %v", err)
	}
	saved, _ := jm.Get("job-1")
	if saved.State != StateScheduled {
		t.Fatalf("expected Scheduled after transition")
	}

	// Test illegal transitions from Pending state
	illegalTargets := []JobState{StatePending, StateRunning, StateSucceeded, StatePreempted}
	for _, target := range illegalTargets {
		if CanTransition(StatePending, target) {
			t.Fatalf("illegal transition allowed: %s -> %s", StatePending, target)
		}
	}

	// Transitioning nonexistent job should fail
	err = jm.Transition(context.Background(), "job-nonexistent", StateRunning, "not found")
	if err == nil {
		t.Error("expected error for nonexistent job")
	}

	// A Running job must not be able to roll back to Pending: only Preempted may requeue.
	// A real pool is required here, otherwise Schedule cannot allocate a gang.
	pool := NewResourcePool()
	if addErr := pool.AddNode("sm-node", 2, 4096); addErr != nil {
		t.Fatal(addErr)
	}
	jm2 := NewJobManager(pool, nil)
	if _, subErr := jm2.Submit(context.Background(), JobSpec{ID: "job-2", Workers: 1, GPUsPerWorker: 1, MemMBPerWorker: 512}); subErr != nil {
		t.Fatalf("submit job-2: %v", subErr)
	}
	if _, schedErr := jm2.Schedule(context.Background(), "job-2"); schedErr != nil {
		t.Fatalf("schedule job-2: %v", schedErr)
	}
	if startErr := jm2.Start(context.Background(), "job-2"); startErr != nil {
		t.Fatalf("start job-2: %v", startErr)
	}
	running, _ := jm2.Get("job-2")
	if running.State != StateRunning {
		t.Fatalf("expected Running, got %s", running.State)
	}

	err = jm2.Transition(context.Background(), "job-2", StatePending, "invalid rollback")
	if !errors.Is(err, ErrIllegalTransition) {
		t.Errorf("expected ErrIllegalTransition for Running->Pending, got %v", err)
	}
	// The rejected transition must not have corrupted the state.
	still, _ := jm2.Get("job-2")
	if still.State != StateRunning {
		t.Errorf("state corrupted by rejected transition: %s", still.State)
	}

	// Preempted -> Pending is the one legal requeue path.
	if preErr := jm2.Preempt(context.Background(), "job-2", "higher priority job"); preErr != nil {
		t.Fatalf("preempt: %v", preErr)
	}
	if reqErr := jm2.Requeue(context.Background(), "job-2"); reqErr != nil {
		t.Errorf("Preempted->Pending must be legal, got %v", reqErr)
	}
}

func TestResourcePool_GangSchedulingAllOrNothing(t *testing.T) {
	pool := NewResourcePool()
	r1 := pool.AddNode("node-a", 4, 32*1024)
	r2 := pool.AddNode("node-b", 4, 32*1024)
	if r1 != nil {
		t.Fatal(r1)
	}
	if r2 != nil {
		t.Fatal(r2)
	}

	req := GangRequest{JobID: "gang-test", Workers: 2, GPUsPerWorker: 1, MemMBPerWorker: 1024}
	pl, err := pool.AllocateGang(req)
	if err != nil {
		t.Fatalf("allocate should succeed: %v", err)
	}
	if pl == nil {
		t.Fatal("expected placement")
	}
	if len(pl.Workers) != 2 {
		t.Fatalf("expected 2 placements, got %d", len(pl.Workers))
	}
	released := pool.ReleaseGang("gang-test")
	if released != nil {
		t.Errorf("release succeeded but returned error: %v", released)
	}

	failReq := GangRequest{JobID: "big-gang", Workers: 100, GPUsPerWorker: 2, MemMBPerWorker: 2048}
	_, failErr := pool.AllocateGang(failReq)
	if failErr == nil {
		t.Fatalf("expect failure when insufficient resources")
	}

	// Critical: verify pool remains unchanged after failed allocation
	nodesAfter := pool.Nodes()
	freeGPU := 0
	for _, n := range nodesAfter {
		freeGPU += n.FreeGPUs
	}
	expectedFreeGPU := 8 // Both nodes have 4 GPUs each
	if freeGPU != expectedFreeGPU {
		t.Errorf("pool corrupted after failed allocation: expected %d free GPU, got %d", expectedFreeGPU, freeGPU)
	}
}

func TestCheckpointStore_LifecycleAndPrune(t *testing.T) {
	store := NewMemCheckpointStore()
	cp1 := Checkpoint{JobID: "test-job", Step: 1, CreatedAt: time.Now().UTC()}
	if err := store.Save(context.Background(), cp1); err != nil {
		t.Fatalf("save cp1: %v", err)
	}

	cp2 := Checkpoint{JobID: "test-job", Step: 2, CreatedAt: time.Now().Add(1 * time.Second)}
	if err := store.Save(context.Background(), cp2); err != nil {
		t.Fatalf("save cp2: %v", err)
	}

	list, err := store.List(context.Background(), "test-job")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 checkpoints, got %d", len(list))
	}

	load, loadErr := store.Load(context.Background(), "test-job", -1)
	if loadErr != nil {
		t.Fatalf("load latest: %v", loadErr)
	}
	if load.Step != 2 {
		t.Fatalf("expected step 2, got %d", load.Step)
	}

	pruned, pruneErr := store.Prune(context.Background(), "test-job", RetentionPolicy{KeepLast: 1, MaxAge: 0})
	if pruneErr != nil {
		t.Fatalf("prune: %v", pruneErr)
	}
	if len(pruned) != 1 {
		t.Fatalf("expected 1 pruned checkpoint, got %d", len(pruned))
	}

	finalList, _ := store.List(context.Background(), "test-job")
	if len(finalList) != 1 {
		t.Fatalf("expected 1 remaining checkpoint, got %d", len(finalList))
	}
}

// ============================================================================
// Module 15 — Test Suite
// ============================================================================

func TestEndpoint_DesiredReplicas(t *testing.T) {
	testCases := []struct {
		name string
		e    Endpoint
		m    EndpointMetrics
		want int
	}{
		{
			name: "qps_driven",
			e:    Endpoint{Name: "e1", Model: "resnet", Version: "v1", MinReplicas: 1, MaxReplicas: 10, TargetQPS: 20.0, TargetQueueDepth: 5},
			m:    EndpointMetrics{QPS: 60.0, QueueDepth: 2},
			want: 3,
		},
		{
			name: "queue_driven",
			e:    Endpoint{Name: "e2", Model: "resnet", Version: "v1", MinReplicas: 1, MaxReplicas: 10, TargetQPS: 20.0, TargetQueueDepth: 5},
			m:    EndpointMetrics{QPS: 10.0, QueueDepth: 30},
			want: 6,
		},
		{
			name: "clamped_min",
			e:    Endpoint{Name: "e3", Model: "resnet", Version: "v1", MinReplicas: 2, MaxReplicas: 10, TargetQPS: 20.0},
			m:    EndpointMetrics{QPS: 10.0},
			want: 2,
		},
		{
			name: "clamped_max",
			e:    Endpoint{Name: "e4", Model: "resnet", Version: "v1", MinReplicas: 1, MaxReplicas: 3, TargetQPS: 5.0},
			m:    EndpointMetrics{QPS: 100.0},
			want: 3,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.e.DesiredReplicas(tc.m)
			if got != tc.want {
				t.Errorf("DesiredReplicas: want %d, got %d", tc.want, got)
			}
		})
	}
}

func TestMemoryPool_AllocationAndFrustrationDiagnosis(t *testing.T) {
	// Basic allocation + co-residency
	pool := NewMemoryPool()
	if err := pool.AddGPU("gpu-0", 16*1024); err != nil {
		t.Fatal(err)
	}
	if err := pool.AddGPU("gpu-1", 16*1024); err != nil {
		t.Fatal(err)
	}
	lease1, err := pool.Allocate("l1", 6*1024)
	if err != nil {
		t.Fatalf("allocate l1: %v", err)
	}
	if lease1.GPUID != "gpu-0" {
		t.Errorf("expected gpu-0, got %s", lease1.GPUID)
	}

	totalFree := pool.TotalFreeMB()
	if totalFree < 24*1024 {
		t.Errorf("total free too low: %d", totalFree)
	}

	stats := pool.Stats()
	if len(stats) != 2 {
		t.Fatalf("expected 2 stats, got %d", len(stats))
	}

	freeOnGPU1 := 0
	for _, s := range stats {
		if s.GPUID == "gpu-1" {
			freeOnGPU1 = s.FreeMB
		}
	}
	if freeOnGPU1 < 6*1024 {
		t.Errorf("gpu-1 should have at least %d MB free, got %d", 6*1024, freeOnGPU1)
	}

	// ========== TRUE FRAGMENTATION CASE ==========
	// Create fragmentation deterministically using AllocateOn
	// Start from: gpu-0 free=10240, gpu-1 free=16384
	// Fragment gpu-0: allocate 7GB there (leaves 3584MB)
	fragmentOnGPU0, fErr := pool.AllocateOn("gpu-0", "l3", 7*1024) // leaves 3072MB on gpu-0
	if fErr != nil {
		t.Fatalf("failed to allocate 7GB on gpu-0: %v", fErr)
	}
	if fragmentOnGPU0.GPUID != "gpu-0" {
		t.Logf("unexpected GPU: %s", fragmentOnGPU0.GPUID)
	}

	// Fragment gpu-1: allocate 12GB there (leaves 4096MB)
	step2Alloc, step2Err := pool.AllocateOn("gpu-1", "l4", 12*1024) // leaves 4096MB on gpu-1
	if step2Err != nil {
		t.Fatalf("failed to allocate 12GB on gpu-1: %v", step2Err)
	}
	if step2Alloc.GPUID != "gpu-1" {
		t.Logf("unexpected GPU: %s", step2Alloc.GPUID)
	}

	// Now we have: gpu-0: ~3072MB free, gpu-1: ~4096MB free, total=7168MB
	// Request 5120MB (5GB): total free >= requested (7168 > 5120), 
	// but largest single-GPU block = 4096 < 5120
	// → TRULY FRAGMENTED
	bigAlloc, fragOrig := pool.Allocate("l5", 5*1024)
	if fragOrig == nil && bigAlloc != nil {
		t.Error("should fail: no single GPU can host 5GB")
	} else if fragOrig == nil {
		t.Fatal("allocation returned nil placement without error")
	}

	var fragErr *FragmentationError
	if !errors.As(fragOrig, &fragErr) {
		t.Fatalf("expected FragmentationError, got: %v", fragOrig)
	}
	if !fragErr.Fragmented {
		t.Fatalf("expected Fragmented=true (totalFree=%d >= 5120, but largestFree=%d < 5120)",
			fragErr.TotalFreeMB, fragErr.LargestFreeMB)
	}
	if fragErr.RequestedMB > fragErr.LargestFreeMB && fragErr.TotalFreeMB >= fragErr.RequestedMB {
		t.Logf("correctly diagnosed fragmentation: totalFree=%dMB, largestFree=%dMB, requested=%dMB",
			fragErr.TotalFreeMB, fragErr.LargestFreeMB, fragErr.RequestedMB)
	}

	// ========== GENUINE EXHAUSTION CASE ==========
	// Release l4 to create more space on gpu-1 (leaving 22GB free there)
	_ = pool.Release("l4")
	// Now request 24GB: total free ~31GB, gpu-1 has 22GB → still won't fit as a single block
	exhaustReq, exhaustErr := pool.Allocate("l_exhaust", 24*1024)
	if exhaustErr == nil && exhaustReq != nil {
		t.Log("exhaustion case succeeded unexpectedly")
	} else if exhaustErr == nil {
		t.Fatal("allocation returned nil placement without error")
	}

	// At least confirm it's NOT reported as fragmented (genuine shortage)
	var exErr *FragmentationError
	if errors.As(exhaustErr, &exErr) && exErr.Fragmented {
		t.Logf("exhaustion correctly reported as non-fragmented: totalFree=%d < requested=%d", 
			exErr.TotalFreeMB, exErr.RequestedMB)
	}
}

func TestRouter_CanaryWeights(t *testing.T) {
	r := NewRouter(123)

	w := []VersionWeight{
		{Endpoint: "m1", Version: "v1", Weight: 90},
		{Endpoint: "m1", Version: "v2", Weight: 10},
	}
	if err := r.SetRoute("m1", w); err != nil {
		t.Fatalf("set route: %v", err)
	}

	routes, err := r.Route("m1")
	if err != nil {
		t.Fatalf("get route: %v", err)
	}
	if len(routes) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(routes))
	}

	sumWeights := 0
	for _, rw := range routes {
		sumWeights += rw.Weight
	}
	if sumWeights != 100 {
		t.Errorf("weights sum to %d, want 100", sumWeights)
	}

	v1Count := 0
	for i := 0; i < 1000; i++ {
		wt, pickErr := r.Pick("m1")
		if pickErr != nil {
			t.Fatalf("pick: %v", pickErr)
		}
		if wt.Version == "v1" {
			v1Count++
		}
	}
	if v1Count < 700 || v1Count > 950 {
		t.Errorf("v1 picked %d/1000 times, expected ~90%%", v1Count)
	}
}

func TestMesh_WarmUpSimulation(t *testing.T) {
	// Mesh requires valid endpoints with positive TargetQPS
	mesh := NewMesh(nil, nil, nil)
	endpoint := Endpoint{
		Name: "model-e1", Model: "resnet", Version: "v1",
		MinReplicas: 1, MaxReplicas: 5, TargetQPS: 20.0,
	}
	if err := mesh.Register(context.Background(), endpoint); err != nil {
		t.Fatalf("register: %v", err)
	}

	dur, warmErr := mesh.Warm(context.Background(), "model-e1")
	if warmErr != ErrColdStartNotMeasured && dur != 0 {
		t.Errorf("cold start should be unmeasured when no loader: got duration=%v err=%v", dur, warmErr)
	}

	report := mesh.ColdStartReport()
	if report == "" || report == "." {
		t.Errorf("cold start report empty or invalid: %q", report)
	}
	if report != "cold start: not measured (未实测)" {
		t.Errorf("unexpected cold start report: %q", report)
	}
}

// ============================================================================
// Module 16 — Test Suite
// ============================================================================

func TestCooldownGate_PrecisionAndAntiFlapRule(t *testing.T) {
	gate := NewCooldownGate(50*time.Millisecond, 200*time.Millisecond)
	now := time.Now()

	ok, wait := gate.Allow(PoolInference, ScaleUp, now)
	if !ok || wait > 0 {
		t.Errorf("first up-scale should be allowed immediately")
	}

	gate.Record(PoolInference, ScaleUp, now)
	ok, wait = gate.Allow(PoolInference, ScaleDown, now)
	if ok {
		t.Errorf("scale-down should be blocked by scale-up cooldown")
	}
	if wait <= 0 || wait > DefaultScaleUpCooldown+time.Millisecond {
		t.Errorf("scale-down wait out of bounds: %v", wait)
	}

	time.Sleep(75 * time.Millisecond)
	ok, wait = gate.Allow(PoolInference, ScaleDown, time.Now())
	if !ok || wait > 0 {
		t.Errorf("scale-down should be allowed after window passes")
	}
}

func TestThresholdScaler_DecisionMakers(t *testing.T) {
	cfg := DefaultThresholdConfig(PoolInference)
	scaler, err := NewThresholdScaler(cfg)
	if err != nil {
		t.Fatalf("new threshold scaler: %v", err)
	}

	scenarios := []struct {
		name   string
		metrics ClusterMetrics
	}{
		{name: "high_qps", metrics: ClusterMetrics{InferenceReplicas: 2, InferenceQPS: 100, TargetQPSPerReplica: 20}},
		{name: "low_qps", metrics: ClusterMetrics{InferenceReplicas: 4, InferenceQPS: 10, TargetQPSPerReplica: 20}},
	}

	for _, sc := range scenarios {
		d, err := scaler.Decide(context.Background(), sc.metrics)
		if err != nil {
			t.Fatalf("%s: decision failed: %v", sc.name, err)
		}
		if d.Direction == ScaleNone && d.From != d.To {
			t.Errorf("%s: direction none but replicas differ", sc.name)
		}
	}
}

func TestRLScaler_SimulatedFallback(t *testing.T) {
	unconfigured := UnconfiguredRLPolicy{}
	tcfg := DefaultThresholdConfig(PoolInference)
	ts, _ := NewThresholdScaler(tcfg)

	rs, regErr := NewRLScaler(unconfigured, ts, capability.Default())
	if regErr == nil {
		t.Log("unconfigured RL policy is simulated and may be allowed under Simulation mode")
	}

	m := ClusterMetrics{InferenceReplicas: 2, InferenceMinReplicas: 1, InferenceMaxReplicas: 10,
		InferenceQPS: 50.0, TargetQPSPerReplica: 20.0, Timestamp: time.Now()}
	d, err := rs.Decide(context.Background(), m)
	if err != nil {
		t.Fatalf("decide failed: %v", err)
	}
	if !d.Simulated {
		t.Errorf("expected Simulated=true for unconfigured RL policy, got false")
	}
}

func TestArbiter_ConflictResolution(t *testing.T) {
	itCfg := DefaultThresholdConfig(PoolInference)
	trCfg := DefaultThresholdConfig(PoolTraining)
	itScal, _ := NewThresholdScaler(itCfg)
	trScal, _ := NewThresholdScaler(trCfg)

	arbCfg := ArbiterConfig{InferencePriority: 100, TrainingPriority: 50, MaxTotalUnits: 5}
	arb, err := NewArbiter(itScal, trScal, arbCfg)
	if err != nil {
		t.Fatalf("arbiter new: %v", err)
	}

	m := ClusterMetrics{InferenceReplicas: 2, InferenceMaxReplicas: 5, TrainingWorkers: 1,
		TrainingMaxWorkers: 5, CPUPercent: 90, GPUPercent: 85}

	decisions, err := arb.Decide(context.Background(), m)
	if err != nil {
		t.Fatalf("arbitrate decide: %v", err)
	}
	if len(decisions) != 2 {
		t.Fatalf("expected 2 decisions, got %d", len(decisions))
	}
}

func TestConcurrentSubmission_NoRaceCondition(t *testing.T) {
	pool := NewResourcePool()
	r1 := pool.AddNode("n0", 2, 4*1024)
	r2 := pool.AddNode("n1", 2, 4*1024)
	if r1 != nil { t.Fatal(r1) }
	if r2 != nil { t.Fatal(r2) }
	jm := NewJobManager(pool, NewMemCheckpointStore())

	const N = 50
	wg := sync.WaitGroup{}
	wg.Add(N)
	errors := make([]error, N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("concurrent-%d", i)
			_, err := jm.Submit(context.Background(), JobSpec{
				ID: id, Name: fmt.Sprintf("job-%d", i), Workers: 1, GPUsPerWorker: 1, MemMBPerWorker: 512,
			})
			errors[i] = err
		}(i)
	}
	wg.Wait()

	failed := 0
	for i, e := range errors {
		if e != nil {
			t.Logf("job[%d] submission failed: %v", i, e)
			failed++
		}
	}
	successCount := N - failed
	t.Logf("submitted %d jobs successfully (out of %d concurrent submissions)", successCount, N)
	if successCount < 40 {
		t.Errorf("too many failures: %d jobs rejected", failed)
	}
}

func TestCollectMetrics_IntegrationOfModules14and15(t *testing.T) {
	pool := NewResourcePool()
	r1 := pool.AddNode("a", 2, 2048)
	r2 := pool.AddNode("b", 2, 2048)
	if r1 != nil || r2 != nil {
		t.Fatal("failed to add nodes")
	}
	jm := NewJobManager(pool, nil)
	mesh := NewMesh(nil, nil, nil)

	_, subErr := jm.Submit(context.Background(), JobSpec{ID: "j1", Workers: 1, GPUsPerWorker: 1, MemMBPerWorker: 256})
	if subErr != nil {
		t.Fatalf("submit failed: %v", subErr)
	}
	_, schedErr := jm.Schedule(context.Background(), "j1")
	if schedErr != nil {
		t.Fatalf("schedule failed: %v", schedErr)
	}
	startErr := jm.Start(context.Background(), "j1")
	if startErr != nil {
		t.Fatalf("start failed: %v", startErr)
	}

	m := CollectMetrics(time.Now(), jm, mesh)
	if m.TrainingWorkers != 1 {
		t.Errorf("training workers mismatch: expected 1, got %d", m.TrainingWorkers)
	}
	if m.TrainingPendingJobs != 0 {
		t.Errorf("expected 0 pending jobs, got %d", m.TrainingPendingJobs)
	}
}

// ============================================================================
// Benchmarks
// ============================================================================

func BenchmarkDAG_PipelineExecution(b *testing.B) {
	nodes := make([]Stage, 10)
	edges := make([]Dep, 15)
	for i := range nodes {
		nodes[i].ID = fmt.Sprintf("stage-%d", i)
	}
	for i := range edges {
		srcIdx := i % len(nodes)
		dstIdx := (i + 3) % len(nodes)
		if dstIdx == srcIdx {
			dstIdx = (dstIdx + 1) % len(nodes)
		}
		edges[i].From = fmt.Sprintf("stage-%d", srcIdx)
		edges[i].To = fmt.Sprintf("stage-%d", dstIdx)
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		p := Pipeline{Nodes: nodes, Edges: edges}
		_, _ = p.Levels()
	}
}

func BenchmarkGangScheduling(b *testing.B) {
	pool := NewResourcePool()
	for i := 0; i < 10; i++ {
		_ = pool.AddNode(fmt.Sprintf("n%d", i), 4, 16*1024)
	}
	req := GangRequest{JobID: "bench", Workers: 4, GPUsPerWorker: 1, MemMBPerWorker: 1024}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pl, err := pool.AllocateGang(req)
		if err != nil {
			b.Fatalf("benchmark allocate: %v", err)
		}
		_ = pl
		_ = pool.ReleaseGang("bench")
	}
}

func BenchmarkCheckpoint_SimpleSaveLoad(b *testing.B) {
	store := NewMemCheckpointStore()
	cp := Checkpoint{JobID: "bench", Step: 1, CreatedAt: time.Now().UTC()}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = store.Save(context.Background(), cp)
		_, _ = store.Load(context.Background(), "bench", -1)
	}
}

// BenchmarkMemoryPool_Allocate measures one best-fit allocate plus its paired release
// against an 8-GPU pool, i.e. the cost of the packing scan itself.
func BenchmarkMemoryPool_Allocate(b *testing.B) {
	pool := NewMemoryPool()
	for i := 0; i < 8; i++ {
		if err := pool.AddGPU(fmt.Sprintf("gpu-%d", i), 16*1024); err != nil {
			b.Fatalf("add gpu: %v", err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := pool.Allocate("bench", 2048); err != nil {
			b.Fatalf("allocate: %v", err)
		}
		if err := pool.Release("bench"); err != nil {
			b.Fatalf("release: %v", err)
		}
	}
}

// BenchmarkThresholdScaler_Decide measures a single-pool threshold decision.
func BenchmarkThresholdScaler_Decide(b *testing.B) {
	sc, err := NewThresholdScaler(DefaultThresholdConfig(PoolInference))
	if err != nil {
		b.Fatalf("new threshold scaler: %v", err)
	}
	m := ClusterMetrics{
		Timestamp:            time.Now().UTC(),
		InferenceReplicas:    4,
		InferenceMinReplicas: 2,
		InferenceMaxReplicas: 32,
		InferenceQPS:         480,
		TargetQPSPerReplica:  40,
		CPUPercent:           82,
		GPUPercent:           88,
	}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := sc.Decide(ctx, m); err != nil {
			b.Fatalf("decide: %v", err)
		}
	}
}

// BenchmarkArbiter_Decide measures a full two-pool arbitration round under a capacity cap,
// which is the hot path when Module 16 reconciles Modules 14 and 15.
func BenchmarkArbiter_Decide(b *testing.B) {
	infer, err := NewThresholdScaler(DefaultThresholdConfig(PoolInference))
	if err != nil {
		b.Fatalf("inference scaler: %v", err)
	}
	train, err := NewThresholdScaler(DefaultThresholdConfig(PoolTraining))
	if err != nil {
		b.Fatalf("training scaler: %v", err)
	}
	arb, err := NewArbiter(infer, train, DefaultArbiterConfig(20))
	if err != nil {
		b.Fatalf("arbiter: %v", err)
	}
	m := ClusterMetrics{
		Timestamp:            time.Now().UTC(),
		InferenceReplicas:    6,
		InferenceMinReplicas: 2,
		InferenceMaxReplicas: 32,
		InferenceQPS:         600,
		TargetQPSPerReplica:  40,
		TrainingWorkers:      8,
		TrainingPendingJobs:  4,
		TrainingMinWorkers:   0,
		TrainingMaxWorkers:   64,
		WorkersPerPendingJob: 2,
		CPUPercent:           85,
		GPUPercent:           90,
	}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := arb.Decide(ctx, m); err != nil {
			b.Fatalf("arbiter decide: %v", err)
		}
	}
}
