package evidence

import "github.com/cloudai-fusion/cloudai-fusion/pkg/capability"

// capability_adapter.go bridges the process-wide pkg/capability registry to the
// ledger's CapabilitySource. This is what turns the STATIC, boot-time
// real-vs-simulated declaration into PER-ACTION provenance: every receipt embeds
// the capability snapshot as it stood when the action executed.

// CapabilityRegistry adapts pkg/capability.Default() (or any *capability.Registry)
// to the CapabilitySource interface.
type CapabilityRegistry struct {
	reg *capability.Registry
}

// DefaultCapabilitySource returns a CapabilitySource backed by the process-wide
// default capability registry.
func DefaultCapabilitySource() *CapabilityRegistry {
	return &CapabilityRegistry{reg: capability.Default()}
}

// NewCapabilitySource wraps a specific registry (useful in tests).
func NewCapabilitySource(reg *capability.Registry) *CapabilityRegistry {
	return &CapabilityRegistry{reg: reg}
}

// Snapshot converts the capability registry's records into BackendFacts.
func (c *CapabilityRegistry) Snapshot() []BackendFact {
	backends := c.reg.Snapshot()
	out := make([]BackendFact, 0, len(backends))
	for _, b := range backends {
		out = append(out, BackendFact{
			Component: b.Component,
			Mode:      string(b.Mode),
			Driver:    b.Driver,
			Detail:    b.Detail,
		})
	}
	return out
}

// RunMode returns the registry's active run-mode policy as a string.
func (c *CapabilityRegistry) RunMode() string {
	return c.reg.Policy().String()
}
