// Package scheduler - predictive_scaling.go implements predictive auto-scaling
// based on historical load patterns. Uses exponential smoothing, seasonal decomposition,
// and trend analysis to forecast future resource demand and proactively scale
// GPU resources before demand spikes.
package scheduler

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Predictive Scaling Configuration
// ============================================================================

// PredictiveScalingConfig holds configuration for predictive scaling
type PredictiveScalingConfig struct {
	HistoryWindowHours    int     `json:"history_window_hours"`    // hours of history to consider
	PredictionHorizonMin  int     `json:"prediction_horizon_min"`  // minutes ahead to predict
	SeasonalPeriodHours   int     `json:"seasonal_period_hours"`   // seasonal cycle length (24=daily)
	SmoothingAlpha        float64 `json:"smoothing_alpha"`         // EWM smoothing factor (0-1)
	TrendBeta             float64 `json:"trend_beta"`              // trend smoothing factor (0-1)
	SeasonalGamma         float64 `json:"seasonal_gamma"`          // seasonal smoothing factor (0-1)
	ConfidenceLevel       float64 `json:"confidence_level"`        // prediction confidence (0.8-0.99)
	ScaleUpBufferPercent  float64 `json:"scale_up_buffer_percent"` // headroom above prediction
	MinDataPointsRequired int     `json:"min_data_points"`         // minimum samples before predicting
	Enabled               bool    `json:"enabled"`
}

// DefaultPredictiveScalingConfig returns production-ready defaults
func DefaultPredictiveScalingConfig() PredictiveScalingConfig {
	return PredictiveScalingConfig{
		HistoryWindowHours:    168, // 7 days
		PredictionHorizonMin:  30,  // 30 minutes ahead
		SeasonalPeriodHours:   24,  // daily pattern
		SmoothingAlpha:        0.3,
		TrendBeta:             0.1,
		SeasonalGamma:         0.2,
		ConfidenceLevel:       0.95,
		ScaleUpBufferPercent:  15.0,
		MinDataPointsRequired: 48, // at least 48 data points (e.g., 2 days at hourly)
		Enabled:               true,
	}
}

// ============================================================================
// Load Data Point & Forecast Models
// ============================================================================

// LoadDataPoint represents a single resource utilization measurement
type LoadDataPoint struct {
	Timestamp      time.Time `json:"timestamp"`
	GPUUtilization float64   `json:"gpu_utilization"`
	GPUMemoryUsage float64   `json:"gpu_memory_usage"`
	QueueDepth     int       `json:"queue_depth"`
	ActiveWorkloads int      `json:"active_workloads"`
	RequestRate    float64   `json:"request_rate"` // requests/sec for inference
}

// LoadForecast represents a predicted future load
type LoadForecast struct {
	Timestamp           time.Time `json:"timestamp"`
	PredictedGPUUtil    float64   `json:"predicted_gpu_utilization"`
	PredictedMemory     float64   `json:"predicted_gpu_memory_usage"`
	PredictedQueueDepth float64   `json:"predicted_queue_depth"`
	PredictedRequests   float64   `json:"predicted_request_rate"`
	ConfidenceLow       float64   `json:"confidence_low"`
	ConfidenceHigh      float64   `json:"confidence_high"`
	TrendDirection      string    `json:"trend_direction"` // "increasing", "decreasing", "stable"
	SeasonalFactor      float64   `json:"seasonal_factor"` // multiplier for seasonal component
}

// ScalingRecommendation represents a proactive scaling suggestion
type ScalingRecommendation struct {
	Timestamp        time.Time     `json:"timestamp"`
	CurrentGPUs      int           `json:"current_gpus"`
	RecommendedGPUs  int           `json:"recommended_gpus"`
	PredictedPeakUtil float64      `json:"predicted_peak_utilization"`
	Confidence       float64       `json:"confidence"`
	Reason           string        `json:"reason"`
	ScaleAction      string        `json:"scale_action"` // "scale_up", "scale_down", "maintain"
	LeadTimeMin      int           `json:"lead_time_min"`
	Forecasts        []LoadForecast `json:"forecasts,omitempty"`
}

// ============================================================================
// Holt-Winters Triple Exponential Smoothing
// ============================================================================

// HoltWinters implements triple exponential smoothing for seasonal forecasting
type HoltWinters struct {
	alpha        float64   // level smoothing
	beta         float64   // trend smoothing
	gamma        float64   // seasonal smoothing
	seasonLength int       // number of periods in a season
	level        float64   // current smoothed level
	trend        float64   // current trend
	seasonal     []float64 // seasonal components
	initialized  bool
}

// NewHoltWinters creates a new Holt-Winters forecaster
func NewHoltWinters(alpha, beta, gamma float64, seasonLength int) *HoltWinters {
	return &HoltWinters{
		alpha:        alpha,
		beta:         beta,
		gamma:        gamma,
		seasonLength: seasonLength,
		seasonal:     make([]float64, seasonLength),
	}
}

// Initialize sets initial values from historical data
func (hw *HoltWinters) Initialize(data []float64) {
	n := len(data)
	if n < hw.seasonLength*2 {
		// Not enough data for full initialization, use simple average
		sum := 0.0
		for _, v := range data {
			sum += v
		}
		hw.level = sum / float64(n)
		hw.trend = 0
		for i := range hw.seasonal {
			hw.seasonal[i] = 1.0
		}
		hw.initialized = true
		return
	}

	// Initialize level as average of first season
	sum := 0.0
	for i := 0; i < hw.seasonLength; i++ {
		sum += data[i]
	}
	hw.level = sum / float64(hw.seasonLength)

	// Initialize trend as average difference between first two seasons
	trendSum := 0.0
	for i := 0; i < hw.seasonLength; i++ {
		trendSum += (data[hw.seasonLength+i] - data[i])
	}
	hw.trend = trendSum / float64(hw.seasonLength*hw.seasonLength)

	// Initialize seasonal factors (multiplicative)
	for i := 0; i < hw.seasonLength; i++ {
		if hw.level > 0 {
			hw.seasonal[i] = data[i] / hw.level
		} else {
			hw.seasonal[i] = 1.0
		}
	}

	hw.initialized = true
}

// Update processes a new observation and updates the model state
func (hw *HoltWinters) Update(observation float64, seasonIdx int) {
	if !hw.initialized {
		hw.level = observation
		hw.initialized = true
		return
	}

	idx := seasonIdx % hw.seasonLength
	prevLevel := hw.level
	seasonal := hw.seasonal[idx]
	if seasonal == 0 {
		seasonal = 1.0
	}

	// Update level
	hw.level = hw.alpha*(observation/seasonal) + (1-hw.alpha)*(prevLevel+hw.trend)

	// Update trend
	hw.trend = hw.beta*(hw.level-prevLevel) + (1-hw.beta)*hw.trend

	// Update seasonal component
	if hw.level > 0 {
		hw.seasonal[idx] = hw.gamma*(observation/hw.level) + (1-hw.gamma)*seasonal
	}
}

// Forecast predicts future values
func (hw *HoltWinters) Forecast(stepsAhead int, currentSeasonIdx int) []float64 {
	forecasts := make([]float64, stepsAhead)
	for h := 1; h <= stepsAhead; h++ {
		seasonIdx := (currentSeasonIdx + h) % hw.seasonLength
		forecasts[h-1] = (hw.level + float64(h)*hw.trend) * hw.seasonal[seasonIdx]
		if forecasts[h-1] < 0 {
			forecasts[h-1] = 0
		}
	}
	return forecasts
}

// ============================================================================
// Predictive Scaling Manager
// ============================================================================

// PredictiveScaler manages predictive scaling decisions
type PredictiveScaler struct {
	config      PredictiveScalingConfig
	history     []LoadDataPoint
	forecaster  *HoltWinters
	lastForecast []LoadForecast
	logger      *logrus.Logger
	mu          sync.RWMutex
}

// NewPredictiveScaler creates a new predictive scaling manager
func NewPredictiveScaler(cfg PredictiveScalingConfig) *PredictiveScaler {
	return &PredictiveScaler{
		config: cfg,
		history: make([]LoadDataPoint, 0, 1024),
		forecaster: NewHoltWinters(
			cfg.SmoothingAlpha,
			cfg.TrendBeta,
			cfg.SeasonalGamma,
			cfg.SeasonalPeriodHours,
		),
		logger: logrus.StandardLogger(),
	}
}

// RecordDataPoint records a new utilization data point
func (ps *PredictiveScaler) RecordDataPoint(dp LoadDataPoint) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ps.history = append(ps.history, dp)

	// Trim history to window
	maxPoints := ps.config.HistoryWindowHours * 60 // assuming per-minute granularity
	if len(ps.history) > maxPoints {
		ps.history = ps.history[len(ps.history)-maxPoints:]
	}

	// Update forecaster
	seasonIdx := dp.Timestamp.Hour()
	ps.forecaster.Update(dp.GPUUtilization, seasonIdx)
}

// GenerateForecast produces a load forecast for the prediction horizon
func (ps *PredictiveScaler) GenerateForecast() ([]LoadForecast, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if len(ps.history) < ps.config.MinDataPointsRequired {
		return nil, fmt.Errorf("insufficient data points: have %d, need %d",
			len(ps.history), ps.config.MinDataPointsRequired)
	}

	// Initialize forecaster if needed
	if !ps.forecaster.initialized {
		data := make([]float64, len(ps.history))
		for i, dp := range ps.history {
			data[i] = dp.GPUUtilization
		}
		ps.forecaster.Initialize(data)
	}

	// Generate forecasts
	stepsAhead := ps.config.PredictionHorizonMin
	now := time.Now().UTC()
	currentHour := now.Hour()

	rawForecasts := ps.forecaster.Forecast(stepsAhead, currentHour)

	// Calculate prediction confidence interval
	stdDev := ps.calculateStdDev()
	zScore := zScoreForConfidence(ps.config.ConfidenceLevel)

	forecasts := make([]LoadForecast, stepsAhead)
	for i, predicted := range rawForecasts {
		t := now.Add(time.Duration(i+1) * time.Minute)
		// Widen confidence interval further in the future
		uncertainty := stdDev * zScore * math.Sqrt(float64(i+1)/float64(stepsAhead))

		direction := "stable"
		if ps.forecaster.trend > 1.0 {
			direction = "increasing"
		} else if ps.forecaster.trend < -1.0 {
			direction = "decreasing"
		}

		forecasts[i] = LoadForecast{
			Timestamp:        t,
			PredictedGPUUtil: math.Min(100, math.Max(0, predicted)),
			ConfidenceLow:    math.Max(0, predicted-uncertainty),
			ConfidenceHigh:   math.Min(100, predicted+uncertainty),
			TrendDirection:   direction,
			SeasonalFactor:   ps.forecaster.seasonal[t.Hour()%ps.config.SeasonalPeriodHours],
		}
	}

	ps.lastForecast = forecasts
	return forecasts, nil
}

// GetScalingRecommendation generates a proactive scaling recommendation
func (ps *PredictiveScaler) GetScalingRecommendation(currentGPUs int, maxGPUs int) (*ScalingRecommendation, error) {
	forecasts, err := ps.GenerateForecast()
	if err != nil {
		return nil, err
	}

	// Find peak predicted utilization within horizon
	peakUtil := 0.0
	peakConfHigh := 0.0
	for _, f := range forecasts {
		if f.PredictedGPUUtil > peakUtil {
			peakUtil = f.PredictedGPUUtil
		}
		if f.ConfidenceHigh > peakConfHigh {
			peakConfHigh = f.ConfidenceHigh
		}
	}

	// Add buffer
	targetUtil := peakConfHigh * (1 + ps.config.ScaleUpBufferPercent/100.0)

	// Calculate recommended GPUs
	recommendedGPUs := currentGPUs
	action := "maintain"
	reason := fmt.Sprintf("Predicted peak GPU utilization: %.1f%% (conf: %.1f%%)", peakUtil, peakConfHigh)

	if targetUtil > 85.0 && currentGPUs < maxGPUs {
		// Need to scale up
		scaleFactor := targetUtil / 75.0 // target 75% util after scaling
		recommendedGPUs = int(math.Ceil(float64(currentGPUs) * scaleFactor))
		if recommendedGPUs > maxGPUs {
			recommendedGPUs = maxGPUs
		}
		action = "scale_up"
		reason = fmt.Sprintf("Predicted peak %.1f%% exceeds threshold, recommend %d GPUs",
			peakConfHigh, recommendedGPUs)
	} else if peakConfHigh < 30.0 && currentGPUs > 1 {
		// Can scale down
		scaleFactor := peakConfHigh / 60.0 // target 60% util after scaling
		recommendedGPUs = int(math.Max(1, math.Ceil(float64(currentGPUs)*scaleFactor)))
		action = "scale_down"
		reason = fmt.Sprintf("Predicted peak %.1f%% is low, recommend %d GPUs",
			peakConfHigh, recommendedGPUs)
	}

	return &ScalingRecommendation{
		Timestamp:         time.Now().UTC(),
		CurrentGPUs:       currentGPUs,
		RecommendedGPUs:   recommendedGPUs,
		PredictedPeakUtil: peakUtil,
		Confidence:        ps.config.ConfidenceLevel,
		Reason:            reason,
		ScaleAction:       action,
		LeadTimeMin:       ps.config.PredictionHorizonMin,
		Forecasts:         forecasts,
	}, nil
}

// calculateStdDev computes standard deviation of recent GPU utilization
func (ps *PredictiveScaler) calculateStdDev() float64 {
	if len(ps.history) < 2 {
		return 10.0 // default uncertainty
	}

	// Use last N points
	n := len(ps.history)
	if n > 100 {
		n = 100
	}
	recent := ps.history[len(ps.history)-n:]

	sum := 0.0
	for _, dp := range recent {
		sum += dp.GPUUtilization
	}
	mean := sum / float64(len(recent))

	variance := 0.0
	for _, dp := range recent {
		diff := dp.GPUUtilization - mean
		variance += diff * diff
	}
	variance /= float64(len(recent))

	return math.Sqrt(variance)
}

// zScoreForConfidence returns the z-score for a given confidence level
func zScoreForConfidence(confidence float64) float64 {
	switch {
	case confidence >= 0.99:
		return 2.576
	case confidence >= 0.95:
		return 1.960
	case confidence >= 0.90:
		return 1.645
	case confidence >= 0.80:
		return 1.282
	default:
		return 1.0
	}
}

// GetHistory returns historical load data
func (ps *PredictiveScaler) GetHistory(limit int) []LoadDataPoint {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	if limit <= 0 || limit > len(ps.history) {
		limit = len(ps.history)
	}
	result := make([]LoadDataPoint, limit)
	copy(result, ps.history[len(ps.history)-limit:])
	return result
}

// GetLastForecast returns the most recent forecast
func (ps *PredictiveScaler) GetLastForecast() []LoadForecast {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.lastForecast
}
