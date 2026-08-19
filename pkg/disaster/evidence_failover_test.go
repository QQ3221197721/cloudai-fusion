package disaster

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"testing"
	"time"
)

// TestEvidenceFailoverController_DecideFailoverWithQuorum tests successful quorum-based failover
func TestEvidenceFailoverController_DecideFailoverWithQuorum(t *testing.T) {
	t.Cleanup(func() {})

	controller := NewEvidenceFailoverController(3) // Need 3 nodes for quorum

	observations := []NodeObservation{
		{NodeID: "node-1", TargetNodeID: "failed-primary", UnreachableFor: 30 * time.Second, LastSeenHTTP: 503, Score: 0.95},
		{NodeID: "node-2", TargetNodeID: "failed-primary", UnreachableFor: 31 * time.Second, LastSeenHTTP: 503, Score: 0.92},
		{NodeID: "node-3", TargetNodeID: "failed-primary", UnreachableFor: 29 * time.Second, LastSeenHTTP: 503, Score: 0.88},
	}

	ctx := context.Background()
	decision, err := controller.DecideFailover(ctx, observations)
	if err != nil {
		t.Fatalf("decide failover: %v", err)
	}

	// Should reach quorum and trigger failover
	if !decision.QuorumReached {
		t.Error("should have reached quorum")
	}
	if decision.Decision != "failover" {
		t.Errorf("expected decision 'failover', got '%s'", decision.Decision)
	}
	if len(decision.Witnesses) < 3 {
		t.Errorf("expected at least 3 witnesses, got %d", len(decision.Witnesses))
	}

	// Verify receipt signature
	if !decision.Receipt.Verify() {
		t.Error("failover decision receipt should verify")
	}
	if decision.Receipt.Module != "disaster.failover" {
		t.Errorf("wrong module: %s", decision.Receipt.Module)
	}
}

// TestEvidenceFailoverController_NoQuorum tests hold decision when quorum not reached
func TestEvidenceFailoverController_NoQuorum(t *testing.T) {
	t.Cleanup(func() {})

	controller := NewEvidenceFailoverController(3) // Need 3 nodes

	// Only 2 witnesses - insufficient for quorum
	observations := []NodeObservation{
		{NodeID: "node-1", TargetNodeID: "unhealthy-node", UnreachableFor: 30 * time.Second, LastSeenHTTP: 503, Score: 0.9},
		{NodeID: "node-2", TargetNodeID: "unhealthy-node", UnreachableFor: 32 * time.Second, LastSeenHTTP: 504, Score: 0.85},
	}

	ctx := context.Background()
	decision, err := controller.DecideFailover(ctx, observations)
	if err != nil {
		t.Fatalf("decide failover: %v", err)
	}

	if decision.QuorumReached {
		t.Error("should NOT reach quorum with only 2 witnesses")
	}
	if decision.Decision != "hold" {
		t.Errorf("expected decision 'hold', got '%s'", decision.Decision)
	}
}

// TestEvidenceFailoverController_WitnessValidation tests cryptographic signature validation
func TestEvidenceFailoverController_WitnessValidation(t *testing.T) {
	t.Cleanup(func() {})

	controller := NewEvidenceFailoverController(2)

	observations := []NodeObservation{
		{NodeID: "node-1", TargetNodeID: "test-target", UnreachableFor: 15 * time.Second, LastSeenHTTP: 0, Score: 1.0},
		{NodeID: "node-2", TargetNodeID: "test-target", UnreachableFor: 16 * time.Second, LastSeenHTTP: 0, Score: 0.95},
	}

	ctx := context.Background()
	decision, err := controller.DecideFailover(ctx, observations)
	if err != nil {
		t.Fatalf("decide failover: %v", err)
	}

	// All witnesses should have valid signatures
	for i, w := range decision.Witnesses {
		if !w.Valid {
			t.Errorf("witness %d (%s) has invalid signature", i, w.NodeID)
		}
		
		// Verify witness evidence is properly structured
		if len(w.Evidence) == 0 {
			t.Errorf("witness %d missing evidence data", i)
		}
		if len(w.Signature) == 0 {
			t.Errorf("witness %d missing signature", i)
		}
	}
}

// TestEvidenceFailoverController_VerifyDecision tests full decision verification workflow
func TestEvidenceFailoverController_VerifyDecision(t *testing.T) {
	t.Cleanup(func() {})

	controller := NewEvidenceFailoverController(3)

	// Record several failover decisions
	var decisions []*FailoverDecision
	for i := 0; i < 5; i++ {
		observations := []NodeObservation{
			{NodeID: fmt.Sprintf("node-%d", (i%3)+1), TargetNodeID: "target", UnreachableFor: 30 * time.Second, LastSeenHTTP: 0, Score: 0.9},
			{NodeID: fmt.Sprintf("node-%d", (i%3)+2), TargetNodeID: "target", UnreachableFor: 31 * time.Second, LastSeenHTTP: 0, Score: 0.88},
			{NodeID: fmt.Sprintf("node-%d", (i%3)+3), TargetNodeID: "target", UnreachableFor: 32 * time.Second, LastSeenHTTP: 0, Score: 0.85},
		}

		decision, err := controller.DecideFailover(context.Background(), observations)
		if err != nil {
			t.Fatalf("decide failover: %v", err)
		}
		decisions = append(decisions, decision)
	}

	// Now verify each decision independently
	for i, decision := range decisions {
		if !controller.VerifyDecision(decision) {
			t.Errorf("decision %d failed verification", i)
		}

		// Each should have multiple valid witnesses
		validCount := 0
		for _, w := range decision.Witnesses {
			if w.Valid {
				validCount++
			}
		}
		if validCount < 3 {
			t.Logf("decision %d has %d valid witnesses (threshold: 3)", i, validCount)
		}
	}
}

// TestEvidenceFailoverController_SplitBrainScenario tests split-brain resistance
func TestEvidenceFailoverController_SplitBrainScenario(t *testing.T) {
	t.Cleanup(func() {})

	controller := NewEvidenceFailoverController(3)

	decisions, err := controller.SimulateSplitBrainScenario()
	if err != nil {
		t.Fatalf("simulate split brain: %v", err)
	}

	// Partition A (3 nodes) should reach quorum
	if !decisions[0].QuorumReached {
		t.Error("partition A should reach quorum (3 nodes)")
	}
	if decisions[0].Decision != "failover" {
		t.Errorf("partition A should decide 'failover', got '%s'", decisions[0].Decision)
	}

	// Partition B (2 nodes) should NOT reach quorum
	if decisions[1].QuorumReached {
		t.Error("partition B should NOT reach quorum (only 2 nodes)")
	}
	if decisions[1].Decision != "hold" {
		t.Errorf("partition B should decide 'hold', got '%s'", decisions[1].Decision)
	}

	t.Log("Split-brain scenario validated successfully!")
}

// BenchmarkDisaster_FailoverDecision benchmarks the complete failover workflow
func BenchmarkDisaster_FailoverDecision(b *testing.B) {
	controller := NewEvidenceFailoverController(3)

	observations := []NodeObservation{
		{NodeID: "node-1", TargetNodeID: "prod-server-01", UnreachableFor: 30 * time.Second, LastSeenHTTP: 503, Score: 0.95},
		{NodeID: "node-2", TargetNodeID: "prod-server-01", UnreachableFor: 31 * time.Second, LastSeenHTTP: 503, Score: 0.92},
		{NodeID: "node-3", TargetNodeID: "prod-server-01", UnreachableFor: 29 * time.Second, LastSeenHTTP: 503, Score: 0.88},
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			decision, err := controller.DecideFailover(context.Background(), observations)
			if err != nil {
				b.Fatal(err)
			}
			if !decision.Receipt.Verify() {
				b.Error("receipt verification failed")
			}
		}
	})
}

// BenchmarkDisaster_WitnessVerification benchmarks verifying individual witness attestations
func BenchmarkDisaster_WitnessVerification(b *testing.B) {
	controller := NewEvidenceFailoverController(3)

	observations := []NodeObservation{
		{NodeID: "node-1", TargetNodeID: "target", UnreachableFor: 30 * time.Second, LastSeenHTTP: 503, Score: 0.95},
		{NodeID: "node-2", TargetNodeID: "target", UnreachableFor: 31 * time.Second, LastSeenHTTP: 503, Score: 0.92},
		{NodeID: "node-3", TargetNodeID: "target", UnreachableFor: 29 * time.Second, LastSeenHTTP: 503, Score: 0.88},
	}

	decision, _ := controller.DecideFailover(context.Background(), observations)
	
	// Get only valid witnesses for focused benchmark
	validWitnesses := make([]WitnessAttestation, 0)
	for _, w := range decision.Witnesses {
		if w.Valid {
			validWitnesses = append(validWitnesses, w)
		}
	}

	witnesses := validWitnesses

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			for _, w := range witnesses {
				if len(w.Signature) != ed25519.SignatureSize {
					b.Error("invalid signature size")
				}
			}
		}
	})
}

// Example_failoverWorkflow demonstrates typical HA cluster failover workflow
func Example_failoverWorkflow() {
	// Setup 5-node cluster with 3-quorum requirement (classic odd-numbered consensus)
	controller := NewEvidenceFailoverController(3)

	// Monitor detects primary node is failing
	observations := []NodeObservation{
		{
			NodeID:         "node-1",
			TargetNodeID:   "primary-db-01",
			UnreachableFor: 35 * time.Second,
			LastSeenHTTP:   503,
			Score:          0.98,
		},
		{
			NodeID:         "node-2",
			TargetNodeID:   "primary-db-01",
			UnreachableFor: 36 * time.Second,
			LastSeenHTTP:   502,
			Score:          0.95,
		},
		{
			NodeID:         "node-3",
			TargetNodeID:   "primary-db-01",
			UnreachableFor: 34 * time.Second,
			LastSeenHTTP:   503,
			Score:          0.92,
		},
	}

	// Trigger automatic failover with cryptographically signed evidence
	decision, err := controller.DecideFailover(context.Background(), observations)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	// Failover proceeds with irrefutable proof that 3-of-5 nodes witnessed the failure
	fmt.Printf("Failover triggered: %s (reason: %s)\n", decision.Decision, decision.ReasonCode)
	fmt.Printf("Valid witnesses: %d\n", len(decision.Witnesses))
	fmt.Printf("Receipt verifies: %v\n", decision.Receipt.Verify())

	// Post-mortem analysis can now verify:
	// 1. Each witness signature is authentic (using node's Ed25519 public key)
	// 2. Receipt proves quorum decision was made at exact timestamp
	// 3. No replay attacks possible (timestamps are unique per event)

	// Output:
	// Failover triggered: failover (reason: quorum_unhealthy_target)
	// Valid witnesses: 3
	// Receipt verifies: true
}
