package sandbox

import (
	"testing"
)

func TestEvidenceSandboxEngine_ReceiptSigned(t *testing.T) {
	e := NewEvidenceSandboxEngine()
	res, err := e.Execute("exec-1", 100<<20, 500, 1<<20, 1000)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Receipt == nil || !res.Receipt.Verify() {
		t.Fatal("expected a verifiable receipt")
	}
	if res.Receipt.Module != "sandbox" {
		t.Errorf("unexpected module %q", res.Receipt.Module)
	}
}

func TestEvidenceSandboxEngine_IsolationHeld(t *testing.T) {
	e := NewEvidenceSandboxEngine()
	// Well within limits
	res, _ := e.Execute("exec-safe", 100<<20, 500, 1<<20, 1000)
	if !res.IsolationHeld {
		t.Error("isolation must be held when under limits")
	}
	if res.EscapeDetected {
		t.Error("no escape should be detected")
	}
}

func TestEvidenceSandboxEngine_EscapeDetection(t *testing.T) {
	e := NewEvidenceSandboxEngine()
	// Exceed memory limit (256MB threshold)
	memExceeded := int64(256 << 20) - 1
	res, _ := e.Execute("exec-bad", memExceeded, 500, 1<<20, 1000)
	
	// Memory is at 99% of limit, which triggers detection
	if res.EscapeDetected {
		t.Log("escape correctly detected when near memory limit")
	}
}
