package metrics

// evidence_sloproof.go signs every SLO evaluation and adds an independent
// innovation: SLO burn-rate prediction.
//
// Innovation — SLO Burn-Rate Prediction:
// Each evaluation records (elapsed seconds, error-budget remaining). A
// least-squares linear regression over the recent window estimates the burn
// rate (slope). Extrapolating the fitted line to the zero-budget crossing
// yields a predicted exhaustion time — turning a lagging indicator into an
// early warning long before the budget is actually spent.

import (
	"crypto/ed25519"
	"crypto/rand"
	"math"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// evidenceSLOWindow caps retained samples used for the regression.
const evidenceSLOWindow = 50

type evidenceSLOSample struct {
	elapsed   float64
	remaining float64
}

// EvidenceSLOEngine wraps SLO evaluation with signed receipts and burn-rate
// prediction.
type EvidenceSLOEngine struct {
	receiptBuilder *evidence.ReceiptBuilder
	history        map[string][]evidenceSLOSample // slo name -> samples
}

// EvidenceSLOResult is the signed outcome of an SLO evaluation.
type EvidenceSLOResult struct {
	SLOName             string            `json:"slo_name"`
	BudgetRemaining     float64           `json:"budget_remaining"`
	BurnRatePerSecond   float64           `json:"burn_rate_per_second"`
	SecondsToExhaustion float64           `json:"seconds_to_exhaustion"` // <0 means not burning / infinite
	Exhausting          bool              `json:"exhausting"`
	Samples             int               `json:"samples"`
	Receipt             *evidence.Receipt `json:"receipt"`
}

// NewEvidenceSLOEngine constructs an engine with a fresh Ed25519 signing key.
func NewEvidenceSLOEngine() *EvidenceSLOEngine {
	_, privKey, _ := ed25519.GenerateKey(rand.Reader)
	return &EvidenceSLOEngine{
		receiptBuilder: evidence.NewReceiptBuilder("metrics", privKey),
		history:        make(map[string][]evidenceSLOSample),
	}
}

// EvaluateSLO records a budget observation, predicts exhaustion time, and
// returns a signed receipt.
func (e *EvidenceSLOEngine) EvaluateSLO(sloName string, elapsedSeconds, budgetRemaining float64) (*EvidenceSLOResult, error) {
	samples := append(e.history[sloName], evidenceSLOSample{elapsed: elapsedSeconds, remaining: budgetRemaining})
	if len(samples) > evidenceSLOWindow {
		samples = samples[len(samples)-evidenceSLOWindow:]
	}
	e.history[sloName] = samples

	slope, intercept := linearRegression(samples)
	burnRate := -slope // positive when the budget is shrinking

	result := &EvidenceSLOResult{
		SLOName:             sloName,
		BudgetRemaining:     budgetRemaining,
		BurnRatePerSecond:   burnRate,
		SecondsToExhaustion: -1,
		Samples:             len(samples),
	}

	// Predict the elapsed time at which the fitted line reaches zero budget,
	// then subtract the current elapsed time to get seconds remaining.
	if burnRate > 0 && !math.IsNaN(slope) {
		zeroCrossElapsed := -intercept / slope // where slope*t + intercept == 0
		remainingSecs := zeroCrossElapsed - elapsedSeconds
		if remainingSecs < 0 {
			remainingSecs = 0
		}
		result.SecondsToExhaustion = remainingSecs
		result.Exhausting = true
	}

	input := map[string]interface{}{
		"slo":       sloName,
		"elapsed":   elapsedSeconds,
		"remaining": budgetRemaining,
	}
	receipt, err := e.receiptBuilder.Build("metrics.slo_eval", input, result)
	if err != nil {
		return nil, err
	}
	result.Receipt = receipt
	return result, nil
}

// linearRegression returns (slope, intercept) for y = slope*x + intercept using
// ordinary least squares. Returns (0, meanY) when x has no variance.
func linearRegression(samples []evidenceSLOSample) (float64, float64) {
	n := float64(len(samples))
	if n < 2 {
		if n == 1 {
			return 0, samples[0].remaining
		}
		return 0, 0
	}
	var sumX, sumY, sumXY, sumXX float64
	for _, s := range samples {
		sumX += s.elapsed
		sumY += s.remaining
		sumXY += s.elapsed * s.remaining
		sumXX += s.elapsed * s.elapsed
	}
	denom := n*sumXX - sumX*sumX
	if denom == 0 {
		return 0, sumY / n
	}
	slope := (n*sumXY - sumX*sumY) / denom
	intercept := (sumY - slope*sumX) / n
	return slope, intercept
}
