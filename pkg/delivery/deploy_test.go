package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// deploy_test.go proves DL-1: a deploy attestation verifies offline, drift is
// attested (signed) but fails the integrity gate, tampering fails, and the
// recorded receipt is a first-class, chain-verifying ledger citizen.

func newDeliveryLedger(t *testing.T, seed byte) *evidence.Ledger {
	t.Helper()
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{seed}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	l, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	return l
}

func TestDeployAttestation_MatchAndDrift(t *testing.T) {
	l := newDeliveryLedger(t, 0x81)
	pub := l.Signer().PublicKey()

	// No drift: running == signed.
	ok, err := BuildDeployAttestation(l.Signer(), DeployInput{
		Workload: "api", Cluster: "prod-eu", ImageDigest: "sha256:aaa", ConfigHash: "cfg1",
		SignedDigest: "sha256:aaa", ProvenanceRef: "slsa:1", ApprovalRef: "chg-1",
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if ok.DriftDetected {
		t.Fatal("no drift expected when running == signed")
	}
	if err := VerifyDeployAttestation(ok, pub); err != nil {
		t.Fatalf("valid attestation must verify: %v", err)
	}
	if err := ok.ProvesIntegrity(); err != nil {
		t.Fatalf("integrity must hold with no drift: %v", err)
	}

	// Drift: running != signed. The attestation is still validly SIGNED (drift is
	// attested, not hidden), but the integrity gate must fail.
	drift, err := BuildDeployAttestation(l.Signer(), DeployInput{
		Workload: "api", Cluster: "prod-eu", ImageDigest: "sha256:bbb", SignedDigest: "sha256:aaa",
	})
	if err != nil {
		t.Fatalf("build drift: %v", err)
	}
	if !drift.DriftDetected {
		t.Fatal("drift expected when running != signed")
	}
	if err := VerifyDeployAttestation(drift, pub); err != nil {
		t.Fatalf("a drift attestation must still be validly signed: %v", err)
	}
	if err := drift.ProvesIntegrity(); err == nil {
		t.Fatal("drift MUST fail the integrity gate")
	}

	// Tamper post-sign -> signature fails.
	bad := *ok
	bad.ImageDigest = "sha256:ccc"
	if err := VerifyDeployAttestation(&bad, pub); err == nil {
		t.Fatal("tampered attestation MUST fail verification")
	}
	// Wrong key -> fails.
	other := newDeliveryLedger(t, 0x82)
	if err := VerifyDeployAttestation(ok, other.Signer().PublicKey()); err == nil {
		t.Fatal("verification under the wrong key MUST fail")
	}
}

func TestDeployAttestation_RecordAndKey(t *testing.T) {
	ctx := context.Background()
	l := newDeliveryLedger(t, 0x83)
	pub := l.Signer().PublicKey()

	att, err := BuildDeployAttestation(l.Signer(), DeployInput{
		Workload: "api", Cluster: "prod-eu", ImageDigest: "sha256:aaa", SignedDigest: "sha256:aaa",
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	ev, err := RecordDeployAttestation(ctx, l, att)
	if err != nil || ev == nil {
		t.Fatalf("record: ev=%v err=%v", ev, err)
	}

	if got := ClusterKeyOf(ev); got != "prod-eu" {
		t.Fatalf("ClusterKeyOf = %q, want prod-eu", got)
	}

	all, _ := l.Store().All(ctx)
	rep, _ := evidence.VerifyChain(all, pub)
	if !rep.Valid {
		t.Fatalf("chain with recorded deploy attestation must verify, got %+v", rep)
	}

	var got DeployAttestation
	if err := json.Unmarshal(ev.Payload, &got); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if err := VerifyDeployAttestation(&got, pub); err != nil {
		t.Fatalf("embedded attestation must verify: %v", err)
	}
}
