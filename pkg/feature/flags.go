// Package feature provides a runtime feature flag system for CloudAI Fusion.
// Flags can be toggled without restart via API or environment variables.
// Supports percentage-based rollout, profiles, and flag persistence to database.
//
// Feature Profiles:
//   - minimal:  Core only (API/Scheduler/Agent/DB/Redis). No advanced modules.
//   - standard: (default) Includes monitoring, edge, wasm, security, cost optimisation.
//   - full:     Everything enabled including experimental features.
//
// Environment Override:
//
//	CLOUDAI_FF_<KEY>=true|false   (e.g. CLOUDAI_FF_EDGE_COMPUTING=false)
//	CLOUDAI_FEATURE_PROFILE=minimal|standard|full
package feature

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ---------------------------------------------------------------------------
// Category groups feature flags for display and profile management.
// ---------------------------------------------------------------------------

// Category classifies feature flags for grouping and display.
type Category string

const (
	CategoryCompute     Category = "compute"       // GPU sharing, WASM, edge
	CategoryAI          Category = "ai"             // RL scheduler, LLM ops, AIOps
	CategoryNetworking  Category = "networking"     // Service mesh, gRPC, WebSocket
	CategoryObservation Category = "observability"  // Tracing, monitoring, audit
	CategorySecurity    Category = "security"       // Threat detection, compliance
	CategoryFinOps      Category = "finops"         // Cost optimisation, spot
	CategoryPlatform    Category = "platform"       // Multi-cluster, GitOps, plugins
)

// Profile is a predefined combination of feature flags.
type Profile string

const (
	ProfileMinimal  Profile = "minimal"  // Core only — fastest startup, lowest resource usage
	ProfileStandard Profile = "standard" // Recommended default — production-ready feature set
	ProfileFull     Profile = "full"     // Everything enabled, including experimental
)

// Flag represents a single feature flag.
type Flag struct {
	Key         string            `json:"key"`
	Enabled     bool              `json:"enabled"`
	Description string            `json:"description,omitempty"`
	Category    Category          `json:"category"`
	Percentage  int               `json:"percentage"`           // 0-100 for gradual rollout
	MinProfile  Profile           `json:"min_profile,omitempty"` // Minimum profile that enables this flag
	Metadata    map[string]string `json:"metadata,omitempty"`
	UpdatedBy   string            `json:"updated_by,omitempty"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// Store is an optional persistence backend for feature flags.
type Store interface {
	LoadFlags(ctx context.Context) ([]Flag, error)
	SaveFlag(ctx context.Context, flag *Flag) error
	DeleteFlag(ctx context.Context, key string) error
}

// Manager manages feature flags with thread-safe read/write access.
type Manager struct {
	flags  map[string]*Flag
	mu     sync.RWMutex
	store  Store
	logger *logrus.Logger
}

// Config configures the feature flag manager.
type Config struct {
	Store  Store
	Logger *logrus.Logger
}

// NewManager creates a new feature flag manager with built-in defaults.
// Load order (later overrides earlier):
//  1. Built-in defaults (ProfileStandard)
//  2. Feature profile (CLOUDAI_FEATURE_PROFILE env var)
//  3. Environment variable overrides (CLOUDAI_FF_<KEY>=true|false)
//  4. Database persistence (if store is available)
func NewManager(cfg Config) *Manager {
	logger := cfg.Logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	m := &Manager{
		flags:  make(map[string]*Flag),
		store:  cfg.Store,
		logger: logger,
	}

	// 1. Register built-in feature flags (defaults = ProfileStandard)
	m.registerDefaults()

	// 2. Apply feature profile from env (if set)
	profile := ActiveProfile()
	if profile != ProfileStandard {
		m.ApplyProfile(profile)
	}

	// 3. Override from individual environment variables (CLOUDAI_FF_<KEY>=true|false)
	m.loadFromEnv()

	// 4. Load from database if available
	if m.store != nil {
		m.loadFromStore()
	}

	m.logger.WithFields(logrus.Fields{
		"profile": string(profile),
		"total":   len(m.flags),
		"enabled": m.EnabledCount(),
	}).Info("Feature flag manager initialised")

	return m
}

// profileOrder defines which profiles include which levels.
// minimal < standard < full
var profileOrder = map[Profile]int{
	ProfileMinimal:  0,
	ProfileStandard: 1,
	ProfileFull:     2,
}

// profileIncludes returns true if the active profile includes the given minimum profile.
func profileIncludes(active, minRequired Profile) bool {
	return profileOrder[active] >= profileOrder[minRequired]
}

// registerDefaults registers the built-in feature flags with safe defaults.
// The default Enabled state corresponds to ProfileStandard.
func (m *Manager) registerDefaults() {
	defaults := []Flag{
		// ---- Compute ----
		{Key: "gpu_sharing_mps", Enabled: false, Category: CategoryCompute, MinProfile: ProfileFull, Description: "NVIDIA MPS GPU sharing for inference workloads", Percentage: 100},
		{Key: "gpu_sharing_mig", Enabled: false, Category: CategoryCompute, MinProfile: ProfileFull, Description: "NVIDIA MIG GPU partitioning for A100/H100", Percentage: 100},
		{Key: "topology_aware_scheduling", Enabled: true, Category: CategoryCompute, MinProfile: ProfileStandard, Description: "GPU topology-aware workload placement", Percentage: 100},
		{Key: "wasm_runtime", Enabled: true, Category: CategoryCompute, MinProfile: ProfileStandard, Description: "WebAssembly serverless runtime", Percentage: 100},
		{Key: "edge_computing", Enabled: true, Category: CategoryCompute, MinProfile: ProfileStandard, Description: "Edge-cloud collaborative computing", Percentage: 100},

		// ---- AI ----
		{Key: "rl_scheduler", Enabled: false, Category: CategoryAI, MinProfile: ProfileFull, Description: "Reinforcement learning scheduler optimizer", Percentage: 100},
		{Key: "auto_scaling", Enabled: false, Category: CategoryAI, MinProfile: ProfileFull, Description: "ML-based predictive auto-scaling", Percentage: 50},
		{Key: "llm_operations_agent", Enabled: false, Category: CategoryAI, MinProfile: ProfileFull, Description: "LLM-powered operations agent for incident analysis", Percentage: 100},
		{Key: "aiops_self_healing", Enabled: false, Category: CategoryAI, MinProfile: ProfileFull, Description: "AIOps self-healing automation", Percentage: 100},
		{Key: "capacity_planning", Enabled: false, Category: CategoryAI, MinProfile: ProfileFull, Description: "AI-driven capacity planning and forecasting", Percentage: 100},

		// ---- Networking ----
		{Key: "service_mesh", Enabled: true, Category: CategoryNetworking, MinProfile: ProfileStandard, Description: "Service mesh (eBPF/Cilium/Istio Ambient)", Percentage: 100},
		{Key: "grpc_internal", Enabled: false, Category: CategoryNetworking, MinProfile: ProfileFull, Description: "gRPC for internal service communication", Percentage: 100},
		{Key: "websocket_events", Enabled: true, Category: CategoryNetworking, MinProfile: ProfileStandard, Description: "WebSocket real-time event push", Percentage: 100},

		// ---- Observability ----
		{Key: "distributed_tracing", Enabled: true, Category: CategoryObservation, MinProfile: ProfileStandard, Description: "OpenTelemetry distributed tracing (Jaeger)", Percentage: 100},
		{Key: "advanced_monitoring", Enabled: true, Category: CategoryObservation, MinProfile: ProfileStandard, Description: "Prometheus/Grafana monitoring stack", Percentage: 100},
		{Key: "audit_logging", Enabled: true, Category: CategoryObservation, MinProfile: ProfileStandard, Description: "Security audit logging", Percentage: 100},

		// ---- Security ----
		{Key: "advanced_threat_detection", Enabled: true, Category: CategorySecurity, MinProfile: ProfileStandard, Description: "Rule-based threat detection engine", Percentage: 100},
		{Key: "compliance_scanning", Enabled: true, Category: CategorySecurity, MinProfile: ProfileStandard, Description: "CIS/SOC2/PCI compliance scanning", Percentage: 100},

		// ---- FinOps ----
		{Key: "cost_optimization", Enabled: true, Category: CategoryFinOps, MinProfile: ProfileStandard, Description: "Automatic cost optimization recommendations", Percentage: 100},
		{Key: "spot_instance_mgmt", Enabled: false, Category: CategoryFinOps, MinProfile: ProfileFull, Description: "Spot/preemptible instance lifecycle management", Percentage: 100},
		{Key: "reserved_instance_advisor", Enabled: false, Category: CategoryFinOps, MinProfile: ProfileFull, Description: "Reserved instance purchase advisor", Percentage: 100},

		// ---- Platform ----
		{Key: "multi_cluster", Enabled: false, Category: CategoryPlatform, MinProfile: ProfileFull, Description: "Multi-cluster federation and failover", Percentage: 100},
		{Key: "gitops", Enabled: false, Category: CategoryPlatform, MinProfile: ProfileFull, Description: "GitOps-based deployment pipeline", Percentage: 100},
		{Key: "plugin_ecosystem", Enabled: true, Category: CategoryPlatform, MinProfile: ProfileStandard, Description: "Plugin SDK and third-party extensions", Percentage: 100},
		{Key: "multi_tenancy", Enabled: true, Category: CategoryPlatform, MinProfile: ProfileStandard, Description: "Multi-tenant resource isolation", Percentage: 100},
		{Key: "disaster_recovery", Enabled: false, Category: CategoryPlatform, MinProfile: ProfileFull, Description: "Cross-region disaster recovery", Percentage: 100},
	}
	for _, f := range defaults {
		f.UpdatedAt = time.Now().UTC()
		fc := f // copy
		m.flags[f.Key] = &fc
	}
}

// loadFromEnv overrides flags from environment variables.
// Format: CLOUDAI_FF_GPU_SHARING_MPS=true
func (m *Manager) loadFromEnv() {
	for key, flag := range m.flags {
		envKey := "CLOUDAI_FF_" + strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
		if val, ok := os.LookupEnv(envKey); ok {
			flag.Enabled = val == "true" || val == "1"
			flag.UpdatedBy = "env"
			flag.UpdatedAt = time.Now().UTC()
			m.logger.WithFields(logrus.Fields{
				"flag":    key,
				"enabled": flag.Enabled,
				"source":  "env",
			}).Info("Feature flag override from environment")
		}
	}
}

// loadFromStore loads flags from the persistence backend.
func (m *Manager) loadFromStore() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	flags, err := m.store.LoadFlags(ctx)
	if err != nil {
		m.logger.WithError(err).Warn("Failed to load feature flags from store")
		return
	}
	for _, f := range flags {
		fc := f
		m.flags[f.Key] = &fc
	}
	m.logger.WithField("count", len(flags)).Info("Loaded feature flags from store")
}

// IsEnabled checks if a feature flag is enabled. Returns false for unknown flags.
func (m *Manager) IsEnabled(key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	flag, ok := m.flags[key]
	if !ok {
		return false
	}
	if !flag.Enabled {
		return false
	}
	// Percentage-based rollout
	if flag.Percentage < 100 {
		return rand.Intn(100) < flag.Percentage
	}
	return true
}

// SetFlag updates or creates a feature flag.
func (m *Manager) SetFlag(key string, enabled bool, updatedBy string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	flag, ok := m.flags[key]
	if !ok {
		flag = &Flag{Key: key, Percentage: 100}
		m.flags[key] = flag
	}
	flag.Enabled = enabled
	flag.UpdatedBy = updatedBy
	flag.UpdatedAt = time.Now().UTC()

	// Persist if store is available
	if m.store != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.store.SaveFlag(ctx, flag); err != nil {
			m.logger.WithError(err).WithField("flag", key).Warn("Failed to persist feature flag")
		}
	}

	m.logger.WithFields(logrus.Fields{
		"flag":    key,
		"enabled": enabled,
		"by":     updatedBy,
	}).Info("Feature flag updated")
	return nil
}

// GetFlag returns a specific flag's state.
func (m *Manager) GetFlag(key string) (*Flag, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	f, ok := m.flags[key]
	if !ok {
		return nil, false
	}
	// Return a copy
	fc := *f
	return &fc, true
}

// ListFlags returns all registered feature flags sorted by category then key.
func (m *Manager) ListFlags() []Flag {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]Flag, 0, len(m.flags))
	for _, f := range m.flags {
		result = append(result, *f)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Category != result[j].Category {
			return result[i].Category < result[j].Category
		}
		return result[i].Key < result[j].Key
	})
	return result
}

// ListByCategory returns flags filtered by the given category.
func (m *Manager) ListByCategory(cat Category) []Flag {
	all := m.ListFlags()
	var result []Flag
	for _, f := range all {
		if f.Category == cat {
			result = append(result, f)
		}
	}
	return result
}

// Categories returns a sorted list of all distinct categories.
func (m *Manager) Categories() []Category {
	m.mu.RLock()
	defer m.mu.RUnlock()
	set := make(map[Category]struct{})
	for _, f := range m.flags {
		set[f.Category] = struct{}{}
	}
	cats := make([]Category, 0, len(set))
	for c := range set {
		cats = append(cats, c)
	}
	sort.Slice(cats, func(i, j int) bool { return cats[i] < cats[j] })
	return cats
}

// ApplyProfile enables/disables flags according to the given profile.
// Flags whose MinProfile <= the target profile are enabled; others disabled.
// Environment overrides (CLOUDAI_FF_*) are re-applied after the profile
// so that per-flag env vars always take precedence.
func (m *Manager) ApplyProfile(p Profile) {
	m.mu.Lock()
	for _, flag := range m.flags {
		// Only change flags that haven't been explicitly overridden by store
		if flag.UpdatedBy == "profile" || flag.UpdatedBy == "" {
			flag.Enabled = profileIncludes(p, flag.MinProfile)
			flag.UpdatedBy = "profile"
			flag.UpdatedAt = time.Now().UTC()
		}
	}
	m.mu.Unlock()

	// Re-apply env overrides so they take precedence over profile
	m.loadFromEnv()

	m.logger.WithField("profile", string(p)).Info("Feature profile applied")
}

// ActiveProfile detects the active profile from the CLOUDAI_FEATURE_PROFILE env var.
// Returns ProfileStandard if not set or invalid.
func ActiveProfile() Profile {
	val := strings.ToLower(os.Getenv("CLOUDAI_FEATURE_PROFILE"))
	switch val {
	case "minimal":
		return ProfileMinimal
	case "full":
		return ProfileFull
	case "standard", "":
		return ProfileStandard
	default:
		return ProfileStandard
	}
}

// EnabledCount returns the number of currently enabled flags.
func (m *Manager) EnabledCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, f := range m.flags {
		if f.Enabled {
			count++
		}
	}
	return count
}

// Summary returns a concise multi-line summary suitable for startup banners.
func (m *Manager) Summary() string {
	flags := m.ListFlags()
	enabled, disabled := 0, 0
	for _, f := range flags {
		if f.Enabled {
			enabled++
		} else {
			disabled++
		}
	}
	return fmt.Sprintf("Features: %d enabled, %d disabled (total %d)", enabled, disabled, len(flags))
}

// String returns a JSON representation for debugging.
func (m *Manager) String() string {
	flags := m.ListFlags()
	data, _ := json.MarshalIndent(flags, "", "  ")
	return fmt.Sprintf("FeatureFlags(%d): %s", len(flags), string(data))
}
