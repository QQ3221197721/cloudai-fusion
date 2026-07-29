// Package wellreadiness is the honesty instrument for the AISecOps "deep wells".
//
// The platform already proves the honesty of its external-dependency backends via
// pkg/capability (real-vs-simulated + Enforce + /api/v1/capabilities). This
// package extends that same discipline to the WELL layer: every deep well reports
// a machine-checkable readiness record, the platform refuses to boot in
// production if any well OVERCLAIMS its maturity, and GET /api/v1/wells publishes
// the honest snapshot.
//
// It is the structural cure for "a well exists as a tested library but is never
// wired / never connected to the fabric, yet is described as delivered". After
// this framework, such a lie fails the production boot and turns CI red.
package wellreadiness

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/runmode"
)

// Maturity is a well's readiness on a strictly increasing ladder. A well may only
// CLAIM a level it can actually prove (see Status.Validate).
type Maturity int

const (
	// M0Scaffold: types/interfaces exist, nothing wired.
	M0Scaffold Maturity = iota
	// M1Wired: instantiated at the composition root and reachable.
	M1Wired
	// M2RealBackend: at least one real backend is active (capability-reported).
	M2RealBackend
	// M3FabricConnected: really publishes/subscribes on the event fabric.
	M3FabricConnected
	// M4CIVerified: the well's real path is exercised in CI.
	M4CIVerified
	// M5ProductionHardened: depth meets a mature-solution baseline.
	M5ProductionHardened
)

// String returns a stable label for the maturity level.
func (m Maturity) String() string {
	switch m {
	case M0Scaffold:
		return "M0-scaffold"
	case M1Wired:
		return "M1-wired"
	case M2RealBackend:
		return "M2-real-backend"
	case M3FabricConnected:
		return "M3-fabric-connected"
	case M4CIVerified:
		return "M4-ci-verified"
	case M5ProductionHardened:
		return "M5-production-hardened"
	default:
		return fmt.Sprintf("M?-%d", int(m))
	}
}

// Backend modes for Status.BackendMode.
const (
	BackendReal      = "real"
	BackendSimulated = "simulated"
	BackendNone      = "none"
)

// Status is one deep well's readiness record. Every field must be backed by a
// fact the platform can check — never a hand-written marketing claim.
type Status struct {
	Well            int       `json:"well"` // 1..16
	Name            string    `json:"name"`
	Claimed         Maturity  `json:"claimed_maturity"`
	Wired           bool      `json:"wired"`
	BackendMode     string    `json:"backend_mode"` // real | simulated | none
	FabricConnected bool      `json:"fabric_connected"`
	EvidenceBacked  bool      `json:"evidence_backed"`
	Detail          string    `json:"detail,omitempty"`
	RegisteredAt    time.Time `json:"registered_at"`
}

// Validate enforces the self-consistency laws that make an overclaim impossible
// to hide: a well cannot claim a maturity it does not structurally satisfy.
func (s Status) Validate() error {
	if s.Well < 1 || s.Well > 16 {
		return fmt.Errorf("wellreadiness: well %d out of range [1,16]", s.Well)
	}
	if s.Claimed >= M1Wired && !s.Wired {
		return fmt.Errorf("wellreadiness: well %q claims %s but is not wired", s.Name, s.Claimed)
	}
	if s.Claimed >= M2RealBackend && s.BackendMode != BackendReal {
		return fmt.Errorf("wellreadiness: well %q claims %s but backend_mode=%q (want real)", s.Name, s.Claimed, s.BackendMode)
	}
	if s.Claimed >= M3FabricConnected && !s.FabricConnected {
		return fmt.Errorf("wellreadiness: well %q claims %s but is not fabric-connected", s.Name, s.Claimed)
	}
	return nil
}

// Registry tracks well readiness under a run-mode policy.
type Registry struct {
	mu     sync.RWMutex
	policy runmode.RunMode
	wells  map[int]Status
}

// NewRegistry creates a registry with the given policy.
func NewRegistry(policy runmode.RunMode) *Registry {
	if !policy.Valid() {
		policy = runmode.Simulation
	}
	return &Registry{policy: policy, wells: make(map[int]Status)}
}

// SetPolicy updates the run-mode policy.
func (r *Registry) SetPolicy(p runmode.RunMode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p.Valid() {
		r.policy = p
	}
}

// Policy returns the current policy.
func (r *Registry) Policy() runmode.RunMode {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.policy
}

// Report records a well's readiness. The record is stored regardless (so
// /api/v1/wells and Enforce always see the truth), but under the Production
// policy an overclaiming (invalid) record returns an error so fail-fast callers
// can propagate it.
func (r *Registry) Report(s Status) error {
	if s.RegisteredAt.IsZero() {
		s.RegisteredAt = time.Now().UTC()
	}
	r.mu.Lock()
	r.wells[s.Well] = s
	policy := r.policy
	r.mu.Unlock()

	if err := s.Validate(); err != nil && policy.IsProduction() {
		return err
	}
	return nil
}

// Snapshot returns all readiness records sorted by well number.
func (r *Registry) Snapshot() []Status {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Status, 0, len(r.wells))
	for _, s := range r.wells {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Well < out[j].Well })
	return out
}

// Enforce is the boot-time backstop: under the Production policy it aggregates
// every overclaiming well into a single error, so a dishonest boot is refused.
func (r *Registry) Enforce() error {
	if !r.Policy().IsProduction() {
		return nil
	}
	var violations []string
	for _, s := range r.Snapshot() {
		if err := s.Validate(); err != nil {
			violations = append(violations, err.Error())
		}
	}
	if len(violations) == 0 {
		return nil
	}
	return fmt.Errorf("wellreadiness: run_mode=production but %d well(s) overclaim: %v", len(violations), violations)
}

// Reset clears all records (used by tests).
func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.wells = make(map[int]Status)
}

// ============================================================================
// Package-level default registry (shared across the process)
// ============================================================================

var defaultRegistry = NewRegistry(runmode.Simulation)

// Default returns the process-wide registry.
func Default() *Registry { return defaultRegistry }

// SetPolicy sets the policy on the default registry.
func SetPolicy(p runmode.RunMode) { defaultRegistry.SetPolicy(p) }

// Policy returns the default registry's policy.
func Policy() runmode.RunMode { return defaultRegistry.Policy() }

// Report records on the default registry (see Registry.Report).
func Report(s Status) error { return defaultRegistry.Report(s) }

// Snapshot returns the default registry's records.
func Snapshot() []Status { return defaultRegistry.Snapshot() }

// Enforce runs the boot-time backstop on the default registry.
func Enforce() error { return defaultRegistry.Enforce() }

// Reset clears the default registry (used by tests).
func Reset() { defaultRegistry.Reset() }
