// Package scheduler - elastic_inference.go implements elastic inference scheduling.
// Supports both batch processing (high throughput) and streaming inference (low latency),
// with automatic mode switching based on request patterns and SLA requirements.
// Handles dynamic batching, request coalescing, and adaptive concurrency control.
package scheduler

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// Elastic Inference Configuration
// ============================================================================

// InferenceMode defines the inference execution mode
type InferenceMode string

const (
	InferenceModeBatch     InferenceMode = "batch"     // high throughput, higher latency
	InferenceModeStreaming  InferenceMode = "streaming" // low latency, lower throughput
	InferenceModeAdaptive  InferenceMode = "adaptive"  // auto-switch based on load
)

// ElasticInferenceConfig holds configuration for the elastic inference engine
type ElasticInferenceConfig struct {
	DefaultMode           InferenceMode `json:"default_mode"`
	MaxBatchSize          int           `json:"max_batch_size"`           // max requests per batch
	BatchTimeoutMs        int           `json:"batch_timeout_ms"`        // max wait time to fill a batch
	StreamingConcurrency  int           `json:"streaming_concurrency"`   // max concurrent streaming requests
	LatencySLAMs          int           `json:"latency_sla_ms"`          // P99 latency SLA in milliseconds
	ThroughputTarget      float64       `json:"throughput_target"`       // target requests/sec
	AdaptiveThreshold     float64       `json:"adaptive_threshold"`      // load threshold to switch modes
	ScaleUpThreshold      float64       `json:"scale_up_threshold"`      // GPU utilization to trigger scale-up
	ScaleDownThreshold    float64       `json:"scale_down_threshold"`    // GPU utilization to trigger scale-down
	ScaleUpCooldownSec    int           `json:"scale_up_cooldown_sec"`
	ScaleDownCooldownSec  int           `json:"scale_down_cooldown_sec"`
	MinReplicas           int           `json:"min_replicas"`
	MaxReplicas           int           `json:"max_replicas"`
}

// DefaultElasticInferenceConfig returns production-ready defaults
func DefaultElasticInferenceConfig() ElasticInferenceConfig {
	return ElasticInferenceConfig{
		DefaultMode:          InferenceModeAdaptive,
		MaxBatchSize:         32,
		BatchTimeoutMs:       50,
		StreamingConcurrency: 64,
		LatencySLAMs:         200,
		ThroughputTarget:     1000,
		AdaptiveThreshold:    0.6,
		ScaleUpThreshold:     80.0,
		ScaleDownThreshold:   30.0,
		ScaleUpCooldownSec:   60,
		ScaleDownCooldownSec: 300,
		MinReplicas:          1,
		MaxReplicas:          16,
	}
}

// ============================================================================
// Inference Endpoint & Request Models
// ============================================================================

// InferenceEndpoint represents a deployed inference service
type InferenceEndpoint struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	ModelName       string         `json:"model_name"`
	ModelVersion    string         `json:"model_version"`
	Mode            InferenceMode  `json:"mode"`
	Replicas        int            `json:"replicas"`
	DesiredReplicas int            `json:"desired_replicas"`
	GPUType         string         `json:"gpu_type"`
	GPUPerReplica   int            `json:"gpu_per_replica"`
	GPUShareMode    GPUShareMode   `json:"gpu_share_mode"`
	BatchConfig     *BatchConfig   `json:"batch_config,omitempty"`
	StreamConfig    *StreamConfig  `json:"stream_config,omitempty"`
	Metrics         *InferenceMetrics `json:"metrics,omitempty"`
	Status          string         `json:"status"` // "running", "scaling", "degraded"
	CreatedAt       time.Time      `json:"created_at"`
	LastScaledAt    time.Time      `json:"last_scaled_at"`
}

// BatchConfig holds batch inference specific settings
type BatchConfig struct {
	MaxBatchSize    int   `json:"max_batch_size"`
	BatchTimeoutMs  int   `json:"batch_timeout_ms"`
	DynamicBatching bool  `json:"dynamic_batching"` // auto-adjust batch size
	PaddingEnabled  bool  `json:"padding_enabled"`  // pad sequences to uniform length
	MaxPaddingRatio float64 `json:"max_padding_ratio"` // max wasted compute from padding
}

// StreamConfig holds streaming inference specific settings
type StreamConfig struct {
	MaxConcurrency    int   `json:"max_concurrency"`
	RequestTimeoutMs  int   `json:"request_timeout_ms"`
	KeepAliveMs       int   `json:"keep_alive_ms"`
	TokenStreaming     bool  `json:"token_streaming"`    // stream tokens for LLMs
	MaxTokensPerSec   int   `json:"max_tokens_per_sec"` // rate limit for token generation
}

// InferenceMetrics tracks real-time inference performance
type InferenceMetrics struct {
	RequestsPerSec     float64        `json:"requests_per_sec"`
	AvgLatencyMs       float64        `json:"avg_latency_ms"`
	P50LatencyMs       float64        `json:"p50_latency_ms"`
	P95LatencyMs       float64        `json:"p95_latency_ms"`
	P99LatencyMs       float64        `json:"p99_latency_ms"`
	GPUUtilization     float64        `json:"gpu_utilization_percent"`
	GPUMemoryUsage     float64        `json:"gpu_memory_usage_percent"`
	BatchFillRate      float64        `json:"batch_fill_rate"`       // avg batch utilization
	QueueDepth         int            `json:"queue_depth"`           // pending requests
	ErrorRate          float64        `json:"error_rate"`
	TotalRequests      int64          `json:"total_requests"`
	CurrentMode        InferenceMode  `json:"current_mode"`
	TokensPerSec       float64        `json:"tokens_per_sec,omitempty"` // for LLM serving
	TimeToFirstTokenMs float64        `json:"time_to_first_token_ms,omitempty"`
}

// ============================================================================
// Elastic Inference Manager
// ============================================================================

// ElasticInferenceManager manages elastic inference endpoints
type ElasticInferenceManager struct {
	config    ElasticInferenceConfig
	endpoints map[string]*InferenceEndpoint
	logger    *logrus.Logger
	mu        sync.RWMutex
}

// NewElasticInferenceManager creates a new elastic inference manager
func NewElasticInferenceManager(cfg ElasticInferenceConfig) *ElasticInferenceManager {
	return &ElasticInferenceManager{
		config:    cfg,
		endpoints: make(map[string]*InferenceEndpoint),
		logger:    logrus.StandardLogger(),
	}
}

// CreateEndpoint registers a new inference endpoint
func (eim *ElasticInferenceManager) CreateEndpoint(name, modelName, modelVersion, gpuType string, gpuPerReplica int) (*InferenceEndpoint, error) {
	eim.mu.Lock()
	defer eim.mu.Unlock()

	ep := &InferenceEndpoint{
		ID:              common.NewUUID(),
		Name:            name,
		ModelName:       modelName,
		ModelVersion:    modelVersion,
		Mode:            eim.config.DefaultMode,
		Replicas:        eim.config.MinReplicas,
		DesiredReplicas: eim.config.MinReplicas,
		GPUType:         gpuType,
		GPUPerReplica:   gpuPerReplica,
		GPUShareMode:    GPUShareMPS,
		BatchConfig: &BatchConfig{
			MaxBatchSize:    eim.config.MaxBatchSize,
			BatchTimeoutMs:  eim.config.BatchTimeoutMs,
			DynamicBatching: true,
			PaddingEnabled:  true,
			MaxPaddingRatio: 0.3,
		},
		StreamConfig: &StreamConfig{
			MaxConcurrency:   eim.config.StreamingConcurrency,
			RequestTimeoutMs: eim.config.LatencySLAMs * 5,
			KeepAliveMs:      30000,
			TokenStreaming:    true,
			MaxTokensPerSec:  500,
		},
		Metrics: &InferenceMetrics{
			CurrentMode: eim.config.DefaultMode,
		},
		Status:    "running",
		CreatedAt: common.NowUTC(),
	}

	eim.endpoints[ep.ID] = ep

	eim.logger.WithFields(logrus.Fields{
		"endpoint": ep.Name,
		"model":    modelName,
		"mode":     ep.Mode,
		"replicas": ep.Replicas,
	}).Info("Inference endpoint created")

	return ep, nil
}

// EvaluateScaling evaluates and applies auto-scaling decisions for all endpoints
func (eim *ElasticInferenceManager) EvaluateScaling(ctx context.Context) []ScalingDecision {
	eim.mu.Lock()
	defer eim.mu.Unlock()

	var decisions []ScalingDecision
	now := common.NowUTC()

	for _, ep := range eim.endpoints {
		if ep.Metrics == nil || ep.Status != "running" {
			continue
		}

		decision := eim.computeScalingDecision(ep, now)
		if decision != nil {
			decisions = append(decisions, *decision)
			// Apply the decision
			ep.DesiredReplicas = decision.DesiredReplicas
			ep.LastScaledAt = now
			ep.Status = "scaling"
		}

		// Evaluate mode switching for adaptive endpoints
		if ep.Mode == InferenceModeAdaptive {
			newMode := eim.evaluateModeSwitch(ep)
			if newMode != ep.Metrics.CurrentMode {
				ep.Metrics.CurrentMode = newMode
				eim.logger.WithFields(logrus.Fields{
					"endpoint": ep.Name,
					"from":     ep.Metrics.CurrentMode,
					"to":       newMode,
				}).Info("Inference mode switched")
			}
		}
	}

	return decisions
}

// ScalingDecision represents an auto-scaling action
type ScalingDecision struct {
	EndpointID      string    `json:"endpoint_id"`
	EndpointName    string    `json:"endpoint_name"`
	CurrentReplicas int       `json:"current_replicas"`
	DesiredReplicas int       `json:"desired_replicas"`
	Direction       string    `json:"direction"` // "up", "down", "none"
	Reason          string    `json:"reason"`
	Timestamp       time.Time `json:"timestamp"`
}

// computeScalingDecision determines whether an endpoint needs scaling
func (eim *ElasticInferenceManager) computeScalingDecision(ep *InferenceEndpoint, now time.Time) *ScalingDecision {
	metrics := ep.Metrics

	// Scale up conditions
	if metrics.GPUUtilization > eim.config.ScaleUpThreshold ||
		metrics.P99LatencyMs > float64(eim.config.LatencySLAMs) ||
		metrics.QueueDepth > eim.config.MaxBatchSize*2 {

		cooldown := time.Duration(eim.config.ScaleUpCooldownSec) * time.Second
		if now.Sub(ep.LastScaledAt) < cooldown {
			return nil
		}

		desired := ep.Replicas + computeScaleUpDelta(metrics, eim.config)
		if desired > eim.config.MaxReplicas {
			desired = eim.config.MaxReplicas
		}
		if desired == ep.Replicas {
			return nil
		}

		return &ScalingDecision{
			EndpointID:      ep.ID,
			EndpointName:    ep.Name,
			CurrentReplicas: ep.Replicas,
			DesiredReplicas: desired,
			Direction:       "up",
			Reason: fmt.Sprintf("GPU util %.1f%% > %.1f%% or P99 %.0fms > %dms",
				metrics.GPUUtilization, eim.config.ScaleUpThreshold,
				metrics.P99LatencyMs, eim.config.LatencySLAMs),
			Timestamp: now,
		}
	}

	// Scale down conditions
	if metrics.GPUUtilization < eim.config.ScaleDownThreshold &&
		metrics.P99LatencyMs < float64(eim.config.LatencySLAMs)*0.5 &&
		metrics.QueueDepth == 0 {

		cooldown := time.Duration(eim.config.ScaleDownCooldownSec) * time.Second
		if now.Sub(ep.LastScaledAt) < cooldown {
			return nil
		}

		desired := ep.Replicas - 1
		if desired < eim.config.MinReplicas {
			desired = eim.config.MinReplicas
		}
		if desired == ep.Replicas {
			return nil
		}

		return &ScalingDecision{
			EndpointID:      ep.ID,
			EndpointName:    ep.Name,
			CurrentReplicas: ep.Replicas,
			DesiredReplicas: desired,
			Direction:       "down",
			Reason: fmt.Sprintf("GPU util %.1f%% < %.1f%% and queue empty",
				metrics.GPUUtilization, eim.config.ScaleDownThreshold),
			Timestamp: now,
		}
	}

	return nil
}

// computeScaleUpDelta calculates how many replicas to add
func computeScaleUpDelta(metrics *InferenceMetrics, cfg ElasticInferenceConfig) int {
	// Proportional scaling based on how far over threshold
	overUtil := (metrics.GPUUtilization - cfg.ScaleUpThreshold) / 100.0
	overLatency := 0.0
	if cfg.LatencySLAMs > 0 {
		overLatency = (metrics.P99LatencyMs - float64(cfg.LatencySLAMs)) / float64(cfg.LatencySLAMs)
	}

	factor := math.Max(overUtil, overLatency)
	delta := int(math.Ceil(factor * 3)) // aggressive scaling
	if delta < 1 {
		delta = 1
	}
	if delta > 4 {
		delta = 4 // max 4 replicas at a time
	}
	return delta
}

// evaluateModeSwitch determines optimal inference mode based on current metrics
func (eim *ElasticInferenceManager) evaluateModeSwitch(ep *InferenceEndpoint) InferenceMode {
	metrics := ep.Metrics
	if metrics == nil {
		return InferenceModeBatch
	}

	// High throughput demand → batch mode
	if metrics.RequestsPerSec > eim.config.ThroughputTarget*eim.config.AdaptiveThreshold &&
		metrics.P99LatencyMs < float64(eim.config.LatencySLAMs) {
		return InferenceModeBatch
	}

	// Latency-sensitive or low throughput → streaming mode
	if metrics.P99LatencyMs > float64(eim.config.LatencySLAMs)*0.8 ||
		metrics.RequestsPerSec < eim.config.ThroughputTarget*0.2 {
		return InferenceModeStreaming
	}

	// Default: keep current mode
	return metrics.CurrentMode
}

// UpdateEndpointMetrics updates metrics for an endpoint
func (eim *ElasticInferenceManager) UpdateEndpointMetrics(endpointID string, metrics *InferenceMetrics) error {
	eim.mu.Lock()
	defer eim.mu.Unlock()

	ep, ok := eim.endpoints[endpointID]
	if !ok {
		return fmt.Errorf("endpoint %s not found", endpointID)
	}
	// Preserve current mode during metrics update
	if ep.Metrics != nil {
		metrics.CurrentMode = ep.Metrics.CurrentMode
	}
	ep.Metrics = metrics
	return nil
}

// OptimizeBatchSize dynamically adjusts batch size based on metrics
func OptimizeBatchSize(current int, metrics *InferenceMetrics, config ElasticInferenceConfig) int {
	if metrics == nil {
		return current
	}

	// If batch fill rate is low, reduce batch size to reduce latency
	if metrics.BatchFillRate < 0.5 && current > 4 {
		return int(math.Max(4, float64(current)*0.75))
	}

	// If latency is well within SLA and fill rate is high, increase batch size
	if metrics.BatchFillRate > 0.9 && metrics.P99LatencyMs < float64(config.LatencySLAMs)*0.6 {
		newSize := int(math.Min(float64(config.MaxBatchSize), float64(current)*1.25))
		return newSize
	}

	return current
}

// GetEndpoints returns all inference endpoints
func (eim *ElasticInferenceManager) GetEndpoints() []*InferenceEndpoint {
	eim.mu.RLock()
	defer eim.mu.RUnlock()

	result := make([]*InferenceEndpoint, 0, len(eim.endpoints))
	for _, ep := range eim.endpoints {
		result = append(result, ep)
	}
	return result
}

// GetEndpoint returns a specific inference endpoint
func (eim *ElasticInferenceManager) GetEndpoint(id string) (*InferenceEndpoint, error) {
	eim.mu.RLock()
	defer eim.mu.RUnlock()

	ep, ok := eim.endpoints[id]
	if !ok {
		return nil, fmt.Errorf("endpoint %s not found", id)
	}
	return ep, nil
}

// DeleteEndpoint removes an inference endpoint
func (eim *ElasticInferenceManager) DeleteEndpoint(id string) error {
	eim.mu.Lock()
	defer eim.mu.Unlock()

	if _, ok := eim.endpoints[id]; !ok {
		return fmt.Errorf("endpoint %s not found", id)
	}
	delete(eim.endpoints, id)
	return nil
}
