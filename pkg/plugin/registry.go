package plugin

import (
	"fmt"
	"sort"
	"sync"
)

// ============================================================================
// Registry — central catalog of plugin factories
// ============================================================================

// Registry holds all registered plugin factories and instantiated plugins,
// keyed by extension point for fast lookup.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory                     // name → factory
	plugins   map[string]Plugin                      // name → instantiated plugin
	byExt     map[ExtensionPoint][]string            // extension → ordered plugin names
	disabled  map[string]bool                        // name → disabled
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[string]Factory),
		plugins:   make(map[string]Plugin),
		byExt:     make(map[ExtensionPoint][]string),
		disabled:  make(map[string]bool),
	}
}

// ============================================================================
// Registration
// ============================================================================

// Register adds a plugin factory to the registry.
// The factory is not invoked until Build() is called.
func (r *Registry) Register(name string, factory Factory) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factories[name]; exists {
		return &ErrPluginAlreadyRegistered{Name: name}
	}
	r.factories[name] = factory
	return nil
}

// MustRegister is like Register but panics on error.
func (r *Registry) MustRegister(name string, factory Factory) {
	if err := r.Register(name, factory); err != nil {
		panic(err)
	}
}

// Unregister removes a plugin from the registry.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.factories, name)
	delete(r.plugins, name)
	// Remove from extension point index.
	for ext, names := range r.byExt {
		filtered := names[:0]
		for _, n := range names {
			if n != name {
				filtered = append(filtered, n)
			}
		}
		r.byExt[ext] = filtered
	}
}

// Disable marks a plugin as disabled (will be skipped during Build/execution).
func (r *Registry) Disable(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.disabled[name] = true
}

// Enable re-enables a disabled plugin.
func (r *Registry) Enable(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.disabled, name)
}

// ============================================================================
// Build — instantiate all registered plugins
// ============================================================================

// Build invokes every registered factory, resolves dependencies (topological
// order), and indexes plugins by extension point. Returns the ordered list of
// plugin names.
func (r *Registry) Build() ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 1. Instantiate all non-disabled plugins via their factories.
	pending := make(map[string]Plugin)
	for name, factory := range r.factories {
		if r.disabled[name] {
			continue
		}
		p, err := factory()
		if err != nil {
			return nil, fmt.Errorf("failed to build plugin %q: %w", name, err)
		}
		pending[name] = p
	}

	// 2. Topological sort based on declared dependencies.
	order, err := topoSort(pending)
	if err != nil {
		return nil, err
	}

	// 3. Store instantiated plugins and build extension index.
	r.plugins = pending
	r.byExt = make(map[ExtensionPoint][]string)
	for _, name := range order {
		p := pending[name]
		meta := p.Metadata()
		for _, ext := range meta.ExtensionPoints {
			r.byExt[ext] = append(r.byExt[ext], name)
		}
	}

	// 4. Sort each extension point's plugins by priority.
	for ext, names := range r.byExt {
		sort.SliceStable(names, func(i, j int) bool {
			pi := pending[names[i]].Metadata().Priority
			pj := pending[names[j]].Metadata().Priority
			return pi < pj
		})
		r.byExt[ext] = names
	}

	return order, nil
}

// ============================================================================
// Lookup
// ============================================================================

// Get returns an instantiated plugin by name.
func (r *Registry) Get(name string) (Plugin, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.plugins[name]
	if !ok {
		return nil, &ErrPluginNotFound{Name: name}
	}
	return p, nil
}

// GetByExtension returns all plugins registered for the given extension point,
// ordered by priority (ascending).
func (r *Registry) GetByExtension(ext ExtensionPoint) []Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := r.byExt[ext]
	result := make([]Plugin, 0, len(names))
	for _, name := range names {
		if p, ok := r.plugins[name]; ok {
			result = append(result, p)
		}
	}
	return result
}

// ListAll returns all instantiated plugins.
func (r *Registry) ListAll() []Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Plugin, 0, len(r.plugins))
	for _, p := range r.plugins {
		result = append(result, p)
	}
	return result
}

// Names returns all registered factory names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.factories))
	for n := range r.factories {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ExtensionPoints returns all extension points that have at least one plugin.
func (r *Registry) ExtensionPoints() []ExtensionPoint {
	r.mu.RLock()
	defer r.mu.RUnlock()

	exts := make([]ExtensionPoint, 0, len(r.byExt))
	for ext, names := range r.byExt {
		if len(names) > 0 {
			exts = append(exts, ext)
		}
	}
	return exts
}

// ============================================================================
// Topological Sort (dependency resolution)
// ============================================================================

func topoSort(plugins map[string]Plugin) ([]string, error) {
	// Build adjacency: plugin → its dependencies.
	inDegree := make(map[string]int)
	dependents := make(map[string][]string) // dep → list of plugins that depend on it

	for name := range plugins {
		if _, ok := inDegree[name]; !ok {
			inDegree[name] = 0
		}
	}

	for name, p := range plugins {
		for _, dep := range p.Metadata().Dependencies {
			if _, ok := plugins[dep]; !ok {
				return nil, fmt.Errorf("plugin %q depends on %q which is not registered", name, dep)
			}
			inDegree[name]++
			dependents[dep] = append(dependents[dep], name)
		}
	}

	// Kahn's algorithm.
	var queue []string
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}
	sort.Strings(queue) // deterministic order among equal-degree nodes

	var order []string
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		order = append(order, name)

		for _, dep := range dependents[name] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
				sort.Strings(queue)
			}
		}
	}

	if len(order) != len(plugins) {
		return nil, fmt.Errorf("cyclic dependency detected among plugins")
	}
	return order, nil
}

// ============================================================================
// Global Registry (convenience)
// ============================================================================

var globalRegistry = NewRegistry()

// GlobalRegistry returns the process-wide default registry.
func GlobalRegistry() *Registry { return globalRegistry }
