// Package edge — Module 24: edge device discovery and hardware capability probing.
//
// HONESTY STATEMENT (read this before quoting any number produced here):
//
// The pre-existing DeviceDiscovery in offline_enhanced.go is an in-memory registry:
// its StartBrowse loop only prunes stale entries and carries a comment saying a real
// mDNS query would go here. There is no zeroconf/mDNS library in go.mod, so this
// package cannot send multicast DNS queries. Rather than pretend otherwise, Module 24
// splits discovery into a transport interface plus a probe interface:
//
//   - DiscoveryTransport      — where device advertisements come from.
//     MockDiscoveryTransport  — deterministic in-memory fleet, IsReal() == false.
//     A real implementation (hashicorp/mdns, miekg/mdns) must be injected to
//     get IsReal() == true; none ships today.
//   - HardwareProber          — how a device's capability spec is obtained.
//     LocalHardwareProber     — REAL: reads this host's own CPU/arch from the Go
//     runtime. Real numbers, but only ever describes the local host.
//     TXTHardwareProber       — parses hardware hints out of mDNS TXT records. As
//     real as the transport that produced the records: with the mock transport the
//     values are synthetic.
//
// SetTransport and SetProber call capability.Report and propagate its error, so a
// simulated discovery path cannot boot silently under run_mode=production: it fails
// at wiring time and shows up in GET /api/v1/capabilities and /readyz.
package edge

import (
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/sirupsen/logrus"
)

// ============================================================================
// Discovery transport
// ============================================================================

// DiscoveryTransport supplies device advertisements to DeviceDiscovery.
//
// Implementations must be safe for concurrent use. Browse performs exactly one
// discovery round and returns the devices that answered within the round budget;
// it must not mutate DeviceDiscovery state itself.
type DiscoveryTransport interface {
	// Browse performs one discovery round.
	Browse(ctx context.Context) ([]*DiscoveredDevice, error)
	// Kind names the transport for capability reporting, e.g. "mock" or "zeroconf".
	Kind() string
	// IsReal reports whether advertisements come from real network traffic.
	IsReal() bool
}

// MockDiscoveryTransportConfig tunes the simulated transport.
type MockDiscoveryTransportConfig struct {
	// RoundLatency is the wall-clock cost charged to one Browse round, modelling
	// the mDNS answer-collection window. Zero keeps benchmarks CPU-bound.
	RoundLatency time.Duration
	// LossRate is the probability in [0,1] that an advertised device fails to
	// answer a given round, modelling multicast packet loss.
	LossRate float64
	// Seed makes loss deterministic so accuracy figures are reproducible.
	Seed int64
}

// MockDiscoveryTransport is an in-memory DiscoveryTransport. It answers Browse
// from an explicitly advertised fleet and never touches the network.
//
// IsReal() returns false. Any metric measured through this transport describes
// this package's bookkeeping cost, not real mDNS latency on a LAN.
type MockDiscoveryTransport struct {
	mu        sync.Mutex
	cfg       MockDiscoveryTransportConfig
	advertise map[string]*DiscoveredDevice
	rng       *rand.Rand
	rounds    int
}

// NewMockDiscoveryTransport creates a deterministic simulated transport.
func NewMockDiscoveryTransport(cfg MockDiscoveryTransportConfig) *MockDiscoveryTransport {
	if cfg.LossRate < 0 {
		cfg.LossRate = 0
	}
	if cfg.LossRate > 1 {
		cfg.LossRate = 1
	}
	return &MockDiscoveryTransport{
		cfg:       cfg,
		advertise: make(map[string]*DiscoveredDevice),
		rng:       rand.New(rand.NewSource(cfg.Seed)),
	}
}

// Kind implements DiscoveryTransport.
func (t *MockDiscoveryTransport) Kind() string { return "mock" }

// IsReal implements DiscoveryTransport. Always false: no network is involved.
func (t *MockDiscoveryTransport) IsReal() bool { return false }

// Advertise adds or replaces a device in the simulated fleet.
func (t *MockDiscoveryTransport) Advertise(dev *DiscoveredDevice) {
	if dev == nil || dev.InstanceName == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.advertise[dev.InstanceName] = cloneDiscoveredDevice(dev)
}

// Withdraw removes a device from the simulated fleet, modelling a device that
// powered off without deregistering.
func (t *MockDiscoveryTransport) Withdraw(instanceName string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.advertise, instanceName)
}

// Rounds returns how many Browse rounds have been served.
func (t *MockDiscoveryTransport) Rounds() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.rounds
}

// Browse implements DiscoveryTransport.
func (t *MockDiscoveryTransport) Browse(ctx context.Context) ([]*DiscoveredDevice, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	t.mu.Lock()
	latency := t.cfg.RoundLatency
	loss := t.cfg.LossRate
	t.rounds++

	names := make([]string, 0, len(t.advertise))
	for name := range t.advertise {
		names = append(names, name)
	}
	// Sorted iteration keeps the RNG draw sequence stable across runs, which is
	// what makes the loss pattern (and therefore recall) reproducible.
	sort.Strings(names)

	answered := make([]*DiscoveredDevice, 0, len(names))
	for _, name := range names {
		if loss > 0 && t.rng.Float64() < loss {
			continue
		}
		answered = append(answered, cloneDiscoveredDevice(t.advertise[name]))
	}
	t.mu.Unlock()

	if latency > 0 {
		timer := time.NewTimer(latency)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return answered, nil
}

func cloneDiscoveredDevice(d *DiscoveredDevice) *DiscoveredDevice {
	if d == nil {
		return nil
	}
	cp := *d
	cp.IPAddresses = append([]string(nil), d.IPAddresses...)
	if d.TXTRecords != nil {
		cp.TXTRecords = make(map[string]string, len(d.TXTRecords))
		for k, v := range d.TXTRecords {
			cp.TXTRecords[k] = v
		}
	}
	return &cp
}

// ============================================================================
// Hardware capability probing
// ============================================================================

// HardwareProber resolves a discovered device into a HardwareSpec.
type HardwareProber interface {
	// Probe returns the device's capability spec.
	Probe(ctx context.Context, dev *DiscoveredDevice) (HardwareSpec, error)
	// Kind names the prober for capability reporting.
	Kind() string
	// IsReal reports whether the returned spec is measured from real hardware.
	IsReal() bool
}

// LocalHardwareProber reports the capabilities of the host it runs on, read from
// the Go runtime. The CPU and architecture figures are real measurements.
//
// Scope limit stated plainly: this prober describes the local host only. It is
// correct for an agent probing itself on an edge box, and wrong for a cloud
// controller probing a remote device — for that case the values would describe
// the controller, so IsReal() is true but the caller must not attribute the
// result to a remote node.
type LocalHardwareProber struct{}

// NewLocalHardwareProber creates a prober backed by the Go runtime.
func NewLocalHardwareProber() *LocalHardwareProber { return &LocalHardwareProber{} }

// Kind implements HardwareProber.
func (p *LocalHardwareProber) Kind() string { return "runtime-local" }

// IsReal implements HardwareProber.
func (p *LocalHardwareProber) IsReal() bool { return true }

// Probe implements HardwareProber. The dev argument is ignored: this prober
// always describes the local host.
func (p *LocalHardwareProber) Probe(ctx context.Context, _ *DiscoveredDevice) (HardwareSpec, error) {
	if err := ctx.Err(); err != nil {
		return HardwareSpec{}, err
	}
	return HardwareSpec{
		CPUCores: runtime.NumCPU(),
		CPUModel: runtime.GOARCH,
	}, nil
}

// TXTHardwareProber reads capability hints out of the mDNS TXT records that a
// device advertises. Recognised keys: cpu_cores, memory_gb, gpu_type, gpu_count,
// gpu_memory_gb, storage_gb, net_mbps, power_w.
//
// IsReal() mirrors the transport that produced the records, because the prober
// itself measures nothing: it only parses what it was handed.
type TXTHardwareProber struct {
	transportIsReal bool
}

// NewTXTHardwareProber creates a TXT-record prober. Pass the IsReal() value of
// the transport whose records will be parsed.
func NewTXTHardwareProber(transportIsReal bool) *TXTHardwareProber {
	return &TXTHardwareProber{transportIsReal: transportIsReal}
}

// Kind implements HardwareProber.
func (p *TXTHardwareProber) Kind() string { return "mdns-txt" }

// IsReal implements HardwareProber.
func (p *TXTHardwareProber) IsReal() bool { return p.transportIsReal }

// Probe implements HardwareProber. A device advertising no recognised key yields
// an error rather than a zero spec, so callers cannot mistake "not advertised"
// for "zero cores".
func (p *TXTHardwareProber) Probe(ctx context.Context, dev *DiscoveredDevice) (HardwareSpec, error) {
	if err := ctx.Err(); err != nil {
		return HardwareSpec{}, err
	}
	if dev == nil {
		return HardwareSpec{}, fmt.Errorf("edge: cannot probe nil device")
	}
	if len(dev.TXTRecords) == 0 {
		return HardwareSpec{}, fmt.Errorf("edge: device %s advertises no TXT records", dev.InstanceName)
	}

	var spec HardwareSpec
	matched := 0
	for key, raw := range dev.TXTRecords {
		value := strings.TrimSpace(raw)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "cpu_cores":
			if n, err := strconv.Atoi(value); err == nil {
				spec.CPUCores = n
				matched++
			}
		case "cpu_model":
			spec.CPUModel = value
			matched++
		case "memory_gb":
			if f, err := strconv.ParseFloat(value, 64); err == nil {
				spec.MemoryGB = f
				matched++
			}
		case "gpu_type":
			spec.GPUType = value
			matched++
		case "gpu_count":
			if n, err := strconv.Atoi(value); err == nil {
				spec.GPUCount = n
				matched++
			}
		case "gpu_memory_gb":
			if f, err := strconv.ParseFloat(value, 64); err == nil {
				spec.GPUMemoryGB = f
				matched++
			}
		case "storage_gb":
			if f, err := strconv.ParseFloat(value, 64); err == nil {
				spec.StorageGB = f
				matched++
			}
		case "net_mbps":
			if f, err := strconv.ParseFloat(value, 64); err == nil {
				spec.NetworkSpeedMbps = f
				matched++
			}
		case "power_w":
			if n, err := strconv.Atoi(value); err == nil {
				spec.PowerLimitWatts = n
				matched++
			}
		}
	}
	if matched == 0 {
		return HardwareSpec{}, fmt.Errorf("edge: device %s advertises no recognised hardware keys", dev.InstanceName)
	}
	return spec, nil
}

// ============================================================================
// Scan orchestration on top of the existing DeviceDiscovery registry
// ============================================================================

// ScanResult summarises one discovery round.
type ScanResult struct {
	// Answered is how many advertisements the transport returned.
	Answered int `json:"answered"`
	// NewDevices is how many of those were seen for the first time.
	NewDevices int `json:"new_devices"`
	// Refreshed is how many were already known and had their liveness renewed.
	Refreshed int `json:"refreshed"`
	// Pruned is how many known devices aged past the TTL during this round.
	Pruned int `json:"pruned"`
	// Duration is the wall-clock cost of the round.
	Duration time.Duration `json:"duration"`
	// TransportKind and Simulated record the provenance of this measurement.
	TransportKind string `json:"transport_kind"`
	Simulated     bool   `json:"simulated"`
}

// DiscoveryAccuracy compares what discovery found against ground truth.
type DiscoveryAccuracy struct {
	Expected       int      `json:"expected"`
	TruePositives  int      `json:"true_positives"`
	FalsePositives int      `json:"false_positives"`
	FalseNegatives int      `json:"false_negatives"`
	Precision      float64  `json:"precision"`
	Recall         float64  `json:"recall"`
	Missing        []string `json:"missing,omitempty"`
	Unexpected     []string `json:"unexpected,omitempty"`
	// Simulated is true when the underlying transport is not real, meaning these
	// figures describe a modelled fleet rather than a measured LAN.
	Simulated bool `json:"simulated"`
}

// SetTransport attaches a discovery transport and reports its provenance to the
// capability registry. The returned error is capability.Report's verdict: under
// run_mode=production a simulated transport is rejected here rather than booting
// silently.
func (d *DeviceDiscovery) SetTransport(t DiscoveryTransport) error {
	if t == nil {
		return fmt.Errorf("edge: discovery transport must not be nil")
	}
	d.mu.Lock()
	d.transport = t
	d.mu.Unlock()

	mode := capability.ModeSimulated
	detail := "in-memory discovery transport: no mDNS/zeroconf library is linked, advertisements are synthetic"
	if t.IsReal() {
		mode = capability.ModeReal
		detail = "discovery transport answers from real network advertisements"
	}
	return capability.Report("edge.discovery", t.Kind(), mode, detail)
}

// SetProber attaches a hardware prober and reports its provenance to the
// capability registry, returning capability.Report's verdict.
func (d *DeviceDiscovery) SetProber(p HardwareProber) error {
	if p == nil {
		return fmt.Errorf("edge: hardware prober must not be nil")
	}
	d.mu.Lock()
	d.prober = p
	d.mu.Unlock()

	mode := capability.ModeSimulated
	detail := "hardware spec is derived from synthetic advertisements, not measured hardware"
	if p.IsReal() {
		mode = capability.ModeReal
		detail = "hardware spec is measured from real hardware introspection"
	}
	return capability.Report("edge.discovery.probe", p.Kind(), mode, detail)
}

// Transport returns the attached transport, or nil when none is set.
func (d *DeviceDiscovery) Transport() DiscoveryTransport {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.transport
}

// Scan performs one discovery round through the attached transport and folds the
// answers into the registry. It returns an error when no transport is attached,
// so a caller cannot mistake an unwired discovery for an empty LAN.
func (d *DeviceDiscovery) Scan(ctx context.Context) (*ScanResult, error) {
	d.mu.RLock()
	transport := d.transport
	d.mu.RUnlock()

	if transport == nil {
		return nil, fmt.Errorf("edge: no discovery transport attached; call SetTransport first")
	}

	started := time.Now()
	answered, err := transport.Browse(ctx)
	if err != nil {
		return nil, fmt.Errorf("edge: discovery browse failed: %w", err)
	}

	result := &ScanResult{
		Answered:      len(answered),
		TransportKind: transport.Kind(),
		Simulated:     !transport.IsReal(),
	}

	for _, dev := range answered {
		if dev == nil || dev.InstanceName == "" {
			continue
		}
		d.mu.RLock()
		_, known := d.devices[dev.InstanceName]
		d.mu.RUnlock()

		d.RegisterDevice(dev)
		if known {
			result.Refreshed++
		} else {
			result.NewDevices++
		}
	}

	result.Pruned = d.PruneStale()
	result.Duration = time.Since(started)
	return result, nil
}

// ProbeDevice resolves one discovered device's hardware spec through the attached
// prober and caches the result on the device record.
func (d *DeviceDiscovery) ProbeDevice(ctx context.Context, instanceName string) (HardwareSpec, error) {
	d.mu.RLock()
	prober := d.prober
	dev := d.devices[instanceName]
	d.mu.RUnlock()

	if prober == nil {
		return HardwareSpec{}, fmt.Errorf("edge: no hardware prober attached; call SetProber first")
	}
	if dev == nil {
		return HardwareSpec{}, fmt.Errorf("edge: device %s not found", instanceName)
	}

	spec, err := prober.Probe(ctx, cloneDiscoveredDevice(dev))
	if err != nil {
		return HardwareSpec{}, err
	}

	d.mu.Lock()
	if current, ok := d.devices[instanceName]; ok {
		specCopy := spec
		current.Hardware = &specCopy
		current.HardwareSource = prober.Kind()
		current.HardwareIsReal = prober.IsReal()
	}
	d.mu.Unlock()

	return spec, nil
}

// EvaluateAccuracy compares the live registry against the ground-truth instance
// names the caller knows to be present.
func (d *DeviceDiscovery) EvaluateAccuracy(expected []string) *DiscoveryAccuracy {
	d.mu.RLock()
	alive := make(map[string]struct{}, len(d.devices))
	for name, dev := range d.devices {
		if dev.IsAlive {
			alive[name] = struct{}{}
		}
	}
	transport := d.transport
	d.mu.RUnlock()

	want := make(map[string]struct{}, len(expected))
	for _, name := range expected {
		want[name] = struct{}{}
	}

	acc := &DiscoveryAccuracy{
		Expected:  len(want),
		Simulated: transport == nil || !transport.IsReal(),
	}
	for name := range want {
		if _, ok := alive[name]; ok {
			acc.TruePositives++
		} else {
			acc.FalseNegatives++
			acc.Missing = append(acc.Missing, name)
		}
	}
	for name := range alive {
		if _, ok := want[name]; !ok {
			acc.FalsePositives++
			acc.Unexpected = append(acc.Unexpected, name)
		}
	}
	sort.Strings(acc.Missing)
	sort.Strings(acc.Unexpected)

	if found := acc.TruePositives + acc.FalsePositives; found > 0 {
		acc.Precision = float64(acc.TruePositives) / float64(found)
	}
	if acc.Expected > 0 {
		acc.Recall = float64(acc.TruePositives) / float64(acc.Expected)
	}
	return acc
}

// MakeMockFleet builds a deterministic fleet of advertisements for benchmarking
// and tests. Every device carries TXT records so TXTHardwareProber has something
// to parse. The values are synthetic by construction.
func MakeMockFleet(size int, prefix string) []*DiscoveredDevice {
	if prefix == "" {
		prefix = "cloudai-edge-"
	}
	fleet := make([]*DiscoveredDevice, 0, size)
	for i := 0; i < size; i++ {
		cores := 4 + (i % 5)*4 // 4, 8, 12, 16, 20
		fleet = append(fleet, &DiscoveredDevice{
			InstanceName: fmt.Sprintf("%s%04d", prefix, i),
			HostName:     fmt.Sprintf("edge-%04d.local", i),
			IPAddresses:  []string{fmt.Sprintf("192.168.%d.%d", i/254, i%254+1)},
			Port:         8082,
			TXTRecords: map[string]string{
				"cpu_cores": strconv.Itoa(cores),
				"memory_gb": strconv.Itoa(cores * 2),
				"gpu_type":  "jetson-orin",
				"gpu_count": "1",
				"net_mbps":  "1000",
				"power_w":   "60",
			},
		})
	}
	return fleet
}

// logDiscoveryProvenance emits a one-line provenance record so operators reading
// logs can tell simulated discovery from real discovery.
func (d *DeviceDiscovery) logDiscoveryProvenance(r *ScanResult) {
	d.logger.WithFields(logrus.Fields{
		"transport": r.TransportKind,
		"simulated": r.Simulated,
		"answered":  r.Answered,
		"duration":  r.Duration,
	}).Debug("edge discovery round complete")
}
