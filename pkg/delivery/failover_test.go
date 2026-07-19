package delivery

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// failover_test.go proves DL-2: a healthy failover meets the SLO/BCP gate, while
// split-brain and RTO/RPO breaches are signed (attested) but fail the gate;
// tampering fails verification; and the recorded receipt is chain-verifying.

func TestFailoverAttestation_SLOAndSplitBrain(t *testing.T) {
	l := newDeliveryLedger(t, 0x84)
	pub := l.Signer().PublicKey()

	// Healthy: exactly one promotion, within RTO/RPO targets.
	ok, err := BuildFailoverAttestation(l.Signer(), FailoverInput{
		Service: "db", FromCluster: "eu-1", ToCluster: "eu-2", Trigger: "health-probe-failure",
		Promotions: 1, RTOSeconds: 20, RPOSeconds: 2, RTOTargetSec: 60, RPOTargetSec: 5,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := VerifyFailoverAttestation(ok, pub); err != nil {
		t.Fatalf("valid attestation must verify: %v", err)
	}
	if err := ok.MeetsSLO(); err != nil {
		t.Fatalf("a healthy failover must meet the SLO gate: %v", err)
	}

	// Split-brain: two promotions won -> signed, but the gate fails.
	sb, err := BuildFailoverAttestation(l.Signer(), FailoverInput{
		Service: "db", Promotions: 2, RTOSeconds: 10, RTOTargetSec: 60,
	})
	if err != nil {
		t.Fatalf("build split-brain: %v", err)
	}
	if err := VerifyFailoverAttestation(sb, pub); err != nil {
		t.Fatalf("split-brain attestation must still be signed: %v", err)
	}
	if err := sb.MeetsSLO(); err == nil {
		t.Fatal("split-brain (2 promotions) MUST fail the SLO gate")
	}

	// RTO breach -> gate fails.
	slow, err := BuildFailoverAttestation(l.Signer(), FailoverInput{
		Service: "db", Promotions: 1, RTOSeconds: 120, RTOTargetSec: 60,
	})
	if err != nil {
		t.Fatalf("build slow: %v", err)
	}
	if err := slow.MeetsSLO(); err == nil {
		t.Fatal("an RTO breach MUST fail the SLO gate")
	}

	// Tamper the measured RTO -> signature fails.
	bad := *ok
	bad.RTOSeconds = 5
	if err := VerifyFailoverAttestation(&bad, pub); err == nil {
		t.Fatal("tampered RTO MUST fail verification")
	}
}

func TestFailoverAttestation_RecordAndKey(t *testing.T) {
	ctx := context.Background()
	l := newDeliveryLedger(t, 0x85)
	pub := l.Signer().PublicKey()

	att, err := BuildFailoverAttestation(l.Signer(), FailoverInput{
		Service: "db", FromCluster: "eu-1", ToCluster: "eu-2", Promotions: 1, RTOSeconds: 10, RTOTargetSec: 60,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	ev, err := RecordFailoverAttestation(ctx, l, att)
	if err != nil || ev == nil {
		t.Fatalf("record: ev=%v err=%v", ev, err)
	}
	if got := FailoverServiceKeyOf(ev); got != "db" {
		t.Fatalf("FailoverServiceKeyOf = %q, want db", got)
	}

	all, _ := l.Store().All(ctx)
	rep, _ := evidence.VerifyChain(all, pub)
	if !rep.Valid {
		t.Fatalf("chain with recorded failover attestation must verify, got %+v", rep)
	}
	var got FailoverAttestation
	if err := json.Unmarshal(ev.Payload, &got); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if err := VerifyFailoverAttestation(&got, pub); err != nil {
		t.Fatalf("embedded attestation must verify: %v", err)
	}
}
