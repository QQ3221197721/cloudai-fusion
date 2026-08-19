// Package edrbypass - EDR bypass techniques interface for cross-platform compilation
// Actual implementations are platform-specific (windows)
package edrbypass

import "context"

// EDRBypasser is the main interface for EDR bypass capabilities
type EDRBypasser interface {
	// Name returns the bypass technique name
	Name() string
	// Execute runs the bypass on the target
	Execute(ctx context.Context, target BypassTarget) (*BypassResult, error)
	// Verify checks if the bypass was successful
	Verify(ctx context.Context) (bool, error)
}

// BypassTarget defines the target for a bypass operation
type BypassTarget struct {
	ProcessID   int    `json:"processId,omitempty"`
	ProcessName string `json:"processName,omitempty"`
	DLLPath     string `json:"dllPath,omitempty"`
	PayloadPath string `json:"payloadPath,omitempty"`
}

// BypassResult contains the outcome of a bypass attempt
type BypassResult struct {
	Success     bool   `json:"success"`
	Technique   string `json:"technique"`
	Description string `json:"description"`
	Evidence    []byte `json:"evidence,omitempty"`
	Error       string `json:"error,omitempty"`
}

// BypassCapability represents available bypass types
type BypassCapability string

const (
	AMSIPatch       BypassCapability = "amsi_patch"
	ETWDisable      BypassCapability = "etw_disable"
	ProcessInject   BypassCapability = "process_injection"
	DLLInject       BypassCapability = "dll_injection"
	PowerShellBypass BypassCapability = "powershell_bypass"
)

// Registry holds available bypass implementations
type Registry struct {
	bypassers map[BypassCapability]EDRBypasser
}

// NewRegistry creates a new bypass registry
func NewRegistry() *Registry {
	return &Registry{
		bypassers: make(map[BypassCapability]EDRBypasser),
	}
}

// Register adds a bypasser to the registry
func (r *Registry) Register(capability BypassCapability, bypasser EDRBypasser) {
	r.bypassers[capability] = bypasser
}

// Get retrieves a bypasser by capability
func (r *Registry) Get(capability BypassCapability) (EDRBypasser, bool) {
	b, ok := r.bypassers[capability]
	return b, ok
}

// ListCapabilities returns all registered capabilities
func (r *Registry) ListCapabilities() []BypassCapability {
	caps := make([]BypassCapability, 0, len(r.bypassers))
	for cap := range r.bypassers {
		caps = append(caps, cap)
	}
	return caps
}

// DefaultRegistry is a convenience function for getting the default registry
func DefaultRegistry() *Registry {
	reg := NewRegistry()
	// Platform-specific implementations are registered in init() functions
	// in platform-specific files (edr_bypass_windows.go)
	return reg
}
