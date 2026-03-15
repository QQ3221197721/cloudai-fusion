package logging

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// Log Collection Pipeline Types
// ============================================================================

// LogBackend defines supported log backend systems.
type LogBackend string

const (
	BackendLoki          LogBackend = "loki"
	BackendElasticsearch LogBackend = "elasticsearch"
	BackendCloudWatch    LogBackend = "cloudwatch"
	BackendLocal         LogBackend = "local"
)

// LogLevel defines log severity levels for archival filtering.
type LogLevel string

const (
	LogLevelDebug   LogLevel = "debug"
	LogLevelInfo    LogLevel = "info"
	LogLevelWarning LogLevel = "warning"
	LogLevelError   LogLevel = "error"
	LogLevelFatal   LogLevel = "fatal"
)

// ArchivalTier defines storage tiers for log archival.
type ArchivalTier string

const (
	TierHot      ArchivalTier = "hot"    // Recent, fast access (SSD)
	TierWarm     ArchivalTier = "warm"   // Moderate, standard storage
	TierCold     ArchivalTier = "cold"   // Old, cheap storage (S3/OSS)
	TierFrozen   ArchivalTier = "frozen" // Long-term archive, glacier
	TierDeleted  ArchivalTier = "deleted"
)

// ============================================================================
// ELK Integration
// ============================================================================

// ElasticsearchConfig configures Elasticsearch/OpenSearch connection.
type ElasticsearchConfig struct {
	Endpoints    []string `json:"endpoints"`     // e.g. ["https://es-01:9200"]
	IndexPrefix  string   `json:"index_prefix"`  // e.g. "cloudai-fusion"
	Username     string   `json:"username,omitempty"`
	Password     string   `json:"password,omitempty"`
	TLSEnabled   bool     `json:"tls_enabled"`
	Shards       int      `json:"shards"`
	Replicas     int      `json:"replicas"`
	RefreshSecs  int      `json:"refresh_seconds"`
}

// ElasticsearchIndex represents an index lifecycle configuration.
type ElasticsearchIndex struct {
	Name         string       `json:"name"`
	Pattern      string       `json:"pattern"` // e.g. "cloudai-fusion-2026.03.*"
	Tier         ArchivalTier `json:"tier"`
	SizeBytes    int64        `json:"size_bytes"`
	DocCount     int64        `json:"doc_count"`
	CreatedAt    time.Time    `json:"created_at"`
	LastWriteAt  *time.Time   `json:"last_write_at,omitempty"`
}

// IndexLifecyclePolicy defines when indices transition between tiers.
type IndexLifecyclePolicy struct {
	Name           string `json:"name"`
	HotMaxAgeDays  int    `json:"hot_max_age_days"`   // Move to warm after N days
	WarmMaxAgeDays int    `json:"warm_max_age_days"`  // Move to cold after N days
	ColdMaxAgeDays int    `json:"cold_max_age_days"`  // Move to frozen after N days
	DeleteAfterDays int   `json:"delete_after_days"`  // Delete after N days
	HotMaxSizeGB    int   `json:"hot_max_size_gb"`    // Rollover hot at size
}

// ============================================================================
// Log Archival Policy
// ============================================================================

// ArchivalPolicy defines how logs are archived across storage tiers.
type ArchivalPolicy struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	Description   string       `json:"description,omitempty"`
	Enabled       bool         `json:"enabled"`
	SourceBackend LogBackend   `json:"source_backend"`
	MinLevel      LogLevel     `json:"min_level"`      // Only archive logs >= this level
	RetentionDays int          `json:"retention_days"`  // Total retention before deletion
	Tiers         []TierConfig `json:"tiers"`
	CreatedAt     time.Time    `json:"created_at"`
	LastRunAt     *time.Time   `json:"last_run_at,omitempty"`
}

// TierConfig defines a specific archival tier's settings.
type TierConfig struct {
	Tier          ArchivalTier `json:"tier"`
	AfterDays     int          `json:"after_days"`     // Transition after N days from creation
	StorageClass  string       `json:"storage_class"`  // e.g. "STANDARD_IA", "GLACIER"
	CompressionOn bool         `json:"compression"`
}

// ============================================================================
// Log Collection Source
// ============================================================================

// LogSource represents a log collection source.
type LogSource struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Type        string            `json:"type"`    // container, file, syslog, journal
	Namespace   string            `json:"namespace,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Path        string            `json:"path,omitempty"`  // For file sources
	Backend     LogBackend        `json:"backend"`
	Enabled     bool              `json:"enabled"`
	ParseFormat string            `json:"parse_format,omitempty"` // json, logfmt, regex
}

// LogCollectionStats tracks log ingestion statistics.
type LogCollectionStats struct {
	SourceID       string    `json:"source_id"`
	SourceName     string    `json:"source_name"`
	BytesIngested  int64     `json:"bytes_ingested"`
	LinesIngested  int64     `json:"lines_ingested"`
	ErrorCount     int64     `json:"error_count"`
	DroppedCount   int64     `json:"dropped_count"`
	LastIngestAt   time.Time `json:"last_ingest_at"`
}

// ============================================================================
// Centralized Log Manager
// ============================================================================

// CentralizedConfig configures the centralized logging manager.
type CentralizedConfig struct {
	DefaultBackend    LogBackend          `json:"default_backend"`
	Elasticsearch     *ElasticsearchConfig `json:"elasticsearch,omitempty"`
	LokiEndpoint      string              `json:"loki_endpoint,omitempty"`
	DefaultRetention  int                 `json:"default_retention_days"` // days
	ArchivalEnabled   bool                `json:"archival_enabled"`
	Logger            *logrus.Logger
}

// CentralizedManager manages centralized log collection, routing, and archival.
type CentralizedManager struct {
	config    CentralizedConfig
	sources   map[string]*LogSource
	policies  map[string]*ArchivalPolicy
	stats     map[string]*LogCollectionStats
	indices   []*ElasticsearchIndex
	ilmPolicy *IndexLifecyclePolicy
	logger    *logrus.Logger
	mu        sync.RWMutex
}

// NewCentralizedManager creates a new centralized log manager.
func NewCentralizedManager(cfg CentralizedConfig) *CentralizedManager {
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}
	if cfg.DefaultRetention == 0 {
		cfg.DefaultRetention = 90
	}
	if cfg.DefaultBackend == "" {
		cfg.DefaultBackend = BackendLoki
	}

	cfg.Logger.WithFields(logrus.Fields{
		"backend":   cfg.DefaultBackend,
		"retention": cfg.DefaultRetention,
		"archival":  cfg.ArchivalEnabled,
	}).Info("Centralized log manager initialized")

	return &CentralizedManager{
		config:   cfg,
		sources:  make(map[string]*LogSource),
		policies: make(map[string]*ArchivalPolicy),
		stats:    make(map[string]*LogCollectionStats),
		logger:   cfg.Logger,
	}
}

// ============================================================================
// Source Management
// ============================================================================

// AddSource registers a log collection source.
func (m *CentralizedManager) AddSource(source *LogSource) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if source.ID == "" {
		source.ID = common.NewUUID()
	}
	if source.Backend == "" {
		source.Backend = m.config.DefaultBackend
	}

	m.sources[source.ID] = source
	m.stats[source.ID] = &LogCollectionStats{
		SourceID:   source.ID,
		SourceName: source.Name,
	}

	m.logger.WithFields(logrus.Fields{
		"source_id": source.ID, "name": source.Name,
		"type": source.Type, "backend": source.Backend,
	}).Info("Log source registered")

	return nil
}

// RemoveSource removes a log collection source.
func (m *CentralizedManager) RemoveSource(sourceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.sources[sourceID]; !ok {
		return fmt.Errorf("source %s not found", sourceID)
	}
	delete(m.sources, sourceID)
	delete(m.stats, sourceID)
	return nil
}

// GetSource returns a specific log source.
func (m *CentralizedManager) GetSource(sourceID string) (*LogSource, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s, ok := m.sources[sourceID]
	if !ok {
		return nil, fmt.Errorf("source %s not found", sourceID)
	}
	return s, nil
}

// ListSources returns all registered log sources.
func (m *CentralizedManager) ListSources() []*LogSource {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := make([]*LogSource, 0, len(m.sources))
	for _, s := range m.sources {
		list = append(list, s)
	}
	return list
}

// ListSourcesByBackend returns sources filtered by backend.
func (m *CentralizedManager) ListSourcesByBackend(backend LogBackend) []*LogSource {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var list []*LogSource
	for _, s := range m.sources {
		if s.Backend == backend {
			list = append(list, s)
		}
	}
	return list
}

// ============================================================================
// Archival Policy Management
// ============================================================================

// CreateArchivalPolicy adds a log archival policy.
func (m *CentralizedManager) CreateArchivalPolicy(policy *ArchivalPolicy) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if policy.ID == "" {
		policy.ID = common.NewUUID()
	}
	if policy.RetentionDays == 0 {
		policy.RetentionDays = m.config.DefaultRetention
	}
	policy.CreatedAt = common.NowUTC()

	// Validate tier transitions are monotonically increasing
	for i := 1; i < len(policy.Tiers); i++ {
		if policy.Tiers[i].AfterDays <= policy.Tiers[i-1].AfterDays {
			return fmt.Errorf("tier transitions must be monotonically increasing (tier %d: %d <= tier %d: %d)",
				i, policy.Tiers[i].AfterDays, i-1, policy.Tiers[i-1].AfterDays)
		}
	}

	m.policies[policy.ID] = policy
	m.logger.WithFields(logrus.Fields{
		"policy_id": policy.ID, "name": policy.Name, "tiers": len(policy.Tiers),
	}).Info("Archival policy created")

	return nil
}

// GetArchivalPolicy returns a specific policy.
func (m *CentralizedManager) GetArchivalPolicy(policyID string) (*ArchivalPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	p, ok := m.policies[policyID]
	if !ok {
		return nil, fmt.Errorf("policy %s not found", policyID)
	}
	return p, nil
}

// ListArchivalPolicies returns all policies.
func (m *CentralizedManager) ListArchivalPolicies() []*ArchivalPolicy {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := make([]*ArchivalPolicy, 0, len(m.policies))
	for _, p := range m.policies {
		list = append(list, p)
	}
	return list
}

// EnableArchivalPolicy enables or disables a policy.
func (m *CentralizedManager) EnableArchivalPolicy(policyID string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.policies[policyID]
	if !ok {
		return fmt.Errorf("policy %s not found", policyID)
	}
	p.Enabled = enabled
	return nil
}

// EvaluateArchivalPolicy determines the current tier for logs of a given age.
func (m *CentralizedManager) EvaluateArchivalPolicy(policyID string, logAgeDays int) (ArchivalTier, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	p, ok := m.policies[policyID]
	if !ok {
		return "", fmt.Errorf("policy %s not found", policyID)
	}

	if logAgeDays >= p.RetentionDays {
		return TierDeleted, nil
	}

	// Walk tiers in reverse to find the highest matching tier
	currentTier := TierHot
	for _, tier := range p.Tiers {
		if logAgeDays >= tier.AfterDays {
			currentTier = tier.Tier
		}
	}
	return currentTier, nil
}

// ============================================================================
// Index Lifecycle Management (ELK)
// ============================================================================

// SetILMPolicy configures the Index Lifecycle Management policy.
func (m *CentralizedManager) SetILMPolicy(policy *IndexLifecyclePolicy) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ilmPolicy = policy
	m.logger.WithField("policy", policy.Name).Info("ILM policy set")
}

// GetILMPolicy returns the current ILM policy.
func (m *CentralizedManager) GetILMPolicy() *IndexLifecyclePolicy {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ilmPolicy
}

// RegisterIndex registers an Elasticsearch index for lifecycle tracking.
func (m *CentralizedManager) RegisterIndex(index *ElasticsearchIndex) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.indices = append(m.indices, index)
}

// EvaluateILM evaluates which indices need tier transitions based on the ILM policy.
func (m *CentralizedManager) EvaluateILM() []map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.ilmPolicy == nil {
		return nil
	}

	now := common.NowUTC()
	var actions []map[string]interface{}

	for _, idx := range m.indices {
		ageDays := int(now.Sub(idx.CreatedAt).Hours() / 24)

		var targetTier ArchivalTier
		if ageDays >= m.ilmPolicy.DeleteAfterDays && m.ilmPolicy.DeleteAfterDays > 0 {
			targetTier = TierDeleted
		} else if ageDays >= m.ilmPolicy.ColdMaxAgeDays && m.ilmPolicy.ColdMaxAgeDays > 0 {
			targetTier = TierFrozen
		} else if ageDays >= m.ilmPolicy.WarmMaxAgeDays && m.ilmPolicy.WarmMaxAgeDays > 0 {
			targetTier = TierCold
		} else if ageDays >= m.ilmPolicy.HotMaxAgeDays && m.ilmPolicy.HotMaxAgeDays > 0 {
			targetTier = TierWarm
		} else {
			targetTier = TierHot
		}

		if targetTier != idx.Tier {
			actions = append(actions, map[string]interface{}{
				"index":       idx.Name,
				"current_tier": idx.Tier,
				"target_tier":  targetTier,
				"age_days":     ageDays,
				"action":       fmt.Sprintf("transition %s -> %s", idx.Tier, targetTier),
			})
		}
	}

	return actions
}

// ============================================================================
// Log Ingestion Stats
// ============================================================================

// RecordIngestion updates ingestion statistics for a source.
func (m *CentralizedManager) RecordIngestion(sourceID string, bytes, lines int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	stats, ok := m.stats[sourceID]
	if !ok {
		return fmt.Errorf("source %s not found", sourceID)
	}
	stats.BytesIngested += bytes
	stats.LinesIngested += lines
	stats.LastIngestAt = common.NowUTC()
	return nil
}

// RecordError increments the error count for a source.
func (m *CentralizedManager) RecordError(sourceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	stats, ok := m.stats[sourceID]
	if !ok {
		return fmt.Errorf("source %s not found", sourceID)
	}
	stats.ErrorCount++
	return nil
}

// GetStats returns ingestion statistics for a source.
func (m *CentralizedManager) GetStats(sourceID string) (*LogCollectionStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats, ok := m.stats[sourceID]
	if !ok {
		return nil, fmt.Errorf("source %s not found", sourceID)
	}
	return stats, nil
}

// GetAllStats returns all ingestion stats sorted by bytes ingested (descending).
func (m *CentralizedManager) GetAllStats() []*LogCollectionStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	list := make([]*LogCollectionStats, 0, len(m.stats))
	for _, s := range m.stats {
		list = append(list, s)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].BytesIngested > list[j].BytesIngested
	})
	return list
}

// ============================================================================
// Summary
// ============================================================================

// GetSummary returns an overview of the centralized logging system.
func (m *CentralizedManager) GetSummary() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	totalSources := len(m.sources)
	enabledSources := 0
	for _, s := range m.sources {
		if s.Enabled {
			enabledSources++
		}
	}

	totalPolicies := len(m.policies)
	enabledPolicies := 0
	for _, p := range m.policies {
		if p.Enabled {
			enabledPolicies++
		}
	}

	var totalBytes, totalLines, totalErrors int64
	for _, s := range m.stats {
		totalBytes += s.BytesIngested
		totalLines += s.LinesIngested
		totalErrors += s.ErrorCount
	}

	backendCounts := make(map[LogBackend]int)
	for _, s := range m.sources {
		backendCounts[s.Backend]++
	}

	return map[string]interface{}{
		"total_sources":    totalSources,
		"enabled_sources":  enabledSources,
		"total_policies":   totalPolicies,
		"enabled_policies": enabledPolicies,
		"total_bytes":      totalBytes,
		"total_lines":      totalLines,
		"total_errors":     totalErrors,
		"backends":         backendCounts,
		"indices":          len(m.indices),
		"has_ilm":          m.ilmPolicy != nil,
		"default_backend":  m.config.DefaultBackend,
		"retention_days":   m.config.DefaultRetention,
	}
}

// GenerateDefaultPolicies creates recommended archival policies.
func (m *CentralizedManager) GenerateDefaultPolicies() []*ArchivalPolicy {
	policies := []*ArchivalPolicy{
		{
			Name:          "standard-archival",
			Description:   "Standard log archival: hot(7d) -> warm(30d) -> cold(90d) -> delete(365d)",
			Enabled:       true,
			SourceBackend: m.config.DefaultBackend,
			MinLevel:      LogLevelInfo,
			RetentionDays: 365,
			Tiers: []TierConfig{
				{Tier: TierWarm, AfterDays: 7, StorageClass: "STANDARD_IA", CompressionOn: true},
				{Tier: TierCold, AfterDays: 30, StorageClass: "GLACIER", CompressionOn: true},
				{Tier: TierFrozen, AfterDays: 90, StorageClass: "DEEP_ARCHIVE", CompressionOn: true},
			},
		},
		{
			Name:          "security-archival",
			Description:   "Security log archival: extended retention for compliance (3 years)",
			Enabled:       true,
			SourceBackend: m.config.DefaultBackend,
			MinLevel:      LogLevelWarning,
			RetentionDays: 1095, // 3 years
			Tiers: []TierConfig{
				{Tier: TierWarm, AfterDays: 30, StorageClass: "STANDARD_IA", CompressionOn: true},
				{Tier: TierCold, AfterDays: 180, StorageClass: "GLACIER", CompressionOn: true},
				{Tier: TierFrozen, AfterDays: 365, StorageClass: "DEEP_ARCHIVE", CompressionOn: true},
			},
		},
		{
			Name:          "debug-ephemeral",
			Description:   "Debug log archival: short-lived, delete after 7 days",
			Enabled:       true,
			SourceBackend: m.config.DefaultBackend,
			MinLevel:      LogLevelDebug,
			RetentionDays: 7,
			Tiers:         []TierConfig{},
		},
	}

	for _, p := range policies {
		m.CreateArchivalPolicy(p)
	}
	return policies
}

// GenerateDefaultILM creates a recommended ILM policy for Elasticsearch.
func (m *CentralizedManager) GenerateDefaultILM() *IndexLifecyclePolicy {
	policy := &IndexLifecyclePolicy{
		Name:            "cloudai-fusion-ilm",
		HotMaxAgeDays:   7,
		WarmMaxAgeDays:  30,
		ColdMaxAgeDays:  90,
		DeleteAfterDays: 365,
		HotMaxSizeGB:    50,
	}
	m.SetILMPolicy(policy)
	return policy
}
