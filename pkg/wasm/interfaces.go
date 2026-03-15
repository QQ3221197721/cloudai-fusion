package wasm

import "context"

// WasmService defines the contract for WebAssembly workload management.
// The API layer depends on this interface rather than the concrete *Manager,
// enabling mock implementations for handler unit testing and pluggable backends.
type WasmService interface {
	// ListModules returns all registered Wasm modules.
	ListModules(ctx context.Context) ([]*WasmModule, error)

	// RegisterModule registers a new Wasm module.
	RegisterModule(ctx context.Context, module *WasmModule) error

	// Deploy deploys Wasm instances from a module.
	Deploy(ctx context.Context, req *WasmDeployRequest) ([]*WasmInstance, error)

	// ListInstances returns all running Wasm instances.
	ListInstances(ctx context.Context) ([]*WasmInstance, error)

	// StopInstance stops a running Wasm instance.
	StopInstance(ctx context.Context, instanceID string) error

	// DeleteInstance removes a Wasm instance.
	DeleteInstance(ctx context.Context, instanceID string) error

	// InstanceHealthCheck checks the health of a Wasm instance.
	InstanceHealthCheck(ctx context.Context, instanceID string) (*InstanceHealth, error)

	// GetMetrics returns Wasm runtime metrics.
	GetMetrics(ctx context.Context) (*RuntimeMetrics, error)

	// CheckRuntimeHealth checks if the Wasm runtime is healthy.
	CheckRuntimeHealth(ctx context.Context) (*RuntimeHealth, error)

	// HandlePluginEcosystem dispatches plugin ecosystem actions
	// (toolchains, sandbox, hot-swap, marketplace).
	HandlePluginEcosystem(ctx context.Context, action string, params map[string]interface{}) (interface{}, error)

	// PluginEcosystemHub returns the underlying plugin ecosystem hub.
	PluginEcosystemHub() *PluginEcosystemHub
}

// Compile-time interface satisfaction check.
var _ WasmService = (*Manager)(nil)
