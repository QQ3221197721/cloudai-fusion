package schedulertypes

import "context"

// NVLinkConnection represents a single NVLink connection between two GPUs
type NVLinkConnection struct {
	GPU1Index     int
	GPU2Index     int
	LinkType      string // NML(同板)|NVS(NVSwitch)|PHB(PCIe Hub)|SYS(系统总线)
	BandwidthGB   float64
}

// WorkloadRequest represents GPU placement request with topology requirements
type WorkloadRequest struct {
	GPUCount         int
	RequireNVLink    bool
	MinBandwidth     float64
	PreferSameNode   bool
	GPUAffinityGroup string
}

// PlacementResult represents scheduling decision outcome
type PlacementResult struct {
	Toposcore           float64
	Fit                 bool
	Reasons             []string
	GPUsNeeded          int
	ExecutionTimeEstimate string
}

// TopologyReader interface for discovering GPU topology information
type TopologyReader interface {
	GetNVLinkConnections(ctx context.Context) ([]NVLinkConnection, error)
	GetNUMAPolicy(ctx context.Context, gpuIdx int) (int, error)
	HasNVSwitch() bool
}
