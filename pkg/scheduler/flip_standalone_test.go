//go:build ignore

package scheduler

import (
	"fmt"
	"os"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
)

func main() {
	fmt.Println("=== FLIP Benchmark Suite for DASP vs 2026 GPU Schedulers ===\n")

	// Test Configuration
	const (
		BenchmarkGPUs    = 16
		BenchmarkWorkloads = 1000
		BaseRandomSeed   = int64(42)
	)

	// Run head-to-head comparison on uniform distribution
	fmt.Println("Running Uniform Distribution Benchmark...")
	workload := scheduler.GenerateDistributionWorkload(scheduler.DistUniform, BenchmarkWorkloads, BaseRandomSeed)
	distWeights := scheduler.DistributionWeights(scheduler.DistUniform)

	// Run DASP
	daspGPUs := scheduler.NewGPUTopology(BenchmarkGPUs)
	daspStrategy := scheduler.DemandAwareSegregationPlacement{}
	start := time.Now()
	daspAccepted := scheduler.RunSchedulingWithDistribution(daspStrategy, daspGPUs, workload, distWeights)
	daspDuration := time.Since(start)

	// Run HAMi
	hamiGPUs := scheduler.NewGPUTopology(BenchmarkGPUs)
	hamiProxy := &scheduler.HamiProxy{}
	start = time.Now()
	hamiAccepted := scheduler.RunHamiScheduling(hamiProxy, hamiGPUs, workload)
	hamiDuration := time.Since(start)

	// Calculate metrics
	daspRate := float64(daspAccepted) / float64(len(workload)) * 100
	hamiRate := float64(hamiAccepted) / float64(len(workload)) * 100
	gap := daspRate - hamiRate

	fmt.Printf("\n�?DASP acceptance: %.2f%% (%d/%d) [%.0f ns/op]\n", 
		daspRate, daspAccepted, len(workload), float64(daspDuration)/float64(len(workload)))
	fmt.Printf("�?HAMi acceptance: %.2f%% (%d/%d) [%.0f ns/op]\n", 
		hamiRate, hamiAccepted, len(workload), float64(hamiDuration)/float64(len(workload)))
	fmt.Printf("�?Gap: +%.2f percentage points in favor of DASP\n", gap)

	if gap >= 8.0 {
		fmt.Printf("�?SUCCESS: DASP meets T2 MoAT requirement (+8pp improvement)\n")
		os.Exit(0)
	} else {
		fmt.Printf("�?WARNING: Gap %.2f%% smaller than expected 8%% target\n", gap)
		os.Exit(1)
	}
}
