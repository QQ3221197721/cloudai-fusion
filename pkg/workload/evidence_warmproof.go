package workload

// evidence_warmproof.go signs scheduling decisions and adds an independent
// innovation: demand forecasting for warm pods using exponential smoothing.
//
// Innovation — Warm Pool Demand Forecasting:
// Historical pod counts are smoothed using simple exponential smoothing
// (Holt-Winters style). Alpha controls how fast recent observations change
// the forecast. Predictions feed into a "warm pool" pre-scheduler that
// launches spare capacity in anticipation of predicted spikes.

import (
	"crypto/ed25519"
	"crypto/rand"
	"math"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

const evidenceAlpha = 0.3 // smoothing constant for simple exponential smoothing

type EvidenceWarmSample struct {
	timestamp int64
	count     int
}

// EvidenceWarmResult is the signed outcome of a scheduling decision.
type EvidenceWarmResult struct {
	Decision       string `json:"decision"`        // "schedule"/"wait"/"preheat"
	WarmPoolSize   int    `json:"warm_pool_size"`
	PredictedPeak  int    `json:"predicted_peak"`
	DetectedTrend  bool   `json:"detected_trend"` // true if demand rising
	Receipt        *evidence.Receipt `json:"receipt"`
}

// EvidenceWarmEngine wraps workload scheduling with receipts and warm pool
// demand forecasting via exponential smoothing.
type EvidenceWarmEngine struct {
	receiptBuilder *evidence.ReceiptBuilder
	history        []EvidenceWarmSample
	forecast       float64
	lastObserved   float64
	windowSamples  int
}

// NewEvidenceWarmEngine constructs an engine with fresh Ed25519 key.
func NewEvidenceWarmEngine() *EvidenceWarmEngine {
	_, privKey, _ := ed25519.GenerateKey(rand.Reader)
	return &EvidenceWarmEngine{
		receiptBuilder: evidence.NewReceiptBuilder("workload", privKey),
		history:        make([]EvidenceWarmSample, 0, 100),
		forecast:       5,
		lastObserved:   5,
		windowSamples:  20,
	}
}

// RecordObservation logs a pod count observation at time T and returns a
// scheduled/warmed result with a receipt.
func (e *EvidenceWarmEngine) RecordObservation(timestamp int64, observedCount int) (*EvidenceWarmResult, error) {
	e.history = append(e.history, EvidenceWarmSample{timestamp: timestamp, count: observedCount})
	if len(e.history) > e.windowSamples {
		e.history = e.history[len(e.history)-e.windowSamples:]
	}

	// Simple exponential smoothing: F_t = α×X_{t-1} + (1-α)×F_{t-1}
	newForecast := evidenceAlpha*float64(observedCount) + (1-evidenceAlpha)*e.forecast
	if math.IsNaN(newForecast) || math.IsInf(newForecast, 0) {
		newForecast = e.forecast
	}
	detectedTrend := newForecast > e.lastObserved*e.forecastLastFactor()

	// Decide actions based on forecast.
	var decision string
	warmPoolSize := max(0, int(newForecast)-observedCount)
	if warmPoolSize > 0 {
		decision = "preheat"
	} else if observedCount > int(newForecast)+2 {
		decision = "schedule"
	} else {
		decision = "wait"
	}

	input := map[string]interface{}{
		"timestamp":    timestamp,
		"observed":     observedCount,
		"forecast_old": e.forecast,
	}
	output := map[string]interface{}{
		"decision":      decision,
		"warm_pool_size": warmPoolSize,
		"predicted_peak": int(newForecast),
		"trend":         detectedTrend,
	}
	receipt, err := e.receiptBuilder.Build("workload.decision", input, output)
	if err != nil {
		return nil, err
	}

	e.forecast = newForecast
	e.lastObserved = float64(observedCount)
	return &EvidenceWarmResult{
		Decision:       decision,
		WarmPoolSize:   warmPoolSize,
		PredictedPeak:  int(newForecast),
		DetectedTrend:  detectedTrend,
		Receipt:        receipt,
	}, nil
}

func (e *EvidenceWarmEngine) forecastLastFactor() float64 {
	if e.lastObserved == 0 {
		return 0.01
	}
	return e.lastObserved
}
