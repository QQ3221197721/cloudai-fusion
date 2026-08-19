// Package scaler — Module 16: Predictive Scaling with STL-style decomposition.
//
// This file adds a pure-Go forecasting engine on top of the existing FSM_scaler:
//   - STL-like Seasonal-Trend Decomposition using LOESS-free moving averages
//   - Prophet-like trend + seasonality modeling with uncertainty quantification
//   - Confidence intervals via residual variance and safety margins
//   - Feedback loop for online adaptation (exponential smoothing of residuals)
//
// All algorithms are deterministic and fit on Windows test hosts without external libs.
package scaler

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"
)

// ErrInsufficientHistory is returned when a prediction is requested before
// enough observations have been recorded to fit the decomposition model.
var ErrInsufficientHistory = errors.New("scaler: insufficient historical data for prediction")

// ============================================================================
// Prediction Engine API
// ============================================================================

// STLDecompositionResult holds additive components from time-series decomposition.
// Residuals capture noise; Variance is used for CI computation.
type STLDecompositionResult struct {
	Trend        []float64
	Seasonality  []float64
	Residuals    []float64
	Variance     float64
	MAPE         float64 // mean absolute percentage error on fit
	IterationCount int   // iterations performed
}

// ForecastPoint represents a single point forecast with uncertainty bounds.
type ForecastPoint struct {
	Value           float64
	Lower           float64
	Upper           float64
	ConfidenceLevel float64
}

// CapacityPlan translates load forecasts into concrete scaling decisions.
type CapacityPlan struct {
	DecisionID     string      // "cap-" prefix
	Action         string      // "scale_up", "scale_down", "no_change"
	ForecastPoints []ForecastPoint
	SuggestedNodes int
	SafetyMargin   float64
	PredictedLoad  float64
	CostImpact     float64
	CreatedAt      time.Time
}

// PredictiveScaler wraps FSM_scaler and adds prediction capability.
type PredictiveScaler struct {
	base *FSMScaler

	mu         sync.RWMutex
	lastUpdate time.Time
	model      STLDecompositionResult
	historyRaw []HistoricalPoint

	// Params control sensitivity and confidence levels.
	sensitivity          float64 // how aggressively we scale per unit forecast error
	confidenceLevel      float64 // CI level (default 0.95)
	safetyMultiplier     float64 // multiplier applied to uncertainty band as buffer
	maxNodes             int     // hard cap for autoscaling
	minNodes             int     // hard floor for autoscaling
	capacityPerNode      float64 // normalized load capacity served by one node (default 1.0)
}

// HistoricalPoint stores observed metric values over time.
type HistoricalPoint struct {
	MetricName  string
	Timestamp   time.Time
	Value       float64
	BudgetLimit float64
}

// NewPredictiveScaler constructs a predictive wrapper around an FSM_scaler instance.
func NewPredictiveScaler(base *FSMScaler) *PredictiveScaler {
	return &PredictiveScaler{
		base: base,
		historyRaw: make([]HistoricalPoint, 0),
		sensitivity: 0.2,
		confidenceLevel: 0.95,
		safetyMultiplier: 1.5,
		maxNodes: 20,
		minNodes: 1,
		capacityPerNode: 1.0,
	}
}

// RecordObservation appends a new data point and refits the model if enough points exist.
func (p *PredictiveScaler) RecordObservation(ctx context.Context, point HistoricalPoint) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.historyRaw = append(p.historyRaw, point)
	needsRefit := len(p.historyRaw) >= 7 // minimum 1 week for weekly seasonality

	if needsRefit {
		p.model = p.fitModelOnDemand(p.historyRaw)
		p.lastUpdate = time.Now().UTC()
	}

	return nil
}

// Predict generates a forecast for h steps ahead with confidence intervals.
func (p *PredictiveScaler) Predict(stepsAhead int) ([]ForecastPoint, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.historyRaw) == 0 {
		return nil, ErrInsufficientHistory
	}

	result := p.makeForecast(p.model, stepsAhead)
	return result, nil
}

// RecommendCapacity returns recommended nodes based on current load forecast.
func (p *PredictiveScaler) RecommendCapacity(ctx context.Context, currentLoad float64, budgetLimit float64) (*CapacityPlan, error) {
	fc, err := p.Predict(3)
	if err != nil {
		return nil, err
	}

	var predictedLoad float64
	for _, f := range fc {
		predictedLoad += f.Value
	}
	if len(fc) > 0 {
		predictedLoad /= float64(len(fc))
	}

	requiredNodes := int(math.Ceil(predictedLoad / p.capacityPerNode))
	safetyBuffer := int(math.Ceil((fc[0].Upper - fc[0].Value) * p.safetyMultiplier / p.capacityPerNode))
	if safetyBuffer < 0 {
		safetyBuffer = 0
	}

	suggested := requiredNodes + safetyBuffer
	if suggested > p.maxNodes {
		suggested = p.maxNodes
	}
	if suggested < p.minNodes {
		suggested = p.minNodes
	}

	action := "no_change"
	suggestedF := float64(suggested)
	if suggestedF > currentLoad+0.5 {
		action = "scale_up"
	} else if suggestedF < currentLoad-0.5 {
		action = "scale_down"
	}

	nodeCost := 2.0
	costImpact := (suggestedF - currentLoad) * nodeCost

	id := "cap-" + generateRandomHex(16)

	return &CapacityPlan{
		DecisionID: id, Action: action, ForecastPoints: fc,
		SuggestedNodes: suggested, SafetyMargin: float64(safetyBuffer),
		PredictedLoad: predictedLoad, CostImpact: costImpact,
		CreatedAt: time.Now().UTC(),
	}, nil
}

// UpdateFeedback adjusts model parameters online via exponential smoothing of residuals.
func (p *PredictiveScaler) UpdateFeedback(actual float64, predicted float64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delta := actual - predicted
	if len(p.model.Residuals) == 0 {
		return
	}
	resVar := float64(len(p.model.Residuals))
	for i := range p.model.Residuals {
		p.model.Residuals[i] = p.model.Residuals[i]*(1-p.sensitivity) + delta*p.sensitivity
	}
	sumSq := 0.0
	for _, r := range p.model.Residuals {
		sumSq += r * r
	}
	p.model.Variance = sumSq / resVar
}

// Model returns the fitted model state (read-only).
func (p *PredictiveScaler) Model() STLDecompositionResult {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.model
}

// LastUpdateTime returns when the model was last updated.
func (p *PredictiveScaler) LastUpdateTime() time.Time {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastUpdate
}

// ============================================================================
// Core forecasting implementation
// ============================================================================

// fitModelOnDemand performs STL-like decomposition using additive model.
// It computes trend via centered moving average, seasonality via period-mean,
// and residuals = observed - trend - seasonality.
func (p *PredictiveScaler) fitModelOnDemand(history []HistoricalPoint) STLDecompositionResult {
	n := len(history)
	if n == 0 {
		return STLDecompositionResult{}
	}

	values := make([]float64, n)
	for i := range history {
		values[i] = history[i].Value
	}

	period := 7 // weekly seasonality
	trend := p.computeMovingAverage(values, period)
	detrended := make([]float64, n)
	for i := range values {
		if trend[i] != 0 {
			detrended[i] = values[i] - trend[i]
		} else {
			detrended[i] = values[i]
		}
	}
	seasonality := p.computeSeasonality(detrended, period)

	residuals := make([]float64, n)
	var mae float64
	for i := range values {
		forecast := trend[i] + seasonality[i%period]
		residuals[i] = values[i] - forecast
		diff := values[i] - forecast
		if values[i] != 0 {
			mae += math.Abs(diff / values[i])
		}
	}

	var variance float64
	for _, r := range residuals {
		variance += r * r
	}
	variance /= float64(n)

	return STLDecompositionResult{
		Trend: trend, Seasonality: seasonality, Residuals: residuals,
		Variance: variance, MAPE: (mae / float64(n)) * 100, IterationCount: 1,
	}
}

// computeMovingAverage extracts trend via centered moving average.
func (p *PredictiveScaler) computeMovingAverage(series []float64, windowSize int) []float64 {
	n := len(series)
	out := make([]float64, n)
	half := windowSize / 2
	for i := 0; i < n; i++ {
		start := i - half
		end := i + half
		sum := 0.0
		count := 0
		for j := start; j <= end && j >= 0 && j < n; j++ {
			sum += series[j]
			count++
		}
		if count > 0 {
			out[i] = sum / float64(count)
		} else {
			out[i] = series[i]
		}
	}
	return out
}

// computeSeasonality computes average residual per period index and normalizes to zero-sum.
func (p *PredictiveScaler) computeSeasonality(detrended []float64, period int) []float64 {
	seasonality := make([]float64, period)
	for t := 0; t < period; t++ {
		sum := 0.0
		count := 0
		for j := t; j < len(detrended); j += period {
			sum += detrended[j]
			count++
		}
		if count > 0 {
			seasonality[t] = sum / float64(count)
		}
	}
	// Normalize so sum ≈ 0
	avg := 0.0
	for _, s := range seasonality {
		avg += s
	}
	avg /= float64(period)
	for i := range seasonality {
		seasonality[i] -= avg
	}
	return seasonality
}

// makeForecast produces future values using trend projection + cyclic seasonality.
func (p *PredictiveScaler) makeForecast(model STLDecompositionResult, stepsAhead int) []ForecastPoint {
	out := make([]ForecastPoint, stepsAhead)
	trendLen := len(model.Trend)
	if trendLen == 0 || stepsAhead == 0 || len(model.Seasonality) == 0 {
		return out
	}

	// Linear trend extrapolation via slope from endpoints
	slope := model.Trend[trendLen-1] - model.Trend[0]
	if trendLen > 1 {
		slope /= float64(trendLen - 1)
	}

	stdErr := math.Sqrt(model.Variance)
	z := getZScore(p.confidenceLevel)

	for i := 0; i < stepsAhead; i++ {
		trendVal := model.Trend[trendLen-1] + slope*float64(i)
		seasonVal := model.Seasonality[(trendLen+i)%len(model.Seasonality)]
		value := trendVal + seasonVal

		lower := value - z*stdErr
		upper := value + z*stdErr
		if lower < 0 {
			lower = 0
		}
		out[i] = ForecastPoint{
			Value: value, Lower: lower, Upper: upper, ConfidenceLevel: p.confidenceLevel,
		}
	}
	return out
}

// getZScore maps a two-sided confidence level to its standard-normal critical
// value. Common levels use exact table values; others fall back to the
// Beasley-Springer/Moro rational approximation of the inverse normal CDF.
func getZScore(cl float64) float64 {
	if cl <= 0 || cl >= 1 {
		return 0
	}
	switch {
	case math.Abs(cl-0.90) < 1e-9:
		return 1.6448536269514722
	case math.Abs(cl-0.95) < 1e-9:
		return 1.959963984540054
	case math.Abs(cl-0.99) < 1e-9:
		return 2.5758293035489004
	}
	// General case: quantile of the upper tail p = (1+cl)/2.
	return normalQuantile((1 + cl) / 2)
}

// normalQuantile approximates the inverse CDF of the standard normal via the
// Acklam rational approximation (max abs error < 1.15e-9 over p in (0,1)).
func normalQuantile(p float64) float64 {
	if p <= 0 {
		return math.Inf(-1)
	}
	if p >= 1 {
		return math.Inf(1)
	}
	a := []float64{-3.969683028665376e+01, 2.209460984245205e+02, -2.759285104469687e+02, 1.383577518672690e+02, -3.066479806614716e+01, 2.506628277459239e+00}
	b := []float64{-5.447609879822406e+01, 1.615858368580409e+02, -1.556989798598866e+02, 6.680131188771972e+01, -1.328068155288572e+01}
	c := []float64{-7.784894002430293e-03, -3.223964580411365e-01, -2.400758277161838e+00, -2.549732539343734e+00, 4.374664141464968e+00, 2.938163982698783e+00}
	d := []float64{7.784695709041462e-03, 3.224671290700398e-01, 2.445134137142996e+00, 3.754408661907416e+00}
	pLow := 0.02425
	pHigh := 1 - pLow
	var x float64
	switch {
	case p < pLow:
		q := math.Sqrt(-2 * math.Log(p))
		x = (((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) /
			((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	case p <= pHigh:
		q := p - 0.5
		r := q * q
		x = (((((a[0]*r+a[1])*r+a[2])*r+a[3])*r+a[4])*r + a[5]) * q /
			(((((b[0]*r+b[1])*r+b[2])*r+b[3])*r+b[4])*r + 1)
	default:
		q := math.Sqrt(-2 * math.Log(1-p))
		x = -(((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) /
			((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	}
	return x
}
