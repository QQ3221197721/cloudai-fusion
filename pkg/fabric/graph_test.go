package fabric_test

import (
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/fabric"
)

// graph_test.go proves the MF continuation: PCA-emitted receipts across three
// pillars, correlated by a shared key, form one Verifiable Knowledge Graph where
// Lineage(key) returns the cross-pillar evidence at once - and the receipts remain
// normal, chain-verifying signed receipts.

func TestVKG_CrossPillarLineage(t *testing.T) {
	ctx := context.Background()
	ledger := newLedger(t) // helper from fabric_test.go (same test package)

	emit := func(intent, pillar, subject string, corr ...string) {
		in, err := fabric.PCA{
			Intent: intent, Pillar: pillar, Subject: subject, Correlations: corr,
			Actor: pillar, Payload: map[string]any{"note": intent},
		}.RecordInput()
		if err != nil {
			t.Fatalf("pca %s: %v", intent, err)
		}
		if _, err := ledger.Record(ctx, in); err != nil {
			t.Fatalf("record %s: %v", intent, err)
		}
	}

	// Three pillars implicated in one incident; plus one unrelated receipt.
	emit("redteam.finding", "redteam", "asset-x", "incident-42", "node-7")
	emit("schedule.bind", "cloud-native", "wl-1", "incident-42")
	emit("delivery.deploy", "delivery", "api", "incident-42")
	emit("finops.reclaim", "cloud-native", "n/g", "other")

	all, _ := ledger.Store().All(ctx)
	g := fabric.NewGraph(fabric.PCACorrelations)
	g.AddAll(all)

	// One query, cross-pillar: everything about incident-42.
	inc := g.Lineage("incident-42")
	if len(inc) != 3 {
		t.Fatalf("incident-42 lineage = %d, want 3 (cross-pillar)", len(inc))
	}
	seen := map[string]bool{}
	for _, e := range inc {
		seen[e.Action] = true
	}
	for _, a := range []string{"redteam.finding", "schedule.bind", "delivery.deploy"} {
		if !seen[a] {
			t.Fatalf("incident-42 lineage missing pillar action %q", a)
		}
	}

	// Narrower and unrelated keys resolve independently.
	if n := len(g.Lineage("node-7")); n != 1 {
		t.Fatalf("node-7 lineage = %d, want 1", n)
	}
	if n := len(g.Lineage("other")); n != 1 {
		t.Fatalf("other lineage = %d, want 1", n)
	}
	if n := len(g.Lineage("nope")); n != 0 {
		t.Fatalf("unknown key lineage = %d, want 0", n)
	}

	// The three incident receipts form a triangle (3 undirected edges).
	inc42Edges := 0
	for _, e := range g.Edges() {
		if e.Key == "incident-42" {
			inc42Edges++
		}
	}
	if inc42Edges != 3 {
		t.Fatalf("incident-42 edges = %d, want 3", inc42Edges)
	}

	// PCA receipts are ordinary signed receipts: the chain still verifies.
	rep, _ := evidence.VerifyChain(all, ledger.Signer().PublicKey())
	if !rep.Valid {
		t.Fatalf("chain of PCA receipts must verify, got %+v", rep)
	}
}
