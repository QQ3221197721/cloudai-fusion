// Package cost - FinOps HTTP integration for external billing systems
package cost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// FINOPS BILLING INTEGRATION WITH EXTERNAL HTTP APIs
// ============================================================================

// BillingIntegrationClient manages communication with external billing APIs
type BillingIntegrationClient struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	logger     *logrus.Logger
	
	// Rate limiting
	rateLimiter *RateLimiter
	
	// Metrics
	metrics *BillingMetrics
}

// UsageRecord represents resource usage data
type UsageRecord struct {
	TenantID     string            `json:"tenant_id"`
	Period       string            `json:"period"`
	ResourceType string            `json:"resource_type"`
	Quantity     float64           `json:"quantity"`
	CostUSD      float64           `json:"cost_usd"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// InvoiceRequest defines invoice creation parameters
type InvoiceRequest struct {
	TenantID      string   `json:"tenant_id"`
	AmountTotal   float64  `json:"amount_total"`
	Currency      string   `json:"currency"`
	DueDate       string   `json:"due_date"`
	Description   string   `json:"description"`
	LineItems     []Item   `json:"line_items"`
	Metadata      Metadata `json:"metadata,omitempty"`
}

// Item represents an invoice line item
type Item struct {
	Description string  `json:"description"`
	Quantity    int     `json:"quantity"`
	UnitPrice   float64 `json:"unit_price"`
	Amount      float64 `json:"amount"`
}

// Metadata stores additional invoice information
type Metadata struct {
	TaxIncluded bool `json:"tax_included"`
	Genre       string `json:"genre,omitempty"`
	Version     string `json:"version,omitempty"`
}

// ============================================================================
// CORE BILLING FUNCTIONS
// ============================================================================

// NewBillingIntegrationClient creates HTTP client for billing API
func NewBillingIntegrationClient(baseURL, apiKey string, logger *logrus.Logger) (*BillingIntegrationClient, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("base URL required")
	}
	
	if apiKey == "" {
		return nil, fmt.Errorf("API key required")
	}
	
	client := &BillingIntegrationClient{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    baseURL,
		apiKey:     apiKey,
		logger:     logger,
		rateLimiter: NewRateLimiter(100), // 100 req/min limit
		metrics:    NewBillingMetrics(),
	}
	
	return client, nil
}

// SubmitUsageReport submits usage report to billing system
func (b *BillingIntegrationClient) SubmitUsageReport(ctx context.Context, records []UsageRecord) error {
	// Check rate limit
	if !b.rateLimiter.Allow() {
		return fmt.Errorf("rate limit exceeded")
	}
	
	// Build request payload
	payload := map[string]interface{}{
		"tenant_id": records[0].TenantID,
		"usage_records": records,
		"submitted_at": time.Now().Format(time.RFC3339),
	}
	
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}
	
	url := fmt.Sprintf("%s/api/v1/usage/report", b.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.apiKey)
	req.Header.Set("X-API-Version", "1.0")
	
	resp, err := b.httpClient.Do(req)
	if err != nil {
		b.metrics.RecordError("submit_usage_report", err)
		return fmt.Errorf("billing API call failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		b.metrics.RecordError("submit_usage_report", fmt.Errorf("status %d", resp.StatusCode))
		return fmt.Errorf("billing API returned status %d", resp.StatusCode)
	}
	
	b.metrics.RecordSuccess("submit_usage_report")
	return nil
}

// CreateInvoice creates invoice in billing system
func (b *BillingIntegrationClient) CreateInvoice(ctx context.Context, request InvoiceRequest) (string, error) {
	// Validate request
	if request.TenantID == "" {
		return "", fmt.Errorf("tenant ID required")
	}
	
	if request.AmountTotal <= 0 {
		return "", fmt.Errorf("amount must be positive")
	}
	
	// Convert to billing system format
	billingRequest := map[string]interface{}{
		"tenant_id": request.TenantID,
		"total_amount": request.AmountTotal,
		"currency": request.Currency,
		"due_date": request.DueDate,
		"description": request.Description,
		"line_items": request.LineItems,
		"metadata": request.Metadata,
	}
	
	jsonData, err := json.Marshal(billingRequest)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}
	
	url := fmt.Sprintf("%s/api/v1/invoices/create", b.baseURL)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.apiKey)
	
	resp, err := b.httpClient.Do(req)
	if err != nil {
		b.metrics.RecordError("create_invoice", err)
		return "", fmt.Errorf("billing API call failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b.metrics.RecordError("create_invoice", fmt.Errorf("status %d", resp.StatusCode))
		return "", fmt.Errorf("billing API returned status %d", resp.StatusCode)
	}
	
	// Parse response
	var response struct {
		ID         string `json:"id"`
		Status     string `json:"status"`
		AmountDue  float64 `json:"amount_due"`
	}
	
	json.NewDecoder(resp.Body).Decode(&response)
	b.metrics.RecordSuccess("create_invoice")
	
	return response.ID, nil
}

// GetUsageCost retrieves cost breakdown for specific period
func (b *BillingIntegrationClient) GetUsageCost(ctx context.Context, tenantID, period string) (map[string]float64, error) {
	url := fmt.Sprintf("%s/api/v1/cost/%s/%s", b.baseURL, tenantID, period)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Authorization", "Bearer "+b.apiKey)
	
	resp, err := b.httpClient.Do(req)
	if err != nil {
		b.metrics.RecordError("get_usage_cost", err)
		return nil, fmt.Errorf("billing API call failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("billing API returned status %d", resp.StatusCode)
	}
	
	var costs map[string]float64
	if err := json.NewDecoder(resp.Body).Decode(&costs); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	
	b.metrics.RecordSuccess("get_usage_cost")
	return costs, nil
}

// GenerateReport generates cost optimization report
func (b *BillingIntegrationClient) GenerateReport(ctx context.Context, tenantID, reportType string) ([]byte, error) {
	url := fmt.Sprintf("%s/api/v1/reports/%s?tenant=%s&type=%s", 
		b.baseURL, reportType, tenantID, reportType)
	
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Authorization", "Bearer "+b.apiKey)
	
	resp, err := b.httpClient.Do(req)
	if err != nil {
		b.metrics.RecordError("generate_report", err)
		return nil, fmt.Errorf("billing API call failed: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("billing API returned status %d", resp.StatusCode)
	}
	
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	
	b.metrics.RecordSuccess("generate_report")
	return data, nil
}

// ============================================================================
// HELPER TYPES
// ============================================================================

// RateLimiter implements simple token bucket rate limiter
type RateLimiter struct {
	maxTokens int
	tokens    int
	lastRefill time.Time
	mu sync.Mutex
}

func NewRateLimiter(maxTokens int) *RateLimiter {
	return &RateLimiter{
		maxTokens: maxTokens,
		tokens: maxTokens,
		lastRefill: time.Now(),
	}
}

func (rl *RateLimiter) Allow() bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	
	// Refill tokens
	now := time.Now()
	refilled := int(now.Sub(rl.lastRefill).Seconds()) * (rl.maxTokens / 60)
	rl.tokens = min(rl.maxTokens, rl.tokens+refilled)
	rl.lastRefill = now
	
	if rl.tokens > 0 {
		rl.tokens--
		return true
	}
	
	return false
}

// BillingMetrics tracks billing operations
type BillingMetrics struct {
	submissionsSent int
	invoicesCreated int
	errorsCount int
}

func NewBillingMetrics() *BillingMetrics {
	return &BillingMetrics{}
}

func (m *BillingMetrics) RecordSuccess(operation string) {
	switch operation {
	case "submit_usage_report":
		m.submissionsSent++
	case "create_invoice":
		m.invoicesCreated++
	}
}

func (m *BillingMetrics) RecordError(operation string, err error) {
	m.errorsCount++
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
