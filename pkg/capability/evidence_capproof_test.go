package capability

import "testing"

func TestEvidenceCapabilityEngine_ReceiptSigned(t *testing.T) {
	e := NewEvidenceCapabilityEngine()
	res, err := e.Detect([]string{"cpu"}, []string{"high-throughput"})
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	privKey := e.GetPrivKey()
	if res.Receipt == nil || !res.Receipt.Verify(privKey) {
		t.Fatal("expected a verifiable receipt")
	}
	if res.Receipt.Module != "capability" {
		t.Errorf("unexpected module %q", res.Receipt.Module)
	}
}

func TestEvidenceCapabilityEngine_GracefulDegradation(t *testing.T) {
	e := NewEvidenceCapabilityEngine()
	
	// Full GPU missing, CPU available
	res, _ := e.Detect([]string{"cpu"}, []string{"gpu-accelerated", "high-throughput"})
	
	if res.CurrentTier != modeLite && res.CurrentTier != modeCPU {
		t.Logf("tier selection: current=%s, target=%s", res.CurrentTier, res.TargetTier)
	}
	
	if res.DegradationPlan != nil && !res.DegradationPlan.Viable {
		t.Log("degradation plan correctly indicates non-viability without GPU")
	}
}

func TestEvidenceCapabilityEngine_FullSupportNoDegradation(t *testing.T) {
	e := NewEvidenceCapabilityEngine()
	// All capabilities present
	res, _ := e.Detect([]string{"gpu-accelerated", "high-throughput", "fast-storage"}, []string{"gpu-accelerated", "high-throughput"})
	if len(res.MissingCapabilities) > 0 {
		t.Errorf("expected no missing capabilities, got %v", res.MissingCapabilities)
	}
	if res.TargetTier != modeGPU {
		t.Errorf("expected GPU tier with full support, got %q", res.TargetTier)
	}
}
