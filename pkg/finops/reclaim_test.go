package finops

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// realReclaim is a test double that reports a real backend (Real()==true) so we
// can exercise the Measured==true path without real cloud/K8s infrastructure.
type realReclaim struct{ called bool }

func (r *realReclaim) Reclaim(context.Context, ReclaimTarget) error { r.called = true; return nil }
func (r *realReclaim) Real() bool                                   { return true }
func (r *realReclaim) Backend() string                              { return "k8s" }

func testLedger(t *testing.T) *evidence.Ledger {
	t.Helper()
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x33}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	l, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	return l
}

func idleSamples(source string, from time.Time, n int) []UtilizationSample {
	out := make([]UtilizationSample, n)
	for i := 0; i < n; i++ {
		out[i] = UtilizationSample{
			Timestamp:      from.Add(time.Duration(i) * time.Hour),
			GPUUtilization: 2.0, // 2% => idle
			Source:         source,
		}
	}
	return out
}

func TestReclaim_MeasuredWithRealBackendAndDCGM(t *testing.T) {
	ledger := testLedger(t)
	action := &realReclaim{}
	eng := NewReclaimEngine(ReclaimEngineConfig{Action: action, Recorder: ledger})

	from := time.Now().UTC().Add(-4 * time.Hour)
	samples := idleSamples("dcgm", from, 5) // spans 4 hours
	target := ReclaimTarget{
		ResourceID: "node-7/gpu-3", Node: "node-7", GPUType: "nvidia-a100",
		GPUCount: 1, HourlyRate: 3.0, PriceSource: "billing-api",
	}

	receipt, ev, err := eng.Reclaim(context.Background(), target, samples)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if !action.called {
		t.Fatal("real reclaim action should have been invoked")
	}
	if !receipt.Measured {
		t.Fatal("receipt must be Measured with a real backend + dcgm samples")
	}
	// 4 idle hours x 1 GPU x $3/h = $12 realized.
	if receipt.ReclaimedGPUHours != 4 || receipt.RealizedSavingsUSD != 12 {
		t.Fatalf("unexpected measured savings: %.2f GPU-hours, $%.2f", receipt.ReclaimedGPUHours, receipt.RealizedSavingsUSD)
	}
	if len(receipt.SampleHashes) != len(samples) {
		t.Fatal("every sample must be hashed into the receipt")
	}
	if ev == nil {
		t.Fatal("an evidence receipt should have been emitted")
	}

	// The emitted evidence must verify and carry honest per-action backends.
	all, _ := ledger.Store().All(context.Background())
	rep, _ := evidence.VerifyChain(all, ledger.Signer().PublicKey())
	if !rep.Valid {
		t.Fatalf("finops evidence must verify, got %+v", rep)
	}
	var sawReclaimReal bool
	for _, b := range all[0].Backends {
		if b.Component == "finops.reclaim" && b.Mode == "real" {
			sawReclaimReal = true
		}
	}
	if !sawReclaimReal {
		t.Fatal("evidence backends must record finops.reclaim as real")
	}

	// Payload round-trips to a SavingsReceipt.
	var decoded SavingsReceipt
	if err := json.Unmarshal(all[0].Payload, &decoded); err != nil {
		t.Fatalf("decode receipt payload: %v", err)
	}
	if !decoded.Measured || decoded.RealizedSavingsUSD != 12 {
		t.Fatalf("payload mismatch: %+v", decoded)
	}
}

func TestReclaim_SimulatedIsNotMeasured(t *testing.T) {
	ledger := testLedger(t)
	eng := NewReclaimEngine(ReclaimEngineConfig{Recorder: ledger}) // default simulated action

	from := time.Now().UTC().Add(-2 * time.Hour)
	// Even with real dcgm samples, a simulated reclaim backend must not be Measured.
	receipt, _, err := eng.Reclaim(context.Background(), ReclaimTarget{
		ResourceID: "n1/g0", GPUCount: 1, HourlyRate: 5, PriceSource: "static-table",
	}, idleSamples("dcgm", from, 3))
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if receipt.Measured {
		t.Fatal("a simulated reclaim must never be reported as Measured")
	}
	if receipt.BackendReal {
		t.Fatal("simulated backend must report BackendReal=false")
	}
}

func TestReclaim_RefusesNonIdle(t *testing.T) {
	eng := NewReclaimEngine(ReclaimEngineConfig{})
	busy := []UtilizationSample{
		{Timestamp: time.Now(), GPUUtilization: 85, Source: "dcgm"},
		{Timestamp: time.Now(), GPUUtilization: 90, Source: "dcgm"},
	}
	_, _, err := eng.Reclaim(context.Background(), ReclaimTarget{ResourceID: "n1/g0", GPUCount: 1}, busy)
	if err != ErrNotIdle {
		t.Fatalf("expected ErrNotIdle for a busy GPU, got %v", err)
	}
}

func TestReclaim_RequiresSamples(t *testing.T) {
	eng := NewReclaimEngine(ReclaimEngineConfig{})
	_, _, err := eng.Reclaim(context.Background(), ReclaimTarget{ResourceID: "n1/g0"}, nil)
	if err == nil {
		t.Fatal("expected an error when no utilization samples are provided")
	}
}
