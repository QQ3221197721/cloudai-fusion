// Package aiops provides AIOps (AI for IT Operations) capabilities for
// CloudAI Fusion, including predictive auto-scaling with multi-metric
// decision making, cooldown management, and scaling event history.
package aiops

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Predictive Auto-Scaler
// ============================================================================

// PredictiveAutoscaler implements predictive horizontal pod auto-scaling.
// Uses time-series forecasting and multi-metric analysis to proactively
// scale workloads before demand spikes, reducing latency impact.
type PredictiveAutoscaler struct {
	workloads    map[string]*ScalableWorkload
	metricsDB    map[string][]MetricSample // workloadID → samples
	scalingRules map[string]*ScalingRule
	history      []ScalingEvent
	config       AutoscaleConfig
	mu           sync.RWMutex
	logger       *logrus.Logger
}

// AutoscaleConfig configures the auto-scaler.
type AutoscaleConfig struct {
	EvaluationInterval   time.Duration `json:"evaluation_interval" yaml:"evaluationInterval"`       // e.g., 30s
	PredictionWindow     time.Duration `json:"prediction_window" yaml:"predictionWindow"`           // e.g., 5min ahead
	MetricHistoryWindow  time.Duration `json:"metric_history_window" yaml:"metricHistoryWindow"`    // e.g., 1h
	ScaleUpCooldown      time.Duration `json:"scale_up_cooldown" yaml:"scaleUpCooldown"`            // e.g., 3min
	ScaleDownCooldown    time.Duration `json:"scale_down_cooldown" yaml:"scaleDownCooldown"`        // e.g., 5min
	ScaleUpMaxStep       int           `json:"scale_up_max_step" yaml:"scaleUpMaxStep"`             // max replicas to add
	ScaleDownMaxStep     int           `json:"scale_down_max_step" yaml:"scaleDownMaxStep"`         // max replicas to remove
	StabilizationWindow  time.Duration `json:"stabilization_window" yaml:"stabilizationWindow"`     // e.g., 2min
	Tolerance            float64       `json:"tolerance" yaml:"tolerance"`                          // e.g., 0.1 = 10%
	EnablePredictive     bool          `json:"enable_predictive" yaml:"enablePredictive"`
}

// DefaultAutoscaleConfig returns sensible defaults.
func DefaultAutoscaleConfig() AutoscaleConfig {
	return AutoscaleConfig{
		EvaluationInterval:  30 * time.Second,
		PredictionWindow:    5 * time.Minute,
		MetricHistoryWindow: 1 * time.Hour,
		ScaleUpCooldown:     3 * time.Minute,
		ScaleDownCooldown:   5 * time.Minute,
		ScaleUpMaxStep:      10,
		ScaleDownMaxStep:    3,
		StabilizationWindow: 2 * time.Minute,
		Tolerance:           0.1,
		EnablePredictive:    true,
	}
}

// ScalableWorkload represents a workload that can be auto-scaled.
type ScalableWorkload struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Namespace       string    `json:"namespace"`
	Kind            string    `json:"kind"` // Deployment, StatefulSet
	CurrentReplicas int       `json:"current_replicas"`
	MinReplicas     int       `json:"min_replicas"`
	MaxReplicas     int       `json:"max_replicas"`
	TargetMetrics   []TargetMetric `json:"target_metrics"`
	LastScaleTime   time.Time `json:"last_scale_time"`
	LastScaleDir    string    `json:"last_scale_direction"` // up, down
}

// TargetMetric defines a scaling metric target.
type TargetMetric struct {
	Name          string  `json:"name"`           // cpu, memory, gpu, rps, latency_p99
	Type          string  `json:"type"`           // utilization, average, value
	TargetValue   float64 `json:"target_value"`
	Weight        float64 `json:"weight"`         // 0-1, for multi-metric decisions
	ScaleUpAt     float64 `json:"scale_up_at"`    // threshold to trigger scale up
	ScaleDownAt   float64 `json:"scale_down_at"`  // threshold to trigger scale down
}

// MetricSample is a single metric observation.
type MetricSample struct {
	Timestamp   time.Time `json:"timestamp"`
	WorkloadID  string    `json:"workload_id"`
	MetricName  string    `json:"metric_name"`
	Value       float64   `json:"value"`
	Replicas    int       `json:"replicas"`
}

// ScalingRule defines custom scaling behavior.
type ScalingRule struct {
	WorkloadID     string        `json:"workload_id"`
	Schedule       []ScheduledScale `json:"schedule,omitempty"`
	Behavior       ScalingBehavior  `json:"behavior"`
}

// ScheduledScale defines scheduled scaling (e.g., scale up before known peaks).
type ScheduledScale struct {
	CronExpr    string `json:"cron_expr"`
	MinReplicas int    `json:"min_replicas"`
	MaxReplicas int    `json:"max_replicas"`
	Reason      string `json:"reason"`
}

// ScalingBehavior defines scale up/down policies.
type ScalingBehavior struct {
	ScaleUp   ScalePolicy `json:"scale_up"`
	ScaleDown ScalePolicy `json:"scale_down"`
}

// ScalePolicy defines how to scale in a direction.
type ScalePolicy struct {
	StabilizationWindow time.Duration `json:"stabilization_window"`
	MaxStepSize         int           `json:"max_step_size"`
	Policies            []PolicyRule  `json:"policies"`
}

// PolicyRule defines a single scaling policy rule.
type PolicyRule struct {
	Type          string        `json:"type"`  // pods, percent
	Value         int           `json:"value"`
	PeriodSeconds time.Duration `json:"period"`
}

// ScalingEvent records a scaling action.
type ScalingEvent struct {
	Timestamp     time.Time `json:"timestamp"`
	WorkloadID    string    `json:"workload_id"`
	WorkloadName  string    `json:"workload_name"`
	Direction     string    `json:"direction"` // up, down
	FromReplicas  int       `json:"from_replicas"`
	ToReplicas    int       `json:"to_replicas"`
	Reason        string    `json:"reason"`
	Metrics       map[string]float64 `json:"metrics"`
	Predicted     bool      `json:"predicted"` // was this a predictive scale?
	Duration      time.Duration `json:"decision_duration"`
}

// ScalingDecision is the output of an evaluation cycle.
type ScalingDecision struct {
	WorkloadID      string             `json:"workload_id"`
	CurrentReplicas int                `json:"current_replicas"`
	DesiredReplicas int                `json:"desired_replicas"`
	Direction       string             `json:"direction"` // up, down, none
	Reason          string             `json:"reason"`
	MetricValues    map[string]float64 `json:"metric_values"`
	Predicted       bool               `json:"predicted"`
	Confidence      float64            `json:"confidence"`
	Blocked         bool               `json:"blocked"`
	BlockReason     string             `json:"block_reason,omitempty"`
}

// NewPredictiveAutoscaler creates a new predictive auto-scaler.
func NewPredictiveAutoscaler(cfg AutoscaleConfig, logger *logrus.Logger) *PredictiveAutoscaler {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &PredictiveAutoscaler{
		workloads:    make(map[string]*ScalableWorkload),
		metricsDB:    make(map[string][]MetricSample),
		scalingRules: make(map[string]*ScalingRule),
		config:       cfg,
		logger:       logger,
	}
}

// RegisterWorkload registers a workload for auto-scaling.
func (a *PredictiveAutoscaler) RegisterWorkload(w *ScalableWorkload) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.workloads[w.ID] = w
	a.logger.WithFields(logrus.Fields{
		"workload":     w.Name,
		"min_replicas": w.MinReplicas,
		"max_replicas": w.MaxReplicas,
		"metrics":      len(w.TargetMetrics),
	}).Info("Workload registered for auto-scaling")
}

// SetScalingRule sets a custom scaling rule for a workload.
func (a *PredictiveAutoscaler) SetScalingRule(rule *ScalingRule) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.scalingRules[rule.WorkloadID] = rule
}

// IngestMetrics adds metric samples for analysis.
func (a *PredictiveAutoscaler) IngestMetrics(samples []MetricSample) {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, s := range samples {
		a.metricsDB[s.WorkloadID] = append(a.metricsDB[s.WorkloadID], s)
	}

	// Trim old metrics
	cutoff := time.Now().Add(-a.config.MetricHistoryWindow)
	for k, samples := range a.metricsDB {
		trimmed := make([]MetricSample, 0, len(samples))
		for _, s := range samples {
			if s.Timestamp.After(cutoff) {
				trimmed = append(trimmed, s)
			}
		}
		a.metricsDB[k] = trimmed
	}
}

// Evaluate runs a single evaluation cycle for a workload.
func (a *PredictiveAutoscaler) Evaluate(ctx context.Context, workloadID string) (*ScalingDecision, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	a.mu.RLock()
	w, ok := a.workloads[workloadID]
	if !ok {
		a.mu.RUnlock()
		return nil, fmt.Errorf("workload %q not registered", workloadID)
	}
	samples := a.metricsDB[workloadID]
	a.mu.RUnlock()

	start := time.Now()
	decision := &ScalingDecision{
		WorkloadID:      workloadID,
		CurrentReplicas: w.CurrentReplicas,
		DesiredReplicas: w.CurrentReplicas,
		Direction:       "none",
		MetricValues:    make(map[string]float64),
	}

	if len(samples) == 0 {
		decision.Reason = "no metrics available"
		return decision, nil
	}

	// Calculate current metric values
	currentValues := a.calculateCurrentMetrics(w, samples)
	decision.MetricValues = currentValues

	// Multi-metric weighted decision
	weightedDesired := 0.0
	totalWeight := 0.0

	for _, target := range w.TargetMetrics {
		currentVal, ok := currentValues[target.Name]
		if !ok {
			continue
		}

		// Calculate desired replicas based on this metric
		ratio := currentVal / target.TargetValue
		metricDesired := float64(w.CurrentReplicas) * ratio

		// Apply predictive adjustment
		if a.config.EnablePredictive {
			predicted := a.predictMetric(workloadID, target.Name, samples)
			if predicted > currentVal {
				predictedRatio := predicted / target.TargetValue
				metricDesired = math.Max(metricDesired, float64(w.CurrentReplicas)*predictedRatio)
				decision.Predicted = true
			}
		}

		weightedDesired += metricDesired * target.Weight
		totalWeight += target.Weight
	}

	if totalWeight > 0 {
		desired := int(math.Ceil(weightedDesired / totalWeight))

		// Apply tolerance
		upperBound := float64(w.CurrentReplicas) * (1 + a.config.Tolerance)
		lowerBound := float64(w.CurrentReplicas) * (1 - a.config.Tolerance)

		if float64(desired) > upperBound || float64(desired) < lowerBound {
			decision.DesiredReplicas = desired
		}
	}

	// Clamp to min/max
	decision.DesiredReplicas = clampInt(decision.DesiredReplicas, w.MinReplicas, w.MaxReplicas)

	// Apply step limits
	diff := decision.DesiredReplicas - w.CurrentReplicas
	if diff > a.config.ScaleUpMaxStep {
		decision.DesiredReplicas = w.CurrentReplicas + a.config.ScaleUpMaxStep
	} else if diff < -a.config.ScaleDownMaxStep {
		decision.DesiredReplicas = w.CurrentReplicas - a.config.ScaleDownMaxStep
	}

	// Set direction
	if decision.DesiredReplicas > w.CurrentReplicas {
		decision.Direction = "up"
		decision.Reason = fmt.Sprintf("metrics above target (weighted avg needs %d replicas)", decision.DesiredReplicas)
	} else if decision.DesiredReplicas < w.CurrentReplicas {
		decision.Direction = "down"
		decision.Reason = fmt.Sprintf("metrics below target (weighted avg needs %d replicas)", decision.DesiredReplicas)
	}

	// Cooldown check
	if decision.Direction != "none" {
		blocked, reason := a.checkCooldown(w, decision.Direction)
		if blocked {
			decision.Blocked = true
			decision.BlockReason = reason
			decision.DesiredReplicas = w.CurrentReplicas
			decision.Direction = "none"
		}
	}

	// Calculate confidence
	decision.Confidence = a.calculateConfidence(samples, decision)

	elapsed := time.Since(start)

	a.logger.WithFields(logrus.Fields{
		"workload":  w.Name,
		"current":   w.CurrentReplicas,
		"desired":   decision.DesiredReplicas,
		"direction": decision.Direction,
		"predicted": decision.Predicted,
		"blocked":   decision.Blocked,
		"duration":  elapsed,
	}).Debug("Autoscale evaluation completed")

	return decision, nil
}

// ApplyDecision applies a scaling decision and records the event.
func (a *PredictiveAutoscaler) ApplyDecision(decision *ScalingDecision) error {
	if decision.Direction == "none" || decision.Blocked {
		return nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	w, ok := a.workloads[decision.WorkloadID]
	if !ok {
		return fmt.Errorf("workload %q not found", decision.WorkloadID)
	}

	event := ScalingEvent{
		Timestamp:    time.Now(),
		WorkloadID:   decision.WorkloadID,
		WorkloadName: w.Name,
		Direction:    decision.Direction,
		FromReplicas: w.CurrentReplicas,
		ToReplicas:   decision.DesiredReplicas,
		Reason:       decision.Reason,
		Metrics:      decision.MetricValues,
		Predicted:    decision.Predicted,
	}

	w.CurrentReplicas = decision.DesiredReplicas
	w.LastScaleTime = time.Now()
	w.LastScaleDir = decision.Direction

	a.history = append(a.history, event)

	a.logger.WithFields(logrus.Fields{
		"workload":  w.Name,
		"from":      event.FromReplicas,
		"to":        event.ToReplicas,
		"direction": event.Direction,
		"predicted": event.Predicted,
	}).Info("Scaling decision applied")

	return nil
}

// EvaluateAll evaluates all registered workloads.
func (a *PredictiveAutoscaler) EvaluateAll(ctx context.Context) ([]*ScalingDecision, error) {
	a.mu.RLock()
	ids := make([]string, 0, len(a.workloads))
	for id := range a.workloads {
		ids = append(ids, id)
	}
	a.mu.RUnlock()

	var decisions []*ScalingDecision
	for _, id := range ids {
		d, err := a.Evaluate(ctx, id)
		if err != nil {
			a.logger.WithError(err).WithField("workload", id).Warn("Evaluation failed")
			continue
		}
		decisions = append(decisions, d)
	}
	return decisions, nil
}

// GetHistory returns scaling event history.
func (a *PredictiveAutoscaler) GetHistory(workloadID string, limit int) []ScalingEvent {
	a.mu.RLock()
	defer a.mu.RUnlock()

	var events []ScalingEvent
	for i := len(a.history) - 1; i >= 0 && len(events) < limit; i-- {
		if workloadID == "" || a.history[i].WorkloadID == workloadID {
			events = append(events, a.history[i])
		}
	}
	return events
}

// ============================================================================
// Internal Methods
// ============================================================================

func (a *PredictiveAutoscaler) calculateCurrentMetrics(w *ScalableWorkload, samples []MetricSample) map[string]float64 {
	result := make(map[string]float64)

	// Group by metric name and get recent values
	recentCutoff := time.Now().Add(-a.config.EvaluationInterval * 3)
	metricValues := make(map[string][]float64)

	for _, s := range samples {
		if s.Timestamp.After(recentCutoff) {
			metricValues[s.MetricName] = append(metricValues[s.MetricName], s.Value)
		}
	}

	for name, values := range metricValues {
		if len(values) == 0 {
			continue
		}
		// Use average of recent samples
		sum := 0.0
		for _, v := range values {
			sum += v
		}
		result[name] = sum / float64(len(values))
	}

	return result
}

func (a *PredictiveAutoscaler) predictMetric(workloadID, metricName string, samples []MetricSample) float64 {
	// Filter for this metric
	var values []float64
	for _, s := range samples {
		if s.MetricName == metricName {
			values = append(values, s.Value)
		}
	}

	if len(values) < 10 {
		return 0
	}

	// Simple linear regression + EMA for prediction
	n := len(values)
	sumX, sumY, sumXY, sumX2 := 0.0, 0.0, 0.0, 0.0
	for i, v := range values {
		x := float64(i)
		sumX += x
		sumY += v
		sumXY += x * v
		sumX2 += x * x
	}
	nf := float64(n)
	denom := nf*sumX2 - sumX*sumX
	if denom == 0 {
		return values[n-1]
	}

	slope := (nf*sumXY - sumX*sumY) / denom
	intercept := (sumY - slope*sumX) / nf

	// Predict future point
	futureSteps := int(a.config.PredictionWindow / a.config.EvaluationInterval)
	predicted := intercept + slope*float64(n+futureSteps)

	// Blend with EMA
	ema := values[0]
	alpha := 0.3
	for i := 1; i < n; i++ {
		ema = alpha*values[i] + (1-alpha)*ema
	}

	return predicted*0.6 + ema*0.4
}

func (a *PredictiveAutoscaler) checkCooldown(w *ScalableWorkload, direction string) (bool, string) {
	if w.LastScaleTime.IsZero() {
		return false, ""
	}

	elapsed := time.Since(w.LastScaleTime)

	if direction == "up" && elapsed < a.config.ScaleUpCooldown {
		remaining := a.config.ScaleUpCooldown - elapsed
		return true, fmt.Sprintf("scale-up cooldown: %v remaining", remaining.Round(time.Second))
	}

	if direction == "down" && elapsed < a.config.ScaleDownCooldown {
		remaining := a.config.ScaleDownCooldown - elapsed
		return true, fmt.Sprintf("scale-down cooldown: %v remaining", remaining.Round(time.Second))
	}

	return false, ""
}

func (a *PredictiveAutoscaler) calculateConfidence(samples []MetricSample, decision *ScalingDecision) float64 {
	if len(samples) < 5 {
		return 0.3
	}

	// More data = higher confidence
	dataConf := math.Min(float64(len(samples))/100.0, 1.0)

	// Consistency of recent values increases confidence
	var recentValues []float64
	cutoff := time.Now().Add(-5 * time.Minute)
	for _, s := range samples {
		if s.Timestamp.After(cutoff) {
			recentValues = append(recentValues, s.Value)
		}
	}

	stabilityConf := 1.0
	if len(recentValues) > 1 {
		mean := 0.0
		for _, v := range recentValues {
			mean += v
		}
		mean /= float64(len(recentValues))
		variance := 0.0
		for _, v := range recentValues {
			d := v - mean
			variance += d * d
		}
		variance /= float64(len(recentValues))
		cv := math.Sqrt(variance) / math.Max(mean, 0.001) // coefficient of variation
		stabilityConf = math.Max(0, 1-cv)
	}

	return dataConf*0.4 + stabilityConf*0.6
}

// GetScalingSummary returns a summary of recent scaling activity.
func (a *PredictiveAutoscaler) GetScalingSummary() *ScalingSummary {
	a.mu.RLock()
	defer a.mu.RUnlock()

	summary := &ScalingSummary{
		WorkloadCount: len(a.workloads),
		ByWorkload:    make(map[string]*WorkloadScalingSummary),
		GeneratedAt:   time.Now(),
	}

	last24h := time.Now().Add(-24 * time.Hour)

	for _, event := range a.history {
		if event.Timestamp.After(last24h) {
			summary.TotalEvents++
			if event.Direction == "up" {
				summary.ScaleUpCount++
			} else {
				summary.ScaleDownCount++
			}
			if event.Predicted {
				summary.PredictiveCount++
			}

			ws, ok := summary.ByWorkload[event.WorkloadID]
			if !ok {
				ws = &WorkloadScalingSummary{WorkloadID: event.WorkloadID, WorkloadName: event.WorkloadName}
				summary.ByWorkload[event.WorkloadID] = ws
			}
			ws.EventCount++
		}
	}

	return summary
}

// ScalingSummary provides an overview of scaling activity.
type ScalingSummary struct {
	WorkloadCount   int                                `json:"workload_count"`
	TotalEvents     int                                `json:"total_events_24h"`
	ScaleUpCount    int                                `json:"scale_up_count"`
	ScaleDownCount  int                                `json:"scale_down_count"`
	PredictiveCount int                                `json:"predictive_count"`
	ByWorkload      map[string]*WorkloadScalingSummary `json:"by_workload"`
	GeneratedAt     time.Time                          `json:"generated_at"`
}

// WorkloadScalingSummary tracks scaling for a single workload.
type WorkloadScalingSummary struct {
	WorkloadID   string `json:"workload_id"`
	WorkloadName string `json:"workload_name"`
	EventCount   int    `json:"event_count"`
}

// ============================================================================
// Helpers
// ============================================================================

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// SortScalingEventsByTime sorts scaling events by timestamp descending.
func SortScalingEventsByTime(events []ScalingEvent) {
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.After(events[j].Timestamp)
	})
}
