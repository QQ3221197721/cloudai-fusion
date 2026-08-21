// Package scheduler - mig_binpack.go implements a MIG-aware Min Fragmentation Increment (MFI)
// binpacking scheduler. This is a standalone, hardware-independent algorithmic implementation
// that models NVIDIA A100 80GB MIG placement constraints as measured on real hardware
// (see docs/final-hardware-validation/results/m2m3_a100.log).
//
// Unlike generic bin-packing, MIG placement has *position constraints*: a profile of a given
// size can only start at specific slice indices. This is a MIG-unique property that competitors
// (HAMi device-level binpack, K8s native) do not model at slice-index granularity.
//
// This file does NOT call into gpu_sharing.go (which is a nvidia-smi CLI wrapper); it is a pure
// algorithmic layer. To avoid a symbol clash with gpu_sharing.go's MIGProfile, the profile type
// here is named MIGSliceProfile.
//
// MIG slice model: A100 memory is partitioned into an 8-wide slice grid (indices 0..7). This is
// required to reproduce the real hardware layout where two 3g.40gb instances occupy placements
// 0:4 and 4:4 simultaneously (m2m3_a100.log line 62). The task's illustrative pseudocode used a
// bound of 7; we use 8 (totalSlices) to stay faithful to measured hardware and to make the
// 2x3g.40gb topology constructible.
package scheduler

import (
	"fmt"
	"math"
)

// totalSlices is the width of the A100 MIG memory slice grid (indices 0..7).
// Real hardware places two 3g.40gb at 0:4 and 4:4 => an 8-wide grid.
const totalSlices = 8

// ============================================================================
// MIG Profile Definitions (position-constrained)
// ============================================================================

// MIGSliceProfile represents a valid MIG slice configuration with position constraints.
// Size = number of contiguous slices; StartConstraints = valid starting slice indices.
type MIGSliceProfile struct {
	Name             string // e.g., "1g.10gb", "2g.20gb"
	Size             int    // contiguous slices required
	MemoryGB         int    // memory per instance
	StartConstraints []int  // valid start indices (from real hardware placements)
}

// A100Profiles defines all valid MIG profiles for A100 80GB based on real validation data.
// Position constraints come from physical MIG partitioning boundaries (m2m3_a100.log):
//   - 1g.10gb: 7 instances at placements 0:1 .. 6:1  => starts {0..6}
//   - 2g.20gb: max 3 instances, even-aligned         => starts {0,2,4}
//   - 3g.40gb: 2 instances at placements 0:4 and 4:4 => starts {0,4}
//   - 4g.40gb: 1 instance                            => start {0}
//   - 7g.80gb: whole GPU                             => start {0}
var A100Profiles = []MIGSliceProfile{
	{Name: "1g.10gb", Size: 1, MemoryGB: 10, StartConstraints: []int{0, 1, 2, 3, 4, 5, 6}},
	{Name: "2g.20gb", Size: 2, MemoryGB: 20, StartConstraints: []int{0, 2, 4}},
	{Name: "3g.40gb", Size: 4, MemoryGB: 40, StartConstraints: []int{0, 4}},
	{Name: "4g.40gb", Size: 4, MemoryGB: 40, StartConstraints: []int{0}},
	{Name: "7g.80gb", Size: 7, MemoryGB: 80, StartConstraints: []int{0}},
}

// profileByName returns the profile definition by name.
func profileByName(name string) (MIGSliceProfile, bool) {
	for _, p := range A100Profiles {
		if p.Name == name {
			return p, true
		}
	}
	return MIGSliceProfile{}, false
}

// ============================================================================
// Core Data Structures
// ============================================================================

// MIGAllocation represents a placed workload on a specific MIG slice range.
type MIGAllocation struct {
	WorkloadID  string
	GPUIndex    int
	ProfileName string
	StartSlice  int // start index within GPU (inclusive)
	EndSlice    int // exclusive end (StartSlice + Size)
}

// MIGSliceState tracks the occupancy of the 8-wide slice grid on a single GPU.
// Each slice can be occupied by at most one allocation.
type MIGSliceState struct {
	Slices      [totalSlices]bool      // is slice occupied?
	Allocations map[int]*MIGAllocation // key = start index -> allocation
	TotalUsed   int                    // count of used slices
}

// NewMIGSliceState creates an empty MIG slice state.
func NewMIGSliceState() *MIGSliceState {
	return &MIGSliceState{
		Allocations: make(map[int]*MIGAllocation),
	}
}

// CanPlace reports whether the profile can be placed at ANY valid start position.
func (s *MIGSliceState) CanPlace(p MIGSliceProfile) bool {
	for _, start := range p.StartConstraints {
		if start+p.Size > totalSlices {
			continue
		}
		if s.freeRange(start, p.Size) {
			return true
		}
	}
	return false
}

// freeRange reports whether [start, start+size) is entirely free.
func (s *MIGSliceState) freeRange(start, size int) bool {
	if start < 0 || start+size > totalSlices {
		return false
	}
	for i := start; i < start+size; i++ {
		if s.Slices[i] {
			return false
		}
	}
	return true
}

// firstValidStart returns the smallest valid start where the profile fits, or -1.
func (s *MIGSliceState) firstValidStart(p MIGSliceProfile) int {
	for _, start := range p.StartConstraints {
		if start+p.Size > totalSlices {
			continue
		}
		if s.freeRange(start, p.Size) {
			return start
		}
	}
	return -1
}

// Allocate validates and occupies slices for a profile at startIndex.
func (s *MIGSliceState) Allocate(p MIGSliceProfile, startIndex int, workloadID string, gpuIndex int) (*MIGAllocation, error) {
	valid := false
	for _, start := range p.StartConstraints {
		if start == startIndex {
			valid = true
			break
		}
	}
	if !valid {
		return nil, fmt.Errorf("start index %d is not a valid placement for profile %s", startIndex, p.Name)
	}
	if !s.freeRange(startIndex, p.Size) {
		return nil, fmt.Errorf("slices [%d,%d) are not free for profile %s", startIndex, startIndex+p.Size, p.Name)
	}

	alloc := &MIGAllocation{
		WorkloadID:  workloadID,
		GPUIndex:    gpuIndex,
		ProfileName: p.Name,
		StartSlice:  startIndex,
		EndSlice:    startIndex + p.Size,
	}
	for i := startIndex; i < startIndex+p.Size; i++ {
		s.Slices[i] = true
	}
	s.Allocations[startIndex] = alloc
	s.TotalUsed += p.Size
	return alloc, nil
}

// Free releases the given allocations.
func (s *MIGSliceState) Free(allocations []*MIGAllocation) error {
	for _, a := range allocations {
		if a == nil {
			continue
		}
		existing, ok := s.Allocations[a.StartSlice]
		if !ok || existing.WorkloadID != a.WorkloadID {
			return fmt.Errorf("allocation %s at start %d not found", a.WorkloadID, a.StartSlice)
		}
		for i := a.StartSlice; i < a.EndSlice; i++ {
			s.Slices[i] = false
		}
		delete(s.Allocations, a.StartSlice)
		s.TotalUsed -= (a.EndSlice - a.StartSlice)
	}
	return nil
}

// remaining returns the number of free slices.
func (s *MIGSliceState) remaining() int {
	return totalSlices - s.TotalUsed
}

// deepCopy creates a defensive copy of MIGSliceState.
func (s *MIGSliceState) deepCopy() *MIGSliceState {
	dst := NewMIGSliceState()
	dst.Slices = s.Slices
	dst.TotalUsed = s.TotalUsed
	for start, alloc := range s.Allocations {
		cpy := *alloc
		dst.Allocations[start] = &cpy
	}
	return dst
}

// GPUTopology represents a single A100 GPU with its MIG slice state.
type GPUTopology struct {
	Index    int
	State    *MIGSliceState
	MemoryGB int
}

// NewGPUTopology initialises `count` A100 GPUs (80GB each).
func NewGPUTopology(count int) []GPUTopology {
	gpus := make([]GPUTopology, count)
	for i := 0; i < count; i++ {
		gpus[i] = GPUTopology{
			Index:    i,
			State:    NewMIGSliceState(),
			MemoryGB: 80,
		}
	}
	return gpus
}

// deepCopyCluster clones the cluster (independent states) for reproducible benchmarks.
func deepCopyCluster(src []GPUTopology) []GPUTopology {
	dst := make([]GPUTopology, len(src))
	for i, g := range src {
		dst[i] = g
		dst[i].State = g.State.deepCopy()
	}
	return dst
}

// ============================================================================
// Fragmentation Metric
// ============================================================================

// FragmentationMetric computes a global fragmentation score for a single GPU state under an
// expected future workload distribution.
//
//	F(gpu, dist) = Σ_p dist[p] * capacityLoss(gpu, p)
//
// capacityLoss(gpu, p) = fraction of p's valid start positions that are currently blocked.
// A higher score means the state is more hostile to future placements. MFI greedily minimises
// the *increment* of this score, thereby preserving schedulability for large profiles.
func FragmentationMetric(state *MIGSliceState, profileDistribution map[string]float64) float64 {
	total := 0.0
	for _, p := range A100Profiles {
		w := profileDistribution[p.Name]
		if w == 0 {
			continue
		}
		total += w * capacityLoss(state, p)
	}
	return total
}

// capacityLoss returns the fraction of valid start positions for p that are blocked.
func capacityLoss(state *MIGSliceState, p MIGSliceProfile) float64 {
	valid := 0
	blocked := 0
	for _, start := range p.StartConstraints {
		if start+p.Size > totalSlices {
			continue
		}
		valid++
		if !state.freeRange(start, p.Size) {
			blocked++
		}
	}
	if valid == 0 {
		return 0
	}
	return float64(blocked) / float64(valid)
}

// defaultDistribution returns a uniform expectation over all profiles.
func defaultDistribution() map[string]float64 {
	return map[string]float64{
		"1g.10gb": 0.2,
		"2g.20gb": 0.2,
		"3g.40gb": 0.2,
		"4g.40gb": 0.2,
		"7g.80gb": 0.2,
	}
}

// ============================================================================
// Placement Strategies
// ============================================================================

// PlacementStrategy selects a (gpuIndex, startIndex) for a requested profile.
type PlacementStrategy interface {
	Name() string
	Select(gpus []GPUTopology, p MIGSliceProfile, dist map[string]float64) (gpuIdx int, startIdx int, err error)
}

var errNoPlacement = fmt.Errorf("no GPU available for placement")

// FirstFit: first GPU (in index order) where the profile fits.
type FirstFit struct{}

func (FirstFit) Name() string { return "FirstFit" }

func (FirstFit) Select(gpus []GPUTopology, p MIGSliceProfile, _ map[string]float64) (int, int, error) {
	for i := range gpus {
		if start := gpus[i].State.firstValidStart(p); start >= 0 {
			return i, start, nil
		}
	}
	return -1, -1, errNoPlacement
}

// BestFit: GPU with the smallest remaining slice count after placement (tightest fit).
type BestFit struct{}

func (BestFit) Name() string { return "BestFit" }

func (BestFit) Select(gpus []GPUTopology, p MIGSliceProfile, _ map[string]float64) (int, int, error) {
	bestGPU, bestStart := -1, -1
	minRemaining := math.MaxInt32
	for i := range gpus {
		start := gpus[i].State.firstValidStart(p)
		if start < 0 {
			continue
		}
		remaining := gpus[i].State.remaining() - p.Size
		if remaining < minRemaining {
			minRemaining = remaining
			bestGPU, bestStart = i, start
		}
	}
	if bestGPU == -1 {
		return -1, -1, errNoPlacement
	}
	return bestGPU, bestStart, nil
}

// HAMiBinpack: emulates Project-HAMI device-level binpack. It ignores slice-index constraints
// for *scoring* (picks the GPU with the most free slices) and only uses constraints to obtain a
// concrete start. This is the "device-level, not slice-index-aware" competitor baseline.
type HAMiBinpack struct{}

func (HAMiBinpack) Name() string { return "HAMiBinpack" }

func (HAMiBinpack) Select(gpus []GPUTopology, p MIGSliceProfile, _ map[string]float64) (int, int, error) {
	bestGPU, bestStart := -1, -1
	maxFree := -1
	for i := range gpus {
		start := gpus[i].State.firstValidStart(p)
		if start < 0 {
			continue
		}
		free := gpus[i].State.remaining()
		if free > maxFree {
			maxFree = free
			bestGPU, bestStart = i, start
		}
	}
	if bestGPU == -1 {
		return -1, -1, errNoPlacement
	}
	return bestGPU, bestStart, nil
}

// MinFragmentationIncrement (MFI) chooses the (gpu, startIdx) that minimises the increase in the
// fragmentation metric. It is the only strategy that reasons at slice-index granularity about how
// a placement blocks future large-profile placements.
//
//	bestGPU, bestDeltaF = -1, +inf
//	for gpu in GPUs:
//	  for startIdx in validStarts(gpu, p):
//	    dF = F(gpu after placing p@startIdx) - F(gpu before)
//	    if dF < bestDeltaF: bestGPU, bestStartIdx, bestDeltaF = gpu, startIdx, dF
//	return bestGPU, bestStartIdx
type MinFragmentationIncrement struct{}

func (MinFragmentationIncrement) Name() string { return "MFI" }

func (MinFragmentationIncrement) Select(gpus []GPUTopology, p MIGSliceProfile, dist map[string]float64) (int, int, error) {
	if dist == nil {
		dist = defaultDistribution()
	}
	bestGPU, bestStart := -1, -1
	bestDeltaF := math.MaxFloat64

	for i := range gpus {
		state := gpus[i].State
		fBefore := FragmentationMetric(state, dist)
		for _, start := range p.StartConstraints {
			if start+p.Size > totalSlices {
				continue
			}
			if !state.freeRange(start, p.Size) {
				continue
			}
			// Hypothetically place.
			for k := start; k < start+p.Size; k++ {
				state.Slices[k] = true
			}
			fAfter := FragmentationMetric(state, dist)
			// Undo.
			for k := start; k < start+p.Size; k++ {
				state.Slices[k] = false
			}

			deltaF := fAfter - fBefore
			if deltaF < bestDeltaF {
				bestDeltaF = deltaF
				bestGPU, bestStart = i, start
			}
		}
	}

	if bestGPU == -1 {
		return -1, -1, errNoPlacement
	}
	return bestGPU, bestStart, nil
}

// ============================================================================
// DASP Algorithm Implementation
// ============================================================================

// DASPClass categorizes GPU states based on what profiles they can still accept
type DASPClass string

const (
	ClassClean       DASPClass = "clean"        // Can still place 7g.80gb (completely empty)
	ClassLargeCap    DASPClass = "large-capable" // Can place at least one 3g.40gb or 4g.40gb
	ClassSmallOnly   DASPClass = "small-only"    // Can only place 1g/2g
	ClassFull        DASPClass = "full"          // Cannot place any profile
)

// classifyGPU categorizes a GPU based on its current state
func classifyGPU(state *MIGSliceState) DASPClass {
	// Check if can place 7g.80gb (largest profile)
	if state.CanPlace(A100Profiles[4]) { // 7g.80gb
		return ClassClean
	}
	// Check if can place 3g.40gb or 4g.40gb (large profiles)
	if state.CanPlace(A100Profiles[2]) || state.CanPlace(A100Profiles[3]) { // 3g.40gb or 4g.40gb
		return ClassLargeCap
	}
	// Check if can place 1g.10gb or 2g.20gb (small profiles)
	if state.CanPlace(A100Profiles[0]) || state.CanPlace(A100Profiles[1]) { // 1g.10gb or 2g.20gb
		return ClassSmallOnly
	}
	return ClassFull
}

// IsLargeProfile checks if a profile is considered "large" (needs special handling)
func IsLargeProfile(p MIGSliceProfile) bool {
	// Large profiles are those requiring >= 4 slices (40GB+)
	return p.Size >= 4
}

// DemandAwareSegregationPlacement (DASP) is a MIG-aware placement strategy that beats naive
// device-level binpack (HAMi) by *actively segregating* small and large requests to protect the
// scarce, position-constrained large-contiguous regions of A100 MIG GPUs.
//
// Key ideas:
//  1. GPU classification (recomputed each placement): clean (can host 7g), large-capable
//     (can host a 3g/4g but not 7g), small-only (only 1g/2g), full.
//  2. Demand-aware zoning: from the workload distribution we estimate the large-profile demand
//     ratio ρ and reserve R = round(ρ·N) GPUs (highest indices) as a "large zone", leaving the
//     rest as a "small zone". Zones are soft: each side spills into the other only when its own
//     side is exhausted.
//  3. Isolation placement:
//       - Large request (3g/4g/7g): best-fit inside the large zone (prefer large-capable over
//         clean so clean GPUs are spent last); cascade to small zone, then whole cluster.
//       - Small request (1g/2g): pack into the *dirtiest* card first (small-only, then
//         large-capable, then small-zone clean), so clean GPUs in the large zone stay pristine
//         for future big requests; cascade to the large zone only as a last resort.
//
// This directly counters HAMi's accidental-protection-via-spreading: DASP protects large
// contiguous regions *by design*, and its zoning prevents small requests from poisoning the
// cards reserved for large demand.
type DemandAwareSegregationPlacement struct{}

func (DemandAwareSegregationPlacement) Name() string { return "DASP" }

// computeReservationRatio returns the fraction of GPU *capacity* (slices, not request count)
// that large profiles (3g/4g/7g) are expected to consume. Slice-weighting is essential: a single
// large request occupies 4-7 slices versus 1-2 for a small one, so a raw request-count ratio
// badly under-reserves the large zone in small-dominated mixes (e.g. skew-small).
//
//	ρ = (Σ_{large p} dist[p]·size[p]) / (Σ_{all p} dist[p]·size[p])
func computeReservationRatio(dist map[string]float64) float64 {
	if dist == nil {
		dist = defaultDistribution()
	}
	sumSlices, largeSlices := 0.0, 0.0
	for _, p := range A100Profiles {
		w := dist[p.Name] * float64(p.Size)
		sumSlices += w
		if IsLargeProfile(p) {
			largeSlices += w
		}
	}
	if sumSlices <= 0 {
		return 0
	}
	return largeSlices / sumSlices
}

// bestFitIn scans the given GPU indices and returns the (gpuIdx, start) with the smallest
// remaining free slices after placing p (tightest fit). Returns (-1,-1) if none fits.
func bestFitIn(gpus []GPUTopology, idxs []int, p MIGSliceProfile) (int, int) {
	bestGPU, bestStart := -1, -1
	minRemaining := math.MaxInt32
	for _, gi := range idxs {
		start := gpus[gi].State.firstValidStart(p)
		if start < 0 {
			continue
		}
		rem := gpus[gi].State.remaining() - p.Size
		if rem < minRemaining {
			minRemaining = rem
			bestGPU, bestStart = gi, start
		}
	}
	return bestGPU, bestStart
}

// dirtiestFitIn scans the given GPU indices and returns the (gpuIdx, start) on the *most occupied*
// card that still fits p (fewest remaining free slices among cards that can host p). This packs
// small requests tightly onto already-contaminated cards. Returns (-1,-1) if none fits.
func dirtiestFitIn(gpus []GPUTopology, idxs []int, p MIGSliceProfile) (int, int) {
	bestGPU, bestStart := -1, -1
	minRemaining := math.MaxInt32
	for _, gi := range idxs {
		start := gpus[gi].State.firstValidStart(p)
		if start < 0 {
			continue
		}
		rem := gpus[gi].State.remaining()
		if rem < minRemaining {
			minRemaining = rem
			bestGPU, bestStart = gi, start
		}
	}
	return bestGPU, bestStart
}

func (DemandAwareSegregationPlacement) Select(gpus []GPUTopology, p MIGSliceProfile, dist map[string]float64) (int, int, error) {
	n := len(gpus)
	if n == 0 {
		return -1, -1, errNoPlacement
	}

	// Demand-aware zoning: reserve R = round(ρ·N) GPUs (highest indices) as the large zone.
	rho := computeReservationRatio(dist)
	R := int(math.Round(rho * float64(n)))
	if R < 0 {
		R = 0
	}
	if R > n {
		R = n
	}
	smallZoneEnd := n - R // small zone = [0, smallZoneEnd), large zone = [smallZoneEnd, n)

	// Classify every GPU and bucket by (zone, class).
	var (
		szSmallOnly, szLargeCap, szClean []int // small-zone buckets
		lzSmallOnly, lzLargeCap, lzClean []int // large-zone buckets
	)
	for i := range gpus {
		cls := classifyGPU(gpus[i].State)
		inSmallZone := i < smallZoneEnd
		switch cls {
		case ClassClean:
			if inSmallZone {
				szClean = append(szClean, i)
			} else {
				lzClean = append(lzClean, i)
			}
		case ClassLargeCap:
			if inSmallZone {
				szLargeCap = append(szLargeCap, i)
			} else {
				lzLargeCap = append(lzLargeCap, i)
			}
		case ClassSmallOnly:
			if inSmallZone {
				szSmallOnly = append(szSmallOnly, i)
			} else {
				lzSmallOnly = append(lzSmallOnly, i)
			}
		case ClassFull:
			// unusable
		}
	}

	gpu, start := -1, -1

	if IsLargeProfile(p) {
		// Large request: consume the large zone first, preferring large-capable over clean so
		// pristine (clean) cards are spent last. 7g can only land on clean cards.
		if gpu, start = bestFitIn(gpus, lzLargeCap, p); gpu == -1 {
			gpu, start = bestFitIn(gpus, lzClean, p)
		}
		// Cascade into the small zone if the large zone cannot host it.
		if gpu == -1 {
			if gpu, start = bestFitIn(gpus, szLargeCap, p); gpu == -1 {
				gpu, start = bestFitIn(gpus, szClean, p)
			}
		}
	} else {
		// Small request: pack onto the dirtiest small-zone card first to protect large-zone
		// clean cards. Order: small-only -> large-capable -> clean, all within the small zone,
		// each using dirtiest-fit for tight packing.
		if gpu, start = dirtiestFitIn(gpus, szSmallOnly, p); gpu == -1 {
			if gpu, start = dirtiestFitIn(gpus, szLargeCap, p); gpu == -1 {
				gpu, start = dirtiestFitIn(gpus, szClean, p)
			}
		}
		// Cascade into the large zone only if the small zone is exhausted. Even here, prefer
		// already-dirty large-zone cards (small-only, then large-capable) before clean ones.
		if gpu == -1 {
			if gpu, start = dirtiestFitIn(gpus, lzSmallOnly, p); gpu == -1 {
				if gpu, start = dirtiestFitIn(gpus, lzLargeCap, p); gpu == -1 {
					gpu, start = dirtiestFitIn(gpus, lzClean, p)
				}
			}
		}
	}

	if gpu == -1 {
		return -1, -1, errNoPlacement
	}
	return gpu, start, nil
}

// ============================================================================
// Scheduler
// ============================================================================

// AllocationResult reports the outcome of a scheduling decision.
type AllocationResult struct {
	Allocation *MIGAllocation
	GPUIndex   int
	StartSlice int
}

// MIGScheduler places workloads across a GPU cluster using a pluggable strategy.
type MIGScheduler struct {
	GPUs         []GPUTopology
	Distribution map[string]float64
}

// NewMIGScheduler builds a scheduler over the given cluster.
func NewMIGScheduler(gpus []GPUTopology, dist map[string]float64) *MIGScheduler {
	if dist == nil {
		dist = defaultDistribution()
	}
	return &MIGScheduler{GPUs: gpus, Distribution: dist}
}

// Schedule places a single request using the supplied strategy.
func (s *MIGScheduler) Schedule(workloadID, profileName string, algo PlacementStrategy) (*AllocationResult, error) {
	p, ok := profileByName(profileName)
	if !ok {
		return nil, fmt.Errorf("unknown profile: %s", profileName)
	}
	gpuIdx, startIdx, err := algo.Select(s.GPUs, p, s.Distribution)
	if err != nil {
		return nil, err
	}
	alloc, err := s.GPUs[gpuIdx].State.Allocate(p, startIdx, workloadID, gpuIdx)
	if err != nil {
		return nil, err
	}
	return &AllocationResult{Allocation: alloc, GPUIndex: gpuIdx, StartSlice: startIdx}, nil
}

// Utilization returns average slice utilization across all GPUs (0..1).
func (s *MIGScheduler) Utilization() float64 {
	if len(s.GPUs) == 0 {
		return 0
	}
	sum := 0.0
	for i := range s.GPUs {
		sum += float64(s.GPUs[i].State.TotalUsed) / float64(totalSlices)
	}
	return sum / float64(len(s.GPUs))
}

// ClusterFragmentation returns the average per-GPU fragmentation metric.
func (s *MIGScheduler) ClusterFragmentation() float64 {
	if len(s.GPUs) == 0 {
		return 0
	}
	sum := 0.0
	for i := range s.GPUs {
		sum += FragmentationMetric(s.GPUs[i].State, s.Distribution)
	}
	return sum / float64(len(s.GPUs))
}
