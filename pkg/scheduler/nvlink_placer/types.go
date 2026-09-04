package schedulertypes

import (
	"context"
)

// TopologyReader interface for discovering GPU topology information
type TopologyReader interface {
	GetNVLinkConnections(ctx context.Context) ([]NVLinkConnection, error)
	GetNUMAPolicy(ctx context.Context, gpuIdx int) (int, error)
	HasNVSwitch() bool
}

// NodeGPUTopology represents complete GPU topology for a single node
type NodeGPUTopology struct {
	NodeName    string
	GPUs        []DiscoveredGPU
	NVLinks     []NVLinkConnection
	NUMANodes   map[int][]int // NUMA node → GPU indices mapping
	P2PMatrix   map[uint64]string // uint64-encoded edge key → connection type
	TotalGPUs   uint32
	HasNVLink   bool
	HasNVSwitch bool
}

// DiscoveredGPU represents a single discovered GPU device
type DiscoveredGPU struct {
	UUID              string
	Name              string
	Model             string
	TotalMemoryGB     float64
	FreeMemoryGB      float64
	UtilizationPercent float64
	TemperatureCelsius float64
}
