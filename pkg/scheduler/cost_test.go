// Package scheduler - cost_test.go verifies Module 17 cost-aware scheduling:
// exact integer-cent math, multi-node comparison, the 0.7:0.3 mix formula,
// budget rejection, and JSONL persistence round-trips.
package scheduler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ptr helpers keep CostModelUpdate patches terse in tests.
func ptrInt(v int) *int               { return &v }
func ptrFloat(v float64) *float64     { return &v }
func ptrString(v string) *string      { return &v }
func ptrBool(v bool) *bool            { return &v }

// TestEstimate_CalculationAccuracy: duration=2h, 4x a100 @ $8.50 → $68.00
// exactly (integer cents: 850 × 4 × 200 / 100 = 6800 cents).
// CPU/memory are only billed when the job actually requests them.
func TestEstimate_CalculationAccuracy(t *testing.T) {
	opt := NewDefaultCostOptimizer(nil)

	t.Run("pure GPU job is exact", func(t *testing.T) {
		job := JobSpec{Name: "train-job", GPUCount: 4, GPUType: "a100", DurationHours: 2}
		est, err := opt.Estimate(job, "node-x") // unconfigured node → default a100 rules
		require.NoError(t, err)
		require.NotNil(t, est)
		assert.Equal(t, 68.0, est.TotalCost, "4 × $8.50 × 2h must be exactly $68.00")
		assert.Equal(t, 2.0, est.DurationHours)
		require.Len(t, est.Breakdown, 1, "no cpu/memory requested → single GPU line")
		assert.Equal(t, "gpu", est.Breakdown[0].Component)
		assert.Equal(t, 68.0, est.Breakdown[0].Amount)
		assert.Contains(t, est.Breakdown[0].Detail, "a100 x 4")
		assert.Contains(t, est.Breakdown[0].Detail, "$8.50")
	})

	t.Run("cpu and memory lines priced in exact cents", func(t *testing.T) {
		job := JobSpec{
			Name: "full-job", GPUCount: 4, GPUType: "a100", DurationHours: 2,
			CPUCores: 16, MemoryGB: 64,
		}
		est, err := opt.Estimate(job, "node-x")
		require.NoError(t, err)
		// gpu $68.00 + cpu 16×$0.10×2h=$3.20 + mem 64×$0.01×2h=$1.28 → $72.48
		assert.Equal(t, 72.48, est.TotalCost)
		require.Len(t, est.Breakdown, 3)
		assert.Equal(t, 3.20, est.Breakdown[1].Amount)
		assert.Equal(t, 1.28, est.Breakdown[2].Amount)
	})

	t.Run("configured node model overrides defaults", func(t *testing.T) {
		models := map[string]*CostModel{
			"gpu-custom": {
				NodeID: "gpu-custom", GPUCount: 8, GPUTypes: []string{"a100"},
				GPUCostPerHour: map[string]float64{"a100": 9.5},
				SpotDiscount:   DefaultSpotDiscount, CPUCostPerHour: DefaultCPUCostPerHour,
				MemoryGBPrice: DefaultMemoryGBPrice,
			},
		}
		opt := NewDefaultCostOptimizer(models)
		job := JobSpec{Name: "j", GPUCount: 4, GPUType: "a100", DurationHours: 2}
		est, err := opt.Estimate(job, "gpu-custom")
		require.NoError(t, err)
		assert.Equal(t, 76.0, est.TotalCost, "4 × $9.50 × 2h = $76.00")
	})

	t.Run("spot discount multiplies only the GPU line", func(t *testing.T) {
		models := map[string]*CostModel{
			"spot-node": {
				NodeID: "spot-node", GPUCount: 8, GPUTypes: []string{"a100"},
				GPUCostPerHour: map[string]float64{"a100": 8.5}, SpotDiscount: 0.4,
				CPUCostPerHour: DefaultCPUCostPerHour, MemoryGBPrice: DefaultMemoryGBPrice,
				UseSpot: true,
			},
		}
		opt := NewDefaultCostOptimizer(models)
		job := JobSpec{Name: "j", GPUCount: 4, GPUType: "a100", DurationHours: 2, CPUCores: 16}
		est, err := opt.Estimate(job, "spot-node")
		require.NoError(t, err)
		// gpu 68×0.4 = 27.20 + cpu 3.20 (cpu not discounted) = 30.40
		assert.Equal(t, 30.40, est.TotalCost)
		assert.Equal(t, 27.20, est.Breakdown[0].Amount)
		assert.Contains(t, est.Breakdown[0].Detail, "spot")
	})

	t.Run("invalid job rejected", func(t *testing.T) {
		_, err := opt.Estimate(JobSpec{Name: "x", GPUCount: 0, DurationHours: 1}, "n")
		require.Error(t, err)
		_, err = opt.Estimate(JobSpec{Name: "x", GPUCount: 1, DurationHours: 0}, "n")
		require.Error(t, err)
	})

	t.Run("vendor prefixes normalize", func(t *testing.T) {
		assert.Equal(t, "a100", NormalizeGPUType("NVIDIA-A100-SXM4-80GB"))
		assert.Equal(t, "a100", NormalizeGPUType("a100"))
		assert.Equal(t, "a10g", NormalizeGPUType("nvidia-a10g"))
		assert.Equal(t, 12.0, DefaultGPUCostForType("nvidia-h100"))
	})
}

// TestCompare_ReturnsBestChoice: three nodes at different prices, all with
// healthy RL scores → the cheapest node that keeps its RL score competitive
// must win under the 0.7:0.3 mix.
func TestCompare_ReturnsBestChoice(t *testing.T) {
	opt := NewDefaultCostOptimizer(nil)
	nodes := []NodeScore{
		{NodeName: "gpu-a100", GPUFreeCount: 8, GPUType: "a100", Score: 85}, // $68
		{NodeName: "gpu-h100", GPUFreeCount: 8, GPUType: "h100", Score: 85}, // $96
		{NodeName: "gpu-a10g", GPUFreeCount: 8, GPUType: "a10g", Score: 85}, // $22.80
	}
	// No pinned GPU type: each node is priced for the GPU type it offers
	// (a100 $68 / h100 $96 / a10g $22.80 for 4 GPUs × 2h).
	job := JobSpec{Name: "compare-job", GPUCount: 4, DurationHours: 2}

	choice := opt.Compare(nodes, job)
	require.NotNil(t, choice)
	require.NotNil(t, choice.Estimate)

	// a10g is cheapest (costScore 1.0) with rl 0.85 → mix 0.85×0.7+1.0×0.3=0.895
	assert.Equal(t, "gpu-a10g", choice.NodeID, "cheapest node with acceptable RL score must win")
	assert.Equal(t, 22.80, choice.Estimate.TotalCost)
	assert.InDelta(t, 1.0, choice.CostScore, 1e-9)
	assert.InDelta(t, 0.85, choice.RLScore, 1e-9)
	assert.InDelta(t, 0.895, choice.MixScore, 1e-9)
	assert.Len(t, choice.Alternatives, 2)

	// A pricier node can still win when its RL score dominates the mix:
	// a10g rl 0.5 → 0.5×0.7+1×0.3=0.65 ; a100 rl 0.99 → 0.99×0.7+0.3825×0.3≈0.8077
	// (costScore(a100) = (96−68)/(96−22.8) = 28/73.2 = 0.3825)
	nodes[2].Score = 50
	nodes[0].Score = 99
	choice2 := opt.Compare(nodes, job)
	require.NotNil(t, choice2)
	assert.Equal(t, "gpu-a100", choice2.NodeID, "high RL score must be able to outweigh raw price")
	assert.InDelta(t, 0.8077, choice2.MixScore, 5e-4)

	// A pinned GPU type prices every node for that type and excludes nodes
	// that do not offer it.
	pinned := opt.Compare(nodes, JobSpec{Name: "pinned", GPUCount: 4, GPUType: "a100", DurationHours: 2})
	require.NotNil(t, pinned)
	assert.Equal(t, "gpu-a100", pinned.NodeID, "pinned a100 job can only use the a100 node")
	require.Len(t, pinned.Alternatives, 2)
	for _, alt := range pinned.Alternatives {
		assert.Contains(t, alt.Message, "infeasible: job needs a100",
			"node %s must be rejected for type mismatch", alt.NodeID)
	}
}

// TestCompare_InfeasibleNodesExcluded: nodes without free capacity are
// rejected instead of silently priced.
func TestCompare_InfeasibleNodesExcluded(t *testing.T) {
	opt := NewDefaultCostOptimizer(nil)
	nodes := []NodeScore{
		{NodeName: "tiny", GPUFreeCount: 2, GPUType: "a100", Score: 90},
		{NodeName: "big", GPUFreeCount: 8, GPUType: "a100", Score: 90},
	}
	job := JobSpec{Name: "j", GPUCount: 4, GPUType: "a100", DurationHours: 1}
	choice := opt.Compare(nodes, job)
	require.NotNil(t, choice)
	assert.Equal(t, "big", choice.NodeID)
	require.Len(t, choice.Alternatives, 1)
	assert.Equal(t, "tiny", choice.Alternatives[0].NodeID)
	assert.Contains(t, choice.Alternatives[0].Message, "infeasible")

	// All-infeasible → empty choice with explanation, no panic.
	choiceAll := opt.Compare(nodes[:1], job)
	require.NotNil(t, choiceAll)
	assert.Empty(t, choiceAll.NodeID)
	assert.Nil(t, choiceAll.Estimate)
	assert.Contains(t, choiceAll.Reason, "no feasible node")
}

// TestMixScore_MixingCorrectness: rl=0.9, cost=0.5, w=0.7/0.3 → 0.78 EXACTLY
// (fixed-point 1e4 blend, not accumulated float error).
func TestMixScore_MixingCorrectness(t *testing.T) {
	opt := NewDefaultCostOptimizer(nil)

	got := opt.MixScore(0.9, 0.5, 0.7, 0.3)
	assert.Equal(t, 0.78, got, "0.9×0.7 + 0.5×0.3 must equal 0.78 exactly")

	// Weight-sum violation is normalized, never silently biased.
	assert.Equal(t, 0.78, opt.MixScore(0.9, 0.5, 0.07, 0.03), "off-by-10x weights normalize to the same blend")

	// Degenerate weights.
	assert.Equal(t, 0.0, opt.MixScore(0.9, 0.5, 0, 0))
	// Pure-cost / pure-RL endpoints.
	assert.Equal(t, 0.5, opt.MixScore(0.9, 0.5, 0.0, 1.0))
	assert.Equal(t, 0.9, opt.MixScore(0.9, 0.5, 1.0, 0.0))
	// Clamping: out-of-range scores saturate instead of extrapolating.
	assert.Equal(t, 1.0, opt.MixScore(1.7, 1.5, 0.5, 0.5))
	assert.Equal(t, 0.0, opt.MixScore(-1, -1, 0.5, 0.5))
}

// TestBudget_Rejected: $150 estimate vs $100 budget must be rejected with a
// message quantifying the overrun; a fitting cost must pass.
func TestBudget_Rejected(t *testing.T) {
	opt := NewDefaultCostOptimizer(nil)

	ok, msg := opt.CheckBudget(150, 100)
	assert.False(t, ok)
	assert.Contains(t, msg, "EXCEEDED")
	assert.Contains(t, msg, "$150.00")
	assert.Contains(t, msg, "$100.00")
	assert.Contains(t, msg, "$50.00", "overrun amount must be quantified")

	ok, msg = opt.CheckBudget(68, 100)
	assert.True(t, ok)
	assert.Contains(t, msg, "within budget")
	assert.Contains(t, msg, "68.0%")

	// Exact fit is within budget (integer cents, no float edge case).
	ok, _ = opt.CheckBudget(100.0, 100.0)
	assert.True(t, ok)

	// Budget flows through Estimate into the report fields.
	job := JobSpec{Name: "over", GPUCount: 4, GPUType: "a100", DurationHours: 2, Budget: 50}
	est, err := opt.Estimate(job, "node-x")
	require.NoError(t, err)
	assert.True(t, est.BudgetExceeded)
	assert.Contains(t, est.Message, "EXCEEDED")
}

// TestPersist_ModelUpdate: config updates must hit the disk as valid JSONL
// (one node per line) and round-trip losslessly through LoadCostModels.
func TestPersist_ModelUpdate(t *testing.T) {
	store := NewCostModelStore(t.TempDir())

	// Configure two nodes through the same patch path `cafctl cost config` uses.
	m1, models, err := store.UpdateCostModel("gpu-a", CostModelUpdate{
		GPUCount: ptrInt(8), GPUType: ptrString("a100"),
		GPUCost: ptrFloat(9.5), SpotDiscount: ptrFloat(0.4),
		CPUCost: ptrFloat(0.1),
	})
	require.NoError(t, err)
	assert.Equal(t, "gpu-a", m1.NodeID)
	assert.Equal(t, 9.5, m1.GPUCostPerHour["a100"])
	require.Len(t, models, 1)

	_, _, err = store.UpdateCostModel("gpu-b", CostModelUpdate{
		GPUType: ptrString("h100"), GPUCost: ptrFloat(12.0),
	})
	require.NoError(t, err)

	// File must exist and every line must be standalone-valid JSON.
	raw, err := os.ReadFile(store.ModelsPath())
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	require.Len(t, lines, 2, "one JSON object per node per line")
	var sawA, sawB bool
	for _, line := range lines {
		assert.True(t, json.Valid([]byte(line)), "line must be valid JSON: %s", line)
		var m CostModel
		require.NoError(t, json.Unmarshal([]byte(line), &m))
		switch m.NodeID {
		case "gpu-a":
			sawA = true
			assert.Equal(t, 8, m.GPUCount)
			assert.Equal(t, 9.5, m.GPUCostPerHour["a100"])
			assert.InDelta(t, 0.4, m.SpotDiscount, 1e-9)
			assert.False(t, m.UseSpot, "spot stays off until explicitly enabled")
			assert.False(t, m.UpdatedAt.IsZero())
		case "gpu-b":
			sawB = true
			assert.Equal(t, 12.0, m.GPUCostPerHour["h100"])
		}
	}
	assert.True(t, sawA && sawB, "both node lines persisted")

	// Round-trip: reload and price a job with the persisted models.
	reloaded, err := store.LoadCostModels()
	require.NoError(t, err)
	require.Len(t, reloaded, 2)
	opt := NewDefaultCostOptimizer(reloaded)
	est, err := opt.Estimate(JobSpec{Name: "j", GPUCount: 4, GPUType: "a100", DurationHours: 2}, "gpu-a")
	require.NoError(t, err)
	assert.Equal(t, 76.0, est.TotalCost, "persisted $9.50 override must survive the round-trip")

	// History append + reload round-trip.
	require.NoError(t, store.AppendCostHistory(CostHistoryEntry{
		JobName: "j", NodeID: "gpu-a", DurationHours: 2, TotalCost: 76, Chosen: true,
	}))
	require.NoError(t, store.AppendCostHistory(CostHistoryEntry{
		JobName: "j", NodeID: "gpu-b", DurationHours: 2, TotalCost: 96,
	}))
	hist, err := store.LoadCostHistory()
	require.NoError(t, err)
	require.Len(t, hist, 2)
	assert.True(t, hist[0].Chosen)
	assert.False(t, hist[1].Chosen)
	assert.Equal(t, "gpu-b", hist[1].NodeID)

	// Missing files degrade gracefully (empty, not error).
	fresh := NewCostModelStore(filepath.Join(t.TempDir(), "nonexistent"))
	empty, err := fresh.LoadCostModels()
	require.NoError(t, err)
	assert.Empty(t, empty)
	noHist, err := fresh.LoadCostHistory()
	require.NoError(t, err)
	assert.Nil(t, noHist)
}
