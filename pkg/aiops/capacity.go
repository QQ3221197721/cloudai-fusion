// Package aiops provides intelligent capacity planning for CloudAI Fusion.
// Includes resource demand forecasting, capacity modeling, bin-packing
// simulation, and automated expansion recommendations.
package aiops

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Capacity Planner
// ============================================================================

// CapacityPlanner performs intelligent capacity planning by forecasting
// resource demand, modeling cluster capacity, and generating expansion
// or optimization recommendations.
type CapacityPlanner struct {
	resourceHistory map[string][]ResourceSnapshot // clusterID → snapshots
	clusters        map[string]*ClusterCapacity
	plans           []*CapacityPlan
	config          CapacityPlanConfig
	mu              sync.RWMutex
	logger          *logrus.Logger
}

// CapacityPlanConfig configures the capacity planner.
type CapacityPlanConfig struct {
	HistoryWindow      time.Duration `json:"history_window" yaml:"historyWindow"`              // e.g., 30 days
	ForecastHorizon    int           `json:"forecast_horizon_days" yaml:"forecastHorizonDays"` // e.g., 90 days
	SafetyMargin       float64       `json:"safety_margin" yaml:"safetyMargin"`                // e.g., 0.2 = 20%
	CPUThreshold       float64       `json:"cpu_threshold" yaml:"cpuThreshold"`                // trigger at 80%
	MemoryThreshold    float64       `json:"memory_threshold" yaml:"memoryThreshold"`          // trigger at 85%
	GPUThreshold       float64       `json:"gpu_threshold" yaml:"gpuThreshold"`                // trigger at 75%
	StorageThreshold   float64       `json:"storage_threshold" yaml:"storageThreshold"`        // trigger at 85%
	MinLeadTimeDays    int           `json:"min_lead_time_days" yaml:"minLeadTimeDays"`        // e.g., 14 days
}

// DefaultCapacityPlanConfig returns sensible defaults.
func DefaultCapacityPlanConfig() CapacityPlanConfig {
	return CapacityPlanConfig{
		HistoryWindow:    30 * 24 * time.Hour,
		ForecastHorizon:  90,
		SafetyMargin:     0.2,
		CPUThreshold:     0.8,
		MemoryThreshold:  0.85,
		GPUThreshold:     0.75,
		StorageThreshold: 0.85,
		MinLeadTimeDays:  14,
	}
}

// ResourceSnapshot captures cluster resource state at a point in time.
type ResourceSnapshot struct {
	Timestamp    time.Time `json:"timestamp"`
	ClusterID    string    `json:"cluster_id"`

	// CPU
	CPUCapacity    int64   `json:"cpu_capacity_millicores"`
	CPURequested   int64   `json:"cpu_requested_millicores"`
	CPUUsed        int64   `json:"cpu_used_millicores"`

	// Memory
	MemCapacity    int64   `json:"mem_capacity_bytes"`
	MemRequested   int64   `json:"mem_requested_bytes"`
	MemUsed        int64   `json:"mem_used_bytes"`

	// GPU
	GPUCapacity    int     `json:"gpu_capacity"`
	GPUAllocated   int     `json:"gpu_allocated"`
	GPUUtilization float64 `json:"gpu_utilization_percent"`

	// Storage
	StorageCapacity int64  `json:"storage_capacity_bytes"`
	StorageUsed     int64  `json:"storage_used_bytes"`

	// Workloads
	PodCount       int     `json:"pod_count"`
	NodeCount      int     `json:"node_count"`
	PendingPods    int     `json:"pending_pods"` // pods waiting for resources
}

// ClusterCapacity represents current cluster capacity information.
type ClusterCapacity struct {
	ClusterID     string    `json:"cluster_id"`
	ClusterName   string    `json:"cluster_name"`
	Provider      string    `json:"provider"`
	Region        string    `json:"region"`
	NodeCount     int       `json:"node_count"`
	NodeTypes     map[string]int `json:"node_types"` // instanceType → count

	// Current utilization
	CPUUtilization    float64 `json:"cpu_utilization"`
	MemUtilization    float64 `json:"mem_utilization"`
	GPUUtilization    float64 `json:"gpu_utilization"`
	StorageUtilization float64 `json:"storage_utilization"`

	// Capacity
	TotalCPU      int64  `json:"total_cpu_millicores"`
	TotalMemory   int64  `json:"total_memory_bytes"`
	TotalGPU      int    `json:"total_gpu"`
	TotalStorage  int64  `json:"total_storage_bytes"`

	// Constraints
	MaxNodes      int    `json:"max_nodes"`
	MaxPods       int    `json:"max_pods"`
}

// CapacityPlan represents a capacity expansion/optimization plan.
type CapacityPlan struct {
	ID              string                 `json:"id"`
	ClusterID       string                 `json:"cluster_id"`
	Type            string                 `json:"type"` // expansion, optimization, rightsizing
	Priority        string                 `json:"priority"` // critical, high, medium, low
	Recommendations []CapacityRecommendation `json:"recommendations"`
	Forecast        *ResourceForecast      `json:"forecast"`
	EstimatedCost   float64                `json:"estimated_monthly_cost"`
	TimeToExhaustion *time.Duration        `json:"time_to_exhaustion,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
}

// CapacityRecommendation is a single recommendation within a plan.
type CapacityRecommendation struct {
	Action        string  `json:"action"`       // add_nodes, remove_nodes, upgrade_nodes, add_storage
	ResourceType  string  `json:"resource_type"` // cpu, memory, gpu, storage
	InstanceType  string  `json:"instance_type,omitempty"`
	Count         int     `json:"count"`
	Reason        string  `json:"reason"`
	Urgency       string  `json:"urgency"` // immediate, 7days, 30days, 90days
	EstimatedCost float64 `json:"estimated_monthly_cost"`
	Impact        string  `json:"impact"` // description of impact
}

// ResourceForecast predicts future resource demand.
type ResourceForecast struct {
	ClusterID   string              `json:"cluster_id"`
	Horizon     int                 `json:"horizon_days"`
	DataPoints  []ForecastDataPoint `json:"data_points"`
	CPUExhaust  *time.Time          `json:"cpu_exhaustion_date,omitempty"`
	MemExhaust  *time.Time          `json:"mem_exhaustion_date,omitempty"`
	GPUExhaust  *time.Time          `json:"gpu_exhaustion_date,omitempty"`
	DiskExhaust *time.Time          `json:"disk_exhaustion_date,omitempty"`
	Confidence  float64             `json:"confidence"` // 0-1
	GeneratedAt time.Time           `json:"generated_at"`
}

// ForecastDataPoint is a single forecast data point.
type ForecastDataPoint struct {
	Date           time.Time `json:"date"`
	CPUDemand      float64   `json:"cpu_demand_percent"`
	MemDemand      float64   `json:"mem_demand_percent"`
	GPUDemand      float64   `json:"gpu_demand_percent"`
	StorageDemand  float64   `json:"storage_demand_percent"`
	ConfidenceLow  float64   `json:"confidence_low"`
	ConfidenceHigh float64   `json:"confidence_high"`
}

// NewCapacityPlanner creates a new capacity planner.
func NewCapacityPlanner(cfg CapacityPlanConfig, logger *logrus.Logger) *CapacityPlanner {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &CapacityPlanner{
		resourceHistory: make(map[string][]ResourceSnapshot),
		clusters:        make(map[string]*ClusterCapacity),
		config:          cfg,
		logger:          logger,
	}
}

// RegisterCluster registers a cluster for capacity planning.
func (p *CapacityPlanner) RegisterCluster(cluster *ClusterCapacity) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clusters[cluster.ClusterID] = cluster
}

// IngestSnapshot adds a resource snapshot for analysis.
func (p *CapacityPlanner) IngestSnapshot(snapshot ResourceSnapshot) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.resourceHistory[snapshot.ClusterID] = append(p.resourceHistory[snapshot.ClusterID], snapshot)

	// Trim old data
	cutoff := time.Now().Add(-p.config.HistoryWindow)
	for k, snapshots := range p.resourceHistory {
		trimmed := make([]ResourceSnapshot, 0, len(snapshots))
		for _, s := range snapshots {
			if s.Timestamp.After(cutoff) {
				trimmed = append(trimmed, s)
			}
		}
		p.resourceHistory[k] = trimmed
	}
}

// ForecastDemand generates a resource demand forecast for a cluster.
func (p *CapacityPlanner) ForecastDemand(ctx context.Context, clusterID string) (*ResourceForecast, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	p.mu.RLock()
	snapshots, ok := p.resourceHistory[clusterID]
	cluster := p.clusters[clusterID]
	p.mu.RUnlock()

	if !ok || len(snapshots) < 7 {
		return nil, fmt.Errorf("insufficient data for cluster %s (need >= 7 snapshots, have %d)", clusterID, len(snapshots))
	}
	if cluster == nil {
		return nil, fmt.Errorf("cluster %s not registered", clusterID)
	}

	// Sort by timestamp
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Timestamp.Before(snapshots[j].Timestamp)
	})

	// Extract utilization time series
	cpuUtil := make([]float64, len(snapshots))
	memUtil := make([]float64, len(snapshots))
	gpuUtil := make([]float64, len(snapshots))
	storUtil := make([]float64, len(snapshots))

	for i, s := range snapshots {
		if s.CPUCapacity > 0 {
			cpuUtil[i] = float64(s.CPUUsed) / float64(s.CPUCapacity)
		}
		if s.MemCapacity > 0 {
			memUtil[i] = float64(s.MemUsed) / float64(s.MemCapacity)
		}
		if s.GPUCapacity > 0 {
			gpuUtil[i] = float64(s.GPUAllocated) / float64(s.GPUCapacity)
		}
		if s.StorageCapacity > 0 {
			storUtil[i] = float64(s.StorageUsed) / float64(s.StorageCapacity)
		}
	}

	forecast := &ResourceForecast{
		ClusterID:   clusterID,
		Horizon:     p.config.ForecastHorizon,
		GeneratedAt: time.Now(),
	}

	// Generate forecast data points
	lastDate := snapshots[len(snapshots)-1].Timestamp

	cpuTrend := linearTrend(cpuUtil)
	memTrend := linearTrend(memUtil)
	gpuTrend := linearTrend(gpuUtil)
	storTrend := linearTrend(storUtil)

	cpuEMA := ema(cpuUtil, 0.3)
	memEMA := ema(memUtil, 0.3)
	gpuEMA := ema(gpuUtil, 0.3)
	storEMA := ema(storUtil, 0.3)

	for day := 1; day <= p.config.ForecastHorizon; day++ {
		date := lastDate.AddDate(0, 0, day)
		step := float64(day)

		cpuPred := clampFloat(cpuEMA+cpuTrend*step, 0, 1)
		memPred := clampFloat(memEMA+memTrend*step, 0, 1)
		gpuPred := clampFloat(gpuEMA+gpuTrend*step, 0, 1)
		storPred := clampFloat(storEMA+storTrend*step, 0, 1)

		// Confidence interval widens with time
		spread := 0.05 + 0.002*step
		maxVal := math.Max(math.Max(cpuPred, memPred), math.Max(gpuPred, storPred))

		forecast.DataPoints = append(forecast.DataPoints, ForecastDataPoint{
			Date:           date,
			CPUDemand:      cpuPred * 100,
			MemDemand:      memPred * 100,
			GPUDemand:      gpuPred * 100,
			StorageDemand:  storPred * 100,
			ConfidenceLow:  clampFloat(maxVal-spread, 0, 1) * 100,
			ConfidenceHigh: clampFloat(maxVal+spread, 0, 1) * 100,
		})

		// Calculate exhaustion dates
		if forecast.CPUExhaust == nil && cpuPred >= p.config.CPUThreshold {
			d := date
			forecast.CPUExhaust = &d
		}
		if forecast.MemExhaust == nil && memPred >= p.config.MemoryThreshold {
			d := date
			forecast.MemExhaust = &d
		}
		if forecast.GPUExhaust == nil && gpuPred >= p.config.GPUThreshold {
			d := date
			forecast.GPUExhaust = &d
		}
		if forecast.DiskExhaust == nil && storPred >= p.config.StorageThreshold {
			d := date
			forecast.DiskExhaust = &d
		}
	}

	// Confidence based on data volume and consistency
	dataConf := math.Min(float64(len(snapshots))/100.0, 1.0)
	cpuStd := stdDev(cpuUtil)
	stabilityConf := math.Max(0, 1-cpuStd*2)
	forecast.Confidence = dataConf*0.5 + stabilityConf*0.5

	p.logger.WithFields(logrus.Fields{
		"cluster":    clusterID,
		"horizon":    p.config.ForecastHorizon,
		"confidence": fmt.Sprintf("%.0f%%", forecast.Confidence*100),
		"cpu_exhaust":  forecast.CPUExhaust,
		"gpu_exhaust":  forecast.GPUExhaust,
	}).Info("Capacity forecast generated")

	return forecast, nil
}

// GeneratePlan creates a capacity plan with recommendations for a cluster.
func (p *CapacityPlanner) GeneratePlan(ctx context.Context, clusterID string) (*CapacityPlan, error) {
	forecast, err := p.ForecastDemand(ctx, clusterID)
	if err != nil {
		return nil, err
	}

	p.mu.RLock()
	cluster := p.clusters[clusterID]
	snapshots := p.resourceHistory[clusterID]
	p.mu.RUnlock()

	if cluster == nil {
		return nil, fmt.Errorf("cluster %s not registered", clusterID)
	}

	plan := &CapacityPlan{
		ID:        fmt.Sprintf("plan-%d", time.Now().UnixNano()),
		ClusterID: clusterID,
		Type:      "expansion",
		Forecast:  forecast,
		CreatedAt: time.Now(),
	}

	// Analyze current state
	if len(snapshots) > 0 {
		latest := snapshots[len(snapshots)-1]

		// CPU recommendations
		if cluster.TotalCPU > 0 {
			cpuUtil := float64(latest.CPUUsed) / float64(cluster.TotalCPU)
			if cpuUtil > p.config.CPUThreshold {
				neededCPU := int64(float64(latest.CPUUsed)*(1+p.config.SafetyMargin)) - cluster.TotalCPU
				nodeCount := int(math.Ceil(float64(neededCPU) / 8000)) // assume 8 cores per node
				plan.Recommendations = append(plan.Recommendations, CapacityRecommendation{
					Action:       "add_nodes",
					ResourceType: "cpu",
					Count:        nodeCount,
					Reason:       fmt.Sprintf("CPU utilization at %.0f%% (threshold: %.0f%%)", cpuUtil*100, p.config.CPUThreshold*100),
					Urgency:      determineUrgency(cpuUtil, p.config.CPUThreshold),
					Impact:       fmt.Sprintf("Adds %d millicores of CPU capacity", neededCPU),
				})
			}
		}

		// Memory recommendations
		if cluster.TotalMemory > 0 {
			memUtil := float64(latest.MemUsed) / float64(cluster.TotalMemory)
			if memUtil > p.config.MemoryThreshold {
				neededMem := int64(float64(latest.MemUsed)*(1+p.config.SafetyMargin)) - cluster.TotalMemory
				nodeCount := int(math.Ceil(float64(neededMem) / (32 * 1024 * 1024 * 1024))) // 32GB per node
				plan.Recommendations = append(plan.Recommendations, CapacityRecommendation{
					Action:       "add_nodes",
					ResourceType: "memory",
					Count:        nodeCount,
					Reason:       fmt.Sprintf("Memory utilization at %.0f%% (threshold: %.0f%%)", memUtil*100, p.config.MemoryThreshold*100),
					Urgency:      determineUrgency(memUtil, p.config.MemoryThreshold),
					Impact:       fmt.Sprintf("Adds %d GB of memory", neededMem/(1024*1024*1024)),
				})
			}
		}

		// GPU recommendations
		if cluster.TotalGPU > 0 {
			gpuUtil := float64(latest.GPUAllocated) / float64(cluster.TotalGPU)
			if gpuUtil > p.config.GPUThreshold {
				neededGPU := int(math.Ceil(float64(latest.GPUAllocated)*(1+p.config.SafetyMargin))) - cluster.TotalGPU
				plan.Recommendations = append(plan.Recommendations, CapacityRecommendation{
					Action:       "add_nodes",
					ResourceType: "gpu",
					InstanceType: "gpu-node",
					Count:        neededGPU,
					Reason:       fmt.Sprintf("GPU utilization at %.0f%% (threshold: %.0f%%)", gpuUtil*100, p.config.GPUThreshold*100),
					Urgency:      determineUrgency(gpuUtil, p.config.GPUThreshold),
					Impact:       fmt.Sprintf("Adds %d GPU devices", neededGPU),
				})
			}
		}

		// Storage recommendations
		if cluster.TotalStorage > 0 {
			storUtil := float64(latest.StorageUsed) / float64(cluster.TotalStorage)
			if storUtil > p.config.StorageThreshold {
				neededStorage := int64(float64(latest.StorageUsed)*(1+p.config.SafetyMargin)) - cluster.TotalStorage
				plan.Recommendations = append(plan.Recommendations, CapacityRecommendation{
					Action:       "add_storage",
					ResourceType: "storage",
					Count:        1,
					Reason:       fmt.Sprintf("Storage utilization at %.0f%% (threshold: %.0f%%)", storUtil*100, p.config.StorageThreshold*100),
					Urgency:      determineUrgency(storUtil, p.config.StorageThreshold),
					Impact:       fmt.Sprintf("Adds %d GB of storage", neededStorage/(1024*1024*1024)),
				})
			}
		}

		// Pending pods (immediate need)
		if latest.PendingPods > 0 {
			plan.Recommendations = append(plan.Recommendations, CapacityRecommendation{
				Action:       "add_nodes",
				ResourceType: "compute",
				Count:        int(math.Ceil(float64(latest.PendingPods) / 30)), // ~30 pods per node
				Reason:       fmt.Sprintf("%d pods pending due to insufficient resources", latest.PendingPods),
				Urgency:      "immediate",
				Impact:       fmt.Sprintf("Resolves %d pending pod(s)", latest.PendingPods),
			})
		}
	}

	// Forecast-based recommendations (proactive)
	if forecast.CPUExhaust != nil {
		daysUntil := int(time.Until(*forecast.CPUExhaust).Hours() / 24)
		if daysUntil < p.config.ForecastHorizon {
			plan.Recommendations = append(plan.Recommendations, CapacityRecommendation{
				Action:       "add_nodes",
				ResourceType: "cpu",
				Count:        2,
				Reason:       fmt.Sprintf("CPU capacity exhaustion forecast in %d days", daysUntil),
				Urgency:      forecastUrgency(daysUntil, p.config.MinLeadTimeDays),
				Impact:       "Proactive CPU capacity expansion before exhaustion",
			})
		}
	}

	if forecast.GPUExhaust != nil {
		daysUntil := int(time.Until(*forecast.GPUExhaust).Hours() / 24)
		if daysUntil < p.config.ForecastHorizon {
			plan.Recommendations = append(plan.Recommendations, CapacityRecommendation{
				Action:       "add_nodes",
				ResourceType: "gpu",
				InstanceType: "gpu-node",
				Count:        1,
				Reason:       fmt.Sprintf("GPU capacity exhaustion forecast in %d days", daysUntil),
				Urgency:      forecastUrgency(daysUntil, p.config.MinLeadTimeDays),
				Impact:       "Proactive GPU capacity expansion before exhaustion",
			})
		}
	}

	// Set time to exhaustion
	var minExhaust *time.Time
	for _, t := range []*time.Time{forecast.CPUExhaust, forecast.MemExhaust, forecast.GPUExhaust, forecast.DiskExhaust} {
		if t != nil && (minExhaust == nil || t.Before(*minExhaust)) {
			minExhaust = t
		}
	}
	if minExhaust != nil {
		tte := time.Until(*minExhaust)
		plan.TimeToExhaustion = &tte
	}

	// Set plan priority
	plan.Priority = determinePlanPriority(plan.Recommendations)

	// Estimate total cost
	for i := range plan.Recommendations {
		plan.Recommendations[i].EstimatedCost = estimateNodeCost(plan.Recommendations[i])
		plan.EstimatedCost += plan.Recommendations[i].EstimatedCost
	}

	p.mu.Lock()
	p.plans = append(p.plans, plan)
	p.mu.Unlock()

	p.logger.WithFields(logrus.Fields{
		"cluster":         clusterID,
		"recommendations": len(plan.Recommendations),
		"priority":        plan.Priority,
		"estimated_cost":  fmt.Sprintf("$%.2f/mo", plan.EstimatedCost),
	}).Info("Capacity plan generated")

	return plan, nil
}

// GetPlans returns generated capacity plans.
func (p *CapacityPlanner) GetPlans(clusterID string) []*CapacityPlan {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var result []*CapacityPlan
	for _, plan := range p.plans {
		if clusterID == "" || plan.ClusterID == clusterID {
			result = append(result, plan)
		}
	}
	return result
}

// GetCapacityOverview returns capacity overview for all clusters.
func (p *CapacityPlanner) GetCapacityOverview() *CapacityOverview {
	p.mu.RLock()
	defer p.mu.RUnlock()

	overview := &CapacityOverview{
		Clusters:    make(map[string]*ClusterCapacitySummary),
		GeneratedAt: time.Now(),
	}

	for id, cluster := range p.clusters {
		summary := &ClusterCapacitySummary{
			ClusterID:     id,
			ClusterName:   cluster.ClusterName,
			NodeCount:     cluster.NodeCount,
			CPUUtil:       cluster.CPUUtilization,
			MemUtil:       cluster.MemUtilization,
			GPUUtil:       cluster.GPUUtilization,
			StorageUtil:   cluster.StorageUtilization,
		}

		// Determine status
		maxUtil := math.Max(math.Max(cluster.CPUUtilization, cluster.MemUtilization),
			math.Max(cluster.GPUUtilization, cluster.StorageUtilization))
		if maxUtil > 0.9 {
			summary.Status = "critical"
		} else if maxUtil > 0.75 {
			summary.Status = "warning"
		} else {
			summary.Status = "healthy"
		}

		overview.Clusters[id] = summary
		overview.TotalNodes += cluster.NodeCount
		overview.TotalGPU += cluster.TotalGPU
	}

	return overview
}

// CapacityOverview provides a high-level view of all cluster capacity.
type CapacityOverview struct {
	Clusters    map[string]*ClusterCapacitySummary `json:"clusters"`
	TotalNodes  int                                `json:"total_nodes"`
	TotalGPU    int                                `json:"total_gpu"`
	GeneratedAt time.Time                          `json:"generated_at"`
}

// ClusterCapacitySummary summarizes a single cluster's capacity.
type ClusterCapacitySummary struct {
	ClusterID   string  `json:"cluster_id"`
	ClusterName string  `json:"cluster_name"`
	NodeCount   int     `json:"node_count"`
	Status      string  `json:"status"` // healthy, warning, critical
	CPUUtil     float64 `json:"cpu_utilization"`
	MemUtil     float64 `json:"mem_utilization"`
	GPUUtil     float64 `json:"gpu_utilization"`
	StorageUtil float64 `json:"storage_utilization"`
}

// ============================================================================
// Statistical Helpers
// ============================================================================

func linearTrend(values []float64) float64 {
	n := len(values)
	if n < 2 {
		return 0
	}
	sumX, sumY, sumXY, sumX2 := 0.0, 0.0, 0.0, 0.0
	for i, v := range values {
		x := float64(i)
		sumX += x
		sumY += v
		sumXY += x * v
		sumX2 += x * x
	}
	nf := float64(n)
	denom := nf*sumX2 - sumX*sumX
	if denom == 0 {
		return 0
	}
	return (nf*sumXY - sumX*sumY) / denom
}

func ema(values []float64, alpha float64) float64 {
	if len(values) == 0 {
		return 0
	}
	result := values[0]
	for i := 1; i < len(values); i++ {
		result = alpha*values[i] + (1-alpha)*result
	}
	return result
}

func stdDev(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	mean := 0.0
	for _, v := range values {
		mean += v
	}
	mean /= float64(len(values))
	sumSq := 0.0
	for _, v := range values {
		d := v - mean
		sumSq += d * d
	}
	return math.Sqrt(sumSq / float64(len(values)-1))
}

func clampFloat(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func determineUrgency(current, threshold float64) string {
	ratio := current / threshold
	if ratio > 1.2 {
		return "immediate"
	} else if ratio > 1.1 {
		return "7days"
	} else if ratio > 1.0 {
		return "30days"
	}
	return "90days"
}

func forecastUrgency(daysUntil, leadTime int) string {
	if daysUntil <= leadTime/2 {
		return "immediate"
	} else if daysUntil <= leadTime {
		return "7days"
	} else if daysUntil <= leadTime*2 {
		return "30days"
	}
	return "90days"
}

func determinePlanPriority(recs []CapacityRecommendation) string {
	for _, r := range recs {
		if r.Urgency == "immediate" {
			return "critical"
		}
	}
	for _, r := range recs {
		if r.Urgency == "7days" {
			return "high"
		}
	}
	for _, r := range recs {
		if r.Urgency == "30days" {
			return "medium"
		}
	}
	return "low"
}

func estimateNodeCost(rec CapacityRecommendation) float64 {
	// Simplified cost estimation per node/month
	baseCost := 0.0
	switch rec.ResourceType {
	case "cpu":
		baseCost = 150.0 // ~$150/mo per compute node
	case "memory":
		baseCost = 200.0 // ~$200/mo per memory-optimized node
	case "gpu":
		baseCost = 1500.0 // ~$1500/mo per GPU node
	case "storage":
		baseCost = 50.0 // ~$50/mo per 1TB
	case "compute":
		baseCost = 180.0
	}
	return baseCost * float64(rec.Count)
}
