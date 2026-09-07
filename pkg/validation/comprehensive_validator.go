// Package validation implements comprehensive input validation framework with business rule enforcement
package validation

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/sirupsen/logrus"
)

const (
	// Maximum request size to prevent DoS
	maxRequestSize = 10 * 1024 * 1024 // 10MB
	
	// Rate limiting per endpoint
	defaultRateLimit = 100 // requests per minute
)

// ValidationResult captures validation errors with detailed context
type ValidationResult struct {
	Field        string      `json:"field"`
	Value        interface{} `json:"value"`
	Rule         string      `json:"rule"`
	Message      string      `json:"message"`
	Code         string      `json:"code"`
	Suggestion   string      `json:"suggestion,omitempty"`
	Severity     string      `json:"severity"` // critical, warning, info
}

// ComprehensiveValidator provides business-aware validation
type ComprehensiveValidator struct {
	validator       *validator.Validate
	rateLimiter     map[string]*RateLimiter
	logger          *logrus.Logger
	businessRules   []BusinessRule
}

func NewComprehensiveValidator(logger *logrus.Logger) *ComprehensiveValidator {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	
	cv := &ComprehensiveValidator{
		validator:     validator.New(),
		rateLimiter:   make(map[string]*RateLimiter),
		logger:        logger.WithFields(logrus.Fields{"component": "validation"}),
		businessRules: initializeBusinessRules(),
	}
	
	// Register custom validators
	cv.registerCustomValidators()
	
	return cv
}

// registerCustomValidators adds custom validation rules for CloudAI Fusion domain
func (cv *ComprehensiveValidator) registerCustomValidators() {
	// Validate time formats strictly (ISO 8601 vs natural language)
(cv.validator.RegisterValidation("iso8601datetime", func(fl validator.FieldLevel) bool {
		value := fl.Field().String()
		// Must be strict ISO 8601 format for API responses
		pattern := `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(.\d+)?(Z|\+00:00|-[\d]{2}:[\d]{2})$`
		matched, _ := regexp.MatchString(pattern, value)
		return matched
}))

	// Validate quantity cannot be zero or negative (prevent null→default hiding bugs)
(cv.validator.RegisterValidation("positivequantity", func(fl validator.FieldLevel) bool {
		value := fl.Field().Float64()
		return value > 0
}))

	// Validate URL doesn't point to internal resources (SSRF prevention)
(cv.validator.RegisterValidation("publicurl", func(fl validator.FieldLevel) bool {
		url := fl.Field().String()
		// Block private IP ranges
		privateIPPatterns := []string{
			`^127\.`,
			`^10\.`,
			`^172\.1[6-9]\.`,
			`^172\.2[0-9]\.`,
			`^172\.3[01]\.`,
			`^192\.168\.`,
			`^localhost`,
		}
		
		for _, pattern := range privateIPPatterns {
			matched, _ := regexp.MatchString(pattern, url)
			if matched {
				return false
			}
		}
		return true
}))

// ValidateBulkOrders validates multiple orders with cross-dependency checks
func (cv *ComprehensiveValidator) ValidateBulkOrders(ctx context.Context, orders []*Order) ([]ValidationResult, error) {
	var results []ValidationResult
	
	for i, order := range orders {
		// Basic field validation
		orderResults := cv.validateSingleOrder(order)
		results = append(results, orderResults...)
		
		// Check for BOM circular dependencies
		if err := cv.detectCircularBomDependencies(ctx, orders[i:]); err != nil {
			results = append(results, ValidationResult{
				Field:    "bom_circular_dependency",
				Value:    order.ID,
				Rule:     "no_circular_bom",
				Message:  fmt.Sprintf("Circular BOM dependency detected in order %s", order.ID),
				Code:     "CIRCULAR_BOM_DETECTED",
				Severity: "critical",
				Suggestion: "Break the circular dependency by restructuring BOM hierarchy",
			})
		}
	}
	
	return results, nil
}

// validateSingleOrder performs comprehensive single-order validation
func (cv *ComprehensiveValidator) validateSingleOrder(order *Order) []ValidationResult {
	var results []ValidationResult
	
	// Required fields validation
	if order.Quantity <= 0 {
		results = append(results, ValidationResult{
			Field:     "quantity",
			Value:     order.Quantity,
			Rule:      "required_positive",
			Message:   "Quantity must be a positive integer",
			Code:      "INVALID_QUANTITY",
			Severity:  "critical",
			Suggestion: "Quantity should not be null or zero - check MES integration",
		})
	}
	
	// Time format validation
	if !validateISO8601Time(order.PlannedStart) {
		results = append(results, ValidationResult{
			Field:     "planned_start",
			Value:     order.PlannedStart,
			Rule:      "iso8601_format",
			Message:   "Planned start time must be in ISO 8601 format",
			Code:      "INVALID_TIME_FORMAT",
			Severity:  "warning",
			Suggestion: "Use format: yyyy-MM-dd'T'HH:mm:ss",
		})
	}
	
	// Business rule validation
	for _, rule := range cv.businessRules {
		if result := rule.Validate(order); result != nil {
			results = append(results, *result)
		}
	}
	
	return results
}

// detectCircularBomDependencies detects circular BOM dependencies with depth limit
func (cv *ComprehensiveValidator) detectCircularBomDependencies(ctx context.Context, orders []*Order) error {
	maxDepth := 100 // Prevent stack overflow
	
	for _, order := range orders {
		visited := make(map[string]bool)
		
		var dfs func(string, int) error
		dfs = func(orderID string, depth int) error {
			if depth > maxDepth {
				return fmt.Errorf("max recursion depth exceeded for order %s", orderID)
			}
			
			if visited[orderID] {
				return fmt.Errorf("circular dependency detected: %s", orderID)
			}
			
			visited[orderID] = true
			
			// Get dependent parts from BOM
			deps := getDependentParts(orderID)
			
			for _, dep := range deps {
				if err := dfs(dep, depth+1); err != nil {
					return err
				}
			}
			
			return nil
		}
		
		if err := dfs(order.ID, 0); err != nil {
			return err
		}
	}
	
	return nil
}

// RootCauseAnalysis analyzes why quantity fallback is frequently triggered
type RootCauseAnalysis struct {
	db           *sql.DB
	logger       *logrus.Logger
}

func NewRootCauseAnalysis(db *sql.DB, logger *logrus.Logger) *RootCauseAnalysis {
	return &RootCauseAnalysis{
		db:     db,
		logger: logger.WithFields(logrus.Fields{"component": "root_cause_analysis"}),
	}
}

// AnalyzeNullQuantities finds upstream sources causing null quantities
func (rca *RootCauseAnalysis) AnalyzeNullQuantities(ctx context.Context) (*QuantityEngineReport, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute*5)
	defer cancel()
	
	rca.logger.Info("Analyzing null quantity root causes")
	
	query := `
		SELECT 
			o.id as order_id,
			o.remote_order_id as source_id,
			o.created_at,
			c.company_name,
			mes_source.system_code
		FROM orders o
		LEFT JOIN customers c ON o.customer_id = c.id
		LEFT JOIN mes_sources mes_source ON o.mes_source_id = mes_source.id
		WHERE o.quantity IS NULL OR o.quantity <= 0
		ORDER BY o.created_at DESC
		LIMIT 100`
	
	rows, err := rca.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query null quantities: %w", err)
	}
	defer rows.Close()
	
	report := &QuantityEngineryReport{
		AnalyzedAt: time.Now().UTC(),
		Occurrences: make([]QuantityIssueRecord, 0),
		RootCauses: make(map[string]int),
	}
	
	for rows.Next() {
		var record QuantityIssueRecord
		if err := rows.Scan(&record.OrderID, &record.SourceID, &record.CreatedAt, &record.CompanyName, &record.MESSystem); err != nil {
			continue
		}
		
		report.Occurrences = append(report.Occurrences, record)
		
		// Categorize root cause
		if record.MESSystem == "" {
			report.RootCauses["missing_mes_connection"]++
		} else if record.CompanyName == "" {
			report.RootCauses["customer_data_missing"]++
		} else {
			report.RootCauses["mes_sync_failure"]++
		}
	}
	
	// Log findings
	for cause, count := range report.RootCauses {
		rca.logger.WithFields(logrus.Fields{
			"cause": cause,
			"count": count,
		}).Warn("Null quantity root cause identified")
	}
	
	return report, nil
}
