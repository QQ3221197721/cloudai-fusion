// Package scheduler - heterogeneous_model.go models A100/H100/L4 mixed GPU topologies
// with NVLink/PCIe/Cross-Node bandwidth differentiation, enabling topology-aware
// constraint scheduling across heterogeneous accelerator pools.
//
// This builds on top of BandwidthGraph (dense_k_subgraph.go) and adds:
//   - GPUType enumeration with architecture-specific compute/memory profiles
//   - HeterogeneousTopology: a typed extension of BandwidthGraph with per-GPU metadata
//   - Constructors that accept GPU type arrays + adjacency hints → weighted graph
//   - Pre-built fixtures: 32-GPU mixed cluster (8×A100 + 16×H100 + 8×L4)
//
// Target: 32-GPU topology construction ≤1µs (pre-allocated adjacency matrix).
package scheduler

// ============================================================================
// GPU Architecture Profiles
// ============================================================================

// GPUType identifies the accelerator architecture.
type GPUType int

const (
	GPUTypeA100 GPUType = iota
	GPUTypeH100
	GPUTypeL4
)

// String returns a human-readable name.
func (t GPUType) String() string {
	switch t {
	case GPUTypeA100:
		return "A100"
	case GPUTypeH100:
		return "H100"
	case GPUTypeL4:
		return "L4"
	default:
		return "Unknown"
	}
}

// GPUProfile holds the architecture-specific characteristics of a GPU type.
type GPUProfile struct {
	Type          GPUType
	MemoryGB      float64 // HBM capacity in GiB
	FP16TFLOPS    float64 // Peak FP16 throughput
	TDPWatts      float64 // Thermal Design Power
	NVLinkLanes   int     // Number of NVLink lanes (0 = no NVLink)
	NVLinkBWGBps  float64 // Per-link NVLink bandwidth (GB/s)
}

// Predefined profiles for the three supported architectures.
var (
	ProfileA100 = GPUProfile{
		Type:         GPUTypeA100,
		MemoryGB:     80,
		FP16TFLOPS:   312,
		TDPWatts:     400,
		NVLinkLanes:  12,
		NVLinkBWGBps: BandwidthTierNVLink, // 600 GB/s
	}
	ProfileH100 = GPUProfile{
		Type:         GPUTypeH100,
		MemoryGB:     80,
		FP16TFLOPS:   990,
		TDPWatts:     700,
		NVLinkLanes:  18,
		NVLinkBWGBps: BandwidthTierNVSwitch, // 900 GB/s (NVSwitch 4.0)
	}
	ProfileL4 = GPUProfile{
		Type:         GPUTypeL4,
		MemoryGB:     24,
		FP16TFLOPS:   121,
		TDPWatts:     72,
		NVLinkLanes:  0,  // L4 has no NVLink
		NVLinkBWGBps: 0,
	}
)

// ProfileForType returns the canonical GPUProfile for a given type.
func ProfileForType(t GPUType) GPUProfile {
	switch t {
	case GPUTypeA100:
		return ProfileA100
	case GPUTypeH100:
		return ProfileH100
	case GPUTypeL4:
		return ProfileL4
	default:
		return ProfileL4
	}
}

// ============================================================================
// Connection Tier
// ============================================================================

// ConnectionTier describes the interconnect type between two GPUs.
type ConnectionTier int

const (
	ConnNVLink    ConnectionTier = iota // Same-node NVLink/NVSwitch
	ConnPCIe                            // Same-node PCIe switch
	ConnCrossNode                       // Cross-node fabric (IB/RoCE)
)

// Bandwidth returns the tier's bandwidth in GB/s.
func (c ConnectionTier) Bandwidth() float64 {
	switch c {
	case ConnNVLink:
		return BandwidthTierNVLink
	case ConnPCIe:
		return BandwidthTierPCIeSwitch
	case ConnCrossNode:
		return BandwidthTierCrossNode
	}
	return 0
}

// ============================================================================
// Heterogeneous Topology Model
// ============================================================================

// HeterogeneousGPU extends GPUVertex with type and profile metadata.
type HeterogeneousGPU struct {
	ID       int        `json:"id"`
	NodeID   int        `json:"node_id"`   // Physical node index
	Type     GPUType    `json:"type"`
	Profile  GPUProfile `json:"profile"`
	MemFreeGB float64   `json:"mem_free_gb"` // Current free memory
}

// HeterogeneousTopology is a weighted graph representing a mixed-GPU cluster
// with per-GPU type metadata and three-tier bandwidth differentiation.
type HeterogeneousTopology struct {
	GPUs   []HeterogeneousGPU `json:"gpus"`
	Graph  *BandwidthGraph    `json:"graph"`
	// nodeGPUs maps node_id → GPU indices on that node.
	nodeGPUs map[int][]int
}

// NewHeterogeneousTopology constructs a heterogeneous topology from GPU types,
// node assignments, and a connection matrix.
//
// Parameters:
//   - gpuTypes: GPU type for each GPU index (len = total GPU count)
//   - nodeAssign: node ID for each GPU index (same length as gpuTypes)
//   - connections: optional explicit connection matrix (nil = auto-derive from node assignment)
//
// When connections is nil, GPUs on the same node get NVLink bandwidth (if both
// support it) or PCIe, and cross-node pairs get cross-node bandwidth.
//
// Target: 32 GPUs in ≤1µs — achieved via pre-allocated flat slice + minimal branching.
func NewHeterogeneousTopology(gpuTypes []GPUType, nodeAssign []int, connections [][]ConnectionTier) *HeterogeneousTopology {
	n := len(gpuTypes)
	if n == 0 {
		return &HeterogeneousTopology{
			Graph:    NewBandwidthGraph(nil, nil),
			nodeGPUs: make(map[int][]int),
		}
	}

	gpus := make([]HeterogeneousGPU, n)
	nodes := make([]GPUVertex, n)
	nodeMap := make(map[int][]int, 8)

	for i := 0; i < n; i++ {
		p := ProfileForType(gpuTypes[i])
		gpus[i] = HeterogeneousGPU{
			ID:        i,
			NodeID:    nodeAssign[i],
			Type:      gpuTypes[i],
			Profile:   p,
			MemFreeGB: p.MemoryGB, // start fully free
		}
		nodes[i] = GPUVertex{
			ID:           i,
			Socket:       nodeAssign[i],
			Host:         "",
			MemoryGB:     p.MemoryGB,
			FreeFraction: 1.0,
		}
		nodeMap[nodeAssign[i]] = append(nodeMap[nodeAssign[i]], i)
	}

	// Pre-allocate weight matrix as a flat backing array for cache friendliness.
	flat := make([]float64, n*n)
	weight := make([][]float64, n)
	for i := range weight {
		weight[i] = flat[i*n : (i+1)*n]
	}

	// Fill bandwidth weights.
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			var bw float64
			if connections != nil && i < len(connections) && j < len(connections[i]) {
				bw = connections[i][j].Bandwidth()
			} else {
				bw = deriveBandwidth(gpus[i], gpus[j])
			}
			weight[i][j] = bw
			weight[j][i] = bw
		}
	}

	return &HeterogeneousTopology{
		GPUs:     gpus,
		Graph:    NewBandwidthGraph(nodes, weight),
		nodeGPUs: nodeMap,
	}
}

// deriveBandwidth infers the interconnect bandwidth between two GPUs based on
// their node assignment and NVLink capability.
func deriveBandwidth(a, b HeterogeneousGPU) float64 {
	if a.NodeID != b.NodeID {
		return BandwidthTierCrossNode
	}
	// Same node: use NVLink if both GPUs have NVLink lanes.
	if a.Profile.NVLinkLanes > 0 && b.Profile.NVLinkLanes > 0 {
		// Use the minimum of both GPUs' NVLink bandwidth (asymmetric pair).
		bw := a.Profile.NVLinkBWGBps
		if b.Profile.NVLinkBWGBps < bw {
			bw = b.Profile.NVLinkBWGBps
		}
		return bw
	}
	return BandwidthTierPCIeSwitch
}

// GPUCount returns the total number of GPUs in the topology.
func (t *HeterogeneousTopology) GPUCount() int { return len(t.GPUs) }

// NodeCount returns the number of distinct physical nodes.
func (t *HeterogeneousTopology) NodeCount() int { return len(t.nodeGPUs) }

// GPUsOnNode returns the GPU indices assigned to a specific node.
func (t *HeterogeneousTopology) GPUsOnNode(nodeID int) []int {
	return t.nodeGPUs[nodeID]
}

// GPUsByType returns indices of GPUs matching the given type.
func (t *HeterogeneousTopology) GPUsByType(typ GPUType) []int {
	var result []int
	for i, g := range t.GPUs {
		if g.Type == typ {
			result = append(result, i)
		}
	}
	return result
}

// TotalMemoryGB returns the aggregate free memory across all GPUs.
func (t *HeterogeneousTopology) TotalMemoryGB() float64 {
	var total float64
	for _, g := range t.GPUs {
		total += g.MemFreeGB
	}
	return total
}

// IntraNodeBandwidth returns the total intra-node bandwidth for a set of GPU indices.
func (t *HeterogeneousTopology) IntraNodeBandwidth(indices []int) float64 {
	return t.Graph.SubsetWeight(indices)
}

// ============================================================================
// Fixtures: 32-GPU Mixed Cluster (8×A100 + 16×H100 + 8×L4)
// ============================================================================

// NewMixed32GPUTopology creates a realistic 32-GPU heterogeneous cluster:
//   - Node 0: 8× A100 (NVLink mesh)
//   - Node 1: 8× H100 (NVSwitch mesh)
//   - Node 2: 8× H100 (NVSwitch mesh)
//   - Node 3: 8× L4   (PCIe only)
func NewMixed32GPUTopology() *HeterogeneousTopology {
	types := make([]GPUType, 32)
	nodes := make([]int, 32)
	for i := 0; i < 8; i++ {
		types[i] = GPUTypeA100
		nodes[i] = 0
	}
	for i := 8; i < 16; i++ {
		types[i] = GPUTypeH100
		nodes[i] = 1
	}
	for i := 16; i < 24; i++ {
		types[i] = GPUTypeH100
		nodes[i] = 2
	}
	for i := 24; i < 32; i++ {
		types[i] = GPUTypeL4
		nodes[i] = 3
	}
	return NewHeterogeneousTopology(types, nodes, nil)
}

// NewMixed64GPUTopology creates a 64-GPU cluster for large-scale benchmarks:
//   - Nodes 0-1: 8× A100 each
//   - Nodes 2-5: 8× H100 each
//   - Nodes 6-7: 8× L4 each
func NewMixed64GPUTopology() *HeterogeneousTopology {
	types := make([]GPUType, 64)
	nodeAssign := make([]int, 64)
	for i := 0; i < 16; i++ {
		types[i] = GPUTypeA100
		nodeAssign[i] = i / 8
	}
	for i := 16; i < 48; i++ {
		types[i] = GPUTypeH100
		nodeAssign[i] = 2 + (i-16)/8
	}
	for i := 48; i < 64; i++ {
		types[i] = GPUTypeL4
		nodeAssign[i] = 6 + (i-48)/8
	}
	return NewHeterogeneousTopology(types, nodeAssign, nil)
}

// ============================================================================
// Benchmark helper: topology construction latency
// ============================================================================

// ConstructTopology32ForBench is a helper exposed for benchmarks.
func ConstructTopology32ForBench() *HeterogeneousTopology {
	return NewMixed32GPUTopology()
}
