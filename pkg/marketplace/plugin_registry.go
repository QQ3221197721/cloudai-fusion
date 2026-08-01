// Package marketplace - Plugin discovery and registration API
package marketplace

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// PluginRegistry manages plugin discovery and registration
type PluginRegistry struct {
	pm        *PluginManager
	cache     sync.Map // Capability -> PluginManifests
	mu        sync.RWMutex
	lastScan  time.Time
	scanDelay time.Duration
}

// NewPluginRegistry creates new registry instance
func NewPluginRegistry(pm *PluginManager, scanDir string) (*PluginRegistry, error) {
	if scanDir == "" {
		scanDir = pm.config.PluginDir
	}
	
	reg := &PluginRegistry{
		pm:       pm,
		scanDelay: 60 * time.Second,
	}
	
	// Initial scan
	if err := reg.ScanPlugins(context.Background()); err != nil {
		return nil, err
	}
	
	return reg, nil
}

// ScanPlugins scans plugin directory for new manifests
func (pr *PluginRegistry) ScanPlugins(ctx context.Context) error {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	
	pluginDir := pr.pm.config.PluginDir
	
	// Find all manifest files
	err := filepath.WalkDir(pluginDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		
		// Skip non-regular files and directories
		if d.IsDir() {
			return nil
		}
		
		// Only process manifest.json files
		if d.Name() != "manifest.json" {
			return nil
		}
		
		// Parse manifest
		manifest, err := pr.pm.parseManifest(path)
		if err != nil {
			return fmt.Errorf("failed to parse %s: %w", path, err)
		}
		
		// Check if already loaded
		if _, exists := pr.pm.plugins.Load(manifest.ID); !exists {
			// Load plugin
			if err := pr.pm.LoadPlugin(ctx, path); err != nil {
				return err
			}
			
			// Update cache
			pr.cacheStore(manifest)
		}
		
		return nil
	})
	
	if err != nil {
		return err
	}
	
	pr.lastScan = time.Now()
	return nil
}

// GetPluginsByCapability returns plugins supporting a capability
func (pr *PluginRegistry) GetPluginsByCapability(cap Capability) ([]*PluginManifest, error) {
	plugins := make([]*PluginManifest, 0)
	
	for _, cap := range AllExtensionPoints {
		if cap.HasCapability(cap) {
			pluginIDs := pr.pm.getCapabilityPluginIDs(cap)
			for _, id := range pluginIDs {
				if p, exists := pr.pm.plugins.Load(id); exists {
					plugins = append(plugins, p.(*Plugin).Manifest)
				}
			}
		}
	}
	
	return plugins, nil
}

// ListAvailableCapabilities lists all capabilities currently available
func (pr *PluginRegistry) ListAvailableCapabilities() []Capability {
	available := make([]Capability, 0)
	
	pr.pm.plugins.Range(func(key, value interface{}) bool {
		plugin := value.(*Plugin)
		for _, cap := range plugin.Manifest.Capabilities {
			available = append(available, cap)
		}
		return true
	})
	
	return uniqueCapabilities(available)
}

// GetPluginStatus returns status of specific plugin
func (pr *PluginRegistry) GetPluginStatus(id string) (*PluginStatus, error) {
	if plugin, exists := pr.pm.plugins.Load(id); exists {
		status := plugin.(*Plugin).Status
		return &status, nil
	}
	
	return nil, fmt.Errorf("plugin not found: %s", id)
}

// EnablePlugin enables a disabled plugin
func (pr *PluginRegistry) EnablePlugin(id string) error {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	
	if plugin, exists := pr.pm.plugins.Load(id); exists {
		p := plugin.(*Plugin)
		if p.Status == StatusStopped {
			p.Status = StatusRunning
			pr.pm.hooks.InitializeHooks(p, p.Manifest.ExtensionPoints)
		}
		return nil
	}
	
	return fmt.Errorf("plugin not found: %s", id)
}

// DisablePlugin disables a running plugin
func (pr *PluginRegistry) DisablePlugin(id string) error {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	
	if plugin, exists := pr.pm.plugins.Load(id); exists {
		p := plugin.(*Plugin)
		if p.Status == StatusRunning {
			pr.pm.hooks.UninitializeHooks(p)
			p.Status = StatusStopped
		}
		return nil
	}
	
	return fmt.Errorf("plugin not found: %s", id)
}

// cacheStore caches plugin manifest
func (pr *PluginRegistry) cacheStore(manifest *PluginManifest) {
	for _, cap := range manifest.Capabilities {
		key := fmt.Sprintf("%s:%s", cap, manifest.ID)
		pr.cache.Store(key, manifest)
	}
}

// Helper functions
func (pr *PluginRegistry) getCapabilityPluginIDs(cap Capability) []string {
	var ids []string
	pr.pm.plugins.Range(func(key, value interface{}) bool {
		p := value.(*Plugin)
		for _, c := range p.Manifest.Capabilities {
			if c == cap {
				ids = append(ids, key.(string))
				break
			}
		}
		return true
	})
	return ids
}

func (cap ExtensionPoint) HasCapability(c Capability) bool {
	for _, cc := range cap.Categories {
		if cc == c {
			return true
		}
	}
	return false
}

func uniqueCapabilities(caps []Capability) []Capability {
	seen := make(map[Capability]bool)
	result := make([]Capability, 0)
	
	for _, c := range caps {
		if !seen[c] {
			seen[c] = true
			result = append(result, c)
		}
	}
	
	return result
}
