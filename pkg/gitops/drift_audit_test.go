package gitops

import (
	"testing"
	"time"
)

// sampleDriftEvent builds a representative drift event (a replica count that
// diverged from Git desired state).
func sampleDriftEvent(app string, drifted bool) DriftEvent {
	ev := DriftEvent{
		Application: app,
		Namespace:   "prod",
		Engine:      EngineArgoCD,
		DesiredSHA:  "git@" + app + ":aaaa1111",
		LiveSHA:     "live@" + app + ":bbbb2222",
		Drifted:     drifted,
		DetectedAt:  time.Now().UTC(),
	}
	if drifted {
		ev.Drifts = []DriftDetail{{
			ResourceKind: "Deployment", ResourceName: app, Namespace: "prod",
			Field: "spec.replicas", Expected: "3", Actual: "1", Severity: "high",
		}}
	}
	return ev
}

// TestDriftAuditTrail_VerifiesIntact records a sequence of drift events and
// proves the untouched trail verifies offline with only the public key.
func TestDriftAuditTrail_VerifiesIntact(t *testing.T) {
	trail := NewDriftAuditTrail()
	for i := 0; i < 5; i++ {
		if _, err := trail.Record(sampleDriftEvent("svc", i%2 == 0)); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}
	if idx, err := trail.Verify(); err != nil {
		t.Fatalf("intact trail must verify, got tamper at %d: %v", idx, err)
	}
	// Offline verification: a third party with ONLY the public key + entries
	// (no server, no private key) reaches the same verdict.
	if idx, err := VerifyDriftAuditEntries(trail.Entries(), trail.PublicKey()); err != nil {
		t.Fatalf("offline verify must pass, got tamper at %d: %v", idx, err)
	}
}

// TestDriftAuditTrail_DetectsContentTamper is the core Module 39 proof: an
// attacker edits a stored drift record (hiding that drift ever happened). The
// trail catches it because the edited event no longer matches the committed
// hash, even though the receipt's own signature is still internally valid.
func TestDriftAuditTrail_DetectsContentTamper(t *testing.T) {
	trail := NewDriftAuditTrail()
	trail.Record(sampleDriftEvent("payments", true))
	trail.Record(sampleDriftEvent("orders", true))
	trail.Record(sampleDriftEvent("inventory", true))

	entries := trail.Entries()
	// Attacker rewrites the middle drift record to pretend nothing drifted.
	entries[1].Event.Drifted = false
	entries[1].Event.Drifts = nil

	idx, err := VerifyDriftAuditEntries(entries, trail.PublicKey())
	if err == nil {
		t.Fatal("expected tamper detection, got none")
	}
	if idx != 1 {
		t.Fatalf("expected tamper at index 1, got %d (%v)", idx, err)
	}
	t.Logf("content-tamper correctly detected at entry %d: %v", idx, err)
}

// TestDriftAuditTrail_DetectsReceiptForgery covers the case where the attacker,
// aware of the hash binding, also rewrites the receipt's committed OutputHash to
// match their edited event. That breaks the Ed25519 signature.
func TestDriftAuditTrail_DetectsReceiptForgery(t *testing.T) {
	trail := NewDriftAuditTrail()
	trail.Record(sampleDriftEvent("api", true))
	trail.Record(sampleDriftEvent("db", true))

	entries := trail.Entries()
	// Forge: flip a byte in the committed output hash.
	entries[0].Receipt.OutputHash[0] ^= 0xFF

	idx, err := VerifyDriftAuditEntries(entries, trail.PublicKey())
	if err == nil {
		t.Fatal("expected signature failure after receipt forgery")
	}
	if idx != 0 {
		t.Fatalf("expected forgery at index 0, got %d (%v)", idx, err)
	}
	t.Logf("receipt-forgery correctly detected at entry %d: %v", idx, err)
}

// TestDriftAuditTrail_DetectsDeletion proves that silently deleting a drift
// record (a common cover-up) breaks the receipt chain linkage.
func TestDriftAuditTrail_DetectsDeletion(t *testing.T) {
	trail := NewDriftAuditTrail()
	trail.Record(sampleDriftEvent("a", true))
	trail.Record(sampleDriftEvent("b", true))
	trail.Record(sampleDriftEvent("c", true))

	entries := trail.Entries()
	// Delete the middle entry, then re-present entries 0 and 2 as the record.
	tampered := []*DriftAuditEntry{entries[0], entries[2]}

	if _, err := VerifyDriftAuditEntries(tampered, trail.PublicKey()); err == nil {
		t.Fatal("expected chain-linkage failure after deletion")
	} else {
		t.Logf("deletion correctly detected via broken chain: %v", err)
	}
}

// TestDriftAuditTrail_Latency measures, on this machine, the cost the evidence
// layer ADDS: signing one drift event and verifying a whole trail offline. This
// is NOT end-to-end cluster-diff latency (that is delegated to the DriftScanner
// backend); it is purely the tamper-evidence overhead our moat introduces.
func TestDriftAuditTrail_Latency(t *testing.T) {
	const n = 1000
	trail := NewDriftAuditTrail()

	start := time.Now()
	for i := 0; i < n; i++ {
		if _, err := trail.Record(sampleDriftEvent("svc", true)); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}
	recordTotal := time.Since(start)

	entries := trail.Entries()
	vstart := time.Now()
	if idx, err := VerifyDriftAuditEntries(entries, trail.PublicKey()); err != nil {
		t.Fatalf("verify: tamper at %d: %v", idx, err)
	}
	verifyTotal := time.Since(vstart)

	t.Logf("record: %d events in %s (%.2f µs/event)", n, recordTotal, float64(recordTotal.Microseconds())/float64(n))
	t.Logf("verify: %d-entry trail in %s (%.2f µs/entry)", n, verifyTotal, float64(verifyTotal.Microseconds())/float64(n))
}
