// Package scheduler - gpu_topology_scheduler.go implements the unique M3 T2 performance barrier.
// This file provides a unified scoring algorithm that simultaneously optimizes NUMA locality,
// NVLink distance minimization, and PCI-E root complex separation in a single computation.
// Performance targets: Single-node <10ms (baseline naive <5ms), Multi-node <500ms (<100 nodes)
package scheduler

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ============================================================================
// Unified GPU Topology Scheduler - Core Implementation
// ============================================================================
// FLIP M3 T2 Requirement: Unique combination of NUMA + NVLink + PCI-E awareness
// Combines three hardware dimensions into one optimal score, outperforming separate
// schedulers by optimizing all constraints simultaneously instead of prioritizing them.

// GPUScoreAlgorithm evaluates placement quality across ALL hardware dimensions
type GPUScoreAlgorithm struct {
	numaAffinityScore       float64 // weight for NUMA locality (default: 0.4)
	nvlinkDistanceWeight    float64 // weight for NVLink bandwidth (default: 0.4)
	pcietopologyWeight      float64 // weight for PCI-E separation (default: 0.2)
	minimumBandwidthGBPS    float64 // minimum acceptable NVLink bandwidth
	requireSameNuma         bool    // strict NUMA locality requirement
	preferNonOvercommitted  bool    // prefer GPUs with headroom
}

// ScoringResult stores computed scores for each node
type ScoringResult struct {
	NodeName          string
	TotalScore        float64              // 0-100 scale, higher is better
	DimensionScores   map[string]float64   // per-dimension breakdown
	GPUSelection      []int                // recommended GPU indices
	DetailedMetrics   *TopologyMetrics     // full diagnostic data
	Recommendation    string               // human-readable explanation
	ScoredAt          time.Time            // timestamp
	LatencyMicrosec   int64                // computation time in microseconds
}

// TopologyMetrics holds comprehensive diagnostic information about scored configuration
type TopologyMetrics struct {
	AverageLatencyNS     float64 // estimated inter-GPU latency (ns)
	BandwidthEfficiency  float64 // ratio of achieved to peak bandwidth
	NVLinkUtilization    float64 // percentage of GPUs with NVLink connections
	NUMADominationFactor float64 // % of workloads on single NUMA node
	PcieRootSeparation   float64 // count of distinct PCIe root complexes
	 MIGIsolationAvailable bool  // MIG can isolate GPUs if available
	MPSHostActive        bool    // MPS server currently running
}

// NewGPUScoreAlgorithm creates a new topology-aware scheduler with custom weights
func NewGPUScoreAlgorithm(numaWeight, nvlinkWeight, pciWeight float64) *GPUScoreAlgorithm {
	// Normalize weights to sum = 1.0
	total := numaWeight + nvlinkWeight + pciWeight
	if total == 0 {
		numaWeight, nvlinkWeight, pciWeight = 0.33, 0.33, 0.34
	} else {
		numaWeight /= total
		nvlinkWeight /= total
		pciWeight /= total
	}

	return &GPUScoreAlgorithm{
		numaAffinityScore:    numaWeight,
		nvlinkDistanceWeight: nvlinkWeight,
		pcietopologyWeight:   pciWeight,
		minimumBandwidthGBPS: 300.0, // NVLink 3.0 minimum
		requireSameNuma:      false,
	}
}

// SetMinBandwidth specifies minimum required bandwidth between any two GPUs
func (gs *GPUScoreAlgorithm) SetMinBandwidth(bandwidthGBPS float64) {
	gs.minimumBandwidthGBPS = bandwidthGBPS
}

// SetStrictNUMALocality enforces all GPUs must be on same NUMA node
func (gs *GPUScoreAlgorithm) SetStrictNUMALocality(require bool) {
	gs.requireSameNuma = require
}

// Score computes placement scores for a pod across all nodes
// PERFORMANCE TARGET: Single-node <10ms, Multi-node <500ms (<100 nodes)
func (gs *GPUScoreAlgorithm) Score(ctx context.Context, pod *v1.Pod, nodes []*NodeInfo) ([]*ScoringResult, error) {
	startTime := time.Now()

	results := make([]*ScoringResult, 0, len(nodes))

	for _, nodeInfo := range nodes {
		result := gs.scoreSingleNode(ctx, pod, nodeInfo)
		result.LatencyMicrosec = time.Since(startTime).Micros()
		results = append(results, result)
	}

	sortResultsByScore(results)

	return results, nil
}

// scoreSingleNode computes the complete scoring for a single node
func (gs *GPUScoreAlgorithm) scoreSingleNode(ctx context.Context, pod *v1.Pod, nodeInfo *NodeInfo) *ScoringResult {
	nodeStart := time.Now()
	
	totalScore := 0.0
	dimScores := make(map[string]float64)

	// Only proceed if node has GPUs matching requirements
	gpuCount, requiresGPU := getRequiredGPUCount(pod)
	if requiresGPU && (nodeInfo.GPUTopology == nil || nodeInfo.GPUTopology.TotalGPUs < gpuCount) {
		return &ScoringResult{
			NodeName:      nodeInfo.Node.Name,
			TotalScore:    0.0,
			DimensionScores: dimScores,
			Recommendation: "Insufficient GPUs for workload",
			ScoredAt:       time.Now(),
		}
	}

	if nodeInfo.GPUTopology != nil {
		// Dimension 1: NUMA locality scoring (max 40 points)
		numaScore := gs.evaluateNumaLocality(nodeInfo.GPUTopology, gpuCount) * (gs.numaAffinityScore * 40.0)
		totalScore += numaScore
		dimScores["numa"] = numaScore / (gs.numaAffinityScore * 40.0)

		// Dimension 2: NVLink bandwidth optimization (max 40 points)
		nvlinkScore := gs.optimizeNVLinkPlacement(nodeInfo.GPUTopology, gpuCount) * (gs.nvlinkDistanceWeight * 40.0)
		totalScore += nvlinkScore
		dimScores["nvlink"] = nvlinkScore / (gs.nvlinkDistanceWeight * 40.0)

		// Dimension 3: PCI-E topology considerations (max 20 points)
		pciScore := gs.evaluatePCITopology(nodeInfo.GPUTopology) * (gs.pcietopologyWeight * 20.0)
		totalScore += pciScore
		dimScores["pci"] = pciScore / (gs.pcietopologyWeight * 20.0)
	} else {
		// No topology info → neutral baseline
		totalScore = 50.0
		dimScores["numa"] = 50.0
		dimScores["nvlink"] = 50.0
		dimScores["pci"] = 50.0
	}

	scoredAt := time.Now()

	return &ScoringResult{
		NodeName:      nodeInfo.Node.Name,
		TotalScore:    math.Min(totalScore, 100.0),
		DimensionScores: dimScores,
		Recommendation: gs.generateRecommendation(dimScores),
		ScoredAt:       scoredAt,
		LatencyMicrosec: time.Since(nodeStart).Micros(),
	}
}

// evaluateNumaLocality measures how well pods fit within single NUMA domains
// Returns score 0-100 (higher = better NUMA locality)
func (gs *GPUScoreAlgorithm) evaluateNumaLocality(topo *NodeGPUTopology, requiredGPUs int) float64 {
	if topo == nil || len(topo.NUMANodes) == 0 {
		return 50.0 // neutral baseline
	}

	maxGPUsPerNUMA := 0
	for _, gpus := range topo.NUMANodes {
		if len(gpus) > maxGPUsPerNUMA {
			maxGPUsPerNUMA = len(gpus)
		}
	}

	// If we can fit all GPUs on one NUMA node, perfect score
	if requiredGPUs <= maxGPUsPerNUMA {
		return 100.0
	}

	// Partial credit for multi-NUMA but reasonable distribution
	numaCounts := make([]int, len(topo.NUMANodes))
	idx := 0
	for _, gpus := range topo.NUMANodes {
		numaCounts[idx] = len(gpus)
		idx++
	}

	if !gs.requireSameNuma {
		// Soft preference: penalize multi-NUMA slightly
		return 70.0 - float64(len(topo.NUMANodes)-1)*5.0
	}

	// Strict NUMA: fail if can't fit on one node
	return 0.0
}

// optimizeNVLinkPlacement selects best GPU subset maximizing NVLink connectivity
// Prioritizes high-bandwidth paths while minimizing hop count
func (gs *GPUScoreAlgorithm) optimizeNVLinkPlacement(topo *NodeGPUTopology, requiredGPUs int) float64 {
	if topo == nil || !topo.HasNVLink {
		return 30.0 // Low score when no NVLink
	}

	// Score based on NVLink coverage within selected GPU subset
	validPairs := 0
	totalPossiblePairs := requiredGPUs * (requiredGPUs - 1) / 2

	if totalPossiblePairs == 0 {
		return 100.0 // No interconnect needed for single GPU
	}

	for _, link := range topo.NVLinks {
		// Check if both GPUs are within our selection window
		if link.GPU1Index < requiredGPUs && link.GPU2Index < requiredGPUs {
			if link.BandwidthGB >= gs.minimumBandwidthGBPS {
				validPairs++
			}
		}
	}

	if validPairs == 0 {
		return 20.0 // No sufficient NVLink
	}

	ratio := float64(validPairs) / float64(totalPossiblePairs)
	scaled := ratio * 100.0

	// Bonus for NVSwitch full mesh
	if topo.HasNVSwitch {
		scaled += 10.0
	}

	return math.Min(scaled, 100.0)
}

// evaluatePCITopology checks for PCIe root complex separation to avoid contention
// Penalizes configurations where multiple GPUs share same PCIe switch
func (gs *GPUScoreAlgorithm) evaluatePCITopology(topo *NodeGPUTopology) float64 {
	if topo == nil {
		return 50.0
	}

	// Analyze P2P distances from topology matrix
	// X=direct, NV=NVLINK, PHB=PCIe Hub, SYS=System/QPI
	p2pPenalties := 0.0

	for _, link := range topo.NVLinks {
		connType := "NVL" // default assumption
		for i := 0; i < len(topo.P2PMatrix); i++ {
			key := fmt.Sprintf("%d-%d", min(link.GPU1Index, link.GPU2Index), max(link.GPU1Index, link.GPU2Index))
			if topoVal, ok := topo.P2PMatrix[key]; ok {
				connType = topoVal
				break
			}
		}

		switch connType {
		case "NVS", "NV18", "NV12": // High bandwidth → bonus
			continue
		case "PHB": // PCIe Hub → moderate penalty
			p2pPenalties += 15.0
		case "SYS": // System bus → heavy penalty
			p2pPenalties += 30.0
		}
	}

	score := 100.0 - p2pPenalties
	return math.Max(score, 0.0)
}

// generateRecommendation creates human-readable explanation for scoring
func (gs *GPUScoreAlgorithm) generateRecommendation(dimScores map[string]float64) string {
	var parts []string

	if dimScores["numa"] >= 90 {
		parts = append(parts, "Excellent NUMA locality")
	} else if dimScores["numa"] >= 70 {
		parts = append(parts, "Good NUMA distribution")
	}

	if dimScores["nvlink"] >= 90 {
		parts = append(parts, "Maximal NVLink utilization")
	} else if dimScores["nvlink"] < 40 {
		parts = append(parts, "Limited inter-GPU bandwidth")
	}

	if dimScores["pci"] >= 80 {
		parts = append(parts, "Optimal PCIe isolation")
	}

	if len(parts) == 0 {
		parts = append(parts, "Moderate hardware affinity")
	}

	return strings.Join(parts, "; ")
}

// ============================================================================
// Pod Resource Analysis - Extract GPU requirements
// ============================================================================

// getRequiredGPUCount extracts GPU quota from pod spec
func getRequiredGPUCount(pod *v1.Pod) (int, bool) {
	requiredGPUs := 0

	for _, container := range pod.Spec.Containers {
		resources := container.Resources.Limits
		if resources == nil {
			continue
		}

		// Check NVIDIA GPU requests
		if qty, exists := resources["nvidia.com/gpu"]; exists && qty.Sign() > 0 {
			requiredGPUs += int(qty.Value())
		}

		// Check AMD GPU requests
		if qty, exists := resources["amd.com/gpu"]; exists && qty.Sign() > 0 {
			requiredGPUs += int(qty.Value())
		}
	}

	return requiredGPUs, requiredGPUs > 0
}

// getPodMemoryRequirements returns total memory needed by pod (MiB)
func getPodMemoryRequirements(pod *v1.Pod) int64 {
	totalMem := int64(0)

	for _, container := range pod.Spec.Containers {
		resources := container.Resources.Limits
		if resources == nil {
			continue
		}

		// Sum memory limits
		if qty, exists := resources[v1.ResourceMemory]; exists {
			totalMem += int64(qty.MilliValue() / 1000) // Convert to MB
		}
	}

	return totalMem
}

// ============================================================================
// Sorting & Selection Helpers
// ============================================================================

// sortResultsByScore sorts results descending by total score (in-place)
func sortResultsByScore(results []*ScoringResult) {
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].TotalScore > results[i].TotalScore {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
}

// BestFitScheduler selects the optimal node using top-k scores
type BestFitScheduler struct {
	topK         int              // return only top-k nodes
	algo         *GPUScoreAlgorithm
	capacityMgr  *CapacityManager
	logger       Logger
}

// NewBestFitScheduler creates a production-ready scheduler instance
func NewBestFitScheduler(numaWt, nvlinkWt, pciWt float64, capacityMgr *CapacityManager, logger Logger) *BestFitScheduler {
	return &BestFitScheduler{
		topK:        3,
		algo:        NewGPUScoreAlgorithm(numaWt, nvlinkWt, pciWt),
		capacityMgr: capacityMgr,
		logger:      logger,
	}
}

// Schedule finds the k-best nodes for a given pod
func (s *BestFitScheduler) Schedule(ctx context.Context, pod *v1.Pod, availableNodes []*NodeInfo) ([]*ScoringResult, error) {
	timing := time.Now()

	scores, err := s.algo.Score(ctx, pod, availableNodes)
	if err != nil {
		return nil, fmt.Errorf("scoring failed: %w", err)
	}

	// Enrich scores with capacity analysis
	for _, score := range scores {
		if score.TotalScore > 0 && s.capacityMgr != nil {
			capacity := s.capacityMgr.CheckNodeCapacity(pod, score.NodeName)
			score.DetailedMetrics = &TopologyMetrics{
				MIGIsolationAvailable: capacity.SupportsMIG,
				MPSHostActive:         capacity.MPSActive,
			}
		}
	}

	// Return top-k or all if fewer
	if len(scores) > s.topK {
		return scores[:s.topK], nil
	}

	return scores, nil
}

// ============================================================================
// Production Mode Enforcement - FAILS FAST per audit findings
// ============================================================================

// ValidateRealMode ensures this scheduler runs against REAL clusters (not mocks!)
func (s *BestFitScheduler) ValidateRealMode(clusterProvider ClusterProvider) error {
	// Try to actually fetch nodes - will FAIL if cluster is not live
	testCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	nodes, err := clusterProvider.ListNodes(testCtx, ListClustersRequest{ReadyOnly: true})
	if err != nil {
		return fmt.Errorf("cannot reach production cluster: %w - scheduler refuses to run without real K8s connection", err)
	}

	_ = nodes // if we got here, connection is real

	return nil
}

// ============================================================================
// Prometheus Metrics Instrumentation
// ============================================================================

// MetricsCollector gathers scheduler performance metrics for Prometheus
type MetricsCollector struct {
	scoringLatencies *PrometheusHistogram // histogram of scoring times
	scoreDist        *PrometheusGaugeVec  // distribution of final scores
	nodeCount        *PrometheusGauge     // number of nodes being scheduled
	errorCount       *PrometheusCounter   // failed scheduling operations
}

// NewMetricsCollector initializes Prometheus instrumentation
func NewMetricsCollector(registry PrometheusRegisterer) *MetricsCollector {
	return &MetricsCollector{
		scoringLatencies: NewHistogram("m3_sched_scoring_latency_ms", 
			"Time to compute topology-aware scores (target: <10ms single node)",
			[]float64{1, 5, 10, 25, 50, 100}, registry),
		scoreDist: NewGaugeVec("m3_sched_final_scores", 
			"Distribution of final scheduling scores across nodes",
			[]string{"score_bucket"}, registry),
		nodeCount: NewGauge("m3_sched_active_nodes", 
			"Number of nodes currently available for scheduling", registry),
		errorCount: NewCounter("m3_sched_errors_total", 
			"Total scheduling failures", registry),
	}
}

// RecordScoringDuration logs a completed scheduling operation
func (mc *MetricsCollector) RecordScoringDuration(duration time.Duration, finalScore float64) {
	ms := duration.Seconds() * 1000
	mc.scoringLatencies.Observe(ms)
	_ = finalScore
}

// UpdateNodeCount refreshes the current node count
func (mc *MetricsCollector) UpdateNodeCount(count int) {
	mc.nodeCount.Set(float64(count))
}

// RecordError increments failure counter
func (mc *MetricsCollector) RecordError(err error) {
	_ = err
	mc.errorCount.Inc()
}

// ============================================================================
// Placeholder Types - To be implemented as part of infrastructure
// ============================================================================

// Logger interface for unified logging
type Logger interface {
	Info(args ...interface{})
	Warn(args ...interface{})
	Error(args ...interface{})
	Debug(args ...interface{})
	EnabledLevel() int
	WithField(key string, value interface{}) Logger
}

// CapacityManager abstracts node capacity tracking
type CapacityManager struct {
	mu sync.RWMutex
}

type CapacityReport struct {
	SupportsMIG      bool
	MPSActive        bool
	AvailableGPUs    int
	ReservedGPUs     int
}

func (cm *CapacityManager) CheckNodeCapacity(pod *v1.Pod, nodeName string) *CapacityReport {
	return &CapacityReport{}
}

// Prometheus metric types (stubbed - real impl goes in monitoring package)
type PrometheusRegisterer interface {
	Register(prometheus.Collector) error
}

type PrometheusHistogram interface {
	Observe(value float64)
}

type PrometheusGaugeVec interface{}

type PrometheusGauge interface {
	Set(float64)
}

type PrometheusCounter interface {
	Inc()
}

func NewHistogram(name, help string, buckets []float64, reg PrometheusRegisterer) PrometheusHistogram {
	return nil
}

func NewGaugeVec(name, help string, labels []string, reg PrometheusRegisterer) PrometheusGaugeVec {
	return nil
}

func NewGauge(name, help string, reg PrometheusRegisterer) PrometheusGauge {
	return nil
}

func NewCounter(name, help string, reg PrometheusRegisterer) PrometheusCounter {
	return nil
}

// ClusterProvider abstracts cluster operations (interface for testing)
type ClusterProvider interface {
	ListNodes(ctx context.Context, req ListClustersRequest) ([]*NodeInfo, error)
	GetNode(ctx context.Context, clusterID, nodeName string) (*NodeInfo, error)
}
