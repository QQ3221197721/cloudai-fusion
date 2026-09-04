package nvlink_placer

import (
	"context"
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
)

// Discoverer wraps scheduler.TopologyDiscoverer to implement TopologyReader interface
type Discoverer struct {
	inner *scheduler.TopologyDiscoverer
}

// NewDiscoverer creates a new topology discoverer
func NewDiscoverer(sysfsPath, dcgmURL string) *Discoverer {
	return &Discoverer{
		inner: scheduler.NewTopologyDiscoverer(sysfsPath, dcgmURL),
	}
}

// GetNVLinkConnections returns NVLink connections from cached discovery
func (d *Discoverer) GetNVLinkConnections(ctx context.Context) ([]scheduler.NVLinkConnection, error) {
	topo, err := d.inner.DiscoverTopology(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("topology discovery failed: %w", err)
	}
	return topo.NVLinks, nil
}

// GetNUMAPolicy returns NUMA node affinity for specified GPU
func (d *Discoverer) GetNUMAPolicy(ctx context.Context, gpuIdx int) (int, error) {
	topo, err := d.inner.DiscoverTopology(ctx, "")
	if err != nil {
		return -1, fmt.Errorf("failed to get NUMA policy: %w", err)
	}
	
	numaNodes := topo.NUMANodes
	if numaNodes == nil {
		return -1, fmt.Errorf("no NUMA topology available")
	}
	
	// Find which NUMA node contains this GPU index
	for numaNode, gpuIndices := range numaNodes {
		for _, idx := range gpuIndices {
			if idx == gpuIdx {
				return numaNode, nil
			}
		}
	}
	
	return -1, fmt.Errorf("GPU %d not found in any NUMA node", gpuIdx)
}

// HasNVSwitch checks if node has NVSwitch fabric
func (d *Discoverer) HasNVSwitch() bool {
	topo, err := d.inner.DiscoverTopology(context.Background(), "")
	if err != nil {
		return false
	}
	return topo.HasNVSwitch
}
