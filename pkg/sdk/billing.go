package sdk

import (
	"context"
	"net/http"
	"time"
)

// BillingClient provides access to billing capabilities — recording usage with
// cryptographic receipts and tracking billable resources.
//
// Obtain it from a Client via the Billing field; do not construct it directly.
type BillingClient struct {
	client *Client
}

// UsageRecord describes a single usage event for billing.
type UsageRecord struct {
	// ResourceID uniquely identifies the billed resource, e.g. a GPU or namespace.
	ResourceID string `json:"resourceId"`
	// Namespace scopes the usage to a tenant namespace when appropriate.
	Namespace string `json:"namespace,omitempty"`
	// Category classifies the usage type, e.g. "gpu", "storage", "compute".
	Category string `json:"category"`
	// Amount is the quantitative amount of usage.
	Amount float64 `json:"amount"`
	// Unit specifies the measurement unit for the amount.
	Unit string `json:"unit"`
	// Timestamp is when the usage occurred.
	Timestamp time.Time `json:"timestamp,omitempty"`
}

// BillingReceipt confirms a recorded billing entry with a cryptographic receipt.
type BillingReceipt struct {
	// ID uniquely identifies this receipt record.
	ID string `json:"id"`
	// Amount matches the corresponding UsageRecord.Amount.
	Amount float64 `json:"amount"`
	// Unit matches the corresponding UsageRecord.Unit.
	Unit string `json:"unit"`
	// ReceiptHash is a SHA-256 digest over the usage data that serves as proof
	// of accurate reporting. Developers can independently verify this hash.
	ReceiptHash string `json:"receiptHash"`
	// SignedAt is when the receipt was generated.
	SignedAt time.Time `json:"signedAt"`
	// Signature is the server's detached signature over the receipt.
	Signature string `json:"signature"`
	// ResourceID is the billed resource identifier.
	ResourceID string `json:"resourceId"`
}

// RecordUsage records resource usage with cryptographic receipt.
//
// Callers should aggregate local measurements into UsageRecords and persist the
// returned receipts for audit reconciliation. The receipt's ReceiptHash enables
// independent verification of billing correctness.
func (b *BillingClient) RecordUsage(ctx context.Context, usage *UsageRecord) (*BillingReceipt, error) {
	var out BillingReceipt
	if err := b.client.do(ctx, http.MethodPost, "/api/v1/billing/records", usage, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
