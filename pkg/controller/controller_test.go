package controller

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/cluster"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/security"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/store"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/workload"
)

// ============================================================================
// Test helpers — lightweight mocks for internal tests
// ============================================================================

type fakeClusterService struct {
	clusters map[string]*cluster.Cluster
	health   map[string]*cluster.ClusterHealth
	err      error
}

func (f *fakeClusterService) ListClusters(ctx context.Context) ([]*cluster.Cluster, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]*cluster.Cluster, 0, len(f.clusters))
	for _, c := range f.clusters {
		out = append(out, c)
	}
	return out, nil
}

func (f *fakeClusterService) GetCluster(ctx context.Context, id string) (*cluster.Cluster, error) {
	if f.err != nil {
		return nil, f.err
	}
	c, ok := f.clusters[id]
	if !ok {
		return nil, fmt.Errorf("cluster %s not found", id)
	}
	return c, nil
}

func (f *fakeClusterService) ImportCluster(ctx context.Context, req *cluster.ImportClusterRequest) (*cluster.Cluster, error) {
	return nil, nil
}
func (f *fakeClusterService) DeleteCluster(ctx context.Context, id string) error { return nil }
func (f *fakeClusterService) GetClusterHealth(ctx context.Context, id string) (*cluster.ClusterHealth, error) {
	if f.err != nil {
		return nil, f.err
	}
	h, ok := f.health[id]
	if !ok {
		return &cluster.ClusterHealth{Status: "unknown"}, nil
	}
	return h, nil
}
func (f *fakeClusterService) GetClusterNodes(ctx context.Context, id string) ([]*cluster.Node, error) {
	return nil, nil
}
func (f *fakeClusterService) GetGPUTopology(ctx context.Context, id string) ([]*common.GPUTopologyInfo, error) {
	return nil, nil
}
func (f *fakeClusterService) GetResourceSummary(ctx context.Context) (*common.ResourceCapacity, error) {
	return nil, nil
}
func (f *fakeClusterService) StartHealthCheckLoop(ctx context.Context, interval time.Duration) {}

type fakeWorkloadService struct {
	workloads map[string]*workload.WorkloadResponse
	updated   map[string]string // id -> new status
	mu        sync.Mutex
}

func (f *fakeWorkloadService) Create(ctx context.Context, req *workload.CreateWorkloadRequest) (*workload.WorkloadResponse, error) {
	return nil, nil
}
func (f *fakeWorkloadService) List(ctx context.Context, cid, status string, page, ps int) ([]workload.WorkloadResponse, int64, error) {
	out := make([]workload.WorkloadResponse, 0)
	for _, w := range f.workloads {
		out = append(out, *w)
	}
	return out, int64(len(out)), nil
}
func (f *fakeWorkloadService) Get(ctx context.Context, id string) (*workload.WorkloadResponse, error) {
	w, ok := f.workloads[id]
	if !ok {
		return nil, fmt.Errorf("workload %s not found", id)
	}
	return w, nil
}
func (f *fakeWorkloadService) Delete(ctx context.Context, id string) error { return nil }
func (f *fakeWorkloadService) UpdateStatus(ctx context.Context, id string, u *workload.WorkloadStatusUpdate) (*workload.WorkloadResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updated == nil {
		f.updated = make(map[string]string)
	}
	f.updated[id] = u.Status
	w, ok := f.workloads[id]
	if !ok {
		return &workload.WorkloadResponse{ID: id, Status: u.Status}, nil
	}
	w.Status = u.Status
	return w, nil
}
func (f *fakeWorkloadService) GetEvents(ctx context.Context, wid string, limit int) ([]store.WorkloadEvent, error) {
	return nil, nil
}

type fakeSecurityService struct {
	policies []*security.SecurityPolicy
	auditLog []*security.AuditLogEntry
	mu       sync.Mutex
}

func (f *fakeSecurityService) ListPolicies(ctx context.Context) ([]*security.SecurityPolicy, error) {
	return f.policies, nil
}
func (f *fakeSecurityService) CreatePolicy(ctx context.Context, p *security.SecurityPolicy) error {
	return nil
}
func (f *fakeSecurityService) GetPolicy(ctx context.Context, id string) (*security.SecurityPolicy, error) {
	for _, p := range f.policies {
		if p.ID == id || p.Name == id {
			return p, nil
		}
	}
	return nil, fmt.Errorf("policy %s not found", id)
}
func (f *fakeSecurityService) RunVulnerabilityScan(ctx context.Context, cid, st string) (*security.VulnerabilityScan, error) {
	return nil, nil
}
func (f *fakeSecurityService) GetComplianceReport(ctx context.Context, cid, fw string) (*security.ComplianceReport, error) {
	return nil, nil
}
func (f *fakeSecurityService) GetAuditLogs(ctx context.Context, limit int) ([]*security.AuditLogEntry, error) {
	return f.auditLog, nil
}
func (f *fakeSecurityService) GetThreats(ctx context.Context) ([]*security.ThreatEvent, error) {
	return nil, nil
}
func (f *fakeSecurityService) RecordAuditLog(entry *security.AuditLogEntry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auditLog = append(f.auditLog, entry)
}

// fakeReconciler is a simple Reconciler for testing the Manager.
type fakeReconciler struct {
	name      string
	kind      string
	calls     []Request
	mu        sync.Mutex
	result    Result
	err       error
	reconcile func(ctx context.Context, req Request) (Result, error)
}

func (f *fakeReconciler) Reconcile(ctx context.Context, req Request) (Result, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	f.mu.Unlock()
	if f.reconcile != nil {
		return f.reconcile(ctx, req)
	}
	return f.result, f.err
}
func (f *fakeReconciler) Name() string         { return f.name }
func (f *fakeReconciler) ResourceKind() string  { return f.kind }
func (f *fakeReconciler) getCalls() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Request, len(f.calls))
	copy(out, f.calls)
	return out
}

// ============================================================================
// Types tests
// ============================================================================

func TestRequest_String(t *testing.T) {
	tests := []struct {
		name string
		req  Request
		want string
	}{
		{"name only", Request{Name: "foo"}, "foo"},
		{"with namespace", Request{Namespace: "ns", Name: "foo"}, "ns/foo"},
		{"with id", Request{Name: "foo", ID: "abc"}, "foo (id=abc)"},
		{"namespace takes priority", Request{Namespace: "ns", Name: "foo", ID: "abc"}, "ns/foo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.req.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRequest_NamespacedName(t *testing.T) {
	if nn := (Request{Namespace: "ns", Name: "foo"}).NamespacedName(); nn != "ns/foo" {
		t.Errorf("NamespacedName() = %q, want ns/foo", nn)
	}
	if nn := (Request{Name: "bar"}).NamespacedName(); nn != "bar" {
		t.Errorf("NamespacedName() = %q, want bar", nn)
	}
}

func TestResult_IsZero(t *testing.T) {
	if !(Result{}).IsZero() {
		t.Error("empty result should be zero")
	}
	if (Result{Requeue: true}).IsZero() {
		t.Error("requeue result should not be zero")
	}
	if (Result{RequeueAfter: time.Second}).IsZero() {
		t.Error("requeue-after result should not be zero")
	}
}

func TestResourceStatus_SetCondition(t *testing.T) {
	s := &ResourceStatus{}

	// Add first condition
	s.SetCondition(ReadyCondition("Test", "ready"))
	if len(s.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(s.Conditions))
	}
	if s.Conditions[0].Type != ConditionReady || s.Conditions[0].Status != ConditionTrue {
		t.Errorf("unexpected condition: %+v", s.Conditions[0])
	}

	// Update same type
	s.SetCondition(NotReadyCondition("Broken", "failed"))
	if len(s.Conditions) != 1 {
		t.Fatalf("expected 1 condition after update, got %d", len(s.Conditions))
	}
	if s.Conditions[0].Status != ConditionFalse {
		t.Errorf("expected False, got %s", s.Conditions[0].Status)
	}

	// Add different type
	s.SetCondition(ReconcilingCondition("Working", "doing stuff"))
	if len(s.Conditions) != 2 {
		t.Fatalf("expected 2 conditions, got %d", len(s.Conditions))
	}
}

func TestResourceStatus_GetCondition(t *testing.T) {
	s := &ResourceStatus{}
	s.SetCondition(ReadyCondition("Ok", "good"))

	c := s.GetCondition(ConditionReady)
	if c == nil {
		t.Fatal("expected Ready condition")
	}
	if c.Reason != "Ok" {
		t.Errorf("expected reason Ok, got %s", c.Reason)
	}

	if s.GetCondition(ConditionError) != nil {
		t.Error("should not find Error condition")
	}
}

func TestResourceStatus_IsReady(t *testing.T) {
	s := &ResourceStatus{}
	if s.IsReady() {
		t.Error("should not be ready with no conditions")
	}
	s.SetCondition(ReadyCondition("Ok", "good"))
	if !s.IsReady() {
		t.Error("should be ready")
	}
	s.SetCondition(NotReadyCondition("Broken", "bad"))
	if s.IsReady() {
		t.Error("should not be ready after NotReady")
	}
}

func TestObjectMeta_HasFinalizer(t *testing.T) {
	m := ObjectMeta{Finalizers: []string{"cleanup.cloudai.io", "protect.cloudai.io"}}
	if !m.HasFinalizer("cleanup.cloudai.io") {
		t.Error("should have cleanup finalizer")
	}
	if m.HasFinalizer("nonexistent") {
		t.Error("should not have nonexistent finalizer")
	}
}

func TestConditionBuilders(t *testing.T) {
	c := ReadyCondition("R", "msg")
	if c.Type != ConditionReady || c.Status != ConditionTrue {
		t.Errorf("ReadyCondition wrong: %+v", c)
	}

	c = NotReadyCondition("NR", "msg")
	if c.Type != ConditionReady || c.Status != ConditionFalse {
		t.Errorf("NotReadyCondition wrong: %+v", c)
	}

	c = ReconcilingCondition("RC", "msg")
	if c.Type != ConditionReconciling || c.Status != ConditionTrue {
		t.Errorf("ReconcilingCondition wrong: %+v", c)
	}

	c = ErrorCondition("E", "msg")
	if c.Type != ConditionError || c.Status != ConditionTrue {
		t.Errorf("ErrorCondition wrong: %+v", c)
	}
}

// ============================================================================
// WorkQueue tests
// ============================================================================

func TestWorkQueue_AddAndGet(t *testing.T) {
	q := NewWorkQueue()
	defer q.ShutDown()

	req := Request{Name: "cluster-1", ID: "c1"}
	q.Add(req)

	if q.Len() != 1 {
		t.Fatalf("expected len 1, got %d", q.Len())
	}

	got, shutdown := q.Get()
	if shutdown {
		t.Fatal("unexpected shutdown")
	}
	if got.Name != "cluster-1" {
		t.Errorf("expected cluster-1, got %s", got.Name)
	}
	q.Done(got)
}

func TestWorkQueue_Dedup(t *testing.T) {
	q := NewWorkQueue()
	defer q.ShutDown()

	req := Request{Name: "dup-item"}
	q.Add(req)
	q.Add(req)
	q.Add(req)

	if q.Len() != 1 {
		t.Errorf("expected len 1 after dedup, got %d", q.Len())
	}

	metrics := q.Metrics()
	if metrics.TotalAdded != 1 {
		t.Errorf("TotalAdded should be 1 (deduped), got %d", metrics.TotalAdded)
	}
}

func TestWorkQueue_ReaddWhileProcessing(t *testing.T) {
	q := NewWorkQueue()
	defer q.ShutDown()

	req := Request{Name: "item-x"}
	q.Add(req)

	got, _ := q.Get()
	// While processing, add the same item
	q.Add(got)

	// Complete processing — item should be re-queued
	q.Done(got)

	if q.Len() != 1 {
		t.Errorf("expected item re-queued, len=%d", q.Len())
	}
}

func TestWorkQueue_Shutdown(t *testing.T) {
	q := NewWorkQueue()

	q.Add(Request{Name: "a"})
	q.ShutDown()

	if !q.ShuttingDown() {
		t.Error("should be shutting down")
	}

	// Get should still return existing items
	got, shutdown := q.Get()
	if shutdown {
		t.Error("should get existing item before reporting shutdown")
	}
	q.Done(got)

	// Now queue is empty + shutdown — should return true
	_, shutdown = q.Get()
	if !shutdown {
		t.Error("expected shutdown=true on empty queue")
	}
}

func TestWorkQueue_RateLimited(t *testing.T) {
	q := NewWorkQueueWithConfig(time.Millisecond, 100*time.Millisecond)
	defer q.ShutDown()

	req := Request{Name: "retry-me"}

	// First call should add with baseDelay (1ms)
	q.AddRateLimited(req)
	if q.NumRequeues(req) != 1 {
		t.Errorf("expected 1 requeue, got %d", q.NumRequeues(req))
	}

	// Second call should increase backoff
	q.AddRateLimited(req)
	if q.NumRequeues(req) != 2 {
		t.Errorf("expected 2 requeues, got %d", q.NumRequeues(req))
	}

	// Forget resets counter
	q.Forget(req)
	if q.NumRequeues(req) != 0 {
		t.Errorf("expected 0 after Forget, got %d", q.NumRequeues(req))
	}
}

func TestWorkQueue_CalculateBackoff(t *testing.T) {
	q := &WorkQueue{baseDelay: 10 * time.Millisecond, maxDelay: 1 * time.Second}

	tests := []struct {
		failures int
		min      time.Duration
		max      time.Duration
	}{
		{0, 10 * time.Millisecond, 10 * time.Millisecond},
		{1, 10 * time.Millisecond, 10 * time.Millisecond},
		{2, 20 * time.Millisecond, 20 * time.Millisecond},
		{3, 40 * time.Millisecond, 40 * time.Millisecond},
		{10, 1 * time.Second, 1 * time.Second}, // capped at max
		{20, 1 * time.Second, 1 * time.Second}, // still capped
	}
	for _, tt := range tests {
		got := q.calculateBackoff(tt.failures)
		if got < tt.min || got > tt.max {
			t.Errorf("backoff(%d)=%v, want [%v, %v]", tt.failures, got, tt.min, tt.max)
		}
	}
}

func TestWorkQueue_Metrics(t *testing.T) {
	q := NewWorkQueue()
	defer q.ShutDown()

	q.Add(Request{Name: "a"})
	q.Add(Request{Name: "b"})

	m := q.Metrics()
	if m.TotalAdded != 2 {
		t.Errorf("TotalAdded=%d, want 2", m.TotalAdded)
	}
	if m.CurrentDepth != 2 {
		t.Errorf("CurrentDepth=%d, want 2", m.CurrentDepth)
	}

	got, _ := q.Get()
	m = q.Metrics()
	if m.InFlight != 1 {
		t.Errorf("InFlight=%d, want 1", m.InFlight)
	}
	q.Done(got)

	m = q.Metrics()
	if m.TotalProcessed != 1 {
		t.Errorf("TotalProcessed=%d, want 1", m.TotalProcessed)
	}
}

func TestWorkQueue_AddAfterZeroDelay(t *testing.T) {
	q := NewWorkQueue()
	defer q.ShutDown()

	q.AddAfter(Request{Name: "instant"}, 0)
	if q.Len() != 1 {
		t.Errorf("AddAfter(0) should add immediately, len=%d", q.Len())
	}
}

func TestWorkQueue_ConcurrentAccess(t *testing.T) {
	q := NewWorkQueue()
	defer q.ShutDown()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			q.Add(Request{Name: fmt.Sprintf("item-%d", n)})
		}(i)
	}
	wg.Wait()

	// Drain all
	count := 0
	for q.Len() > 0 {
		got, _ := q.Get()
		q.Done(got)
		count++
	}
	if count != 50 {
		t.Errorf("expected 50 items, got %d", count)
	}
}

// ============================================================================
// Controller Manager tests
// ============================================================================

func newTestLogger() *logrus.Logger {
	l := logrus.New()
	l.SetLevel(logrus.WarnLevel) // suppress debug/info in tests
	return l
}

func TestManager_RegisterAndStart(t *testing.T) {
	mgr := NewManager(ManagerConfig{
		Logger:                  newTestLogger(),
		MaxConcurrentReconciles: 1,
		SyncPeriod:              1 * time.Hour, // long period to avoid resync during test
	})

	fr := &fakeReconciler{name: "test-ctrl", kind: "TestResource"}
	if err := mgr.RegisterReconciler(fr); err != nil {
		t.Fatalf("RegisterReconciler: %v", err)
	}

	// Duplicate registration should fail
	if err := mgr.RegisterReconciler(fr); err == nil {
		t.Error("expected error for duplicate registration")
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Start in background
	done := make(chan error, 1)
	go func() { done <- mgr.Start(ctx) }()

	time.Sleep(50 * time.Millisecond)

	if !mgr.Healthy() {
		t.Error("manager should be healthy")
	}

	// Enqueue a request
	if err := mgr.Enqueue("test-ctrl", Request{Name: "obj-1", ID: "id-1"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	calls := fr.getCalls()
	if len(calls) == 0 {
		t.Error("expected reconciler to be called")
	}

	// Enqueue to nonexistent controller
	if err := mgr.Enqueue("nonexistent", Request{Name: "x"}); err == nil {
		t.Error("expected error for unknown controller")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Start returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("manager did not stop in time")
	}
}

func TestManager_DoubleStart(t *testing.T) {
	mgr := NewManager(ManagerConfig{Logger: newTestLogger(), SyncPeriod: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go mgr.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	if err := mgr.Start(ctx); err == nil {
		t.Error("expected error on double Start()")
	}
}

func TestManager_RegisterAfterStart(t *testing.T) {
	mgr := NewManager(ManagerConfig{Logger: newTestLogger(), SyncPeriod: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go mgr.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	fr := &fakeReconciler{name: "late-ctrl", kind: "LateResource"}
	if err := mgr.RegisterReconciler(fr); err == nil {
		t.Error("expected error registering after Start()")
	}
}

func TestManager_RequeueAfter(t *testing.T) {
	mgr := NewManager(ManagerConfig{
		Logger:     newTestLogger(),
		SyncPeriod: time.Hour,
	})

	callCount := 0
	fr := &fakeReconciler{
		name: "requeue-ctrl",
		kind: "Requeue",
		reconcile: func(ctx context.Context, req Request) (Result, error) {
			callCount++
			if callCount == 1 {
				return Result{RequeueAfter: 50 * time.Millisecond}, nil
			}
			return Result{}, nil
		},
	}
	mgr.RegisterReconciler(fr)

	ctx, cancel := context.WithCancel(context.Background())
	go mgr.Start(ctx)

	time.Sleep(30 * time.Millisecond)
	mgr.Enqueue("requeue-ctrl", Request{Name: "requeue-obj"})

	// Wait for initial + delayed requeue
	time.Sleep(800 * time.Millisecond)
	cancel()
	time.Sleep(100 * time.Millisecond)

	calls := fr.getCalls()
	if len(calls) < 2 {
		t.Errorf("expected at least 2 calls (initial + requeue), got %d", len(calls))
	}
}

func TestManager_ErrorBackoff(t *testing.T) {
	mgr := NewManager(ManagerConfig{
		Logger:     newTestLogger(),
		SyncPeriod: time.Hour,
	})

	callCount := 0
	fr := &fakeReconciler{
		name: "error-ctrl",
		kind: "ErrorResource",
		reconcile: func(ctx context.Context, req Request) (Result, error) {
			callCount++
			if callCount <= 2 {
				return Result{}, fmt.Errorf("transient error #%d", callCount)
			}
			return Result{}, nil
		},
	}
	mgr.RegisterReconciler(fr)

	ctx, cancel := context.WithCancel(context.Background())
	go mgr.Start(ctx)

	time.Sleep(30 * time.Millisecond)
	mgr.Enqueue("error-ctrl", Request{Name: "error-obj"})

	// Give time for retries with exponential backoff
	time.Sleep(2 * time.Second)
	cancel()
	time.Sleep(100 * time.Millisecond)

	calls := fr.getCalls()
	if len(calls) < 2 {
		t.Errorf("expected at least 2 calls from backoff retry, got %d", len(calls))
	}

	// Check events were recorded
	events := mgr.GetEvents(10)
	foundError := false
	for _, e := range events {
		if e.Reason == "ReconcileError" {
			foundError = true
		}
	}
	if !foundError {
		t.Error("expected ReconcileError event to be recorded")
	}
}

func TestManager_Status(t *testing.T) {
	mgr := NewManager(ManagerConfig{Logger: newTestLogger(), SyncPeriod: time.Hour})
	fr := &fakeReconciler{name: "status-ctrl", kind: "StatusRes"}
	mgr.RegisterReconciler(fr)

	ctx, cancel := context.WithCancel(context.Background())
	go mgr.Start(ctx)
	time.Sleep(50 * time.Millisecond)

	status := mgr.Status()
	if !status.Started {
		t.Error("expected started=true")
	}
	if !status.Healthy {
		t.Error("expected healthy=true")
	}
	if len(status.Controllers) != 1 {
		t.Errorf("expected 1 controller, got %d", len(status.Controllers))
	}
	if status.Controllers[0].Name != "status-ctrl" {
		t.Errorf("expected status-ctrl, got %s", status.Controllers[0].Name)
	}

	cancel()
	time.Sleep(100 * time.Millisecond)
}

func TestManager_Events(t *testing.T) {
	mgr := NewManager(ManagerConfig{Logger: newTestLogger(), SyncPeriod: time.Hour})

	mgr.RecordEvent(Event{Type: EventNormal, Reason: "TestEvent", Message: "hello"})
	mgr.RecordEvent(Event{Type: EventWarning, Reason: "TestWarn", Message: "oops"})

	events := mgr.GetEvents(10)
	if len(events) != 2 {
		t.Errorf("expected 2 events, got %d", len(events))
	}

	// Test limit
	events = mgr.GetEvents(1)
	if len(events) != 1 {
		t.Errorf("expected 1 event with limit, got %d", len(events))
	}
}

func TestManager_Stop(t *testing.T) {
	mgr := NewManager(ManagerConfig{Logger: newTestLogger(), SyncPeriod: time.Hour})
	fr := &fakeReconciler{name: "stop-ctrl", kind: "StopRes"}
	mgr.RegisterReconciler(fr)

	ctx := context.Background()
	done := make(chan error, 1)
	go func() { done <- mgr.Start(ctx) }()

	time.Sleep(50 * time.Millisecond)
	mgr.Stop()

	select {
	case <-done:
		// ok
	case <-time.After(5 * time.Second):
		t.Fatal("manager did not stop after Stop()")
	}
}

// ============================================================================
// ClusterReconciler tests
// ============================================================================

func TestClusterReconciler_HealthyCluster(t *testing.T) {
	svc := &fakeClusterService{
		clusters: map[string]*cluster.Cluster{
			"c1": {ID: "c1", Name: "prod", Status: common.ClusterStatusHealthy, NodeCount: 3},
		},
		health: map[string]*cluster.ClusterHealth{
			"c1": {Status: "healthy", APIServerHealthy: true, ETCDHealthy: true, NodeTotalCount: 3, NodeReadyCount: 3},
		},
	}

	r := NewClusterReconciler(ClusterReconcilerConfig{
		ClusterService: svc,
		Logger:         newTestLogger(),
	})

	if r.Name() != "cluster-controller" {
		t.Errorf("Name()=%s", r.Name())
	}
	if r.ResourceKind() != "Cluster" {
		t.Errorf("ResourceKind()=%s", r.ResourceKind())
	}

	result, err := r.Reconcile(context.Background(), Request{Name: "c1", ID: "c1"})
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	if result.RequeueAfter != 2*time.Minute {
		t.Errorf("expected 2min requeue, got %v", result.RequeueAfter)
	}

	// Check status
	status := r.GetStatus("c1")
	if status == nil {
		t.Fatal("expected status for c1")
	}
	if status.Phase != "Ready" {
		t.Errorf("expected phase Ready, got %s", status.Phase)
	}
	if status.DriftDetected {
		t.Error("should not detect drift for healthy cluster")
	}
}

func TestClusterReconciler_DriftDetected(t *testing.T) {
	svc := &fakeClusterService{
		clusters: map[string]*cluster.Cluster{
			"c2": {ID: "c2", Name: "staging", Status: common.ClusterStatusHealthy, NodeCount: 5},
		},
		health: map[string]*cluster.ClusterHealth{
			"c2": {Status: "degraded", APIServerHealthy: true, ETCDHealthy: false, NodeTotalCount: 3, NodeReadyCount: 2},
		},
	}

	r := NewClusterReconciler(ClusterReconcilerConfig{
		ClusterService: svc,
		Logger:         newTestLogger(),
	})

	result, err := r.Reconcile(context.Background(), Request{Name: "c2", ID: "c2"})
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	// After drift + convergence, should requeue for verification
	if result.RequeueAfter != 15*time.Second {
		t.Errorf("expected 15s requeue for convergence, got %v", result.RequeueAfter)
	}

	status := r.GetStatus("c2")
	if status == nil {
		t.Fatal("expected status")
	}
	if !status.DriftDetected {
		t.Error("should detect drift")
	}
}

func TestClusterReconciler_ClusterNotFound(t *testing.T) {
	svc := &fakeClusterService{
		clusters: map[string]*cluster.Cluster{},
		health:   map[string]*cluster.ClusterHealth{},
	}

	r := NewClusterReconciler(ClusterReconcilerConfig{
		ClusterService: svc,
		Logger:         newTestLogger(),
	})

	result, err := r.Reconcile(context.Background(), Request{Name: "gone", ID: "gone"})
	if err != nil {
		t.Fatalf("should not error for not-found: %v", err)
	}
	if !result.IsZero() {
		t.Error("should not requeue for deleted cluster")
	}
}

func TestClusterReconciler_HealthCheckFails(t *testing.T) {
	svc := &fakeClusterService{
		clusters: map[string]*cluster.Cluster{
			"c3": {ID: "c3", Name: "broken"},
		},
		health: map[string]*cluster.ClusterHealth{},
		err:    nil,
	}
	// Override to return error for health check only
	r := NewClusterReconciler(ClusterReconcilerConfig{
		ClusterService: &healthErrorClusterService{fakeClusterService: svc},
		Logger:         newTestLogger(),
	})

	result, err := r.Reconcile(context.Background(), Request{ID: "c3"})
	if err != nil {
		t.Fatalf("should not return error: %v", err)
	}
	if result.RequeueAfter != 30*time.Second {
		t.Errorf("expected 30s requeue on health failure, got %v", result.RequeueAfter)
	}
}

type healthErrorClusterService struct {
	*fakeClusterService
}

func (h *healthErrorClusterService) GetClusterHealth(ctx context.Context, id string) (*cluster.ClusterHealth, error) {
	return nil, fmt.Errorf("connection refused")
}

func TestClusterReconciler_Resync(t *testing.T) {
	svc := &fakeClusterService{
		clusters: map[string]*cluster.Cluster{
			"c1": {ID: "c1", Name: "cl-1"},
			"c2": {ID: "c2", Name: "cl-2"},
		},
		health: map[string]*cluster.ClusterHealth{},
	}

	mgr := NewManager(ManagerConfig{Logger: newTestLogger(), SyncPeriod: time.Hour})
	r := NewClusterReconciler(ClusterReconcilerConfig{
		ClusterService: svc,
		Manager:        mgr,
		Logger:         newTestLogger(),
	})
	mgr.RegisterReconciler(r)

	result, err := r.Reconcile(context.Background(), Request{Name: "__resync__"})
	if err != nil {
		t.Fatalf("resync error: %v", err)
	}
	if !result.IsZero() {
		t.Error("resync should return zero result")
	}
}

// ============================================================================
// WorkloadReconciler tests
// ============================================================================

func TestWorkloadReconciler_PendingWorkload(t *testing.T) {
	svc := &fakeWorkloadService{
		workloads: map[string]*workload.WorkloadResponse{
			"w1": {ID: "w1", Name: "train-job", Status: "pending", Image: "pytorch:latest", ClusterID: "c1", CreatedAt: time.Now()},
		},
	}

	r := NewWorkloadReconciler(WorkloadReconcilerConfig{
		WorkloadService: svc,
		Logger:          newTestLogger(),
	})

	if r.Name() != "workload-controller" {
		t.Errorf("Name()=%s", r.Name())
	}

	result, err := r.Reconcile(context.Background(), Request{Name: "w1", ID: "w1"})
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	if result.RequeueAfter != 5*time.Second {
		t.Errorf("expected 5s requeue after pending→queued, got %v", result.RequeueAfter)
	}

	// Check it transitioned to queued
	svc.mu.Lock()
	if svc.updated["w1"] != "queued" {
		t.Errorf("expected status update to queued, got %s", svc.updated["w1"])
	}
	svc.mu.Unlock()
}

func TestWorkloadReconciler_RunningWorkload(t *testing.T) {
	now := time.Now().Add(-10 * time.Minute)
	svc := &fakeWorkloadService{
		workloads: map[string]*workload.WorkloadResponse{
			"w2": {ID: "w2", Name: "inference", Status: "running", Image: "tf:2", ClusterID: "c1", AssignedNode: "node-1", StartedAt: &now},
		},
	}

	r := NewWorkloadReconciler(WorkloadReconcilerConfig{
		WorkloadService: svc,
		Logger:          newTestLogger(),
	})

	result, err := r.Reconcile(context.Background(), Request{ID: "w2"})
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	if result.RequeueAfter != 30*time.Second {
		t.Errorf("expected 30s monitoring interval, got %v", result.RequeueAfter)
	}

	status := r.GetStatus("w2")
	if status == nil || status.Phase != "Running" {
		t.Errorf("expected Running phase, got %v", status)
	}
}

func TestWorkloadReconciler_TerminalWorkload(t *testing.T) {
	svc := &fakeWorkloadService{
		workloads: map[string]*workload.WorkloadResponse{
			"w3": {ID: "w3", Name: "done-job", Status: "succeeded"},
		},
	}

	r := NewWorkloadReconciler(WorkloadReconcilerConfig{
		WorkloadService: svc,
		Logger:          newTestLogger(),
	})

	result, err := r.Reconcile(context.Background(), Request{ID: "w3"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !result.IsZero() {
		t.Error("terminal workload should not requeue")
	}
}

func TestWorkloadReconciler_NotFound(t *testing.T) {
	svc := &fakeWorkloadService{workloads: map[string]*workload.WorkloadResponse{}}

	r := NewWorkloadReconciler(WorkloadReconcilerConfig{
		WorkloadService: svc,
		Logger:          newTestLogger(),
	})

	result, err := r.Reconcile(context.Background(), Request{ID: "gone"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !result.IsZero() {
		t.Error("not found should not requeue")
	}
}

func TestWorkloadReconciler_PreemptedWorkload(t *testing.T) {
	svc := &fakeWorkloadService{
		workloads: map[string]*workload.WorkloadResponse{
			"wp": {ID: "wp", Name: "preempted-job", Status: "preempted", Image: "img:1", ClusterID: "c1"},
		},
	}

	r := NewWorkloadReconciler(WorkloadReconcilerConfig{
		WorkloadService: svc,
		Logger:          newTestLogger(),
	})

	result, err := r.Reconcile(context.Background(), Request{ID: "wp"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.RequeueAfter != 5*time.Second {
		t.Errorf("expected 5s requeue after re-queuing preempted, got %v", result.RequeueAfter)
	}

	svc.mu.Lock()
	if svc.updated["wp"] != "queued" {
		t.Errorf("expected status update to queued, got %s", svc.updated["wp"])
	}
	svc.mu.Unlock()
}

func TestWorkloadReconciler_ValidationFailure(t *testing.T) {
	svc := &fakeWorkloadService{
		workloads: map[string]*workload.WorkloadResponse{
			"wv": {ID: "wv", Name: "bad-job", Status: "pending", Image: "", ClusterID: ""},
		},
	}

	r := NewWorkloadReconciler(WorkloadReconcilerConfig{
		WorkloadService: svc,
		Logger:          newTestLogger(),
	})

	result, err := r.Reconcile(context.Background(), Request{ID: "wv"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// Validation failure → 5min requeue
	if result.RequeueAfter != 5*time.Minute {
		t.Errorf("expected 5min requeue on validation failure, got %v", result.RequeueAfter)
	}
}

// ============================================================================
// SecurityPolicyReconciler tests
// ============================================================================

func TestSecurityPolicyReconciler_NewPolicyEnforced(t *testing.T) {
	svc := &fakeSecurityService{
		policies: []*security.SecurityPolicy{
			{
				ID: "sp1", Name: "restrict-network", Type: security.PolicyTypeNetwork,
				Enforcement: security.EnforcementEnforce, Status: "active",
				ClusterIDs: []string{"c1", "c2"},
			},
		},
	}

	r := NewSecurityPolicyReconciler(SecurityPolicyReconcilerConfig{
		SecurityService: svc,
		Logger:          newTestLogger(),
	})

	if r.Name() != "security-policy-controller" {
		t.Errorf("Name()=%s", r.Name())
	}
	if r.ResourceKind() != "SecurityPolicy" {
		t.Errorf("ResourceKind()=%s", r.ResourceKind())
	}

	// First reconcile: policy not yet applied → drift → enforce
	result, err := r.Reconcile(context.Background(), Request{Name: "sp1", ID: "sp1"})
	if err != nil {
		t.Fatalf("Reconcile error: %v", err)
	}
	// Should requeue after enforcement for verification
	if result.RequeueAfter != 15*time.Second {
		t.Errorf("expected 15s requeue after enforcement, got %v", result.RequeueAfter)
	}

	// Second reconcile: policy is now applied and recent → no drift → compliance check → OK
	result, err = r.Reconcile(context.Background(), Request{Name: "sp1", ID: "sp1"})
	if err != nil {
		t.Fatalf("Second reconcile error: %v", err)
	}
	if result.RequeueAfter != 5*time.Minute {
		t.Errorf("expected 5min standard interval, got %v", result.RequeueAfter)
	}

	status := r.GetStatus("sp1")
	if status == nil || status.Phase != "Enforced" {
		t.Errorf("expected Enforced phase, got %v", status)
	}

	info := r.GetEnforcementInfo("sp1")
	if info == nil || !info.Applied {
		t.Error("expected enforcement to be applied")
	}
	if len(info.EnforcedClusters) != 2 {
		t.Errorf("expected 2 enforced clusters, got %d", len(info.EnforcedClusters))
	}
}

func TestSecurityPolicyReconciler_PolicyNotFound(t *testing.T) {
	svc := &fakeSecurityService{policies: []*security.SecurityPolicy{}}

	r := NewSecurityPolicyReconciler(SecurityPolicyReconcilerConfig{
		SecurityService: svc,
		Logger:          newTestLogger(),
	})

	result, err := r.Reconcile(context.Background(), Request{ID: "gone"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !result.IsZero() {
		t.Error("not found should not requeue")
	}
}

func TestSecurityPolicyReconciler_Resync(t *testing.T) {
	svc := &fakeSecurityService{
		policies: []*security.SecurityPolicy{
			{ID: "p1", Name: "pol-1"},
			{ID: "p2", Name: "pol-2"},
		},
	}

	mgr := NewManager(ManagerConfig{Logger: newTestLogger(), SyncPeriod: time.Hour})
	r := NewSecurityPolicyReconciler(SecurityPolicyReconcilerConfig{
		SecurityService: svc,
		Manager:         mgr,
		Logger:          newTestLogger(),
	})
	mgr.RegisterReconciler(r)

	result, err := r.Reconcile(context.Background(), Request{Name: "__resync__"})
	if err != nil {
		t.Fatalf("resync error: %v", err)
	}
	if !result.IsZero() {
		t.Error("resync should return zero result")
	}
}

// ============================================================================
// Integration: Manager + real reconcilers
// ============================================================================

func TestIntegration_ManagerWithClusterReconciler(t *testing.T) {
	svc := &fakeClusterService{
		clusters: map[string]*cluster.Cluster{
			"c1": {ID: "c1", Name: "prod", Status: common.ClusterStatusHealthy, NodeCount: 3},
		},
		health: map[string]*cluster.ClusterHealth{
			"c1": {Status: "healthy", APIServerHealthy: true, ETCDHealthy: true, NodeTotalCount: 3, NodeReadyCount: 3},
		},
	}

	mgr := NewManager(ManagerConfig{
		Logger:                  newTestLogger(),
		MaxConcurrentReconciles: 1,
		SyncPeriod:              time.Hour,
	})

	r := NewClusterReconciler(ClusterReconcilerConfig{
		ClusterService: svc,
		Manager:        mgr,
		Logger:         newTestLogger(),
	})
	mgr.RegisterReconciler(r)

	ctx, cancel := context.WithCancel(context.Background())
	go mgr.Start(ctx)

	time.Sleep(50 * time.Millisecond)

	// Trigger reconciliation via Enqueue
	mgr.Enqueue("cluster-controller", Request{Name: "prod", ID: "c1"})

	time.Sleep(200 * time.Millisecond)

	status := r.GetStatus("c1")
	if status == nil {
		t.Fatal("expected reconciliation status for c1")
	}
	if status.ReconcileCount < 1 {
		t.Error("expected at least 1 reconciliation")
	}

	mgrStatus := mgr.Status()
	if len(mgrStatus.Controllers) != 1 {
		t.Errorf("expected 1 controller in status, got %d", len(mgrStatus.Controllers))
	}

	cancel()
	time.Sleep(200 * time.Millisecond)
}
