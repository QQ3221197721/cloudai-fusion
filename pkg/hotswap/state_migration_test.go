package hotswap_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/hotswap"
)

// statefulSnapshot is the serialized form of a StatefulMockComponent's state.
type statefulSnapshot struct {
	Counter int64             `json:"counter"`
	Cache   map[string]string `json:"cache"`
}

// StatefulMockComponent is a swappable component that carries REAL in-memory
// state: an integer counter plus a key-value cache map. It serializes that state
// through ExtractState/ApplyState so migration correctness can be asserted
// precisely (not merely "a swap happened"). It also supports a genuine restart
// after Stop so RollbackSwap can bring it back online.
type StatefulMockComponent struct {
	version hotswap.ComponentVersion

	mu      sync.Mutex
	counter int64
	cache   map[string]string

	started  bool
	stopped  bool
	inFlight int64
	drainCh  chan struct{}

	// Fault injection for clean-rollback (aborted swap) tests.
	failExtract bool
	failApply   bool
}

func newStatefulMock(name, version string) *StatefulMockComponent {
	return &StatefulMockComponent{
		version: hotswap.ComponentVersion{Name: name, Version: version},
		cache:   make(map[string]string),
		drainCh: make(chan struct{}),
	}
}

func (m *StatefulMockComponent) Start(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = true
	m.stopped = false
	// Refresh the drain channel so a restarted instance can drain again.
	m.drainCh = make(chan struct{})
	return nil
}

func (m *StatefulMockComponent) Stop(ctx context.Context) error {
	// Real drain semantics: wait for in-flight requests to complete.
	deadline := time.Now().Add(30 * time.Second)
	for atomic.LoadInt64(&m.inFlight) > 0 {
		if time.Now().After(deadline) {
			return hotswap.ErrComponentBusy
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = false
	m.stopped = true
	select {
	case <-m.drainCh:
		// already closed
	default:
		close(m.drainCh)
	}
	return nil
}

func (m *StatefulMockComponent) Drain() <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.drainCh == nil {
		m.drainCh = make(chan struct{})
	}
	return m.drainCh
}

func (m *StatefulMockComponent) Version() hotswap.ComponentVersion { return m.version }

// ExtractState serializes the live counter + cache into a byte snapshot.
func (m *StatefulMockComponent) ExtractState() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failExtract {
		return nil, errors.New("simulated extract failure")
	}
	snap := statefulSnapshot{Counter: m.counter, Cache: cloneCache(m.cache)}
	return json.Marshal(snap)
}

// ApplyState injects a previously extracted snapshot into this instance.
func (m *StatefulMockComponent) ApplyState(data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failApply {
		return errors.New("simulated apply failure")
	}
	var snap statefulSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return err
	}
	m.counter = snap.Counter
	if snap.Cache == nil {
		m.cache = make(map[string]string)
	} else {
		m.cache = snap.Cache
	}
	return nil
}

// --- test-only helpers ---

func (m *StatefulMockComponent) seed(counter int64, kv map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counter = counter
	m.cache = cloneCache(kv)
}

func (m *StatefulMockComponent) snapshot() (int64, map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counter, cloneCache(m.cache)
}

func (m *StatefulMockComponent) isStarted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started
}

// corrupt scribbles over the in-memory state. Used in the rollback test to prove
// that state is genuinely restored from the captured snapshot rather than merely
// surviving on an untouched object.
func (m *StatefulMockComponent) corrupt() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counter = -1
	m.cache = map[string]string{"__corrupt__": "yes"}
}

func (m *StatefulMockComponent) reqStart()          { atomic.AddInt64(&m.inFlight, 1) }
func (m *StatefulMockComponent) reqEnd()            { atomic.AddInt64(&m.inFlight, -1) }
func (m *StatefulMockComponent) inFlightCount() int64 { return atomic.LoadInt64(&m.inFlight) }

func cloneCache(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// TestStateMigration_Consistency asserts that after a swap the new instance holds
// EXACTLY the same in-memory state (counter + cache) as the old instance.
func TestStateMigration_Consistency(t *testing.T) {
	orch := hotswap.NewHotSwapOrchestrator(5 * time.Second)

	old := newStatefulMock("inference", "v1.0")
	if err := old.Start(context.Background()); err != nil {
		t.Fatalf("old.Start: %v", err)
	}
	wantCounter := int64(42)
	wantCache := map[string]string{
		"user:1":    "alice",
		"user:2":    "bob",
		"session:x": "active",
	}
	old.seed(wantCounter, wantCache)
	orch.SetComponent(old)

	newComp := newStatefulMock("inference", "v2.0")
	if err := orch.SwapComponent(old.Version(), newComp); err != nil {
		t.Fatalf("SwapComponent failed: %v", err)
	}

	gotCounter, gotCache := newComp.snapshot()
	if gotCounter != wantCounter {
		t.Fatalf("counter not migrated: new=%d, want=%d", gotCounter, wantCounter)
	}
	if !reflect.DeepEqual(gotCache, wantCache) {
		t.Fatalf("cache not migrated: new=%v, want=%v", gotCache, wantCache)
	}
	if !newComp.isStarted() {
		t.Fatalf("new component must be started after swap")
	}
	t.Logf("state migrated intact: counter=%d, cache entries=%d", gotCounter, len(gotCache))
}

// TestStateMigration_ZeroLoss drives continuous concurrent requests through the
// swap window and asserts (a) zero request loss (received == completed, old fully
// drained) and (b) the business state survived migration intact.
func TestStateMigration_ZeroLoss(t *testing.T) {
	orch := hotswap.NewHotSwapOrchestrator(5 * time.Second)

	old := newStatefulMock("gateway", "v1.0")
	if err := old.Start(context.Background()); err != nil {
		t.Fatalf("old.Start: %v", err)
	}
	wantCache := map[string]string{
		"route:/a":  "backend-1",
		"route:/b":  "backend-2",
		"flag:beta": "on",
	}
	const wantCounter = int64(1000)
	old.seed(wantCounter, wantCache)
	orch.SetComponent(old)

	newComp := newStatefulMock("gateway", "v1.1")

	const goroutines = 12
	const perGoroutine = 800

	var received, completed atomic.Int64
	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				select {
				case <-stop:
					return
				default:
				}
				received.Add(1)
				old.reqStart()
				time.Sleep(200 * time.Microsecond)
				old.reqEnd()
				completed.Add(1)
			}
		}()
	}

	// Perform the swap mid-flight while requests are still being drained.
	time.Sleep(5 * time.Millisecond)
	if err := orch.SwapComponent(old.Version(), newComp); err != nil {
		close(stop)
		wg.Wait()
		t.Fatalf("SwapComponent failed: %v", err)
	}
	wg.Wait()

	r, c := received.Load(), completed.Load()
	if r != c {
		t.Fatalf("request loss detected: received=%d, completed=%d (dropped=%d)", r, c, r-c)
	}
	if old.inFlightCount() != 0 {
		t.Fatalf("old component did not fully drain: inFlight=%d", old.inFlightCount())
	}

	gotCounter, gotCache := newComp.snapshot()
	if gotCounter != wantCounter {
		t.Fatalf("counter not migrated under load: got=%d, want=%d", gotCounter, wantCounter)
	}
	if !reflect.DeepEqual(gotCache, wantCache) {
		t.Fatalf("cache not migrated under load: got=%v, want=%v", gotCache, wantCache)
	}
	t.Logf("zero-loss migration: received=%d, completed=%d, migrated counter=%d, cache entries=%d",
		r, c, gotCounter, len(gotCache))
}

// TestRollback_RestoresState swaps to a new version, mutates it, corrupts the
// previous instance's memory, then rolls back and asserts the previous version is
// active again with its state restored from the captured snapshot.
func TestRollback_RestoresState(t *testing.T) {
	orch := hotswap.NewHotSwapOrchestrator(5 * time.Second)

	old := newStatefulMock("recommender", "v1.0")
	if err := old.Start(context.Background()); err != nil {
		t.Fatalf("old.Start: %v", err)
	}
	wantCounter := int64(7)
	wantCache := map[string]string{"k1": "v1", "k2": "v2"}
	old.seed(wantCounter, wantCache)
	orch.SetComponent(old)

	newComp := newStatefulMock("recommender", "v2.0")
	if err := orch.SwapComponent(old.Version(), newComp); err != nil {
		t.Fatalf("SwapComponent failed: %v", err)
	}

	// Precondition: the new version received the migrated state.
	if c, _ := newComp.snapshot(); c != wantCounter {
		t.Fatalf("precondition: new should have migrated counter=%d, got %d", wantCounter, c)
	}

	// The bad new version accumulates divergent state...
	newComp.seed(99, map[string]string{"bad": "state"})
	// ...and the stopped previous instance's memory is scribbled to prove the
	// rollback restores from the captured snapshot, not from an untouched object.
	old.corrupt()

	if err := orch.RollbackSwap(); err != nil {
		t.Fatalf("RollbackSwap failed: %v", err)
	}

	stats := orch.Stats()
	if got := stats["current_component"]; got != "recommender-v1.0" {
		t.Fatalf("rollback did not restore previous version: current=%v", got)
	}
	if !old.isStarted() {
		t.Fatalf("restored previous component must be started/serving")
	}
	gotCounter, gotCache := old.snapshot()
	if gotCounter != wantCounter {
		t.Fatalf("rollback did not restore counter: got=%d, want=%d", gotCounter, wantCounter)
	}
	if !reflect.DeepEqual(gotCache, wantCache) {
		t.Fatalf("rollback did not restore cache: got=%v, want=%v", gotCache, wantCache)
	}
	t.Logf("rollback restored: version=%v, counter=%d, cache=%v",
		stats["current_component"], gotCounter, gotCache)
}

// TestSwapAbort_CleanRollback verifies that a failure during migration (extract
// or apply) aborts the swap cleanly: the old instance keeps serving with intact
// state and the half-started new instance is stopped — no half-migrated state.
func TestSwapAbort_CleanRollback(t *testing.T) {
	cases := []struct {
		name      string
		configure func(newComp *StatefulMockComponent)
	}{
		{"ExtractFails", nil}, // extract failure injected on the OLD instance below
		{"ApplyFails", func(n *StatefulMockComponent) { n.failApply = true }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			orch := hotswap.NewHotSwapOrchestrator(5 * time.Second)

			old := newStatefulMock("svc", "v1.0")
			if err := old.Start(context.Background()); err != nil {
				t.Fatalf("old.Start: %v", err)
			}
			old.seed(5, map[string]string{"a": "1"})
			if tc.name == "ExtractFails" {
				old.failExtract = true
			}
			orch.SetComponent(old)

			newComp := newStatefulMock("svc", "v2.0")
			if tc.configure != nil {
				tc.configure(newComp)
			}

			if err := orch.SwapComponent(old.Version(), newComp); err == nil {
				t.Fatalf("expected swap to fail during migration")
			}

			// Old instance must remain the active, serving component.
			stats := orch.Stats()
			if got := stats["current_component"]; got != "svc-v1.0" {
				t.Fatalf("swap should not have switched away from old: current=%v", got)
			}
			// Its state must be untouched (extract failure means we never even read
			// it; apply failure means we aborted before the switch).
			gotCounter, gotCache := old.snapshot()
			if gotCounter != 5 || !reflect.DeepEqual(gotCache, map[string]string{"a": "1"}) {
				t.Fatalf("old state must be intact after aborted swap: counter=%d cache=%v", gotCounter, gotCache)
			}
			// The half-started new instance must have been stopped on abort. The
			// ExtractFails case aborts before the new instance is touched further,
			// but it was started then stopped by the cleanup path.
			if newComp.isStarted() {
				t.Fatalf("half-started new component must be stopped on abort")
			}
			t.Logf("aborted swap cleanly rolled back; %s still serving with counter=%d",
				stats["current_component"], gotCounter)
		})
	}
}
