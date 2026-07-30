package fabric

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// sixteen_wells_test.go is the executable proof that the Verifiable Fabric's
// "closed under composition / open for extension" invariant scales from the nine
// flagship moat wells (nine_wells_test.go) to ALL SIXTEEN AISecOps deep wells
// (docs/aisecops-subsystem-spec.md; pkg/eventbus/deepwell.go). It is the M1
// milestone of the "depth matches breadth" plan: every well — Intelligence
// (L1-L2), Operations (L3-L8) and Foundation (L9-L16) — registers with ONE
// uniform Register call, emits a Proof-Carrying Action, is linked cross-layer by
// the Verifiable Knowledge Graph, and participates in a kill-chain saga that is
// verified OFFLINE as a single completeness-proven chain.
//
// This changes no backend: it uses synthetic evidence to prove the Fabric
// machinery (seal / completeness / VKG / choreographer) is uniform across all 16
// wells. Later milestones give each foundation well a real backend and real
// receipts; this test guarantees the spine they plug into already scales to 16.

// sixteenWells returns the canonical 16 AISecOps deep wells across three layers,
// each onboarded with the same single-predicate Register contract.
func sixteenWells() []Well {
	return []Well{
		// Intelligence layer (L1-L2)
		{Name: "L1-intel", Prefix: "well/l1/intel", KeyOf: keyField("well_key")},
		{Name: "L2-hunt", Prefix: "well/l2/hunt", KeyOf: keyField("well_key")},
		// Operations layer (L3-L8)
		{Name: "L3-endpoint", Prefix: "well/l3/endpoint", KeyOf: keyField("well_key")},
		{Name: "L4-network", Prefix: "well/l4/network", KeyOf: keyField("well_key")},
		{Name: "L5-workload", Prefix: "well/l5/workload", KeyOf: keyField("well_key")},
		{Name: "L6-identity", Prefix: "well/l6/identity", KeyOf: keyField("well_key")},
		{Name: "L7-image", Prefix: "well/l7/image", KeyOf: keyField("well_key")},
		{Name: "L8-response", Prefix: "well/l8/response", KeyOf: keyField("well_key")},
		// Foundation layer (L9-L16)
		{Name: "L9-data", Prefix: "well/l9/data", KeyOf: keyField("well_key")},
		{Name: "L10-compute", Prefix: "well/l10/compute", KeyOf: keyField("well_key")},
		{Name: "L11-model", Prefix: "well/l11/model", KeyOf: keyField("well_key")},
		{Name: "L12-inference", Prefix: "well/l12/inference", KeyOf: keyField("well_key")},
		{Name: "L13-evidence", Prefix: "well/l13/evidence", KeyOf: keyField("well_key")},
		{Name: "L14-redteam", Prefix: "well/l14/redteam", KeyOf: keyField("well_key")},
		{Name: "L15-finops", Prefix: "well/l15/finops", KeyOf: keyField("well_key")},
		{Name: "L16-netpolicy", Prefix: "well/l16/netpolicy", KeyOf: keyField("well_key")},
	}
}

func TestSixteenWellsInterconnect(t *testing.T) {
	ctx := context.Background()
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x16}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	l, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	f := New(l)

	// (1) Open for extension: 16 wells, 16 uniform Register calls, nothing else.
	for _, w := range sixteenWells() {
		if err := f.Register(w); err != nil {
			t.Fatalf("register %s: %v", w.Name, err)
		}
	}
	if got := len(f.Wells()); got != 16 {
		t.Fatalf("expected 16 registered wells, got %d", got)
	}

	// (2) Each well emits ONE PCA. A single incident threads the AISecOps
	// kill-chain across all three layers so the VKG fuses them; a tenant/GPU
	// threads the compute/finops/redteam foundation wells.
	const incident = "INC-1"
	const host = "host-7"
	const tenant = "tenant-42"
	const gpu = "gpu-A100-7"
	emit := func(intent, layer, wellKey string, corr []string) {
		in, err := PCA{
			Intent: intent, Pillar: layer, Correlations: corr, Subject: wellKey,
			Payload: map[string]any{"well_key": wellKey},
		}.RecordInput()
		if err != nil {
			t.Fatalf("pca %s: %v", intent, err)
		}
		if _, err := l.Record(ctx, in); err != nil {
			t.Fatalf("record %s: %v", intent, err)
		}
	}

	// Intelligence layer
	emit("intel.cve.ingested", "intelligence", incident, []string{incident, host})
	emit("hunt.correlate", "intelligence", incident, []string{incident, host})
	// Operations layer (L3-L8) — detection kill-chain converging on L8 response
	emit("detect.endpoint", "operations", incident, []string{incident, host})
	emit("detect.network", "operations", incident, []string{incident, host})
	emit("detect.workload", "operations", incident, []string{incident})
	emit("detect.identity", "operations", incident, []string{incident})
	emit("detect.image", "operations", incident, []string{incident})
	emit("response.soar", "operations", incident, []string{incident, host})
	// Foundation layer (L9-L16)
	emit("data.retain", "foundation", incident, []string{incident})
	emit("compute.schedule", "foundation", tenant, []string{tenant, gpu})
	emit("model.provenance", "foundation", tenant, []string{tenant})
	emit("inference.replay", "foundation", tenant, []string{tenant})
	emit("evidence.anchor", "foundation", incident, []string{incident})
	emit("redteam.exploit.proof", "foundation", incident, []string{incident, gpu})
	emit("finops.reclaim", "foundation", "2026-07", []string{tenant, gpu})
	emit("netpolicy.isolate", "foundation", incident, []string{incident, host})

	all, err := l.Store().All(ctx)
	if err != nil {
		t.Fatalf("all: %v", err)
	}

	// (2a) The VKG projects the signed ledger; correlate by explicit PCA keys +
	// Subject. One query on the incident returns the whole kill-chain across ALL
	// THREE layers (intelligence + operations + foundation).
	g := NewGraph(PCACorrelations, BySubject)
	g.AddAll(all)
	lineage := g.Lineage(incident)
	layers := map[string]bool{}
	intents := map[string]bool{}
	for _, e := range lineage {
		intents[e.Action] = true
		var env struct {
			PCA struct {
				Pillar string `json:"pillar"`
			} `json:"pca"`
		}
		if err := json.Unmarshal(e.Payload, &env); err == nil {
			layers[env.PCA.Pillar] = true
		}
	}
	for _, want := range []string{"intel.cve.ingested", "detect.endpoint", "response.soar", "evidence.anchor", "netpolicy.isolate"} {
		if !intents[want] {
			t.Errorf("incident lineage missing cross-layer action %q", want)
		}
	}
	if len(layers) < 3 {
		t.Fatalf("incident lineage spans %d layers, want 3 (intelligence, operations, foundation): %v", len(layers), layers)
	}
	if len(g.Edges()) == 0 {
		t.Fatal("VKG has no edges — the 16 wells are not interconnected")
	}

	// (2b) The foundation compute/finops/redteam wells fuse on the shared GPU.
	if got := len(g.Lineage(gpu)); got < 3 {
		t.Fatalf("gpu lineage has %d receipts, want >= 3 (compute, finops, redteam)", got)
	}

	// (3) A six-step kill-chain saga spanning all three layers, verified OFFLINE
	// as ONE completeness-proven chain (L1 intel → L3 detect → L8 respond →
	// L16 isolate → L13 anchor → L9 retain).
	ch := NewChoreographer(l, nil)
	res, err := ch.Run(ctx, "killchain-saga", []SagaStep{
		{Name: "l1.intel-ingest", Do: func(context.Context) error { return nil }},
		{Name: "l3.endpoint-detect", Do: func(context.Context) error { return nil }},
		{Name: "l8.soar-respond", Do: func(context.Context) error { return nil }},
		{Name: "l16.network-isolate", Do: func(context.Context) error { return nil }},
		{Name: "l13.evidence-anchor", Do: func(context.Context) error { return nil }},
		{Name: "l9.data-retain", Do: func(context.Context) error { return nil }},
	})
	if err != nil || !res.Completed || res.Steps != 6 {
		t.Fatalf("saga run: completed=%v steps=%d err=%v", res.Completed, res.Steps, err)
	}
	if _, err := ch.Seal(ctx, "killchain-saga"); err != nil {
		t.Fatalf("seal saga: %v", err)
	}
	proof, err := ch.Proof(ctx, "killchain-saga")
	if err != nil {
		t.Fatalf("saga proof: %v", err)
	}
	if err := evidence.VerifyCompleteness(proof, signer.PublicKey()); err != nil {
		t.Fatalf("kill-chain saga must verify offline as one chain: %v", err)
	}
	out := SagaOutcomeOf(proof)
	if !out.Completed || out.Steps != 6 {
		t.Fatalf("verified saga outcome: completed=%v steps=%d, want completed with 6 steps", out.Completed, out.Steps)
	}
}
