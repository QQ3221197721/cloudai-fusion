// Package aiops - M49 Self-healing Controller: Healer Registry Pattern
// This module implements dynamic healer registration with API compatibility layer,
// preserving existing handlers in cmd/apiserver/redteam/* for backward compatibility.
package aiops

import (
	"context"
	"fmt"
	"sync"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// HEALER INTERFACE DEFINITION
// ===========================================================================

// HealerInterface defines contract for all healing models/actions
type HealerInterface interface {
	// ID returns unique identifier
	ID() string
	
	// Heal performs healing action and returns result
	Heal(ctx context.Context, inputs ...interface{}) HealingResult
	
	// UpdateHealth updates internal health state
	UpdateHealth(metrics map[string]float64) error
	
	// GetMetrics returns current healer metrics
	GetMetrics() map[string]interface{}
	
	// Reset resets healer state
	Reset()
	
	// IsHealthy checks if healer is operational
	IsHealthy() bool
}

// HealingResult contains outcome of healing attempt
type HealingResult struct {
	HealerID        string  `json:"healer_id"`
	AnomalyDetected bool    `json:"anomaly_detected"`
	ConfidenceScore float64 `json:"confidence_score"`
	RecoveryTimeMs  int64   `json:"recovery_time_ms"`
	ActionExecuted  string  `json:"action_executed,omitempty"`
	MetricsChanged  bool    `json:"metrics_changed"`
	Success         bool    `json:"success"`
	ErrorMessage    string  `json:"error_message,omitempty"`
	RetryNeeded     bool    `json:"retry_needed,omitempty"`
}

// ============================================================================
// REGISTRY CORE IMPLEMENTATION
// ===========================================================================

// HealerRegistry manages dynamic healer lifecycle and discovery
type HealerRegistry struct {
	logger      *logrus.Logger
	healers     map[string]HealerInterface // id -> healer instance
	healerTypes map[string]string          // id -> type name
	factories   map[string]HealerFactory   // type -> factory function
	
	// Health tracking
	healthStatus map[string]bool // healthy/unhealthy per healer
	mu           sync.RWMutex
}

// HealerFactory creates new healer instances
type HealerFactory func(logger *logrus.Logger) HealerInterface

// NewHealerRegistry initializes empty registry
func NewHealerRegistry(logger *logrus.Logger) *HealerRegistry {
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	return &HealerRegistry{
		logger:       logger,
		healers:      make(map[string]HealerInterface),
		healerTypes:  make(map[string]string),
		factories:    make(map[string]HealerFactory),
		healthStatus: make(map[string]bool),
	}
}

// RegisterFactory registers factory function for a healer type
func (r *HealerRegistry) RegisterFactory(healerType string, factory HealerFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.factories[healerType] = factory
	r.logger.WithFields(logrus.Fields{
		"type": healerType,
	}).Info("Registered healer factory")
}

// RegisterNewHealer adds a pre-built healer instance
func (r *HealerRegistry) RegisterNewHealer(healer HealerInterface) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	id := healer.ID()
	if _, exists := r.healers[id]; exists {
		return fmt.Errorf("healer %s already registered", id)
	}

	r.healers[id] = healer
	r.healerTypes[id] = r.inferHealerType(healer)
	r.healthStatus[id] = true // Assume healthy initially

	r.logger.WithFields(logrus.Fields{
		"id":   id,
		"type": r.healerTypes[id],
	}).Info("Registered new healer")

	return nil
}

// RegisterDynamicHealer instantiates and registers healers from factories
func (r *HealerRegistry) RegisterDynamicHealer(id, healerType string) error {
	r.mu.Lock()
	factory, exists := r.factories[healerType]
	r.mu.Unlock()

	if !exists {
		return fmt.Errorf("unknown healer type %s, available types: %v", healerType, r.ListAvailableTypes())
	}

	healer := factory(r.logger)
	if err := r.RegisterNewHealer(healer); err != nil {
		return err
	}

	return nil
}

// UnregisterHealer removes healer from registry
func (r *HealerRegistry) UnregisterHealer(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	healer, exists := r.healers[id]
	if !exists {
		return fmt.Errorf("healer %s not found", id)
	}

	delete(r.healers, id)
	delete(r.healerTypes, id)
	delete(r.healthStatus, id)

	healer.Reset()
	
	r.logger.WithField("id", id).Info("Unregistered healer")
	return nil
}

// GetHealer retrieves healer by ID
func (r *HealerRegistry) GetHealer(id string) (HealerInterface, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	healer, exists := r.healers[id]
	return healer, exists
}

// ListAllHealers returns copy of all registered healers
func (r *HealerRegistry) ListAllHealers() []HealerInterface {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]HealerInterface, 0, len(r.healers))
	for _, h := range r.healers {
		result = append(result, h)
	}

	return result
}

// ListAvailableTypes returns supported healer type names
func (r *HealerRegistry) ListAvailableTypes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.factories))
	for t := range r.factories {
		types = append(types, t)
	}

	return types
}

// CountHealers returns number of registered healers
func (r *HealerRegistry) CountHealers() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.healers)
}

// GetAllHealerIDs returns list of healer IDs
func (r *HealerRegistry) GetAllHealerIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make([]string, 0, len(r.healers))
	for id := range r.healers {
		ids = append(ids, id)
	}

	return ids
}

// ============================================================================
// HEALTH MANAGEMENT
// ===========================================================================

// MarkHealerAsHealthy explicitly sets healer health status
func (r *HealerRegistry) MarkHealerAsHealthy(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.healers[id]; !exists {
		return false
	}

	r.healthStatus[id] = true
	return true
}

// MarkHealerAsUnhealthy marks healer as unhealthy
func (r *HealerRegistry) MarkHealerAsUnhealthy(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.healers[id]; !exists {
		return false
	}

	r.healthStatus[id] = false
	return true
}

// IsHealerHealthy checks if healer is operational
func (r *HealerRegistry) IsHealerHealthy(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	status, exists := r.healthStatus[id]
	if !exists {
		return false
	}

	return status
}

// GetHealthyHealers returns only healthy healers
func (r *HealerRegistry) GetHealthyHealers() []HealerInterface {
	r.mu.RLock()
	defer r.mu.RUnlock()

	healthy := make([]HealerInterface, 0)
	for id, h := range r.healers {
		if r.healthStatus[id] && h.IsHealthy() {
			healthy = append(healthy, h)
		}
	}

	return healthy
}

// HealthReport provides overall system health summary
func (r *HealerRegistry) HealthReport() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	total := len(r.healers)
	healthyCount := 0
	unhealthyHealers := make([]string, 0)

	for id, h := range r.healers {
		if r.healthStatus[id] && h.IsHealthy() {
			healthyCount++
		} else {
			unhealthyHealers = append(unhealthyHealers, id)
		}
	}

	return map[string]interface{}{
		"total_healers":       total,
		"healthy_count":       healthyCount,
		"unhealthy_count":     total - healthyCount,
		"health_percentage":   float64(healthyCount) / float64(total) * 100,
		"unhealthy_healers":   unhealthyHealers,
		"all_healthy":         len(unhealthyHealers) == 0,
	}
}

// ============================================================================
// BACKWARD COMPATIBILITY LAYER
// ===========================================================================

// RedTeamAPICompatLayer maintains API compatibility with existing redteam handlers
type RedTeamAPICompatLayer struct {
	registry *HealerRegistry
	logger   *logrus.Logger
}

// NewRedTeamAPICompatLayer creates compatibility wrapper
func NewRedTeamAPICompatLayer(registry *HealerRegistry, logger *logrus.Logger) *RedTeamAPICompatLayer {
	return &RedTeamAPICompatLayer{
		registry: registry,
		logger:   logger,
	}
}

// ExecuteLegacyRedTeamAction executes actions via traditional API format
func (c *RedTeamAPICompatLayer) ExecuteLegacyRedTeamAction(actionType string, targetTarget interface{}, params map[string]interface{}) (map[string]interface{}, error) {
	healers := c.registry.GetHealthyHealers()
	
	results := make([]HealingResult, 0, len(healers))
	for _, healer := range healers {
		ctx := context.Background()
		
		// Convert legacy params to modern input format
		inputs := []interface{}{targetTarget}
		if payload, ok := params["payload"].(map[string]interface{}); ok {
			// Create MetricsSnapshot from legacy payload
			snapshot := c.parseLegacyPayload(payload)
			inputs = append(inputs, snapshot)
		}
		
		result := healer.Heal(ctx, inputs...)
		results = append(results, result)
	}

	// Aggregate results into legacy-compatible response
	return c.aggregateResults(results), nil
}

// parseLegacyPayload converts legacy payload format to MetricsSnapshot
func (c *RedTeamAPICompatLayer) parseLegacyPayload(payload map[string]interface{}) MetricsSnapshot {
	snapshot := MetricsSnapshot{}

	if cpu, ok := payload["cpu_util"].(float64); ok {
		snapshot.CPUUtilization = cpu
	}
	if mem, ok := payload["memory_usage"].(float64); ok {
		snapshot.MemoryUsage = mem
	}
	if err, ok := payload["error_rate"].(float64); ok {
		snapshot.ErrorRate = err
	}
	
	return snapshot
}

// aggregateResults merges individual healer outcomes into single response
func (c *RedTeamAPICompatLayer) aggregateResults(results []HealingResult) map[string]interface{} {
	if len(results) == 0 {
		return map[string]interface{}{"success": false, "error": "no healers available"}
	}

	successCount := 0
	detectionCount := 0
	totalConfidence := 0.0
	minConfidence := 1.0
	maxConfidence := 0.0

	for _, r := range results {
		if r.Success {
			successCount++
		}
		if r.AnomalyDetected {
			detectionCount++
		}
		totalConfidence += r.ConfidenceScore
		if r.ConfidenceScore < minConfidence {
			minConfidence = r.ConfidenceScore
		}
		if r.ConfidenceScore > maxConfidence {
			maxConfidence = r.ConfidenceScore
		}
	}

	avgConfidence := totalConfidence / float64(len(results))

	return map[string]interface{}{
		"success":              successCount == len(results),
		"anomalies_detected":   detectionCount > 0,
		"detection_rate":       float64(detectionCount) / float64(len(results)) * 100,
		"average_confidence":   avgConfidence,
		"min_confidence":       minConfidence,
		"max_confidence":       maxConfidence,
		"healer_count":         len(results),
		"successful_healers":   successCount,
		"ensemble_voting":      detectionCount > len(results)/2, // Majority voting
	}
}

// ============================================================================
// TYPE INFERENCE AND FALLBACKS
// ===========================================================================

// inferHealerType attempts to determine healer's type from struct name
func (r *HealerRegistry) inferHealerType(healer HealerInterface) string {
	id := healer.ID()
	
	// Fallback to ID-based inference
	if len(id) > 0 {
		parts := SplitString(id, "_")
		if len(parts) > 0 {
			baseType := parts[0]
			typeMap := map[string]string{
				"mahalanobis": "statistical_distance",
				"isolation_forest": "tree_based",
				"autoencoder": "deep_learning",
				"ensemble": "hybrid_ensemble",
			}
			if inferred, ok := typeMap[baseType]; ok {
				return inferred
			}
			return baseType
		}
	}

	return "unknown"
}

// SplitString splits string by delimiter
func SplitString(s, delim string) []string {
	result := make([]string, 0)
	current := ""
	
	for _, ch := range s {
		if string(ch) == delim {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(ch)
		}
	}
	
	if current != "" {
		result = append(result, current)
	}
	
	return result
}

// ============================================================================
// STATISTICS AND TELEMETRY
// ===========================================================================

// PerformanceStats collects performance statistics across all healers
func (r *HealerRegistry) PerformanceStats() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := make(map[string]interface{})
	healerStats := make(map[string]map[string]interface{})

	var totalLatencyMs int64
	var totalExecutions int64

	for id, h := range r.healers {
		metrics := h.GetMetrics()
		
		latencyMs := int64(0)
		executions := int64(0)
		if lat, ok := metrics["last_execution_latency_ms"].(int64); ok {
			latencyMs = lat
		}
		if exec, ok := metrics["total_executions"].(int64); ok {
			executions = exec
		}

		healerStats[id] = map[string]interface{}{
			"last_execution_latency_ms": latencyMs,
			"total_executions":          executions,
			"is_healthy":                h.IsHealthy(),
		}

		totalLatencyMs += latencyMs
		totalExecutions += executions
	}

	avgLatency := int64(0)
	if totalExecutions > 0 {
		avgLatency = totalLatencyMs / totalExecutions
	}

	stats["total_healers"] = len(r.healers)
	stats["total_executions"] = totalExecutions
	stats["average_latency_ms"] = avgLatency
	stats["per_healer"] = healerStats

	return stats
}

// ResetAllHealers resets all registered healers
func (r *HealerRegistry) ResetAllHealers() {
	r.mu.RLock()
	healers := make([]HealerInterface, 0, len(r.healers))
	r.mu.RUnlock()

	for _, h := range healers {
		h.Reset()
	}

	r.logger.Info("Reset all healers")
}
