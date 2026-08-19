package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RateLimiterDocs provides API documentation for rate limiting
type RateLimiterDocs struct {
	mu            sync.RWMutex
	rateLimits    map[string]*RateLimitConfig
	globalLimit   *RateLimitConfig
	customers     map[string]*CustomerQuota
	lastUpdated   time.Time
}

// RateLimitConfig defines rate limit configuration
type RateLimitConfig struct {
	Endpoint      string              `json:"endpoint"`
	Method        string              `json:"method"`
	Requests      int                 `json:"requests"`          // Max requests in window
	Window        time.Duration       `json:"window"`           // Time window
	BurstSize     int                 `json:"burst_size"`       // Allow burst above limit
	Strategy      string              `json:"strategy"`         // token_bucket, fixed_window, sliding_window
	Tier          string              `json:"tier,omitempty"`   // free, basic, premium, enterprise
	CustomRules   []CustomRateRule    `json:"custom_rules,omitempty"`
	Description   string              `json:"description,omitempty"`
	Examples      []RateExample       `json:"examples,omitempty"`
}

// CustomRateRule defines custom rate limiting rule
type CustomRateRule struct {
	Field      string `json:"field"`              // Field to check (e.g., "x-api-key")
	ValueRegex string `json:"value_regex"`        // Regex pattern for field value
	RateLimit  int    `json:"rate_limit"`         // Rate limit for matching requests
}

// RateExample shows typical usage scenario
type RateExample struct {
	Description string `json:"description"`
	RestPeriod  string `json:"rest_period"`
	Hint        string `json:"hint"`
	Warning     string `json:"warning,omitempty"`
}

// CustomerQuota tracks customer-specific quota usage
type CustomerQuota struct {
	ID                string                    `json:"id"`
	TenantID          string                    `json:"tenant_id"`
	Tier              string                    `json:"tier"`
	CurrentUsage      map[string]UsageSnapshot  `json:"current_usage"`
	Limits            map[string]*RateLimitConfig `json:"limits"`
	ResetAt           time.Time                 `json:"reset_at"`
	OVERRIDE          bool                      `json:"override,omitempty"`
	Metadata          map[string]interface{}    `json:"metadata,omitempty"`
}

// UsageSnapshot captures current usage state
type UsageSnapshot struct {
	Count         int       `json:"count"`
	LastRequestAt time.Time `json:"last_request_at"`
	WindowStart   time.Time `json:"window_start"`
	WindowEnd     time.Time `json:"window_end"`
}

// NewRateLimiterDocs creates rate limiter docs system
func NewRateLimiterDocs() *RateLimiterDocs {
	return &RateLimiterDocs{
		rateLimits: make(map[string]*RateLimitConfig),
		customers:  make(map[string]*CustomerQuota),
		lastUpdated: time.Now(),
	}
}

// AddRateLimit registers new rate limit configuration
func (rl *RateLimiterDocs) AddRateLimit(config *RateLimitConfig) error {
	if config.Endpoint == "" || config.Method == "" {
		return fmt.Errorf("endpoint and method are required")
	}
	
	key := config.Method + ":" + config.Endpoint
	
	rl.mu.Lock()
	defer rl.mu.Unlock()
	
	rl.rateLimits[key] = config
	rl.lastUpdated = time.Now()
	
	return nil
}

// GetRateLimit returns rate limit config for endpoint
func (rl *RateLimiterDocs) GetRateLimit(method, endpoint string) (*RateLimitConfig, bool) {
	key := method + ":" + endpoint
	
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	
	config, exists := rl.rateLimits[key]
	return config, exists
}

// SetGlobalLimit sets global API-wide rate limits
func (rl *RateLimiterDocs) SetGlobalLimit(limit *RateLimitConfig) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.globalLimit = limit
	rl.lastUpdated = time.Now()
}

// RegisterCustomer adds customer with custom quotas
func (rl *RateLimiterDocs) RegisterCustomer(customer *CustomerQuota) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	
	if customer.ID == "" {
		// Auto-generate customer ID
		customer.ID = uuid.New().String()
	}
	
	rl.customers[customer.ID] = customer
	rl.lastUpdated = time.Now()
}

// GetCustomerQuota retrieves customer's quota status
func (rl *RateLimiterDocs) GetCustomerQuota(customerID string) (*CustomerQuota, bool) {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	
	customer, exists := rl.customers[customerID]
	return customer, exists
}

// UpdateUsage increments usage counter for request
func (rl *RateLimiterDocs) UpdateUsage(customerID, method, endpoint string) (*UsageCheck, error) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	
	config, exists := rl.GetRateLimit(method, endpoint)
	if !exists {
		// Use default limits if not configured
		config = rl.getDefaultRateLimit()
	}
	
	quota, exists := rl.customers[customerID]
	if !exists {
		// Create temporary quota for anonymous users
		quota = &CustomerQuota{Tier: "free"}
		rl.customers[customerID] = quota
	}
	
	snapshot, exceeds := rl.checkAndRecordUsage(quota.CurrentUsage, config)
	quota.CurrentUsage[method+":"+endpoint] = snapshot
	
	return &UsageCheck{
		Limit:      config.Requests,
		Remaining:  config.Requests - snapshot.Count,
		ResetAt:    snapshot.WindowEnd,
		Exceeded:   exceeds,
		RetryAfter: rl.calculateRetryDuration(snapshot.Count, config),
	}, nil
}

// UsageCheck represents result of usage check
type UsageCheck struct {
	Limit      int
	Remaining  int
	ResetAt    time.Time
	Exceeded   bool
	RetryAfter time.Duration
}

// checkAndRecordUsage checks if request would exceed limit and records it
func (rl *RateLimiterDocs) checkAndRecordUsage(currentUsages map[string]UsageSnapshot, config *RateLimitConfig) (UsageSnapshot, bool) {
	key := config.Method + ":" + config.Endpoint
	
	now := time.Now()
	windowStart := now.Add(-config.Window)
	
	var prevUsage UsageSnapshot
	if usage, exists := currentUsages[key]; exists && usage.WindowEnd.After(windowStart) {
		prevUsage = usage
	} else {
		prevUsage = UsageSnapshot{
			WindowStart: windowStart,
			WindowEnd:   now.Add(config.Window),
		}
	}
	
	newCount := prevUsage.Count + 1
	exceeds := newCount > config.Requests+config.BurstSize
	
	return UsageSnapshot{
		Count:         newCount,
		LastRequestAt: now,
		WindowStart:   windowStart,
		WindowEnd:     now.Add(config.Window),
	}, exceeds
}

// getDefaultRateLimit returns default rate limit when none configured
func (rl *RateLimiterDocs) getDefaultRateLimit() *RateLimitConfig {
	return &RateLimitConfig{
		Requests: 1000,
		Window:   24 * time.Hour,
		BurstSize: 100,
	}
}

// calculateRetryDuration calculates how long to wait before retry
func (rl *RateLimiterDocs) calculateRetryDuration(count int, config *RateLimitConfig) time.Duration {
	if count <= config.Requests {
		return 0
	}
	
	// Calculate when window resets
	windowEnd := time.Now().Add(config.Window)
	return windowEnd.Sub(time.Now())
}

// GenerateDocumentation generates comprehensive rate limit docs
func (rl *RateLimiterDocs) GenerateDocumentation() []byte {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	
	type DocSection struct {
		Title       string            `json:"title"`
		Content     string            `json:"content"`
		Endpoints   []*RateLimitEntry `json:"endpoints"`
		Strategies  []StrategyDoc     `json:"strategies"`
		Headers     map[string]string `json:"headers"`
	}
	
	doc := DocSection{
		Title:     "Rate Limiting Documentation",
		Content:   rl.generateOverview(),
		Endpoints: rl.listEndpoints(),
		Strategies: []StrategyDoc{
			{Name: "token_bucket", Description: "Smooth traffic flow", UseCase: "high_frequency"},
			{Name: "fixed_window", Description: "Simple sliding counter", UseCase: "standard"},
			{Name: "sliding_window", Description: "Precise rolling window", UseCase: "metered"},
		},
		Headers: map[string]string{
			"X-RateLimit-Limit":     "Max requests allowed",
			"X-RateLimit-Remaining": "Requests remaining",
			"X-RateLimit-Reset":     "Unix timestamp for reset",
			"Retry-After":           "Seconds to wait (if exceeded)",
		},
	}
	
	data, _ := json.MarshalIndent(doc, "", "  ")
	return data
}

func (rl *RateLimiterDocs) generateOverview() string {
	totalEndpoints := len(rl.rateLimits)
	globalSet := rl.globalLimit != nil
	
	return "CloudAI Fusion Platform uses token-bucket rate limiting across all APIs. " +
		"You have " + formatNumber(totalEndpoints) + " endpoints configured with rate limits. " +
		"The global limit is " + formatBool(globalSet) + "."
}

func (rl *RateLimiterDocs) listEndpoints() []*RateLimitEntry {
	endpoints := make([]*RateLimitEntry, 0, len(rl.rateLimits))
	
	for key, config := range rl.rateLimits {
		parts := strings.Split(key, ":")
		if len(parts) == 2 {
			endpoint := &RateLimitEntry{
				Method:     parts[0],
				Endpoint:   parts[1],
				Limit:      config.Requests,
				Window:     formatDuration(config.Window),
				Tier:       config.Tier,
				Description: config.Description,
			}
			endpoints = append(endpoints, endpoint)
		}
	}
	
	return endpoints
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return formatNumber(int(d.Seconds())) + "s"
	} else if d < time.Hour {
		return formatNumber(int(d.Minutes())) + "m"
	}
	return formatNumber(int(d.Hours())) + "h"
}

func formatBool(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}

func formatNumber(n int) string {
	return strconv.Itoa(n)
}

// RateLimitEntry represents single endpoint config
type RateLimitEntry struct {
	Method     string `json:"method"`
	Endpoint   string `json:"endpoint"`
	Limit      int    `json:"limit"`
	Window     string `json:"window"`
	Tier       string `json:"tier"`
	Description string `json:"description"`
}

// StrategyDoc documents rate limiting strategy
type StrategyDoc struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	UseCase     string `json:"use_case"`
}

// API Handlers
type RateLimiterDocsAPI struct {
	limiters *RateLimiterDocs
}

func NewRateLimiterDocsAPI(limiters *RateLimiterDocs) *RateLimiterDocsAPI {
	return &RateLimiterDocsAPI{limiters: limiters}
}

// HandleGenerate returns full documentation as JSON
func (api *RateLimiterDocsAPI) HandleGenerate(c *gin.Context) {
	doc := api.limiters.GenerateDocumentation()
	c.Data(http.StatusOK, "application/json", doc)
}

// HandleLimits returns all configured rate limits
func (api *RateLimiterDocsAPI) HandleLimits(c *gin.Context) {
	api.limiters.mu.RLock()
	defer api.limiters.mu.RUnlock()
	
	result := make(map[string]*RateLimitConfig)
	for k, v := range api.limiters.rateLimits {
		result[k] = v
	}
	
	c.JSON(http.StatusOK, gin.H{
		"rate_limits":     result,
		"global_limit":    api.limiters.globalLimit,
		"last_updated":    api.limiters.lastUpdated.Format(time.RFC3339),
	})
}

// HandleUpdate allows runtime updates via admin API
func (api *RateLimiterDocsAPI) HandleUpdate(c *gin.Context) {
	var config RateLimitConfig
	
	if err := c.ShouldBindJSON(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	
	if err := api.limiters.AddRateLimit(&config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{
		"message":   "Rate limit updated",
		"endpoint":  config.Method + ":" + config.Endpoint,
	})
}
