// Package scheduler — M2 Direction 2: Minimal-Disruption MIG Reconfiguration.
//
// Barrier framing (real competitor short-comings this module attacks):
//   - NVIDIA GPU Operator / MIG Manager reconfigures MIG geometry at *whole-device*
//     granularity. To change the geometry of a GPU it first drives the device to a
//     clean state, which means terminating EVERY pod currently running on that GPU
//     (and in some CSP setups rebooting the node).
//   - The K8s MIG device plugin likewise requires the GPU to be idle to switch MIG
//     mode / geometry.
//
// We implement *minimal-disruption* reconfiguration: when a new request cannot be
// served by the current geometry we destroy/recreate only the minimal subset of MIG
// instances required, keeping every other active workload on the same GPU running.
//
// Model (distinct from the pure bin-packing layer in mig_binpack.go):
//   - Each GPU carries a *current MIG geometry* — the set of configured instances,
//     each with a profile, a start slice, and an optional active workload occupying
//     it. Idle (configured-but-unoccupied) instances persist exactly like real MIG,
//     where instances stay partitioned until an explicit reconfiguration.
//   - A workload <-> instance mapping so departures leave sticky idle geometry, which
//     is what fragments the device over time and eventually forces reshapes.
//
// This file is self-contained: it reuses only the shared, hardware-derived primitives
// from mig_binpack.go (MIGSliceProfile, A100Profiles, profileByName, totalSlices,
// IsLargeProfile). It does NOT touch DASP / the bin-packing strategies.
package scheduler

import (
	"fmt"
	"math"
)

// ============================================================================
// Geometry model
// ============================================================================

// migInstanceR is a single configured MIG instance in a GPU's current geometry.
type migInstanceR struct {
	profile  MIGSliceProfile
	start    int    // start slice index (inclusive)
	workload string // "" => idle (configured but unoccupied)
}

func (m *migInstanceR) size() int     { return m.profile.Size }
func (m *migInstanceR) end() int      { return m.start + m.profile.Size }
func (m *migInstanceR) active() bool  { return m.workload != "" }

// reconfigGPUState is one GPU's current MIG geometry (its set of instances).
type reconfigGPUState struct {
	index     int
	instances []*migInstanceR
}

// covered returns a slice-occupancy mask of every slice currently owned by an
// instance (idle or active). Uncovered slices are "unpartitioned" free capacity.
func (g *reconfigGPUState) covered() [totalSlices]bool {
	var c [totalSlices]bool
	for _, ins := range g.instances {
		for i := ins.start; i < ins.end() && i < totalSlices; i++ {
			c[i] = true
		}
	}
	return c
}

// validFreeStart returns the smallest valid start index where p fits entirely inside
// currently *uncovered* (unpartitioned) slices, or -1. This is a geometry ADD that
// touches no existing instance.
func (g *reconfigGPUState) validFreeStart(p MIGSliceProfile) int {
	cov := g.covered()
	for _, s := range p.StartConstraints {
		if s+p.Size > totalSlices {
			continue
		}
		free := true
		for i := s; i < s+p.Size; i++ {
			if cov[i] {
				free = false
				break
			}
		}
		if free {
			return s
		}
	}
	return -1
}

// freeSlices counts the currently uncovered slices.
func (g *reconfigGPUState) freeSlices() int {
	cov := g.covered()
	n := 0
	for _, b := range cov {
		if !b {
			n++
		}
	}
	return n
}

// idleMatching returns an idle instance whose profile matches name, or nil. MIG
// instances are fixed-size, so a workload of profile X can only reuse an idle X.
func (g *reconfigGPUState) idleMatching(name string) *migInstanceR {
	for _, ins := range g.instances {
		if !ins.active() && ins.profile.Name == name {
			return ins
		}
	}
	return nil
}

// overlapping returns every instance whose slice range intersects [start,start+size).
func (g *reconfigGPUState) overlapping(start, size int) []*migInstanceR {
	var out []*migInstanceR
	for _, ins := range g.instances {
		if ins.start < start+size && start < ins.end() {
			out = append(out, ins)
		}
	}
	return out
}

// activeCount returns the number of instances currently occupied by a workload.
func (g *reconfigGPUState) activeCount() int {
	n := 0
	for _, ins := range g.instances {
		if ins.active() {
			n++
		}
	}
	return n
}

// removeInstances drops the given instances from the geometry (identity match).
func (g *reconfigGPUState) removeInstances(victims []*migInstanceR) {
	if len(victims) == 0 {
		return
	}
	vset := make(map[*migInstanceR]bool, len(victims))
	for _, v := range victims {
		vset[v] = true
	}
	kept := g.instances[:0]
	for _, ins := range g.instances {
		if !vset[ins] {
			kept = append(kept, ins)
		}
	}
	g.instances = kept
}

// ============================================================================
// Reconfiguration policies
// ============================================================================

type reconfigPolicy int

const (
	// policyMinDisruption destroys only the minimal subset of instances (preferring
	// idle ones, cost 0) needed to open a contiguous region for the new request.
	policyMinDisruption reconfigPolicy = iota
	// policyFullDrain models NVIDIA MIG Manager: any reshape drains the whole GPU.
	policyFullDrain
)

// ReconfigMetrics is the outcome tally for a full workload sequence.
type ReconfigMetrics struct {
	Placed         int // requests successfully placed
	ZeroDisrupt    int // requests served with 0 interrupted workloads (reuse or free-carve)
	TotalDisrupted int // active workloads interrupted (THE headline metric)
	ReconfigCount  int // reshape events that destroyed >= 1 instance
	AffectedSlices int // total slices belonging to destroyed instances
}

// AvgAffectedSlices returns the mean number of slices destroyed per reshape.
func (m ReconfigMetrics) AvgAffectedSlices() float64 {
	if m.ReconfigCount == 0 {
		return 0
	}
	return float64(m.AffectedSlices) / float64(m.ReconfigCount)
}

// ============================================================================
// Cluster
// ============================================================================

// MIGReconfigCluster runs a workload arrival/departure sequence under one policy and
// tallies the reconfiguration disruption it incurs.
type MIGReconfigCluster struct {
	gpus    []*reconfigGPUState
	policy  reconfigPolicy
	inst    map[string]*migInstanceR // workload id -> its live instance
	metrics ReconfigMetrics
}

// NewMinDisruptionCluster builds a cluster driven by the minimal-disruption policy.
func NewMinDisruptionCluster(n int) *MIGReconfigCluster {
	return newReconfigCluster(n, policyMinDisruption)
}

// NewFullDrainCluster builds the MIG-Manager-style whole-device-drain baseline.
func NewFullDrainCluster(n int) *MIGReconfigCluster {
	return newReconfigCluster(n, policyFullDrain)
}

func newReconfigCluster(n int, policy reconfigPolicy) *MIGReconfigCluster {
	gpus := make([]*reconfigGPUState, n)
	for i := range gpus {
		gpus[i] = &reconfigGPUState{index: i}
	}
	return &MIGReconfigCluster{
		gpus:   gpus,
		policy: policy,
		inst:   make(map[string]*migInstanceR),
	}
}

// Metrics returns the accumulated outcome tally.
func (c *MIGReconfigCluster) Metrics() ReconfigMetrics { return c.metrics }

// Arrive places a new workload of the given profile.
//
// Shared fast paths (identical for both policies — no reshape needed):
//
//	A. Reuse: an already-configured idle instance of the same profile exists -> assign
//	   it (0 disruption, 0 geometry change).
//	B. Free-carve: unpartitioned free space can host a fresh instance -> create it
//	   (0 disruption; a pure addition that MIG supports without draining).
//
// Only when neither applies is a reshape required, and that is where the policies
// diverge (see reshapeMinDisruption vs reshapeFullDrain).
func (c *MIGReconfigCluster) Arrive(id, profileName string) error {
	p, ok := profileByName(profileName)
	if !ok {
		return fmt.Errorf("unknown profile %s", profileName)
	}

	// Step A: reuse an idle configured instance of the same profile.
	for _, g := range c.gpus {
		if ins := g.idleMatching(p.Name); ins != nil {
			ins.workload = id
			c.inst[id] = ins
			c.metrics.Placed++
			c.metrics.ZeroDisrupt++
			return nil
		}
	}

	// Step B: carve the instance out of unpartitioned free space (best-fit).
	if g, start := c.bestFreeFit(p); g != nil {
		ins := &migInstanceR{profile: p, start: start, workload: id}
		g.instances = append(g.instances, ins)
		c.inst[id] = ins
		c.metrics.Placed++
		c.metrics.ZeroDisrupt++
		return nil
	}

	// Step C: reshape required.
	switch c.policy {
	case policyMinDisruption:
		c.reshapeMinDisruption(id, p)
	default:
		c.reshapeFullDrain(id, p)
	}
	c.metrics.Placed++
	return nil
}

// Depart releases a workload. Its instance persists as idle geometry (sticky MIG
// partitioning). A no-op if the workload was already interrupted by a reshape.
func (c *MIGReconfigCluster) Depart(id string) {
	ins, ok := c.inst[id]
	if !ok {
		return
	}
	ins.workload = ""
	delete(c.inst, id)
}

// bestFreeFit picks the GPU with a valid free start that leaves the least free space
// afterwards (tightest fit), preserving large contiguous regions on other GPUs.
func (c *MIGReconfigCluster) bestFreeFit(p MIGSliceProfile) (*reconfigGPUState, int) {
	var best *reconfigGPUState
	bestStart := -1
	bestLeftover := math.MaxInt32
	for _, g := range c.gpus {
		s := g.validFreeStart(p)
		if s < 0 {
			continue
		}
		leftover := g.freeSlices() - p.Size
		if leftover < bestLeftover {
			bestLeftover = leftover
			best = g
			bestStart = s
		}
	}
	return best, bestStart
}

// reshapeMinDisruption opens a contiguous region for p by destroying the minimal set
// of instances. It scans every (GPU, valid-start) candidate and picks the region
// overlapping the FEWEST ACTIVE workloads (idle instances are free to reclaim), then
// the fewest total instances. Only the overlapping instances are destroyed; every
// other active workload on that GPU keeps running.
func (c *MIGReconfigCluster) reshapeMinDisruption(id string, p MIGSliceProfile) {
	var bestG *reconfigGPUState
	bestStart := -1
	bestActive := math.MaxInt32
	bestDestroy := math.MaxInt32
	for _, g := range c.gpus {
		for _, s := range p.StartConstraints {
			if s+p.Size > totalSlices {
				continue
			}
			victims := g.overlapping(s, p.Size)
			active := 0
			for _, v := range victims {
				if v.active() {
					active++
				}
			}
			if active < bestActive || (active == bestActive && len(victims) < bestDestroy) {
				bestActive = active
				bestDestroy = len(victims)
				bestG = g
				bestStart = s
			}
		}
	}
	if bestG == nil {
		return // impossible for n >= 1: every GPU has valid start regions
	}
	victims := bestG.overlapping(bestStart, p.Size)
	// Free defragmentation: additionally reclaim every OTHER idle instance on the
	// target GPU. Destroying idle instances interrupts nobody (0 disruption) yet
	// consolidates free space, so future requests need fewer reshapes. This is the
	// surgical advantage MIG Manager cannot replicate: it can only reclaim capacity by
	// draining active workloads too. Active instances outside the region keep running.
	seen := make(map[*migInstanceR]bool, len(victims))
	for _, v := range victims {
		seen[v] = true
	}
	for _, ins := range bestG.instances {
		if !ins.active() && !seen[ins] {
			victims = append(victims, ins)
			seen[ins] = true
		}
	}
	c.destroy(bestG, victims)
	ins := &migInstanceR{profile: p, start: bestStart, workload: id}
	bestG.instances = append(bestG.instances, ins)
	c.inst[id] = ins
	c.metrics.ReconfigCount++
}

// reshapeFullDrain models MIG Manager: it selects the least-busy GPU (fewest active
// workloads — the best-case choice, so the baseline is NOT weakened) and drains the
// ENTIRE device before recreating the requested instance on the now-clean GPU.
func (c *MIGReconfigCluster) reshapeFullDrain(id string, p MIGSliceProfile) {
	var bestG *reconfigGPUState
	bestActive := math.MaxInt32
	for _, g := range c.gpus {
		a := g.activeCount()
		if a < bestActive {
			bestActive = a
			bestG = g
		}
	}
	if bestG == nil {
		return
	}
	all := make([]*migInstanceR, len(bestG.instances))
	copy(all, bestG.instances)
	c.destroy(bestG, all)
	start := p.StartConstraints[0]
	ins := &migInstanceR{profile: p, start: start, workload: id}
	bestG.instances = append(bestG.instances, ins)
	c.inst[id] = ins
	c.metrics.ReconfigCount++
}

// destroy removes victim instances, tallying interrupted active workloads and the
// slices reclaimed. Interrupted workloads are unbound (a later Depart becomes a no-op).
func (c *MIGReconfigCluster) destroy(g *reconfigGPUState, victims []*migInstanceR) {
	for _, v := range victims {
		c.metrics.AffectedSlices += v.size()
		if v.active() {
			c.metrics.TotalDisrupted++
			delete(c.inst, v.workload)
		}
	}
	g.removeInstances(victims)
}
