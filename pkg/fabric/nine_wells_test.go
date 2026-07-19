package fabric

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// nine_wells_test.go is the executable proof of the moat's central claim
// (docs/verifiable-moat-spec.md §5.4): the nine wells are not parallel silos — they
// fuse into ONE system through the Fabric. It:
//
//  1. registers all NINE wells with a single uniform Register call each ("open for
//     extension" — the extension contract is the moat, not any one well);
//  2. emits one Proof-Carrying Action per well, correlated by shared entities, and
//     shows the Verifiable Knowledge Graph links them ACROSS all three pillars
//     ("everything touching this GPU", spanning cloud-native + red team + delivery);
//  3. runs the §5.4c cross-pillar saga (finding → quarantine → redeploy → re-prove →
//     cost) and verifies the whole workflow OFFLINE as one completeness-proven chain.
//
// If any of these regressed, the "nine interconnected wells" claim would be a slide,
// not a theorem. This test keeps it a theorem.

// keyField adapts a payload-field extractor into a Well KeyFunc.
func keyField(field string) KeyFunc {
	f := ByPayloadString(field)
	return func(e *evidence.Evidence) string {
		if ks := f(e); len(ks) > 0 {
			return ks[0]
		}
		return ""
	}
}

// nineWells returns the canonical nine wells across the three pillars.
func nineWells() []Well {
	return []Well{
		{Name: "CN-1", Prefix: "scheduler/tenant", KeyOf: keyField("well_key")},
		{Name: "CN-2", Prefix: "finops/month", KeyOf: keyField("well_key")},
		{Name: "CN-3", Prefix: "gpu/isolation", KeyOf: keyField("well_key")},
		{Name: "RT-1", Prefix: "redteam/exploit", KeyOf: keyField("well_key")},
		{Name: "RT-2", Prefix: "redteam/report", KeyOf: keyField("well_key")},
		{Name: "RT-3", Prefix: "redteam/bench", KeyOf: keyField("well_key")},
		{Name: "DL-1", Prefix: "delivery/deploy", KeyOf: keyField("well_key")},
		{Name: "DL-2", Prefix: "delivery/failover", KeyOf: keyField("well_key")},
		{Name: "DL-3", Prefix: "delivery/edge", KeyOf: keyField("well_key")},
	}
}

func TestNineWellsInterconnect(t *testing.T) {
	ctx := context.Background()
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x9e}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	l, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	f := New(l)

	// (1) Open for extension: nine wells, nine uniform Register calls, nothing else.
	for _, w := range nineWells() {
		if err := f.Register(w); err != nil {
			t.Fatalf("register %s: %v", w.Name, err)
		}
	}
	if got := len(f.Wells()); got != 9 {
		t.Fatalf("expected 9 registered wells, got %d", got)
	}

	// (2) Each well emits ONE PCA. Shared entities (a GPU, an engagement, a tenant)
	// appear in multiple wells' correlations so the VKG links them cross-pillar.
	const gpu = "gpu-A100-7"
	const eng = "ENG-1"
	const tenant = "tenant-42"
	emit := func(intent, pillar, wellKey string, corr []string) *evidence.Evidence {
		in, err := PCA{
			Intent: intent, Pillar: pillar, Correlations: corr, Subject: wellKey,
			Payload: map[string]any{"well_key": wellKey},
		}.RecordInput()
		if err != nil {
			t.Fatalf("pca %s: %v", intent, err)
		}
		rec, err := l.Record(ctx, in)
		if err != nil {
			t.Fatalf("record %s: %v", intent, err)
		}
		return rec
	}

	// Cloud-native pillar
	emit("schedule.bind", "cloud-native", tenant, []string{tenant, gpu})     // CN-1: bound on the GPU
	emit("finops.reclaim", "cloud-native", "2026-07", []string{tenant, gpu}) // CN-2: cost of that GPU
	emit("gpu.isolation.attest", "cloud-native", gpu, []string{gpu})         // CN-3: isolation claim on the GPU
	// Red-team pillar
	emit("redteam.exploit.proof", "redteam", eng, []string{eng, gpu}) // RT-1: attacked the GPU isolation
	emit("redteam.report", "redteam", eng, []string{eng})             // RT-2: complete report
	emit("redteam.bench", "redteam", "cve-tier2", []string{eng})      // RT-3: capability bench
	// Delivery pillar
	emit("delivery.deploy", "delivery", eng, []string{eng, gpu}) // DL-1: redeployed patched config to the GPU node
	emit("delivery.failover", "delivery", "cluster-east", []string{"cluster-east"})
	emit("delivery.edge", "delivery", "edge-9", []string{"edge-9"})

	all, err := l.Store().All(ctx)
	if err != nil {
		t.Fatalf("all: %v", err)
	}

	// The VKG projects the signed ledger; correlate by explicit PCA keys + Subject.
	g := NewGraph(PCACorrelations, BySubject)
	g.AddAll(all)

	// (2a) One query returns EVERYTHING touching the GPU — across all three pillars.
	lineage := g.Lineage(gpu)
	pillars := map[string]bool{}
	intents := map[string]bool{}
	for _, e := range lineage {
		intents[e.Action] = true
		var env struct {
			PCA struct {
				Pillar string `json:"pillar"`
			} `json:"pca"`
		}
		if err := json.Unmarshal(e.Payload, &env); err == nil {
			pillars[env.PCA.Pillar] = true
		}
	}
	// CN-1, CN-2, CN-3, RT-1, DL-1 all touch the GPU: cloud-native + redteam + delivery.
	for _, want := range []string{"schedule.bind", "finops.reclaim", "gpu.isolation.attest", "redteam.exploit.proof", "delivery.deploy"} {
		if !intents[want] {
			t.Errorf("GPU lineage missing cross-pillar action %q", want)
		}
	}
	if len(pillars) < 3 {
		t.Fatalf("GPU lineage spans %d pillars, want 3 (cloud-native, redteam, delivery): %v", len(pillars), pillars)
	}

	// (2b) The engagement lineage fuses RT-1/RT-2/RT-3 + DL-1 (report ↔ deploy).
	if got := len(g.Lineage(eng)); got < 4 {
		t.Fatalf("engagement lineage has %d receipts, want >= 4", got)
	}
	if len(g.Edges()) == 0 {
		t.Fatal("VKG has no edges — wells are not interconnected")
	}

	// (3) The §5.4c cross-pillar saga, verified offline as ONE chain.
	ch := NewChoreographer(l, nil)
	res, err := ch.Run(ctx, "moat-saga", []SagaStep{
		{Name: "rt.find-isolation-break", Do: func(context.Context) error { return nil }},
		{Name: "cn.quarantine-mig-slice", Do: func(context.Context) error { return nil }},
		{Name: "dl.redeploy-patched", Do: func(context.Context) error { return nil }},
		{Name: "rt.replay-witness-fixed", Do: func(context.Context) error { return nil }},
		{Name: "cn.close-finops-cost", Do: func(context.Context) error { return nil }},
	})
	if err != nil || !res.Completed || res.Steps != 5 {
		t.Fatalf("saga run: completed=%v steps=%d err=%v", res.Completed, res.Steps, err)
	}
	if _, err := ch.Seal(ctx, "moat-saga"); err != nil {
		t.Fatalf("seal saga: %v", err)
	}
	proof, err := ch.Proof(ctx, "moat-saga")
	if err != nil {
		t.Fatalf("saga proof: %v", err)
	}
	if err := evidence.VerifyCompleteness(proof, signer.PublicKey()); err != nil {
		t.Fatalf("cross-pillar saga must verify offline as one chain: %v", err)
	}
	out := SagaOutcomeOf(proof)
	if !out.Completed || out.Steps != 5 {
		t.Fatalf("verified saga outcome: completed=%v steps=%d, want completed with 5 steps", out.Completed, out.Steps)
	}
}
