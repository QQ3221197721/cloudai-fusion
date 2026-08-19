package billing_test

import (
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/billing"
)

func TestVerifyStripeSignature_Valid(t *testing.T) {
	secret := "whsec_test123"
	payload := []byte(`{"test":"data"}`)

	sigHeader := billing.GenerateStripeSignatureHeader(payload, secret, time.Now())

	// Valid signature should pass
	if err := billing.VerifyStripeSignature(payload, sigHeader, secret); err != nil {
		t.Errorf("Expected valid signature, got error: %v", err)
	}
}

func TestVerifyStripeSignature_TamperedPayload(t *testing.T) {
	secret := "whsec_test123"
	payload := []byte(`{"test":"data"}`)

	sigHeader := billing.GenerateStripeSignatureHeader(payload, secret, time.Now())

	// Tampered payload should fail
	if err := billing.VerifyStripeSignature([]byte(`{"evil":"payload"}`), sigHeader, secret); err == nil {
		t.Error("Expected invalid signature for tampered payload")
	}
}

func TestVerifyStripeSignature_Expired(t *testing.T) {
	secret := "whsec_test123"
	payload := []byte(`{"test":"data"}`)

	// Signature older than tolerance (5 min) should fail
	old := time.Now().Add(-10 * time.Minute)
	sigHeader := billing.GenerateStripeSignatureHeader(payload, secret, old)

	if err := billing.VerifyStripeSignature(payload, sigHeader, secret); err == nil {
		t.Error("Expected expired signature to fail")
	}
}

func TestVerifyStripeSignature_EmptySecret(t *testing.T) {
	if err := billing.VerifyStripeSignature([]byte(`{}`), "t=123,v1=abc", ""); err == nil {
		t.Error("Expected error for empty webhook secret")
	}
}

func TestVerifyStripeSignature_MalformedHeader(t *testing.T) {
	if err := billing.VerifyStripeSignature([]byte(`{}`), "", "secret"); err == nil {
		t.Error("Expected error for empty signature header")
	}
}
