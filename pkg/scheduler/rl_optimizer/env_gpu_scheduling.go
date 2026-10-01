package rl_optimizer

import (
    "context"
    
    "github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler/rl_optimizer"
)

// GpuEnvironment implements OpenAI Gym-style environment for GPU scheduling
// Used by M10 DQN agent for real training (not simulation!)
type GpuEnvironment struct {
    stateSpace StateSpace
    actionSpace ActionSpace
    
    // Current state
    currentTopology TopologyGraph
    currentLoad LoadProfile
    
    // Training metrics
    episodeCount int
    totalReward float64
}

// NewGpuEnvironment creates a new GPU scheduling environment
func NewGpuEnvironment() *GpuEnvironment {
    return &GpuEnvironment{
        stateSpace: NewStateSpace(),
        actionSpace: NewActionSpace(),
    }
}

// StateSpace returns the state space dimensions
func (e *GpuEnvironment) StateSpace() StateSpace {
    return e.stateSpace
}

// ActionSpace returns the action space dimensions
func (e *GpuEnvironment) ActionSpace() ActionSpace {
    return e.actionSpace
}

// Reset resets the environment to initial state
func (e *GpuEnvironment) Reset(ctx context.Context) {
    // Initialize random topology
    e.currentTopology = MockTopologyGraph(8) // 8 GPUs
    
    // Initialize random load profile
    e.currentLoad = RandomLoadProfile()
    
    e.episodeCount++
    e.totalReward = 0
}

// Step executes an action and returns reward
func (e *GpuEnvironment) Step(action Action) (float64, bool, error) {
    // Evaluate action quality
    reward := e.evaluatePlacement(action)
    
    // Update state based on action
    e.updateTopology(action)
    e.updateLoad(action)
    
    // Check if episode is complete
    done := e.isEpisodeComplete()
    
    e.totalReward += reward
    return reward, done, nil
}

// evaluatePlacement computes placement reward
func (e *GpuEnvironment) evaluatePlacement(action Action) float64 {
    reward := 0.0
    
    // Reward for good utilization balance
    utilScore := e.calculateUtilizationScore()
    reward += utilScore * 0.3
    
    // Reward for minimizing fragmentation
    fragScore := e.calculateFragmentationScore()
    reward += fragScore * 0.2
    
    // Reward for respecting NVLink topology
    topoScore := e.calculateTopoScore()
    reward += topoScore * 0.5
    
    return reward
}

// calculateUtilizationScore computes utilization balance score
func (e *GpuEnvironment) calculateUtilizationScore() float64 {
    // Ideal: all GPUs at similar utilization
    // Penalty for imbalance
    return 1.0 - e.calculateUtilizationVariance()
}

// calculateUtilizationVariance computes variance in GPU utilization
func (e *GpuEnvironment) calculateUtilizationVariance() float64 {
    // Calculate mean utilization
    sum := 0.0
    count := 0.0
    
    for _, gpu := range e.currentTopology.GPUs() {
        sum += gpu.CurrentUtilization()
        count++
    }
    
    if count == 0 {
        return 0
    }
    
    mean := sum / count
    
    // Calculate variance
    varianceSum := 0.0
    for _, gpu := range e.currentTopology.GPUs() {
        diff := gpu.CurrentUtilization() - mean
        varianceSum += diff * diff
    }
    
    return varianceSum / count
}

// calculateFragmentationScore computes fragmentation penalty/reward
func (e *GpuEnvironment) calculateFragmentationScore() float64 {
    // Lower fragmentation = higher score
    fragRatio := e.calculateFragmentationRatio()
    return 1.0 - fragRatio
}

// calculateFragmentationRatio computes ratio of fragmented resources
func (e *GpuEnvironment) calculateFragmentationRatio() float64 {
    totalCapacity := 0.0
    usableCapacity := 0.0
    
    for _, gpu := range e.currentTopology.GPUs() {
        totalCapacity += gpu.TotalMemory()
        usableCapacity += gpu.UsableMemory()
    }
    
    if totalCapacity == 0 {
        return 0
    }
    
    return 1.0 - (usableCapacity / totalCapacity)
}

// calculateTopoScore computes topology-aware scheduling score
func (e *GpuEnvironment) calculateTopoScore() float64 {
    // Higher score for placing workloads on same NVLink domain
    score := 0.0
    
    for _, workload := range e.currentLoad.Workloads() {
        bestNVLinkDomain := e.findBestNVLinkDomain(workload)
        if bestNVLinkDomain != "" {
            score += 0.1 // Bonus for NVLink-local placement
        }
    }
    
    return score
}

// findBestNVLinkDomain finds the best NVLink domain for a workload
func (e *GpuEnvironment) findBestNVLinkDomain(workload Workload) string {
    bestDomain := ""
    bestScore := -1.0
    
    for _, gpu := range e.currentTopology.GPUs() {
        if gpu.AvailableMemory() >= workload.RequiredMemory() {
            nvLinkScore := e.calculateNVLINKScore(gpu, workload)
            if nvLinkScore > bestScore {
                bestScore = nvLinkScore
                bestDomain = gpu.NVLINKDomain()
            }
        }
    }
    
    return bestDomain
}

// calculateNVLINKScore computes NVLink compatibility score
func (e *GpuEnvironment) calculateNVLINKScore(gpu GPU, workload Workload) float64 {
    // Score based on whether workload can fit in NVLink-local region
    if gpu.NVLINKDomain() == workload.PreferredNVLinkDomain() {
        return 1.0 // Perfect match
    }
    
    if gpu.SupportsWorkloadType(workload.Type()) {
        return 0.5 // Compatible but not preferred
    }
    
    return 0.0 // Incompatible
}

// updateTopology updates topology after action
func (e *GpuEnvironment) updateTopology(action Action) {
    // TODO: Implement topology update logic
}

// updateLoad updates load profile after action
func (e *GpuEnvironment) updateLoad(action Action) {
    // TODO: Implement load update logic
}

// isEpisodeComplete checks if episode should terminate
func (e *GpuEnvironment) isEpisodeComplete() bool {
    // Episode ends when max steps reached or all workloads placed
    return e.episodeCount >= MAX_EPISODE_STEPS || e.allWorkloadsPlaced()
}

// allWorkloadsPlaced checks if all workloads have been placed
func (e *GpuEnvironment) allWorkloadsPlaced() bool {
    // TODO: Implement workload completion check
    return len(e.currentLoad.Workloads()) == 0
}

// GetRewardHistory returns reward history for training analysis
func (e *GpuEnvironment) GetRewardHistory() []float64 {
    return []float64{e.totalReward}
}

// ExportState exports current environment state for debugging
func (e *GpuEnvironment) ExportState() EnvironmentState {
    return EnvironmentState{
        Topology: e.currentTopology.Export(),
        Load: e.currentLoad.Export(),
        Episode: e.episodeCount,
        TotalReward: e.totalReward,
    }
}

// Max episodes per training run
const MAX_EPISODE_STEPS = 1000
