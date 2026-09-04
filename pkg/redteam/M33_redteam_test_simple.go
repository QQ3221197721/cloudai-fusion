package redteam

import (
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// Minimal benchmark test to verify M33_PureAsync compiles and runs
func TestM33_PureAsync_Basic(t *testing.T) {
	packages := generateTestPackages(10)
	
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	
	store := evidence.NewMemoryStore()
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{Store: store, Signer: signer})
	if err != nil {
		t.Fatalf("new_ledger: %v", err)
	}
	
	scanner := NewVulnScanner(ledger)
	
	// Hot path: should return immediately (<5ms expected for 10 packages)
	findings, err := scanner.Scan(packages)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	
	if len(findings) == 0 {
		t.Log("No findings - this is okay for zero-vuln packages")
	} else {
		t.Logf("Scan returned %d findings", len(findings))
	}
}
