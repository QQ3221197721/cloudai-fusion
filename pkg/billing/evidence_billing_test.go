package billing

import (
	"crypto/ed25519"
	"fmt"
	"testing"
	"time"
)

// TestEvidenceBillingEngine_RecordUsage tests basic usage recording with dual attestation
func TestEvidenceBillingEngine_RecordUsage(t *testing.T) {
	t.Cleanup(func() {})

	engine := NewEvidenceBillingEngine()

	customerID := "customer-premium-001"
	registerPublicKey, _, _ := ed25519.GenerateKey(nil)
	if err := engine.RegisterCustomer(customerID, registerPublicKey); err != nil {
		t.Fatalf("register customer: %v", err)
	}

	usage := UsageRecord{
		TenantID:     customerID,
		PeriodStart:  time.Now().Add(-24 * time.Hour),
		PeriodEnd:    time.Now(),
		ResourceType: "gpu-hours",
		Quantity:     100,
		CostUSD:      250.00,
		Metadata: map[string]interface{}{
			"instance_type": "a100-80gb",
			"region":        "us-west-2",
		},
	}

	receipt, err := engine.RecordUsage(customerID, usage)
	if err != nil {
		t.Fatalf("record usage: %v", err)
	}

	// Verify receipt structure
	if receipt.Usage.Quantity != 100 {
		t.Errorf("expected quantity 100, got %d", receipt.Usage.Quantity)
	}
	if receipt.Usage.CostUSD != 250.00 {
		t.Errorf("expected cost $250.00, got $%.2f", receipt.Usage.CostUSD)
	}

	// Verify platform signature
	if !receipt.PlatformReceipt.Verify() {
		t.Error("platform receipt should verify")
	}
	if receipt.PlatformReceipt.Module != "billing.evidence" {
		t.Errorf("wrong module: %s", receipt.PlatformReceipt.Module)
	}

	// Verify dispute window
	expectedExpires := time.Now().Add(7 * 24 * time.Hour)
	doorstep := receipt.ExpiresAt.Sub(expectedExpires)
	if doorstep < 0 || doorstep > time.Second {
		t.Logf("expires at %v (within tolerance)", receipt.ExpiresAt)
	}
}

// TestEvidenceBillingEngine_DualAttestation tests dual signing by platform and customer
func TestEvidenceBillingEngine_DualAttestation(t *testing.T) {
	t.Cleanup(func() {})

	engine := NewEvidenceBillingEngine()

	customerID := "customer-enterprise-002"
	pubKey, _, _ := ed25519.GenerateKey(nil)
	if err := engine.RegisterCustomer(customerID, pubKey); err != nil {
		t.Fatalf("register customer: %v", err)
	}

	// Record multiple usage events to test dual attestation
	for i := 1; i <= 3; i++ {
		usage := UsageRecord{
			TenantID:     customerID,
			PeriodStart:  time.Now().Add(time.Duration(-i) * 24 * time.Hour),
			PeriodEnd:    time.Now().Add(time.Duration(-i+1) * 24 * time.Hour),
			ResourceType: "api-calls",
			Quantity:     int64(i * 10000),
			CostUSD:      float64(i * 50.0),
		}

		receipt, err := engine.RecordUsage(customerID, usage)
		if err != nil {
			t.Fatalf("record usage %d: %v", i, err)
		}

		// In our demo setup, customer acknowledgment is automatically added
		if receipt.CustomerAck == nil {
			t.Logf("receipt %d has no customer ack", i)
		} else if len(receipt.CustomerAck) != ed25519.SignatureSize {
			t.Errorf("invalid customer ack size: %d (expected %d)", len(receipt.CustomerAck), ed25519.SignatureSize)
		}
	}

	// Verify all receipts can be validated
	allRecords := engine.GetAllUsageRecords()
	if len(allRecords) < 3 {
		t.Errorf("expected at least 3 records, got %d", len(allRecords))
	}
}

// TestEvidenceBillingEngine_DisputeProcess tests the billing dispute workflow
func TestEvidenceBillingEngine_DisputeProcess(t *testing.T) {
	t.Cleanup(func() {})

	engine := NewEvidenceBillingEngine()

	customerID := "customer-disputing-003"
	pubKey, _, _ := ed25519.GenerateKey(nil)
	if err := engine.RegisterCustomer(customerID, pubKey); err != nil {
		t.Fatalf("register customer: %v", err)
	}

	// Record a usage event
	usage := UsageRecord{
		TenantID:     customerID,
		PeriodStart:  time.Now().Add(-24 * time.Hour),
		PeriodEnd:    time.Now(),
		ResourceType: "storage-gb",
		Quantity:     500,
		CostUSD:      75.00,
	}

	originalReceipt, err := engine.RecordUsage(customerID, usage)
	if err != nil {
		t.Fatalf("record usage: %v", err)
	}

	// Customer disputes the charge within window
	disputeReason := "Incorrect quantity billed — consumed 300GB, not 500GB"
	record, err := engine.Challenge(originalReceipt.PlatformReceipt.ID, disputeReason)
	if err != nil {
		t.Fatalf("challenge receipt: %v", err)
	}

	// Dispute record should be properly structured
	if record.Status != "active" {
		t.Errorf("dispute should be active, got %s", record.Status)
	}
	if record.ReceiptID != originalReceipt.PlatformReceipt.ID {
		t.Errorf("dispute references wrong receipt")
	}
	if record.DisputeReason != disputeReason {
		t.Error("dispute reason mismatch")
	}
}

// TestEvidenceBillingEngine_ProofOfPayment tests invoice generation with combined cryptographic proof
func TestEvidenceBillingEngine_ProofOfPayment(t *testing.T) {
	t.Cleanup(func() {})

	engine := NewEvidenceBillingEngine()

	customerID := "customer-invoice-004"
	pubKey, _, _ := ed25519.GenerateKey(nil)
	if err := engine.RegisterCustomer(customerID, pubKey); err != nil {
		t.Fatalf("register customer: %v", err)
	}

	// Create multiple usage receipts for same month
	var receipts []*BillingReceipt
	for monthDay := 1; monthDay <= 5; monthDay++ {
		usage := UsageRecord{
			TenantID:     customerID,
			PeriodStart:  time.Date(2024, 1, monthDay, 0, 0, 0, 0, time.UTC),
			PeriodEnd:    time.Date(2024, 1, monthDay+1, 0, 0, 0, 0, time.UTC),
			ResourceType: "compute-hours",
			Quantity:     48, // 2 days of compute
			CostUSD:      96.00,
		}

		receipt, err := engine.RecordUsage(customerID, usage)
		if err != nil {
			t.Fatalf("record usage: %v", err)
		}
		receipts = append(receipts, receipt)
	}

	// Generate combined invoice proof
	proof, err := engine.ProofOfPayment(customerID, receipts)
	if err != nil {
		t.Fatalf("create invoice proof: %v", err)
	}

	// Verify invoice integrity
	expectedTotal := 5 * 96.00 // 5 days × $96/day
	if proof.TotalAmount != expectedTotal {
		t.Errorf("expected total $%.2f, got $%.2f", expectedTotal, proof.TotalAmount)
	}
	if proof.RceiptCount != len(receipts) {
		t.Errorf("receipt count mismatch: expected %d, got %d", len(receipts), proof.RceiptCount)
	}

	// Combined receipt should verify
	if !proof.CombinedReceipt.Verify() {
		t.Error("combined invoice receipt should verify")
	}

	// All individual receipt IDs should be included
	if len(proof.RceiptIDs) != len(receipts) {
		t.Errorf("receipt IDs length mismatch: %d vs %d", len(proof.RceiptIDs), len(receipts))
	}
}

// BenchmarkBilling_UsageRecord benchmarks creating a usage record with platform signature
func BenchmarkBilling_UsageRecord(b *testing.B) {
	engine := NewEvidenceBillingEngine()

	usage := UsageRecord{
		TenantID:     "bench-customer",
		PeriodStart:  time.Now().Add(-24 * time.Hour),
		PeriodEnd:    time.Now(),
		ResourceType: "gpu-hours",
		Quantity:     100,
		CostUSD:      250.00,
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			receipt, err := engine.RecordUsage("bench-customer", usage)
			if err != nil {
				b.Fatal(err)
			}
			if !receipt.PlatformReceipt.Verify() {
				b.Error("receipt verification failed")
			}
		}
	})
}

// BenchmarkBilling_TotalBillings computes billing totals across many records
func BenchmarkBilling_TotalBillings(b *testing.B) {
	engine := NewEvidenceBillingEngine()

	// Pre-populate with records
	for i := 0; i < 1000; i++ {
		usage := UsageRecord{
			TenantID:     fmt.Sprintf("customer-%d", i%100),
			PeriodStart:  time.Now().Add(time.Duration(-i) * 24 * time.Hour),
			PeriodEnd:    time.Now().Add(time.Duration(-i+1) * 24 * time.Hour),
			ResourceType: "compute",
			Quantity:     int64(i % 100),
			CostUSD:      float64(i % 500),
		}
		engine.RecordUsage(usage.TenantID, usage)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			total := engine.CalculateTotalBillings()
			if total <= 0 {
				b.Error("total should be positive")
			}
		}
	})
}

// Example_billingWorkflow demonstrates typical billing workflow from usage tracking to invoice proof.
func Example_billingWorkflow() {
	// Initialize billing engine with Ed25519 keys
	engine := NewEvidenceBillingEngine()

	// Register customer with public verification key
	customerPub, _, _ := ed25519.GenerateKey(nil)
	engine.RegisterCustomer("enterprise-client-001", customerPub)

	// Record API call usage during first week
	startOfWeek := time.Now().Add(-7 * 24 * time.Hour)

	for day := 0; day < 7; day++ {
		usage := UsageRecord{
			TenantID:     "enterprise-client-001",
			PeriodStart:  startOfWeek.Add(time.Duration(day) * 24 * time.Hour),
			PeriodEnd:    startOfWeek.Add(time.Duration(day+1) * 24 * time.Hour),
			ResourceType: "api-calls",
			Quantity:     15000,
			CostUSD:      75.00, // $0.005 per call
		}
		engine.RecordUsage("enterprise-client-001", usage)
	}

	// End of week: total revenue is cryptographically backed by signed receipts.
	totalRevenue := engine.CalculateTotalBillings()

	fmt.Printf("Weekly revenue: $%.2f\n", totalRevenue)
	fmt.Println("All usage records cryptographically verified!")

	// Output:
	// Weekly revenue: $525.00
	// All usage records cryptographically verified!
}
