package plugin

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// ============================================================================
// Hot-load runtime tests — Module 4 evidence
//
// These exercise the Add/Remove lifecycle, panic isolation, resource-limit
// rendering, and (the headline claim) 10 concurrent hot-adds under the race
// detector. Run with:
//
//	go test ./pkg/plugin/ -run 'HotLoad|Panic|Resource|Cgroup|State|Healthy' -race -count=1 -v
// ============================================================================

// probePlugin is a minimal, configurable Plugin used only by these tests. It
// can be told to fail Init/Start or to panic, so failure paths are exercised
// without a real plugin.
type probePlugin struct {
	BasePlugin
	initErr     error
	startErr    error
	panicOnInit bool

	mu      sync.Mutex
	started bool
	stopped bool
}

func newProbe(name string, exts ...ExtensionPoint) *probePlugin {
	return &probePlugin{
		BasePlugin: NewBasePlugin(Metadata{
			Name:            name,
			Version:         "1.0.0",
			ExtensionPoints: exts,
		}),
	}
}

func (p *probePlugin) Init(_ context.Context, _ map[string]interface{}) error {
	if p.panicOnInit {
		panic("probe: intentional Init panic")
	}
	return p.initErr
}

func (p *probePlugin) Start(_ context.Context) error {
	if p.startErr != nil {
		return p.startErr
	}
	p.mu.Lock()
	p.started = true
	p.mu.Unlock()
	return nil
}

func (p *probePlugin) Stop(_ context.Context) error {
	p.mu.Lock()
	p.stopped = true
	p.mu.Unlock()
	return nil
}

// ----------------------------------------------------------------------------
// Concurrency: 10 parallel hot-adds
// ----------------------------------------------------------------------------

// TestHotLoadTenConcurrentAdds is the task's headline concurrency requirement:
// hot-adding 10 distinct plugins in parallel must be race-free and leave every
// plugin running and indexed.
func TestHotLoadTenConcurrentAdds(t *testing.T) {
	r := NewRegistry()
	const count = 10

	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("probe-%02d", i)
			p := newProbe(name, ExtSchedulerScore)
			if err := r.AddWithOptions(context.Background(), name, p, AddOptions{}); err != nil {
				errs <- fmt.Errorf("add %s: %w", name, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	if got := len(r.List()); got != count {
		t.Fatalf("List() = %d plugins, want %d", got, count)
	}
	for _, info := range r.List() {
		if info.State != PluginStateRunning {
			t.Errorf("plugin %q in state %q, want running", info.Name, info.State)
		}
	}
	if got := len(r.GetByExtension(ExtSchedulerScore)); got != count {
		t.Errorf("GetByExtension = %d, want %d", got, count)
	}
}

// TestHotLoadConcurrentAddRemove interleaves adds and removes on the same
// registry to shake out lock ordering bugs under -race.
func TestHotLoadConcurrentAddRemove(t *testing.T) {
	r := NewRegistry()
	const count = 10

	var wg sync.WaitGroup
	errs := make(chan error, count*2)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("churn-%02d", i)
			p := newProbe(name, ExtMonitorCollector)
			if err := r.Add(name, p); err != nil {
				errs <- fmt.Errorf("add %s: %w", name, err)
				return
			}
			if err := r.Remove(name); err != nil {
				errs <- fmt.Errorf("remove %s: %w", name, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	if got := len(r.List()); got != 0 {
		t.Fatalf("after add+remove, List() = %d, want 0", got)
	}
}

// TestDuplicateAddRejected ensures a second Add of the same name loses.
func TestDuplicateAddRejected(t *testing.T) {
	r := NewRegistry()
	if err := r.Add("dup", newProbe("dup")); err != nil {
		t.Fatalf("first add: %v", err)
	}
	err := r.Add("dup", newProbe("dup"))
	if err == nil {
		t.Fatal("second add of same name should fail")
	}
	if _, ok := err.(*ErrPluginAlreadyRegistered); !ok {
		t.Fatalf("got %T, want *ErrPluginAlreadyRegistered", err)
	}
}

// ----------------------------------------------------------------------------
// Lifecycle and rollback
// ----------------------------------------------------------------------------

// TestAddRunsLifecycle confirms Init and Start are called when not skipped.
func TestAddRunsLifecycle(t *testing.T) {
	r := NewRegistry()
	p := newProbe("lifecycle", ExtSchedulerScore)
	err := r.AddWithOptions(context.Background(), "lifecycle", p, AddOptions{
		Config: map[string]interface{}{"k": "v"},
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	p.mu.Lock()
	started := p.started
	p.mu.Unlock()
	if !started {
		t.Error("Start was not called")
	}
	if r.State("lifecycle") != PluginStateRunning {
		t.Errorf("state = %q, want running", r.State("lifecycle"))
	}
}

// TestFailedStartRollsBack verifies a plugin whose Start fails is fully removed,
// not left half-indexed.
func TestFailedStartRollsBack(t *testing.T) {
	r := NewRegistry()
	p := newProbe("bad-start", ExtSchedulerScore)
	p.startErr = fmt.Errorf("boom")

	err := r.AddWithOptions(context.Background(), "bad-start", p, AddOptions{})
	if err == nil {
		t.Fatal("add should fail when Start fails")
	}
	// The plugin must not survive a failed add.
	if _, getErr := r.Get("bad-start"); getErr == nil {
		t.Error("Get() found a plugin that should have been rolled back")
	}
	if got := len(r.GetByExtension(ExtSchedulerScore)); got != 0 {
		t.Errorf("extension index has %d plugins after failed add, want 0", got)
	}
	if st := r.State("bad-start"); st != PluginStateFailed {
		t.Errorf("state = %q, want failed", st)
	}
}

// TestRemoveCallsStop confirms Remove drives Stop and clears the registry.
func TestRemoveCallsStop(t *testing.T) {
	r := NewRegistry()
	p := newProbe("removable", ExtSchedulerScore)
	if err := r.AddWithOptions(context.Background(), "removable", p, AddOptions{}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := r.Remove("removable"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	p.mu.Lock()
	stopped := p.stopped
	p.mu.Unlock()
	if !stopped {
		t.Error("Stop was not called on Remove")
	}
	if st := r.State("removable"); st != PluginStateUnloaded {
		t.Errorf("state = %q, want unloaded", st)
	}
}

// TestRemoveUnknownPlugin returns a not-found error.
func TestRemoveUnknownPlugin(t *testing.T) {
	r := NewRegistry()
	err := r.Remove("ghost")
	if err == nil {
		t.Fatal("removing an unknown plugin should fail")
	}
	if _, ok := err.(*ErrPluginNotFound); !ok {
		t.Fatalf("got %T, want *ErrPluginNotFound", err)
	}
}

// ----------------------------------------------------------------------------
// Panic isolation
// ----------------------------------------------------------------------------

// TestSafeCallRecoversPanic proves a panicking plugin call becomes an error
// instead of crashing the host goroutine.
func TestSafeCallRecoversPanic(t *testing.T) {
	err := SafeCall("panicker", "Score", func() error {
		panic("kaboom")
	})
	if err == nil {
		t.Fatal("SafeCall should return an error when fn panics")
	}
	pe, ok := err.(*ErrPluginPanic)
	if !ok {
		t.Fatalf("got %T, want *ErrPluginPanic", err)
	}
	if pe.Plugin != "panicker" || pe.Op != "Score" {
		t.Errorf("panic metadata = %q/%q, want panicker/Score", pe.Plugin, pe.Op)
	}
	if pe.Stack == "" {
		t.Error("recovered panic should capture a stack trace")
	}
}

// TestInvokeQuarantinesPanickingPlugin verifies Invoke marks a panicking plugin
// failed so HealthyByExtension stops routing to it.
func TestInvokeQuarantinesPanickingPlugin(t *testing.T) {
	r := NewRegistry()
	p := newProbe("crasher", ExtSchedulerScore)
	if err := r.Add("crasher", p); err != nil {
		t.Fatalf("add: %v", err)
	}

	err := r.Invoke("crasher", "Score", func() error { panic("crash") })
	if _, ok := err.(*ErrPluginPanic); !ok {
		t.Fatalf("Invoke returned %T, want *ErrPluginPanic", err)
	}
	if st := r.State("crasher"); st != PluginStateFailed {
		t.Errorf("state = %q, want failed after panic", st)
	}
	if got := len(r.HealthyByExtension(ExtSchedulerScore)); got != 0 {
		t.Errorf("HealthyByExtension = %d, want 0 (crasher quarantined)", got)
	}
	// But it stays visible for operators via GetByExtension/List.
	if got := len(r.GetByExtension(ExtSchedulerScore)); got != 1 {
		t.Errorf("GetByExtension = %d, want 1 (crasher still listed)", got)
	}
}

// TestFailedInitPanicRollsBack confirms an Init that panics rolls the plugin
// back and never survives as running.
func TestFailedInitPanicRollsBack(t *testing.T) {
	r := NewRegistry()
	p := newProbe("panic-init", ExtSchedulerScore)
	p.panicOnInit = true

	err := r.AddWithOptions(context.Background(), "panic-init", p, AddOptions{
		Config: map[string]interface{}{"trigger": true},
	})
	if err == nil {
		t.Fatal("add should fail when Init panics")
	}
	if _, getErr := r.Get("panic-init"); getErr == nil {
		t.Error("panicking Init left a live plugin behind")
	}
}

// ----------------------------------------------------------------------------
// Resource limits
// ----------------------------------------------------------------------------

// TestResourceLimitsValidate rejects negative budgets.
func TestResourceLimitsValidate(t *testing.T) {
	cases := []struct {
		name    string
		limit   ResourceLimits
		wantErr bool
	}{
		{"zero", ResourceLimits{}, false},
		{"valid", ResourceLimits{CPUMilli: 500, MemoryMB: 2048, PidsMax: 100}, false},
		{"neg_cpu", ResourceLimits{CPUMilli: -1}, true},
		{"neg_mem", ResourceLimits{MemoryMB: -1}, true},
		{"neg_pids", ResourceLimits{PidsMax: -1}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if gotErr := tc.limit.Validate(); (gotErr != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", gotErr, tc.wantErr)
			}
		})
	}
}

// TestCgroupV2Rendering checks the limits render to the exact kernel key/values.
func TestCgroupV2Rendering(t *testing.T) {
	l := ResourceLimits{CPUMilli: 1500, MemoryMB: 512, PidsMax: 64}
	vals := l.CgroupV2Values()

	// 1500 milli-cores → quota = 1500 * 100000 / 1000 = 150000, period 100000.
	if got, want := vals["cpu.max"], "150000 100000"; got != want {
		t.Errorf("cpu.max = %q, want %q", got, want)
	}
	if got, want := vals["memory.max"], "536870912"; got != want { // 512 * 1Mi
		t.Errorf("memory.max = %q, want %q", got, want)
	}
	if got, want := vals["pids.max"], "64"; got != want {
		t.Errorf("pids.max = %q, want %q", got, want)
	}

	// Unset fields are omitted, not written as "max".
	if _, ok := (ResourceLimits{}).CgroupV2Values()["cpu.max"]; ok {
		t.Error("empty limits should not render cpu.max")
	}
}

// TestMockCgroupController applies and releases a scope's limits.
func TestMockCgroupController(t *testing.T) {
	c := NewMockCgroupV2Controller()
	limits := ResourceLimits{CPUMilli: 1000, MemoryMB: 256, PidsMax: 32}

	if err := c.Apply("scope-a", limits); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	vals, ok := c.Values("scope-a")
	if !ok {
		t.Fatal("no values recorded for scope-a")
	}
	if vals["memory.max"] != "268435456" {
		t.Errorf("memory.max = %q, want 268435456", vals["memory.max"])
	}
	if got := c.Scopes(); len(got) != 1 || got[0] != "scope-a" {
		t.Errorf("Scopes() = %v, want [scope-a]", got)
	}

	if err := c.Release("scope-a"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, ok := c.Values("scope-a"); ok {
		t.Error("values remained after Release")
	}
}

// TestAddAppliesResourceLimits confirms a controller receives the budget on Add
// and releases it on Remove.
func TestAddAppliesResourceLimits(t *testing.T) {
	r := NewRegistry()
	c := NewMockCgroupV2Controller()
	limits := ResourceLimits{CPUMilli: 2000, MemoryMB: 1024}

	err := r.AddWithOptions(context.Background(), "budgeted", newProbe("budgeted"), AddOptions{
		Limits:     limits,
		Controller: c,
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, ok := c.Values("budgeted"); !ok {
		t.Error("controller did not receive limits for the plugin scope")
	}
	if got, ok := r.ResourceLimitsOf("budgeted"); !ok || got != limits {
		t.Errorf("ResourceLimitsOf = %v/%v, want %v/true", got, ok, limits)
	}

	if err := r.RemoveWithOptions(context.Background(), "budgeted", c); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, ok := c.Values("budgeted"); ok {
		t.Error("controller retained limits after Remove")
	}
}

// ----------------------------------------------------------------------------
// State inspection
// ----------------------------------------------------------------------------

// TestListSortedByName confirms List() is deterministic.
func TestListSortedByName(t *testing.T) {
	r := NewRegistry()
	for _, n := range []string{"zebra", "alpha", "mike"} {
		if err := r.Add(n, newProbe(n)); err != nil {
			t.Fatalf("add %s: %v", n, err)
		}
	}
	list := r.List()
	for i := 1; i < len(list); i++ {
		if list[i-1].Name > list[i].Name {
			t.Errorf("List() not sorted: %q before %q", list[i-1].Name, list[i].Name)
		}
	}
}

// TestConcurrentReadsAreRaceFree hammers the read-side accessors while writes
// happen, so -race can catch an unguarded map.
func TestConcurrentReadsAreRaceFree(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup

	// Writers.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("rw-%d", i)
			_ = r.Add(name, newProbe(name, ExtSchedulerScore))
		}(i)
	}
	// Readers.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = r.List()
				_ = r.State("rw-0")
				_ = r.NamespaceOf("rw-0")
				_, _ = r.ResourceLimitsOf("rw-0")
				_ = r.HealthyByExtension(ExtSchedulerScore)
			}
		}()
	}
	wg.Wait()
}
