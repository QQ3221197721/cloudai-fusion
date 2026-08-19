package mesh

import (
	"crypto/ed25519"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

func newTestRoutingEngine(t *testing.T) *EvidenceRoutingEngine {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 5)
	}
	key := ed25519.NewKeyFromSeed(seed)
	rb := evidence.NewReceiptBuilder("mesh", key)
	return NewEvidenceRoutingEngine(rb, 0.3)
}

func TestRecordCall_ProducesVerifiableReceipt(t *testing.T) {
	e := newTestRoutingEngine(t)
	res, err := e.RecordCall("svc-a", "svc-b", 42)
	if err != nil {
		t.Fatalf("RecordCall: %v", err)
	}
	if res.Receipt == nil || !res.Receipt.Verify() {
		t.Fatal("expected a verifiable receipt")
	}
	if res.Receipt.Operation != "mesh.call" {
		t.Fatalf("operation = %q, want mesh.call", res.Receipt.Operation)
	}
}

func TestCalls_ChainTogether(t *testing.T) {
	e := newTestRoutingEngine(t)
	var receipts []*evidence.Receipt
	for i := 0; i < 5; i++ {
		r, err := e.RecordCall("a", "b", 10+i)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		receipts = append(receipts, r.Receipt)
	}
	if err := evidence.VerifyChainOfReceipts(receipts); err != nil {
		t.Fatalf("chain verify: %v", err)
	}
}

// TestAnomalyDetection_SpikeRaisesScore feeds a stable latency profile then a
// large spike, asserting the exponential-smoothing detector reacts.
func TestAnomalyDetection_SpikeRaisesScore(t *testing.T) {
	e := newTestRoutingEngine(t)
	// Stable ~50ms profile builds a small variance.
	for i := 0; i < 40; i++ {
		if _, err := e.RecordCall("a", "b", 50+i%3); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	stable := e.Anomaly("b")
	// A 10x latency spike must produce a much higher anomaly score.
	if _, err := e.RecordCall("a", "b", 600); err != nil {
		t.Fatalf("spike: %v", err)
	}
	spiked := e.Anomaly("b")
	t.Logf("stable=%.3f spiked=%.3f", stable, spiked)
	if spiked <= stable {
		t.Fatalf("spike anomaly %.3f should exceed stable %.3f", spiked, stable)
	}
	if spiked < 0.5 {
		t.Fatalf("spike anomaly = %.3f, want >= 0.5", spiked)
	}
}

// TestAnomalyAwareRouting_AvoidsAnomalousCallee proves the router steers away
// from a callee exhibiting anomalous latency toward a healthy one.
func TestAnomalyAwareRouting_AvoidsAnomalousCallee(t *testing.T) {
	e := newTestRoutingEngine(t)
	// Healthy callee: stable low latency.
	for i := 0; i < 40; i++ {
		e.RecordCall("gw", "healthy", 20+i%2)
	}
	// Anomalous callee: stable then a violent spike.
	for i := 0; i < 40; i++ {
		e.RecordCall("gw", "sick", 20+i%2)
	}
	e.RecordCall("gw", "sick", 900)

	dec, err := e.Route([]string{"healthy", "sick"})
	if err != nil {
		t.Fatalf("route: %v", err)
	}
	if dec.Callee != "healthy" {
		t.Fatalf("router chose %q, want healthy (anomaly=%.3f)", dec.Callee, dec.Anomaly)
	}
}

func TestRoute_EmptyCandidates(t *testing.T) {
	e := newTestRoutingEngine(t)
	if _, err := e.Route(nil); err == nil {
		t.Fatal("expected error for empty candidates")
	}
}
