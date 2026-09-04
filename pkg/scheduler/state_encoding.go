package scheduler

import (
	"math"
)

// ============================================================================
// ENHANCED STATE REPRESENTATION FOR RL SCHEDULER
// Fixes Defect #4: Incomplete state representation missing critical features
// ============================================================================

// EnhancedState extends basic State with queue depth, memory pressure, and topology
type EnhancedState struct {
	*State // Embed base state
	
	// NEW: Per-node queue depth (pending jobs count for each node)
	QueueDepth []float64 `json:"queue_depth"`
	
	// NEW: Per-node memory fragmentation ratio (0.0 = no fragmentation, 1.0 = fully fragmented)
	MemoryPressure []float64 `json:"memory_pressure"`
	
	// NEW: GPU topology as adjacency matrix flattened (num_nodes x num_nodes)
	GPUTopologyMatrix [][]float64 `json:"gpu_topology_matrix"`
	
	// NEW: Global cluster contention indicator (average wait time across all nodes)
	ClusterPressure float64 `json:"cluster_pressure"`
}

// NewEnhancedState creates enhanced state from base state
func NewEnhancedState(base *State, queueDepth []float64, memoryPressure []float64, gpuTopology [][]float64, clusterPressure float64) *EnhancedState {
	return &EnhancedState{
		State:           base,
		QueueDepth:      queueDepth,
		MemoryPressure:  memoryPressure,
		GPUTopologyMatrix: gpuTopology,
		ClusterPressure: clusterPressure,
	}
}

// EncodeToFeatures converts enhanced state to feature vector for neural network input
func (s *EnhancedState) EncodeToFeatures(inputDim int) []float64 {
	features := make([]float64, 0, inputDim)
	
	// Add base features first
	baseFeatures := s.encodeBaseState()
	features = append(features, baseFeatures...)
	
	// Add queue depth features
	for _, qd := range s.QueueDepth {
		features = append(features, normalizeFloat(qd, 0.0, 100.0))
	}
	
	// Add memory pressure features
	for _, mp := range s.MemoryPressure {
		features = append(features, math.Abs(mp)) // Already in [0, 1] range
	}
	
	// Add GPU topology features (flatten adjacency matrix)
	for _, row := range s.GPUTopologyMatrix {
		for _, val := range row {
			features = append(features, val) // Already normalized edge weights
		}
	}
	
	// Add cluster pressure
	features = append(features, normalizeFloat(s.ClusterPressure, 0.0, 100.0))
	
	// Pad or truncate to fixed input dimension
	if len(features) < inputDim {
		padding := make([]float64, inputDim-len(features))
		features = append(features, padding...)
	} else if len(features) > inputDim {
		features = features[:inputDim]
	}
	
	return features
}

// encodeBaseState encodes the original State fields (copied from deep_rl_optimizer.go)
func (s *EnhancedState) encodeBaseState() []float64 {
	features := make([]float64, 0, 50)
	
	// Node features
	features = append(features, s.State.NodeFeatures...)
	
	// GPU features
	features = append(features, s.State.GPUFeatures...)
	
	// NVLink features
	features = append(features, s.State.NVLinkFeatures...)
	
	// Queue features
	for _, req := range s.State.RequestQueue {
		features = append(features, req.Priority, float64(req.GPUCount), float64(req.MemoryRequired))
	}
	
	// Aggregate features
	features = append(features, s.State.CurrentLoad, s.State.AvgWaitTime, s.State.EnergyEfficiency, s.State.CostFactor)
	
	// Contextual features
	features = append(features, s.State.TimeOfDay, s.State.DayOfWeek, mathBoolToFloat64(s.State.BusinessHour))
	
	// Pad to expected size
	for len(features) < 50 {
		features = append(features, 0.0)
	}
	
	return normalizeFeatures(features[:50])
}

// normalizeFloat normalizes a value to [0, 1] range given min and max
func normalizeFloat(value, minVal, maxVal float64) float64 {
	if maxVal == minVal {
		return 0.0
	}
	return (value - minVal) / (maxVal - minVal)
}

// ComputeClusterPressure calculates global contention from per-node metrics
// avgQueueDepth: average pending jobs across all nodes
// avgMemoryUtilization: average memory usage percentage
// avgWaitTime: average job wait time across all nodes
func ComputeClusterPressure(avgQueueDepth, avgMemoryUtilization, avgWaitTime float64) float64 {
	// Weighted combination: queue depth (50%), memory utilization (30%), wait time (20%)
	queueScore := normalizeFloat(avgQueueDepth, 0.0, 100.0)
	memoryScore := normalizeFloat(avgMemoryUtilization, 0.0, 100.0)
	waitScore := normalizeFloat(avgWaitTime, 0.0, 300.0) // Scale wait time to [0, 1]
	
	score := 0.5*queueScore + 0.3*memoryScore + 0.2*waitScore
	return math.Min(1.0, math.Max(0.0, score))
}

// computeGPUTopologyMatrix constructs GPU adjacency matrix from NVLink distances
// nvlinkDistances: map[node1][node2] = distance in mm or bandwidth inverse
func computeGPUTopologyMatrix(nvlinkDistances map[int]map[int]float64, numNodes int) [][]float64 {
	matrix := make([][]float64, numNodes)
	
	for i := 0; i < numNodes; i++ {
		matrix[i] = make([]float64, numNodes)
		
		for j := 0; j < numNodes; j++ {
			if i == j {
				matrix[i][j] = 0.0 // Self-distance is zero
			} else if dist, ok := nvlinkDistances[i][j]; ok {
				// Normalize distance: closer GPUs get higher values (inverse)
				if dist > 0 {
					matrix[i][j] = math.Exp(-dist/100.0) // Exponential decay scale
				} else {
					matrix[i][j] = 1.0 // Same node or unknown distance
				}
			} else {
				matrix[i][j] = 0.0 // No connection
			}
		}
	}
	
	return matrix
}
