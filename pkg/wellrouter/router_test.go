package wellrouter

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestLedger builds a real in-memory ledger (signed + hash-chained).
func newTestLedger(t *testing.T) (*evidence.Ledger, *evidence.MemoryStore) {
	t.Helper()
	signer, err := evidence.GenerateEphemeralSigner()
	require.NoError(t, err)
	store := evidence.NewMemoryStore()
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{
		Store:    store,
		Signer:   signer,
		Anchorer: evidence.NewSimulatedAnchorer(),
	})
	require.NoError(t, err)
	return ledger, store
}

// newTestRouter builds a router over a fresh memory bus + temp store.
func newTestRouter(t *testing.T, ledger *evidence.Ledger) (*FSMWellRouter, eventbus.EventBus) {
	t.Helper()
	bus := eventbus.New(eventbus.DefaultConfig(), nil)
	r, err := NewFSMWellRouter(bus, ledger, t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	return r, bus
}

// newWellEvent builds a well event with the given source/hop metadata.
func newWellEvent(t *testing.T, src eventbus.DeepWell, hop int, corr string) *eventbus.Event {
	t.Helper()
	ev, err := eventbus.NewEvent(eventbus.TopicWellEvent, "test", src.String(),
		eventbus.WellEvent{Well: src, Kind: "test"})
	require.NoError(t, err)
	ev.WithMetadata(MetaWell, strconv.Itoa(int(src))).
		WithMetadata(MetaWellName, src.String()).
		WithMetadata(MetaHop, strconv.Itoa(hop))
	if corr != "" {
		ev.CorrelationID = corr
	}
	return ev
}

// deleteDefaultRuleFor removes the default rule of the given source well.
func deleteDefaultRuleFor(t *testing.T, ctx context.Context, r *FSMWellRouter, src eventbus.DeepWell) {
	t.Helper()
	for _, rr := range r.ListRules() {
		if rr.SourceWell == src {
			require.NoError(t, r.DeleteRule(ctx, rr.ID))
			return
		}
	}
}

// ----------------------------------------------------------------------------
// 1. Default rule set compiles the connectivity matrix correctly.
// ----------------------------------------------------------------------------

func TestDefaultRuleSetMatchesConnectivityMatrix(t *testing.T) {
	rules := DefaultRules()
	allWells := eventbus.AllWells()

	// One aggregated rule per source well that has downstream edges.
	require.Len(t, rules, len(allWells), "one rule per well in AllWells()")

	totalTargets := 0
	bySource := map[eventbus.DeepWell]*RouteRule{}
	for _, rr := range rules {
		bySource[rr.SourceWell] = rr
		totalTargets += len(rr.TargetWells)
		assert.Equal(t, eventbus.TopicWellEvent, rr.TopicPattern)
		assert.Equal(t, DefaultMaxHops, rr.MaxHops)
		assert.True(t, rr.Enabled)
		assert.True(t, rr.DLQ)
		assert.Regexp(t, `^rule-[0-9a-f]{8}$`, rr.ID)
	}

	// Total edges must equal the matrix's directed edge count (39).
	assert.Equal(t, DefaultRuleEdgeCount(), totalTargets)
	assert.Equal(t, 39, totalTargets, "eventbus connectivity matrix has 39 edges")

	// Per-well targets must equal DownstreamWells exactly.
	for _, src := range allWells {
		rr := bySource[src]
		require.NotNil(t, rr, "missing rule for %s", src)
		want := eventbus.DownstreamWells(src)
		assert.Equal(t, want, rr.TargetWells, "%s targets", src)
	}
}

// ----------------------------------------------------------------------------
// 2. Hop bound: maxHops=2 rule rejects hop=2 events (hop >= MaxHops).
// ----------------------------------------------------------------------------

func TestHopBoundGuaranteed(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t, nil)
	l3 := eventbus.WellEndpoint

	deleteDefaultRuleFor(t, ctx, r, l3)
	require.NoError(t, r.AddRule(ctx, &RouteRule{
		TopicPattern: eventbus.TopicWellEvent,
		SourceWell:   l3,
		TargetWells:  []eventbus.DeepWell{eventbus.WellNetwork},
		MaxHops:      2,
		Enabled:      true,
		DLQ:          true,
	}))

	// hop=1 (< 2) forwards.
	require.NoError(t, r.Publish(ctx, newWellEvent(t, l3, 1, "hopbound-ok")))

	// hop=2 (>= 2) must be rejected.
	err := r.Publish(ctx, newWellEvent(t, l3, 2, "hopbound-rej"))
	assert.ErrorIs(t, err, ErrHopLimitExceeded)

	stats := r.Stats()
	assert.Equal(t, int64(1), stats.Rejected)
	assert.Equal(t, int64(1), stats.Forwarded)
	assert.Equal(t, int64(1), stats.DLQ)
}

// ----------------------------------------------------------------------------
// 3. Storm injection: 1000 over-hop events → all rejected, zero forwards,
//    no panic (concurrent publishers included).
// ----------------------------------------------------------------------------

func TestStormInjectionAllRejected(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t, nil)
	l1 := eventbus.WellIntel

	deleteDefaultRuleFor(t, ctx, r, l1)
	l1Targets := eventbus.DownstreamWells(l1)
	require.NoError(t, r.AddRule(ctx, &RouteRule{
		TopicPattern: eventbus.TopicWellEvent,
		SourceWell:   l1,
		TargetWells:  l1Targets,
		MaxHops:      2,
		Enabled:      true,
		DLQ:          true,
	}))

	const n = 1000
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = r.Publish(ctx, newWellEvent(t, l1, 2, "storm-"+strconv.Itoa(i)))
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		assert.ErrorIs(t, err, ErrHopLimitExceeded, "event %d", i)
	}

	stats := r.Stats()
	assert.Equal(t, int64(n), stats.Rejected, "all 1000 rejected")
	assert.Equal(t, int64(0), stats.Forwarded, "zero forwards")
	assert.Equal(t, int64(n), stats.DLQ, "all dead-lettered")

	dlq := r.DLQList(n)
	require.Len(t, dlq, n)
	assert.Equal(t, StatusRejected, dlq[0].Status)
}

// ----------------------------------------------------------------------------
// 4. Rejected events land in the queryable DLQ with rule id and reason.
// ----------------------------------------------------------------------------

func TestDLQContainsRejectedEvents(t *testing.T) {
	ctx := context.Background()
	ledger, _ := newTestLedger(t)
	r, _ := newTestRouter(t, ledger)
	l3 := eventbus.WellEndpoint

	deleteDefaultRuleFor(t, ctx, r, l3)
	require.NoError(t, r.AddRule(ctx, &RouteRule{
		TopicPattern: eventbus.TopicWellEvent,
		SourceWell:   l3,
		TargetWells:  []eventbus.DeepWell{eventbus.WellNetwork},
		MaxHops:      1,
		Enabled:      true,
		DLQ:          true,
	}))

	ev := newWellEvent(t, l3, 1, "dlq-corr")
	require.ErrorIs(t, r.Publish(ctx, ev), ErrHopLimitExceeded)

	dlq := r.DLQList(10)
	require.Len(t, dlq, 1)
	rej := dlq[0]
	assert.Equal(t, ev.ID, rej.EventID)
	assert.Equal(t, StatusRejected, rej.Status)
	assert.Equal(t, "dlq-corr", rej.CorrelationID)
	assert.Contains(t, rej.Reason, "hop limit exceeded")
	require.Len(t, rej.Trace, 1)
	assert.Equal(t, l3.String(), rej.Trace[0].Well)
}

// ----------------------------------------------------------------------------
// 5. Trace chain: derived events carry hop+1 and the appended trace.
// ----------------------------------------------------------------------------

func TestTraceChainAppendsPerHop(t *testing.T) {
	ctx := context.Background()
	ledger, _ := newTestLedger(t)
	r, bus := newTestRouter(t, ledger)

	l1 := eventbus.WellIntel
	received := make(chan *eventbus.Event, 16)
	_, err := bus.Subscribe(eventbus.TopicWellEvent, func(_ context.Context, e *eventbus.Event) error {
		if e.Source == "wellrouter-test" { // ignore our own publisher-side artifacts
			return nil
		}
		received <- e
		return nil
	})
	require.NoError(t, err)

	ev := newWellEvent(t, l1, 0, "trace-corr-1")
	require.NoError(t, r.Publish(ctx, ev))

	select {
	case fwd := <-received:
		assert.Equal(t, "trace-corr-1", fwd.CorrelationID, "CorrelationID threads through")
		assert.Equal(t, ev.ID, fwd.CausationID, "CausationID equals original event ID")
		assert.Equal(t, "1", fwd.Metadata[MetaHop], "hop incremented to 1")
		assert.Equal(t, l1.String(), fwd.Metadata[MetaForwardedFrom])

		var trace []HopRecord
		require.NoError(t, json.Unmarshal([]byte(fwd.Metadata[MetaTrace]), &trace))
		require.Len(t, trace, 1)
		assert.Equal(t, l1.String(), trace[0].Well)
	case <-time.After(2 * time.Second):
		t.Fatal("forwarded event not observed on the bus")
	}
}

// ----------------------------------------------------------------------------
// 6. CRUD persistence round-trip: a second instance on the same store sees
//    the same rule table (add survives; delete survives).
// ----------------------------------------------------------------------------

func TestRuleCRUDPersistenceRoundTrip(t *testing.T) {
	ctx := context.Background()
	ledger, _ := newTestLedger(t)
	root := t.TempDir()

	bus := eventbus.New(eventbus.DefaultConfig(), nil)
	r1, err := NewFSMWellRouter(bus, ledger, root)
	require.NoError(t, err)

	l1 := eventbus.WellIntel
	deleteDefaultRuleFor(t, ctx, r1, l1)
	custom := &RouteRule{
		TopicPattern: eventbus.TopicWellEvent,
		SourceWell:   l1,
		TargetWells:  []eventbus.DeepWell{eventbus.WellFinOps},
		MaxHops:      3,
		Enabled:      true,
		DLQ:          true,
	}
	require.NoError(t, r1.AddRule(ctx, custom))

	// Restart simulation: fresh instance, same store.
	r2, err := NewFSMWellRouter(bus, ledger, root)
	require.NoError(t, err)

	var found *RouteRule
	for _, rr := range r2.ListRules() {
		if rr.SourceWell == l1 {
			require.Nil(t, found, "deleted default rule must not resurrect")
			found = rr
		}
	}
	require.NotNil(t, found, "custom rule must survive restart")
	assert.Equal(t, custom.TargetWells, found.TargetWells)
	assert.Equal(t, 3, found.MaxHops)
	assert.True(t, found.Enabled)

	// Delete on r2, verify on a third instance.
	require.NoError(t, r2.DeleteRule(ctx, found.ID))
	r3, err := NewFSMWellRouter(bus, ledger, root)
	require.NoError(t, err)
	for _, rr := range r3.ListRules() {
		assert.NotEqual(t, l1, rr.SourceWell, "deleted rule must stay deleted across restarts")
	}
}

// ----------------------------------------------------------------------------
// 7. Dedup: same CorrelationID + rule + target is skipped the second time.
// ----------------------------------------------------------------------------

func TestDedupSuppressesDuplicateCorrelation(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t, nil)
	l1 := eventbus.WellIntel

	// Two events sharing one CorrelationID but distinct event IDs.
	// (generateEventID is nanosecond-based; force uniqueness for determinism.)
	evA := newWellEvent(t, l1, 0, "dedup-corr")
	evB := newWellEvent(t, l1, 0, "dedup-corr")
	evB.ID = evA.ID + "-b"
	require.NotEqual(t, evA.ID, evB.ID)

	require.NoError(t, r.Publish(ctx, evA))
	require.NoError(t, r.Publish(ctx, evB))

	stats := r.Stats()
	expected := int64(len(eventbus.DownstreamWells(l1)))
	assert.Equal(t, expected, stats.Forwarded, "only the first event's targets forwarded")
	assert.Equal(t, expected, stats.DedupSkipped, "second event's targets all deduped")

	// Different correlation → forwards again.
	require.NoError(t, r.Publish(ctx, newWellEvent(t, l1, 0, "dedup-corr-2")))
	assert.Equal(t, expected*2, r.Stats().Forwarded)
}

// ----------------------------------------------------------------------------
// 8. Attestations: rule.add / forward / hop.rejected each leave a ledger
//    record; LastAttestation exposes the newest receipt.
// ----------------------------------------------------------------------------

func TestAttestationsRecordedInLedger(t *testing.T) {
	ctx := context.Background()
	ledger, store := newTestLedger(t)
	r, _ := newTestRouter(t, ledger)
	l3 := eventbus.WellEndpoint

	deleteDefaultRuleFor(t, ctx, r, l3)
	require.NoError(t, r.AddRule(ctx, &RouteRule{
		TopicPattern: eventbus.TopicWellEvent,
		SourceWell:   l3,
		TargetWells:  []eventbus.DeepWell{eventbus.WellNetwork},
		MaxHops:      2,
		Enabled:      true,
		DLQ:          true,
	}))

	// forward attestation
	require.NoError(t, r.Publish(ctx, newWellEvent(t, l3, 0, "attest-fwd")))
	// rejection attestation
	require.ErrorIs(t, r.Publish(ctx, newWellEvent(t, l3, 2, "attest-rej")), ErrHopLimitExceeded)

	all, err := store.All(ctx)
	require.NoError(t, err)

	counts := map[string]int{}
	for _, e := range all {
		counts[e.Action]++
	}
	assert.GreaterOrEqual(t, counts["wellrouter.rule.add"], 1, "rule.add attestation")
	assert.GreaterOrEqual(t, counts["wellrouter.forward"], 1, "forward attestation")
	assert.GreaterOrEqual(t, counts["wellrouter.hop.rejected"], 1, "hop.rejected attestation")

	last := r.LastAttestation()
	require.NotNil(t, last)
	assert.NotEmpty(t, last.Hash)
	assert.NotEmpty(t, last.Signature)
}

// ----------------------------------------------------------------------------
// 9. nil-ledger degraded mode: everything works, no attestations, no crash.
// ----------------------------------------------------------------------------

func TestNilLedgerDegradedMode(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t, nil)
	l1 := eventbus.WellIntel

	assert.Nil(t, r.LastAttestation())

	require.NoError(t, r.AddRule(ctx, &RouteRule{
		TopicPattern: "custom.>",
		SourceWell:   l1,
		TargetWells:  []eventbus.DeepWell{eventbus.WellFinOps},
		MaxHops:      8,
		Enabled:      true,
		DLQ:          true,
	}))
	require.NoError(t, r.Publish(ctx, newWellEvent(t, eventbus.WellIntel, 0, "")))
	require.ErrorIs(t, r.Publish(ctx, newWellEvent(t, l1, 8, "")), ErrHopLimitExceeded)

	assert.Nil(t, r.LastAttestation(), "nil ledger must never produce receipts")
	assert.Greater(t, r.Stats().Forwarded, int64(0))
	assert.Greater(t, r.Stats().Rejected, int64(0))
	assert.NotEmpty(t, r.DLQList(1))
}

// ----------------------------------------------------------------------------
// 10. Wildcard matching mirrors eventbus semantics (* one segment, > rest).
// ----------------------------------------------------------------------------

func TestTopicWildcardMatching(t *testing.T) {
	cases := []struct {
		pattern, topic string
		want           bool
	}{
		{"aisecops.well.event", "aisecops.well.event", true},
		{"aisecops.*", "aisecops.well.event", false}, // * = ONE segment, 2 vs 3 parts
		{"aisecops.*", "aisecops.event", true},
		{"aisecops.*.event", "aisecops.well.event", true},
		{"aisecops.>", "aisecops.a.b.c", true},
		{"aisecops.well.event", "aisecops.cluster.created", false},
		{"*", "anything", true},
		{">", "deep.nested.topic.here", true},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, topicMatches(tc.pattern, tc.topic), "pattern=%q topic=%q", tc.pattern, tc.topic)
	}
}

// ----------------------------------------------------------------------------
// 11. No matching rule → ErrNoMatchingRule.
// ----------------------------------------------------------------------------

func TestNoMatchingRuleReturnsSentinel(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t, nil)

	// Valid well, but a topic no rule covers.
	ev := newWellEvent(t, eventbus.WellIntel, 0, "")
	ev.Topic = "unmatched.topic"
	assert.ErrorIs(t, r.Publish(ctx, ev), ErrNoMatchingRule)

	// Valid well + matching topic, but its rule was deleted.
	l16 := eventbus.WellNetPolicy
	deleteDefaultRuleFor(t, ctx, r, l16)
	assert.ErrorIs(t, r.Publish(ctx, newWellEvent(t, l16, 0, "")), ErrNoMatchingRule)
}

// ----------------------------------------------------------------------------
// 12. Rule validation: MaxHops cap, malformed patterns, bad wells.
// ----------------------------------------------------------------------------

func TestRuleValidation(t *testing.T) {
	good := func() *RouteRule {
		return &RouteRule{
			TopicPattern: "a.b",
			SourceWell:   eventbus.WellIntel,
			TargetWells:  []eventbus.DeepWell{eventbus.WellHunt},
			MaxHops:      4,
		}
	}

	r := good()
	require.NoError(t, r.Validate())
	assert.Regexp(t, `^rule-[0-9a-f]{8}$`, r.ID, "ID auto-filled")

	r = good()
	r.MaxHops = 9
	assert.Error(t, r.Validate(), "MaxHops > 8 must be rejected")

	r = good()
	r.MaxHops = 0
	require.NoError(t, r.Validate())
	assert.Equal(t, DefaultMaxHops, r.MaxHops, "0 defaults to 8")

	r = good()
	r.TopicPattern = "a..b"
	assert.Error(t, r.Validate(), "empty segment pattern rejected")

	r = good()
	r.SourceWell = eventbus.DeepWell(99)
	assert.Error(t, r.Validate())

	r = good()
	r.TargetWells = nil
	assert.Error(t, r.Validate())
}

// ----------------------------------------------------------------------------
// 13. AddRule duplicate ID rejected; DeleteRule unknown ID → ErrRuleNotFound.
// ----------------------------------------------------------------------------

func TestAddDuplicateAndDeleteUnknown(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t, nil)

	dup := &RouteRule{
		ID:           "rule-fixed0001",
		TopicPattern: "x.y",
		SourceWell:   eventbus.WellIntel,
		TargetWells:  []eventbus.DeepWell{eventbus.WellHunt},
	}
	require.NoError(t, r.AddRule(ctx, dup))
	assert.Error(t, r.AddRule(ctx, dup), "duplicate ID rejected")

	assert.ErrorIs(t, r.DeleteRule(ctx, "rule-doesnotexist"), ErrRuleNotFound)
}

// ----------------------------------------------------------------------------
// 14. Durable audit trail: every decision lands in <root>/wellrouter/audit.jsonl
//     (ledger-independent — survives with nil ledger).
// ----------------------------------------------------------------------------

func TestAuditLogPersistedToDisk(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t, nil) // nil ledger: audit must not depend on it
	l3 := eventbus.WellEndpoint

	deleteDefaultRuleFor(t, ctx, r, l3)
	require.NoError(t, r.AddRule(ctx, &RouteRule{
		TopicPattern: eventbus.TopicWellEvent,
		SourceWell:   l3,
		TargetWells:  []eventbus.DeepWell{eventbus.WellNetwork},
		MaxHops:      1,
		Enabled:      true,
		DLQ:          true,
	}))
	require.NoError(t, r.Publish(ctx, newWellEvent(t, l3, 0, "audit-fwd")))
	require.ErrorIs(t, r.Publish(ctx, newWellEvent(t, l3, 1, "audit-rej")), ErrHopLimitExceeded)

	raw, err := r.Store().ListAudits(ctx, 50)
	require.NoError(t, err)
	require.NotEmpty(t, raw, "audit.jsonl must contain decisions")

	actions := map[string]int{}
	for _, line := range raw {
		var rec AuditRecord
		require.NoError(t, json.Unmarshal(line, &rec), "line: %s", line)
		assert.False(t, rec.At.IsZero())
		assert.NotEmpty(t, rec.Subject)
		actions[rec.Action]++
	}
	assert.GreaterOrEqual(t, actions["wellrouter.rule.add"], 1, "rule.add audited")
	assert.GreaterOrEqual(t, actions["wellrouter.forward"], 1, "forward audited")
	assert.GreaterOrEqual(t, actions["wellrouter.hop.rejected"], 1, "rejection audited")
	assert.GreaterOrEqual(t, actions["wellrouter.rule.delete"], 1, "default-rule delete audited")
}
