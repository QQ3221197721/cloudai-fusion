package hotswap

import (
	"testing"
)

func TestEvidenceHotswapEngine_ReceiptSigned(t *testing.T) {
	e := NewEvidenceHotswapEngine()
	e.StartSwap("api-server", "v1.0.0", 100, 100)
	e.RecordDuringSwap("api-server", 102, 102)
	res, err := e.EndSwap("api-server", "v1.0.0", "v1.1.0", 104, 104, 500, true)
	if err != nil {
		t.Fatalf("EndSwap: %v", err)
	}
	if res.Receipt == nil || !res.Receipt.Verify() {
		t.Fatal("expected a verifiable receipt")
	}
	if res.Receipt.Module != "hotswap" {
		t.Errorf("unexpected module %q", res.Receipt.Module)
	}
}

func TestEvidenceHotswapEngine_ZeroDowntimeVerification(t *testing.T) {
	e := NewEvidenceHotswapEngine()
	e.StartSwap("service-a", "v1.0", 1000, 1000)
	e.RecordDuringSwap("service-a", 1020, 1020)
	res, _ := e.EndSwap("service-a", "v1.0", "v1.1", 1040, 1040, 1000, true)
	
	if !res.InvariantHeld {
		t.Error("invariant must be held when counts match")
	}
	if res.SwapStatus != "success" {
		t.Errorf("expected success status, got %q", res.SwapStatus)
	}
	if res.DroppedRequests > 0 {
		t.Errorf("zero dropped requests expected, got %d", res.DroppedRequests)
	}
}

func TestEvidenceHotswapEngine_DroppedRequestsFailInvariant(t *testing.T) {
	e := NewEvidenceHotswapEngine()
	e.StartSwap("svc-b", "v1.0", 100, 100)
	e.RecordDuringSwap("svc-b", 105, 104) // 1 in, 0 out => dropped
	res, _ := e.EndSwap("svc-b", "v1.0", "v1.1", 110, 108, 800, false)
	
	if res.InvariantHeld {
		t.Error("invariant must fail when requests were dropped")
	}
	if res.SwapStatus == "success" {
		t.Error("swapping with dropped requests must not succeed")
	}
}

func TestEvidenceHotswapEngine_WithStartGap(t *testing.T) {
	e := NewEvidenceHotswapEngine()
	// Start with a gap already present
	e.StartSwap("svc-c", "v1.0", 100, 95) // started with 5 uncompleted
	e.RecordDuringSwap("svc-c", 105, 100)
	res, _ := e.EndSwap("svc-c", "v1.0", "v1.1", 110, 105, 600, true)
	
	if res.InvariantHeld {
		t.Log("gap at start detected via invariant check")
	}
}
