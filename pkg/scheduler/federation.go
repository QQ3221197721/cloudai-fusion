// Package scheduler - federation.go implements multi-cluster federated scheduling.
// Distributes AI workloads across multiple Kubernetes clusters based on resource
// availability, network locality, data gravity, cost, and compliance constraints.
// Supports cross-cluster GPU resource pooling and workload migration.
package scheduler

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// Federation Configuration
// ============================================================================

// FederationConfig holds multi-cluster federation settings
type FederationConfig struct {
	Enabled                bool    `json:"enabled"`
	HeartbeatIntervalSec   int     `json:"heartbeat_interval_sec"`
	ClusterTimeoutSec      int     `json:"cluster_timeout_sec"`
	DataGravityWeight      float64 `json:"data_gravity_weight"`      // prefer cluster with data
	NetworkLocalityWeight  float64 `json:"network_locality_weight"`  // prefer low-latency cluster
	CostWeight             float64 `json:"cost_weight"`              // prefer cheaper cluster
	ResourceFitWeight      float64 `json:"resource_fit_weight"`      // prefer cluster with best GPU fit
	ComplianceStrict       bool    `json:"compliance_strict"`        // enforce data residency
	CrossClusterMigration  bool    `json:"cross_cluster_migration"`  // allow live migration
	MaxClustersPerWorkload int     `json:"max_clusters_per_workload"` // for distributed training
}

// DefaultFederationConfig returns production-ready defaults
func DefaultFederationConfig() FederationConfig {
	return FederationConfig{
		Enabled:                true,
		HeartbeatIntervalSec:   30,
		ClusterTimeoutSec:      90,
		DataGravityWeight:      0.25,
		NetworkLocalityWeight:  0.20,
		CostWeight:             0.25,
		ResourceFitWeight:      0.30,
		ComplianceStrict:       true,
		CrossClusterMigration:  true,
		MaxClustersPerWorkload: 4,
	}
}

// ============================================================================
// Cluster & Federation Models
// ============================================================================

// FederatedCluster represents a cluster in the federation
type FederatedCluster struct {
	ID               string               `json:"id"`
	Name             string               `json:"name"`
	Provider         string               `json:"provider"`      // aws, gcp, azure, aliyun, huawei
	Region           string               `json:"region"`
	Zone             string               `json:"zone"`
	Status           string               `json:"status"`        // "active", "degraded", "offline"
	Capacity         FederatedCapacity     `json:"capacity"`
	NetworkLatencyMs map[string]float64    `json:"network_latency_ms"` // cluster_id → latency
	CostMultiplier   float64              `json:"cost_multiplier"`    // relative cost factor
	Compliance       []string             `json:"compliance"`         // e.g., "gdpr", "hipaa", "china-data-residency"
	DataLocations    []string             `json:"data_locations"`     // datasets available locally
	LastHeartbeat    time.Time            `json:"last_heartbeat"`
	Weights          *ClusterWeightOverride `json:"weights,omitempty"` // per-cluster scoring overrides
}

// FederatedCapacity describes GPU/resource capacity of a federated cluster
type FederatedCapacity struct {
	TotalGPUs        int                `json:"total_gpus"`
	AvailableGPUs    int                `json:"available_gpus"`
	GPUTypes         map[string]int     `json:"gpu_types"`           // type → count available
	TotalCPUCores    int                `json:"total_cpu_cores"`
	AvailableCPU     int                `json:"available_cpu_cores"`
	TotalMemoryGiB   int                `json:"total_memory_gib"`
	AvailableMemGiB  int                `json:"available_memory_gib"`
	GPUUtilization   float64            `json:"gpu_utilization_percent"`
	QueuedWorkloads  int                `json:"queued_workloads"`
}

// ClusterWeightOverride allows per-cluster scoring weight adjustments
type ClusterWeightOverride struct {
	PreferForTraining  bool    `json:"prefer_for_training"`
	PreferForInference bool    `json:"prefer_for_inference"`
	CostBias           float64 `json:"cost_bias"` // -1 to 1, positive = cheaper
}

// FederationPlacement represents a cross-cluster scheduling decision
type FederationPlacement struct {
	WorkloadID      string              `json:"workload_id"`
	WorkloadName    string              `json:"workload_name"`
	PrimaryCluster  string              `json:"primary_cluster"`
	AssistClusters  []string            `json:"assist_clusters,omitempty"` // for distributed training
	ClusterScores   []ClusterScore      `json:"cluster_scores"`
	Reason          string              `json:"reason"`
	DecidedAt       time.Time           `json:"decided_at"`
	Constraints     *PlacementConstraint `json:"constraints,omitempty"`
}

// ClusterScore holds scoring details for a candidate cluster
type ClusterScore struct {
	ClusterID       string  `json:"cluster_id"`
	ClusterName     string  `json:"cluster_name"`
	TotalScore      float64 `json:"total_score"`
	ResourceScore   float64 `json:"resource_score"`
	CostScore       float64 `json:"cost_score"`
	LocalityScore   float64 `json:"locality_score"`
	DataGravityScore float64 `json:"data_gravity_score"`
	CompliancePass  bool    `json:"compliance_pass"`
	Reason          string  `json:"reason"`
}

// PlacementConstraint specifies where a workload can be placed
type PlacementConstraint struct {
	RequiredRegions    []string `json:"required_regions,omitempty"`    // must be in these regions
	ExcludedRegions    []string `json:"excluded_regions,omitempty"`    // must not be in these regions
	RequiredProviders  []string `json:"required_providers,omitempty"`  // must use these providers
	RequiredCompliance []string `json:"required_compliance,omitempty"` // must have these certifications
	DataLocality       string   `json:"data_locality,omitempty"`       // dataset must be local
	MaxNetworkLatency  float64  `json:"max_network_latency_ms"`       // max acceptable latency
}

// ============================================================================
// Federation Scheduler
// ============================================================================

// FederationScheduler manages multi-cluster federated scheduling
type FederationScheduler struct {
	config    FederationConfig
	clusters  map[string]*FederatedCluster
	placements []FederationPlacement
	logger    *logrus.Logger
	mu        sync.RWMutex
}

// NewFederationScheduler creates a new federation scheduler
func NewFederationScheduler(cfg FederationConfig) *FederationScheduler {
	return &FederationScheduler{
		config:     cfg,
		clusters:   make(map[string]*FederatedCluster),
		placements: make([]FederationPlacement, 0),
		logger:     logrus.StandardLogger(),
	}
}

// RegisterCluster registers a cluster in the federation
func (fs *FederationScheduler) RegisterCluster(cluster *FederatedCluster) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if cluster.ID == "" {
		cluster.ID = common.NewUUID()
	}
	cluster.LastHeartbeat = time.Now().UTC()
	cluster.Status = "active"
	fs.clusters[cluster.ID] = cluster

	fs.logger.WithFields(logrus.Fields{
		"cluster":  cluster.Name,
		"provider": cluster.Provider,
		"region":   cluster.Region,
		"gpus":     cluster.Capacity.TotalGPUs,
	}).Info("Cluster registered in federation")

	return nil
}

// DeregisterCluster removes a cluster from the federation
func (fs *FederationScheduler) DeregisterCluster(clusterID string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if _, ok := fs.clusters[clusterID]; !ok {
		return fmt.Errorf("cluster %s not found", clusterID)
	}
	delete(fs.clusters, clusterID)
	return nil
}

// UpdateClusterHeartbeat updates cluster status and capacity
func (fs *FederationScheduler) UpdateClusterHeartbeat(clusterID string, capacity FederatedCapacity) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	cluster, ok := fs.clusters[clusterID]
	if !ok {
		return fmt.Errorf("cluster %s not found", clusterID)
	}

	cluster.Capacity = capacity
	cluster.LastHeartbeat = time.Now().UTC()
	cluster.Status = "active"
	return nil
}

// SelectCluster chooses the best cluster(s) for a workload using multi-factor scoring
func (fs *FederationScheduler) SelectCluster(ctx context.Context, workload *Workload, constraints *PlacementConstraint) (*FederationPlacement, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	activeClusters := fs.getActiveClusters()
	if len(activeClusters) == 0 {
		return nil, fmt.Errorf("no active clusters in federation")
	}

	// Score each cluster
	var scores []ClusterScore
	for _, cluster := range activeClusters {
		score := fs.scoreCluster(cluster, workload, constraints)
		if score.CompliancePass {
			scores = append(scores, score)
		}
	}

	if len(scores) == 0 {
		return nil, fmt.Errorf("no eligible clusters after applying constraints")
	}

	// Sort by total score descending
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].TotalScore > scores[j].TotalScore
	})

	placement := &FederationPlacement{
		WorkloadID:     workload.ID,
		WorkloadName:   workload.Name,
		PrimaryCluster: scores[0].ClusterID,
		ClusterScores:  scores,
		Reason:         scores[0].Reason,
		DecidedAt:      time.Now().UTC(),
		Constraints:    constraints,
	}

	// For distributed training, select assist clusters
	if workload.ResourceRequest.GPUCount > 8 && fs.config.MaxClustersPerWorkload > 1 {
		gpusNeeded := workload.ResourceRequest.GPUCount
		gpusAllocated := 0

		// Check primary cluster capacity
		if primary, ok := fs.clusters[scores[0].ClusterID]; ok {
			gpusAllocated = primary.Capacity.AvailableGPUs
			if gpusAllocated >= gpusNeeded {
				gpusAllocated = gpusNeeded
			}
		}

		// Allocate remaining from assist clusters
		for i := 1; i < len(scores) && gpusAllocated < gpusNeeded; i++ {
			if len(placement.AssistClusters) >= fs.config.MaxClustersPerWorkload-1 {
				break
			}
			if assist, ok := fs.clusters[scores[i].ClusterID]; ok {
				if assist.Capacity.AvailableGPUs > 0 {
					placement.AssistClusters = append(placement.AssistClusters, scores[i].ClusterID)
					gpusAllocated += assist.Capacity.AvailableGPUs
				}
			}
		}
	}

	// Record placement
	fs.mu.RUnlock()
	fs.mu.Lock()
	fs.placements = append(fs.placements, *placement)
	fs.mu.Unlock()
	fs.mu.RLock()

	return placement, nil
}

// scoreCluster computes a multi-factor score for a cluster
func (fs *FederationScheduler) scoreCluster(cluster *FederatedCluster, workload *Workload, constraints *PlacementConstraint) ClusterScore {
	score := ClusterScore{
		ClusterID:      cluster.ID,
		ClusterName:    cluster.Name,
		CompliancePass: true,
	}

	// === Compliance Check ===
	if constraints != nil {
		if !fs.checkCompliance(cluster, constraints) {
			score.CompliancePass = false
			score.Reason = "Failed compliance check"
			return score
		}
	}

	// === Resource Fit Score (0-100) ===
	if cluster.Capacity.AvailableGPUs >= workload.ResourceRequest.GPUCount {
		score.ResourceScore = 100.0 * (1.0 - float64(cluster.Capacity.AvailableGPUs-workload.ResourceRequest.GPUCount)/float64(cluster.Capacity.TotalGPUs+1))
	} else {
		score.ResourceScore = float64(cluster.Capacity.AvailableGPUs) / float64(workload.ResourceRequest.GPUCount) * 50.0
	}

	// GPU type match bonus
	if gpuType := string(workload.ResourceRequest.GPUType); gpuType != "" {
		if avail, ok := cluster.Capacity.GPUTypes[gpuType]; ok && avail >= workload.ResourceRequest.GPUCount {
			score.ResourceScore += 20.0
		}
	}

	// === Cost Score (0-100) ===
	if cluster.CostMultiplier > 0 {
		score.CostScore = 100.0 / cluster.CostMultiplier
	} else {
		score.CostScore = 100.0
	}
	if score.CostScore > 100 {
		score.CostScore = 100
	}

	// === Network Locality Score (0-100) ===
	score.LocalityScore = 80.0 // baseline
	if constraints != nil && constraints.MaxNetworkLatency > 0 {
		// Check latency to source cluster
		for _, latency := range cluster.NetworkLatencyMs {
			if latency <= constraints.MaxNetworkLatency {
				score.LocalityScore = 100.0 - (latency / constraints.MaxNetworkLatency * 40.0)
			}
		}
	}

	// Queue depth penalty
	if cluster.Capacity.QueuedWorkloads > 10 {
		score.LocalityScore -= float64(cluster.Capacity.QueuedWorkloads) * 0.5
	}

	// === Data Gravity Score (0-100) ===
	score.DataGravityScore = 50.0 // baseline
	if constraints != nil && constraints.DataLocality != "" {
		for _, loc := range cluster.DataLocations {
			if loc == constraints.DataLocality {
				score.DataGravityScore = 100.0
				break
			}
		}
	}

	// === Weighted Total Score ===
	score.TotalScore = fs.config.ResourceFitWeight*score.ResourceScore +
		fs.config.CostWeight*score.CostScore +
		fs.config.NetworkLocalityWeight*score.LocalityScore +
		fs.config.DataGravityWeight*score.DataGravityScore

	// Apply cluster-specific weight overrides
	if cluster.Weights != nil {
		if cluster.Weights.PreferForTraining && (workload.Type == common.WorkloadTypeTraining || workload.Type == common.WorkloadTypeFineTuning) {
			score.TotalScore *= 1.2
		}
		if cluster.Weights.PreferForInference && (workload.Type == common.WorkloadTypeInference || workload.Type == common.WorkloadTypeServing) {
			score.TotalScore *= 1.2
		}
		score.TotalScore += cluster.Weights.CostBias * 10
	}

	score.TotalScore = math.Min(100, math.Max(0, score.TotalScore))
	score.Reason = fmt.Sprintf("resource=%.0f cost=%.0f locality=%.0f data=%.0f",
		score.ResourceScore, score.CostScore, score.LocalityScore, score.DataGravityScore)

	return score
}

// checkCompliance verifies a cluster meets placement constraints
func (fs *FederationScheduler) checkCompliance(cluster *FederatedCluster, constraints *PlacementConstraint) bool {
	// Region check
	if len(constraints.RequiredRegions) > 0 {
		found := false
		for _, r := range constraints.RequiredRegions {
			if cluster.Region == r {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Excluded regions
	for _, r := range constraints.ExcludedRegions {
		if cluster.Region == r {
			return false
		}
	}

	// Provider check
	if len(constraints.RequiredProviders) > 0 {
		found := false
		for _, p := range constraints.RequiredProviders {
			if cluster.Provider == p {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Compliance certifications
	if fs.config.ComplianceStrict && len(constraints.RequiredCompliance) > 0 {
		for _, reqComp := range constraints.RequiredCompliance {
			found := false
			for _, clComp := range cluster.Compliance {
				if clComp == reqComp {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}

	return true
}

// getActiveClusters returns clusters that are healthy and recent
func (fs *FederationScheduler) getActiveClusters() []*FederatedCluster {
	timeout := time.Duration(fs.config.ClusterTimeoutSec) * time.Second
	now := time.Now().UTC()

	var active []*FederatedCluster
	for _, c := range fs.clusters {
		if c.Status == "active" && now.Sub(c.LastHeartbeat) < timeout {
			active = append(active, c)
		}
	}
	return active
}

// GetClusters returns all federated clusters
func (fs *FederationScheduler) GetClusters() []*FederatedCluster {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	result := make([]*FederatedCluster, 0, len(fs.clusters))
	for _, c := range fs.clusters {
		result = append(result, c)
	}
	return result
}

// GetRecentPlacements returns recent scheduling placements
func (fs *FederationScheduler) GetRecentPlacements(limit int) []FederationPlacement {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	if limit <= 0 || limit > len(fs.placements) {
		limit = len(fs.placements)
	}
	start := len(fs.placements) - limit
	result := make([]FederationPlacement, limit)
	copy(result, fs.placements[start:])
	return result
}

// GetFederationStatus returns overall federation health status
func (fs *FederationScheduler) GetFederationStatus() map[string]interface{} {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	totalGPUs := 0
	availGPUs := 0
	activeClusters := 0
	providerCounts := make(map[string]int)

	timeout := time.Duration(fs.config.ClusterTimeoutSec) * time.Second
	now := time.Now().UTC()

	for _, c := range fs.clusters {
		if c.Status == "active" && now.Sub(c.LastHeartbeat) < timeout {
			activeClusters++
			totalGPUs += c.Capacity.TotalGPUs
			availGPUs += c.Capacity.AvailableGPUs
			providerCounts[c.Provider]++
		}
	}

	return map[string]interface{}{
		"total_clusters":     len(fs.clusters),
		"active_clusters":    activeClusters,
		"total_gpus":         totalGPUs,
		"available_gpus":     availGPUs,
		"providers":          providerCounts,
		"total_placements":   len(fs.placements),
		"federation_enabled": fs.config.Enabled,
	}
}
