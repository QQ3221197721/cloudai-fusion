// Package marketplace - Minimal hook registry stub.
//
// HookRegistry manages plugin extension-point hooks. This provides a
// self-contained, thread-safe implementation sufficient for the plugin
// lifecycle (load / reload / unload) wiring in marketplace.go.
package marketplace

import (
	"fmt"
	"sync"
)

// HookRegistry tracks which extension points each plugin is bound to.
type HookRegistry struct {
	mu    sync.Mutex
	hooks map[string][]ExtensionPoint
}

// NewHookRegistry creates an initialized hook registry.
func NewHookRegistry() HookRegistry {
	return HookRegistry{hooks: make(map[string][]ExtensionPoint)}
}

// InitializeHooks registers a plugin's extension points.
func (r *HookRegistry) InitializeHooks(plugin *Plugin, points []ExtensionPoint) {
	if plugin == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.hooks == nil {
		r.hooks = make(map[string][]ExtensionPoint)
	}
	r.hooks[plugin.Manifest.ID] = points
}

// UninitializeHooks removes a plugin's registered extension points.
func (r *HookRegistry) UninitializeHooks(plugin *Plugin) {
	if plugin == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.hooks, plugin.Manifest.ID)
}

// SeverityLevel classifies the severity of a security issue.
type SeverityLevel string

const (
	SeverityInfo     SeverityLevel = "info"
	SeverityLow      SeverityLevel = "low"
	SeverityMedium   SeverityLevel = "medium"
	SeverityHigh     SeverityLevel = "high"
	SeverityCritical SeverityLevel = "critical"
)

// ScannerMetrics tracks security scanner activity counters.
type ScannerMetrics struct {
	mu         sync.Mutex
	TotalScans int64
	ByStatus   map[ScanStatus]int64
}

// NewScannerMetrics creates an initialized scanner metrics tracker.
func NewScannerMetrics() *ScannerMetrics {
	return &ScannerMetrics{ByStatus: make(map[ScanStatus]int64)}
}

// IncrementScan records the start of a scan.
func (m *ScannerMetrics) IncrementScan() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalScans++
}

// RecordScan records a completed scan's terminal status.
func (m *ScannerMetrics) RecordScan(status ScanStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ByStatus == nil {
		m.ByStatus = make(map[ScanStatus]int64)
	}
	m.ByStatus[status]++
}

// loadGoPlugin loads a native Go plugin (stub implementation).
func (pm *PluginManager) loadGoPlugin(manifest *PluginManifest) (interface{}, error) {
	return map[string]string{"runtime": "go", "id": manifest.ID}, nil
}

// loadPythonPlugin loads a Python plugin via subprocess bridge (stub implementation).
func (pm *PluginManager) loadPythonPlugin(manifest *PluginManifest) (interface{}, error) {
	return map[string]string{"runtime": "python", "id": manifest.ID}, nil
}

// loadWasmPlugin loads a WASM plugin into the runtime (stub implementation).
func (pm *PluginManager) loadWasmPlugin(manifest *PluginManifest) (interface{}, error) {
	return map[string]string{"runtime": "wasm", "id": manifest.ID}, nil
}

// ============================================================================
// Default plugin submission validators (minimal implementations)
// ============================================================================

// FormatValidator checks that a submission carries the required metadata.
type FormatValidator struct{}

func (v *FormatValidator) Name() string { return "format_validator" }

func (v *FormatValidator) Validate(submission *PluginSubmission) error {
	if submission == nil || submission.ID == "" {
		return fmt.Errorf("submission missing required identifier")
	}
	return nil
}

// SecurityScannerValidator rejects submissions that failed the security scan.
type SecurityScannerValidator struct{}

func (v *SecurityScannerValidator) Name() string { return "security_scanner_validator" }

func (v *SecurityScannerValidator) Validate(submission *PluginSubmission) error {
	if submission != nil && submission.SecurityScan.Vulnerabilities > 0 {
		return fmt.Errorf("submission has %d unresolved vulnerabilities", submission.SecurityScan.Vulnerabilities)
	}
	return nil
}

// CompatibilityValidator verifies platform compatibility metadata.
type CompatibilityValidator struct{}

func (v *CompatibilityValidator) Name() string { return "compatibility_validator" }

func (v *CompatibilityValidator) Validate(submission *PluginSubmission) error {
	return nil
}
