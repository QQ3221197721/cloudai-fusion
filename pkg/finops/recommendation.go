// Package finops provides Reserved Instance recommendation capabilities.
// Analyzes usage patterns, calculates optimal RI coverage, and generates
// cost-saving recommendations for long-term commitments.
package finops

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Reserved Instance Recommendation Engine
// ============================================================================

// RIRecommendationEngine analyzes resource usage patterns and recommends
// optimal Reserved Instance purchases to maximize cost savings.
type RIRecommendationEngine struct {
	usageRecords     map[string][]UsageRecord    // instanceType → usage records
	riInventory      map[string][]*ReservedInstance
	recommendations  []*RIRecommendation
	config           RIConfig
	mu               sync.RWMutex
	logger           *logrus.Logger
}

// RIConfig configures the RI recommendation engine.
type RIConfig struct {
	AnalysisWindow     time.Duration `json:"analysis_window" yaml:"analysisWindow"`         // e.g., 30 days
	MinUtilization     float64       `json:"min_utilization" yaml:"minUtilization"`           // e.g., 0.7
	TargetCoverage     float64       `json:"target_coverage" yaml:"targetCoverage"`           // e.g., 0.8
	BreakEvenThreshold float64       `json:"break_even_threshold" yaml:"breakEvenThreshold"` // months
	MaxCommitmentYears int           `json:"max_commitment_years" yaml:"maxCommitmentYears"` // 1 or 3
	IncludeGPU         bool          `json:"include_gpu" yaml:"includeGPU"`
}

// DefaultRIConfig returns sensible defaults.
func DefaultRIConfig() RIConfig {
	return RIConfig{
		AnalysisWindow:     30 * 24 * time.Hour,
		MinUtilization:     0.7,
		TargetCoverage:     0.8,
		BreakEvenThreshold: 8,
		MaxCommitmentYears: 3,
		IncludeGPU:         true,
	}
}

// UsageRecord represents resource usage data for a time period.
type UsageRecord struct {
	Timestamp      time.Time `json:"timestamp"`
	InstanceType   string    `json:"instance_type"`
	InstanceCount  int       `json:"instance_count"`
	CPUUtilization float64   `json:"cpu_utilization"`
	MemUtilization float64   `json:"mem_utilization"`
	GPUUtilization float64   `json:"gpu_utilization"`
	OnDemandCost   float64   `json:"on_demand_cost"`
	Region         string    `json:"region"`
	IsGPU          bool      `json:"is_gpu"`
}

// ReservedInstance represents an existing RI commitment.
type ReservedInstance struct {
	ID             string    `json:"id"`
	InstanceType   string    `json:"instance_type"`
	Count          int       `json:"count"`
	Term           string    `json:"term"` // "1yr", "3yr"
	PaymentOption  string    `json:"payment_option"` // "all_upfront", "partial_upfront", "no_upfront"
	HourlyRate     float64   `json:"hourly_rate"`
	TotalCost      float64   `json:"total_cost"`
	Utilization    float64   `json:"utilization"`
	ExpiresAt      time.Time `json:"expires_at"`
	Region         string    `json:"region"`
}

// RIRecommendation represents a purchase recommendation.
type RIRecommendation struct {
	InstanceType     string  `json:"instance_type"`
	RecommendedCount int     `json:"recommended_count"`
	Term             string  `json:"term"`
	PaymentOption    string  `json:"payment_option"`
	EstimatedSavings float64 `json:"estimated_monthly_savings"`
	SavingsPercent   float64 `json:"savings_percent"`
	BreakEvenMonths  float64 `json:"break_even_months"`
	UpfrontCost      float64 `json:"upfront_cost"`
	MonthlyRate      float64 `json:"monthly_rate"`
	CurrentCoverage  float64 `json:"current_coverage"`
	TargetCoverage   float64 `json:"target_coverage"`
	Confidence       float64 `json:"confidence"` // 0-1
	Reason           string  `json:"reason"`
	Region           string  `json:"region"`
	IsGPU            bool    `json:"is_gpu"`
}

// RICoverageReport shows current RI coverage analysis.
type RICoverageReport struct {
	TotalInstances   int                        `json:"total_instances"`
	CoveredByRI      int                        `json:"covered_by_ri"`
	CoveragePercent  float64                    `json:"coverage_percent"`
	MonthlySavings   float64                    `json:"monthly_savings"`
	WastedRI         int                        `json:"wasted_ri"` // unused RI capacity
	ByType           map[string]*TypeCoverage   `json:"by_type"`
	GeneratedAt      time.Time                  `json:"generated_at"`
}

// TypeCoverage shows coverage for a specific instance type.
type TypeCoverage struct {
	InstanceType    string  `json:"instance_type"`
	OnDemandCount   int     `json:"on_demand_count"`
	RICount         int     `json:"ri_count"`
	Coverage        float64 `json:"coverage_percent"`
	MonthlyOnDemand float64 `json:"monthly_on_demand_cost"`
	MonthlyRI       float64 `json:"monthly_ri_cost"`
	Savings         float64 `json:"monthly_savings"`
}

// NewRIRecommendationEngine creates a new RI recommendation engine.
func NewRIRecommendationEngine(cfg RIConfig, logger *logrus.Logger) *RIRecommendationEngine {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &RIRecommendationEngine{
		usageRecords: make(map[string][]UsageRecord),
		riInventory:  make(map[string][]*ReservedInstance),
		config:       cfg,
		logger:       logger,
	}
}

// IngestUsageData adds historical usage data for analysis.
func (e *RIRecommendationEngine) IngestUsageData(records []UsageRecord) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, r := range records {
		e.usageRecords[r.InstanceType] = append(e.usageRecords[r.InstanceType], r)
	}

	// Trim old data
	cutoff := time.Now().Add(-e.config.AnalysisWindow)
	for k, recs := range e.usageRecords {
		trimmed := make([]UsageRecord, 0, len(recs))
		for _, r := range recs {
			if r.Timestamp.After(cutoff) {
				trimmed = append(trimmed, r)
			}
		}
		e.usageRecords[k] = trimmed
	}
}

// RegisterExistingRI registers currently owned reserved instances.
func (e *RIRecommendationEngine) RegisterExistingRI(ri *ReservedInstance) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.riInventory[ri.InstanceType] = append(e.riInventory[ri.InstanceType], ri)
}

// Analyze generates RI recommendations for all tracked instance types.
func (e *RIRecommendationEngine) Analyze(ctx context.Context) ([]*RIRecommendation, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	e.recommendations = nil

	for instanceType, records := range e.usageRecords {
		if !e.config.IncludeGPU {
			if len(records) > 0 && records[0].IsGPU {
				continue
			}
		}

		rec := e.analyzeInstanceType(instanceType, records)
		if rec != nil {
			e.recommendations = append(e.recommendations, rec)
		}
	}

	// Sort by savings descending
	sort.Slice(e.recommendations, func(i, j int) bool {
		return e.recommendations[i].EstimatedSavings > e.recommendations[j].EstimatedSavings
	})

	e.logger.WithField("recommendations", len(e.recommendations)).Info("RI analysis completed")
	return e.recommendations, nil
}

func (e *RIRecommendationEngine) analyzeInstanceType(instanceType string, records []UsageRecord) *RIRecommendation {
	if len(records) < 24 { // Need at least 24 data points
		return nil
	}

	// Calculate baseline usage (P50 and P90)
	counts := make([]int, len(records))
	totalCost := 0.0
	for i, r := range records {
		counts[i] = r.InstanceCount
		totalCost += r.OnDemandCost
	}

	sort.Ints(counts)
	p50 := counts[len(counts)/2]
	p90 := counts[int(float64(len(counts))*0.9)]

	// Current RI coverage
	existingRI := 0
	for _, ri := range e.riInventory[instanceType] {
		if ri.ExpiresAt.After(time.Now()) {
			existingRI += ri.Count
		}
	}

	currentCoverage := 0.0
	if p90 > 0 {
		currentCoverage = float64(existingRI) / float64(p90)
	}

	// How many more RIs needed to reach target coverage
	targetCount := int(math.Ceil(float64(p50) * e.config.TargetCoverage))
	additionalRI := targetCount - existingRI
	if additionalRI <= 0 {
		return nil // Already sufficiently covered
	}

	// Average utilization check
	avgUtil := 0.0
	for _, r := range records {
		avgUtil += r.CPUUtilization
	}
	avgUtil /= float64(len(records))
	if avgUtil < e.config.MinUtilization {
		return nil // Usage too low to justify RI
	}

	// Cost analysis: compare 1yr vs 3yr
	monthlyOnDemand := totalCost / (float64(e.config.AnalysisWindow) / float64(30*24*time.Hour))
	perInstanceMonthly := monthlyOnDemand / float64(p50)

	// Typical RI discounts
	type termOption struct {
		term     string
		payment  string
		discount float64
		upfront  float64
	}
	options := []termOption{
		{"1yr", "no_upfront", 0.30, 0},
		{"1yr", "partial_upfront", 0.37, 0.5},
		{"1yr", "all_upfront", 0.40, 1.0},
		{"3yr", "no_upfront", 0.46, 0},
		{"3yr", "partial_upfront", 0.53, 0.5},
		{"3yr", "all_upfront", 0.58, 1.0},
	}

	var best *termOption
	bestSavings := 0.0
	for i := range options {
		opt := &options[i]
		if opt.term == "3yr" && e.config.MaxCommitmentYears < 3 {
			continue
		}
		monthlySaving := perInstanceMonthly * opt.discount * float64(additionalRI)
		termMonths := 12.0
		if opt.term == "3yr" {
			termMonths = 36.0
		}
		upfrontCost := perInstanceMonthly * (1 - opt.discount) * opt.upfront * termMonths * float64(additionalRI)
		breakEven := upfrontCost / monthlySaving
		if breakEven < e.config.BreakEvenThreshold || opt.upfront == 0 {
			if monthlySaving > bestSavings {
				bestSavings = monthlySaving
				best = opt
			}
		}
	}

	if best == nil {
		return nil
	}

	termMonths := 12.0
	if best.term == "3yr" {
		termMonths = 36.0
	}
	upfrontCost := perInstanceMonthly * (1 - best.discount) * best.upfront * termMonths * float64(additionalRI)
	breakEven := 0.0
	if bestSavings > 0 {
		breakEven = upfrontCost / bestSavings
	}

	// Confidence based on data consistency
	confidence := math.Min(float64(len(records))/720.0, 1.0) * avgUtil

	isGPU := false
	if len(records) > 0 {
		isGPU = records[0].IsGPU
	}
	region := ""
	if len(records) > 0 {
		region = records[0].Region
	}

	return &RIRecommendation{
		InstanceType:     instanceType,
		RecommendedCount: additionalRI,
		Term:             best.term,
		PaymentOption:    best.payment,
		EstimatedSavings: bestSavings,
		SavingsPercent:   best.discount,
		BreakEvenMonths:  breakEven,
		UpfrontCost:      upfrontCost,
		MonthlyRate:      perInstanceMonthly * (1 - best.discount) * float64(additionalRI),
		CurrentCoverage:  currentCoverage,
		TargetCoverage:   e.config.TargetCoverage,
		Confidence:       confidence,
		Reason:           fmt.Sprintf("Stable usage pattern (P50=%d, P90=%d, avg_util=%.0f%%)", p50, p90, avgUtil*100),
		Region:           region,
		IsGPU:            isGPU,
	}
}

// GetCoverageReport generates a current RI coverage report.
func (e *RIRecommendationEngine) GetCoverageReport() *RICoverageReport {
	e.mu.RLock()
	defer e.mu.RUnlock()

	report := &RICoverageReport{
		ByType:      make(map[string]*TypeCoverage),
		GeneratedAt: time.Now(),
	}

	for instanceType, records := range e.usageRecords {
		if len(records) == 0 {
			continue
		}

		// Current on-demand count (latest record)
		latest := records[len(records)-1]
		onDemandCount := latest.InstanceCount

		// RI count
		riCount := 0
		for _, ri := range e.riInventory[instanceType] {
			if ri.ExpiresAt.After(time.Now()) {
				riCount += ri.Count
			}
		}

		coverage := 0.0
		if onDemandCount > 0 {
			coverage = math.Min(float64(riCount)/float64(onDemandCount), 1.0)
		}

		tc := &TypeCoverage{
			InstanceType:    instanceType,
			OnDemandCount:   onDemandCount,
			RICount:         riCount,
			Coverage:        coverage,
			MonthlyOnDemand: latest.OnDemandCost * 730, // ~hours/month
		}

		report.ByType[instanceType] = tc
		report.TotalInstances += onDemandCount
		report.CoveredByRI += int(math.Min(float64(riCount), float64(onDemandCount)))

		if riCount > onDemandCount {
			report.WastedRI += riCount - onDemandCount
		}
	}

	if report.TotalInstances > 0 {
		report.CoveragePercent = float64(report.CoveredByRI) / float64(report.TotalInstances)
	}

	return report
}
