// Package scaler - FLIP M16 Production Optimization: O(1) Incremental Holt-Winters Forecasting
//
// This file adds a production-ready Holt-Winters exponential smoothing forecaster that
// achieves O(1) update AND O(1) forecast complexity — a decisive advantage over the
// O(n·window) STL refit in predictive_scaling.go and over Kubernetes' reactive HPA loop.
//
// Triple exponential smoothing (additive):
//   level_t    = α·y_t + (1-α)·(level_{t-1} + trend_{t-1})
//   trend_t    = β·(level_t - level_{t-1}) + (1-β)·trend_{t-1}
//   seasonal_t = γ·(y_t - level_t) + (1-γ)·seasonal_{t-period}
//   forecast_{t+h} = level_t + h·trend_t + seasonal_{(t+h) mod period}
//
// Every Update touches a constant number of scalars plus one seasonal-buffer slot, so the
// hot path is allocation-free and cache-friendly. Residual variance is maintained via an
// exponentially weighted running estimate (also O(1)) for confidence-interval sizing.
package scaler

import (
	"math"
	"time"
)

// hwParams holds the smoothing coefficients and capacity-planning bounds.
type hwParams struct {
	alpha        float64 // level smoothing
	beta         float64 // trend smoothing
	gamma        float64 // seasonal smoothing
	period       int     // seasonality period (e.g. 7 for weekly)
	safetyMargin float64 // multiplier applied to the uncertainty band as buffer
	maxNodes     int     // hard cap
	minNodes     int     // hard floor
	capPerNode   float64 // normalized load served by one node
}

// HoltWintersState is an O(1) incremental Holt-Winters forecaster.
type HoltWintersState struct {
	level     float64
	trend     float64
	seasonals []float64 // length == params.period
	count     int       // number of observations processed
	lastTime  time.Time
	lastValue float64

	params hwParams

	// Running residual variance via exponentially weighted estimate (O(1)).
	variance    float64
	warmSumSq   float64 // sum of squared residuals during warmup
	warmCount   int     // residual samples seen during warmup
	warmupLimit int     // switch to EW variance after this many samples
}

// NewHoltWintersState creates an optimized forecaster with sensible cloud-workload defaults.
func NewHoltWintersState() *HoltWintersState {
	p := hwParams{
		alpha: 0.2, beta: 0.1, gamma: 0.1, period: 7,
		safetyMargin: 1.5, maxNodes: 20, minNodes: 1, capPerNode: 10.0,
	}
	return &HoltWintersState{
		seasonals:   make([]float64, p.period),
		params:      p,
		warmupLimit: 30,
	}
}

// Update folds one new observation into the model in O(1) and reports whether
// enough history exists to produce a trustworthy forecast.
func (hw *HoltWintersState) Update(value float64, timestamp time.Time) bool {
	if !hw.lastTime.IsZero() && timestamp.Before(hw.lastTime) {
		// Out-of-order sample: ignore to preserve temporal coherence.
		return hw.count >= hw.params.period
	}

	period := hw.params.period
	idx := hw.count % period

	if hw.count == 0 {
		// Seed from the first observation.
		hw.level = value
		hw.trend = 0
		for i := range hw.seasonals {
			hw.seasonals[i] = 0
		}
		hw.lastValue = value
		hw.lastTime = timestamp
		hw.count = 1
		return false
	}

	// One-step-ahead prediction BEFORE update, for the residual.
	predicted := hw.level + hw.trend + hw.seasonals[idx]

	oldLevel := hw.level
	newLevel := hw.params.alpha*value + (1-hw.params.alpha)*(hw.level+hw.trend)
	newTrend := hw.params.beta*(newLevel-oldLevel) + (1-hw.params.beta)*hw.trend
	newSeasonal := hw.params.gamma*(value-newLevel) + (1-hw.params.gamma)*hw.seasonals[idx]

	hw.level = newLevel
	hw.trend = newTrend
	hw.seasonals[idx] = newSeasonal
	hw.lastValue = value
	hw.lastTime = timestamp
	hw.count++

	hw.updateVariance(value - predicted)

	return hw.count >= period
}

// predictInternal computes an h-step-ahead point forecast without mutating state.
func (hw *HoltWintersState) predictInternal(h int) float64 {
	period := len(hw.seasonals)
	if period == 0 {
		return hw.level + float64(h)*hw.trend
	}
	idx := (hw.count + h - 1) % period
	if idx < 0 {
		idx += period
	}
	return hw.level + float64(h)*hw.trend + hw.seasonals[idx]
}

// Predict returns an h-step-ahead forecast with a 95% confidence band. O(1).
func (hw *HoltWintersState) Predict(stepsAhead int) (ForecastPoint, error) {
	if hw.count == 0 {
		return ForecastPoint{}, ErrInsufficientHistory
	}
	value := hw.predictInternal(stepsAhead)
	stdErr := math.Sqrt(hw.variance)
	z := getZScore(0.95)
	band := z * stdErr * hw.params.safetyMargin
	lower := value - band
	upper := value + band
	if lower < 0 {
		lower = 0
	}
	return ForecastPoint{
		Value:           value,
		Lower:           lower,
		Upper:           upper,
		ConfidenceLevel: 0.95,
	}, nil
}

// RecommendedNodes converts the next-step forecast into a concrete node count. O(1).
func (hw *HoltWintersState) RecommendedNodes() (int, error) {
	fc, err := hw.Predict(1)
	if err != nil {
		return 0, err
	}
	required := int(math.Ceil(fc.Value / hw.params.capPerNode))
	safetyBuffer := int(math.Ceil((fc.Upper - fc.Value) / hw.params.capPerNode))
	if safetyBuffer < 0 {
		safetyBuffer = 0
	}
	suggested := required + safetyBuffer
	if suggested > hw.params.maxNodes {
		suggested = hw.params.maxNodes
	}
	if suggested < hw.params.minNodes {
		suggested = hw.params.minNodes
	}
	return suggested, nil
}

// AdaptOnline nudges α based on recent signed prediction error — the online feedback loop.
func (hw *HoltWintersState) AdaptOnline(actual, predicted float64) {
	delta := actual - predicted
	const sensitivity = 0.05
	switch {
	case delta > 10 && hw.params.alpha < 0.5:
		hw.params.alpha += sensitivity
	case delta < -10 && hw.params.alpha > 0.1:
		hw.params.alpha -= sensitivity
	}
	if hw.params.alpha < 0.1 {
		hw.params.alpha = 0.1
	}
	if hw.params.alpha > 0.5 {
		hw.params.alpha = 0.5
	}
}

// Variance exposes the current residual variance estimate (for accuracy assessment).
func (hw *HoltWintersState) Variance() float64 { return hw.variance }

// updateVariance maintains an O(1) residual-variance estimate: exact mean-square during
// warmup, then an exponentially weighted estimate to track regime changes.
func (hw *HoltWintersState) updateVariance(residual float64) {
	if hw.warmCount < hw.warmupLimit {
		hw.warmSumSq += residual * residual
		hw.warmCount++
		hw.variance = hw.warmSumSq / float64(hw.warmCount)
		return
	}
	const decay = 0.95
	hw.variance = hw.variance*decay + residual*residual*(1-decay)
}
