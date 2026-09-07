// Package nvlink provides NVLink topology-aware GPU placement scoring
// as an independent package to avoid circular dependencies with plugin/builtin
package nvlink

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// TopologyReader interface abstracts GPU topology discovery
type TopologyReader interface {
	GetNVLinkConnections(ctx context.Context) ([]NVLinkConnection, error)
	GetNUMAPolicy(ctx context.Context, gpuIdx int) (int, error)
	HasNVSwitch() bool
}

// NVLinkConnection represents a single NVLink connection between two GPUs
type NVLinkConnection struct {
	SourceGPU      uint8
	DestinationGPU uint8
	LinkType       string // "NVL", "NVS", "NML", etc.
	BandwidthGB    float64
	GPU1Model      string
	GPU2Model      string
}

// NUMAPolicy represents NUMA affinity policy for GPU allocation
type NUMAPolicy int

const (
	NUMAPolicyNone NUMAPolicy = iota
	NUMAPolicyStrict
	NUMAPolicyPreferred
)

// DiscoveredGPU represents a single GPU with its topology info
type DiscoveredGPU struct {
	ID            string
	Model         string
	MemoryMiB     uint64
	Utilization   float64
	TemperatureC  float64
	PowerWatts    float64
	NUMANode      int
	NVLinkEnabled bool
}

// NodeGPUTopology represents complete GPU topology for a single node
type NodeGPUTopology struct {
	NodeName      string
	GPUs          []DiscoveredGPU
	NVLinks       []NVLinkConnection
	NUMANodes     map[int][]int
	HasNVSwitch   bool
	TotalGPUs     uint32
	TotalNVLinks  uint32
	MaxBandwidth  float64 // GB/s
	AvgBandwidth  float64 // GB/s
}

// WorkloadRequest specifies GPU topology requirements for placement
type WorkloadRequest struct {
	GPUCount           int
	RequireNVLink      bool
	PreferSameNode     bool
	MinBandwidth       float64    // Minimum bandwidth in GB/s
	PreferredGPUModels []string   // e.g., ["nvidia-a100", "nvidia-h100"]
	MaxCostPerHour     float64    // Maximum hourly cost
	SchedulingHints    *SchedulingHint
}

// SchedulingHint contains additional placement preferences
type SchedulingHint struct {
	PreferredNodes []string
	AvoidNodes     []string
}

// PlacementResult represents the outcome of a topology-aware placement decision
type PlacementResult struct {
	Toposcore           float64  // 0-100 score for topology quality
	Fit                 bool     // Whether workload fits topology requirements
	Reasons             []string // Explanations for the score
	GPUsNeeded          int      // Number of GPUs required
	RoomsNeeded         int      // Number of NUMA rooms needed
	ExecutionTimeEstimate string // Estimated execution time based on topology
}

// Discoverer wraps topology discovery logic
type Discoverer struct {
	sysfsPath  string
	dcgmURL    string
	cache      *topologyCache
	reader     TopologyReader
}

// topologyCache caches discovered topology for zero-allocation reads
type topologyCache struct {
	lastDiscovery time.Time
	topology      *NodeGPUTopology
	mu            sync.RWMutex
}

// NewDiscoverer creates a new topology discoverer
func NewDiscoverer(sysfsPath, dcgmURL string) *Discoverer {
	return &Discoverer{
		sysfsPath: sysfsPath,
		dcgmURL:   dcgmURL,
		cache:     &topologyCache{},
		reader:    nil, // Set via InjectReader() for testability
	}
}

// InjectReader allows dependency injection for testing
func (d *Discoverer) InjectReader(reader TopologyReader) {
	d.reader = reader
}

// GetNVLinkConnections returns NVLink connections from cache or fresh discovery
func (d *Discoverer) GetNVLinkConnections(ctx context.Context) ([]NVLinkConnection, error) {
	if d.cache.topology != nil {
		// Fast path: return cached copy (zero-copy design)
		d.cache.mu.RLock()
		defer d.cache.mu.RUnlock()
		if d.cache.topology != nil {
			result := make([]NVLinkConnection, len(d.cache.topology.NVLinks))
			copy(result, d.cache.topology.NVLinks)
			return result, nil
		}
	}

	// Slow path: fresh discovery
	return d.discoverTopology(ctx, "")
}

// ScoreTopology calculates NVLink-aware topology score (0-100)
func ScoreTopology(topology *NodeGPUTopology, gpuCount int, requireNVLink bool, minBandwidth float64) float64 {
	score := 50.0 // Neutral baseline

	if topology == nil || len(topology.NVLinks) == 0 {
		if requireNVLink {
			return 80.0 // Boost if NVLink required but unavailable
		}
		return 50.0
	}

	// NVLink connectivity bonus (+20 points)
	if len(topology.NVLinks) > 0 {
		score += 20.0
	}

	// Full mesh bonus (+10 points if all GPUs interconnected)
	totalPossiblePairs := gpuCount * (gpuCount - 1) / 2
	actualPairs := 0
	for _, link := range topology.NVLinks {
		if link.LinkType == "NML" || link.LinkType == "NVS" {
			actualPairs++
		}
	}

	fullMeshCoverage := float64(actualPairs) / float64(totalPossiblePairs)
	if fullMeshCoverage >= 0.9 {
		score += 10.0
	} else if fullMeshCoverage >= 0.5 {
		score += 5.0
	}

	// Bandwidth guarantee bonus (+5 points if min bandwidth met)
	if minBandwidth <= 0 || topology.MaxBandwidth >= minBandwidth {
		score += 5.0
	}

	// Clamp score to 0-100 range
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}

	return score
}

// NewPlacer creates a new NVLink-aware placer
func NewPlacer(reader TopologyReader) *Discoverer {
	return &Discoverer{
		cache: &topologyCache{},
		reader: reader,
	}
}
