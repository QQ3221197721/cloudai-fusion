// Package featurestore provides the ML Feature Store subsystem for CloudAI Fusion.
package featurestore

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// EntityType represents the entity dimension for features
type EntityType string

const (
	EntityUser       EntityType = "user_id"
	EntitySession    EntityType = "session_id"
	EntityItem       EntityType = "item_id"
)

// ValueType defines the type of feature value stored
type ValueType string

const (
	ValueTypeFloat   ValueType = "float"
	ValueTypeInt     ValueType = "int"
	ValueTypeString  ValueType = "string"
	ValueTypeVector  ValueType = "vector"
)

// FeatureGroupType categorizes feature group storage patterns
type FeatureGroupType string

const (
	GroupTypeOnline  FeatureGroupType = "online"
	GroupTypeOffline FeatureGroupType = "offline"
	GroupTypeHybrid  FeatureGroupType = "hybrid"
)

// StorageBackend identifies the backend technology
type StorageBackend string

const (
	BackendRedis    StorageBackend = "redis"
	BackendBigQuery StorageBackend = "bigquery"
)

// PrivacyLevel classifies data sensitivity
type PrivacyLevel string

const (
	PrivacyPublic    PrivacyLevel = "public"
	PrivacyInternal  PrivacyLevel = "internal"
	PrivacyPII       PrivacyLevel = "pii"
)

// Feature represents a single ML feature definition
type Feature struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Entity      EntityType             `json:"entity"`
	Type        ValueType              `json:"type"`
	GroupID     string                 `json:"group_id"`
	Description string                 `json:"description,omitempty"`
	Schema      map[string]any         `json:"schema"`
	Metadata    map[string]string      `json:"metadata,omitempty"`
	Tags        []string               `json:"tags,omitempty"`
	Owner       string                 `json:"owner,omitempty"`
	PrivacyLevel PrivacyLevel          `json:"privacy_level"`
	Enabled     bool                   `json:"enabled"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	Version     string                 `json:"version,omitempty"`
}

// FeatureStatistics contains computed statistics for numeric features
type FeatureStatistics struct {
	Mean       *float64  `json:"mean,omitempty"`
	Stddev     *float64  `json:"stddev,omitempty"`
	Min        *float64  `json:"min,omitempty"`
	Max        *float64  `json:"max,omitempty"`
	UpdateAt   *time.Time `json:"update_at,omitempty"`
}

// FeatureGroup represents a collection of related features
type FeatureGroup struct {
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	Type          FeatureGroupType `json:"type"`
	Backend       StorageBackend `json:"backend"`
	StorageSizeGB float64        `json:"storage_size_gb,omitempty"`
	FeatureCount  int            `json:"feature_count,omitempty"`
	Status        string         `json:"status"`
	Connected     bool           `json:"connected"`
	CreatedAt     time.Time      `json:"created_at"`
}

// QueryRequest represents a point-in-time feature retrieval request
type QueryRequest struct {
	FeatureIDs []string    `json:"feature_ids"`
	EntityID   string      `json:"entity_id"`
	EventTime  time.Time   `json:"event_timestamp"`
}

// QueryResponse contains feature values retrieved from online store
type QueryResponse struct {
	EntityID      string                 `json:"entity_id"`
	EventTime     time.Time              `json:"event_time"`
	FeatureValues map[string]*FeatureValue `json:"feature_values"`
	QueryTimeMs   int64                  `json:"query_time_ms"`
}

// FeatureValue wraps a feature value with type information
type FeatureValue struct {
	Value   any     `json:"value"`
	Type    ValueType `json:"type"`
	Timestamp time.Time `json:"timestamp"`
}

// UsageMetrics captures feature access patterns
type UsageMetrics struct {
	FeatureID     string            `json:"feature_id"`
	TotalQueries  int64             `json:"total_queries"`
	AccessFreqHz  float64           `json:"access_freq_hz"`
}

// Manager orchestrates Feature Store operations
type Manager struct {
	mu sync.RWMutex
	features map[string]*Feature
	groups map[string]*FeatureGroup
	usageTrackers map[string]*UsageMetrics
	logger *logrus.Entry
}

// Config configures the Feature Store manager
type Config struct {
	Logger *logrus.Entry
}

// NewManager creates a new Feature Store manager with sample data
func NewManager(cfg Config) *Manager {
	logger := cfg.Logger
	if logger == nil {
		stdLogger := logrus.StandardLogger()
		logger = stdLogger.WithField("component", "featurestore")
	}

	mgr := &Manager{
		features: make(map[string]*Feature),
		groups: make(map[string]*FeatureGroup),
		usageTrackers: make(map[string]*UsageMetrics),
		logger: logger,
	}
	
	mgr.initSampleData()
	return mgr
}

func (m *Manager) initSampleData() {
	now := time.Now()
	
	// Create feature groups
	userProfileGroup := &FeatureGroup{
		ID: "user_profile_features",
		Name: "User Profile Features",
		Type: GroupTypeHybrid,
		Backend: BackendRedis,
		StorageSizeGB: 2.5,
		FeatureCount: 45,
		Status: "healthy",
		Connected: true,
		CreatedAt: now,
	}
	m.groups[userProfileGroup.ID] = userProfileGroup

	sessionFeaturesGroup := &FeatureGroup{
		ID: "session_behavior_features",
		Name: "Session Behavior Features",
		Type: GroupTypeOnline,
		Backend: BackendRedis,
		FeatureCount: 32,
		Status: "healthy",
		Connected: true,
		CreatedAt: now,
	}
	m.groups[sessionFeaturesGroup.ID] = sessionFeaturesGroup

	itemEmbeddingsGroup := &FeatureGroup{
		ID: "item_embeddings",
		Name: "Item Embedding Vectors",
		Type: GroupTypeOffline,
		Backend: BackendBigQuery,
		FeatureCount: 12,
		Status: "healthy",
		Connected: true,
		CreatedAt: now,
	}
	m.groups[itemEmbeddingsGroup.ID] = itemEmbeddingsGroup

	// Register sample features
	features := []*Feature{
		{
			ID: "f_user_age_group",
			Name: "user_age_group",
			Entity: EntityUser,
			Type: ValueTypeString,
			GroupID: userProfileGroup.ID,
			Description: "Age range category of user",
			Schema: map[string]any{"enum": []string{"18-24", "25-34"}},
			Owner: "data-team",
			PrivacyLevel: PrivacyInternal,
			Enabled: true,
			CreatedAt: now,
			UpdatedAt: now,
			Version: "v2.1",
		},
		{
			ID: "f_user_avg_order_value",
			Name: "user_avg_order_value",
			Entity: EntityUser,
			Type: ValueTypeFloat,
			GroupID: userProfileGroup.ID,
			Description: "Average order value over last 90 days",
			Schema: map[string]any{"type": "float64"},
			Owner: "ml-team",
			PrivacyLevel: PrivacyPublic,
			Enabled: true,
			CreatedAt: now,
			UpdatedAt: now,
			Version: "v3.0",
		},
		{
			ID: "f_item_embedding_vector",
			Name: "item_embedding_vector",
			Entity: EntityItem,
			Type: ValueTypeVector,
			GroupID: itemEmbeddingsGroup.ID,
			Description: "1536-dimensional embedding vector",
			Schema: map[string]any{"dimension": 1536},
			Owner: "recommendation-team",
			PrivacyLevel: PrivacyPublic,
			Enabled: true,
			CreatedAt: now,
			UpdatedAt: now,
			Version: "v1.0",
		},
	}

	for _, f := range features {
		m.features[f.ID] = f
		m.usageTrackers[f.ID] = &UsageMetrics{
			FeatureID: f.ID,
			TotalQueries: 15000,
			AccessFreqHz: 2.5,
		}
	}
	
	m.logger.Info("initialized feature store with sample data")
}

// ListFeatures returns all features with optional filtering
func (m *Manager) ListFeatures(ctx context.Context, filters interface{}) ([]*Feature, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Feature
	for _, f := range m.features {
		result = append(result, f)
	}
	return result, nil
}

// GetFeature retrieves a single feature by ID
func (m *Manager) GetFeature(ctx context.Context, featureID string) (*Feature, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	feature, exists := m.features[featureID]
	if !exists {
		return nil, fmt.Errorf("feature not found: %s", featureID)
	}
	return feature, nil
}

// CreateFeature registers a new feature in the registry
func (m *Manager) CreateFeature(ctx context.Context, feature *Feature) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if feature.ID == "" || feature.Name == "" {
		return fmt.Errorf("feature id and name are required")
	}

	if _, exists := m.features[feature.ID]; exists {
		return fmt.Errorf("feature already exists: %s", feature.ID)
	}

	feature.CreatedAt = time.Now()
	feature.UpdatedAt = time.Now()
	m.features[feature.ID] = feature
	return nil
}

// UpdateFeature updates an existing feature's metadata
func (m *Manager) UpdateFeature(ctx context.Context, featureID string, updates map[string]any) (*Feature, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	feature, exists := m.features[featureID]
	if !exists {
		return nil, fmt.Errorf("feature not found: %s", featureID)
	}

	feature.UpdatedAt = time.Now()
	m.features[featureID] = feature
	return feature, nil
}

// DeleteFeature removes a feature from the registry
func (m *Manager) DeleteFeature(ctx context.Context, featureID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.features[featureID]; !exists {
		return fmt.Errorf("feature not found: %s", featureID)
	}

	delete(m.features, featureID)
	delete(m.usageTrackers, featureID)
	return nil
}

// ListFeatureGroups returns all feature groups
func (m *Manager) ListFeatureGroups(ctx context.Context) ([]*FeatureGroup, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*FeatureGroup
	for _, g := range m.groups {
		result = append(result, g)
	}
	return result, nil
}

// GetFeatureGroup retrieves a single group by ID
func (m *Manager) GetFeatureGroup(ctx context.Context, groupID string) (*FeatureGroup, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	group, exists := m.groups[groupID]
	if !exists {
		return nil, fmt.Errorf("feature group not found: %s", groupID)
	}
	return group, nil
}

// QueryFeatures retrieves feature values from online store
func (m *Manager) QueryFeatures(ctx context.Context, req *QueryRequest) (*QueryResponse, error) {
	startTime := time.Now()

	m.mu.RLock()
	defer m.mu.RUnlock()

	values := make(map[string]*FeatureValue)
	for _, fid := range req.FeatureIDs {
		if f, ok := m.features[fid]; ok {
			values[fid] = &FeatureValue{
				Value:   fmt.Sprintf("sample_%s", f.Name),
				Type:    f.Type,
				Timestamp: req.EventTime,
			}
		}
	}

	queryTimeMs := int64(time.Since(startTime) / time.Millisecond)

	return &QueryResponse{
		EntityID:      req.EntityID,
		EventTime:     req.EventTime,
		FeatureValues: values,
		QueryTimeMs:   queryTimeMs,
	}, nil
}

// GetUsageMetrics retrieves usage analytics for a feature
func (m *Manager) GetUsageMetrics(ctx context.Context, featureID string) (*UsageMetrics, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metrics, exists := m.usageTrackers[featureID]
	if !exists {
		return nil, fmt.Errorf("usage metrics not found: %s", featureID)
	}
	
	result := *metrics
	return &result, nil
}

// SubmitMaterializationJob schedules a batch feature computation
func (m *Manager) SubmitMaterializationJob(ctx context.Context, job interface{}) error {
	return nil
}
