// Package wasm — Plugin ecosystem for WASM extensions.
// Implements multi-language support (Rust/Go/AssemblyScript), WASI sandbox
// isolation with capability-based security, hot-swap mechanism for zero-downtime
// updates, and a plugin marketplace/repository for discovery and distribution.
package wasm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	apperrors "github.com/cloudai-fusion/cloudai-fusion/pkg/errors"
)

// ============================================================================
// Multi-Language Support
// ============================================================================

// PluginLanguage defines supported source languages for WASM plugins
type PluginLanguage string

const (
	LangRust           PluginLanguage = "rust"
	LangGo             PluginLanguage = "go"            // via TinyGo
	LangAssemblyScript PluginLanguage = "assemblyscript"
	LangC              PluginLanguage = "c"
	LangCPP            PluginLanguage = "cpp"
	LangZig            PluginLanguage = "zig"
	LangPython         PluginLanguage = "python"        // via Componentize-py
	LangJavaScript     PluginLanguage = "javascript"    // via javy
)

// LanguageToolchain describes build toolchain for a source language
type LanguageToolchain struct {
	Language       PluginLanguage `json:"language"`
	CompilerCmd    string         `json:"compiler_cmd"`    // e.g., "cargo build --target wasm32-wasi"
	TargetTriple   string         `json:"target_triple"`   // e.g., "wasm32-wasi"
	WASISupport    bool           `json:"wasi_support"`
	ComponentModel bool           `json:"component_model"` // WASM Component Model support
	AvgBinaryKB    int            `json:"avg_binary_kb"`   // typical output size
	Features       []string       `json:"features"`
}

// SupportedToolchains returns all supported language toolchains
func SupportedToolchains() map[PluginLanguage]*LanguageToolchain {
	return map[PluginLanguage]*LanguageToolchain{
		LangRust: {
			Language:       LangRust,
			CompilerCmd:    "cargo build --release --target wasm32-wasi",
			TargetTriple:   "wasm32-wasi",
			WASISupport:    true,
			ComponentModel: true,
			AvgBinaryKB:    256,
			Features:       []string{"wasi-preview2", "component-model", "multi-memory", "simd128", "threads"},
		},
		LangGo: {
			Language:       LangGo,
			CompilerCmd:    "tinygo build -target wasi -o plugin.wasm",
			TargetTriple:   "wasm32-wasi",
			WASISupport:    true,
			ComponentModel: false,
			AvgBinaryKB:    512,
			Features:       []string{"wasi-preview1", "goroutines", "gc"},
		},
		LangAssemblyScript: {
			Language:       LangAssemblyScript,
			CompilerCmd:    "asc assembly/index.ts --target release --outFile plugin.wasm",
			TargetTriple:   "wasm32",
			WASISupport:    true,
			ComponentModel: false,
			AvgBinaryKB:    64,
			Features:       []string{"wasi-preview1", "simd128", "reference-types"},
		},
		LangC: {
			Language:       LangC,
			CompilerCmd:    "clang --target=wasm32-wasi --sysroot=/opt/wasi-sdk/share/wasi-sysroot -O2",
			TargetTriple:   "wasm32-wasi",
			WASISupport:    true,
			ComponentModel: false,
			AvgBinaryKB:    32,
			Features:       []string{"wasi-preview1", "bulk-memory", "simd128"},
		},
		LangPython: {
			Language:       LangPython,
			CompilerCmd:    "componentize-py componentize app -o plugin.wasm",
			TargetTriple:   "wasm32-wasi",
			WASISupport:    true,
			ComponentModel: true,
			AvgBinaryKB:    4096,
			Features:       []string{"wasi-preview2", "component-model"},
		},
		LangJavaScript: {
			Language:       LangJavaScript,
			CompilerCmd:    "javy compile plugin.js -o plugin.wasm",
			TargetTriple:   "wasm32-wasi",
			WASISupport:    true,
			ComponentModel: false,
			AvgBinaryKB:    128,
			Features:       []string{"wasi-preview1", "quickjs-runtime"},
		},
	}
}

// ============================================================================
// WASI Sandbox Isolation
// ============================================================================

// WASICapability defines a WASI capability that can be granted to a plugin
type WASICapability string

const (
	CapFilesystemRead  WASICapability = "filesystem:read"
	CapFilesystemWrite WASICapability = "filesystem:write"
	CapNetworkOutbound WASICapability = "network:outbound"
	CapNetworkInbound  WASICapability = "network:inbound"
	CapEnvironment     WASICapability = "environment"
	CapClockWall       WASICapability = "clock:wall"
	CapClockMonotonic  WASICapability = "clock:monotonic"
	CapRandom          WASICapability = "random"
	CapStdio           WASICapability = "stdio"
	CapHTTPOutgoing    WASICapability = "http:outgoing"
	CapHTTPIncoming    WASICapability = "http:incoming"
	CapKeyValueStore   WASICapability = "keyvalue:store"
	CapMessaging       WASICapability = "messaging"
	CapLogging         WASICapability = "logging"
)

// SandboxProfile defines the security profile for a WASM plugin sandbox
type SandboxProfile struct {
	Name               string            `json:"name"`
	AllowedCapabilities []WASICapability `json:"allowed_capabilities"`
	DeniedCapabilities  []WASICapability `json:"denied_capabilities"`
	MemoryLimitMB      int               `json:"memory_limit_mb"`
	CPULimitMillis     int               `json:"cpu_limit_millis"`
	MaxOpenFiles       int               `json:"max_open_files"`
	MaxNetworkConns    int               `json:"max_network_connections"`
	AllowedHosts       []string          `json:"allowed_hosts"`         // network allowlist
	DeniedHosts        []string          `json:"denied_hosts"`          // network denylist
	FSMountPoints      []FSMount         `json:"fs_mount_points"`       // virtual filesystem
	EnvVars            map[string]string `json:"env_vars"`              // allowed env vars
	ExecutionTimeoutMs int               `json:"execution_timeout_ms"`
	EnableProfiling    bool              `json:"enable_profiling"`
}

// FSMount defines a virtual filesystem mount for the sandbox
type FSMount struct {
	GuestPath  string `json:"guest_path"`  // path inside WASM sandbox
	HostPath   string `json:"host_path"`   // path on host (or virtual)
	ReadOnly   bool   `json:"read_only"`
}

// PredefinedSandboxProfiles returns built-in security profiles
func PredefinedSandboxProfiles() map[string]*SandboxProfile {
	return map[string]*SandboxProfile{
		"minimal": {
			Name:               "minimal",
			AllowedCapabilities: []WASICapability{CapClockMonotonic, CapRandom, CapLogging},
			MemoryLimitMB:      32,
			CPULimitMillis:     100,
			MaxOpenFiles:       0,
			MaxNetworkConns:    0,
			ExecutionTimeoutMs: 5000,
		},
		"standard": {
			Name:               "standard",
			AllowedCapabilities: []WASICapability{CapClockMonotonic, CapClockWall, CapRandom, CapStdio, CapLogging, CapEnvironment, CapHTTPOutgoing, CapKeyValueStore},
			MemoryLimitMB:      128,
			CPULimitMillis:     1000,
			MaxOpenFiles:       10,
			MaxNetworkConns:    5,
			ExecutionTimeoutMs: 30000,
		},
		"privileged": {
			Name:               "privileged",
			AllowedCapabilities: []WASICapability{CapFilesystemRead, CapFilesystemWrite, CapNetworkOutbound, CapNetworkInbound, CapEnvironment, CapClockWall, CapClockMonotonic, CapRandom, CapStdio, CapHTTPOutgoing, CapHTTPIncoming, CapKeyValueStore, CapMessaging, CapLogging},
			MemoryLimitMB:      512,
			CPULimitMillis:     4000,
			MaxOpenFiles:       100,
			MaxNetworkConns:    50,
			ExecutionTimeoutMs: 60000,
		},
	}
}

// SandboxManager manages WASI sandbox profiles and runtime isolation
type SandboxManager struct {
	profiles map[string]*SandboxProfile
	active   map[string]*SandboxProfile // instanceID -> assigned profile
	mu       sync.RWMutex
	logger   *logrus.Logger
}

// NewSandboxManager creates a new sandbox manager with predefined profiles
func NewSandboxManager(logger *logrus.Logger) *SandboxManager {
	return &SandboxManager{
		profiles: PredefinedSandboxProfiles(),
		active:   make(map[string]*SandboxProfile),
		logger:   logger,
	}
}

// RegisterProfile adds a custom sandbox profile
func (s *SandboxManager) RegisterProfile(profile *SandboxProfile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.profiles[profile.Name] = profile
	s.logger.WithField("profile", profile.Name).Info("Registered sandbox profile")
}

// AssignProfile assigns a sandbox profile to a plugin instance
func (s *SandboxManager) AssignProfile(instanceID, profileName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	profile, ok := s.profiles[profileName]
	if !ok {
		return fmt.Errorf("sandbox profile %q not found", profileName)
	}
	s.active[instanceID] = profile
	return nil
}

// CheckCapability verifies if an instance has a specific capability
func (s *SandboxManager) CheckCapability(instanceID string, cap WASICapability) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	profile, ok := s.active[instanceID]
	if !ok {
		return false
	}

	// Check denied list first
	for _, denied := range profile.DeniedCapabilities {
		if denied == cap {
			return false
		}
	}

	// Check allowed list
	for _, allowed := range profile.AllowedCapabilities {
		if allowed == cap {
			return true
		}
	}
	return false
}

// GetProfile returns the sandbox profile for an instance
func (s *SandboxManager) GetProfile(instanceID string) *SandboxProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.active[instanceID]
}

// ListProfiles returns all available profiles
func (s *SandboxManager) ListProfiles() []*SandboxProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*SandboxProfile, 0, len(s.profiles))
	for _, p := range s.profiles {
		result = append(result, p)
	}
	return result
}

// ============================================================================
// Hot-Swap Mechanism
// ============================================================================

// HotSwapConfig configures the hot-swap mechanism
type HotSwapConfig struct {
	DrainTimeoutSec    int  `json:"drain_timeout_sec"`
	HealthCheckAfterMs int  `json:"health_check_after_ms"`
	RollbackOnFailure  bool `json:"rollback_on_failure"`
	MaxConcurrentSwaps int  `json:"max_concurrent_swaps"`
	GracefulDrainEnabled bool `json:"graceful_drain_enabled"`
}

// DefaultHotSwapConfig returns production defaults
func DefaultHotSwapConfig() HotSwapConfig {
	return HotSwapConfig{
		DrainTimeoutSec:    30,
		HealthCheckAfterMs: 5000,
		RollbackOnFailure:  true,
		MaxConcurrentSwaps: 5,
		GracefulDrainEnabled: true,
	}
}

// SwapState represents the state of a hot-swap operation
type SwapState string

const (
	SwapPending    SwapState = "pending"
	SwapDraining   SwapState = "draining"     // draining old instance
	SwapLoading    SwapState = "loading"       // loading new module
	SwapValidating SwapState = "validating"    // health check on new instance
	SwapSwitching  SwapState = "switching"     // atomic pointer swap
	SwapComplete   SwapState = "complete"
	SwapRolledBack SwapState = "rolled_back"
	SwapFailed     SwapState = "failed"
)

// SwapOperation represents an in-progress hot-swap
type SwapOperation struct {
	ID              string    `json:"id"`
	InstanceID      string    `json:"instance_id"`
	OldModuleID     string    `json:"old_module_id"`
	NewModuleID     string    `json:"new_module_id"`
	OldVersion      string    `json:"old_version"`
	NewVersion      string    `json:"new_version"`
	State           SwapState `json:"state"`
	StartedAt       time.Time `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	RequestsDrained int64     `json:"requests_drained"`
	Error           string    `json:"error,omitempty"`
}

// HotSwapManager manages zero-downtime plugin updates
type HotSwapManager struct {
	config     HotSwapConfig
	operations map[string]*SwapOperation // opID -> operation
	mu         sync.RWMutex
	logger     *logrus.Logger
}

// NewHotSwapManager creates a new hot-swap manager
func NewHotSwapManager(cfg HotSwapConfig, logger *logrus.Logger) *HotSwapManager {
	return &HotSwapManager{
		config:     cfg,
		operations: make(map[string]*SwapOperation),
		logger:     logger,
	}
}

// InitiateSwap starts a hot-swap operation
func (h *HotSwapManager) InitiateSwap(ctx context.Context, instanceID, oldModuleID, newModuleID, oldVer, newVer string) (*SwapOperation, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	// Check concurrent swap limit
	activeSwaps := 0
	for _, op := range h.operations {
		if op.State != SwapComplete && op.State != SwapFailed && op.State != SwapRolledBack {
			activeSwaps++
		}
	}
	if activeSwaps >= h.config.MaxConcurrentSwaps {
		return nil, fmt.Errorf("max concurrent swaps (%d) reached", h.config.MaxConcurrentSwaps)
	}

	op := &SwapOperation{
		ID:          fmt.Sprintf("swap-%s-%d", instanceID, time.Now().UnixNano()),
		InstanceID:  instanceID,
		OldModuleID: oldModuleID,
		NewModuleID: newModuleID,
		OldVersion:  oldVer,
		NewVersion:  newVer,
		State:       SwapPending,
		StartedAt:   time.Now().UTC(),
	}
	h.operations[op.ID] = op

	h.logger.WithFields(logrus.Fields{
		"swap_id":  op.ID,
		"instance": instanceID,
		"from":     oldVer,
		"to":       newVer,
	}).Info("Hot-swap operation initiated")

	return op, nil
}

// AdvanceSwapState progresses a swap to the next state
func (h *HotSwapManager) AdvanceSwapState(opID string, newState SwapState, errMsg string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	op, ok := h.operations[opID]
	if !ok {
		return fmt.Errorf("swap operation %s not found", opID)
	}

	op.State = newState
	if errMsg != "" {
		op.Error = errMsg
	}

	if newState == SwapComplete || newState == SwapFailed || newState == SwapRolledBack {
		now := time.Now().UTC()
		op.CompletedAt = &now
	}

	h.logger.WithFields(logrus.Fields{
		"swap_id": opID,
		"state":   newState,
	}).Info("Hot-swap state advanced")

	return nil
}

// Rollback reverts a swap to the old module
func (h *HotSwapManager) Rollback(opID string) error {
	return h.AdvanceSwapState(opID, SwapRolledBack, "manual rollback requested")
}

// GetOperation returns a swap operation
func (h *HotSwapManager) GetOperation(opID string) *SwapOperation {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.operations[opID]
}

// ListOperations returns all swap operations
func (h *HotSwapManager) ListOperations() []*SwapOperation {
	h.mu.RLock()
	defer h.mu.RUnlock()
	result := make([]*SwapOperation, 0, len(h.operations))
	for _, op := range h.operations {
		result = append(result, op)
	}
	return result
}

// ============================================================================
// Plugin Marketplace / Repository
// ============================================================================

// MarketplaceConfig configures the plugin marketplace
type MarketplaceConfig struct {
	RegistryURL       string `json:"registry_url"`        // OCI registry for plugins
	CacheDir          string `json:"cache_dir"`
	VerifySignatures  bool   `json:"verify_signatures"`
	AllowUntrusted    bool   `json:"allow_untrusted"`
	AutoUpdateEnabled bool   `json:"auto_update_enabled"`
	UpdateIntervalMin int    `json:"update_interval_min"`
}

// DefaultMarketplaceConfig returns production defaults
func DefaultMarketplaceConfig() MarketplaceConfig {
	return MarketplaceConfig{
		RegistryURL:       "oci://registry.cloudai-fusion.io/plugins",
		CacheDir:          "/var/cache/cloudai-fusion/plugins",
		VerifySignatures:  true,
		AllowUntrusted:    false,
		AutoUpdateEnabled: false,
		UpdateIntervalMin: 60,
	}
}

// PluginManifest describes a plugin in the marketplace
type PluginManifest struct {
	Name             string            `json:"name"`
	DisplayName      string            `json:"display_name"`
	Version          string            `json:"version"`
	Description      string            `json:"description"`
	Author           string            `json:"author"`
	License          string            `json:"license"`
	Language         PluginLanguage    `json:"language"`
	Runtime          RuntimeType       `json:"runtime"`
	Capabilities     []WASICapability  `json:"capabilities"`
	SandboxProfile   string            `json:"sandbox_profile"`
	MinPlatformVer   string            `json:"min_platform_version"`
	Dependencies     []PluginDep       `json:"dependencies,omitempty"`
	Tags             []string          `json:"tags"`
	Category         string            `json:"category"` // scheduler, security, monitoring, auth, integration
	Downloads        int64             `json:"downloads"`
	Rating           float64           `json:"rating"` // 0-5
	Verified         bool              `json:"verified"`
	Deprecated       bool              `json:"deprecated"`
	Homepage         string            `json:"homepage,omitempty"`
	Repository       string            `json:"repository,omitempty"`
	BinarySizeBytes  int64             `json:"binary_size_bytes"`
	ChecksumSHA256   string            `json:"checksum_sha256"`
	PublishedAt      time.Time         `json:"published_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

// PluginDep represents a plugin dependency
type PluginDep struct {
	Name       string `json:"name"`
	MinVersion string `json:"min_version"`
}

// MarketplaceSearchResult contains search results from the marketplace
type MarketplaceSearchResult struct {
	Plugins    []*PluginManifest `json:"plugins"`
	TotalCount int               `json:"total_count"`
	Page       int               `json:"page"`
	PageSize   int               `json:"page_size"`
}

// InstalledPlugin tracks a locally installed plugin
type InstalledPlugin struct {
	Manifest       *PluginManifest `json:"manifest"`
	InstalledAt    time.Time       `json:"installed_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
	LocalPath      string          `json:"local_path"`
	IsActive       bool            `json:"is_active"`
	InstanceCount  int             `json:"instance_count"`
	AutoUpdate     bool            `json:"auto_update"`
}

// PluginMarketplace manages plugin discovery, installation, and updates
type PluginMarketplace struct {
	config    MarketplaceConfig
	catalog   map[string]*PluginManifest  // name -> latest manifest
	installed map[string]*InstalledPlugin // name -> installed info
	mu        sync.RWMutex
	logger    *logrus.Logger
}

// NewPluginMarketplace creates a new marketplace manager
func NewPluginMarketplace(cfg MarketplaceConfig, logger *logrus.Logger) *PluginMarketplace {
	mp := &PluginMarketplace{
		config:    cfg,
		catalog:   make(map[string]*PluginManifest),
		installed: make(map[string]*InstalledPlugin),
		logger:    logger,
	}
	mp.seedBuiltinPlugins()
	return mp
}

// seedBuiltinPlugins populates the catalog with built-in / example plugins
func (mp *PluginMarketplace) seedBuiltinPlugins() {
	builtins := []*PluginManifest{
		{
			Name: "gpu-topology-filter", DisplayName: "GPU Topology Filter",
			Version: "1.0.0", Description: "Scheduler filter for GPU topology-aware placement",
			Author: "CloudAI Fusion", License: "Apache-2.0", Language: LangRust,
			Runtime: RuntimeWasmtime, Category: "scheduler",
			Capabilities: []WASICapability{CapLogging, CapKeyValueStore},
			SandboxProfile: "minimal", Tags: []string{"gpu", "scheduler", "topology"},
			Verified: true, Rating: 4.8, Downloads: 15200,
			BinarySizeBytes: 256 * 1024, PublishedAt: time.Now().UTC().Add(-90 * 24 * time.Hour),
		},
		{
			Name: "opa-policy-enforcer", DisplayName: "OPA Policy Enforcer",
			Version: "2.1.0", Description: "Open Policy Agent integration for admission control",
			Author: "CloudAI Fusion", License: "Apache-2.0", Language: LangGo,
			Runtime: RuntimeWasmEdge, Category: "security",
			Capabilities: []WASICapability{CapLogging, CapHTTPOutgoing, CapEnvironment},
			SandboxProfile: "standard", Tags: []string{"opa", "policy", "admission", "security"},
			Verified: true, Rating: 4.9, Downloads: 32100,
			BinarySizeBytes: 512 * 1024, PublishedAt: time.Now().UTC().Add(-60 * 24 * time.Hour),
		},
		{
			Name: "cost-anomaly-detector", DisplayName: "Cost Anomaly Detector",
			Version: "1.2.0", Description: "ML-based cost anomaly detection for multi-cloud",
			Author: "Community", License: "MIT", Language: LangAssemblyScript,
			Runtime: RuntimeSpin, Category: "monitoring",
			Capabilities: []WASICapability{CapLogging, CapClockWall, CapKeyValueStore},
			SandboxProfile: "standard", Tags: []string{"cost", "anomaly", "ml", "monitoring"},
			Verified: false, Rating: 4.2, Downloads: 8700,
			BinarySizeBytes: 64 * 1024, PublishedAt: time.Now().UTC().Add(-30 * 24 * time.Hour),
		},
		{
			Name: "custom-authn-ldap", DisplayName: "LDAP Authenticator",
			Version: "1.0.0", Description: "LDAP/Active Directory authentication plugin",
			Author: "Community", License: "Apache-2.0", Language: LangRust,
			Runtime: RuntimeWasmtime, Category: "auth",
			Capabilities: []WASICapability{CapLogging, CapNetworkOutbound, CapEnvironment},
			SandboxProfile: "standard", Tags: []string{"ldap", "auth", "active-directory"},
			Verified: true, Rating: 4.5, Downloads: 12400,
			BinarySizeBytes: 384 * 1024, PublishedAt: time.Now().UTC().Add(-45 * 24 * time.Hour),
		},
		{
			Name: "prometheus-exporter", DisplayName: "Prometheus Metrics Exporter",
			Version: "1.3.0", Description: "Export custom metrics to Prometheus",
			Author: "CloudAI Fusion", License: "Apache-2.0", Language: LangRust,
			Runtime: RuntimeWasmtime, Category: "monitoring",
			Capabilities: []WASICapability{CapLogging, CapHTTPIncoming, CapHTTPOutgoing},
			SandboxProfile: "standard", Tags: []string{"prometheus", "metrics", "monitoring"},
			Verified: true, Rating: 4.7, Downloads: 28500,
			BinarySizeBytes: 196 * 1024, PublishedAt: time.Now().UTC().Add(-75 * 24 * time.Hour),
		},
	}

	for _, p := range builtins {
		checksum := sha256.Sum256([]byte(p.Name + p.Version))
		p.ChecksumSHA256 = hex.EncodeToString(checksum[:])
		p.UpdatedAt = p.PublishedAt
		mp.catalog[p.Name] = p
	}
}

// Search searches the marketplace catalog
func (mp *PluginMarketplace) Search(query, category string, verifiedOnly bool, page, pageSize int) *MarketplaceSearchResult {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	if pageSize <= 0 {
		pageSize = 20
	}

	var matches []*PluginManifest
	for _, m := range mp.catalog {
		if m.Deprecated {
			continue
		}
		if verifiedOnly && !m.Verified {
			continue
		}
		if category != "" && m.Category != category {
			continue
		}
		if query != "" {
			found := false
			if containsLower(m.Name, query) || containsLower(m.DisplayName, query) || containsLower(m.Description, query) {
				found = true
			}
			for _, tag := range m.Tags {
				if containsLower(tag, query) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		matches = append(matches, m)
	}

	// Sort by downloads descending
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Downloads > matches[j].Downloads
	})

	total := len(matches)
	start := page * pageSize
	if start >= total {
		return &MarketplaceSearchResult{Plugins: nil, TotalCount: total, Page: page, PageSize: pageSize}
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	return &MarketplaceSearchResult{
		Plugins:    matches[start:end],
		TotalCount: total,
		Page:       page,
		PageSize:   pageSize,
	}
}

// Install installs a plugin from the marketplace
func (mp *PluginMarketplace) Install(ctx context.Context, pluginName string) (*InstalledPlugin, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}
	mp.mu.Lock()
	defer mp.mu.Unlock()

	manifest, ok := mp.catalog[pluginName]
	if !ok {
		return nil, fmt.Errorf("plugin %q not found in marketplace", pluginName)
	}

	if !mp.config.AllowUntrusted && !manifest.Verified {
		return nil, fmt.Errorf("plugin %q is not verified; set allow_untrusted=true to install", pluginName)
	}

	installed := &InstalledPlugin{
		Manifest:    manifest,
		InstalledAt: time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		LocalPath:   fmt.Sprintf("%s/%s/%s/plugin.wasm", mp.config.CacheDir, pluginName, manifest.Version),
		IsActive:    true,
		AutoUpdate:  mp.config.AutoUpdateEnabled,
	}

	mp.installed[pluginName] = installed

	mp.logger.WithFields(logrus.Fields{
		"plugin":   pluginName,
		"version":  manifest.Version,
		"language": manifest.Language,
		"size_kb":  manifest.BinarySizeBytes / 1024,
	}).Info("Plugin installed from marketplace")

	return installed, nil
}

// Uninstall removes an installed plugin
func (mp *PluginMarketplace) Uninstall(pluginName string) error {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if _, ok := mp.installed[pluginName]; !ok {
		return fmt.Errorf("plugin %q is not installed", pluginName)
	}

	delete(mp.installed, pluginName)
	mp.logger.WithField("plugin", pluginName).Info("Plugin uninstalled")
	return nil
}

// ListInstalled returns all locally installed plugins
func (mp *PluginMarketplace) ListInstalled() []*InstalledPlugin {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	result := make([]*InstalledPlugin, 0, len(mp.installed))
	for _, inst := range mp.installed {
		result = append(result, inst)
	}
	return result
}

// GetManifest returns the manifest of a specific plugin
func (mp *PluginMarketplace) GetManifest(pluginName string) *PluginManifest {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return mp.catalog[pluginName]
}

// PublishPlugin adds a new plugin to the catalog (for authors)
func (mp *PluginMarketplace) PublishPlugin(manifest *PluginManifest) error {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if manifest.Name == "" || manifest.Version == "" {
		return fmt.Errorf("plugin name and version are required")
	}

	manifest.PublishedAt = time.Now().UTC()
	manifest.UpdatedAt = manifest.PublishedAt
	if manifest.ChecksumSHA256 == "" {
		checksum := sha256.Sum256([]byte(manifest.Name + manifest.Version))
		manifest.ChecksumSHA256 = hex.EncodeToString(checksum[:])
	}

	mp.catalog[manifest.Name] = manifest

	mp.logger.WithFields(logrus.Fields{
		"plugin":  manifest.Name,
		"version": manifest.Version,
		"author":  manifest.Author,
	}).Info("Plugin published to marketplace")

	return nil
}

// containsLower checks if s contains substr (case-insensitive via simple ASCII lower)
func containsLower(s, substr string) bool {
	sl := toLower(s)
	subl := toLower(substr)
	return len(subl) <= len(sl) && findSubstring(sl, subl)
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		b[i] = c
	}
	return string(b)
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ============================================================================
// Plugin Ecosystem Hub — Unified Access Point
// ============================================================================

// PluginEcosystemHub aggregates all WASM plugin ecosystem subsystems
type PluginEcosystemHub struct {
	Sandbox     *SandboxManager
	HotSwap     *HotSwapManager
	Marketplace *PluginMarketplace
	Toolchains  map[PluginLanguage]*LanguageToolchain
	logger      *logrus.Logger
}

// PluginEcosystemConfig holds all plugin ecosystem configuration
type PluginEcosystemConfig struct {
	HotSwap     HotSwapConfig     `json:"hot_swap"`
	Marketplace MarketplaceConfig `json:"marketplace"`
}

// DefaultPluginEcosystemConfig returns production defaults
func DefaultPluginEcosystemConfig() PluginEcosystemConfig {
	return PluginEcosystemConfig{
		HotSwap:     DefaultHotSwapConfig(),
		Marketplace: DefaultMarketplaceConfig(),
	}
}

// NewPluginEcosystemHub creates and initializes all plugin ecosystem subsystems
func NewPluginEcosystemHub(cfg PluginEcosystemConfig, logger *logrus.Logger) *PluginEcosystemHub {
	return &PluginEcosystemHub{
		Sandbox:     NewSandboxManager(logger),
		HotSwap:     NewHotSwapManager(cfg.HotSwap, logger),
		Marketplace: NewPluginMarketplace(cfg.Marketplace, logger),
		Toolchains:  SupportedToolchains(),
		logger:      logger,
	}
}

// GetStatus returns a unified status of all plugin ecosystem subsystems
func (h *PluginEcosystemHub) GetStatus() map[string]interface{} {
	installed := h.Marketplace.ListInstalled()
	catalog := h.Marketplace.Search("", "", false, 0, 1000)

	return map[string]interface{}{
		"sandbox_profiles":    len(h.Sandbox.profiles),
		"active_sandboxes":    len(h.Sandbox.active),
		"supported_languages": len(h.Toolchains),
		"hot_swap_operations": len(h.HotSwap.operations),
		"marketplace_plugins": catalog.TotalCount,
		"installed_plugins":   len(installed),
	}
}

// HandlePluginEcosystem handles plugin ecosystem requests via action routing
func (h *PluginEcosystemHub) HandlePluginEcosystem(ctx context.Context, action string, params map[string]interface{}) (interface{}, error) {
	if err := apperrors.CheckContext(ctx); err != nil {
		return nil, err
	}

	switch action {
	case "search":
		query, _ := params["query"].(string)
		category, _ := params["category"].(string)
		verifiedOnly, _ := params["verified_only"].(bool)
		return h.Marketplace.Search(query, category, verifiedOnly, 0, 20), nil

	case "install":
		name, _ := params["name"].(string)
		return h.Marketplace.Install(ctx, name)

	case "uninstall":
		name, _ := params["name"].(string)
		return nil, h.Marketplace.Uninstall(name)

	case "installed":
		return h.Marketplace.ListInstalled(), nil

	case "manifest":
		name, _ := params["name"].(string)
		return h.Marketplace.GetManifest(name), nil

	case "publish":
		data, _ := json.Marshal(params)
		var manifest PluginManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return nil, fmt.Errorf("invalid manifest: %w", err)
		}
		return nil, h.Marketplace.PublishPlugin(&manifest)

	case "sandbox_profiles":
		return h.Sandbox.ListProfiles(), nil

	case "sandbox_assign":
		instanceID, _ := params["instance_id"].(string)
		profileName, _ := params["profile"].(string)
		return nil, h.Sandbox.AssignProfile(instanceID, profileName)

	case "sandbox_check":
		instanceID, _ := params["instance_id"].(string)
		capStr, _ := params["capability"].(string)
		return h.Sandbox.CheckCapability(instanceID, WASICapability(capStr)), nil

	case "toolchains":
		return h.Toolchains, nil

	case "hotswap_initiate":
		instanceID, _ := params["instance_id"].(string)
		oldModule, _ := params["old_module_id"].(string)
		newModule, _ := params["new_module_id"].(string)
		oldVer, _ := params["old_version"].(string)
		newVer, _ := params["new_version"].(string)
		return h.HotSwap.InitiateSwap(ctx, instanceID, oldModule, newModule, oldVer, newVer)

	case "hotswap_advance":
		opID, _ := params["op_id"].(string)
		stateStr, _ := params["state"].(string)
		errMsg, _ := params["error"].(string)
		return nil, h.HotSwap.AdvanceSwapState(opID, SwapState(stateStr), errMsg)

	case "hotswap_rollback":
		opID, _ := params["op_id"].(string)
		return nil, h.HotSwap.Rollback(opID)

	case "hotswap_list":
		return h.HotSwap.ListOperations(), nil

	case "status":
		return h.GetStatus(), nil

	default:
		return nil, fmt.Errorf("unknown plugin ecosystem action: %s", action)
	}
}
