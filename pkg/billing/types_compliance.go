// Package billing - Compliance and tax calculation types
package billing

import "github.com/sirupsen/logrus"

// ============================================================================
// COMPLIANCE AND TAX TYPES FOR INVOICE GENERATION
// ============================================================================

// ComplianceRule defines a compliance requirement for invoicing
type ComplianceRule struct {
	Jurisdiction string `json:"jurisdiction"`
	RuleType     string `json:"rule_type"` // sales_tax, vat, gst, nexus
	Threshold    float64 `json:"threshold,omitempty"`
	IsActive     bool   `json:"is_active"`
	Description  string `json:"description"`
}

// ComplianceRequirement defines specific compliance obligations
type ComplianceRequirement struct {
	RequirementID         string   `json:"requirement_id"`
	Jurisdiction          string   `json:"jurisdiction"`
	DocumentationRequired []string `json:"documentation_required,omitempty"`
	FilingFrequency       string   `json:"filing_frequency"` // monthly, quarterly, annually
	DeadlineDay           int      `json:"deadline_day"`     // day of month/quarter
}

// NexusValidation determines if nexus exists in jurisdiction
type NexusValidation struct {
	Jurisdiction               string  `json:"jurisdiction"`
	SalesThresholdExceeded     bool    `json:"sales_threshold_exceeded"`
	TransactionCountExceeded   bool    `json:"transaction_count_exceeded"`
	NexusEstablished           bool    `json:"nexus_established"`
	ThresholdAmount            float64 `json:"threshold_amount"`
	ActualSales                float64 `json:"actual_sales"`
}

// InvoiceTotals represents the final invoice totals after all calculations
type InvoiceTotals struct {
	Subtotal        float64            `json:"subtotal"`
	TaxAmount       float64            `json:"tax_amount"`
	DiscountAmount  float64            `json:"discount_amount"`
	Total           float64            `json:"total"`
	Currency        string             `json:"currency"`
	Breakdown       []TaxBreakdownItem `json:"breakdown"`
}

// TaxBreakdownItem details tax by jurisdiction
type TaxBreakdownItem struct {
	Jurisdiction    string  `json:"jurisdiction"`
	Rate            float64 `json:"rate"`
	TaxableAmount   float64 `json:"taxable_amount"`
	TaxAmount       float64 `json:"tax_amount"`
}

// CompleteInvoiceData extends with all fields needed for compliance checking
type CompleteInvoiceData struct {
	CustomerID      string            `json:"customer_id"`
	CustomerEmail   string            `json:"customer_email"`
	CustomerName    string            `json:"customer_name"`
	CustomerAddress InvoiceAddress    `json:"customer_address"`
	LineItems       []map[string]interface{} `json:"line_items"`
	Discounts       map[string]*Discount `json:"discounts,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

// Additional helper types for invoice generation

// ComplianceValidation validates invoice compliance across jurisdictions
type ComplianceValidation struct {
	PassesAllRules bool              `json:"passes_all_rules"`
	RulesChecked   []ComplianceRule  `json:"rules_checked"`
	FailedRules    []ComplianceRule  `json:"failed_rules,omitempty"`
	Jurisdictions  []NexusValidation `json:"jurisdictions"`
	Valid          bool              `json:"valid"`
	Reason         string            `json:"reason,omitempty"`
}
