// Package plugin provides a plugin architecture for CloudAI Fusion,
// inspired by Kubernetes scheduler framework, Istio MCP, and
// Terraform provider model.
//
// Key concepts:
//   - Plugin: basic unit of extension with lifecycle management
//   - ExtensionPoint: typed hook where plugins attach (Filter, Score, etc.)
//   - Registry: central catalog of registered plugins
//   - Manager: orchestrates plugin lifecycle (init → start → stop)
//   - Webhook: HTTP-based external plugin for out-of-process extensions
package plugin

import (
	"context"
	"fmt"
	"time"
)

// ============================================================================
// Plugin Phase & Status
// ============================================================================

// Phase represents the lifecycle phase of a plugin.
type Phase string

const (
	PhaseCreated      Phase = "created"
	PhaseInitializing Phase = "initializing"
	PhaseReady        Phase = "ready"
	PhaseRunning      Phase = "running"
	PhaseStopping     Phase = "stopping"
	PhaseStopped      Phase = "stopped"
	PhaseError        Phase = "error"
)

// ============================================================================
// Extension Point Types
// ============================================================================

// ExtensionPoint identifies a hook where plugins can be attached.
type ExtensionPoint string

const (
	// --- Scheduler extension points (K8s scheduler framework style) ---
	ExtSchedulerFilter  ExtensionPoint = "scheduler.filter"
	ExtSchedulerScore   ExtensionPoint = "scheduler.score"
	ExtSchedulerPreBind ExtensionPoint = "scheduler.prebind"
	ExtSchedulerBind    ExtensionPoint = "scheduler.bind"
	ExtSchedulerPostBind ExtensionPoint = "scheduler.postbind"
	ExtSchedulerReserve ExtensionPoint = "scheduler.reserve"
	ExtSchedulerPermit  ExtensionPoint = "scheduler.permit"

	// --- Security extension points ---
	ExtSecurityScanner       ExtensionPoint = "security.scanner"
	ExtSecurityPolicyEnforce ExtensionPoint = "security.policy.enforce"
	ExtSecurityAudit         ExtensionPoint = "security.audit"
	ExtSecurityThreatDetect  ExtensionPoint = "security.threat.detect"

	// --- Cloud provider extension points ---
	ExtCloudProvider ExtensionPoint = "cloud.provider"

	// --- Monitoring extension points ---
	ExtMonitorCollector ExtensionPoint = "monitor.collector"
	ExtMonitorAlerter   ExtensionPoint = "monitor.alerter"

	// --- Auth extension points ---
	ExtAuthAuthenticator ExtensionPoint = "auth.authenticator"
	ExtAuthAuthorizer    ExtensionPoint = "auth.authorizer"

	// --- Webhook (catch-all for external HTTP hooks) ---
	ExtWebhookMutating   ExtensionPoint = "webhook.mutating"
	ExtWebhookValidating ExtensionPoint = "webhook.validating"
)

// ============================================================================
// Plugin Metadata
// ============================================================================

// Metadata describes a plugin.
type Metadata struct {
	// Name is the unique identifier for this plugin.
	Name string `json:"name" yaml:"name"`

	// Version follows semver (e.g., "1.2.0").
	Version string `json:"version" yaml:"version"`

	// Description is a human-readable summary.
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Author identifies the plugin author.
	Author string `json:"author,omitempty" yaml:"author,omitempty"`

	// License is the SPDX license identifier.
	License string `json:"license,omitempty" yaml:"license,omitempty"`

	// ExtensionPoints lists the hooks this plugin attaches to.
	ExtensionPoints []ExtensionPoint `json:"extensionPoints" yaml:"extensionPoints"`

	// Dependencies lists plugin names that must be loaded first.
	Dependencies []string `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`

	// Priority determines execution order within an extension point
	// (lower = earlier). Default 100. Range [0, 10000].
	Priority int `json:"priority,omitempty" yaml:"priority,omitempty"`

	// Tags are arbitrary key-value pairs for filtering.
	Tags map[string]string `json:"tags,omitempty" yaml:"tags,omitempty"`
}

// ============================================================================
// Core Plugin Interface
// ============================================================================

// Plugin is the base interface that all plugins must implement.
type Plugin interface {
	// Metadata returns the plugin's metadata.
	Metadata() Metadata

	// Init is called once during plugin initialization.
	// The config map is plugin-specific configuration.
	Init(ctx context.Context, config map[string]interface{}) error

	// Start is called after Init to begin the plugin's operation.
	Start(ctx context.Context) error

	// Stop gracefully shuts down the plugin.
	Stop(ctx context.Context) error

	// Health returns nil if the plugin is healthy.
	Health(ctx context.Context) error
}

// ============================================================================
// Plugin Status (runtime state)
// ============================================================================

// Status describes the runtime state of a loaded plugin.
type Status struct {
	Name        string        `json:"name"`
	Version     string        `json:"version"`
	Phase       Phase         `json:"phase"`
	Healthy     bool          `json:"healthy"`
	StartedAt   *time.Time    `json:"startedAt,omitempty"`
	Uptime      time.Duration `json:"uptime,omitempty"`
	LastError   string        `json:"lastError,omitempty"`
	Extensions  []ExtensionPoint `json:"extensions"`
}

// ============================================================================
// Framework Status (returned by the scheduler plugin chain)
// ============================================================================

// Code is the result code returned by plugins.
type Code int

const (
	// Success means the plugin completed without error.
	Success Code = iota
	// Error means the plugin encountered a fatal error.
	Error
	// Unschedulable means the node cannot host the workload (filter).
	Unschedulable
	// Skip means the plugin chose not to act.
	Skip
	// Wait means the plugin needs to wait (permit phase).
	Wait
)

// Result is returned by extension-point plugin methods.
type Result struct {
	Code    Code   `json:"code"`
	Reason  string `json:"reason,omitempty"`
	Plugin  string `json:"plugin,omitempty"`
}

// IsSuccess returns true if the result indicates success.
func (r *Result) IsSuccess() bool { return r.Code == Success }

// Merge combines multiple results; first non-success wins.
func Merge(results ...*Result) *Result {
	for _, r := range results {
		if r != nil && !r.IsSuccess() {
			return r
		}
	}
	return &Result{Code: Success}
}

// NewResult creates a new result.
func NewResult(code Code, plugin, reason string) *Result {
	return &Result{Code: code, Plugin: plugin, Reason: reason}
}

// SuccessResult returns a success result for the given plugin.
func SuccessResult(plugin string) *Result {
	return &Result{Code: Success, Plugin: plugin}
}

// ErrorResult returns an error result.
func ErrorResult(plugin string, err error) *Result {
	return &Result{Code: Error, Plugin: plugin, Reason: err.Error()}
}

// ============================================================================
// BasePlugin — Embeddable no-op base
// ============================================================================

// BasePlugin provides default no-op implementations for Plugin methods.
// Embed this in concrete plugins to only override what you need.
type BasePlugin struct {
	meta Metadata
}

// NewBasePlugin creates a BasePlugin with the given metadata.
func NewBasePlugin(meta Metadata) BasePlugin {
	if meta.Priority == 0 {
		meta.Priority = 100
	}
	return BasePlugin{meta: meta}
}

func (b *BasePlugin) Metadata() Metadata                                  { return b.meta }
func (b *BasePlugin) Init(_ context.Context, _ map[string]interface{}) error { return nil }
func (b *BasePlugin) Start(_ context.Context) error                        { return nil }
func (b *BasePlugin) Stop(_ context.Context) error                         { return nil }
func (b *BasePlugin) Health(_ context.Context) error                       { return nil }

// ============================================================================
// Plugin Factory
// ============================================================================

// Factory is a function that creates a new Plugin instance.
// It enables lazy construction and is the primary registration mechanism.
type Factory func() (Plugin, error)

// ============================================================================
// Errors
// ============================================================================

// ErrPluginNotFound is returned when a plugin is not in the registry.
type ErrPluginNotFound struct {
	Name string
}

func (e *ErrPluginNotFound) Error() string {
	return fmt.Sprintf("plugin %q not found in registry", e.Name)
}

// ErrPluginAlreadyRegistered is returned on duplicate registration.
type ErrPluginAlreadyRegistered struct {
	Name string
}

func (e *ErrPluginAlreadyRegistered) Error() string {
	return fmt.Sprintf("plugin %q is already registered", e.Name)
}

// ErrExtensionPointMismatch is returned when a plugin doesn't implement the
// required interface for an extension point.
type ErrExtensionPointMismatch struct {
	Plugin    string
	Extension ExtensionPoint
}

func (e *ErrExtensionPointMismatch) Error() string {
	return fmt.Sprintf("plugin %q does not implement extension point %q", e.Plugin, e.Extension)
}
