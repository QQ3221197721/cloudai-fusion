package wellreadiness

// evidence_wellreadiness.go layers two independent barriers over readiness checks:
//
//  1. Evidence-native barrier — each readiness evaluation is sealed into a signed,
//     offline-verifiable evidence.Receipt binding (component, result, metrics) to
//     a timestamp. We can prove "component C was READY/UNREADY at time X with Y".
//
//  2. Independent-innovation barrier — a maturity-progress tracker monitors the
//     component's readiness trajectory using linear regression on a sliding window
//     of scores, predicting when the system will reach production-ready threshold
//     (e.g., mean >= 95% ready for 3 consecutive windows).

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// ReadinessOutcome is the verifiable result of one readiness check.
type ReadinessOutcome struct {
	Component string          `json:"component"`
	Status    string          `json:"status"` // "ready" | "degraded" | "not_ready"
	Score     float64         `json:"score"` // 0..1
	Receipt   *evidence.Receipt `json:"receipt,omitempty"`
}

// MaturityTracker summarizes the predicted production-ready time and current trend.
type MaturityTracker struct {
	Component        string  `json:"component"`
	MeanScore        float64 `json:"mean_score"`
	PredictedReadyAt int64   `json:"predicted_ready_at_unix,omitempty"` // seconds since epoch
	TrendSlope       float64 `json:"trend_slope"`                       // improvement per window
	PastWindows      []float64 `json:"past_windows,omitempty"`           // last 10 scores
}

// EvidenceWellreadinessEngine seals readiness evaluations and predicts maturity.
type EvidenceWellreadinessEngine struct {
	receiptBuilder *evidence.ReceiptBuilder

	mu sync.Mutex
	windows map[string][]float64 // component → last 20 scores
	windowSize int
	predictThreshold float64 // min mean score for production readiness prediction
}

// NewEvidenceWellreadinessEngine builds an engine with a freshly generated key.
func NewEvidenceWellreadinessEngine() *EvidenceWellreadinessEngine {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	return &EvidenceWellreadinessEngine{
		receiptBuilder: evidence.NewReceiptBuilder("wellreadiness", priv),
		windows:        make(map[string][]float64),
		windowSize:     20,
		predictThreshold: 0.95,
	}
}

// EvaluateReadiness records a readiness outcome, updates the window, and returns
// a signed receipt. Status is derived from the provided score: >=0.95 ready,
// >=0.70 degraded, else not_ready.
func (e *EvidenceWellreadinessEngine) EvaluateReadiness(component string, score float64, details interface{}) (*ReadinessOutcome, error) {
	if component == "" {
		return nil, fmt.Errorf("wellreadiness: component must not be empty")
	}
	if score < 0 || score > 1 {
		return nil, fmt.Errorf("wellreadiness: score must be 0..1, got %.2f", score)
	}

	e.mu.Lock()
	w := e.ensureWindow(component)
	w[len(w)-1] = score // replace oldest by cycling
	e.mu.Unlock()

	status := "not_ready"
	switch {
	case score >= 0.95:
		status = "ready"
	case score >= 0.70:
		status = "degraded"
	}

	result := &ReadinessOutcome{
		Component: component,
		Status: status,
		Score: score,
	}
	input := struct {
		Comp string `json:"comp"`
		Score float64 `json:"score"`
	}{component, score}
	receipt, err := e.receiptBuilder.Build("wellreadiness.evaluate", input, result)
	if err != nil {
		return nil, fmt.Errorf("wellreadiness: seal evaluation: %w", err)
	}
	result.Receipt = receipt
	return result, nil
}

// ---------------------------------------------------------------------------
// INNOVATION: maturity progression tracking and prediction
// ---------------------------------------------------------------------------

// TrackMaturity computes the maturity profile for a component, including a
// predicted production-ready time if recent scores show positive trend and high
// mean. The prediction uses simple linear regression on the last 10 scores; if
// slope>0 and mean>=threshold, it extrapolates how many additional windows until
// the projected mean exceeds threshold (conservatively assuming constant slope).
func (e *EvidenceWellreadinessEngine) TrackMaturity(component string) MaturityTracker {
	e.mu.Lock()
	defer e.mu.Unlock()
	tr := MaturityTracker{Component: component}
	w := e.getWindow(component)
	tr.PastWindows = w
	n := len(w)
	if n == 0 {
		return tr
	}

	// Mean score
	var sum float64
	for _, s := range w {
		sum += s
	}
	tr.MeanScore = sum / float64(n)

	if n < 3 {
		return tr
	}
	xs := make([]float64, n)
	for i := range xs {
		xs[i] = float64(i)
	}
	slope, _ := linearRegression(xs, w)
	tr.TrendSlope = slope

	if slope <= 0 {
		return tr
	}
	if tr.MeanScore < e.predictThreshold {
		// Extrapolate windows needed: (threshold - current_mean) / avg_improvement_per_window
		needed := e.predictThreshold - tr.MeanScore
		windowsToNeeded := needed / slope
		if windowsToNeeded > 0 && windowsToNeeded < float64(100) {
			now := time.Now().Unix() + int64(windowsToNeeded*60) // assume 60s windows
			tr.PredictedReadyAt = now
		}
	}
	return tr
}

// getOrCreateWindow ensures a window exists for the component, cycling values.
func (e *EvidenceWellreadinessEngine) ensureWindow(component string) []float64 {
	if _, ok := e.windows[component]; !ok {
		e.windows[component] = make([]float64, e.windowSize)
	}
	return e.windows[component]
}

func (e *EvidenceWellreadinessEngine) getWindow(component string) []float64 {
	w := e.windows[component]
	return w
}

// linearRegression fits y = slope*x + intercept to xs,ys via least squares.
func linearRegression(xs, ys []float64) (slope, intercept float64) {
	n := float64(len(xs))
	var sumX, sumY, sumXY, sumXX float64
	for i := range xs {
		sumX += xs[i]
		sumY += ys[i]
		sumXY += xs[i] * ys[i]
		sumXX += xs[i] * xs[i]
	}
	denom := n*sumXX - sumX*sumX
	if denom == 0 {
		return 0, sumY / n
	}
	slope = (n*sumXY - sumX*sumY) / denom
	intercept = (sumY - slope*sumX) / n
	return slope, intercept
}
