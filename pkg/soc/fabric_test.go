package soc

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/eventbus"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
)

// TestClosedLoop_DetectionAutoTriggersResponse wires the real event fabric
// (WellRouter) to the SOC engine exactly as the composition root does, then
// proves a single L4 detection AUTOMATICALLY drives an evidence-signed L8 SOAR
// response — with no manual Respond call — and that multi-path fan-in on the
// fabric responds at most once (idempotent).
func TestClosedLoop_DetectionAutoTriggersResponse(t *testing.T) {
	t.Cleanup(capability.Reset)
	ctx := context.Background()

	// A real, recording evidence ledger so we can assert the response was signed.
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x33}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}

	// L1 intel with a known-malicious IP so L4 produces a finding.
	store := intel.NewMemoryStore()
	if uerr := store.UpsertIOCs([]intel.IOCEntry{
		{IOCType: "ip", Value: "203.0.113.9", Severity: intel.SeverityCritical},
	}); uerr != nil {
		t.Fatalf("seed ioc: %v", uerr)
	}

	// Fabric + router, wired as in main.go.
	bus := eventbus.NewMemoryBus(eventbus.DefaultConfig(), nil)
	defer func() { _ = bus.Close() }()
	router := eventbus.NewWellRouter(bus, 4, nil)
	if cerr := router.Connect(ctx); cerr != nil {
		t.Fatalf("router connect: %v", cerr)
	}

	eng := NewEngine(store, nil)
	eng.SetEvidenceRecorder(ledger)
	eng.SetWellPublisher(func(c context.Context, well int, kind string, detail map[string]any) {
		_ = eventbus.PublishWellEvent(c, bus, eventbus.DeepWell(well), kind, detail)
	})

	// L8 auto-consumer (identical glue to the composition root).
	if _, serr := bus.Subscribe(eventbus.TopicWellEvent, func(c context.Context, ev *eventbus.Event) error {
		w, ok := eventbus.WellOf(ev)
		if !ok || w != eventbus.WellResponse {
			return nil
		}
		var we eventbus.WellEvent
		if uerr := ev.UnmarshalData(&we); uerr != nil {
			return nil
		}
		ids, _ := we.Detail["finding_ids"].(string)
		if ids == "" {
			return nil
		}
		eng.OnEscalation(c, strings.Split(ids, ","))
		return nil
	}); serr != nil {
		t.Fatalf("subscribe consumer: %v", serr)
	}

	// Trigger ONE detection. Everything after is automatic via the fabric.
	f, aerr := eng.AnalyzeNetwork(ctx, "node-1", []string{"203.0.113.9"}, nil)
	if aerr != nil {
		t.Fatalf("analyze: %v", aerr)
	}
	if len(f) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(f))
	}

	// Poll the ledger for the auto-generated L8 response receipt.
	deadline := time.Now().Add(2 * time.Second)
	respCount := 0
	for time.Now().Before(deadline) {
		all, _ := ledger.Store().All(ctx)
		respCount = 0
		for _, ev := range all {
			if ev.Action == "soc.respond" {
				respCount++
			}
		}
		if respCount >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if respCount == 0 {
		t.Fatalf("detection did not auto-trigger an L8 response receipt")
	}

	// Let any remaining multi-path fan-in settle, then assert idempotency:
	// exactly ONE response for the single finding despite L3/L4→L8 fan-in.
	time.Sleep(200 * time.Millisecond)
	all, _ := ledger.Store().All(ctx)
	final := 0
	for _, ev := range all {
		if ev.Action == "soc.respond" {
			final++
		}
	}
	if final != 1 {
		t.Fatalf("expected exactly 1 auto-response (idempotent), got %d", final)
	}
}

// TestOnEscalation_DedupeAndUnknown covers OnEscalation directly: duplicates and
// unknown IDs are skipped, and a known finding responds once.
func TestOnEscalation_DedupeAndUnknown(t *testing.T) {
	t.Cleanup(capability.Reset)
	ctx := context.Background()
	store := intel.NewMemoryStore()
	_ = store.UpsertIOCs([]intel.IOCEntry{{IOCType: "ip", Value: "10.0.0.9", Severity: intel.SeverityHigh}})
	eng := NewEngine(store, nil)

	f, err := eng.AnalyzeNetwork(ctx, "h", []string{"10.0.0.9"}, nil)
	if err != nil || len(f) != 1 {
		t.Fatalf("seed finding: %v (%d)", err, len(f))
	}
	id := f[0].ID

	// Same id twice + an unknown id → exactly one response, unknown skipped.
	resps := eng.OnEscalation(ctx, []string{id, id, "does-not-exist"})
	if len(resps) != 1 {
		t.Fatalf("expected 1 response (dedupe + skip unknown), got %d", len(resps))
	}
	// A subsequent escalation of the same id is a no-op (already responded).
	if again := eng.OnEscalation(ctx, []string{id}); len(again) != 0 {
		t.Fatalf("re-escalation of a responded finding must be a no-op, got %d", len(again))
	}
}
