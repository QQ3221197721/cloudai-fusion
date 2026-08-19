package soc

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
)

// soar_bench_test.go pins Module 32 performance differentiators: the L8 response
// engine actuates fast enough for live threats, approval decisions are sealed
// into receipts in well under a millisecond, and offline receipt verification is
// far faster than an attacker's evasion window. These numbers matter: if the
// compliance layer adds real latency it kills adoption; we prove "security +
// speed", not one at the cost of the other.

// benchGate builds a deterministic-key gate + recording actuator for benchmarks.
func benchGate(b *testing.B) (*ApprovalGate, *RecordingActuator) {
	b.Helper()
	priv := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x42}, ed25519.SeedSize))
	gate, err := NewApprovalGateWithKey(priv)
	if err != nil {
		b.Fatal(err)
	}
	return gate, NewRecordingActuator()
}

// BenchmarkPlaybook_Match measures pure orchestration overhead (match + build a
// Response, no evidence, no actuation): the SOAR matching throughput before any
// security layer is added.
func BenchmarkPlaybook_Match(b *testing.B) {
	o := NewOrchestrator(nil)
	f := newFinding(WellIdentity, "T1078", "alice", "impossible travel", intel.SeverityCritical, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if r := o.Respond(f); r.Playbook == "" {
			b.Fatal("expected a matched playbook")
		}
	}
}

// BenchmarkResponse_Automation measures the Engine.Respond end-to-end latency for
// an automated playbook (c2-egress): match + actuate all automated actions +
// seal the aggregate signed response receipt. This is the "detection→response"
// hot path a live threat drives.
func BenchmarkResponse_Automation(b *testing.B) {
	store := intel.NewMemoryStore()
	if err := store.UpsertIOCs([]intel.IOCEntry{
		{IOCType: "ip", Value: "203.0.113.9", Severity: intel.SeverityHigh},
	}); err != nil {
		b.Fatal(err)
	}
	eng := NewEngine(store, nil)
	ctx := context.Background()
	f, err := eng.AnalyzeNetwork(ctx, "host-1", []string{"203.0.113.9"}, nil)
	if err != nil || len(f) == 0 {
		b.Fatalf("seed finding: %v (%d)", err, len(f))
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := eng.Respond(ctx, f[0].ID)
		if err != nil {
			b.Fatal(err)
		}
		if !resp.Executed {
			b.Fatal("automated playbook must be executed")
		}
	}
}

// BenchmarkApproval_Decide measures how fast a single human approval decision is
// sealed into a signed Ed25519 receipt (the per-action evidence generation cost).
func BenchmarkApproval_Decide(b *testing.B) {
	gate, _ := benchGate(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ap, err := gate.Decide(ActionBlockNetwork, fmt.Sprintf("10.0.0.%d", i%256), "lead@corp", "known C2", true)
		if err != nil {
			b.Fatal(err)
		}
		if ap.Receipt == nil {
			b.Fatal("approval must seal a receipt")
		}
	}
}

// BenchmarkGuardedActuate measures the guarded execution path (per-action gate
// check + per-action receipt) for a mixed response of two destructive actions
// plus notify — the end-to-end compliance-enforced actuation cost.
func BenchmarkGuardedActuate(b *testing.B) {
	gate, act := benchGate(b)
	// Approve both destructive actions so they execute (worst case: two actuations
	// + two receipts + one notify receipt per iteration).
	if _, err := gate.Decide(ActionIsolateHost, "host-1", "lead@corp", "confirmed", true); err != nil {
		b.Fatal(err)
	}
	if _, err := gate.Decide(ActionBlockNetwork, "10.0.0.1", "lead@corp", "confirmed", true); err != nil {
		b.Fatal(err)
	}
	resp := Response{Actions: []ResponseAction{
		{Type: ActionIsolateHost, Target: "host-1"},
		{Type: ActionBlockNetwork, Target: "10.0.0.1"},
		{Type: ActionNotify, Target: "host-1"},
	}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results := gate.GuardedActuate(context.Background(), act, resp)
		if len(results) != len(resp.Actions) {
			b.Fatalf("expected %d results, got %d", len(resp.Actions), len(results))
		}
	}
}

// BenchmarkReceipt_VerifySingle proves the auditor-facing number: one Ed25519
// signature verify costs tens of microseconds on commodity hardware (pure-Go
// crypto/ed25519), i.e. tens of thousands of verifications/sec single-threaded —
// fast enough for real-time offline verification during triage or audits.
func BenchmarkReceipt_VerifySingle(b *testing.B) {
	gate, _ := benchGate(b)
	ap, err := gate.Decide(ActionRevokeCredential, "alice", "auditor@corp", "false positive", false)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !ap.Receipt.Verify() {
			b.Fatal("receipt must verify")
		}
	}
}

// BenchmarkReceipt_VerifyOfflineAudit simulates a full auditor workflow over a
// batch of receipts: pin each receipt's signer to the published public key and
// verify its signature — zero platform trust, purely cryptographic.
func BenchmarkReceipt_VerifyOfflineAudit(b *testing.B) {
	gate, _ := benchGate(b)
	const n = 100
	receipts := make([]*evidence.Receipt, 0, n)
	pub := gate.PublicKey()
	for i := 0; i < n; i++ {
		action := DestructiveActions()[i%len(DestructiveActions())]
		ap, err := gate.Decide(action, fmt.Sprintf("asset-%d", i), "auditor@corp", "audit chain", true)
		if err != nil {
			b.Fatal(err)
		}
		receipts = append(receipts, ap.Receipt)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		verified := 0
		for _, r := range receipts {
			if bytes.Equal(r.SignerPublicKey, pub) && r.Verify() {
				verified++
			}
		}
		if verified != n {
			b.Fatalf("offline audit: expected all %d receipts to verify, got %d", n, verified)
		}
	}
}
