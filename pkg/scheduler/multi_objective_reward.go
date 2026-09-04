package scheduler

import (
	"math"
)

// ============================================================================
// MULTI-OBJECTIVE REWARD FUNCTIONS FOR RL SCHEDULER
// Fixes Defect #5: Single-objective throughput-only reward
// ============================================================================

// RewardConfig configures multi-objective reward weights
type RewardConfig struct {
	ThroughputWeight float64 // Default: 0.4
	FairnessWeight   float64 // Default: 0.3
	CostWeight       float64 // Default: 0.2
	EnergyWeight     float64 // Default: 0.1
}

// DefaultRewardConfig returns standard weights balanced for fairness and efficiency
func DefaultRewardConfig() RewardConfig {
	return RewardConfig{
		ThroughputWeight: 0.4,
		FairnessWeight:   0.3,
		CostWeight:       0.2,
		EnergyWeight:     0.1,
	}
}

// MultiObjectiveReward computes weighted combination of all objectives
// throughput: jobs completed per minute vs baseline
// fairness_gini: 1 - Gini coefficient of job completion times (higher = fairer)
// cost_efficiency: baseline_cost / actual_cost (>1 = cheaper than baseline)
// energy_savings: baseline_energy / actual_energy (>1 = more efficient than baseline)
func MultiObjectiveReward(cfg RewardConfig, throughput, fairness_gini, cost_efficiency, energy_saving float64) float64 {
	return cfg.ThroughputWeight*throughput +
		cfg.FairnessWeight*fairness_gini +
		cfg.CostWeight*cost_efficiency +
		cfg.EnergyWeight*energy_saving
}

// CalculateThroughputGain computes throughput improvement vs round-robin baseline
// current_jobs_per_min: observed throughput under current scheduling policy
// baseline_jobs_per_min: throughput under naive round-robin scheduling
func CalculateThroughputGain(currentJobsPerMin, baselineJobsPerMin float64) float64 {
	if baselineJobsPerMin == 0 {
		return 1.0 // No baseline means undefined ratio, return neutral
	}
	gain := currentJobsPerMin / baselineJobsPerMin
	return math.Max(0.0, gain) // Clip negative gains
}

// CalculateFairnessGini computes fairness score using Gini coefficient
// completionTimes: slice of job completion times in minutes
// Returns: 1 - Gini coefficient (1 = perfectly fair, 0 = maximally unfair)
func CalculateFairnessGini(completionTimes []float64) float64 {
	n := len(completionTimes)
	if n == 0 {
		return 1.0
	}
	
	// Sort completion times
	sortedTimes := make([]float64, n)
	copy(sortedTimes, completionTimes)
	sortFloat64Slice(sortedTimes)
	
	// Compute Gini coefficient using formula: G = (2 * sum(i*x_i) - (n+1) * sum(x_i)) / (n * sum(x_i))
	var sumXi, sumIXi float64
	for i, t := range sortedTimes {
		sumXi += t
		sumIXi += float64(i+1) * t
	}
	
	if sumXi == 0 {
		return 1.0
	}
	
	gini := (2.0*sumIXi - float64(n+1)*sumXi) / (float64(n) * sumXi)
	fairness := 1.0 - gini
	
	return math.Max(0.0, math.Min(1.0, fairness)) // Clip to [0, 1]
}

// CalculateCostEfficiency computes cost savings vs baseline
// baselineCost: estimated cost under naive scheduling (USD)
// actualCost: actual cost incurred (USD)
func CalculateCostEfficiency(baselineCost, actualCost float64) float64 {
	if actualCost == 0 {
		return 1.0 // No cost is perfect efficiency
	}
	efficiency := baselineCost / actualCost
	return math.Max(0.0, efficiency)
}

// CalculateEnergySavings computes energy efficiency improvement
// baselineEnergy: estimated energy consumption under naive scheduling (kWh)
// actualEnergy: actual energy consumed (kWh)
func CalculateEnergySavings(baselineEnergy, actualEnergy float64) float64 {
	if actualEnergy == 0 {
		return 1.0 // No energy consumption is ideal
	}
	savings := baselineEnergy / actualEnergy
	return math.Max(0.0, savings)
}

// sortFloat64Slice sorts a slice of float64 values in ascending order (bubble sort fallback for no imports)
func sortFloat64Slice(arr []float64) {
	n := len(arr)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if arr[j] > arr[j+1] {
				arr[j], arr[j+1] = arr[j+1], arr[j]
			}
		}
	}
}
