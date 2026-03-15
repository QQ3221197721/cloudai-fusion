// Package scheduler - cost_optimizer.go implements cost-aware scheduling with
// Spot/Preemptible instance support and Reserved Instance optimization.
// Balances cost savings with workload reliability using a mixed instance strategy.
// Provides intelligent fallback from spot to on-demand when interruption risk is high.
package scheduler

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// Cost Optimizer Configuration
// ============================================================================

// InstancePricingType defines instance pricing models
type InstancePricingType string

const (
	PricingOnDemand  InstancePricingType = "on-demand"
	PricingSpot      InstancePricingType = "spot"
	PricingReserved  InstancePricingType = "reserved"
	PricingPreemptible InstancePricingType = "preemptible" // GCP terminology
)

// CostOptimizerConfig holds cost optimization configuration
type CostOptimizerConfig struct {
	SpotMaxPriceRatio       float64 `json:"spot_max_price_ratio"`       // max spot/on-demand ratio to bid
	SpotInterruptionBuffer  int     `json:"spot_interruption_buffer_sec"` // seconds to gracefully handle interruption
	ReservedUtilThreshold   float64 `json:"reserved_util_threshold"`    // min utilization to justify reserved
	ReservedBreakevenMonths int     `json:"reserved_breakeven_months"`  // months to break even on reserved
	MaxSpotPercent          float64 `json:"max_spot_percent"`           // max % of fleet on spot
	FallbackToOnDemand      bool    `json:"fallback_to_on_demand"`      // auto-fallback when spot unavailable
	CostWeightInScoring     float64 `json:"cost_weight_in_scoring"`     // weight in scheduling score (0-1)
	Enabled                 bool    `json:"enabled"`
}

// DefaultCostOptimizerConfig returns production-ready defaults
func DefaultCostOptimizerConfig() CostOptimizerConfig {
	return CostOptimizerConfig{
		SpotMaxPriceRatio:       0.7,  // pay at most 70% of on-demand
		SpotInterruptionBuffer:  120,  // 2 minutes for checkpointing
		ReservedUtilThreshold:   60.0, // 60%+ utilization justifies reserved
		ReservedBreakevenMonths: 6,
		MaxSpotPercent:          60.0,
		FallbackToOnDemand:      true,
		CostWeightInScoring:     0.25,
		Enabled:                 true,
	}
}

// ============================================================================
// Instance Pricing & Availability Models
// ============================================================================

// InstancePricing holds pricing information for an instance type
type InstancePricing struct {
	InstanceType    string              `json:"instance_type"`
	Provider        string              `json:"provider"`
	Region          string              `json:"region"`
	Zone            string              `json:"zone"`
	GPUType         string              `json:"gpu_type"`
	GPUCount        int                 `json:"gpu_count"`
	OnDemandPrice   float64             `json:"on_demand_price_per_hour"`
	SpotPrice       float64             `json:"spot_price_per_hour"`
	ReservedPrice   float64             `json:"reserved_price_per_hour"` // 1-year, no upfront
	SpotDiscount    float64             `json:"spot_discount_percent"`
	ReservedDiscount float64            `json:"reserved_discount_percent"`
	SpotAvailable   bool                `json:"spot_available"`
	SpotInterruptProb float64           `json:"spot_interrupt_probability"` // 0-1, higher = more likely to be interrupted
	LastUpdated     time.Time           `json:"last_updated"`
}

// InstanceAllocation represents how an instance is allocated
type InstanceAllocation struct {
	ID              string              `json:"id"`
	InstanceType    string              `json:"instance_type"`
	PricingType     InstancePricingType `json:"pricing_type"`
	WorkloadID      string              `json:"workload_id"`
	WorkloadType    string              `json:"workload_type"`
	Provider        string              `json:"provider"`
	Region          string              `json:"region"`
	HourlyPrice     float64             `json:"hourly_price"`
	AllocatedAt     time.Time           `json:"allocated_at"`
	ExpectedRuntime time.Duration       `json:"expected_runtime"`
	Preemptible     bool                `json:"preemptible"` // can be interrupted
	CheckpointURL   string              `json:"checkpoint_url,omitempty"` // for spot resumption
}

// CostReport summarizes scheduling cost analysis
type CostReport struct {
	Period             string                    `json:"period"`
	TotalCost          float64                   `json:"total_cost"`
	OnDemandCost       float64                   `json:"on_demand_cost"`
	SpotCost           float64                   `json:"spot_cost"`
	ReservedCost       float64                   `json:"reserved_cost"`
	SpotSavings        float64                   `json:"spot_savings"`
	ReservedSavings    float64                   `json:"reserved_savings"`
	TotalSavings       float64                   `json:"total_savings"`
	SavingsPercent     float64                   `json:"savings_percent"`
	SpotInterruptions  int                       `json:"spot_interruptions"`
	InstanceBreakdown  map[string]int            `json:"instance_breakdown"`  // type → count
	PricingBreakdown   map[string]float64        `json:"pricing_breakdown"`   // type → cost
	Recommendations    []CostRecommendation      `json:"recommendations"`
}

// CostRecommendation is a specific cost optimization suggestion
type CostRecommendation struct {
	Type             string  `json:"type"`              // "switch-to-spot", "reserve", "right-size"
	Priority         string  `json:"priority"`          // "high", "medium", "low"
	Description      string  `json:"description"`
	EstimatedSavings float64 `json:"estimated_savings_per_month"`
	Confidence       float64 `json:"confidence"`
}

// ============================================================================
// Cost Optimizer
// ============================================================================

// CostOptimizer manages cost-aware scheduling decisions
type CostOptimizer struct {
	config       CostOptimizerConfig
	pricingDB    map[string]*InstancePricing   // instance_type → pricing
	allocations  map[string]*InstanceAllocation // allocation_id → allocation
	costHistory  []DailyCostEntry
	logger       *logrus.Logger
	mu           sync.RWMutex
}

// DailyCostEntry tracks daily cost
type DailyCostEntry struct {
	Date         time.Time `json:"date"`
	OnDemandCost float64   `json:"on_demand_cost"`
	SpotCost     float64   `json:"spot_cost"`
	ReservedCost float64   `json:"reserved_cost"`
	Interruptions int      `json:"interruptions"`
}

// NewCostOptimizer creates a new cost optimizer
func NewCostOptimizer(cfg CostOptimizerConfig) *CostOptimizer {
	co := &CostOptimizer{
		config:      cfg,
		pricingDB:   make(map[string]*InstancePricing),
		allocations: make(map[string]*InstanceAllocation),
		costHistory: make([]DailyCostEntry, 0),
		logger:      logrus.StandardLogger(),
	}

	// Initialize with common GPU instance pricing
	co.initDefaultPricing()
	return co
}

// initDefaultPricing populates pricing for common GPU instances
func (co *CostOptimizer) initDefaultPricing() {
	defaults := []InstancePricing{
		{InstanceType: "p4d.24xlarge", Provider: "aws", Region: "us-east-1", GPUType: "nvidia-a100", GPUCount: 8, OnDemandPrice: 32.77, SpotPrice: 12.0, ReservedPrice: 20.0, SpotAvailable: true, SpotInterruptProb: 0.15},
		{InstanceType: "p5.48xlarge", Provider: "aws", Region: "us-east-1", GPUType: "nvidia-h100", GPUCount: 8, OnDemandPrice: 98.32, SpotPrice: 35.0, ReservedPrice: 60.0, SpotAvailable: true, SpotInterruptProb: 0.20},
		{InstanceType: "g5.12xlarge", Provider: "aws", Region: "us-east-1", GPUType: "nvidia-a10g", GPUCount: 4, OnDemandPrice: 5.672, SpotPrice: 2.0, ReservedPrice: 3.5, SpotAvailable: true, SpotInterruptProb: 0.10},
		{InstanceType: "a2-highgpu-8g", Provider: "gcp", Region: "us-central1", GPUType: "nvidia-a100", GPUCount: 8, OnDemandPrice: 29.39, SpotPrice: 8.82, ReservedPrice: 18.5, SpotAvailable: true, SpotInterruptProb: 0.12},
		{InstanceType: "a3-highgpu-8g", Provider: "gcp", Region: "us-central1", GPUType: "nvidia-h100", GPUCount: 8, OnDemandPrice: 88.94, SpotPrice: 26.68, ReservedPrice: 55.0, SpotAvailable: true, SpotInterruptProb: 0.18},
		{InstanceType: "Standard_ND96amsr_A100_v4", Provider: "azure", Region: "eastus", GPUType: "nvidia-a100", GPUCount: 8, OnDemandPrice: 32.77, SpotPrice: 13.0, ReservedPrice: 20.5, SpotAvailable: true, SpotInterruptProb: 0.14},
		{InstanceType: "ecs.gn7-c13g1.13xlarge", Provider: "aliyun", Region: "cn-beijing", GPUType: "nvidia-a100", GPUCount: 8, OnDemandPrice: 28.50, SpotPrice: 10.0, ReservedPrice: 18.0, SpotAvailable: true, SpotInterruptProb: 0.10},
	}

	for i := range defaults {
		d := &defaults[i]
		d.SpotDiscount = (1 - d.SpotPrice/d.OnDemandPrice) * 100
		d.ReservedDiscount = (1 - d.ReservedPrice/d.OnDemandPrice) * 100
		d.LastUpdated = time.Now().UTC()
		co.pricingDB[d.InstanceType] = d
	}
}

// SelectOptimalPricing chooses the best pricing strategy for a workload
func (co *CostOptimizer) SelectOptimalPricing(workloadType string, gpuType string, gpuCount int, expectedHours float64, priority int, faultTolerant bool) *PricingDecision {
	co.mu.RLock()
	defer co.mu.RUnlock()

	// Find matching instances
	candidates := co.findMatchingInstances(gpuType, gpuCount)
	if len(candidates) == 0 {
		return &PricingDecision{
			PricingType: PricingOnDemand,
			Reason:      "No matching instances found",
		}
	}

	// Sort by effective cost
	best := candidates[0]

	decision := &PricingDecision{
		InstanceType: best.InstanceType,
		Provider:     best.Provider,
		Region:       best.Region,
	}

	// Decision logic based on workload characteristics
	switch {
	case priority >= 9:
		// Critical workloads → on-demand or reserved (never spot)
		if expectedHours > float64(co.config.ReservedBreakevenMonths)*720 {
			decision.PricingType = PricingReserved
			decision.HourlyPrice = best.ReservedPrice
			decision.Reason = "Critical workload with long runtime → reserved instance"
			decision.MonthlySavings = (best.OnDemandPrice - best.ReservedPrice) * 720
		} else {
			decision.PricingType = PricingOnDemand
			decision.HourlyPrice = best.OnDemandPrice
			decision.Reason = "Critical workload → on-demand instance"
		}

	case faultTolerant && best.SpotAvailable && best.SpotInterruptProb < 0.25:
		// Fault-tolerant + low interrupt risk → spot
		spotPercent := co.currentSpotPercent()
		if spotPercent < co.config.MaxSpotPercent {
			decision.PricingType = PricingSpot
			decision.HourlyPrice = best.SpotPrice
			decision.MonthlySavings = (best.OnDemandPrice - best.SpotPrice) * 720
			decision.Reason = fmt.Sprintf("Fault-tolerant workload, spot discount %.0f%%, interrupt prob %.0f%%",
				best.SpotDiscount, best.SpotInterruptProb*100)
		} else {
			decision.PricingType = PricingOnDemand
			decision.HourlyPrice = best.OnDemandPrice
			decision.Reason = fmt.Sprintf("Spot fleet at %.0f%% limit", co.config.MaxSpotPercent)
		}

	case workloadType == "inference" || workloadType == "serving":
		// Inference → reserved if long-running, spot if fault-tolerant
		if expectedHours > 720 { // > 1 month
			decision.PricingType = PricingReserved
			decision.HourlyPrice = best.ReservedPrice
			decision.MonthlySavings = (best.OnDemandPrice - best.ReservedPrice) * 720
			decision.Reason = "Long-running inference → reserved instance"
		} else if faultTolerant && best.SpotAvailable {
			decision.PricingType = PricingSpot
			decision.HourlyPrice = best.SpotPrice
			decision.MonthlySavings = (best.OnDemandPrice - best.SpotPrice) * expectedHours
			decision.Reason = "Short inference with fallback → spot instance"
		} else {
			decision.PricingType = PricingOnDemand
			decision.HourlyPrice = best.OnDemandPrice
			decision.Reason = "Inference workload → on-demand"
		}

	case workloadType == "training" && expectedHours > 24:
		// Long training → consider reserved
		decision.PricingType = PricingReserved
		decision.HourlyPrice = best.ReservedPrice
		decision.MonthlySavings = (best.OnDemandPrice - best.ReservedPrice) * 720
		decision.Reason = "Long training job → reserved instance"

	default:
		// Default: on-demand with spot opportunity
		if best.SpotAvailable && faultTolerant && best.SpotPrice < best.OnDemandPrice*co.config.SpotMaxPriceRatio {
			decision.PricingType = PricingSpot
			decision.HourlyPrice = best.SpotPrice
			decision.MonthlySavings = (best.OnDemandPrice - best.SpotPrice) * expectedHours
			decision.Reason = "Good spot price → spot instance"
		} else {
			decision.PricingType = PricingOnDemand
			decision.HourlyPrice = best.OnDemandPrice
			decision.Reason = "Default → on-demand instance"
		}
	}

	return decision
}

// PricingDecision represents the chosen pricing strategy
type PricingDecision struct {
	InstanceType   string              `json:"instance_type"`
	Provider       string              `json:"provider"`
	Region         string              `json:"region"`
	PricingType    InstancePricingType `json:"pricing_type"`
	HourlyPrice    float64             `json:"hourly_price"`
	MonthlySavings float64             `json:"monthly_savings"`
	Reason         string              `json:"reason"`
}

// findMatchingInstances finds instances matching GPU requirements
func (co *CostOptimizer) findMatchingInstances(gpuType string, gpuCount int) []*InstancePricing {
	var matches []*InstancePricing
	for _, p := range co.pricingDB {
		if p.GPUType == gpuType && p.GPUCount >= gpuCount {
			matches = append(matches, p)
		}
	}

	// Sort by on-demand price (ascending)
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].OnDemandPrice < matches[j].OnDemandPrice
	})

	return matches
}

// currentSpotPercent calculates what percentage of allocations are spot
func (co *CostOptimizer) currentSpotPercent() float64 {
	total := len(co.allocations)
	if total == 0 {
		return 0
	}
	spotCount := 0
	for _, a := range co.allocations {
		if a.PricingType == PricingSpot {
			spotCount++
		}
	}
	return float64(spotCount) / float64(total) * 100
}

// RecordAllocation tracks a new instance allocation
func (co *CostOptimizer) RecordAllocation(alloc *InstanceAllocation) {
	co.mu.Lock()
	defer co.mu.Unlock()

	if alloc.ID == "" {
		alloc.ID = common.NewUUID()
	}
	co.allocations[alloc.ID] = alloc
}

// ReleaseAllocation removes an allocation
func (co *CostOptimizer) ReleaseAllocation(allocID string) {
	co.mu.Lock()
	defer co.mu.Unlock()
	delete(co.allocations, allocID)
}

// GenerateCostReport generates a cost analysis report
func (co *CostOptimizer) GenerateCostReport() *CostReport {
	co.mu.RLock()
	defer co.mu.RUnlock()

	report := &CostReport{
		Period:            "current",
		InstanceBreakdown: make(map[string]int),
		PricingBreakdown:  make(map[string]float64),
	}

	for _, alloc := range co.allocations {
		report.InstanceBreakdown[alloc.InstanceType]++

		monthlyEstimate := alloc.HourlyPrice * 720 // approx monthly hours
		switch alloc.PricingType {
		case PricingOnDemand:
			report.OnDemandCost += monthlyEstimate
		case PricingSpot:
			report.SpotCost += monthlyEstimate
		case PricingReserved:
			report.ReservedCost += monthlyEstimate
		}
		report.PricingBreakdown[string(alloc.PricingType)] += monthlyEstimate
	}

	report.TotalCost = report.OnDemandCost + report.SpotCost + report.ReservedCost

	// Calculate savings vs all-on-demand baseline
	allOnDemandCost := 0.0
	for _, alloc := range co.allocations {
		if p, ok := co.pricingDB[alloc.InstanceType]; ok {
			allOnDemandCost += p.OnDemandPrice * 720
		} else {
			allOnDemandCost += alloc.HourlyPrice * 720
		}
	}

	report.SpotSavings = allOnDemandCost - report.OnDemandCost - report.SpotCost - report.ReservedCost
	if report.SpotSavings < 0 {
		report.SpotSavings = 0
	}
	report.TotalSavings = report.SpotSavings
	if allOnDemandCost > 0 {
		report.SavingsPercent = report.TotalSavings / allOnDemandCost * 100
	}

	// Generate recommendations
	report.Recommendations = co.generateRecommendations()

	return report
}

// generateRecommendations produces cost optimization suggestions
func (co *CostOptimizer) generateRecommendations() []CostRecommendation {
	var recs []CostRecommendation

	// Check for on-demand instances that could be spot
	onDemandCount := 0
	spotCandidates := 0
	for _, alloc := range co.allocations {
		if alloc.PricingType == PricingOnDemand {
			onDemandCount++
			if alloc.Preemptible {
				spotCandidates++
			}
		}
	}

	if spotCandidates > 0 {
		savings := 0.0
		for _, alloc := range co.allocations {
			if alloc.PricingType == PricingOnDemand && alloc.Preemptible {
				if p, ok := co.pricingDB[alloc.InstanceType]; ok {
					savings += (p.OnDemandPrice - p.SpotPrice) * 720
				}
			}
		}
		recs = append(recs, CostRecommendation{
			Type:             "switch-to-spot",
			Priority:         "high",
			Description:      fmt.Sprintf("Switch %d fault-tolerant on-demand instances to spot", spotCandidates),
			EstimatedSavings: math.Round(savings*100) / 100,
			Confidence:       0.85,
		})
	}

	// Check for long-running instances that should be reserved
	longRunning := 0
	for _, alloc := range co.allocations {
		if alloc.PricingType == PricingOnDemand && alloc.ExpectedRuntime > 720*time.Hour {
			longRunning++
		}
	}
	if longRunning > 0 {
		recs = append(recs, CostRecommendation{
			Type:             "reserve",
			Priority:         "medium",
			Description:      fmt.Sprintf("Convert %d long-running on-demand instances to reserved", longRunning),
			EstimatedSavings: float64(longRunning) * 3000, // rough estimate
			Confidence:       0.90,
		})
	}

	return recs
}

// UpdatePricing updates pricing data for an instance type
func (co *CostOptimizer) UpdatePricing(pricing *InstancePricing) {
	co.mu.Lock()
	defer co.mu.Unlock()

	pricing.SpotDiscount = (1 - pricing.SpotPrice/pricing.OnDemandPrice) * 100
	pricing.ReservedDiscount = (1 - pricing.ReservedPrice/pricing.OnDemandPrice) * 100
	pricing.LastUpdated = time.Now().UTC()
	co.pricingDB[pricing.InstanceType] = pricing
}

// GetPricingDB returns all instance pricing data
func (co *CostOptimizer) GetPricingDB() map[string]*InstancePricing {
	co.mu.RLock()
	defer co.mu.RUnlock()

	result := make(map[string]*InstancePricing, len(co.pricingDB))
	for k, v := range co.pricingDB {
		result[k] = v
	}
	return result
}

// GetAllocations returns all current allocations
func (co *CostOptimizer) GetAllocations() map[string]*InstanceAllocation {
	co.mu.RLock()
	defer co.mu.RUnlock()

	result := make(map[string]*InstanceAllocation, len(co.allocations))
	for k, v := range co.allocations {
		result[k] = v
	}
	return result
}
