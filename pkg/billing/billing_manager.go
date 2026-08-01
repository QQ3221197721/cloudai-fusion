// Package billing - Billing manager for pricing and charge calculations
package billing

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// BILLING MANAGER WITH REAL PRICING LOGIC (ACTUAL IMPLEMENTATION)
// ============================================================================

// BillingManager manages pricing, discounts, and charge calculations
type BillingManager struct {
	mu sync.RWMutex
	logger *logrus.Logger
	
	// Pricing models
	pricingModels map[string]*PricingModel
	
	// Active customers
	customers map[string]*CustomerInfo
	
	// Active invoices
	invoices map[string]*ActiveInvoice
	
	// Discount codes
	discountCodes map[string]*DiscountCode
	
	// Metrics
	metrics *BillingManagerMetrics
}

// PricingModel defines pricing for resources
type PricingModel struct {
	Name string `json:"name"`
	Version int `json:"version"`
	Currency string `json:"currency"`
	Prices map[string]ResourcePrice `json:"prices"`
	TieredPrices []*TieredPrice `json:"tiered_prices,omitempty"`
	Discounts map[string]*Discount `json:"discounts"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ResourcePrice describes price for single resource
type ResourcePrice struct {
	ResourceType string `json:"resource_type"`
	BasePrice float64 `json:"base_price"`
	Unit string `json:"unit"` // per_gb, per_hour, per_request, etc.
	SetupFee float64 `json:"setup_fee"`
	TaxRate float64 `json:"tax_rate"` // percentage
	MinCommitment int64 `json:"min_commitment"`
}

// TieredPrice describes tiered pricing (volume discount)
type TieredPrice struct {
	ResourceType string `json:"resource_type"`
	 tiers []PriceTier `json:"tiers"`
}

// PriceTier describes a single pricing tier
type PriceTier struct {
	MinQuantity int64 `json:"min_quantity"`
	MaxQuantity int64 `json:"max_quantity"`
	PricePerUnit float64 `json:"price_per_unit"`
	DiscountPercent float64 `json:"discount_percent"`
}

// CustomerInfo stores customer billing information
type CustomerInfo struct {
	ID string `json:"id"`
	TenantID string `json:"tenant_id"`
	Email string `json:"email"`
	PaymentMethod string `json:"payment_method"`
	BillingAddress Address `json:"billing_address"`
	CurrentPlan string `json:"current_plan"`
	TrialEnds time.Time `json:"trial_ends"`
	IsTrial bool `json:"is_trial"`
	Metadata map[string]interface{} `json:"metadata"`
}

// Address represents billing address
type Address struct {
	Line1 string `json:"line1"`
	Line2 string `json:"line2"`
	City string `json:"city"`
	State string `json:"state"`
	PostalCode string `json:"postal_code"`
	Country string `json:"country"`
}

// ActiveInvoice stores invoice details before completion
type ActiveInvoice struct {
	ID string `json:"id"`
	TenantID string `json:"tenant_id"`
	Status InvoiceStatus `json:"status"`
	AmountDue float64 `json:"amount_due"`
	DueDate time.Time `json:"due_date"`
	CreatedAt time.Time `json:"created_at"`
	Items []InvoiceItem `json:"items"`
	Subtotal float64 `json:"subtotal"`
	TaxTotal float64 `json:"tax_total"`
	DiscountTotal float64 `json:"discount_total"`
}

// InvoiceItem represents line item in invoice
type InvoiceItem struct {
	Description string `json:"description"`
	Quantity int64 `json:"quantity"`
	UnitPrice float64 `json:"unit_price"`
	Amount float64 `json:"amount"`
}

// DiscountCode stores discount code information
type DiscountCode struct {
	Code string `json:"code"`
	Type string `json:"type"` // percentage, fixed
	Value float64 `json:"value"`
	UsageLimit int `json:"usage_limit"`
	UsedCount int `json:"used_count"`
	StartDate time.Time `json:"start_date"`
	EndDate time.Time `json:"end_date"`
	MinimumPurchase float64 `json:"minimum_purchase"`
	ValidForPlans []string `json:"valid_for_plans"`
}

// ============================================================================
// CORE PRICING FUNCTIONS (REAL IMPLEMENTATION)
// ============================================================================

// NewBillingManager creates billing manager with configured pricing
func NewBillingManager(logger *logrus.Logger) (*BillingManager, error) {
	manager := &BillingManager{
		logger: logger,
		pricingModels: make(map[string]*PricingModel),
		customers: make(map[string]*CustomerInfo),
		invoices: make(map[string]*ActiveInvoice),
		discountCodes: make(map[string]*DiscountCode),
		metrics: NewBillingManagerMetrics(),
	}
	
	// Initialize default pricing model
	defaultModel := manager.createDefaultPricing()
	manager.pricingModels["default"] = defaultModel
	
	return manager, nil
}

// createDefaultPricing creates default cloud resource pricing
func (bm *BillingManager) createDefaultPricing() *PricingModel {
	return &PricingModel{
		Name: "Standard Cloud",
		Version: 1,
		Currency: "USD",
		Prices: map[string]ResourcePrice{
			"compute": {
				ResourceType: "compute",
				BasePrice: 0.05,
				Unit: "vCPU-hour",
				SetupFee: 0.0,
				TaxRate: 8.0,
			},
			"storage": {
				ResourceType: "storage",
				BasePrice: 0.1,
				Unit: "GB-month",
				SetupFee: 0.0,
				TaxRate: 8.0,
			},
			"bandwidth": {
				ResourceType: "bandwidth",
				BasePrice: 0.09,
				Unit: "GB-transferred",
				SetupFee: 0.0,
				TaxRate: 8.0,
			},
			"gpu": {
				ResourceType: "gpu",
				BasePrice: 1.0,
				Unit: "GPU-hour",
				SetupFee: 0.0,
				TaxRate: 8.0,
			},
		},
		TieredPrices: []*TieredPrice{
			{
				ResourceType: "storage",
				tiers: []PriceTier{
					{MinQuantity: 0, MaxQuantity: 100, PricePerUnit: 0.1, DiscountPercent: 0},
					{MinQuantity: 100, MaxQuantity: 500, PricePerUnit: 0.09, DiscountPercent: 10},
					{MinQuantity: 500, MaxQuantity: 1000, PricePerUnit: 0.08, DiscountPercent: 20},
					{MinQuantity: 1000, MaxQuantity: 0, PricePerUnit: 0.07, DiscountPercent: 30},
				},
			},
		},
		Discounts: make(map[string]*Discount),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

// CalculateCharge calculates total charge for tenant
func (bm *BillingManager) CalculateCharge(ctx context.Context, tenantID string, usage map[string]int64, billingPeriod Period) (float64, error) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	
	pricing := bm.pricingModels["default"]
	if pricing == nil {
		return 0, fmt.Errorf("pricing model not found")
	}
	
	total := 0.0
	
	for resourceType, quantity := range usage {
		price, exists := pricing.Prices[resourceType]
		if !exists {
			continue
		}
		
		// Check for tiered pricing
		var unitPrice float64
		foundTiered := false
		
		for _, tiered := range pricing.TieredPrices {
			if tiered.ResourceType == resourceType {
				unitPrice = bm.applyTieredPricing(tiered, quantity)
				foundTiered = true
				break
			}
		}
		
		if !foundTiered {
			unitPrice = price.BasePrice
		}
		
		charge := float64(quantity) * unitPrice
		total += charge
	}
	
	bm.metrics.RecordCalculation(len(usage))
	
	return total, nil
}

// applyTieredPricing applies tiered pricing based on usage quantity
func (bm *BillingManager) applyTieredPricing(tiered *TieredPrice, quantity int64) float64 {
	var totalCharge float64
	remainingQuantity := quantity
	
	for _, tier := range tiered.tiers {
		if remainingQuantity <= 0 {
			break
		}
		
		if tier.MaxQuantity > 0 && quantity > tier.MaxQuantity {
			// Use this tier for max allowed
			payableInTier := min(remainingQuantity, tier.MaxQuantity-tier.MinQuantity)
			totalCharge += payableInTier * tier.PricePerUnit
			remainingQuantity -= payableInTier
		} else if tier.MaxQuantity == 0 || quantity >= tier.MinQuantity {
			// Apply this tier
			payableInTier := remainingQuantity
			totalCharge += payableInTier * tier.PricePerUnit
			remainingQuantity = 0
		}
	}
	
	return totalCharge
}

// GetPrice returns price for specific resource
func (bm *BillingManager) GetPrice(resourceType string) float64 {
	pricing := bm.pricingModels["default"]
	if pricing == nil {
		return 0
	}
	
	if price, exists := pricing.Prices[resourceType]; exists {
		return price.BasePrice
	}
	
	return 0
}

// ValidateDiscountCode validates discount code
func (bm *BillingManager) ValidateDiscountCode(code string, purchaseAmount float64, plans []string) (*DiscountCode, bool, error) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	
	discount, exists := bm.discountCodes[code]
	if !exists {
		return nil, false, fmt.Errorf("discount code not found")
	}
	
	now := time.Now()
	if now.Before(discount.StartDate) || now.After(discount.EndDate) {
		return nil, false, fmt.Errorf("discount code expired or not yet active")
	}
	
	if discount.UsedCount >= discount.UsageLimit && discount.UsageLimit > 0 {
		return nil, false, fmt.Errorf("discount code usage limit exceeded")
	}
	
	if purchaseAmount < discount.MinimumPurchase {
		return nil, false, fmt.Errorf("minimum purchase not met")
	}
	
	if len(discount.ValidForPlans) > 0 {
		valid := false
		for _, plan := range plans {
			for _, validPlan := range discount.ValidForPlans {
				if plan == validPlan {
					valid = true
					break
				}
			}
			if valid {
				break
			}
		}
		if !valid {
			return nil, false, fmt.Errorf("discount code not valid for selected plans")
		}
	}
	
	return discount, true, nil
}

// CalculateWithDiscount calculates final amount after applying discount
func (bm *BillingManager) CalculateWithDiscount(subtotal float64, discount *DiscountCode) float64 {
	if discount == nil {
		return subtotal
	}
	
	var discountAmount float64
	if discount.Type == "percentage" {
		discountAmount = subtotal * discount.Value / 100.0
	} else {
		discountAmount = discount.Value
	}
	
	finalAmount := subtotal - discountAmount
	if finalAmount < 0 {
		finalAmount = 0
	}
	
	return finalAmount
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// ============================================================================
// METRICS TRACKING
// ============================================================================

// BillingManagerMetrics tracks billing metrics
type BillingManagerMetrics struct {
	mu sync.RWMutex
	CalculationsTotal int
	DiscountsApplied int
	InvoicesGenerated int
}

func NewBillingManagerMetrics() *BillingManagerMetrics {
	return &BillingManagerMetrics{}
}

func (m *BillingManagerMetrics) RecordCalculation(count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CalculationsTotal += count
}
