// Package billing - SaaS Billing with usage-based pricing and subscription management
package billing

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// SAAS BILLING ENGINE WITH USAGE-BASED PRICING
// ============================================================================

// SaaSBilling handles complete SaaS billing lifecycle
type SaaSBilling struct {
	mu sync.RWMutex
	logger *logrus.Logger
	
	// Subscription records
	subscriptions map[string]*Subscription
	
	// Usage data
	usageData map[string][]UsageRecord
	
	// Invoice records
	invoices []*Invoice
	
	// Stripe integration
	stripeIntegration *StripeIntegration
	
	// Metrics
	metrics *BillingMetrics
}

// Subscription represents a customer subscription
type Subscription struct {
	ID           string            `json:"id"`
	TenantID     string            `json:"tenant_id"`
	Status       SubscriptionStatus `json:"status"`
	Plan         string            `json:"plan"`
	StartDate    time.Time         `json:"start_date"`
	EndDate      time.Time         `json:"end_date"`
	AutoRenew    bool              `json:"auto_renew"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	CurrentPeriodStart time.Time `json:"current_period_start"`
	CurrentPeriodEnd time.Time `json:"current_period_end"`
}

// SubscriptionStatus describes subscription status
type SubscriptionStatus string

const (
	StatusActive   SubscriptionStatus = "active"
	StatusCanceled SubscriptionStatus = "canceled"
	StatusExpired  SubscriptionStatus = "expired"
	StatusTrial    SubscriptionStatus = "trial"
)

// UsageRecord tracks usage for billing
type UsageRecord struct {
	TenantID     string    `json:"tenant_id"`
	PeriodStart  time.Time `json:"period_start"`
	PeriodEnd    time.Time `json:"period_end"`
	ResourceType string    `json:"resource_type"`
	Quantity     int64     `json:"quantity"`
	CostUSD      float64   `json:"cost_usd"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// Invoice represents a customer invoice
type Invoice struct {
	ID           string            `json:"id"`
	TenantID     string            `json:"tenant_id"`
	Subscription string            `json:"subscription"`
	Status       InvoiceStatus     `json:"status"`
	AmountTotal  float64           `json:"amount_total"`
	Subtotal     float64           `json:"subtotal"`
	TaxAmount    float64           `json:"tax_amount"`
	Currency     string            `json:"currency"`
	DueDate      time.Time         `json:"due_date"`
	CreatedAt    time.Time         `json:"created_at"`
	PaidAt       time.Time         `json:"paid_at,omitempty"`
	Lines        []InvoiceLineItem `json:"lines"`
}

// InvoiceStatus describes invoice status
type InvoiceStatus string

const (
	InvoiceDraft InvoiceStatus = "draft"
	InvoiceSent InvoiceStatus = "sent"
	InvoicePaid InvoiceStatus = "paid"
	InvoiceVoided InvoiceStatus = "voided"
	InvoiceOverdue InvoiceStatus = "overdue"
)

// InvoiceLineItem represents a line item in an invoice
type InvoiceLineItem struct {
	Description string            `json:"description"`
	Quantity    int64             `json:"quantity"`
	UnitPrice   float64           `json:"unit_price"`
	Amount      float64           `json:"amount"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// ============================================================================
// CORE BILLING FUNCTIONS
// ============================================================================

// NewSaaSBilling creates SaaS billing engine
func NewSaaSBilling(logger *logrus.Logger, stripeIntegration *StripeIntegration) (*SaaSBilling, error) {
	billing := &SaaSBilling{
		logger: logger,
		subscriptions: make(map[string]*Subscription),
		usageData: make(map[string][]UsageRecord),
		invoices: make([]*Invoice, 0),
		stripeIntegration: stripeIntegration,
		metrics: NewBillingMetrics(),
	}
	
	logger.Info("SaaS billing engine initialized")
	return billing, nil
}

// CreateSubscription creates new subscription for tenant
func (b *SaaSBilling) CreateSubscription(ctx context.Context, tenantID, plan string, autoRenew bool) (*Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	now := time.Now()
	endDate := now.AddDate(0, 1, 0) // Monthly by default
	
	sub := &Subscription{
		ID: fmt.Sprintf("sub_%s_%d", tenantID, now.Unix()),
		TenantID: tenantID,
		Plan: plan,
		Status: StatusActive,
		StartDate: now,
		EndDate: endDate,
		AutoRenew: autoRenew,
		CurrentPeriodStart: now,
		CurrentPeriodEnd: now.AddDate(0, 1, 0),
		Metadata: make(map[string]interface{}),
	}
	
	b.subscriptions[tenantID] = sub
	b.metrics.IncrementSubscription()
	
	b.logger.WithField("subscription", sub.ID).Info("Created subscription")
	return sub, nil
}

// CancelSubscription cancels tenant subscription
func (b *SaaSBilling) CancelSubscription(ctx context.Context, tenantID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	sub, exists := b.subscriptions[tenantID]
	if !exists {
		return fmt.Errorf("subscription not found for tenant %s", tenantID)
	}
	
	sub.Status = StatusCanceled
	b.metrics.IncrementCancellation()
	
	b.logger.WithField("subscription", sub.ID).Info("Cancelled subscription")
	return nil
}

// RecordUsage records usage for billing
func (b *SaaSBilling) RecordUsage(ctx context.Context, tenantID, resourceType string, quantity int64, costUSD float64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	record := UsageRecord{
		TenantID: tenantID,
		ResourceType: resourceType,
		Quantity: quantity,
		CostUSD: costUSD,
		PeriodStart: time.Now().AddDate(0, 0, -1),
		PeriodEnd: time.Now(),
		Metadata: make(map[string]interface{}),
	}
	
	b.usageData[tenantID] = append(b.usageData[tenantID], record)
	b.metrics.IncrementUsage()
	
	b.logger.WithFields(logrus.Fields{
		"tenant": tenantID,
		"resource": resourceType,
		"quantity": quantity,
	}).Debug("Recorded usage")
	
	return nil
}

// GenerateInvoice generates invoice for tenant
func (b *SaaSBilling) GenerateInvoice(ctx context.Context, tenantID string) (*Invoice, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	
	sub, exists := b.subscriptions[tenantID]
	if !exists {
		return nil, fmt.Errorf("no active subscription for tenant %s", tenantID)
	}
	
	if sub.Status != StatusActive {
		return nil, fmt.Errorf("subscription not active for tenant %s", tenantID)
	}
	
	// Get usage data
	records := b.usageData[tenantID]
	
	// Build invoice lines
	lines := make([]InvoiceLineItem, 0)
	subtotal := 0.0
	
	for _, record := range records {
		line := InvoiceLineItem{
			Description: fmt.Sprintf("%s usage (%d)", record.ResourceType, record.Quantity),
			Quantity: record.Quantity,
			UnitPrice: record.CostUSD / float64(record.Quantity),
			Amount: record.CostUSD,
			Metadata: make(map[string]string),
		}
		
		lines = append(lines, line)
		subtotal += record.CostUSD
	}
	
	// Calculate tax (simplified 8%)
	taxAmount := subtotal * 0.08
	
	// Create invoice
	invoice := &Invoice{
		ID: fmt.Sprintf("inv_%s_%d", tenantID, time.Now().Unix()),
		TenantID: tenantID,
		Subscription: sub.ID,
		Status: InvoiceDraft,
		AmountTotal: subtotal + taxAmount,
		Subtotal: subtotal,
		TaxAmount: taxAmount,
		Currency: "USD",
		DueDate: time.Now().AddDate(0, 0, 30),
		CreatedAt: time.Now(),
		Lines: lines,
	}
	
	b.invoices = append(b.invoices, invoice)
	b.metrics.IncrementInvoice()
	
	b.logger.WithField("invoice", invoice.ID).Info("Generated invoice")
	return invoice, nil
}

// SendInvoice sends invoice to customer via Stripe
func (b *SaaSBilling) SendInvoice(ctx context.Context, invoice *Invoice) error {
	if b.stripeIntegration == nil {
		return fmt.Errorf("stripe integration not configured")
	}
	
	invoice.Status = InvoiceSent
	
	err := b.stripeIntegration.ChargeCustomer(invoice)
	if err != nil {
		invoice.Status = InvoiceOverdue
		return err
	}
	
	invoice.Status = InvoicePaid
	invoice.PaidAt = time.Now()
	
	b.metrics.RecordPayment()
	
	return nil
}

// ============================================================================
// METRICS TRACKING
// ============================================================================

// BillingMetrics tracks billing metrics
type BillingMetrics struct {
	mu sync.RWMutex
	SubscriptionsCreated int
	Cancellations int
	UsagesRecorded int
	InvoicesGenerated int
	PaymentsReceived int
}

func NewBillingMetrics() *BillingMetrics {
	return &BillingMetrics{}
}

func (m *BillingMetrics) IncrementSubscription() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SubscriptionsCreated++
}

func (m *BillingMetrics) IncrementCancellation() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Cancellations++
}

func (m *BillingMetrics) IncrementUsage() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.UsagesRecorded++
}

func (m *BillingMetrics) IncrementInvoice() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.InvoicesGenerated++
}

func (m *BillingMetrics) RecordPayment() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PaymentsReceived++
}
