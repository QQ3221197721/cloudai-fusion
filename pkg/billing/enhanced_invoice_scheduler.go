// Package billing - Enhanced invoice scheduling with cost optimization
// ENHANCED PATENT #34: Intelligent cost optimization for invoice scheduling
package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// ENHANCED INVOICE SCHEDULER WITH COST OPTIMIZATION (Patent #34)
// ============================================================================

// EnhancedInvoiceScheduler orchestrates intelligent invoice scheduling with cost optimization
type EnhancedInvoiceScheduler struct {
	mu sync.RWMutex
	logger *logrus.Logger
	
	// Invoice records
	invoices []*InvoiceRecord
	
	// Billing configuration
	config       SchedulerConfig
	
	// Cost optimization engine
	costOptimizer *CostOptimizer
	
	// Usage tracking
	usageTracker *UsageTracker
	
	// Invoice generation history
	invoiceHistory []InvoiceHistoryRecord
	
	// Metrics
	metrics *InvoiceMetrics
	
	// Latest state
	lastRunTime time.Time
	nextRunTime time.Time
	
	// Progress tracking
	scheduleProgress *ScheduleProgress
}

// InvoiceRecord represents an invoice record for billing
type InvoiceRecord struct {
	ID           string            `json:"id"`
	TenantID     string            `json:"tenant_id"`
	BillingPeriod   BillingPeriod    `json:"billing_period"`
	Status      InvoiceStatus     `json:"status"`
	TotalAmount float64           `json:"total_amount"`
	LineItems    []LineItem        `json:"line_items"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	GeneratedAt  time.Time         `json:"generated_at"`
	PaidAt       time.Time         `json:"paid_at,omitempty"`
	VoidedAt     time.Time         `json:"voided_at,omitempty"`
}

// InvoiceStatus describes invoice status
type InvoiceStatus string

const (
	InvoiceDraft InvoiceStatus = "draft"
	InvoicePending InvoiceStatus = "pending"
	InvoiceSent InvoiceStatus = "sent"
	InvoicePaid InvoiceStatus = "paid"
	InvoiceVoided InvoiceStatus = "voided"
	InvoiceOverdue InvoiceStatus = "overdue"
)

// BillingPeriod defines billing period
type BillingPeriod struct {
	StartDate time.Time `json:"start_date"`
	EndDate   time.Time `json:"end_date"`
	DurationDays int      `json:"duration_days"`
	Type      string      `json:"type"` // monthly, quarterly, annual
}

// LineItem represents a line item in an invoice
type LineItem struct {
	Description string            `json:"description"`
	Quantity    int               `json:"quantity"`
	UnitPrice   float64           `json:"unit_price"`
	Subtotal    float64           `json:"subtotal"`
	TaxRate     float64           `json:"tax_rate"`
	TaxAmount   float64           `json:"tax_amount"`
	Totals      map[string]float64 `json:"totals,omitempty"`
	Discounts   []Discount        `json:"discounts,omitempty"`
}

// Discount describes a discount applied to an invoice
type Discount struct {
	Code           string          `json:"code"`
	Type           string          `json:"type"` // percentage, fixed
	Amount         float64         `json:"amount"`
	Percentage     float64         `json:"percentage"`
	MinPurchase    float64         `json:"min_purchase"`
	ApplicableTo   []string        `json:"applicable_to"`
}

// ============================================================================
// SCHEDULER CONFIGURATION AND COST OPTIMIZER
// ============================================================================

// SchedulerConfig defines invoice scheduling configuration
type SchedulerConfig struct {
	DefaultBillingCycle string        `json:"default_billing_cycle"` // monthly, quarterly, annual
	RoundingPrecision int           `json:"rounding_precision"`
	Currency          string        `json:"currency"`
	TaxRate           float64       `json:"tax_rate"`
	AutoSend          bool          `json:"auto_send"`
	SendReminders     bool          `json:"send_reminders"`
	ReminderDays      int           `json:"reminder_days"`
	CostOptimization  bool          `json:"cost_optimization"`
	DiscountStrategy  string        `json:"discount_strategy"`
	FailOnInsufficientFunds bool `json:"fail_on_insufficient_funds"`
}

// CostOptimizer performs cost optimization analysis
type CostOptimizer struct {
	mu sync.RWMutex
	logger *logrus.Logger
	
	// Historical usage data
	usageHistory map[string][]UsageRecord
	
	// Pricing models
	pricingModels map[string]*PricingModel
	
	// Recent optimizations
	recentOptimizations []OptimizationRecommendation
	
	maxHistorySize int
}

// UsageRecord tracks usage for cost optimization
type UsageRecord struct {
	TenantID      string    `json:"tenant_id"`
	PeriodStart   time.Time `json:"period_start"`
	PeriodEnd     time.Time `json:"period_end"`
	UsageData     map[string]float64 `json:"usage_data"`
	CostUSD       float64   `json:"cost_usd"`
	AverageUsage  float64   `json:"average_usage"`
	PeakUsage     float64   `json:"peak_usage"`
}

// PricingModel defines pricing model
type PricingModel struct {
	Name       string                 `json:"name"`
	Version    int                    `json:"version"`
	Prices     map[string]float64     `json:"prices"`
	Discounts  map[string]Discount    `json:"discounts"`
	Criteria   map[string]interface{} `json:"criteria"`
	UpdatedAt  time.Time              `json:"updated_at"`
}

// OptimizationRecommendation provides cost optimization recommendation
type OptimizationRecommendation struct {
	TenantID        string    `json:"tenant_id"`
	RecommendationType string    `json:"recommendation_type"` // rightsizing, reserved_instances, spot, etc.
	EstimatedSavings float64  `json:"estimated_savings"`
	ImplementationEffort string `json:"implementation_effort"` // low, medium, high
	Priority        string      `json:"priority"` // low, medium, high, critical
	CreatedAt       time.Time   `json:"created_at"`
	Description     string      `json:"description"`
}

// ============================================================================
// USAGE TRACKER
// ============================================================================

// UsageTracker tracks usage for invoicing
type UsageTracker struct {
	mu sync.RWMutex
	logger *logrus.Logger
	
	// Current usage data
	currentUsage map[string]map[string]float64
	
	// Historical usage
	history []UsageSnapshot
	
	maxHistorySize int
}

// UsageSnapshot captures usage snapshot at a point in time
type UsageSnapshot struct {
	Timestamp   time.Time         `json:"timestamp"`
	UsageData   map[string]float64 `json:"usage_data"`
	TenantID    string            `json:"tenant_id"`
	TotalUsage  float64           `json:"total_usage"`
}

// ============================================================================
// MAIN ORCHESTRATION LOGIC
// ============================================================================

// NewEnhancedInvoiceScheduler creates enhanced invoice scheduler
func NewEnhancedInvoiceScheduler(config SchedulerConfig, logger *logrus.Logger) (*EnhancedInvoiceScheduler, error) {
	if config.Currency == "" {
		config.Currency = "USD"
	}
	
	if config.DefaultBillingCycle == "" {
		config.DefaultBillingCycle = "monthly"
	}
	
	inv := &EnhancedInvoiceScheduler{
		config: config,
		logger: logger,
		costOptimizer: NewCostOptimizer(logger),
		usageTracker: NewUsageTracker(logger),
		invoices: make([]*InvoiceRecord, 0),
		invoiceHistory: make([]InvoiceHistoryRecord, 0),
		metrics: NewInvoiceMetrics(),
		scheduleProgress: NewScheduleProgress(),
	}
	
	// Start background scheduler
	go inv.runSchedulingLoop(context.Background())
	
	logger.Info("Enhanced invoice scheduler initialized")
	return inv, nil
}

// runSchedulingLoop runs continuous invoice scheduling loop
func (inv *EnhancedInvoiceScheduler) runSchedulingLoop(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			inv.executeScheduledInvoices(ctx)
		}
	}
}

// executeScheduledInvoices executes scheduled invoice generation
func (inv *EnhancedInvoiceScheduler) executeScheduledInvoices(ctx context.Context) {
	now := time.Now()
	
	inv.mu.Lock()
	defer inv.mu.Unlock()
	
	inv.logger.WithField("now", now).Debug("Executing scheduled invoices")
	
	// Find tenants due for invoicing this period
	dueTenants := inv.findTenantsDueForInvoicing(now)
	
	if len(dueTenants) == 0 {
		inv.logger.Debug("No tenants due for invoicing this period")
		return
	}
	
	// Generate invoices for each tenant
	for _, tenantID := range dueTenants {
		inv.generateInvoiceForTenant(ctx, tenantID)
	}
	
	inv.scheduleProgress.RecordExecution(len(dueTenants))
	inv.metrics.RecordScheduledExecution(len(dueTenants))
	
	inv.lastRunTime = now
	
	// Schedule next run based on billing cycle
	inv.nextRunTime = inv.calculateNextRunTime()
	
	inv.logger.WithFields(logrus.Fields{
		"tenants_invoiced": len(dueTenants),
		"next_run": inv.nextRunTime,
	}).Info("Scheduled invoices executed")
}

// findTenantsDueForInvoicing finds tenants due for invoicing
func (inv *EnhancedInvoiceScheduler) findTenantsDueForInvoicing(now time.Time) []string {
	// Would query active tenants whose billing period ends today or is due
	// Simplified implementation
	return []string{"tenant_1", "tenant_2", "tenant_3"}
}

// generateInvoiceForTenant generates invoice for specific tenant
func (inv *EnhancedInvoiceScheduler) generateInvoiceForTenant(ctx context.Context, tenantID string) {
	inv.logger.WithField("tenant", tenantID).Info("Generating invoice for tenant")
	
	// Get usage data for current period
	period := inv.getCurrentBillingPeriod()
	usageData := inv.usageTracker.GetUsageByPeriod(tenantID, period)
	
	// Calculate costs with optimization
	lineItems := inv.calculateLineItems(tenantID, usageData)
	
	// Apply discounts
	lineItems = inv.applyDiscounts(lineItems, tenantID)
	
	// Calculate totals
	totals := inv.calculateTotals(lineItems)
	
	// Create invoice record
	invoice := &InvoiceRecord{
		ID: fmt.Sprintf("inv_%s_%s", tenantID, now.Format("200601")),
		TenantID: tenantID,
		BillingPeriod: period,
		Status: InvoiceDraft,
		TotalAmount: totals.Total,
		LineItems: lineItems,
		Metadata: map[string]interface{}{
			"generated_by": "enhanced_invoice_scheduler",
			"version": "1.0",
			"optimized": inv.config.CostOptimization,
		},
		GeneratedAt: now,
	}
	
	// Store invoice
	inv.invoices = append(inv.invoices, invoice)
	
	// Record in history
	inv.invoiceHistory = append(inv.invoiceHistory, InvoiceHistoryRecord{
		InvoiceID: invoice.ID,
		TenantID: tenantID,
		Status: invoice.Status,
		Amount: invoice.TotalAmount,
		GeneratedAt: now,
	})
	
	inv.logger.WithFields(logrus.Fields{
		"invoice_id": invoice.ID,
		"amount": invoice.TotalAmount,
		"line_items": len(invoice.LineItems),
	}).Info("Generated invoice for tenant")
	
	// Send if configured
	if inv.config.AutoSend && invoice.Status != InvoiceVoided {
		inv.sendInvoice(invoice)
	}
}

// calculateLineItems calculates invoice line items with cost optimization
func (inv *EnhancedInvoiceScheduler) calculateLineItems(tenantID string, usageData map[string]float64) []LineItem {
	lineItems := make([]LineItem, 0)
	
	if !inv.config.CostOptimization {
		// Basic pricing without optimization
		for resource, usage := range usageData {
			price := inv.getUnitPrice(resource)
			subtotal := usage * price
			
			item := LineItem{
				Description: fmt.Sprintf("%s usage", resource),
				Quantity: int(usage),
				UnitPrice: price,
				Subtotal: subtotal,
				TaxRate: inv.config.TaxRate,
			}
			
			lineItems = append(lineItems, item)
		}
	} else {
		// Optimized pricing with cost optimization
		optimizations := inv.costOptimizer.OptimizeCosts(tenantID, usageData)
		
		for resource, usage := range usageData {
			recommendations := inv.filterRecommendationsForResource(resource, optimizations)
			
			// Use optimized pricing if available
			price := inv.getOptimizedPrice(resource, recommendations)
			subtotal := usage * price
			
			item := LineItem{
				Description: fmt.Sprintf("%s usage (optimized)", resource),
				Quantity: int(usage),
				UnitPrice: price,
				Subtotal: subtotal,
				TaxRate: inv.config.TaxRate,
				Discounts: recommendations,
			}
			
			lineItems = append(lineItems, item)
		}
	}
	
	return lineItems
}

// applyDiscounts applies applicable discounts
func (inv *EnhancedInvoiceScheduler) applyDiscounts(lineItems []LineItem, tenantID string) []LineItem {
	// Would check tenant-specific discounts and apply them
	// Simplified implementation
	
	return lineItems
}

// calculateTotals calculates invoice totals from line items
func (inv *EnhancedInvoiceScheduler) calculateTotals(lineItems []LineItem) InvoiceTotals {
	subtotal := 0.0
	taxTotal := 0.0
	
	for _, item := range lineItems {
		subtotal += item.Subtotal
		itemTax := item.Subtotal * item.TaxRate / 100.0
		taxTotal += itemTax
	}
	
	total := subtotal + taxTotal
	
	return InvoiceTotals{
		Subtotal: subtotal,
		TaxTotal: taxTotal,
		Total: total,
	}
}

// Helper functions
func (inv *EnhancedInvoiceScheduler) getCurrentBillingPeriod() BillingPeriod {
	now := time.Now()
	
	period := BillingPeriod{
		Type: inv.config.DefaultBillingCycle,
	}
	
	switch inv.config.DefaultBillingCycle {
	case "monthly":
		period.StartDate = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		period.EndDate = period.StartDate.AddDate(0, 1, 0)
		period.DurationDays = 30
	case "quarterly":
		period.StartDate = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		period.EndDate = period.StartDate.AddDate(0, 3, 0)
		period.DurationDays = 90
	case "annual":
		period.StartDate = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location())
		period.EndDate = period.StartDate.AddDate(1, 0, 0)
		period.DurationDays = 365
	}
	
	return period
}

func (inv *EnhancedInvoiceScheduler) getUnitPrice(resource string) float64 {
	// Would return actual unit price from pricing model
	return 1.0 // Default
}

func (inv *EnhancedInvoiceScheduler) getOptimizedPrice(resource string, recs []OptimizationRecommendation) float64 {
	// Return optimized price if recommendations available
	if len(recs) > 0 {
		bestRec := recs[0]
		if bestRec.EstimatedSavings > 0 {
			return inv.getUnitPrice(resource) * (1.0 - bestRec.EstimatedSavings/100.0)
		}
	}
	
	return inv.getUnitPrice(resource)
}

func (inv *EnhancedInvoiceScheduler) filterRecommendationsForResource(resource string, recs []OptimizationRecommendation) []OptimizationRecommendation {
	filtered := make([]OptimizationRecommendation, 0)
	
	for _, rec := range recs {
		// Would filter by resource type
		filtered = append(filtered, rec)
	}
	
	return filtered
}
