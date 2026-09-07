// Package scheduler - FLIP Benchmark Utilities and Workload Generators
package scheduler

import (
	"fmt"
	"math/rand"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

const (
	// Distribution constants for FLIP benchmarks
	DistUniform   = "uniform"    // 20% each profile
	DistSkewSmall = "skew-small" // ~80% small, ~20% large
	DistSkewBig   = "skew-big"   // ~80% large, ~20% small
	DistBimodal   = "bimodal"    // 50% smallest + largest
)

// GenerateDistributionWorkload creates a benchmark workload trace from specified distribution
func GenerateDistributionWorkload(dist string, n int, seed int64) []common.BenchmarkWorkload {
	rng := rand.New(rand.NewSource(seed))
	workloads := make([]common.BenchmarkWorkload, 0, n)

	switch dist {
	case DistUniform:
		for i := 0; i < n; i++ {
			profileIdx := rng.Intn(len(A100Profiles))
			profileName := A100Profiles[profileIdx].Name
			workloads = append(workloads, common.NewSimpleBenchmarkWorkload(fmt.Sprintf("wl-%d", i), profileName))
		}

	case DistSkewSmall:
		type pair struct{ profile string; weight float64 }
		pairs := []pair{{"1g.10gb", 0.80}, {"2g.20gb", 0.10}, {"3g.40gb", 0.05}, {"4g.40gb", 0.03}, {"7g.80gb", 0.02}}
		for i := 0; i < n; i++ {
			r := rng.Float64()
			cum := 0.0
			var selectedProfile string
			for _, pp := range pairs {
				cum += pp.weight
				if r < cum {
					selectedProfile = pp.profile
					break
				}
			}
			workloads = append(workloads, common.NewSimpleBenchmarkWorkload(fmt.Sprintf("wl-%d", i), selectedProfile))
		}

	case DistSkewBig:
		type pair struct{ profile string; weight float64 }
		pairs := []pair{{"7g.80gb", 0.80}, {"4g.40gb", 0.10}, {"3g.40gb", 0.05}, {"2g.20gb", 0.03}, {"1g.10gb", 0.02}}
		for i := 0; i < n; i++ {
			r := rng.Float64()
			cum := 0.0
			var selectedProfile string
			for _, pp := range pairs {
				cum += pp.weight
				if r < cum {
					selectedProfile = pp.profile
					break
				}
			}
			workloads = append(workloads, common.NewSimpleBenchmarkWorkload(fmt.Sprintf("wl-%d", i), selectedProfile))
		}

	case DistBimodal:
		type pair struct{ profile string; weight float64 }
		pairs := []pair{{"1g.10gb", 0.50}, {"7g.80gb", 0.50}}
		for i := 0; i < n; i++ {
			r := rng.Float64()
			cum := 0.0
			var selectedProfile string
			for _, pp := range pairs {
				cum += pp.weight
				if r < cum {
					selectedProfile = pp.profile
					break
				}
			}
			workloads = append(workloads, common.NewSimpleBenchmarkWorkload(fmt.Sprintf("wl-%d", i), selectedProfile))
		}

	default:
		for i := 0; i < n; i++ {
			profileIdx := rng.Intn(len(A100Profiles))
			profileName := A100Profiles[profileIdx].Name
			workloads = append(workloads, common.NewSimpleBenchmarkWorkload(fmt.Sprintf("wl-%d", i), profileName))
		}
	}

	return workloads
}

// GenerateOnesThenSevensWorkload creates the canonical adversarial pattern: N ones followed by N sevens
func GenerateOnesThenSevensWorkload(nOnes, nSevens int) []common.BenchmarkWorkload {
	workloads := make([]common.BenchmarkWorkload, 0, nOnes+nSevens)
	for i := 0; i < nOnes; i++ {
		workloads = append(workloads, common.NewSimpleBenchmarkWorkload(fmt.Sprintf("one-%d", i), "1g.10gb"))
	}
	for i := 0; i < nSevens; i++ {
		workloads = append(workloads, common.NewSimpleBenchmarkWorkload(fmt.Sprintf("seven-%d", i), "7g.80gb"))
	}
	return workloads
}

// DistributionWeights returns demand distribution weights for adaptive tuning
func DistributionWeights(dist string) map[string]float64 {
	switch dist {
	case DistUniform:
		return map[string]float64{"1g.10gb": 0.20, "2g.20gb": 0.20, "3g.40gb": 0.20, "4g.40gb": 0.20, "7g.80gb": 0.20}
	case DistSkewSmall:
		return map[string]float64{"1g.10gb": 0.80, "2g.20gb": 0.10, "3g.40gb": 0.05, "4g.40gb": 0.03, "7g.80gb": 0.02}
	case DistSkewBig:
		return map[string]float64{"1g.10gb": 0.02, "2g.20gb": 0.03, "3g.40gb": 0.05, "4g.40gb": 0.10, "7g.80gb": 0.80}
	case DistBimodal:
		return map[string]float64{"1g.10gb": 0.50, "7g.80gb": 0.50}
	default:
		return map[string]float64{"1g.10gb": 0.20, "2g.20gb": 0.20, "3g.40gb": 0.20, "4g.40gb": 0.20, "7g.80gb": 0.20}
	}
}
