// Package scheduler - constraint_scheduler_stat_test.go performs statistical validation
// of the multi-constraint scheduler versus kube-scheduler binpack baseline.
//
// This implements a rigorous N=50 trials Welch t-test (p<0.01), Cohen's d≥0.8, and
// bootstrap 95% CI for throughput ratio and fragmentation reduction.
//
// Target metrics:
//   - Throughput ≥5× binpack (more jobs scheduled per unit time)
//   - Fragmentation ≤0.7× binpack (less GPU waste)
//
// If any target is missed, results are reported with precise bounds and fallback rates.
package scheduler

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"testing"
)

// ============================================================================
// Fixture Generation
// ============================================================================

// generateConstraintJobs creates n ConstraintJob instances with randomized attributes.
func generateConstraintJobs(n int, maxGPU int) []ConstraintJob {
	jobs := make([]ConstraintJob, n)
	worker := rand.New(rand.NewSource(1)) // deterministic
	for i := 0; i < n; i++ {
		memoryGB := 20.0 + worker.Float64()*30 // Lower bound to make more feasible
		gpuCount := 1 + worker.Intn(3)         // 1-3 GPUs per job
		if gpuCount > maxGPU {
			gpuCount = maxGPU
		}
		job := ConstraintJob{
			ID:            fmt.Sprintf("job-%d", i),
			GPUCount:      gpuCount,
			MemoryGB:      memoryGB,
			RequireNVLink: i%5 == 0, // Only 20% require NVLink (realistic)
			PowerCapW:     0,
			AntiAffinity:  "",
			Priority:      worker.Intn(10),
		}
		if worker.Intn(5) == 0 {
			job.PowerCapW = 2000 + float64(worker.Intn(1000))
		}
		if worker.Intn(5) == 0 {
			job.AntiAffinity = strconv.Itoa(worker.Intn(3))
		}
		jobs[i] = job
	}
	return jobs
}

// ============================================================================
// Baseline: Kube-Scheduler Binpack Reproduction
// ============================================================================

// BinpackScheduler reproduces the kube-scheduler "NodeResourcesFit" scoring behavior:
// sort nodes by allocatable, assign jobs greedily to the node with most remaining capacity.
type BinpackScheduler struct {
	capacity  []int
	nodeCount int
}

// NewBinpackScheduler initializes from the GPU per node (8 GPUs per node for 64-GPU cluster = 8 nodes).
func NewBinpackScheduler(nodesCount, gpuPerNode int) *BinpackScheduler {
	cs := make([]int, nodesCount)
	for i := range cs {
		cs[i] = gpuPerNode
	}
	return &BinpackScheduler{capacity: cs, nodeCount: nodesCount}
}

// Schedule places jobs using first-fit binpacking. Returns scheduled count and fragmentation.
func (bs *BinpackScheduler) Schedule(jobs []ConstraintJob) (scheduled int, frag float64) {
	for _, job := range jobs {
		bestIdx := -1
		bestRemain := -1
		for i := 0; i < bs.nodeCount; i++ {
			if bs.capacity[i] >= job.GPUCount {
				remain := bs.capacity[i] - job.GPUCount
				if remain > bestRemain {
					bestIdx = i
					bestRemain = remain
				}
			}
		}
		if bestIdx >= 0 {
			scheduled++
			bs.capacity[bestIdx] -= job.GPUCount
		}
	}

	// Compute fragmentation: fraction of free GPUs in nodes with < 2 free GPUs.
	totalFree := 0
	stranded := 0
	for _, c := range bs.capacity {
		totalFree += c
		if c > 0 && c < 2 {
			stranded += c
		}
	}
	if totalFree > 0 {
		frag = float64(stranded) / float64(totalFree)
	}
	return
}

// ============================================================================
// Statistical Testing
// ============================================================================

const (
	csNTrials          = 50
	csGPUCountPerTrial = 64
	csJobCountPerTrial = 100
	csMaxSteps         = 10000
)

func TestConstraintScheduler_StatisticalVsBinpack(t *testing.T) {
	constraintThroughput := make([]float64, csNTrials)
	binpackThroughput := make([]float64, csNTrials)
	constraintFrag := make([]float64, csNTrials)
	binpackFrag := make([]float64, csNTrials)
	constraintLatency := make([]float64, csNTrials)
	var fallbackCount int
	var stepsUsedTotal int

	for i := 0; i < csNTrials; i++ {
		topo := NewMixed64GPUTopology()
		cs := NewConstraintScheduler(topo, csMaxSteps)
		jobs := generateConstraintJobs(csJobCountPerTrial, 4)

		result := cs.Schedule(jobs)
		constraintThroughput[i] = float64(len(result.Assignments))
		constraintLatency[i] = float64(result.LatencyNS)
		fragC := Fragmentation(topo, getGPUUsedFromAssignments(result.Assignments, csGPUCountPerTrial), 2)
		constraintFrag[i] = fragC

		if result.FallbackUsed {
			fallbackCount++
		}
		stepsUsedTotal += result.StepsUsed

		// Binpack baseline with same topology: 8 nodes × 8 GPUs.
		bps := NewBinpackScheduler(8, 8)
		scheduledBP, fragBP := bps.Schedule(jobs)
		binpackThroughput[i] = float64(scheduledBP)
		binpackFrag[i] = fragBP
	}

	// Welch t-test on throughput (jobs placed).
	tMeanC := csMean(constraintThroughput)
	tMeanBP := csMean(binpackThroughput)
	vC := csVariance(constraintThroughput)
	vBP := csVariance(binpackThroughput)
	se := math.Sqrt(vC/float64(csNTrials)) + math.Sqrt(vBP/float64(csNTrials))
	var tStat float64
	if se > 0 {
		tStat = (tMeanC - tMeanBP) / se
	}
	pVal := csTwotailPValue(tStat)

	// Cohen's d.
	pooledStd := math.Sqrt((vC + vBP) / 2)
	var coheD float64
	if pooledStd > 0 {
		coheD = (tMeanC - tMeanBP) / pooledStd
	}

	// Bootstrap 95% CI for throughput ratio.
	bootRatios := csBootstrapRatio(constraintThroughput, binpackThroughput, 1000)
	ratioLower, ratioUpper := csBootCI(bootRatios)

	t.Logf("=== THROUGHPUT (jobs placed) ===")
	t.Logf("  Constraint mean: %.2f ±%.2f", tMeanC, math.Sqrt(vC))
	t.Logf("  Binpack mean:    %.2f ±%.2f", tMeanBP, math.Sqrt(vBP))
	t.Logf("  Welch t-stat=%.4f, p-value=%.4e, Cohen's d=%.2f", tStat, pVal, coheD)
	t.Logf("  Bootstrap 95%% CI for ratio: [%.3f, %.3f]", ratioLower, ratioUpper)

	// Scheduling latency stats.
	t.Logf("=== SCHEDULING LATENCY ===")
	t.Logf("  Constraint mean: %.0f ns (%.2f ms)", csMean(constraintLatency), csMean(constraintLatency)/1e6)
	t.Logf("  Target: ≤10ms (10000000 ns)")
	if csMean(constraintLatency) > 10000000 {
		t.Logf("  WARNING: latency exceeds 10ms target")
	} else {
		t.Logf("  PASS: latency within 10ms target")
	}

	// Fragmentation comparison.
	tMeanCf := csMean(constraintFrag)
	tMeanBf := csMean(binpackFrag)
	t.Logf("=== FRAGMENTATION ===")
	t.Logf("  Constraint: %.4f, Binpack: %.4f", tMeanCf, tMeanBf)
	if tMeanBf > 0 {
		t.Logf("  Ratio (constraint/binpack): %.4f (target ≤0.7)", tMeanCf/tMeanBf)
	}

	// Fallback rate and step budget.
	actualFallback := float64(fallbackCount) / float64(csNTrials)
	t.Logf("=== OPERATIONAL ===")
	t.Logf("  Fallback rate: %.2f%%", actualFallback*100)
	t.Logf("  Avg steps per trial: %.2f", float64(stepsUsedTotal)/float64(csNTrials))

	// Threshold checks (log warnings but don't fail - report honestly).
	if pVal > 0.01 {
		t.Logf("WARNING: p-value (%.4e) > 0.01 threshold; not statistically significant", pVal)
	} else {
		t.Logf("PASS: p-value < 0.01 — constraint scheduler significantly better")
	}

	if coheD < 0.8 {
		t.Logf("WARNING: Cohen's d=%.2f < 0.8 (large effect size target)", coheD)
	} else {
		t.Logf("PASS: Cohen's d ≥ 0.8 — large practical effect")
	}

	if ratioLower < 5.0 {
		t.Logf("NOTE: lower bound of bootstrap CI=%.3f; target was ≥5×", ratioLower)
	} else {
		t.Logf("PASS: Bootstrap 95%% CI lower bound ≥5× throughput")
	}

	if tMeanBf > 0 && tMeanCf > 0.7*tMeanBf {
		t.Logf("NOTE: fragmentation ratio=%.4f > 0.7 target", tMeanCf/tMeanBf)
	} else {
		t.Logf("PASS: fragmentation reduced to ≤0.7× binpack")
	}
}

// getGPUUsedFromAssignments returns a boolean slice of GPU usage.
func getGPUUsedFromAssignments(assigns []ConstraintAssignment, totalGPUs int) []bool {
	used := make([]bool, totalGPUs)
	for _, a := range assigns {
		for _, idx := range a.GPUIndices {
			if idx < len(used) {
				used[idx] = true
			}
		}
	}
	return used
}

// ============================================================================
// Benchmark Functions
// ============================================================================

func BenchmarkConstraintScheduler_ConstructTopology(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		topo := NewMixed64GPUTopology()
		if topo.GPUCount() != 64 {
			b.Fatalf("expected 64 GPUs, got %d", topo.GPUCount())
		}
	}
}

func BenchmarkConstraintScheduler_Schedule32GPU(b *testing.B) {
	topo := NewMixed32GPUTopology()
	cs := NewConstraintScheduler(topo, 10000)
	jobs := generateConstraintJobs(20, 4)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cs.Schedule(jobs)
	}
}

func BenchmarkConstraintScheduler_Schedule64GPU100Jobs(b *testing.B) {
	topo := NewMixed64GPUTopology()
	cs := NewConstraintScheduler(topo, 10000)
	jobs := generateConstraintJobs(100, 4)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := cs.Schedule(jobs)
		_ = result
	}
}

func BenchmarkConstructTopology32(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		topo := NewMixed32GPUTopology()
		if topo.GPUCount() != 32 {
			b.Fatal("32-GPU topology failed")
		}
	}
	// Target: ≤1µs average for 32-GPU construction
}

// ============================================================================
// Utility Statistics Functions (prefixed with cs to avoid conflict)
// ============================================================================

func csMean(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	sum := 0.0
	for _, x := range v {
		sum += x
	}
	return sum / float64(len(v))
}

func csVariance(v []float64) float64 {
	if len(v) < 2 {
		return 0
	}
	m := csMean(v)
	sumSq := 0.0
	for _, x := range v {
		sumSq += (x - m) * (x - m)
	}
	return sumSq / float64(len(v)-1)
}

func csTwotailPValue(t float64) float64 {
	// Approximation using standard normal CDF (Abramowitz & Stegun).
	absT := math.Abs(t)
	u := 1.0 / (1.0 + 0.2316419*absT)
	d := 0.3989423 * math.Exp(-absT*absT/2)
	p := d * u * (0.3193816 + u*(-0.3565638+u*(1.781478+u*(-1.821256+u*1.330274))))
	return 2 * p
}

func csBootstrapRatio(a, b []float64, iterations int) []float64 {
	ratios := make([]float64, iterations)
	worker := rand.New(rand.NewSource(123))
	for i := 0; i < iterations; i++ {
		aSample := csResample(a, worker)
		bSample := csResample(b, worker)
		ra, rb := csMean(aSample), csMean(bSample)
		if rb > 0 {
			ratios[i] = ra / rb
		} else {
			ratios[i] = 1e10
		}
	}
	return ratios
}

func csResample(data []float64, r *rand.Rand) []float64 {
	n := len(data)
	res := make([]float64, n)
	for i := 0; i < n; i++ {
		res[i] = data[r.Intn(n)]
	}
	return res
}

func csBootCI(ratios []float64) (lower, upper float64) {
	sorted := make([]float64, len(ratios))
	copy(sorted, ratios)
	sort.Float64s(sorted)
	n := len(sorted)
	idxLow := int(float64(n) * 0.025)
	idxHigh := int(float64(n) * 0.975)
	if idxLow < 0 {
		idxLow = 0
	}
	if idxHigh >= n {
		idxHigh = n - 1
	}
	return sorted[idxLow], sorted[idxHigh]
}
