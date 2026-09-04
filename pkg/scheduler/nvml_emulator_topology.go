package scheduler

// ============================================================================
// nvml_emulator_topology.go
//
// FAITHFUL NVML COMPETITOR for the T2 GPU-topology head-to-head benchmark.
//
// WHY AN EMULATOR (documented limitation):
//   The real competitor is NVIDIA's NVML C library (libnvidia-ml.so), reached
//   via CGO. This developer host has NO NVIDIA GPU and NO libnvidia-ml, so a
//   CGO binding could neither build cleanly nor run — and a CGO stub returning
//   errors would be a *strawman that we auto-beat*, violating the fairness rule.
//
//   Instead we model NVML's REAL discovery cost path in pure Go:
//     - NVML never parses text. It iterates device handles and reads NVLink
//       state per link as a binary enum via ioctl-backed calls:
//         nvmlDeviceGetCount            (device count)
//         nvmlDeviceGetHandleByIndex    (handle per GPU)
//         nvmlDeviceGetNvLinkState      (per-link active/inactive enum)
//         nvmlDeviceGetNvLinkVersion    (per-link generation)
//         nvmlDeviceGetNvLinkRemotePciInfo (peer identification per link)
//       Source: NVML Reference Manual (device queries, NvLink section).
//     - Bandwidth to a peer = active_links_to_peer × per_link_bidir_GBps.
//
//   LIMITATION (stated up front so the verdict stays honest):
//     This emulator models NVML's ALGORITHMIC path (O(GPU × links) direct
//     integer/enum reads, no string tokenization). It does NOT add the real
//     driver ioctl syscall latency that NVML pays per call. Omitting that
//     overhead makes NVML look FASTER here than it would be relative to a
//     subprocess-based nvidia-smi call — i.e. this is a CONSERVATIVE setup
//     that favors the competitor, never us. Likewise our side is benchmarked
//     on the REAL production text parser (parseNVSmiTopoMatrix), excluding the
//     subprocess spawn. Both sides therefore measure PROCESSING work only, on
//     identical ground-truth topology, which is the fair same-work-unit.
// ============================================================================

const (
	// nvmlMaxLinks is the max NVLink count per GPU on NVLink 4.0 (H100 = 18).
	nvmlMaxLinks = 18

	// perLinkBidirGBps is bidirectional bandwidth per single NVLink lane.
	// NVLink 3.0/4.0 lane ≈ 25 GB/s per direction → 50 GB/s bidirectional.
	// 12 lanes → 600 GB/s (NV12, A100); 18 lanes → 900 GB/s (NV18, H100).
	perLinkBidirGBps = 50.0
)

// nvmlLinkState mirrors what NVML returns per NVLink lane for a device.
// Each field maps to a distinct NVML query the C library performs.
type nvmlLinkState struct {
	active    bool // nvmlDeviceGetNvLinkState -> NVML_FEATURE_ENABLED
	version   int  // nvmlDeviceGetNvLinkVersion (3 or 4)
	remoteGPU int  // nvmlDeviceGetNvLinkRemotePciInfo -> peer device index
}

// nvmlDeviceRecord mirrors a device handle plus its queryable NVLink lanes.
type nvmlDeviceRecord struct {
	index   int
	uuid    string
	name    string
	migMode bool
	links   [nvmlMaxLinks]nvmlLinkState
}

// nvmlEmulatedTopology is the "hardware" the emulated NVML driver reads from.
type nvmlEmulatedTopology struct {
	devices []nvmlDeviceRecord
}

// nvmlBandwidthGraph is the same-work-unit OUTPUT: peer bandwidth in GB/s.
// Key format "i-j" with i<j, identical to parseNVSmiTopoMatrix's P2P keys.
type nvmlBandwidthGraph struct {
	edges map[string]float64
}

// discoverBandwidthGraph replicates NVML's discovery loop and produces the
// peer bandwidth graph. This is the competitor's work unit.
//
// Cost model (faithful to NVML): for each device handle we scan its lanes,
// and for each ACTIVE lane read its version + remote peer and accumulate
// bandwidth. No strings are tokenized; all reads are integer/enum field reads.
func (t *nvmlEmulatedTopology) discoverBandwidthGraph() *nvmlBandwidthGraph {
	graph := &nvmlBandwidthGraph{edges: make(map[string]float64, len(t.devices)*2)}

	// nvmlDeviceGetCount + per-index handle acquisition.
	count := len(t.devices)
	for i := 0; i < count; i++ {
		dev := &t.devices[i] // nvmlDeviceGetHandleByIndex(i)

		// Accumulate active-lane bandwidth per peer for this device.
		var peerLanes [nvmlMaxLinks * 8]int // scratch peer->laneCount (>= max GPUs)
		var peerSeen [nvmlMaxLinks * 8]bool

		for l := 0; l < nvmlMaxLinks; l++ {
			ls := dev.links[l] // nvmlDeviceGetNvLinkState / Version / RemotePciInfo
			if !ls.active {
				continue
			}
			peer := ls.remoteGPU
			if peer < 0 || peer >= len(peerLanes) {
				continue
			}
			peerLanes[peer]++
			peerSeen[peer] = true
		}

		for peer := 0; peer < len(peerSeen); peer++ {
			if !peerSeen[peer] || peer == dev.index {
				continue
			}
			bw := float64(peerLanes[peer]) * perLinkBidirGBps
			lo, hi := dev.index, peer
			if lo > hi {
				lo, hi = hi, lo
			}
			key := formatPairKey(lo, hi)
			// NVLink is symmetric; only record once (i<j), matching our P2P keys.
			if _, ok := graph.edges[key]; !ok {
				graph.edges[key] = bw
			}
		}
	}
	return graph
}

// formatPairKey builds the "i-j" key without fmt to keep the hot path lean,
// mirroring how the C library composes peer identifiers from integers.
func formatPairKey(i, j int) string {
	return itoaSmall(i) + "-" + itoaSmall(j)
}

// itoaSmall is a tiny non-negative int to string helper for the pair key.
func itoaSmall(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
