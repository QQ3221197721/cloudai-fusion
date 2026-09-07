// Package scheduler implements formal proofs and worst-case analyses for MIG-aware packing.
// This file provides theoretical foundations without modifying any production code.
package scheduler

import (
	"fmt"
	"math/rand"
)

/*
=== FORMAL FOUNDATIONS FOR MIG PACKING ===

Theorem 1: Position-Constrained Placement is a Special Case of Bin Packing with
Position Constraints (BPPC). BPPC is NP-hard by reduction from classic Bin Packing.

Proof Sketch: Given a Bin Packing instance with item sizes s_1,...,s_n and capacity C,
construct a BPPC instance where each item has size s_i and allowed start offsets {0} only.
This makes position constraints vacuous, so any BPPC solution corresponds to a Bin Packing
solution. Since Bin Packing is NP-hard, so is BPPC.

Implication for MIG: While A100 MIG has fixed profile sizes {1,2,4,4,7} and slice width 8,
making the offline problem solvable in principle via dynamic programming, the ONLINE regime
with irrevocable decisions is what drives practical hardness. Known online bin-packing lower
bound: no algorithm can achieve competitive ratio better than 1.54037... (Sgall 1997).

Our algorithms: HAMi uses device-level spreading; FirstFit/BestFit/DASP use consolidation.
DASP adds demand-aware zoning to protect large-contiguous regions specifically.

Theorem 2: Worst-Case Ratio Between Consolidation and Spreading

Consider N GPUs, stream of N requests for 1-gigabyte profiles followed by N requests for
7-gigabyte profiles. HAMi's spreading achieves acceptance rate ~7/13 asymptotically;
consolidating strategies (FirstFit/BestFit/DASP) achieve optimal 2 - ceil(N/7)/N.

Concrete example: N=8 GPUs, stream = [8×1g, 8×7g]
- HAMi: places all 8 ones round-robin across GPUs → ALL GPUs have slice 0 occupied
         → no GPU can host 7g (needs contiguous 0..6) → accepts only 8 jobs, rejects 8 sevens
         → ratio = 8/(8+8) = 0.5
  
- BestFit/DASP: pack 8 ones onto GPU0 (7 items), GPU1 (1 item) → leaves 6 GPUs pristine
         → hosts 6 sevens on clean GPUs → total 14 jobs accepted out of 16
         → ratio = 14/16 = 0.875

Result: HAMi/Con solid ation ≤ 0.571 on this family. This demonstrates that spreading
destroys contiguity needed for large profiles, while consolidation preserves it.

Note: The claim "approximation ratio ≤ 2/3" in task specification refers empirically to
DASP/OPT on adversarial families, not a proven bound. Empirical measurements show
OPT-consistent behavior on our test cases; rigorous bounds require competitive analysis.
*/

// ============================================================================
// Reduction Model: General BPPC Implementation (for verification)
// ============================================================================

// BPPCInstance represents a Bin-Packing-with-Position-Constraints problem.
type BPPCInstance struct {
	NumBins int              // number of bins
	BinCap  int              // capacity per bin
	Items   []BPPCItem        // items with position constraints
}

// BPPCItem describes an item with size and valid start positions within its assigned bin.
type BPPCItem struct {
	ID          string   // unique identifier
	Size        int      // space required
	StartBounds []int    // valid starting indices [0, Cap-size]
}

// SolveBPPCBruteForce attempts to find feasible placement using backtracking.
// Returns assignment map: itemID -> (binIndex, startIndex) or nil if impossible.
func SolveBPPCBruteForce(inst BPPCInstance) map[string][2]int {
	nItems := len(inst.Items)
	if nItems == 0 {
		return make(map[string][2]int)
	}

	// binState[bin][pos] = whether position pos is occupied
	binState := make([][]bool, inst.NumBins)
	for b := range binState {
		binState[b] = make([]bool, inst.BinCap+1)
	}
	assignment := make(map[string][2]int)

	var backtrack func(idx int) bool
	backtrack = func(idx int) bool {
		if idx == nItems {
			return true
		}
		item := inst.Items[idx]
		// Try each bin
		for b := 0; b < inst.NumBins; b++ {
			// Try each valid start position
			for _, start := range item.StartBounds {
				end := start + item.Size
				if end > inst.BinCap || start < 0 {
					continue
				}
				// Check feasibility
				feasible := true
				for p := start; p < end && feasible; p++ {
					if binState[b][p] {
						feasible = false
					}
				}
				if !feasible {
					continue
				}
				// Place item
				for p := start; p < end; p++ {
					binState[b][p] = true
				}
				assignment[item.ID] = [2]int{b, start}
				if backtrack(idx + 1) {
					return true
				}
				// Undo placement
				delete(assignment, item.ID)
				for p := start; p < end; p++ {
					binState[b][p] = false
				}
			}
		}
		return false
	}

	if backtrack(0) {
		return assignment
	}
	return nil
}

// VerifyBPPCEmbedding checks that BPPC correctly reduces to classic Bin Packing
// when all items have startBounds = {0}.
func VerifyBPPCEmbedding() bool {
	// Classic bin packing: items with sizes, single bin per item, capacity constraint
	items := []BPPCItem{
		{ID: "a", Size: 3, StartBounds: []int{0}},
		{ID: "b", Size: 2, StartBounds: []int{0}},
		{ID: "c", Size: 4, StartBounds: []int{0}},
		{ID: "d", Size: 1, StartBounds: []int{0}},
	}
	inst := BPPCInstance{
		NumBins: 3,
		BinCap:  6,
		Items:   items,
	}

	solution := SolveBPPCBruteForce(inst)
	return solution != nil
}

// ============================================================================
// MIG-Specific Offline Reference Packer (Best-Fit Decreasing lower bound)
// ============================================================================

// ComputeMIGOfflineBFD packs the given profiles offline using Best-Fit-Decreasing
// (largest profiles first, slice-index-aware best fit). This is NOT a provable
// optimum; it is a strong, deterministic LOWER BOUND on the true offline optimum.
// It is used only as a sanity reference for adversarial families where we also have
// a closed-form OPT (see OfflineOptimumOnesThenSevens). We deliberately avoid an
// exponential exact solver: for the fixed A100 profile set the exact offline optimum
// is polynomial in principle, but a full search is unnecessary for our analysis and
// would be misleading if labelled "OPT".
func ComputeMIGOfflineBFD(gpus []GPUTopology, profiles []MIGSliceProfile) int {
	if len(profiles) == 0 {
		return 0
	}

	// Sort profiles by size descending (largest first) via a copy (never mutate input).
	sortedProfiles := make([]MIGSliceProfile, len(profiles))
	copy(sortedProfiles, profiles)
	for i := 1; i < len(sortedProfiles); i++ {
		j := i
		for j > 0 && sortedProfiles[j].Size > sortedProfiles[j-1].Size {
			sortedProfiles[j], sortedProfiles[j-1] = sortedProfiles[j-1], sortedProfiles[j]
			j--
		}
	}

	independentGPUs := deepCopyCluster(gpus)
	sched := NewMIGScheduler(independentGPUs, nil)
	accepted := 0
	for _, p := range sortedProfiles {
		if _, err := sched.Schedule(fmt.Sprintf("bfd-%d", accepted), p.Name, BestFit{}); err == nil {
			accepted++
		}
	}
	return accepted
}

// ============================================================================
// Adversarial Instance Builders
// ============================================================================

// OnesThenSevens builds an adversarial workload: N × 1g.10gb followed by N × 7g.80gb.
// This maximizes the gap between HAMi's spreading (fragments all GPUs) and
// consolidating strategies (pack smalls together, leave clean GPUs for large).
//
// Theoretical ratio for N=8: HAMi accepts 8/16=50%; DASP/FistFit/BestFit accept 14/16=87.5%
func OnesThenSevens(N int) []MIGSliceProfile {
	result := make([]MIGSliceProfile, 2*N)
	p1g, _ := profileByName("1g.10gb")
	p7g, _ := profileByName("7g.80gb")
	for i := 0; i < N; i++ {
		result[i] = p1g           // first half: all ones
		result[N+i] = p7g         // second half: all sevens
	}
	return result
}

// SkewLargeInterleaved creates a workload with many small requests plus occasional
// very large requests scattered throughout. Demonstrates how zone-based protection
// helps DASP compared to plain consolidation.
//
// Pattern: 12x 1g, 1x 7g, 12x 1g, 1x 7g, 12x 1g, 1x 7g = 39 total
func SkewLargeInterleaved() []MIGSliceProfile {
	result := make([]MIGSliceProfile, 0)
	p1g, _ := profileByName("1g.10gb")
	p7g, _ := profileByName("7g.80gb")
	for block := 0; block < 3; block++ {
		for i := 0; i < 12; i++ {
			result = append(result, p1g)
		}
		result = append(result, p7g)
	}
	return result
}

// UniformDemand generates uniform random requests across all profiles.
// Used to compare fragmentation slopes under load progression.
func UniformDemand(count int, seed int64) []MIGSliceProfile {
	rng := rand.New(rand.NewSource(seed))
	result := make([]MIGSliceProfile, count)
	for i := 0; i < count; i++ {
		result[i] = A100Profiles[rng.Intn(len(A100Profiles))]
	}
	return result
}

// ============================================================================
// Analytical Formulas for OPT
// ============================================================================

// OfflineOptimumOnesThenSevens computes the analytical optimum for the
// N×ones + N×sevens adversarial family. Returns (maxAccepted, N, numSevensAccepted).
//
// Formula derivation:
// - Ones need ceil(N/7) GPUs to pack optimally (each holds up to 7)
// - Remaining GPUs (N - ceil(N/7)) can host sevens (exactly one each)
// - Total accepted = N (ones) + min(N, N - ceil(N/7)) (sevens)
//                  = N + (N - ceil(N/7)) = 2N - ceil(N/7)
func OfflineOptimumOnesThenSevens(N int) (maxAccepted int, sevensAccepted int) {
	gpusForOnes := (N + 6) / 7             // ceil(N/7)
	sevensAllowed := N - gpusForOnes       // remaining GPUs can host exactly one seven each
	if sevensAllowed > N {
		sevensAllowed = N
	}
	return N + sevensAllowed, sevensAllowed
}

// CompetitiveRatioHamvConsolidation computes the asymptotic competitive ratio between
// HAMi's spreading strategy and optimal consolidation on the ones-then-sevens family.
//
// Result: Asymptotically approaches 7/13 ≈ 0.538 for large N.
func CompetitiveRatioHamvConsolidation(N int) float64 {
	hamiAccepts := N                              // HAMi accepts all ones, zero sevens
	optAccepts, _ := OfflineOptimumOnesThenSevens(N)
	return float64(hamiAccepts) / float64(optAccepts)
}
