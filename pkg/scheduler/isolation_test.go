package scheduler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// isolation_test.go proves CN-3: an isolated MIG placement verifies and proves
// isolation; a non-isolated co-placement is signed (attested) but fails the gate;
// tampering fails; and the recorded receipt is a chain-verifying ledger citizen.

func TestIsolationAttestation(t *testing.T) {
	ctx := context.Background()
	l := newTestLedger(t) // shared helper from decision_test.go (same package)
	pub := l.Signer().PublicKey()

	// Isolated MIG partition -> verifies + proves isolation.
	ok, err := BuildIsolationAttestation(ctx, l.Signer(), SimulatedIsolationProbe{Verdict: true},
		"gpu-node-1", 0, GPUShareMIG, []string{"team-a", "team-b"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if ok.ProbeReal {
		t.Fatal("simulated probe must report ProbeReal=false")
	}
	if err := VerifyIsolationAttestation(ok, pub); err != nil {
		t.Fatalf("valid attestation must verify: %v", err)
	}
	if err := ok.ProvesIsolation(); err != nil {
		t.Fatalf("an isolated MIG placement must prove isolation: %v", err)
	}

	// Non-isolated co-placement -> signed, but the isolation gate fails.
	bad, err := BuildIsolationAttestation(ctx, l.Signer(), SimulatedIsolationProbe{Verdict: false},
		"gpu-node-1", 1, GPUShareMPS, []string{"team-a", "team-b"})
	if err != nil {
		t.Fatalf("build bad: %v", err)
	}
	if err := VerifyIsolationAttestation(bad, pub); err != nil {
		t.Fatalf("non-isolated attestation must still be signed: %v", err)
	}
	if err := bad.ProvesIsolation(); err == nil {
		t.Fatal("a non-isolated co-placement MUST fail the isolation gate")
	}

	// Tamper the verdict -> signature fails.
	tampered := *ok
	tampered.Isolated = false
	if err := VerifyIsolationAttestation(&tampered, pub); err == nil {
		t.Fatal("tampered attestation MUST fail verification")
	}

	// Record + chain verify + key.
	ev, err := RecordIsolationAttestation(ctx, l, ok)
	if err != nil || ev == nil {
		t.Fatalf("record: ev=%v err=%v", ev, err)
	}
	if got := IsolationNodeKeyOf(ev); got != "gpu-node-1" {
		t.Fatalf("IsolationNodeKeyOf = %q, want gpu-node-1", got)
	}
	all, _ := l.Store().All(ctx)
	rep, _ := evidence.VerifyChain(all, pub)
	if !rep.Valid {
		t.Fatalf("chain with recorded isolation attestation must verify, got %+v", rep)
	}
	var got IsolationAttestation
	if err := json.Unmarshal(ev.Payload, &got); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if err := VerifyIsolationAttestation(&got, pub); err != nil {
		t.Fatalf("embedded attestation must verify: %v", err)
	}
}
