package evidence

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"testing"
	"time"
)

func TestReceiptBuild_SignsCorrectly(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	builder := NewReceiptBuilder("test.module", priv)

	input := map[string]interface{}{"key": "value", "count": 42}
	output := map[string]interface{}{"status": "ok", "processed": true}

	receipt, err := builder.Build("test.operation", input, output)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if receipt.ID == "" {
		t.Error("Receipt ID should be non-empty")
	}
	if receipt.Module != "test.module" {
		t.Errorf("Unexpected module: got %q, want %q", receipt.Module, "test.module")
	}
	if receipt.Operation != "test.operation" {
		t.Errorf("Unexpected operation: got %q, want %q", receipt.Operation, "test.operation")
	}
	if receipt.Timestamp.IsZero() {
		t.Error("Timestamp should not be zero")
	}
	if len(receipt.SignerPublicKey) != ed25519.PublicKeySize {
		t.Errorf("Public key size mismatch: got %d bytes, want %d", len(receipt.SignerPublicKey), ed25519.PublicKeySize)
	}
	if len(receipt.Signature) != ed25519.SignatureSize {
		t.Errorf("Signature size mismatch: got %d bytes, want %d", len(receipt.Signature), ed25519.SignatureSize)
	}
	if !receipt.Verify() {
		t.Error("Receipt signature should verify successfully")
	}
}

func TestReceiptVerify_ValidSignature(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	receipt := &Receipt{
		ID:                "rcpt_test_valid_signature",
		Module:            "evidence.test",
		Operation:         "verify.valid",
		Timestamp:         time.Now(),
		InputHash:         [32]byte{},
		OutputHash:        [32]byte{},
		SignerPublicKey:   priv.Public().(ed25519.PublicKey),
		PreviousReceiptID: "",
		Metadata:          make(map[string]string),
	}

	payload := receipt.signablePayload()
	signature := ed25519.Sign(priv, payload)
	receipt.Signature = signature

	if !receipt.Verify() {
		t.Error("Valid signature should pass verification")
	}
}

func TestReceiptVerify_TamperedData(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	receipt := &Receipt{
		ID:                "rcpt_test_tampered_data",
		Module:            "evidence.test",
		Operation:         "verify.original",
		Timestamp:         time.Now(),
		InputHash:         sha256.Sum256([]byte("original_data")),
		OutputHash:        sha256.Sum256([]byte("output_data")),
		SignerPublicKey:   priv.Public().(ed25519.PublicKey),
		PreviousReceiptID: "",
		Metadata:          make(map[string]string),
	}

	// Sign the original payload
	receipt.Signature = ed25519.Sign(priv, receipt.signablePayload())

	// Modify the receipt data after signing to simulate tampering
	receipt.Operation = "tampered_operation"

	if receipt.Verify() {
		t.Error("Tampered receipt should fail verification")
	}
}

func TestReceiptChain_OrderPreserved(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	builder := NewReceiptBuilder("chain.test", priv)

	var receipts []*Receipt
	for i := 0; i < 5; i++ {
		input := map[string]int{"index": i}
		output := map[string]int{"result": i * 2}

		receipt, err := builder.Build("chain.operation", input, output)
		if err != nil {
			t.Fatalf("Build #%d failed: %v", i, err)
		}
		receipts = append(receipts, receipt)
	}

	// Verify chain integrity
	err = VerifyChainOfReceipts(receipts)
	if err != nil {
		t.Errorf("Chain verification failed: %v", err)
	}

	// Verify each receipt in sequence
	for i := range receipts {
		if !receipts[i].Verify() {
			t.Errorf("Receipt #%d should verify individually", i)
		}
		if i > 0 && receipts[i].PreviousReceiptID != receipts[i-1].ID {
			t.Errorf("Receipt #%d chain link broken", i)
		}
	}
}

func BenchmarkReceiptBuild(b *testing.B) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatalf("Failed to generate key: %v", err)
	}

	builder := NewReceiptBuilder("benchmark.test", priv)

	input := map[string]interface{}{"user_id": 12345, "action": "create_order", "amount": 99.99}
	output := map[string]interface{}{"order_id": "ord_abc123", "status": "confirmed"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := builder.Build("order.create", input, output)
		if err != nil {
			b.Fatalf("Build failed: %v", err)
		}
	}
}
