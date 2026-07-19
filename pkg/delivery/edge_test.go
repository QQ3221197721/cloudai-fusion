package delivery

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// edge_test.go proves DL-3: a reconnected edge node's reconciliation verifies
// offline; a node CANNOT lie about compliance (the verifier recomputes from the
// embedded chain, defeating even a re-signed false claim); tampering fails; and
// the recorded receipt is chain-verifying.

func buildChain(t *testing.T, node string, policyOK ...bool) *EdgeChain {
	t.Helper()
	c := NewEdgeChain(node)
	for i, ok := range policyOK {
		if _, err := c.Append("edge.action", ok); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	return c
}

func TestEdgeReconciliation_CompliantAndTamper(t *testing.T) {
	l := newDeliveryLedger(t, 0x86)
	pub := l.Signer().PublicKey()

	// All offline decisions in-policy -> reconciliation verifies + proves compliance.
	ok, err := BuildReconciliation(l.Signer(), buildChain(t, "edge-7", true, true, true))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := VerifyEdgeReconciliation(ok, pub); err != nil {
		t.Fatalf("valid reconciliation must verify: %v", err)
	}
	if err := ok.ProvesCompliance(); err != nil {
		t.Fatalf("all-in-policy chain must prove compliance: %v", err)
	}

	// One out-of-policy decision -> signed + verifies, but compliance gate fails.
	nc, err := BuildReconciliation(l.Signer(), buildChain(t, "edge-8", true, false, true))
	if err != nil {
		t.Fatalf("build non-compliant: %v", err)
	}
	if err := VerifyEdgeReconciliation(nc, pub); err != nil {
		t.Fatalf("non-compliant reconciliation is still validly signed: %v", err)
	}
	if err := nc.ProvesCompliance(); err == nil {
		t.Fatal("an out-of-policy offline decision MUST fail the compliance gate")
	}

	// A node cannot LIE: claim compliant over a non-compliant chain, even re-signed.
	liar := *nc
	liar.AllPolicyCompliant = true
	liar.Signature = ""
	sig, err := evidence.SignStatement(l.Signer(), edgeDomain, liar)
	if err != nil {
		t.Fatalf("re-sign: %v", err)
	}
	liar.Signature = sig
	if err := VerifyEdgeReconciliation(&liar, pub); err == nil {
		t.Fatal("a re-signed FALSE compliance claim MUST fail (verifier recomputes from the chain)")
	}

	// Tamper a decision's action after signing -> signature fails.
	bad := *ok
	bad.Decisions = append([]EdgeDecision(nil), ok.Decisions...)
	bad.Decisions[1].Action = "edge.tampered"
	if err := VerifyEdgeReconciliation(&bad, pub); err == nil {
		t.Fatal("tampered offline decision MUST fail verification")
	}
}

func TestEdgeReconciliation_RecordAndKey(t *testing.T) {
	ctx := context.Background()
	l := newDeliveryLedger(t, 0x87)
	pub := l.Signer().PublicKey()

	att, err := BuildReconciliation(l.Signer(), buildChain(t, "edge-9", true, true))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	ev, err := RecordReconciliation(ctx, l, att)
	if err != nil || ev == nil {
		t.Fatalf("record: ev=%v err=%v", ev, err)
	}
	if got := EdgeNodeKeyOf(ev); got != "edge-9" {
		t.Fatalf("EdgeNodeKeyOf = %q, want edge-9", got)
	}

	all, _ := l.Store().All(ctx)
	rep, _ := evidence.VerifyChain(all, pub)
	if !rep.Valid {
		t.Fatalf("chain with recorded reconciliation must verify, got %+v", rep)
	}
	var got EdgeReconciliation
	if err := json.Unmarshal(ev.Payload, &got); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if err := VerifyEdgeReconciliation(&got, pub); err != nil {
		t.Fatalf("embedded reconciliation must verify: %v", err)
	}
}
