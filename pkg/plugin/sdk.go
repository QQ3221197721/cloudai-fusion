// Package plugin provides a Plugin Marketplace SDK for CloudAI Fusion.
// Enables plugin developers to build, validate, package, version, and publish
// plugins to the marketplace. Includes a marketplace client for plugin
// discovery, installation, and dependency resolution.
package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Plugin Manifest — Marketplace Metadata
// ============================================================================

// PluginManifest describes a plugin for marketplace distribution.
type PluginManifest struct {
	APIVersion    string            `json:"api_version" yaml:"apiVersion"` // "v1"
	Kind          string            `json:"kind" yaml:"kind"`             // "CloudAIPlugin"
	Metadata      Metadata          `json:"metadata" yaml:"metadata"`
	Spec          PluginSpec        `json:"spec" yaml:"spec"`
	Distribution  DistributionInfo  `json:"distribution" yaml:"distribution"`
}

// PluginSpec describes plugin capabilities and requirements.
type PluginSpec struct {
	MinPlatformVersion string            `json:"min_platform_version" yaml:"minPlatformVersion"`
	MaxPlatformVersion string            `json:"max_platform_version,omitempty" yaml:"maxPlatformVersion,omitempty"`
	GoModule           string            `json:"go_module" yaml:"goModule"`
	EntryPoint         string            `json:"entry_point" yaml:"entryPoint"` // factory function name
	ConfigSchema       map[string]interface{} `json:"config_schema,omitempty" yaml:"configSchema,omitempty"`
	Permissions        []string          `json:"permissions,omitempty" yaml:"permissions,omitempty"`
	Resources          ResourceRequirements `json:"resources,omitempty" yaml:"resources,omitempty"`
}

// ResourceRequirements specifies plugin resource needs.
type ResourceRequirements struct {
	MinMemoryMB int `json:"min_memory_mb,omitempty"`
	MinCPUMilli int `json:"min_cpu_milli,omitempty"`
	NeedsGPU    bool `json:"needs_gpu,omitempty"`
}

// DistributionInfo holds marketplace distribution data.
type DistributionInfo struct {
	Registry    string    `json:"registry" yaml:"registry"`       // e.g., "marketplace.cloudai-fusion.io"
	Repository  string    `json:"repository" yaml:"repository"`   // e.g., "plugins/my-plugin"
	Checksum    string    `json:"checksum" yaml:"checksum"`       // SHA256
	Signature   string    `json:"signature,omitempty" yaml:"signature,omitempty"`
	PublishedAt time.Time `json:"published_at" yaml:"publishedAt"`
	Downloads   int64     `json:"downloads" yaml:"downloads"`
	Rating      float64   `json:"rating" yaml:"rating"`           // 0-5
	Verified    bool      `json:"verified" yaml:"verified"`
}

// ============================================================================
// Manifest Validation
// ============================================================================

// ValidationResult contains the results of manifest validation.
type ValidationResult struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

var semverRe = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[a-zA-Z0-9.]+)?$`)

// ValidateManifest checks a plugin manifest for correctness.
func ValidateManifest(m *PluginManifest) *ValidationResult {
	result := &ValidationResult{Valid: true}

	if m.APIVersion == "" {
		result.Errors = append(result.Errors, "api_version is required")
	}
	if m.Kind != "CloudAIPlugin" {
		result.Errors = append(result.Errors, "kind must be 'CloudAIPlugin'")
	}
	if m.Metadata.Name == "" {
		result.Errors = append(result.Errors, "metadata.name is required")
	}
	if !semverRe.MatchString(m.Metadata.Version) {
		result.Errors = append(result.Errors, fmt.Sprintf("metadata.version '%s' is not valid semver", m.Metadata.Version))
	}
	if len(m.Metadata.ExtensionPoints) == 0 {
		result.Errors = append(result.Errors, "at least one extension point is required")
	}
	if m.Spec.GoModule == "" {
		result.Warnings = append(result.Warnings, "spec.go_module is recommended")
	}
	if m.Spec.EntryPoint == "" {
		result.Warnings = append(result.Warnings, "spec.entry_point is recommended")
	}
	if m.Metadata.Author == "" {
		result.Warnings = append(result.Warnings, "metadata.author is recommended for marketplace")
	}
	if m.Metadata.License == "" {
		result.Warnings = append(result.Warnings, "metadata.license is recommended for marketplace")
	}
	if m.Metadata.Description == "" {
		result.Warnings = append(result.Warnings, "metadata.description improves discoverability")
	}

	result.Valid = len(result.Errors) == 0
	return result
}

// ============================================================================
// Plugin Package — Build & Bundle
// ============================================================================

// PluginPackage represents a built and packaged plugin ready for distribution.
type PluginPackage struct {
	Manifest    PluginManifest `json:"manifest"`
	BinaryHash  string         `json:"binary_hash"`
	SizeBytes   int64          `json:"size_bytes"`
	BuildInfo   BuildInfo      `json:"build_info"`
	BuiltAt     time.Time      `json:"built_at"`
}

// BuildInfo contains plugin build metadata.
type BuildInfo struct {
	GoVersion    string `json:"go_version"`
	Platform     string `json:"platform"`     // e.g., "linux/amd64"
	GitCommit    string `json:"git_commit"`
	BuildTime    string `json:"build_time"`
	Reproducible bool   `json:"reproducible"` // deterministic build
}

// PackageBuilder builds plugin packages from source.
type PackageBuilder struct {
	logger *logrus.Logger
}

// NewPackageBuilder creates a new plugin package builder.
func NewPackageBuilder(logger *logrus.Logger) *PackageBuilder {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &PackageBuilder{logger: logger}
}

// Build creates a plugin package from a manifest and binary data.
func (b *PackageBuilder) Build(manifest PluginManifest, binaryData []byte) (*PluginPackage, error) {
	// Validate manifest
	validation := ValidateManifest(&manifest)
	if !validation.Valid {
		return nil, fmt.Errorf("manifest validation failed: %s", strings.Join(validation.Errors, "; "))
	}

	hash := sha256.Sum256(binaryData)
	manifest.Distribution.Checksum = hex.EncodeToString(hash[:])

	pkg := &PluginPackage{
		Manifest:   manifest,
		BinaryHash: manifest.Distribution.Checksum,
		SizeBytes:  int64(len(binaryData)),
		BuildInfo: BuildInfo{
			GoVersion: "go1.22",
			Platform:  "linux/amd64",
			BuildTime: time.Now().UTC().Format(time.RFC3339),
		},
		BuiltAt: time.Now().UTC(),
	}

	b.logger.WithFields(logrus.Fields{
		"plugin":  manifest.Metadata.Name,
		"version": manifest.Metadata.Version,
		"size":    pkg.SizeBytes,
		"hash":    pkg.BinaryHash[:12],
	}).Info("Plugin package built")

	return pkg, nil
}

// ============================================================================
// Version Manager — Semver Comparison & Resolution
// ============================================================================

// PluginVersionManager manages plugin version history and compatibility.
type PluginVersionManager struct {
	versions map[string][]*PluginManifest // pluginName → sorted versions
	mu       sync.RWMutex
}

// NewPluginVersionManager creates a new version manager.
func NewPluginVersionManager() *PluginVersionManager {
	return &PluginVersionManager{
		versions: make(map[string][]*PluginManifest),
	}
}

// Register adds a new plugin version.
func (vm *PluginVersionManager) Register(manifest *PluginManifest) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	name := manifest.Metadata.Name
	for _, existing := range vm.versions[name] {
		if existing.Metadata.Version == manifest.Metadata.Version {
			return fmt.Errorf("version %s already registered for %s", manifest.Metadata.Version, name)
		}
	}

	vm.versions[name] = append(vm.versions[name], manifest)

	// Sort by version descending
	sort.Slice(vm.versions[name], func(i, j int) bool {
		return compareSemver(vm.versions[name][i].Metadata.Version, vm.versions[name][j].Metadata.Version) > 0
	})

	return nil
}

// GetLatest returns the latest version of a plugin.
func (vm *PluginVersionManager) GetLatest(name string) (*PluginManifest, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	versions := vm.versions[name]
	if len(versions) == 0 {
		return nil, fmt.Errorf("plugin %s not found", name)
	}
	return versions[0], nil
}

// GetVersion returns a specific version.
func (vm *PluginVersionManager) GetVersion(name, version string) (*PluginManifest, error) {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	for _, m := range vm.versions[name] {
		if m.Metadata.Version == version {
			return m, nil
		}
	}
	return nil, fmt.Errorf("version %s not found for plugin %s", version, name)
}

// ListVersions returns all versions of a plugin.
func (vm *PluginVersionManager) ListVersions(name string) []*PluginManifest {
	vm.mu.RLock()
	defer vm.mu.RUnlock()

	versions := vm.versions[name]
	result := make([]*PluginManifest, len(versions))
	copy(result, versions)
	return result
}

// compareSemver compares two semver strings. Returns >0 if a>b, <0 if a<b, 0 if equal.
func compareSemver(a, b string) int {
	a = strings.TrimPrefix(a, "v")
	b = strings.TrimPrefix(b, "v")

	aParts := strings.SplitN(a, ".", 3)
	bParts := strings.SplitN(b, ".", 3)

	for i := 0; i < 3; i++ {
		var av, bv int
		if i < len(aParts) {
			fmt.Sscanf(aParts[i], "%d", &av)
		}
		if i < len(bParts) {
			fmt.Sscanf(bParts[i], "%d", &bv)
		}
		if av != bv {
			return av - bv
		}
	}
	return 0
}

// ============================================================================
// Marketplace Client — Discovery, Search, Install
// ============================================================================

// MarketplaceClient provides access to the plugin marketplace.
type MarketplaceClient struct {
	registryURL string
	plugins     map[string]*PluginManifest // local index: name → latest manifest
	installed   map[string]*PluginPackage  // name → installed package
	versionMgr  *PluginVersionManager
	mu          sync.RWMutex
	logger      *logrus.Logger
}

// MarketplaceSearchResult holds search results.
type MarketplaceSearchResult struct {
	Plugins     []*PluginManifest `json:"plugins"`
	TotalCount  int               `json:"total_count"`
	Query       string            `json:"query"`
	Page        int               `json:"page"`
	PageSize    int               `json:"page_size"`
}

// NewMarketplaceClient creates a new marketplace client.
func NewMarketplaceClient(registryURL string, logger *logrus.Logger) *MarketplaceClient {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &MarketplaceClient{
		registryURL: registryURL,
		plugins:     make(map[string]*PluginManifest),
		installed:   make(map[string]*PluginPackage),
		versionMgr:  NewPluginVersionManager(),
		logger:      logger,
	}
}

// Publish publishes a plugin package to the marketplace.
func (mc *MarketplaceClient) Publish(ctx context.Context, pkg *PluginPackage) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	mc.mu.Lock()
	defer mc.mu.Unlock()

	name := pkg.Manifest.Metadata.Name
	pkg.Manifest.Distribution.PublishedAt = time.Now().UTC()
	pkg.Manifest.Distribution.Registry = mc.registryURL

	mc.plugins[name] = &pkg.Manifest
	if err := mc.versionMgr.Register(&pkg.Manifest); err != nil {
		return err
	}

	mc.logger.WithFields(logrus.Fields{
		"plugin":  name,
		"version": pkg.Manifest.Metadata.Version,
	}).Info("Plugin published to marketplace")
	return nil
}

// Search finds plugins matching a query.
func (mc *MarketplaceClient) Search(ctx context.Context, query string, extensionPoint ExtensionPoint, page, pageSize int) *MarketplaceSearchResult {
	if ctx.Err() != nil {
		return &MarketplaceSearchResult{Query: query}
	}
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	queryLower := strings.ToLower(query)
	matches := make([]*PluginManifest, 0)

	for _, m := range mc.plugins {
		if extensionPoint != "" {
			hasExt := false
			for _, ep := range m.Metadata.ExtensionPoints {
				if ep == extensionPoint {
					hasExt = true
					break
				}
			}
			if !hasExt {
				continue
			}
		}

		if query == "" || strings.Contains(strings.ToLower(m.Metadata.Name), queryLower) ||
			strings.Contains(strings.ToLower(m.Metadata.Description), queryLower) {
			matches = append(matches, m)
		}
	}

	// Pagination
	total := len(matches)
	start := page * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	return &MarketplaceSearchResult{
		Plugins:    matches[start:end],
		TotalCount: total,
		Query:      query,
		Page:       page,
		PageSize:   pageSize,
	}
}

// Install installs a plugin from the marketplace.
func (mc *MarketplaceClient) Install(ctx context.Context, name, version string) (*PluginPackage, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	mc.mu.Lock()
	defer mc.mu.Unlock()

	var manifest *PluginManifest
	var err error
	if version == "" || version == "latest" {
		manifest, err = mc.versionMgr.GetLatest(name)
	} else {
		manifest, err = mc.versionMgr.GetVersion(name, version)
	}
	if err != nil {
		return nil, fmt.Errorf("plugin not found: %w", err)
	}

	// Check dependencies
	for _, dep := range manifest.Metadata.Dependencies {
		if _, ok := mc.installed[dep]; !ok {
			return nil, fmt.Errorf("dependency %q not installed", dep)
		}
	}

	pkg := &PluginPackage{
		Manifest: *manifest,
		BuiltAt:  manifest.Distribution.PublishedAt,
	}
	mc.installed[name] = pkg

	mc.logger.WithFields(logrus.Fields{
		"plugin":  name,
		"version": manifest.Metadata.Version,
	}).Info("Plugin installed from marketplace")
	return pkg, nil
}

// Uninstall removes an installed plugin.
func (mc *MarketplaceClient) Uninstall(name string) error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	// Check if other installed plugins depend on this one
	for installedName, pkg := range mc.installed {
		for _, dep := range pkg.Manifest.Metadata.Dependencies {
			if dep == name {
				return fmt.Errorf("cannot uninstall %s: %s depends on it", name, installedName)
			}
		}
	}

	delete(mc.installed, name)
	mc.logger.WithField("plugin", name).Info("Plugin uninstalled")
	return nil
}

// ListInstalled returns all installed plugins.
func (mc *MarketplaceClient) ListInstalled() []*PluginPackage {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	result := make([]*PluginPackage, 0, len(mc.installed))
	for _, pkg := range mc.installed {
		result = append(result, pkg)
	}
	return result
}

// GetInfo returns detailed info about a marketplace plugin.
func (mc *MarketplaceClient) GetInfo(name string) (json.RawMessage, error) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	m, ok := mc.plugins[name]
	if !ok {
		return nil, fmt.Errorf("plugin %s not found in marketplace", name)
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return data, nil
}
