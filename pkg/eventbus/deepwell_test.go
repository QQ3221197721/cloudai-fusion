package eventbus

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestDeepWell_TaxonomyComplete(t *testing.T) {
	wells := AllWells()
	if len(wells) != 16 {
		t.Fatalf("expected exactly 16 deep wells, got %d", len(wells))
	}
	// Ascending, contiguous L1..L16.
	for i, w := range wells {
		if int(w) != i+1 {
			t.Fatalf("wells not contiguous/ordered at %d: %v", i, w)
		}
		if !w.Valid() {
			t.Fatalf("well %v reported invalid", w)
		}
		if w.String() == "" {
			t.Fatalf("well %d has empty name", int(w))
		}
	}
	if DeepWell(0).Valid() || DeepWell(17).Valid() {
		t.Fatalf("out-of-range wells must be invalid")
	}
}

func TestConnectivity_EveryWellConnected(t *testing.T) {
	// Fabric requirement: every well must participate — either it has downstream
	// edges (a source) or it is a downstream of some other well (a sink).
	hasOut := map[DeepWell]bool{}
	hasIn := map[DeepWell]bool{}
	for _, src := range AllWells() {
		ds := DownstreamWells(src)
		if len(ds) > 0 {
			hasOut[src] = true
		}
		for _, d := range ds {
			if !d.Valid() {
				t.Fatalf("well %v has invalid downstream %v", src, d)
			}
			hasIn[d] = true
		}
	}
	for _, w := range AllWells() {
		if !hasOut[w] && !hasIn[w] {
			t.Fatalf("well %v is isolated (no in/out edges) — fabric not fully connected", w)
		}
	}
}

func TestConnectivity_KnownEdges(t *testing.T) {
	// Spot-check the designed intelligence fan-out and response escalation.
	if !IsConnected(WellIntel, WellHunt) {
		t.Fatalf("L1 intel must feed L2 hunt")
	}
	if !IsConnected(WellIntel, WellRedTeam) {
		t.Fatalf("L1 intel must feed L14 red-team")
	}
	if !IsConnected(WellEndpoint, WellResponse) {
		t.Fatalf("L3 endpoint must escalate to L8 response")
	}
	if !IsConnected(WellResponse, WellEvidence) {
		t.Fatalf("L8 response must record into L13 evidence")
	}
	if IsConnected(WellImage, WellIntel) {
		t.Fatalf("unexpected edge L7 image → L1 intel")
	}
}

// collector captures well events delivered on the fabric for assertions.
type collector struct {
	mu     sync.Mutex
	events []*WellEvent
	byWell map[DeepWell]int
}

func newCollector() *collector { return &collector{byWell: map[DeepWell]int{}} }

func (c *collector) handle(_ context.Context, ev *Event) error {
	// The routed target well lives in metadata (the payload keeps the origin
	// well). Count by target so we can assert cross-well delivery.
	well := WellIntel
	if raw, ok := ev.Metadata[mdWell]; ok {
		if n, err := strconv.Atoi(raw); err == nil {
			well = DeepWell(n)
		}
	}
	c.mu.Lock()
	c.events = append(c.events, &WellEvent{Well: well})
	c.byWell[well]++
	c.mu.Unlock()
	return nil
}

func (c *collector) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.events)
}

func waitFor(t *testing.T, cond func() bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !cond() {
		t.Fatalf("condition not met within %s", timeout)
	}
}

func TestWellRouter_PropagatesDownstream(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)
	bus := NewMemoryBus(DefaultConfig(), logger)
	defer func() { _ = bus.Close() }()

	col := newCollector()
	if _, err := bus.Subscribe(TopicWellEvent, col.handle); err != nil {
		t.Fatalf("subscribe collector: %v", err)
	}

	router := NewWellRouter(bus, 4, logger)
	if err := router.Connect(context.Background()); err != nil {
		t.Fatalf("router connect: %v", err)
	}

	// Emit one L1 intel event; the router must fan it out to L1's downstreams
	// (L2, L3, L4, L14) and beyond, bounded by MaxHops.
	if err := PublishWellEvent(context.Background(), bus, WellIntel, "cve_ingested",
		map[string]any{"cve": "CVE-2024-0001"}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// The original event + forwarded derivatives arrive. Expect the direct
	// downstreams of L1 to each receive at least one event.
	waitFor(t, func() bool {
		col.mu.Lock()
		defer col.mu.Unlock()
		return col.byWell[WellHunt] > 0 && col.byWell[WellEndpoint] > 0 &&
			col.byWell[WellNetwork] > 0 && col.byWell[WellRedTeam] > 0
	}, 2*time.Second)

	if router.ForwardedCount() == 0 {
		t.Fatalf("router should have forwarded at least the direct downstreams")
	}
}

func TestWellRouter_HopCapBoundsCycles(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)
	bus := NewMemoryBus(DefaultConfig(), logger)
	defer func() { _ = bus.Close() }()

	col := newCollector()
	if _, err := bus.Subscribe(TopicWellEvent, col.handle); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	// L1↔L2 and L1↔L14 form cycles; a small hop cap must keep the fan-out finite.
	router := NewWellRouter(bus, 2, logger)
	if err := router.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}

	if err := PublishWellEvent(context.Background(), bus, WellIntel, "cve_ingested", nil); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// Give the fabric time to quiesce, then assert it terminated (finite events).
	time.Sleep(300 * time.Millisecond)
	if got := col.count(); got == 0 {
		t.Fatalf("expected some events, got 0")
	}
	stable := col.count()
	time.Sleep(150 * time.Millisecond)
	if col.count() != stable {
		t.Fatalf("event count still growing (%d → %d): cycle not bounded", stable, col.count())
	}
}

func TestPublishWellEvent_RejectsInvalidWell(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.PanicLevel)
	bus := NewMemoryBus(DefaultConfig(), logger)
	defer func() { _ = bus.Close() }()
	if err := PublishWellEvent(context.Background(), bus, DeepWell(99), "x", nil); err == nil {
		t.Fatalf("expected error for invalid well")
	}
}
