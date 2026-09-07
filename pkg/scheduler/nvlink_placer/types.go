package scheduler

import (
	"context"
)

// NVLinkConnection represents a single NVLink connection between two GPUs
type NVLinkConnection struct {
	SourceGPU   int
	DestinationGPU int
	SpeedInGBps float64
}

// NUMAPolicy represents NUMA affinity policy for GPU allocation
type NUMAPolicy int

const (
	NUMAPolicyNone NUMAPolicy = iota
	NUMAPolicyStrict
	NUMAPolicyPreferred
)
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
