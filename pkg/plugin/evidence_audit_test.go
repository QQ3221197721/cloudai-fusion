package plugin

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func newTestAuditor(t *testing.T) *EvidencePluginAuditor {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return NewEvidencePluginAuditor(priv)
}

// TestAudit_CleanBehaviorStaysTrusted verifies benign executions keep a plugin
// trusted and un-quarantined, and every audit emits a verifiable receipt.
func TestAudit_CleanBehaviorStaysTrusted(t *testing.T) {
	auditor := newTestAuditor(t)
	clean := PluginBehavior{SyscallCount: 120, SensitiveSyscalls: 0, NetworkBytes: 4096, MemoryPeakBytes: 1 << 20, CPUMillis: 20}

	for i := 0; i < 10; i++ {
		res, err := auditor.AuditPluginExecution("good-plugin", clean)
		if err != nil {
			t.Fatalf("audit: %v", err)
		}
		if res.Receipt == nil || !res.Receipt.Verify() {
			t.Fatalf("audit %d must produce a verifiable receipt", i)
		}
		if res.Quarantined {
			t.Fatalf("clean plugin should not be quarantined (score=%.2f)", res.TrustScore)
		}
	}
	// Final trust should be at the ceiling for consistently clean behaviour.
	res, _ := auditor.AuditPluginExecution("good-plugin", clean)
	if res.TrustScore < 0.9 {
		t.Fatalf("expected high trust for clean plugin, got %.2f", res.TrustScore)
	}
}

// TestAudit_MaliciousBehaviorQuarantines verifies that a plugin issuing
// sensitive syscalls has its trust driven below the threshold and is
// automatically quarantined.
func TestAudit_MaliciousBehaviorQuarantines(t *testing.T) {
	auditor := newTestAuditor(t)
	malicious := PluginBehavior{SyscallCount: 40, SensitiveSyscalls: 40, NetworkBytes: 1 << 20, MemoryPeakBytes: 1 << 20, Errors: 3}

	res, err := auditor.AuditPluginExecution("evil-plugin", malicious)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if !res.Quarantined {
		t.Fatalf("malicious plugin should be quarantined (score=%.2f)", res.TrustScore)
	}
	if len(res.Reasons) == 0 {
		t.Fatal("quarantine decision must be explained with reasons")
	}
	if !res.Receipt.Verify() {
		t.Fatal("quarantine receipt must verify")
	}
}

// TestTrust_DecaysWithSilence verifies trust erodes over time without positive
// signals (white-box: rewind LastUpdated to simulate elapsed time).
func TestTrust_DecaysWithSilence(t *testing.T) {
	scorer := NewBehavioralTrustScorer(0.1, 0.5) // 0.1 trust lost per hour
	clean := PluginBehavior{SyscallCount: 50, NetworkBytes: 1024, MemoryPeakBytes: 1 << 20}

	// Establish a trusted plugin.
	if st, _ := scorer.Score("p", clean); st.Score < 0.9 {
		t.Fatalf("expected initial high trust, got %.2f", st.Score)
	}

	// Simulate 6 hours of silence, then a benign observation.
	scorer.mu.Lock()
	scorer.scores["p"].LastUpdated = time.Now().Add(-6 * time.Hour)
	scorer.mu.Unlock()

	st, _ := scorer.Score("p", clean)
	// 6h * 0.1 = 0.6 decay applied before the +0.05 clean reward.
	if st.Score > 0.6 {
		t.Fatalf("expected trust to decay with silence, got %.2f", st.Score)
	}
}

// TestBehavioralHelpers exercises the anomaly/EMA math directly.
func TestBehavioralHelpers(t *testing.T) {
	if anomalyFactor(0, 1e9) != 0 {
		t.Fatal("no penalty while baseline is still learning")
	}
	if anomalyFactor(100, 150) != 0 {
		t.Fatal("value within 2x baseline should not be anomalous")
	}
	if f := anomalyFactor(100, 600); f <= 0 || f > 1 {
		t.Fatalf("6x baseline should be moderately anomalous, got %.2f", f)
	}
	if got := ema(0, 500, 0); got != 500 {
		t.Fatalf("first EMA observation should seed the average, got %.1f", got)
	}
	if clamp01(-1) != 0 || clamp01(2) != 1 {
		t.Fatal("clamp01 must bound to [0,1]")
	}
}

// TestAudit_InstallUninstallReceipts verifies lifecycle events are sealed.
func TestAudit_InstallUninstallReceipts(t *testing.T) {
	auditor := newTestAuditor(t)
	r1, err := auditor.RecordInstall("p", "1.2.3")
	if err != nil || !r1.Verify() {
		t.Fatalf("install receipt must verify: err=%v", err)
	}
	r2, err := auditor.RecordUninstall("p")
	if err != nil || !r2.Verify() {
		t.Fatalf("uninstall receipt must verify: err=%v", err)
	}
	// The two receipts should chain (uninstall references install).
	if r2.PreviousReceiptID != r1.ID {
		t.Fatalf("expected receipt chaining, got prev=%q want %q", r2.PreviousReceiptID, r1.ID)
	}
}
