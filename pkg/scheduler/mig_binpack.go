package scheduler

import (
	"fmt"
	"math"
	"sync"
	"time"
)

const totalSlices = 8

type MIGSliceProfile struct {
	Name             string
	Size             int
	MemoryGB         int
	StartConstraints []int
}

var A100Profiles = []MIGSliceProfile{
	{Name: "1g.10gb", Size: 1, MemoryGB: 10, StartConstraints: []int{0, 1, 2, 3, 4, 5, 6}},
	{Name: "2g.20gb", Size: 2, MemoryGB: 20, StartConstraints: []int{0, 2, 4, 6}},
	{Name: "3g.40gb", Size: 3, MemoryGB: 40, StartConstraints: []int{0, 4}},
	{Name: "4g.40gb", Size: 4, MemoryGB: 40, StartConstraints: []int{0}},
	{Name: "7g.80gb", Size: 7, MemoryGB: 80, StartConstraints: []int{0}},
	{Name: "8g.80gb", Size: 8, MemoryGB: 80, StartConstraints: []int{0}},
}

type GPUState struct {
	Slices        []bool
	Allocations   map[int]*Allocation
	mu            sync.Mutex
}

// GetTotalAllocated returns count of allocated slices
func (state *GPUState) GetTotalAllocated() int {
	count := 0
	for _, occupied := range state.Slices {
		if occupied {
			count++
		}
	}
	return count
}

type Allocation struct {
	WorkloadID   string
	ProfileName  string
	StartSlice   int
	EndSlice     int
	CreatedAt    time.Time
}

// firstValidStart finds first valid starting position for a MIG slice profile on given GPU
func (state *GPUState) firstValidStart(profile MIGSliceProfile) int {
	for _, constraint := range profile.StartConstraints {
		if state.canPlaceAt(constraint, profile.Size) {
			return constraint
		}
	}
	return -1
}

// canPlaceAt checks if a profile fits at a given start position
func (state *GPUState) canPlaceAt(start, size int) bool {
	const totalSlices = 8
	if start+size > totalSlices {
		return false
	}

	for i := start; i < start+size; i++ {
		if state.Slices[i] {
			return false
		}
	}
	return true
}

// remaining returns count of free slices on this GPU
func (state *GPUState) remaining() int {
	count := 0
	for _, occupied := range state.Slices {
		if !occupied {
			count++
		}
	}
	return count
}

type GPUTopology struct {
	Index  int
	State  *GPUState
	MemoryGB int  // Total memory capacity in GB for this GPU
}

type PlacementStrategy interface {
	Select(gpus []GPUTopology, profile MIGSliceProfile, dist map[string]float64) (gpuIdx int, startSlice int, err error)
	Name() string
}

type BestFit struct{}

func (b BestFit) Name() string { return "BestFit" }

func (b BestFit) Select(gpus []GPUTopology, profile MIGSliceProfile, dist map[string]float64) (int, int, error) {
	bestGPU := -1
	bestStart := -1
	minRemaining := math.MaxInt32

	for i := range gpus {
		start := firstValidStart(gpus[i].State, profile)
		if start < 0 {
			continue
		}

		remaining := countFreeSlices(gpus[i].State) - profile.Size

		if remaining < minRemaining {
			minRemaining = remaining
			bestGPU = i
			bestStart = start
		}
	}

	if bestGPU == -1 {
		return -1, -1, fmt.Errorf("no suitable GPU found")
	}

	return bestGPU, bestStart, nil
}

type FirstFit struct{}

func (f FirstFit) Name() string { return "FirstFit" }

func (f FirstFit) Select(gpus []GPUTopology, profile MIGSliceProfile, dist map[string]float64) (int, int, error) {
	for i := range gpus {
		start := firstValidStart(gpus[i].State, profile)
		if start >= 0 {
			return i, start, nil
		}
	}
	return -1, -1, fmt.Errorf("no suitable GPU found")
}

type HAMiBinpack struct{}

func (h HAMiBinpack) Name() string { return "HAMiBinpack" }

func (h HAMiBinpack) Select(gpus []GPUTopology, profile MIGSliceProfile, dist map[string]float64) (int, int, error) {
	candidates := make([]int, 0)

	for i := range gpus {
		start := firstValidStart(gpus[i].State, profile)
		if start >= 0 {
			candidates = append(candidates, i)
		}
	}

	if len(candidates) == 0 {
		return -1, -1, fmt.Errorf("no suitable GPU found")
	}

	bestGPU := candidates[0]
	minUtil := float64(countOccupiedSlices(gpus[bestGPU].State)) / float64(totalSlices)

	for _, gpuIdx := range candidates[1:] {
		util := float64(countOccupiedSlices(gpus[gpuIdx].State)) / float64(totalSlices)
		if util < minUtil {
			minUtil = util
			bestGPU = gpuIdx
		}
	}

	return bestGPU, firstValidStart(gpus[bestGPU].State, profile), nil
}

type GPUClass int

const (
	ClassClean GPUClass = iota
	ClassSmallOnly
	ClassLargeCap
	ClassFull
)

func classifyGPU(state *GPUState, isLargeZone bool, largeFraction float64) GPUClass {
	state.mu.Lock()
	defer state.mu.Unlock()

	freeSlices := countFreeSlicesUnlocked(state)

	if freeSlices == totalSlices {
		return ClassClean
	}

	if freeSlices == 0 {
		return ClassFull
	}

	canHostLarge := hasContiguousRegionUnlocked(state, 7)

	if !isLargeZone && canHostLarge {
		return ClassLargeCap
	}

	if !isLargeZone && !canHostLarge {
		return ClassSmallOnly
	}

	return ClassClean
}

func hasContiguousRegion(state *GPUState, requiredSize int) bool {
	state.mu.Lock()
	defer state.mu.Unlock()
	return hasContiguousRegionUnlocked(state, requiredSize)
}

func hasContiguousRegionUnlocked(state *GPUState, requiredSize int) bool {
	consecutive := 0
	for i := 0; i < totalSlices; i++ {
		if !state.Slices[i] {
			consecutive++
			if consecutive >= requiredSize {
				return true
			}
		} else {
			consecutive = 0
		}
	}
	return false
}

func computeLargeRequestFraction(dist map[string]float64) float64 {
	largeProfiles := []string{"3g.40gb", "4g.40gb", "7g.80gb"}

	largeFraction := 0.0
	for profile, weight := range dist {
		for _, large := range largeProfiles {
			if profile == large {
				largeFraction += weight
			}
		}
	}

	return largeFraction
}

func IsLargeProfile(profile MIGSliceProfile) bool {
	return profile.Size >= 3
}

func calculateOptimalZoneSize(largeFraction float64, totalGPUs int, profileDist map[string]float64) int {
	emaAlpha := 0.3
	peakDemand := estimatePeakDemand(profileDist, largeFraction, emaAlpha)

	largeProfiles := []string{"3g.40gb", "4g.40gb", "7g.80gb"}
	requiredSlices := 0.0

	for _, pname := range largeProfiles {
		if weight, ok := profileDist[pname]; ok {
			p, _ := profileByName(pname)
			requiredSlices += float64(weight*peakDemand) * float64(p.Size)
		}
	}

	gpuRequired := requiredSlices / float64(totalSlices)
	bufferFactor := 1.05
	optimalR := int(math.Ceil(gpuRequired*float64(totalGPUs)*bufferFactor)) + 1

	if optimalR < 1 {
		optimalR = 1
	}
	if optimalR >= totalGPUs {
		optimalR = totalGPUs - 1
	}

	return optimalR
}

func estimatePeakDemand(profileDist map[string]float64, currentFraction float64, alpha float64) float64 {
	if currentFraction < 0.99 {
		return currentFraction / (1.0 - alpha)
	}
	return currentFraction * 1.5
}

type DemandAwareSegregationPlacement struct {
	cache             *DemandCache
	metricsEnabled    bool
	abTest            *DASPABTest
	cascadeDepthLimit int
}

func NewDemandAwareSegregationPlacement() *DemandAwareSegregationPlacement {
	return &DemandAwareSegregationPlacement{
		cache:             NewDemandCache(),
		metricsEnabled:    true,
		cascadeDepthLimit: 1,
	}
}

func (d *DemandAwareSegregationPlacement) Name() string {
	return "DemandAwareSegregationPlacement"
}

func (d *DemandAwareSegregationPlacement) Select(
	gpus []GPUTopology,
	profile MIGSliceProfile,
	dist map[string]float64,
) (int, int, error) {
	startTime := time.Now()
	defer func() {
		if d.metricsEnabled {
			duration := time.Since(startTime)
			daspRuntimeOverhead.WithLabelValues("dasp").Observe(float64(duration.Nanoseconds()))
		}
	}()

	if d.abTest != nil {
		return d.abTest.Select(gpus, profile, dist)
	}

	largeFraction := computeLargeRequestFraction(dist)

	if largeFraction >= 0.40 && largeFraction <= 0.60 {
		return BestFit{}.Select(gpus, profile, dist)
	}

	nGPUs := len(gpus)
	if nGPUs == 0 {
		return -1, -1, fmt.Errorf("no GPUs available")
	}

	optimalR := calculateOptimalZoneSize(largeFraction, nGPUs, dist)

	smallZone := gpus[:optimalR]
	largeZone := gpus[optimalR:]

	var gpuIdx int
	var startSlice int
	var err error

	if IsLargeProfile(profile) {
		gpuIdx, startSlice, err = bestFitIn(largeZone, profile)

		if err != nil && gpuIdx == -1 && d.cascadeDepthLimit > 0 {
			gpuIdx, startSlice, err = bestFitIn(smallZone, profile)

			if err != nil && gpuIdx == -1 {
				return -1, -1, fmt.Errorf("no placement available after cascade")
			}

			if d.metricsEnabled {
				daspCascadeEventsTotal.WithLabelValues("level1").Inc()
			}
		}
	} else {
		gpuIdx, startSlice, err = dirtiestFitIn(smallZone, profile)

		if err != nil && gpuIdx == -1 {
			gpuIdx, startSlice, err = dirtiestFitIn(largeZone, profile)
		}
	}

	if d.metricsEnabled && gpuIdx >= 0 {
		acceptanceRate := 1.0
		daspAcceptanceRate.WithLabelValues("current").Set(acceptanceRate)
	}

	return gpuIdx, startSlice, err
}

func executePlacement(gpu *GPUTopology, startSlice int, profileName string, workloadID string) error {
	state := gpu.State
	state.mu.Lock()
	defer state.mu.Unlock()

	profile, err := profileByName(profileName)
	if err != nil {
		return err
	}

	validConstraint := false
	for _, constraint := range profile.StartConstraints {
		if constraint == startSlice {
			validConstraint = true
			break
		}
	}

	if !validConstraint {
		return fmt.Errorf("invalid start slice %d for profile %s", startSlice, profileName)
	}

	for i := startSlice; i < startSlice+profile.Size; i++ {
		if state.Slices[i] {
			return fmt.Errorf("slice %d already allocated", i)
		}
	}

	endSlice := startSlice + profile.Size
	state.Allocations[startSlice] = &Allocation{
		WorkloadID:   workloadID,
		ProfileName:  profileName,
		StartSlice:   startSlice,
		EndSlice:     endSlice,
		CreatedAt:    time.Now(),
	}

	for i := startSlice; i < endSlice; i++ {
		state.Slices[i] = true
	}

	return nil
}

func profileByName(name string) (MIGSliceProfile, error) {
	for _, p := range A100Profiles {
		if p.Name == name {
			return p, nil
		}
	}
	return MIGSliceProfile{}, fmt.Errorf("unknown profile: %s", name)
}

func firstValidStart(state *GPUState, profile MIGSliceProfile) int {
	state.mu.Lock()
	defer state.mu.Unlock()
	return firstValidStartUnlocked(state, profile)
}

func firstValidStartUnlocked(state *GPUState, profile MIGSliceProfile) int {
	for _, constraint := range profile.StartConstraints {
		if canPlaceAtUnlocked(state, constraint, profile.Size) {
			return constraint
		}
	}
	return -1
}

func canPlaceAt(state *GPUState, start, size int) bool {
	state.mu.Lock()
	defer state.mu.Unlock()
	return canPlaceAtUnlocked(state, start, size)
}

func canPlaceAtUnlocked(state *GPUState, start, size int) bool {
	if start+size > totalSlices {
		return false
	}

	for i := start; i < start+size; i++ {
		if state.Slices[i] {
			return false
		}
	}

	return true
}

func bestFitIn(gpus []GPUTopology, profile MIGSliceProfile) (int, int, error) {
	bestIdx := -1
	bestStart := -1
	minRemaining := math.MaxInt32

	for i := range gpus {
		start := firstValidStart(gpus[i].State, profile)
		if start < 0 {
			continue
		}

		remaining := countFreeSlices(gpus[i].State) - profile.Size

		if remaining < minRemaining {
			minRemaining = remaining
			bestIdx = i
			bestStart = start
		}
	}

	if bestIdx == -1 {
		return -1, -1, fmt.Errorf("no suitable GPU")
	}

	return bestIdx, bestStart, nil
}

func dirtiestFitIn(gpus []GPUTopology, profile MIGSliceProfile) (int, int, error) {
	worstIdx := -1
	worstStart := -1
	maxUtil := -1.0

	for i := range gpus {
		start := firstValidStart(gpus[i].State, profile)
		if start < 0 {
			continue
		}

		util := float64(countOccupiedSlices(gpus[i].State)) / float64(totalSlices)

		if util > maxUtil {
			maxUtil = util
			worstIdx = i
			worstStart = start
		}
	}

	if worstIdx == -1 {
		return -1, -1, fmt.Errorf("no suitable GPU")
	}

	return worstIdx, worstStart, nil
}

func countFreeSlices(state *GPUState) int {
	state.mu.Lock()
	defer state.mu.Unlock()
	return countFreeSlicesUnlocked(state)
}

func countFreeSlicesUnlocked(state *GPUState) int {
	count := 0
	for _, occupied := range state.Slices {
		if !occupied {
			count++
		}
	}
	return count
}

func countOccupiedSlices(state *GPUState) int {
	state.mu.Lock()
	defer state.mu.Unlock()
	return countOccupiedSlicesUnlocked(state)
}

func countOccupiedSlicesUnlocked(state *GPUState) int {
	count := 0
	for _, occupied := range state.Slices {
		if occupied {
			count++
		}
	}
	return count
}

type MIGScheduler struct {
	gpus               []GPUTopology
	demandDistribution map[string]float64
	cache              *DemandCache
	metricsEnabled     bool
	abTest             *DASPABTest
}

func NewMIGScheduler(gpus []GPUTopology, dist map[string]float64) *MIGScheduler {
	return &MIGScheduler{
		gpus:               gpus,
		demandDistribution: dist,
		cache:              NewDemandCache(),
		metricsEnabled:     true,
	}
}

type ScheduleResult struct {
	GPUIndex   int
	StartSlice int
	EndSlice   int
}

func (m *MIGScheduler) Schedule(workloadID, profileName string, strategy PlacementStrategy) (*ScheduleResult, error) {
	profile, err := profileByName(profileName)
	if err != nil {
		return nil, err
	}

	dist := m.demandDistribution

	gpuIdx, startSlice, err := strategy.Select(m.gpus, profile, dist)
	if err != nil || gpuIdx < 0 {
		return nil, err
	}

	err = executePlacement(&m.gpus[gpuIdx], startSlice, profileName, workloadID)
	if err != nil {
		return nil, err
	}

	return &ScheduleResult{
		GPUIndex:   gpuIdx,
		StartSlice: startSlice,
		EndSlice:   startSlice + profile.Size,
	}, nil
}

func (m *MIGScheduler) Utilization() float64 {
	totalUsed := 0
	totalCapacity := len(m.gpus) * totalSlices

	for _, gpu := range m.gpus {
		totalUsed += countOccupiedSlices(gpu.State)
	}

	if totalCapacity == 0 {
		return 0.0
	}

	return float64(totalUsed) / float64(totalCapacity)
}

func (m *MIGScheduler) ClusterFragmentation() float64 {
	totalBlocked := 0.0
	totalCapacity := float64(len(m.gpus) * totalSlices)

	for _, gpu := range m.gpus {
		for _, profile := range A100Profiles {
			blockPos := countBlockedPositions(gpu.State, profile)
			totalBlocked += float64(blockPos)
		}
	}

	if totalCapacity == 0 {
		return 0.0
	}

	return totalBlocked / totalCapacity
}

func countBlockedPositions(state *GPUState, profile MIGSliceProfile) int {
	state.mu.Lock()
	defer state.mu.Unlock()
	return countBlockedPositionsUnlocked(state, profile)
}

func countBlockedPositionsUnlocked(state *GPUState, profile MIGSliceProfile) int {
	blocked := 0
	for _, constraint := range profile.StartConstraints {
		if !canPlaceAtUnlocked(state, constraint, profile.Size) {
			blocked++
		}
	}
	return blocked
}



func deepCopyCluster(original []GPUTopology) []GPUTopology {
	copied := make([]GPUTopology, len(original))

	for i, gpu := range original {
		gpu.State.mu.Lock()
		gpuStateCopy := &GPUState{
			Slices:      make([]bool, len(gpu.State.Slices)),
			Allocations: make(map[int]*Allocation),
		}
		gpu.State.mu.Unlock()

		copy(gpuStateCopy.Slices, gpu.State.Slices)

		gpu.State.mu.Lock()
		for start, alloc := range gpu.State.Allocations {
			gpuStateCopy.Allocations[start] = &Allocation{
				WorkloadID:   alloc.WorkloadID,
				ProfileName:  alloc.ProfileName,
				StartSlice:   alloc.StartSlice,
				EndSlice:     alloc.EndSlice,
				CreatedAt:    alloc.CreatedAt,
			}
		}
		gpu.State.mu.Unlock()

		copied[i] = GPUTopology{
			Index: gpu.Index,
			State: gpuStateCopy,
		}
	}

	return copied
}

func ReleaseAllocations(gpu *GPUTopology, startSlice int, size int) error {
	gpu.State.mu.Lock()
	defer gpu.State.mu.Unlock()

	profile, err := profileByName(gpu.State.Allocations[startSlice].ProfileName)
	if err != nil {
		return err
	}

	if profile.Size != size {
		return fmt.Errorf("allocation size mismatch: expected %d, got %d", profile.Size, size)
	}

	for i := startSlice; i < startSlice+size; i++ {
		if !gpu.State.Slices[i] {
			return fmt.Errorf("slice %d was not allocated")
		}
		gpu.State.Slices[i] = false
	}

	delete(gpu.State.Allocations, startSlice)

	return nil
}

func GetAllocations(gpu *GPUTopology) []*Allocation {
	gpu.State.mu.Lock()
	defer gpu.State.mu.Unlock()

	result := make([]*Allocation, 0, len(gpu.State.Allocations))
	for _, alloc := range gpu.State.Allocations {
		clone := *alloc
		result = append(result, &clone)
	}

	return result
}

func ResetGPU(gpu *GPUTopology) {
	gpu.State.mu.Lock()
	defer gpu.State.mu.Unlock()

	for i := 0; i < len(gpu.State.Slices); i++ {
		gpu.State.Slices[i] = false
	}
	gpu.State.Allocations = make(map[int]*Allocation)
}

func GetFreeSlicesCount(gpu *GPUTopology) int {
	return countFreeSlices(gpu.State)
}

func GetOccupiedSlicesCount(gpu *GPUTopology) int {
	return countOccupiedSlices(gpu.State)
}

func CanPlaceProfile(gpu *GPUTopology, profile MIGSliceProfile) bool {
	return firstValidStart(gpu.State, profile) >= 0
}

func GetSuitableProfiles(gpu *GPUTopology) []MIGSliceProfile {
	suitable := make([]MIGSliceProfile, 0)
	for _, profile := range A100Profiles {
		if CanPlaceProfile(gpu, profile) {
			suitable = append(suitable, profile)
		}
	}
	return suitable
}

// Allocate allocates a range of slices to a workload
func (state *GPUState) Allocate(startSlice, count int) error {
	state.mu.Lock()
	defer state.mu.Unlock()

	if startSlice+count > len(state.Slices) {
		return fmt.Errorf("allocation exceeds slice boundary")
	}

	for i := startSlice; i < startSlice+count; i++ {
		if state.Slices[i] {
			return fmt.Errorf("slice %d already allocated", i)
		}
	}

	for i := startSlice; i < startSlice+count; i++ {
		state.Slices[i] = true
		allocID := fmt.Sprintf("alloc-%d", i)
		state.Allocations[i] = &Allocation{
			WorkloadID: allocID,
			StartSlice: startSlice,
			EndSlice:   startSlice + count - 1,
		}
	}

	return nil
}
