package eventbus

import (
	"context"
	"crypto/ed25519"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/runmode"
	"github.com/sirupsen/logrus"
)

// fabric_test.go covers the "real" Event Message Fabric behaviour layered onto
// WellRouter: the hard 8-hop cap with explicit errors, the automatic L8 SOAR
// consumer at the terminal hop, evidence-signed consumed events, concurrency
// safety, and cycle termination.

// quietLogger returns a logger that stays silent during tests.
func quietLogger() *logrus.Logger {
	lg := logrus.New()
	lg.SetLevel(logrus.PanicLevel)
	return lg
}

// testReceiptBuilder returns a deterministic Ed25519-backed receipt builder.
func testReceiptBuilder() *evidence.ReceiptBuilder {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i*7 + 1)
	}
	return evidence.NewReceiptBuilder("eventbus.fabric", ed25519.NewKeyFromSeed(seed))
}

// eventAtHop builds a well event whose current well and hop counter are set
// directly in metadata (as the fabric records them).
func eventAtHop(t *testing.T, well DeepWell, hop int) *Event {
	t.Helper()
	ev, err := NewEvent(TopicWellEvent, "test", well.String(), WellEvent{Well: well, Kind: "test"})
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	ev.WithMetadata(mdWell, strconv.Itoa(int(well))).
		WithMetadata(mdWellName, well.String()).
		WithMetadata(mdHop, strconv.Itoa(hop))
	return ev
}

func TestFabric_MaxWellHopsIsEight(t *testing.T) {
	if MaxWellHops != 8 {
		t.Fatalf("MaxWellHops must be 8 (hard AISecOps constraint), got %d", MaxWellHops)
	}
}

func TestFabric_ForwardRejectedAtHardCap(t *testing.T) {
	bus := NewMemoryBus(DefaultConfig(), quietLogger())
	defer func() { _ = bus.Close() }()
	r := NewWellRouter(bus, 4, quietLogger())

	// At hop 7 a forward is allowed (it produces the terminal hop 8).
	if err := r.Forward(context.Background(), eventAtHop(t, WellIntel, 7), WellHunt); err != nil {
		t.Fatalf("forward at hop 7 must be allowed, got %v", err)
	}

	// At hop 8 (and beyond) forwarding would exceed the cap and must fail with
	// an explicit, wrapped ErrHopLimitExceeded — never a silent drop.
	for _, hop := range []int{8, 9, 20} {
		err := r.Forward(context.Background(), eventAtHop(t, WellIntel, hop), WellHunt)
		if !errors.Is(err, ErrHopLimitExceeded) {
			t.Fatalf("forward at hop %d must return ErrHopLimitExceeded, got %v", hop, err)
		}
		if err.Error() == "" {
			t.Fatalf("hop-limit error message must be non-empty")
		}
	}
}

func TestFabric_RouteEventRejectsBeyondCap(t *testing.T) {
	bus := NewMemoryBus(DefaultConfig(), quietLogger())
	defer func() { _ = bus.Close() }()
	r := NewWellRouter(bus, 4, quietLogger())

	// An event that somehow arrived above the cap must be refused explicitly.
	err := r.RouteEvent(context.Background(), eventAtHop(t, WellIntel, MaxWellHops+1))
	if !errors.Is(err, ErrHopLimitExceeded) {
		t.Fatalf("RouteEvent beyond cap must return ErrHopLimitExceeded, got %v", err)
	}
}

func TestFabric_L8ConsumerFiresAtTerminalHop(t *testing.T) {
	bus := NewMemoryBus(DefaultConfig(), quietLogger())
	defer func() { _ = bus.Close() }()
	r := NewWellRouter(bus, 4, quietLogger())

	var fired int64
	var sawEvent *Event
	r.SetL8Consumer(func(_ context.Context, ev *Event) error {
		atomic.AddInt64(&fired, 1)
		sawEvent = ev
		return nil
	})

	// An event at the terminal hop is consumed by L8 and NOT forwarded further.
	if err := r.RouteEvent(context.Background(), eventAtHop(t, WellNetwork, MaxWellHops)); err != nil {
		t.Fatalf("RouteEvent at terminal hop must succeed, got %v", err)
	}
	if got := atomic.LoadInt64(&fired); got != 1 {
		t.Fatalf("L8 consumer must fire exactly once at hop %d, fired=%d", MaxWellHops, got)
	}
	if r.L8InvocationCount() != 1 {
		t.Fatalf("L8InvocationCount=%d, want 1", r.L8InvocationCount())
	}
	if sawEvent == nil {
		t.Fatalf("L8 consumer must receive the triggering event")
	}
	if r.ForwardedCount() != 0 {
		t.Fatalf("terminal-hop event must not be forwarded, forwarded=%d", r.ForwardedCount())
	}

	// A below-cap event does not fire L8 but does forward downstream.
	if err := r.RouteEvent(context.Background(), eventAtHop(t, WellNetwork, 0)); err != nil {
		t.Fatalf("RouteEvent below cap: %v", err)
	}
	if r.L8InvocationCount() != 1 {
		t.Fatalf("L8 must not fire below the terminal hop, count=%d", r.L8InvocationCount())
	}
	if r.ForwardedCount() == 0 {
		t.Fatalf("below-cap event must forward to downstream wells")
	}
}

func TestFabric_EvidenceSignsConsumedEvents(t *testing.T) {
	bus := NewMemoryBus(DefaultConfig(), quietLogger())
	defer func() { _ = bus.Close() }()
	r := NewWellRouter(bus, 4, quietLogger())
	r.SetEvidence(testReceiptBuilder())

	// Route several terminal-hop events sequentially so no forwarding happens;
	// each consumed event yields exactly one signed receipt, forming a chain.
	const n = 6
	for i := 0; i < n; i++ {
		if err := r.RouteEvent(context.Background(), eventAtHop(t, WellResponse, MaxWellHops)); err != nil {
			t.Fatalf("RouteEvent %d: %v", i, err)
		}
	}

	receipts := r.Receipts()
	if len(receipts) != n {
		t.Fatalf("expected %d receipts, got %d", n, len(receipts))
	}
	for i, rc := range receipts {
		if !rc.Verify() {
			t.Fatalf("receipt %d failed signature verification", i)
		}
	}
	// Sequential routing must produce a valid hash-chain of receipts.
	if err := evidence.VerifyChainOfReceipts(receipts); err != nil {
		t.Fatalf("evidence chain verify failed: %v", err)
	}
}

func TestFabric_ConcurrentRoutingNoRace(t *testing.T) {
	bus := NewMemoryBus(DefaultConfig(), quietLogger())
	defer func() { _ = bus.Close() }()
	r := NewWellRouter(bus, 4, quietLogger())
	r.SetEvidence(testReceiptBuilder())

	var l8 int64
	r.SetL8Consumer(func(_ context.Context, _ *Event) error {
		atomic.AddInt64(&l8, 1)
		return nil
	})

	const goroutines = 32
	const perG = 40
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				// Each goroutine routes its own terminal-hop events (distinct
				// pointers) so we exercise concurrent sign + L8 without sharing
				// event state.
				if err := r.RouteEvent(context.Background(), eventAtHop(t, WellResponse, MaxWellHops)); err != nil {
					t.Errorf("concurrent RouteEvent: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	total := int64(goroutines * perG)
	if got := atomic.LoadInt64(&l8); got != total {
		t.Fatalf("L8 fired %d times, want %d", got, total)
	}
	receipts := r.Receipts()
	if int64(len(receipts)) != total {
		t.Fatalf("got %d receipts, want %d", len(receipts), total)
	}
	// Individual signatures must all verify (chain order is not guaranteed
	// under concurrency, so we verify each receipt independently).
	for i, rc := range receipts {
		if !rc.Verify() {
			t.Fatalf("concurrent receipt %d failed verification", i)
		}
	}
}

func TestFabric_RingRoutingTerminates(t *testing.T) {
	bus := NewMemoryBus(DefaultConfig(), quietLogger())
	defer func() { _ = bus.Close() }()

	col := newCollector()
	if _, err := bus.Subscribe(TopicWellEvent, col.handle); err != nil {
		t.Fatalf("subscribe collector: %v", err)
	}

	r := NewWellRouter(bus, 4, quietLogger())
	r.SetEvidence(testReceiptBuilder())
	var l8 int64
	r.SetL8Consumer(func(_ context.Context, _ *Event) error {
		atomic.AddInt64(&l8, 1)
		return nil
	})
	if err := r.ConnectFabric(context.Background()); err != nil {
		t.Fatalf("connect fabric: %v", err)
	}

	// L1 sits on cycles (L1->L2->L1, L1->L14->L1). Without the hard cap this
	// would loop forever. Publish one seed and assert the fabric quiesces.
	if err := PublishWellEvent(context.Background(), bus, WellIntel, "cve_ingested", nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	waitFor(t, func() bool { return col.count() > 0 }, 2*time.Second)

	// Give the fabric time and assert the event count stabilizes (finite).
	time.Sleep(300 * time.Millisecond)
	stable := col.count()
	time.Sleep(200 * time.Millisecond)
	if col.count() != stable {
		t.Fatalf("event count still growing (%d -> %d): cycle not bounded", stable, col.count())
	}
	if atomic.LoadInt64(&l8) == 0 {
		t.Fatalf("expected L8 SOAR to fire at least once as events reach the terminal hop")
	}
	// Every consumed event on the fabric produced a verifiable receipt.
	for i, rc := range r.Receipts() {
		if !rc.Verify() {
			t.Fatalf("fabric receipt %d failed verification", i)
		}
	}
}

func TestFabric_ReportsSimulatedForMemoryBus(t *testing.T) {
	bus := NewMemoryBus(DefaultConfig(), quietLogger())
	defer func() { _ = bus.Close() }()
	r := NewWellRouter(bus, 4, quietLogger())

	// Under a non-production policy, reporting a simulated backend is allowed
	// but recorded truthfully.
	reg := capability.NewRegistry(runmode.Simulation)
	if err := r.ReportCapability(reg); err != nil {
		t.Fatalf("report under simulation policy: %v", err)
	}
	sim := reg.Simulated()
	found := false
	for _, b := range sim {
		if b.Component == "eventbus.fabric" {
			found = true
			if b.Mode != capability.ModeSimulated {
				t.Fatalf("memory bus fabric must be reported simulated, got %s", b.Mode)
			}
			if b.Driver != "memory" {
				t.Fatalf("driver should be memory, got %s", b.Driver)
			}
		}
	}
	if !found {
		t.Fatalf("eventbus.fabric not recorded in capability registry")
	}

	// Under production, a simulated backend must fail fast (honesty framework).
	prod := capability.NewRegistry(runmode.Production)
	if err := r.ReportCapability(prod); err == nil {
		t.Fatalf("production policy must reject a simulated messaging backend")
	}
}

func TestFabric_ReportsSimulatedForNATSFallback(t *testing.T) {
	// NATS pointed at a dead port degrades to the in-memory fallback, which must
	// be reported as simulated, not real.
	cfg := Config{Backend: "nats", NATSURL: "nats://127.0.0.1:14223", MaxRetries: 1}
	bus, err := NewNATSBus(cfg, quietLogger())
	if err != nil {
		t.Fatalf("NewNATSBus: %v", err)
	}
	defer func() { _ = bus.Close() }()

	r := NewWellRouter(bus, 4, quietLogger())
	reg := capability.NewRegistry(runmode.Degraded)
	if err := r.ReportCapability(reg); err != nil {
		t.Fatalf("report under degraded policy: %v", err)
	}
	if !reg.HasSimulated() {
		t.Fatalf("NATS fallback must be reported as simulated")
	}
}
