package alerting

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func newTestAlertManager(t *testing.T) *EvidenceAlertManager {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return NewEvidenceAlertManager(priv)
}

// TestSendAlert_CorrelatesRelatedAlerts sends 10 alerts from the same source and
// verifies exactly one is delivered fresh while the other nine are suppressed
// as correlated.
func TestSendAlert_CorrelatesRelatedAlerts(t *testing.T) {
	mgr := newTestAlertManager(t)

	delivered := 0
	suppressed := 0
	for i := 0; i < 10; i++ {
		proof, err := mgr.SendAlert(EvidenceAlert{
			ID:        "alert-" + time.Now().Format("150405.000000000"),
			Severity:  "high",
			Source:    "node-exporter-1",
			Message:   "disk pressure",
			Labels:    map[string]string{"host": "web-1"},
			Timestamp: time.Now(),
		})
		if err != nil {
			t.Fatalf("send alert %d: %v", i, err)
		}
		if proof.Receipt == nil || !proof.Receipt.Verify() {
			t.Fatalf("alert %d: receipt must verify", i)
		}
		if proof.Suppressed {
			suppressed++
			if proof.GroupID == "" {
				t.Errorf("suppressed alert %d must reference a group", i)
			}
		} else {
			delivered++
		}
	}

	if delivered != 1 {
		t.Errorf("expected exactly 1 fresh delivery, got %d", delivered)
	}
	if suppressed != 9 {
		t.Errorf("expected 9 suppressed alerts, got %d", suppressed)
	}
}

func TestSendAlert_UnrelatedAlertsNotSuppressed(t *testing.T) {
	mgr := newTestAlertManager(t)

	a, err := mgr.SendAlert(EvidenceAlert{ID: "a1", Source: "svc-a", Labels: map[string]string{"k": "1"}})
	if err != nil {
		t.Fatalf("send a: %v", err)
	}
	b, err := mgr.SendAlert(EvidenceAlert{ID: "b1", Source: "svc-b", Labels: map[string]string{"k": "2"}})
	if err != nil {
		t.Fatalf("send b: %v", err)
	}
	if a.Suppressed || b.Suppressed {
		t.Errorf("unrelated alerts should both be delivered fresh, got a=%v b=%v", a.Suppressed, b.Suppressed)
	}
}

func TestIsSimilar_LabelOverlap(t *testing.T) {
	e := &CausalCorrelationEngine{window: 5 * time.Minute}
	a := EvidenceAlert{Source: "x", Labels: map[string]string{"a": "1", "b": "2"}}
	b := EvidenceAlert{Source: "y", Labels: map[string]string{"a": "1", "b": "2"}}
	if !e.isSimilar(a, b) {
		t.Errorf("alerts with full label overlap should be similar")
	}
	c := EvidenceAlert{Source: "z", Labels: map[string]string{"a": "1", "c": "9"}}
	if e.isSimilar(a, c) {
		t.Errorf("alerts with only 25%% label overlap should not be similar")
	}
}

func BenchmarkSendAlertWithEvidence(b *testing.B) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	mgr := NewEvidenceAlertManager(priv)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = mgr.SendAlert(EvidenceAlert{ID: "a", Source: "src", Labels: map[string]string{"k": "v"}})
	}
}
