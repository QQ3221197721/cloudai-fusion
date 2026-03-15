package plugin

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ============================================================================
// Types Tests
// ============================================================================

func TestPhase_Constants(t *testing.T) {
	phases := []Phase{PhaseCreated, PhaseInitializing, PhaseReady, PhaseRunning, PhaseStopping, PhaseStopped, PhaseError}
	seen := make(map[Phase]bool)
	for _, p := range phases {
		if string(p) == "" {
			t.Error("phase should not be empty")
		}
		if seen[p] {
			t.Errorf("duplicate phase: %s", p)
		}
		seen[p] = true
	}
}

func TestExtensionPoint_Constants(t *testing.T) {
	exts := []ExtensionPoint{
		ExtSchedulerFilter, ExtSchedulerScore, ExtSchedulerPreBind, ExtSchedulerBind,
		ExtSchedulerPostBind, ExtSchedulerReserve, ExtSchedulerPermit,
		ExtSecurityScanner, ExtSecurityPolicyEnforce, ExtSecurityAudit, ExtSecurityThreatDetect,
		ExtCloudProvider, ExtMonitorCollector, ExtMonitorAlerter,
		ExtAuthAuthenticator, ExtAuthAuthorizer,
		ExtWebhookMutating, ExtWebhookValidating,
	}
	seen := make(map[ExtensionPoint]bool)
	for _, ext := range exts {
		if string(ext) == "" {
			t.Error("extension point should not be empty")
		}
		if seen[ext] {
			t.Errorf("duplicate extension point: %s", ext)
		}
		seen[ext] = true
	}
}

func TestCode_Constants(t *testing.T) {
	if Success != 0 {
		t.Errorf("Success = %d, want 0", Success)
	}
	if Error != 1 {
		t.Errorf("Error = %d, want 1", Error)
	}
}

func TestResult_IsSuccess(t *testing.T) {
	r := &Result{Code: Success}
	if !r.IsSuccess() {
		t.Error("Success result should be IsSuccess")
	}
	r = &Result{Code: Error}
	if r.IsSuccess() {
		t.Error("Error result should not be IsSuccess")
	}
}

func TestNewResult(t *testing.T) {
	r := NewResult(Unschedulable, "test-plugin", "node too small")
	if r.Code != Unschedulable {
		t.Errorf("code = %d, want Unschedulable", r.Code)
	}
	if r.Plugin != "test-plugin" || r.Reason != "node too small" {
		t.Errorf("unexpected result: %+v", r)
	}
}

func TestSuccessResult(t *testing.T) {
	r := SuccessResult("my-plugin")
	if !r.IsSuccess() {
		t.Error("SuccessResult should be success")
	}
	if r.Plugin != "my-plugin" {
		t.Errorf("plugin = %q", r.Plugin)
	}
}

func TestErrorResult(t *testing.T) {
	r := ErrorResult("bad-plugin", errors.New("boom"))
	if r.IsSuccess() {
		t.Error("ErrorResult should not be success")
	}
	if r.Reason != "boom" {
		t.Errorf("reason = %q", r.Reason)
	}
}

func TestMerge(t *testing.T) {
	s1 := SuccessResult("a")
	s2 := SuccessResult("b")
	r := Merge(s1, s2)
	if !r.IsSuccess() {
		t.Error("merging two successes should be success")
	}

	e := ErrorResult("c", errors.New("fail"))
	r = Merge(s1, e, s2)
	if r.IsSuccess() {
		t.Error("merge with error should not be success")
	}
	if r.Plugin != "c" {
		t.Errorf("first non-success should win, got plugin=%q", r.Plugin)
	}
}

func TestMerge_AllNil(t *testing.T) {
	r := Merge(nil, nil)
	if !r.IsSuccess() {
		t.Error("merge of nils should be success")
	}
}

// ============================================================================
// BasePlugin Tests
// ============================================================================

func TestNewBasePlugin(t *testing.T) {
	bp := NewBasePlugin(Metadata{Name: "test", Version: "1.0"})
	if bp.Metadata().Name != "test" {
		t.Errorf("name = %q", bp.Metadata().Name)
	}
	if bp.Metadata().Priority != 100 {
		t.Errorf("default priority = %d, want 100", bp.Metadata().Priority)
	}
}

func TestBasePlugin_NoOps(t *testing.T) {
	bp := NewBasePlugin(Metadata{Name: "noop"})
	ctx := context.Background()
	if err := bp.Init(ctx, nil); err != nil {
		t.Errorf("Init: %v", err)
	}
	if err := bp.Start(ctx); err != nil {
		t.Errorf("Start: %v", err)
	}
	if err := bp.Health(ctx); err != nil {
		t.Errorf("Health: %v", err)
	}
	if err := bp.Stop(ctx); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

// ============================================================================
// Error Types Tests
// ============================================================================

func TestErrPluginNotFound(t *testing.T) {
	err := &ErrPluginNotFound{Name: "missing"}
	if err.Error() == "" {
		t.Error("error message should not be empty")
	}
}

func TestErrPluginAlreadyRegistered(t *testing.T) {
	err := &ErrPluginAlreadyRegistered{Name: "dup"}
	if err.Error() == "" {
		t.Error("error message should not be empty")
	}
}

func TestErrExtensionPointMismatch(t *testing.T) {
	err := &ErrExtensionPointMismatch{Plugin: "p", Extension: ExtSchedulerFilter}
	if err.Error() == "" {
		t.Error("error message should not be empty")
	}
}

// ============================================================================
// Registry Tests
// ============================================================================

func makeTestFactory(name string, exts []ExtensionPoint, deps []string, priority int) Factory {
	return func() (Plugin, error) {
		meta := Metadata{
			Name:            name,
			Version:         "1.0",
			ExtensionPoints: exts,
			Dependencies:    deps,
			Priority:        priority,
		}
		bp := NewBasePlugin(meta)
		return &bp, nil
	}
}

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry()
	err := r.Register("plug-a", makeTestFactory("plug-a", nil, nil, 100))
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Duplicate should error
	err = r.Register("plug-a", makeTestFactory("plug-a", nil, nil, 100))
	if err == nil {
		t.Error("duplicate register should error")
	}
}

func TestRegistry_MustRegister_Panic(t *testing.T) {
	r := NewRegistry()
	r.MustRegister("x", makeTestFactory("x", nil, nil, 100))
	defer func() {
		if recover() == nil {
			t.Error("duplicate MustRegister should panic")
		}
	}()
	r.MustRegister("x", makeTestFactory("x", nil, nil, 100))
}

func TestRegistry_Unregister(t *testing.T) {
	r := NewRegistry()
	r.Register("a", makeTestFactory("a", []ExtensionPoint{ExtSchedulerFilter}, nil, 10))
	r.Build()
	r.Unregister("a")
	_, err := r.Get("a")
	if err == nil {
		t.Error("unregistered plugin should not be found")
	}
}

func TestRegistry_DisableEnable(t *testing.T) {
	r := NewRegistry()
	r.Register("a", makeTestFactory("a", nil, nil, 100))
	r.Disable("a")
	order, err := r.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(order) != 0 {
		t.Errorf("disabled plugin should not be built, order=%v", order)
	}
	r.Enable("a")
	order, _ = r.Build()
	if len(order) != 1 {
		t.Errorf("re-enabled plugin should be built, order=%v", order)
	}
}

func TestRegistry_Build_Dependencies(t *testing.T) {
	r := NewRegistry()
	r.Register("base", makeTestFactory("base", nil, nil, 100))
	r.Register("child", makeTestFactory("child", nil, []string{"base"}, 100))

	order, err := r.Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// "base" should come before "child"
	baseIdx, childIdx := -1, -1
	for i, n := range order {
		if n == "base" {
			baseIdx = i
		}
		if n == "child" {
			childIdx = i
		}
	}
	if baseIdx >= childIdx {
		t.Errorf("base should come before child: %v", order)
	}
}

func TestRegistry_Build_CyclicDeps(t *testing.T) {
	r := NewRegistry()
	r.Register("a", makeTestFactory("a", nil, []string{"b"}, 100))
	r.Register("b", makeTestFactory("b", nil, []string{"a"}, 100))
	_, err := r.Build()
	if err == nil {
		t.Error("cyclic dependency should error")
	}
}

func TestRegistry_Build_MissingDep(t *testing.T) {
	r := NewRegistry()
	r.Register("a", makeTestFactory("a", nil, []string{"nonexistent"}, 100))
	_, err := r.Build()
	if err == nil {
		t.Error("missing dependency should error")
	}
}

func TestRegistry_GetByExtension(t *testing.T) {
	r := NewRegistry()
	r.Register("filter1", makeTestFactory("filter1", []ExtensionPoint{ExtSchedulerFilter}, nil, 20))
	r.Register("filter2", makeTestFactory("filter2", []ExtensionPoint{ExtSchedulerFilter}, nil, 10))
	r.Register("scorer", makeTestFactory("scorer", []ExtensionPoint{ExtSchedulerScore}, nil, 100))
	r.Build()

	filters := r.GetByExtension(ExtSchedulerFilter)
	if len(filters) != 2 {
		t.Fatalf("expected 2 filter plugins, got %d", len(filters))
	}
	// Lower priority first
	if filters[0].Metadata().Name != "filter2" {
		t.Errorf("filter2 (priority=10) should come first, got %s", filters[0].Metadata().Name)
	}

	scorers := r.GetByExtension(ExtSchedulerScore)
	if len(scorers) != 1 {
		t.Fatalf("expected 1 score plugin, got %d", len(scorers))
	}
}

func TestRegistry_Names(t *testing.T) {
	r := NewRegistry()
	r.Register("b", makeTestFactory("b", nil, nil, 100))
	r.Register("a", makeTestFactory("a", nil, nil, 100))
	names := r.Names()
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Errorf("Names() should be sorted: %v", names)
	}
}

func TestRegistry_ExtensionPoints(t *testing.T) {
	r := NewRegistry()
	r.Register("p", makeTestFactory("p", []ExtensionPoint{ExtSchedulerFilter, ExtSchedulerScore}, nil, 100))
	r.Build()
	exts := r.ExtensionPoints()
	if len(exts) != 2 {
		t.Errorf("expected 2 extension points, got %d", len(exts))
	}
}

func TestGlobalRegistry(t *testing.T) {
	gr := GlobalRegistry()
	if gr == nil {
		t.Error("GlobalRegistry should not be nil")
	}
}

// ============================================================================
// Manager Tests
// ============================================================================

func TestDefaultManagerConfig(t *testing.T) {
	cfg := DefaultManagerConfig()
	if cfg.InitTimeout != 30*time.Second {
		t.Errorf("InitTimeout = %v, want 30s", cfg.InitTimeout)
	}
	if cfg.HealthCheckInterval != 30*time.Second {
		t.Errorf("HealthCheckInterval = %v", cfg.HealthCheckInterval)
	}
}

func TestManager_InitStartStop(t *testing.T) {
	reg := NewRegistry()
	reg.Register("test-plug", makeTestFactory("test-plug", []ExtensionPoint{ExtSchedulerFilter}, nil, 100))

	m := NewManager(reg, DefaultManagerConfig())

	ctx := context.Background()
	if err := m.InitAll(ctx); err != nil {
		t.Fatalf("InitAll: %v", err)
	}

	if err := m.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	// Check status
	st, err := m.Status("test-plug")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Phase != PhaseRunning {
		t.Errorf("phase = %s, want running", st.Phase)
	}
	if !st.Healthy {
		t.Error("should be healthy")
	}

	// Stop
	if err := m.StopAll(ctx); err != nil {
		t.Errorf("StopAll: %v", err)
	}
	st, _ = m.Status("test-plug")
	if st.Phase != PhaseStopped {
		t.Errorf("phase after stop = %s, want stopped", st.Phase)
	}
}

func TestManager_AllStatuses(t *testing.T) {
	reg := NewRegistry()
	reg.Register("a", makeTestFactory("a", nil, nil, 100))
	reg.Register("b", makeTestFactory("b", nil, nil, 100))

	m := NewManager(reg, DefaultManagerConfig())
	m.InitAll(context.Background())
	m.StartAll(context.Background())

	statuses := m.AllStatuses()
	if len(statuses) != 2 {
		t.Errorf("expected 2 statuses, got %d", len(statuses))
	}
	m.StopAll(context.Background())
}

func TestManager_PluginOrder(t *testing.T) {
	reg := NewRegistry()
	reg.Register("base", makeTestFactory("base", nil, nil, 100))
	reg.Register("ext", makeTestFactory("ext", nil, []string{"base"}, 100))

	m := NewManager(reg, DefaultManagerConfig())
	m.InitAll(context.Background())

	order := m.PluginOrder()
	if len(order) != 2 {
		t.Fatalf("expected 2 in order, got %d", len(order))
	}
	if order[0] != "base" {
		t.Errorf("base should come first, got %v", order)
	}
}

func TestManager_StatusNotFound(t *testing.T) {
	reg := NewRegistry()
	m := NewManager(reg, DefaultManagerConfig())
	_, err := m.Status("nonexistent")
	if err == nil {
		t.Error("Status of nonexistent plugin should error")
	}
}

// ============================================================================
// CycleState Tests
// ============================================================================

func TestCycleState(t *testing.T) {
	cs := NewCycleState()
	cs.Write("key1", 42)

	val, ok := cs.Read("key1")
	if !ok || val.(int) != 42 {
		t.Errorf("Read key1 = %v, %v", val, ok)
	}

	_, ok = cs.Read("missing")
	if ok {
		t.Error("missing key should return ok=false")
	}

	cs.Delete("key1")
	_, ok = cs.Read("key1")
	if ok {
		t.Error("deleted key should not be found")
	}
}

// ============================================================================
// Webhook Plugin Tests
// ============================================================================

func TestNewWebhookPlugin(t *testing.T) {
	wp := NewWebhookPlugin(WebhookConfig{
		Name:            "test-webhook",
		URL:             "https://example.com/hook",
		ExtensionPoints: []ExtensionPoint{ExtWebhookValidating},
	})
	if wp == nil {
		t.Fatal("should not be nil")
	}
	meta := wp.Metadata()
	if meta.Name != "test-webhook" {
		t.Errorf("name = %q", meta.Name)
	}
	if meta.Priority != 500 {
		t.Errorf("default webhook priority = %d, want 500", meta.Priority)
	}
}

func TestNewWebhookPlugin_Defaults(t *testing.T) {
	wp := NewWebhookPlugin(WebhookConfig{Name: "w"})
	if wp.cfg.FailurePolicy != "Fail" {
		t.Errorf("default FailurePolicy = %q, want 'Fail'", wp.cfg.FailurePolicy)
	}
	if wp.client.Timeout != 10*time.Second {
		t.Errorf("default timeout = %v, want 10s", wp.client.Timeout)
	}
}

func TestWebhookPluginFactory(t *testing.T) {
	f := WebhookPluginFactory(WebhookConfig{Name: "factory-test", URL: "http://localhost"})
	p, err := f()
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if p.Metadata().Name != "factory-test" {
		t.Errorf("name = %q", p.Metadata().Name)
	}
}

// ============================================================================
// SchedulerPluginChain Tests
// ============================================================================

func TestSchedulerPluginChain_EmptyRegistry(t *testing.T) {
	reg := NewRegistry()
	reg.Build()
	chain := NewSchedulerPluginChain(reg)

	w := &WorkloadInfo{Name: "test", GPUCount: 1}
	node := &NodeInfo{Name: "node-1", GPUFree: 4}

	r := chain.RunFilterPlugins(context.Background(), NewCycleState(), w, node)
	if !r.IsSuccess() {
		t.Error("empty filter chain should succeed")
	}

	scores, sr := chain.RunScorePlugins(context.Background(), NewCycleState(), w, []*NodeInfo{node})
	if !sr.IsSuccess() {
		t.Error("empty score chain should succeed")
	}
	if scores["node-1"] != 0 {
		t.Errorf("empty score chain should give 0, got %d", scores["node-1"])
	}

	pr := chain.RunPreBindPlugins(context.Background(), NewCycleState(), w, "node-1")
	if !pr.IsSuccess() {
		t.Error("empty prebind chain should succeed")
	}

	// PostBind and Reserve should not panic
	chain.RunPostBindPlugins(context.Background(), NewCycleState(), w, "node-1")
	rr := chain.RunReservePlugins(context.Background(), NewCycleState(), w, "node-1")
	if !rr.IsSuccess() {
		t.Error("empty reserve chain should succeed")
	}
}
