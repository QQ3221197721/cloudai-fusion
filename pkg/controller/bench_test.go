package controller

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Controller Benchmarks — work queue, reconcile loop, manager orchestration
// ============================================================================
//
// Run with:  go test ./pkg/controller/ "-bench=." -benchmem "-run=^$"
//
// Scope note: this package has no informer/shared-cache layer (no client-go
// informers, no object cache). Reconcilers read through injected service
// interfaces, so there is no "cache hit path" to benchmark here; the closest
// analogue is the queue's dedup set, covered by BenchmarkQueue_Add_Dedup.
//
// All benchmarks are hermetic: no network, no DB, logging discarded so that
// logrus formatting cost does not pollute the measurements.

// newBenchLogger returns a logger that discards output.
func newBenchLogger() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(io.Discard)
	l.SetLevel(logrus.PanicLevel)
	return l
}

// atomicNextID provides goroutine-unique IDs via atomic counter for parallel benchmarks.
var atomicNextID func(*int64) int64 = func(counter *int64) int64 {
	return atomic.AddInt64(counter, 1)
}

// benchReconciler is a configurable no-op Reconciler used by manager benchmarks.
// It is distinct from controller_test.go's fakeReconciler, which records every
// request and would make allocation counts reflect the recorder, not the loop.
type benchReconciler struct {
	name   string
	kind   string
	result Result
	err    error
}

func (r *benchReconciler) Reconcile(_ context.Context, _ Request) (Result, error) {
	return r.result, r.err
}
func (r *benchReconciler) Name() string         { return r.name }
func (r *benchReconciler) ResourceKind() string { return r.kind }

// ----------------------------------------------------------------------------
// Request key computation — called on every queue operation
// ----------------------------------------------------------------------------

// BenchmarkRequest_NamespacedName measures the key derivation that Add/Get/Done
// each perform, so its cost is multiplied across the whole queue hot path.
func BenchmarkRequest_NamespacedName(b *testing.B) {
	req := Request{Namespace: "production", Name: "gpt-training-job-42"}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if req.NamespacedName() == "" {
			b.Fatal("empty key")
		}
	}
}

// ----------------------------------------------------------------------------
// Work queue: enqueue / dequeue latency
// ----------------------------------------------------------------------------

// BenchmarkQueue_Add_Unique measures enqueue of distinct keys — the miss path,
// which grows both the dirty set and the FIFO slice.
func BenchmarkQueue_Add_Unique(b *testing.B) {
	q := NewWorkQueue()
	defer q.ShutDown()

	reqs := make([]Request, b.N)
	for i := range reqs {
		reqs[i] = Request{Namespace: "default", Name: fmt.Sprintf("obj-%d", i)}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Add(reqs[i])
	}
	b.StopTimer()

	if got := q.Len(); got != b.N {
		b.Fatalf("queue depth = %d, want %d", got, b.N)
	}
}

// BenchmarkQueue_Add_Dedup measures the dedup hit path: re-adding a key that is
// already dirty must return early without touching the FIFO slice.
func BenchmarkQueue_Add_Dedup(b *testing.B) {
	q := NewWorkQueue()
	defer q.ShutDown()

	req := Request{Namespace: "default", Name: "hot-object"}
	q.Add(req)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.Add(req)
	}
	b.StopTimer()

	if got := q.Len(); got != 1 {
		b.Fatalf("dedup broken: queue depth = %d, want 1", got)
	}
}

// BenchmarkQueue_AddGetDone_RoundTrip measures one full item lifecycle:
// Add -> Get (moves to processing) -> Done. This is the per-item queue overhead
// that every reconcile pays on top of the reconciler's own work.
func BenchmarkQueue_AddGetDone_RoundTrip(b *testing.B) {
	q := NewWorkQueue()
	defer q.ShutDown()

	reqs := make([]Request, b.N)
	for i := range reqs {
		reqs[i] = Request{Namespace: "default", Name: fmt.Sprintf("obj-%d", i)}
	}

	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()

	for i := 0; i < b.N; i++ {
		q.Add(reqs[i])
		req, shutdown := q.Get()
		if shutdown {
			b.Fatalf("unexpected shutdown at i=%d", i)
		}
		q.Done(req)
	}

	b.StopTimer()
	b.ReportMetric(float64(b.N)/time.Since(start).Seconds(), "items/sec")
}

// BenchmarkQueue_AddRateLimited measures the failure/backoff path: backoff map
// lookup, exponential delay computation, and insertion into the delayed slice.
func BenchmarkQueue_AddRateLimited(b *testing.B) {
	q := NewWorkQueueWithConfig(5*time.Millisecond, time.Second)
	defer q.ShutDown()

	reqs := make([]Request, b.N)
	for i := range reqs {
		reqs[i] = Request{Namespace: "default", Name: fmt.Sprintf("fail-%d", i)}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q.AddRateLimited(reqs[i])
	}
}

// BenchmarkQueue_CalculateBackoff isolates the exponential backoff loop, which
// iterates once per recorded failure and so is O(failures).
func BenchmarkQueue_CalculateBackoff(b *testing.B) {
	q := NewWorkQueueWithConfig(5*time.Millisecond, 1000*time.Second)
	defer q.ShutDown()

	for _, failures := range []int{1, 8, 18, 64} {
		b.Run(fmt.Sprintf("failures=%d", failures), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if q.calculateBackoff(failures) <= 0 {
					b.Fatal("non-positive backoff")
				}
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Work queue: concurrency / lock contention
// ----------------------------------------------------------------------------

// BenchmarkQueue_Parallel_AddGetDone measures the single-mutex work queue under
// GOMAXPROCS-way contention. Each goroutine Adds before it Gets, so the queue
// never starves and Get() never blocks on the condition variable.
//
// Compare ns/op against BenchmarkQueue_AddGetDone_RoundTrip: any increase is
// pure sync.Mutex contention, since the per-item work is identical.
func BenchmarkQueue_Parallel_AddGetDone(b *testing.B) {
	q := NewWorkQueue()
	defer q.ShutDown()

	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()

	var counter int64
	b.RunParallel(func(pb *testing.PB) {
		// Per-goroutine key space avoids cross-goroutine dedup collisions,
		// which would otherwise silently drop items and inflate throughput.
		id := atomicNextID(&counter)
		i := 0
		for pb.Next() {
			q.Add(Request{Namespace: "default", Name: fmt.Sprintf("p%d-%d", id, i)})
			req, shutdown := q.Get()
			if shutdown {
				return
			}
			q.Done(req)
			i++
		}
	})

	b.StopTimer()
	b.ReportMetric(float64(b.N)/time.Since(start).Seconds(), "items/sec")
}

// BenchmarkQueue_Parallel_AddOnly isolates producer-side contention: many
// goroutines Add concurrently with no consumer, so the mutex is the only
// serialization point.
func BenchmarkQueue_Parallel_AddOnly(b *testing.B) {
	q := NewWorkQueue()
	defer q.ShutDown()

	b.ReportAllocs()
	b.ResetTimer()

	var counter int64
	b.RunParallel(func(pb *testing.PB) {
		id := atomicNextID(&counter)
		i := 0
		for pb.Next() {
			q.Add(Request{Namespace: "default", Name: fmt.Sprintf("p%d-%d", id, i)})
			i++
		}
	})
}

// BenchmarkQueue_Parallel_Metrics measures reader/writer contention between
// hot-path Add calls and Metrics() polling (the /status endpoint pattern).
// Metrics() takes the same exclusive mutex as Add, so polling steals throughput.
func BenchmarkQueue_Parallel_Metrics(b *testing.B) {
	q := NewWorkQueue()
	defer q.ShutDown()

	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				_ = q.Metrics()
			}
		}
	}()
	defer close(stop)

	b.ReportAllocs()
	b.ResetTimer()

	var counter int64
	b.RunParallel(func(pb *testing.PB) {
		id := atomicNextID(&counter)
		i := 0
		for pb.Next() {
			q.Add(Request{Namespace: "default", Name: fmt.Sprintf("m%d-%d", id, i)})
			i++
		}
	})
}

// ----------------------------------------------------------------------------
// Reconcile loop
// ----------------------------------------------------------------------------

// BenchmarkReconcile_Direct_NoOp is the floor for reconcile cost: the interface
// dispatch alone, with no queue and no manager. Every other reconcile number
// should be read as "this plus orchestration overhead".
func BenchmarkReconcile_Direct_NoOp(b *testing.B) {
	var r Reconciler = &benchReconciler{name: "bench", kind: "Bench"}
	ctx := context.Background()
	req := Request{Namespace: "default", Name: "obj-1"}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Reconcile(ctx, req); err != nil {
			b.Fatalf("Reconcile failed at i=%d: %v", i, err)
		}
	}
}

// BenchmarkManager_Enqueue measures Manager.Enqueue: RWMutex read lock plus map
// lookup plus queue Add. This is the entry point used by event sources.
func BenchmarkManager_Enqueue(b *testing.B) {
	mgr := NewManager(ManagerConfig{Logger: newBenchLogger(), SyncPeriod: time.Hour})
	if err := mgr.RegisterReconciler(&benchReconciler{name: "bench", kind: "Bench"}); err != nil {
		b.Fatalf("RegisterReconciler failed: %v", err)
	}

	reqs := make([]Request, b.N)
	for i := range reqs {
		reqs[i] = Request{Namespace: "default", Name: fmt.Sprintf("obj-%d", i)}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := mgr.Enqueue("bench", reqs[i]); err != nil {
			b.Fatalf("Enqueue failed at i=%d: %v", i, err)
		}
	}
}

// benchmarkManagerE2E drives the full loop through a running Manager:
// Enqueue -> worker Get -> Reconcile -> Forget -> Done, and waits until the
// queue reports every item processed. workers sets MaxConcurrentReconciles.
//
// ns/op here is wall-clock per item including scheduler handoff, so it is not
// comparable to the lock-free direct-dispatch number.
func benchmarkManagerE2E(b *testing.B, workers int) {
	mgr := NewManager(ManagerConfig{
		Logger:                  newBenchLogger(),
		MaxConcurrentReconciles: workers,
		SyncPeriod:              time.Hour, // keep the resync loop out of the measurement
	})
	if err := mgr.RegisterReconciler(&benchReconciler{name: "bench", kind: "Bench"}); err != nil {
		b.Fatalf("RegisterReconciler failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := mgr.Start(ctx); err != nil {
			b.Errorf("Start failed: %v", err)
		}
	}()

	// Wait for workers to be running before timing.
	deadline := time.Now().Add(5 * time.Second)
	for !mgr.Healthy() {
		if time.Now().After(deadline) {
			cancel()
			b.Fatal("manager did not become healthy")
		}
		time.Sleep(time.Millisecond)
	}

	reqs := make([]Request, b.N)
	for i := range reqs {
		reqs[i] = Request{Namespace: "default", Name: fmt.Sprintf("obj-%d", i)}
	}

	b.ResetTimer()
	start := time.Now()

	for i := 0; i < b.N; i++ {
		if err := mgr.Enqueue("bench", reqs[i]); err != nil {
			b.Fatalf("Enqueue failed at i=%d: %v", i, err)
		}
	}

	// Drain: wait until every enqueued item has completed Done().
	drainDeadline := time.Now().Add(60 * time.Second)
	for {
		st := mgr.Status()
		if len(st.Controllers) > 0 && st.Controllers[0].TotalProcessed >= int64(b.N) {
			break
		}
		if time.Now().After(drainDeadline) {
			cancel()
			b.Fatalf("timeout draining queue: processed %d of %d",
				mgr.Status().Controllers[0].TotalProcessed, b.N)
		}
		time.Sleep(time.Millisecond)
	}

	b.StopTimer()
	b.ReportMetric(float64(b.N)/time.Since(start).Seconds(), "reconciles/sec")

	cancel()
	<-done
}

// BenchmarkManager_E2E_Reconcile sweeps worker counts to expose how much
// concurrency the single-mutex queue can actually convert into throughput.
func BenchmarkManager_E2E_Reconcile(b *testing.B) {
	for _, workers := range []int{1, 2, 4, 8} {
		b.Run(fmt.Sprintf("workers=%d", workers), func(b *testing.B) {
			benchmarkManagerE2E(b, workers)
		})
	}
}

// ----------------------------------------------------------------------------
// Status conditions — reconciler-side hot path
// ----------------------------------------------------------------------------

// BenchmarkStatus_SetCondition_Update measures the common case: a condition of
// this type already exists, so SetCondition rewrites it in place.
func BenchmarkStatus_SetCondition_Update(b *testing.B) {
	st := &ResourceStatus{}
	st.SetCondition(ReadyCondition("Initial", "seeded"))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		st.SetCondition(ReadyCondition("Reconciled", "converged"))
	}
	b.StopTimer()

	if len(st.Conditions) != 1 {
		b.Fatalf("conditions = %d, want 1 (in-place update expected)", len(st.Conditions))
	}
}

// BenchmarkStatus_SetCondition_Flapping alternates status values, forcing the
// LastTransitionTime branch (time.Now) on every call.
func BenchmarkStatus_SetCondition_Flapping(b *testing.B) {
	st := &ResourceStatus{}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%2 == 0 {
			st.SetCondition(ReadyCondition("Up", "healthy"))
		} else {
			st.SetCondition(NotReadyCondition("Down", "degraded"))
		}
	}
}

// BenchmarkStatus_GetCondition measures the linear scan over the condition
// slice; realistic resources carry a handful of condition types.
func BenchmarkStatus_GetCondition(b *testing.B) {
	st := &ResourceStatus{}
	st.SetCondition(ReconcilingCondition("Working", "in progress"))
	st.SetCondition(ErrorCondition("Transient", "retrying"))
	st.SetCondition(Condition{Type: ConditionDegraded, Status: ConditionFalse})
	st.SetCondition(Condition{Type: ConditionProgressing, Status: ConditionTrue})
	st.SetCondition(ReadyCondition("Converged", "done")) // worst case: last

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if st.GetCondition(ConditionReady) == nil {
			b.Fatal("Ready condition not found")
		}
	}
}

// ----------------------------------------------------------------------------
// Event recorder — observability path
// ----------------------------------------------------------------------------

// BenchmarkManager_RecordEvent measures event append including the ring-buffer
// eviction that fires once the 1000-event cap is reached.
func BenchmarkManager_RecordEvent(b *testing.B) {
	mgr := NewManager(ManagerConfig{Logger: newBenchLogger(), SyncPeriod: time.Hour})
	evt := Event{
		Type: EventNormal, Reason: "Reconciled", Message: "converged",
		Object: Request{Namespace: "default", Name: "obj-1"}, Controller: "bench",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		evt.Timestamp = time.Now().UTC()
		mgr.RecordEvent(evt)
	}
}

// BenchmarkManager_GetEvents_100 measures the read path, which copies the
// requested window out under a read lock.
func BenchmarkManager_GetEvents_100(b *testing.B) {
	mgr := NewManager(ManagerConfig{Logger: newBenchLogger(), SyncPeriod: time.Hour})
	for i := 0; i < 1000; i++ {
		mgr.RecordEvent(Event{
			Type: EventNormal, Reason: "Reconciled", Message: fmt.Sprintf("evt-%d", i),
			Timestamp: time.Now().UTC(), Controller: "bench",
		})
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if len(mgr.GetEvents(100)) != 100 {
			b.Fatal("expected 100 events")
		}
	}
}

// BenchmarkManager_Status measures the aggregation served by the status
// endpoint: it walks every controller and snapshots each queue's metrics.
func BenchmarkManager_Status(b *testing.B) {
	mgr := NewManager(ManagerConfig{Logger: newBenchLogger(), SyncPeriod: time.Hour})
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("bench-%d", i)
		if err := mgr.RegisterReconciler(&benchReconciler{name: name, kind: "Bench"}); err != nil {
			b.Fatalf("RegisterReconciler failed: %v", err)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if len(mgr.Status().Controllers) != 5 {
			b.Fatal("expected 5 controllers")
		}
	}
}
