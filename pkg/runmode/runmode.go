// Package runmode defines the platform operating mode, the single lever that
// governs how CloudAI Fusion treats simulated/in-memory fallbacks.
//
// Historically every external-dependency boundary silently degraded to an
// in-memory or simulated implementation when the real backend was missing.
// That made it impossible to tell — at startup or at runtime — whether the
// platform was running on real infrastructure or on fakes. RunMode fixes that
// systemically:
//
//   - Simulation: everything may be simulated (local dev, unit/integration tests).
//   - Degraded:   real backends preferred; simulated backends are ALLOWED but
//     surfaced loudly (warnings + /capabilities + degraded readiness). Staging.
//   - Production: simulated backends are FORBIDDEN. Any component that can only
//     offer a simulated backend must fail fast rather than lie. Production.
package runmode

import "strings"

// RunMode is the platform operating mode.
type RunMode string

const (
	// Simulation permits any simulated/in-memory backend without escalation.
	Simulation RunMode = "simulation"
	// Degraded prefers real backends but tolerates simulated ones (surfaced).
	Degraded RunMode = "degraded"
	// Production forbids simulated backends; components must fail fast.
	Production RunMode = "production"
)

// Parse converts a string (case-insensitive) into a RunMode.
// Unknown or empty values fall back to Simulation (the safe local default);
// callers that want production strictness must set it explicitly.
func Parse(s string) RunMode {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "production", "prod":
		return Production
	case "degraded", "staging":
		return Degraded
	case "simulation", "sim", "development", "dev", "test", "":
		return Simulation
	default:
		return Simulation
	}
}

// FromEnvName derives a default RunMode from the general environment name
// (config.env) when run_mode is not set explicitly. Production-like envs map
// to Production so strictness is the default there, not an opt-in.
func FromEnvName(env string) RunMode {
	switch strings.ToLower(strings.TrimSpace(env)) {
	case "production", "prod":
		return Production
	case "staging", "stage":
		return Degraded
	default:
		return Simulation
	}
}

// Valid reports whether m is a recognized mode.
func (m RunMode) Valid() bool {
	switch m {
	case Simulation, Degraded, Production:
		return true
	default:
		return false
	}
}

// IsProduction reports whether simulated backends are forbidden.
func (m RunMode) IsProduction() bool { return m == Production }

// AllowsSimulation reports whether simulated backends may be used at all.
func (m RunMode) AllowsSimulation() bool { return m != Production }

// String returns the mode as a string.
func (m RunMode) String() string { return string(m) }
