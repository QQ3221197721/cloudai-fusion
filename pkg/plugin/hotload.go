package plugin

import (
	"context"
	"fmt"
	"runtime/debug"
	"sort"
	"sync"
	"time"
)

// ============================================================================
// Hot-load runtime
//
// This is the Go-plugin path: plugins are ordinary Go types compiled into (or
// linked against) the host binary and handed to the registry as constructed
// values. Add/Remove swap them in and out of the live extension-point index
// without restarting the process — the host keeps serving while a plugin's
// scoring or collection logic changes underneath it.
//
// What this path can and cannot isolate, stated plainly:
//
//   - Panics ARE contained. Every plugin entry point invoked through Invoke or
//     SafeCall recovers, marks the plugin failed, and returns an error, so one
//     misbehaving plugin cannot take down the host goroutine.
//   - Memory and CPU are NOT enforced by this package. Go plugins share the
//     host's address space and OS process; a plugin that allocates without
//     bound will OOM the host. ResourceLimits below records the intended
//     budget and renders it as cgroup v2 control values, but the controller in
//     this file is a mock that does not write to a real cgroup hierarchy.
//     Enforcement requires either the out-of-process WASM runtime or running
//     the host itself inside a cgroup.
//   - Namespaces are LOGICAL. Each plugin gets a name prefix used for resource
//     scoping and capability checks (see security.go), not an OS namespace.
//
// Understating this is deliberate: an operator reading "resource limits" must
// not believe a runaway Go plugin is fenced off.
// ============================================================================

// PluginState is the hot-load lifecycle state of a plugin.
type PluginState string

const (
	// PluginStateUnloaded means the plugin is absent — never added, or removed.
	PluginStateUnloaded PluginState = "unloaded"
	// PluginStateLoading means an Add is in progress (Init/Start running).
	PluginStateLoading PluginState = "loading"
	// PluginStateRunning means the plugin is live and serving its extension points.
	PluginStateRunning PluginState = "running"
	// PluginStateFailed means Init/Start returned an error or a call panicked. The
	// plugin is skipped by HealthyByExtension lookups.
	PluginStateFailed PluginState = "failed"
	// PluginStateUnloading means a Remove is in progress (Stop running).
	PluginStateUnloading PluginState = "unloading"
)

// ============================================================================
// Resource limits (cgroup v2 rendering)
// ============================================================================

// ResourceLimits is the resource budget declared for a plugin.
type ResourceLimits struct {
	// CPUMilli is the CPU budget in milli-cores (1000 = one core). 0 = unset.
	CPUMilli int `json:"cpu_milli,omitempty"`
	// MemoryMB is the memory ceiling in mebibytes. 0 = unset.
	MemoryMB int `json:"memory_mb,omitempty"`
	// PidsMax caps the plugin's thread budget. 0 = unset.
	PidsMax int `json:"pids_max,omitempty"`
}

// IsZero reports whether no limit was declared.
func (l ResourceLimits) IsZero() bool {
	return l.CPUMilli == 0 && l.MemoryMB == 0 && l.PidsMax == 0
}

// Validate rejects negative budgets.
func (l ResourceLimits) Validate() error {
	if l.CPUMilli < 0 {
		return fmt.Errorf("cpu_milli must not be negative, got %d", l.CPUMilli)
	}
	if l.MemoryMB < 0 {
		return fmt.Errorf("memory_mb must not be negative, got %d", l.MemoryMB)
	}
	if l.PidsMax < 0 {
		return fmt.Errorf("pids_max must not be negative, got %d", l.PidsMax)
	}
	return nil
}

// cgroupV2Period is the standard cpu.max enforcement window in microseconds.
const cgroupV2Period = 100000

// CgroupV2Values renders the limits as the exact key/value pairs the kernel's
// cgroup v2 interface files expect:
//
//	cpu.max     "<quota> <period>"  quota = CPUMilli * period / 1000
//	memory.max  "<bytes>"           bytes = MemoryMB * 1Mi
//	pids.max    "<count>"
//
// Unset fields are omitted rather than written as "max", so a caller can tell
// "no opinion" apart from "explicitly unlimited".
func (l ResourceLimits) CgroupV2Values() map[string]string {
	out := make(map[string]string, 3)
	if l.CPUMilli > 0 {
		quota := l.CPUMilli * cgroupV2Period / 1000
		out["cpu.max"] = fmt.Sprintf("%d %d", quota, cgroupV2Period)
	}
	if l.MemoryMB > 0 {
		out["memory.max"] = fmt.Sprintf("%d", int64(l.MemoryMB)*1024*1024)
	}
	if l.PidsMax > 0 {
		out["pids.max"] = fmt.Sprintf("%d", l.PidsMax)
	}
	return out
}

// ResourceController applies a resource budget to a plugin's scope.
type ResourceController interface {
	// Apply installs the limits for a plugin scope, returning an error if the
	// budget cannot be honoured.
	Apply(scope string, limits ResourceLimits) error
	// Release tears down a plugin scope's limits.
	Release(scope string) error
}

// MockCgroupV2Controller records the cgroup v2 control values that *would* be
// written for each plugin scope. It performs no kernel calls and enforces
// nothing; it exists so the hot-load path, the manifest resource declarations,
// and the API surface can be exercised and asserted on any OS. A production
// deployment substitutes a controller that writes to
// /sys/fs/cgroup/<scope>/{cpu.max,memory.max,pids.max}.
type MockCgroupV2Controller struct {
	mu      sync.Mutex
	applied map[string]map[string]string
}

// NewMockCgroupV2Controller creates an in-memory resource controller.
func NewMockCgroupV2Controller() *MockCgroupV2Controller {
	return &MockCgroupV2Controller{
		applied: make(map[string]map[string]string),
	}
}

// Apply records the rendered cgroup v2 values for a scope.
func (m *MockCgroupV2Controller) Apply(scope string, limits ResourceLimits) error {
	if scope == "" {
		return fmt.Errorf("cgroup scope must not be empty")
	}
	if err := limits.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.applied[scope] = limits.CgroupV2Values()
	return nil
}

// Release drops a scope's recorded values.
func (m *MockCgroupV2Controller) Release(scope string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.applied, scope)
	return nil
}

// Values returns the control values recorded for a scope.
func (m *MockCgroupV2Controller) Values(scope string) (map[string]string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.applied[scope]
	if !ok {
		return nil, false
	}
	cp := make(map[string]string, len(v))
	for k, val := range v {
		cp[k] = val
	}
	return cp, true
}

// Scopes returns every scope with recorded limits, sorted.
func (m *MockCgroupV2Controller) Scopes() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.applied))
	for s := range m.applied {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// ============================================================================
// PluginInfo — what List() reports
// ============================================================================

// PluginInfo is a snapshot of one registered plugin.
type PluginInfo struct {
	Name            string           `json:"name"`
	Version         string           `json:"version"`
	State           PluginState      `json:"state"`
	ExtensionPoints []ExtensionPoint `json:"extension_points,omitempty"`
	Priority        int              `json:"priority,omitempty"`
	Namespace       string           `json:"namespace,omitempty"`
	Limits          ResourceLimits   `json:"limits,omitempty"`
	LoadedAt        time.Time        `json:"loaded_at,omitempty"`
	Disabled        bool             `json:"disabled,omitempty"`
}

// ============================================================================
// Panic isolation
// ============================================================================

// ErrPluginPanic reports a recovered panic from plugin code.
type ErrPluginPanic struct {
	Plugin string
	Op     string
	Value  interface{}
	Stack  string
}

func (e *ErrPluginPanic) Error() string {
	return fmt.Sprintf("plugin %q panicked during %s: %v", e.Plugin, e.Op, e.Value)
}

// SafeCall runs plugin code, converting a panic into an *ErrPluginPanic so a
// faulty plugin degrades to an error instead of killing the host process.
//
// This contains control flow, not corruption: a plugin that panics half-way
// through mutating shared state leaves that state as it found it. Panic
// recovery buys the host a chance to quarantine the plugin, nothing more.
func SafeCall(pluginName, op string, fn func() error) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = &ErrPluginPanic{
				Plugin: pluginName,
				Op:     op,
				Value:  rec,
				Stack:  string(debug.Stack()),
			}
		}
	}()
	return fn()
}

// Invoke runs a plugin call through SafeCall and quarantines the plugin
// (StateFailed) when it panics, so later HealthyByExtension lookups skip it.
func (r *Registry) Invoke(name, op string, fn func() error) error {
	err := SafeCall(name, op, fn)
	if _, isPanic := err.(*ErrPluginPanic); isPanic {
		r.MarkFailed(name)
	}
	return err
}

// ============================================================================
// Hot add / remove
// ============================================================================

// AddOptions tunes a hot-add.
type AddOptions struct {
	// Limits is the resource budget for the plugin.
	Limits ResourceLimits
	// Namespace overrides the logical namespace (defaults to the plugin name).
	Namespace string
	// Controller applies the limits. When nil, limits are recorded on the
	// registry but no controller is invoked.
	Controller ResourceController
	// Config is passed to the plugin's Init. Nil skips Init.
	Config map[string]interface{}
	// SkipStart leaves the plugin registered without calling Start.
	SkipStart bool
	// InitTimeout bounds Init and Start (default 30s).
	InitTimeout time.Duration
}

// Add hot-registers an already-constructed plugin and indexes it for its
// extension points, without restarting the host. It is the simple form of
// AddWithOptions: no lifecycle calls, no resource budget.
func (r *Registry) Add(name string, p Plugin) error {
	return r.AddWithOptions(context.Background(), name, p, AddOptions{SkipStart: true})
}

// AddWithOptions hot-registers a plugin, optionally running its Init/Start
// lifecycle under panic recovery and applying a resource budget.
//
// On any failure the plugin is rolled back out of the registry, so a failed
// hot-add cannot leave a half-indexed plugin serving traffic.
func (r *Registry) AddWithOptions(ctx context.Context, name string, p Plugin, opts AddOptions) error {
	if name == "" {
		return fmt.Errorf("plugin name must not be empty")
	}
	if p == nil {
		return fmt.Errorf("plugin %q must not be nil", name)
	}
	if err := opts.Limits.Validate(); err != nil {
		return fmt.Errorf("plugin %q: %w", name, err)
	}

	namespace := opts.Namespace
	if namespace == "" {
		namespace = name
	}

	// Claim the name and mark it loading, so a concurrent Add for the same
	// name loses the race instead of both indexing themselves.
	r.mu.Lock()
	if _, exists := r.factories[name]; exists {
		r.mu.Unlock()
		return &ErrPluginAlreadyRegistered{Name: name}
	}
	if _, exists := r.plugins[name]; exists {
		r.mu.Unlock()
		return &ErrPluginAlreadyRegistered{Name: name}
	}
	r.states[name] = PluginStateLoading
	// Register a factory returning this instance so a later Build() reproduces
	// the hot-added plugin instead of dropping it.
	r.factories[name] = func() (Plugin, error) { return p, nil }
	r.mu.Unlock()

	// Roll back the claim unless we reach the success path.
	committed := false
	defer func() {
		if !committed {
			r.mu.Lock()
			r.unregisterLocked(name)
			r.states[name] = PluginStateFailed
			r.mu.Unlock()
		}
	}()

	timeout := opts.InitTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	// Apply the resource budget before handing control to plugin code.
	if opts.Controller != nil && !opts.Limits.IsZero() {
		if err := opts.Controller.Apply(namespace, opts.Limits); err != nil {
			return fmt.Errorf("plugin %q: apply resource limits: %w", name, err)
		}
	}

	if opts.Config != nil {
		initCtx, cancel := context.WithTimeout(ctx, timeout)
		err := SafeCall(name, "Init", func() error { return p.Init(initCtx, opts.Config) })
		cancel()
		if err != nil {
			return fmt.Errorf("plugin %q init: %w", name, err)
		}
	}

	if !opts.SkipStart {
		startCtx, cancel := context.WithTimeout(ctx, timeout)
		err := SafeCall(name, "Start", func() error { return p.Start(startCtx) })
		cancel()
		if err != nil {
			return fmt.Errorf("plugin %q start: %w", name, err)
		}
	}

	// Commit: publish the instance and index its extension points.
	r.mu.Lock()
	r.plugins[name] = p
	r.limits[name] = opts.Limits
	r.namespaces[name] = namespace
	r.loadedAt[name] = time.Now().UTC()
	r.states[name] = PluginStateRunning
	r.indexPluginLocked(name, p)
	r.mu.Unlock()

	committed = true
	return nil
}

// indexPluginLocked adds a plugin to the extension-point index and restores
// priority ordering. Callers must hold r.mu.
func (r *Registry) indexPluginLocked(name string, p Plugin) {
	for _, ext := range p.Metadata().ExtensionPoints {
		already := false
		for _, n := range r.byExt[ext] {
			if n == name {
				already = true
				break
			}
		}
		if !already {
			r.byExt[ext] = append(r.byExt[ext], name)
		}
		names := r.byExt[ext]
		sort.SliceStable(names, func(i, j int) bool {
			return r.priorityLocked(names[i]) < r.priorityLocked(names[j])
		})
		r.byExt[ext] = names
	}
}

// priorityLocked returns a plugin's priority, or a large sentinel when the
// instance is gone. Callers must hold r.mu.
func (r *Registry) priorityLocked(name string) int {
	if p, ok := r.plugins[name]; ok {
		return p.Metadata().Priority
	}
	return 1 << 30
}

// Remove hot-unregisters a plugin, calling Stop under panic recovery. The
// plugin is dropped from the registry even when Stop fails, so a plugin that
// refuses to shut down cleanly cannot pin itself into the extension index; the
// Stop error is still returned to the caller.
func (r *Registry) Remove(name string) error {
	return r.RemoveWithOptions(context.Background(), name, nil)
}

// RemoveWithOptions is Remove with a resource controller to release.
func (r *Registry) RemoveWithOptions(ctx context.Context, name string, controller ResourceController) error {
	r.mu.Lock()
	p, hasInstance := r.plugins[name]
	_, hasFactory := r.factories[name]
	if !hasInstance && !hasFactory {
		r.mu.Unlock()
		return &ErrPluginNotFound{Name: name}
	}
	namespace := r.namespaces[name]
	if namespace == "" {
		namespace = name
	}
	r.states[name] = PluginStateUnloading
	r.mu.Unlock()

	var stopErr error
	if hasInstance {
		stopCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		stopErr = SafeCall(name, "Stop", func() error { return p.Stop(stopCtx) })
		cancel()
	}

	if controller != nil {
		if err := controller.Release(namespace); err != nil && stopErr == nil {
			stopErr = fmt.Errorf("release resource limits: %w", err)
		}
	}

	r.mu.Lock()
	r.unregisterLocked(name)
	r.states[name] = PluginStateUnloaded
	r.mu.Unlock()

	if stopErr != nil {
		return fmt.Errorf("plugin %q removed with error: %w", name, stopErr)
	}
	return nil
}

// ============================================================================
// State inspection
// ============================================================================

// State returns a plugin's hot-load state. Plugins built via Build() report
// PluginStateRunning; names never seen report PluginStateUnloaded.
func (r *Registry) State(name string) PluginState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.stateLocked(name)
}

func (r *Registry) stateLocked(name string) PluginState {
	if st, ok := r.states[name]; ok {
		return st
	}
	if _, ok := r.plugins[name]; ok {
		return PluginStateRunning
	}
	return PluginStateUnloaded
}

// List returns a snapshot of every plugin known to the registry, sorted by name.
func (r *Registry) List() []PluginInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]bool, len(r.factories)+len(r.plugins))
	for n := range r.factories {
		seen[n] = true
	}
	for n := range r.plugins {
		seen[n] = true
	}
	// Keep quarantined plugins visible even if their instance is gone.
	for n, st := range r.states {
		if st == PluginStateFailed {
			seen[n] = true
		}
	}

	out := make([]PluginInfo, 0, len(seen))
	for name := range seen {
		info := PluginInfo{
			Name:      name,
			State:     r.stateLocked(name),
			Namespace: r.namespaces[name],
			Limits:    r.limits[name],
			LoadedAt:  r.loadedAt[name],
			Disabled:  r.disabled[name],
		}
		if p, ok := r.plugins[name]; ok {
			meta := p.Metadata()
			info.Version = meta.Version
			info.ExtensionPoints = meta.ExtensionPoints
			info.Priority = meta.Priority
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// MarkFailed records that a plugin misbehaved, excluding it from
// HealthyByExtension without removing it from the registry.
func (r *Registry) MarkFailed(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.plugins[name]; ok {
		r.states[name] = PluginStateFailed
	}
}

// HealthyByExtension is GetByExtension restricted to plugins that are not in
// PluginStateFailed — the lookup a live chain should use so a plugin that
// panicked stops receiving traffic.
func (r *Registry) HealthyByExtension(ext ExtensionPoint) []Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := r.byExt[ext]
	result := make([]Plugin, 0, len(names))
	for _, name := range names {
		if r.stateLocked(name) == PluginStateFailed {
			continue
		}
		if p, ok := r.plugins[name]; ok {
			result = append(result, p)
		}
	}
	return result
}

// ResourceLimitsOf returns the budget recorded for a plugin.
func (r *Registry) ResourceLimitsOf(name string) (ResourceLimits, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	l, ok := r.limits[name]
	return l, ok
}

// NamespaceOf returns a plugin's logical namespace.
func (r *Registry) NamespaceOf(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if ns, ok := r.namespaces[name]; ok && ns != "" {
		return ns
	}
	return name
}
