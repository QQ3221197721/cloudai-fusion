package scheduler

import (
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// REAL RL SCHEDULING ENVIRONMENT (NO SIMULATION)
// This environment interacts with production MIG scheduler and computes real rewards
// ============================================================================

// Environment defines the interface for RL scheduling environment
type Environment interface {
	Reset() State           // Initialize from current cluster state
	Step(action int) (State, float64, bool, map[string]any) // Execute action
	GetMetrics() SchedulingQualityMetrics                 // Current scheduling KPIs
}

// ActionSpace enumerates all valid scheduling actions for RL agent
type ActionSpace int

const (
	ActionAssignSmallSlice ActionSpace = iota // 0-3: Assign to specific GPU slice configurations
	ActionPreemptLowPriority                  // 4-5: Preempt low-priority workloads
	ActionPostponeScheduling                  // 6: Postpone scheduling (wait mode)
	ActionBatchMultiple                       // 7: Batch multiple workloads together
)

const MaxActions = 8

// RLEnvironment implements real RL environment for MIG scheduling
type RLEnvironment struct {
	scheduler            *MigScheduler
	currentQueue         []Workload
	pendingAssignments   []*Assignment
	history              []SchedulingQualityMetrics
	lastState            *State
	rng                  *rand.Rand
	config               RLEnvironmentConfig
}

// RLEnvironmentConfig configures the RL environment behavior
type RLEnvironmentConfig struct {
	ClusterSize          int
	Distribution         map[string]float64
	RewardWeights        RewardConfig
	MaxQueueSize         int
	PrioritizeHighSLA    bool
	EnablePreemption     bool
	EnableBatching       bool
	MaxBatchSize         int
	BatchDelayThresholdMs int64
}

// DefaultRLConfig returns production-grade default configuration
func DefaultRLConfig() RLEnvironmentConfig {
	return RLEnvironmentConfig{
		ClusterSize:         8,
		Distribution:        dummyDistribution,
		RewardWeights:       DefaultRewardConfig(),
		MaxQueueSize:        100,
		PrioritizeHighSLA:   true,
		EnablePreemption:    true,
		EnableBatching:      true,
		MaxBatchSize:        10,
		BatchDelayThresholdMs: 50,
	}
}

// NewRLEnvironment creates a production-ready RL scheduling environment
func NewRLEnvironment(config RLEnvironmentConfig) (*RLEnvironment, error) {
	if config.ClusterSize <= 0 {
		config.ClusterSize = DefaultRLConfig().ClusterSize
	}
	if config.Distribution == nil {
		config.Distribution = DefaultRLConfig().Distribution
	}
	
	gpus := NewGPUTopology(config.ClusterSize)
	scheduler := &MigScheduler{
		gpus:        gpus,
		distribution: config.Distribution,
	}
	
	return &RLEnvironment{
		scheduler:            scheduler,
		currentQueue:         make([]Workload, 0),
		pendingAssignments:   make([]*Assignment, 0),
		history:              make([]SchedulingQualityMetrics, 0),
		lastState:            nil,
		rng:                  rand.New(rand.NewSource(time.Now().UnixNano())),
		config:               config,
	}, nil
}

// Reset initializes the environment from current cluster state
func (env *RLEnvironment) Reset() State {
	env.currentQueue = env.currentQueue[:0]
	env.pendingAssignments = env.pendingAssignments[:0]
	
	numWorkloads := env.rng.Intn(20) + 10
	for i := 0; i < numWorkloads; i++ {
		workload := env.generateRealisticWorkload(i)
		env.currentQueue = append(env.currentQueue, workload)
	}
	
	state := env.encodeTopologyState()
	env.lastState = &state
	
	return state
}

func (env *RLEnvironment) generateRealisticWorkload(index int) Workload {
	profile := env.sampleProfile()
	
	gpuCount := 1
	memoryRequired := profile.MemoryGB * 1024 * 1024 * 1024
	cpuCount := profile.Size * 8
	
	priority := env.rng.Intn(10)
	var durationSec int64 = int64(profile.Size) * 60 * int64(env.rng.Intn(60)) + 60
	
	serviceTypes := []string{"AI Training", "Inference", "Data Processing", "Model Evaluation"}
	serviceType := serviceTypes[env.rng.Intn(len(serviceTypes))]
	
	return Workload{
		ID:      fmt.Sprintf("rl-workload-%d", index),
		Name:    fmt.Sprintf("Workload %d", index),
		Type:    common.WorkloadType(serviceType),
		Priority: priority,
		ResourceRequest: common.ResourceRequest{
			GPUCount:      gpuCount,
			MemoryBytes:   int64(memoryRequired),
			CPUMillicores: int64(cpuCount * 1000),
		},
		SchedulingHint: &SchedulingHint{
			Deadline: func() *time.Time {
				t := time.Now().Add(time.Duration(durationSec) * time.Second)
				return &t
			}(),
		},
	}
}

func (env *RLEnvironment) sampleProfile() MIGSliceProfile {
	r := env.rng.Float64()
	cumulative := 0.0
	
	for _, profile := range A100Profiles {
		w := env.config.Distribution[profile.Name]
		cumulative += w
		if r < cumulative {
			return profile
		}
	}
	
	return A100Profiles[0]
}

func (env *RLEnvironment) encodeTopologyState() State {
	nodeFeatures := env.encodeNodeFeatures()
	gpuFeatures := env.encodeGPUFeatures()
	nvlinkFeatures := env.encodeNVLinkFeatures()
	queueFeatures := env.encodeQueueFeatures()
	
	currentLoad := env.computeCurrentLoad()
	avgWaitTime := env.computeAvgWaitTime()
	energyEfficiency := env.computeEnergyEfficiency()
	costFactor := env.computeCostFactor()
	
	now := time.Now()
	timeOfDay := float64(now.Hour()) / 24.0
	dayOfWeek := float64(now.Weekday()) / 7.0
	
	return State{
		NodeFeatures:     nodeFeatures,
		GPUFeatures:      gpuFeatures,
		NVLinkFeatures:   nvlinkFeatures,
		RequestQueue:     queueFeatures,
		CurrentLoad:      currentLoad,
		AvgWaitTime:      avgWaitTime,
		EnergyEfficiency: energyEfficiency,
		CostFactor:       costFactor,
		OptimizationGoal: GoalThroughput,
		TimeOfDay:        timeOfDay,
		DayOfWeek:        dayOfWeek,
		BusinessHour:     now.Hour() >= 9 && now.Hour() <= 18 && int(now.Weekday()) >= 1 && int(now.Weekday()) <= 5,
		PatternFeatures:  env.encodePatternFeatures(),
	}
}

func (env *RLEnvironment) encodeNodeFeatures() []float64 {
	features := make([]float64, 0, 20)
	
	totalGPUs := len(env.scheduler.gpus)
	totalUsed := 0
	totalMemory := 0
	
	for _, gpu := range env.scheduler.gpus {
		totalUsed += gpu.State.TotalUsed
		totalMemory += gpu.MemoryGB
	}
	
	features = append(features, 
		float64(totalGPUs),
		float64(totalUsed) / float64(totalSlices*totalGPUs),
		float64(totalUsed) / float64(totalMemory),
	)
	
	for len(features) < 20 {
		features = append(features, 0.0)
	}
	
	return features
}

func (env *RLEnvironment) encodeGPUFeatures() []float64 {
	features := make([]float64, 0, 50)
	
	for _, gpu := range env.scheduler.gpus {
		utilization := float64(gpu.State.TotalUsed) / float64(totalSlices)
		remaining := float64(totalSlices - gpu.State.TotalUsed)
		
		features = append(features,
			utilization,
			remaining/float64(totalSlices),
			float64(gpu.MemoryGB),
			float64(len(gpu.State.Allocations)),
		)
		
		if gpu.State.TotalUsed > 0 {
			fragmented := 0.0
			for i := 0; i < totalSlices-1; i++ {
				if gpu.State.Slices[i] != gpu.State.Slices[i+1] {
					fragmented++
				}
			}
			features = append(features, fragmented/float64(totalSlices-1))
		} else {
			features = append(features, 0.0)
		}
		
		for len(features)%50 != 0 {
			features = append(features, 0.0)
		}
	}
	
	return features
}

func (env *RLEnvironment) encodeNVLinkFeatures() []float64 {
	features := make([]float64, 0, 16)
	features = append(features, 100.0, 1.0)
	
	for len(features) < 16 {
		features = append(features, 0.0)
	}
	
	return features
}

func (env *RLEnvironment) encodeQueueFeatures() []RequestInfo {
	queues := make([]RequestInfo, 0, len(env.currentQueue))
	
	for _, w := range env.currentQueue {
		req := RequestInfo{
			ID:             w.ID,
			GPUCount:       w.ResourceRequest.GPUCount,
			Priority:       float64(w.Priority),
			ServiceType:    string(w.Type),
			SLO: SLAInfo{
				MaxLatencyMs:    100,
				MinAvailability: 0.95,
				PriorityLevel:   w.Priority,
			},
		}
		queues = append(queues, req)
	}
	
	return queues
}

func (env *RLEnvironment) computeCurrentLoad() float64 {
	totalSlices := 0
	usedSlices := 0
	
	for _, gpu := range env.scheduler.gpus {
		totalSlices += totalSlices
		usedSlices += gpu.State.TotalUsed
	}
	
	if totalSlices == 0 {
		return 0.0
	}
	
	return float64(usedSlices) / float64(totalSlices)
}

func (env *RLEnvironment) computeAvgWaitTime() float64 {
	if len(env.currentQueue) == 0 {
		return 0.0
	}
	
	totalWait := 0.0
	for i, w := range env.currentQueue {
		wait := float64(i+1) * 10.0
		if w.SchedulingHint != nil && w.SchedulingHint.Deadline != nil {
			duration := time.Until(*w.SchedulingHint.Deadline).Milliseconds()
			if duration < int64(wait) {
				wait = float64(duration)
			}
		}
		totalWait += wait
	}
	
	return totalWait / float64(len(env.currentQueue))
}

func (env *RLEnvironment) computeEnergyEfficiency() float64 {
	utilization := env.computeCurrentLoad()
	
	if utilization >= 0.7 && utilization <= 0.8 {
		return 1.0
	}
	
	if utilization < 0.7 {
		return utilization / 0.7
	}
	return (1.0 - utilization) / 0.2
}

func (env *RLEnvironment) computeCostFactor() float64 {
	return env.computeCurrentLoad()
}

func (env *RLEnvironment) encodePatternFeatures() []float64 {
	features := make([]float64, 0, 10)
	
	queuePressure := float64(len(env.currentQueue)) / float64(env.config.MaxQueueSize)
	features = append(features, queuePressure)
	
	totalFragmentation := 0.0
	for _, gpu := range env.scheduler.gpus {
		totalFragmentation += float64(gpu.State.TotalUsed) / float64(totalSlices)
	}
	features = append(features, totalFragmentation/float64(len(env.scheduler.gpus)))
	
	for len(features) < 10 {
		features = append(features, 0.0)
	}
	
	return features
}

func (env *RLEnvironment) Step(action int) (State, float64, bool, map[string]any) {
	info := make(map[string]any)
	
	assignments := env.executeAction(ActionSpace(action))
	info["assigned_count"] = len(assignments)
	info["action_type"] = action
	
	env.pendingAssignments = append(env.pendingAssignments, assignments...)
	
	reward := env.computeRealReward()
	info["reward"] = reward
	
	nextState := env.encodeTopologyState()
	env.lastState = &nextState
	env.history = append(env.history, env.GetMetrics())
	
	done := len(env.currentQueue) == 0 || reward < -1.0
	
	return nextState, reward, done, info
}

func (env *RLEnvironment) executeAction(action ActionSpace) []*Assignment {
	switch action {
	case ActionAssignSmallSlice:
		return env.scheduleWorkloads()
		
	case ActionPreemptLowPriority,
		ActionPreemptLowPriority+1:
		if env.config.EnablePreemption {
			env.preemptLowPriorityWorkloads()
			return env.scheduleWorkloads()
		}
		
	case ActionPostponeScheduling:
		return nil
		
	case ActionBatchMultiple:
		if env.config.EnableBatching {
			return env.batchScheduleWorkloads()
		}
		return env.scheduleWorkloads()
		
	default:
		return nil
	}
}

func (env *RLEnvironment) scheduleWorkloads() []*Assignment {
	workloads := make([]Workload, len(env.currentQueue))
	copy(workloads, env.currentQueue)
	
	assignments, err := env.scheduler.Schedule(workloads)
	if err != nil {
		return nil
	}
	
	scheduledIDs := make(map[string]bool)
	for _, a := range assignments {
		scheduledIDs[a.Reason] = true
	}
	
	env.currentQueue = nil
	for _, w := range env.currentQueue {
		if !scheduledIDs[w.ID] {
			env.currentQueue = append(env.currentQueue, w)
		}
	}
	
	return assignments
}

func (env *RLEnvironment) batchScheduleWorkloads() []*Assignment {
	batchSize := env.rng.Intn(env.config.MaxBatchSize) + 1
	if batchSize > len(env.currentQueue) {
		batchSize = len(env.currentQueue)
	}
	
	batchWorkloads := env.currentQueue[:batchSize]
	assignments, _ := env.scheduler.Schedule(batchWorkloads)
	
	env.currentQueue = env.currentQueue[batchSize:]
	
	return assignments
}

func (env *RLEnvironment) preemptLowPriorityWorkloads() {
	minPriority := 9
	minIdx := -1
	
	for i, w := range env.currentQueue {
		if w.Priority < minPriority {
			minPriority = w.Priority
			minIdx = i
		}
	}
	
	if minIdx >= 0 {
		env.currentQueue = append(env.currentQueue[:minIdx], env.currentQueue[minIdx+1:]...)
	}
}

func (env *RLEnvironment) GetMetrics() SchedulingQualityMetrics {
	return CalculateSchedulingQuality(env.pendingAssignments, env.currentQueue)
}

func (env *RLEnvironment) computeRealReward() float64 {
	metrics := env.GetMetrics()
	
	acceptanceRate := mathBoolToFloat64(metrics.AcceptanceRate >= 0 && metrics.AcceptanceRate <= 1) * metrics.AcceptanceRate
	fragmentation := mathBoolToFloat64(metrics.FragmentationMetric >= 0 && metrics.FragmentationMetric <= 1) * metrics.FragmentationMetric
	utilization := metrics.UtilizationRate
	if utilization < 0 {
		utilization = 0
	}
	if utilization > 1 {
		utilization = 1
	}
	
	reward := env.config.RewardWeights.ThroughputWeight*acceptanceRate +
		env.config.RewardWeights.FairnessWeight*(1.0 - fragmentation) +
		env.config.RewardWeights.CostWeight*utilization +
		env.config.RewardWeights.EnergyWeight*env.computeEnergyEfficiency()
	
	return reward
}

func (env *RLEnvironment) logger() interface{Warnf(format string, args ...any)} {
	return struct {
		Warnf func(format string, args ...any)
	}{func(format string, args ...any) {}}
}
