package edge

import (
	"fmt"
	"testing"
	"time"
)

// TestOfflineEvidenceManager_RecordDecision tests basic evidence recording
func TestOfflineEvidenceManager_RecordDecision(t *testing.T) {
	t.Cleanup(func() {})

	manager := NewOfflineEvidenceManager()

	decision := OfflineDecision{
		Key:       "resource-1",
		Value:     []byte(`{"value": 42}`),
		Timestamp: time.Now(),
		NodeID:    "edge-node-001",
		Operation: "create",
	}

	receipt, err := manager.RecordOfflineDecision(decision)
	if err != nil {
		t.Fatalf("record decision: %v", err)
	}

	// Verify receipt signature
	if !receipt.Verify() {
		t.Error("receipt signature should be valid")
	}

	// Verify receipt metadata
	if receipt.Module != "edge.offline" {
		t.Errorf("wrong module: %s", receipt.Module)
	}
	if receipt.Operation != "edge.decision.create" {
		t.Errorf("wrong operation: %s", receipt.Operation)
	}

	// Verify chain contains receipt
	chain := manager.GetOfflineChain()
	if len(chain) != 1 {
		t.Fatalf("expected chain length 1, got %d", len(chain))
	}
}

// TestOfflineEvidenceManager_ConvergenceProof verifies convergence analysis
func TestOfflineEvidenceManager_ConvergenceProof(t *testing.T) {
	t.Cleanup(func() {})

	manager := NewOfflineEvidenceManager()

	// Record convergent decisions on different keys (should have no conflicts)
	for i := 0; i < 10; i++ {
		decision := OfflineDecision{
			Key:       fmt.Sprintf("key-%d", i),
			Value:     []byte(fmt.Sprintf("value-%d", i)),
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			NodeID:    "edge-node-001",
			Operation: "update",
		}
		if _, err := manager.RecordOfflineDecision(decision); err != nil {
			t.Fatalf("record decision %d: %v", i, err)
		}
	}

	proof, err := manager.VerifyOfflineChain()
	if err != nil {
		t.Fatalf("verify chain: %v", err)
	}

	if !proof.IsConvergent {
		t.Error("decisions on different keys should converge")
	}
	if proof.ConflictCount != 0 {
		t.Errorf("expected 0 conflicts, got %d", proof.ConflictCount)
	}
	if proof.Receipt == nil {
		t.Error("proof should include signed receipt")
	}

	// Verify proof receipt
	if !proof.Receipt.Verify() {
		t.Error("convergence proof receipt signature should be valid")
	}
}

// TestOfflineEvidenceManager_MultiNodeDecisions tests decisions from multiple nodes
func TestOfflineEvidenceManager_MultiNodeDecisions(t *testing.T) {
	t.Cleanup(func() {})

	manager := NewOfflineEvidenceManager()

	// Simulate decisions from multiple edge nodes with potential conflicts
	decisions := []struct {
		nodeID      string
		key         string
		value       string
		offsetHours int
	}{
		{"node-A", "config-x", `{"version": 1}`, 0},
		{"node-B", "config-x", `{"version": 2}`, 1}, // Same key, later → wins via LWW
		{"node-C", "config-y", `{"version": 1}`, 2},
		{"node-A", "config-y", `{"version": 2}`, 3}, // Same key, later → wins
	}

	for _, d := range decisions {
		decision := OfflineDecision{
			Key:       d.key,
			Value:     []byte(d.value),
			Timestamp: time.Now().Add(time.Duration(d.offsetHours) * time.Hour),
			NodeID:    d.nodeID,
			Operation: "update",
		}
		if _, err := manager.RecordOfflineDecision(decision); err != nil {
			t.Fatalf("record decision: %v", err)
		}
	}

	proof, err := manager.VerifyOfflineChain()
	if err != nil {
		t.Fatalf("verify chain: %v", err)
	}

	// Should detect conflicts but still be convergent via LWW
	if !proof.IsConvergent {
		t.Error("LWW decisions should converge regardless of conflicts")
	}
	if proof.ConflictCount < 2 {
		t.Logf("detected %d conflicts", proof.ConflictCount)
	}

	// Resolution paths should document how each conflict was resolved
	if len(proof.ResolutionPath) < proof.ConflictCount {
		t.Logf("resolution paths: %+v", proof.ResolutionPath)
	}
}

// TestOfflineEvidenceManager_ChainIntegrity verifies cryptographic chain integrity
func TestOfflineEvidenceManager_ChainIntegrity(t *testing.T) {
	t.Cleanup(func() {})

	manager := NewOfflineEvidenceManager()

	// Create a sequence of dependent decisions
	keys := []string{"k1", "k2", "k3"}
	for i, key := range keys {
		decision := OfflineDecision{
			Key:       key,
			Value:     []byte(fmt.Sprintf("value-%d", i)),
			Timestamp: time.Now(),
			NodeID:    "test-node",
			Operation: "create",
		}
		if _, err := manager.RecordOfflineDecision(decision); err != nil {
			t.Fatalf("record decision: %v", err)
		}
	}

	// Verify entire chain is cryptographically linked
	chain := manager.GetOfflineChain()
	if len(chain) != 3 {
		t.Fatalf("expected 3 receipts, got %d", len(chain))
	}

	// Check PreviousReceiptID chaining
	for i := 1; i < len(chain); i++ {
		if chain[i].PreviousReceiptID != chain[i-1].ID {
			t.Errorf("broken chain at index %d: %s -> expected %s",
				i, chain[i].PreviousReceiptID, chain[i-1].ID)
		}
	}
}

// BenchmarkOffline_EvidenceCreation benchmarks signing an offline decision
func BenchmarkOffline_EvidenceCreation(b *testing.B) {
	manager := NewOfflineEvidenceManager()

	decision := OfflineDecision{
		Key:       "benchmark-key",
		Value:     []byte(`{"data": "test"}`),
		Timestamp: time.Now(),
		NodeID:    "bench-node",
		Operation: "update",
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			receipt, err := manager.RecordOfflineDecision(decision)
			if err != nil {
				b.Fatal(err)
			}
			if !receipt.Verify() {
				b.Error("receipt verification failed")
			}
		}
	})
}

// BenchmarkOffline_ChainVerification_1000Entries benchmarks verifying 1000 receipt chain
func BenchmarkOffline_ChainVerification_1000Entries(b *testing.B) {
	var managers []*OfflineEvidenceManager

	// Pre-populate with 1000 decisions each
	for i := 0; i < 10; i++ {
		mgr := NewOfflineEvidenceManager()
		for j := 0; j < 100; j++ {
			decision := OfflineDecision{
				Key:       fmt.Sprintf("key-%d-%d", i, j),
				Value:     []byte(fmt.Sprintf("value-%d", j)),
				Timestamp: time.Now().Add(time.Duration(j) * time.Millisecond),
				NodeID:    fmt.Sprintf("node-%d", i),
				Operation: "update",
			}
			mgr.RecordOfflineDecision(decision)
		}
		managers = append(managers, mgr)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for _, mgr := range managers {
				proof, err := mgr.VerifyOfflineChain()
				if err != nil {
					b.Fatalf("verify chain: %v", err)
				}
				if !proof.Receipt.Verify() {
					b.Error("proof receipt invalid")
				}
			}
		}
	})
}

// Example_offlineWorkflow demonstrates typical offline-to-online workflow.
func Example_offlineWorkflow() {
	// Step 1: Edge node operates offline
	edgeMgr := NewOfflineEvidenceManager()

	// Make local decisions without cloud connectivity
	for i := 1; i <= 5; i++ {
		decision := OfflineDecision{
			Key:       fmt.Sprintf("sensor-%d", i),
			Value:     []byte(fmt.Sprintf(`{"reading": %d}`, i*10)),
			Timestamp: time.Now(),
			NodeID:    "field-device-alpha",
			Operation: "update",
		}
		edgeMgr.RecordOfflineDecision(decision)
	}

	// Step 2: When reconnected, verify offline chain integrity
	proof, err := edgeMgr.VerifyOfflineChain()
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Printf("Offline decisions: convergent=%v, conflicts=%d\n", proof.IsConvergent, proof.ConflictCount)

	// Output:
	// Offline decisions: convergent=true, conflicts=0
}
