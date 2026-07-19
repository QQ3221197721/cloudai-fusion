// Package capability provides a process-wide registry that records, for every
// external-dependency-backed subsystem, whether it is running on a REAL backend
// or a SIMULATED/in-memory fallback — and enforces the run-mode policy.
//
// This is the cure for the "silent degradation" anti-pattern: instead of each
// boundary quietly faking a backend and returning success, every factory now
// Reports its true nature here. In Production, reporting a simulated backend is
// an error (fail fast); in Degraded it is allowed but surfaced; in Simulation
// it is expected. The registry also powers the honest GET /api/v1/capabilities
// endpoint and the /readyz gate.
package capability

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/runmode"
)

// Mode describes how a subsystem is currently backed.
type Mode string

const (
	// ModeReal means the subsystem uses a real external backend.
	ModeReal Mode = "real"
	// ModeSimulated means the subsystem uses an in-memory/simulated fallback.
	ModeSimulated Mode = "simulated"
	// ModeDisabled means the subsystem is intentionally turned off.
	ModeDisabled Mode = "disabled"
)

// Backend is a single subsystem's capability record.
type Backend struct {
	Component    string    `json:"component"` // e.g. "cache", "messaging", "scheduler.nodes"
	Mode         Mode      `json:"mode"`      // real | simulated | disabled
	Driver       string    `json:"driver"`    // e.g. "redis", "memory", "k8s", "sim"
	Detail       string    `json:"detail,omitempty"`
	RegisteredAt time.Time `json:"registered_at"`
}

// Registry tracks capability records under a run-mode policy.
type Registry struct {
	mu       sync.RWMutex
	policy   runmode.RunMode
	backends map[string]Backend
}

// NewRegistry creates a registry with the given policy.
func NewRegistry(policy runmode.RunMode) *Registry {
	if !policy.Valid() {
		policy = runmode.Simulation
	}
	return &Registry{policy: policy, backends: make(map[string]Backend)}
}

// SetPolicy updates the run-mode policy.
func (r *Registry) SetPolicy(p runmode.RunMode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p.Valid() {
		r.policy = p
	}
}

// Policy returns the current run-mode policy.
func (r *Registry) Policy() runmode.RunMode {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.policy
}

// Report records a subsystem's backing mode and enforces the policy.
// It returns a non-nil error when a simulated backend is reported under the
// Production policy, so callers that can fail fast should propagate it.
// The record is stored regardless, so /capabilities and Enforce() always see
// the truth (even for callers that cannot return an error).
func (r *Registry) Report(component, driver string, mode Mode, detail string) error {
	r.mu.Lock()
	r.backends[component] = Backend{
		Component:    component,
		Mode:         mode,
		Driver:       driver,
		Detail:       detail,
		RegisteredAt: time.Now().UTC(),
	}
	policy := r.policy
	r.mu.Unlock()

	if mode == ModeSimulated && policy.IsProduction() {
		return fmt.Errorf("capability %q is simulated (driver=%q) but run_mode=production forbids simulated backends: %s",
			component, driver, detail)
	}
	return nil
}

// MustReal is a convenience for call sites that require a real backend under
// the current policy. It returns an error in Production when real is false.
// In non-production modes it records the (possibly simulated) backend and
// returns nil so the caller may proceed with the fallback.
func (r *Registry) MustReal(component, driver string, real bool, detail string) error {
	mode := ModeReal
	if !real {
		mode = ModeSimulated
	}
	return r.Report(component, driver, mode, detail)
}

// Snapshot returns all capability records sorted by component name.
func (r *Registry) Snapshot() []Backend {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Backend, 0, len(r.backends))
	for _, b := range r.backends {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Component < out[j].Component })
	return out
}

// Simulated returns the subset of records currently backed by a simulation.
func (r *Registry) Simulated() []Backend {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Backend, 0)
	for _, b := range r.backends {
		if b.Mode == ModeSimulated {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Component < out[j].Component })
	return out
}

// HasSimulated reports whether any registered subsystem is simulated.
func (r *Registry) HasSimulated() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, b := range r.backends {
		if b.Mode == ModeSimulated {
			return true
		}
	}
	return false
}

// Enforce is the boot-time backstop: under the Production policy it returns an
// aggregated error if ANY subsystem is simulated. Call it at the composition
// root after all subsystems have been initialized to refuse a dishonest boot.
func (r *Registry) Enforce() error {
	if !r.Policy().IsProduction() {
		return nil
	}
	sim := r.Simulated()
	if len(sim) == 0 {
		return nil
	}
	names := make([]string, 0, len(sim))
	for _, b := range sim {
		names = append(names, fmt.Sprintf("%s(driver=%s)", b.Component, b.Driver))
	}
	return fmt.Errorf("run_mode=production but %d subsystem(s) are simulated: %v — configure real backends or lower run_mode", len(sim), names)
}

// Reset clears all records (used by tests).
func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.backends = make(map[string]Backend)
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
func Report(component, driver string, mode Mode, detail string) error {
	return defaultRegistry.Report(component, driver, mode, detail)
}

// MustReal records on the default registry (see Registry.MustReal).
func MustReal(component, driver string, real bool, detail string) error {
	return defaultRegistry.MustReal(component, driver, real, detail)
}

// Snapshot returns the default registry's records.
func Snapshot() []Backend { return defaultRegistry.Snapshot() }

// Simulated returns the default registry's simulated records.
func Simulated() []Backend { return defaultRegistry.Simulated() }

// HasSimulated reports whether the default registry has any simulated backend.
func HasSimulated() bool { return defaultRegistry.HasSimulated() }

// Enforce runs the boot-time backstop on the default registry.
func Enforce() error { return defaultRegistry.Enforce() }

// Reset clears the default registry (used by tests).
func Reset() { defaultRegistry.Reset() }
