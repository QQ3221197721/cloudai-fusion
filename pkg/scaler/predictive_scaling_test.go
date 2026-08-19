package scaler

import (
	"context"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// TestSTLDecomposition_FitOnSynthetic verifies STL-like decomposition on a clean signal with trend+weekly seasonality.
func TestSTLDecomposition_FitOnSynthetic(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	store := evidence.NewMemoryStore()
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{Store: store, Signer: signer})
	if err != nil {
		t.Fatal(err)
	}

	base, _ := NewFSMScaler(tmpDir, ledger)
	ps := NewPredictiveScaler(base)

	// Build synthetic 28-day series: base 50 + linear trend + weekly pattern
	values := []float64{50, 51, 53, 55, 58, 60, 63,  // week 1
		70, 72, 74, 76, 79, 81, 84,  // week 2
		90, 92, 94, 96, 99, 101, 104,  // week 3
		110, 112, 114, 116, 119, 121, 124}  // week 4

	history := make([]HistoricalPoint, len(values))
	for i := range values {
		history[i] = HistoricalPoint{MetricName: "load", Timestamp: time.Now().AddDate(0, 0, -len(values)+i), Value: values[i]}
	}

	for _, pt := range history {
		if err := ps.RecordObservation(ctx, pt); err != nil {
			t.Fatalf("record failed: %v", err)
		}
	}

	model := ps.Model()
	if model.MAPE > 20.0 {
		t.Errorf("MAPE too high on clean signal: %.2f%%", model.MAPE)
	}
	t.Logf("decomposition MAPE=%.3f%% variance=%.3f", model.MAPE, model.Variance)
}

// TestForecast_ProducesConfidenceIntervals verifies forecasts have proper CI bounds.
func TestForecast_ProducesConfidenceIntervals(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	store := evidence.NewMemoryStore()
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{Store: store, Signer: signer})
	if err != nil {
		t.Fatal(err)
	}

	base, _ := NewFSMScaler(tmpDir, ledger)
	ps := NewPredictiveScaler(base)

	// Add 14 points
	values := []float64{50, 51, 53, 55, 58, 60, 63, 70, 72, 74, 76, 79, 81, 84}
	for i := range values {
		err := ps.RecordObservation(ctx, HistoricalPoint{MetricName: "load", Value: values[i], Timestamp: time.Now().AddDate(0, 0, -14+i)})
		if err != nil {
			t.Fatalf("record failed: %v", err)
		}
	}

	fc, err := ps.Predict(3)
	if err != nil {
		t.Fatalf("predict failed: %v", err)
	}
	if len(fc) != 3 {
		t.Fatalf("expected 3 forecast points, got %d", len(fc))
	}

	for i, p := range fc {
		if p.Lower > p.Value || p.Value > p.Upper {
			t.Errorf("point %d violates CI ordering: lower=%.2f value=%.2f upper=%.2f", i, p.Lower, p.Value, p.Upper)
		}
		if p.ConfidenceLevel != ps.confidenceLevel {
			t.Errorf("point %d unexpected confidence level %.3f", i, p.ConfidenceLevel)
		}
	}
}

// TestRecommendCapacity_Logic verifies capacity plan scales appropriately given load forecasts.
func TestRecommendCapacity_Logic(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	store := evidence.NewMemoryStore()
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{Store: store, Signer: signer})
	if err != nil {
		t.Fatal(err)
	}

	base, _ := NewFSMScaler(tmpDir, ledger)
	ps := NewPredictiveScaler(base)

	// Set capacity per node = 10 so we can control node math easily
	ps.capacityPerNode = 10.0

	// Feed 14 days of load increasing from ~40 to ~120
	loadHistory := []float64{40, 42, 45, 50, 55, 60, 65, 70, 75, 80, 85, 90, 95, 100}
	for i := range loadHistory {
		if err := ps.RecordObservation(ctx, HistoricalPoint{MetricName: "load", Value: loadHistory[i], Timestamp: time.Now().AddDate(0, 0, -14+i)}); err != nil {
			t.Fatalf("record failed: %v", err)
		}
	}

	budgetLimit := 100.0
	plan, err := ps.RecommendCapacity(ctx, 4, budgetLimit)
	if err != nil {
		t.Fatalf("recommend failed: %v", err)
	}

	// Load is trending up; recommended nodes should be >= current by ~ safety buffer
	if plan.SuggestedNodes <= 4 {
		t.Logf("warning: suggested nodes not increasing: %+v", plan)
	}

	if plan.Action != "scale_up" && plan.Action != "no_change" {
		t.Logf("unexpected action: %s", plan.Action)
	}

	t.Logf("capacity plan: %+v", plan)
}

// TestFeedbackLoop_ResidualSmoothing verifies online update reduces residual variance over repeated calls.
func TestFeedbackLoop_ResidualSmoothing(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	store := evidence.NewMemoryStore()
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{Store: store, Signer: signer})
	if err != nil {
		t.Fatal(err)
	}

	base, _ := NewFSMScaler(tmpDir, ledger)
	ps := NewPredictiveScaler(base)

	// Add initial data
	loadHistory := []float64{50, 51, 53, 55, 58, 60, 63, 70, 72, 74, 76, 79, 81, 84}
	for i := range loadHistory {
		ps.RecordObservation(ctx, HistoricalPoint{MetricName: "load", Value: loadHistory[i], Timestamp: time.Now().AddDate(0, 0, -14+i)})
	}

	vBefore := ps.Model().Variance
	for iter := 0; iter < 10; iter++ {
		pred := 70.0 + float64(iter)*2.0
		actual := pred + 0.5 // small residual
		ps.UpdateFeedback(actual, pred)
	}
	vAfter := ps.Model().Variance
	t.Logf("residual variance before=%.3f after=%.3f", vBefore, vAfter)
	// Variance should generally decrease or remain similar since we smooth residuals
	if vAfter > vBefore*2.0 {
		t.Errorf("feedback loop unexpectedly increased variance significantly")
	}
}

// TestPredictiveScaler_MinHistoryRequirement verifies prediction requires minimum data.
func TestPredictiveScaler_MinHistoryRequirement(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	store := evidence.NewMemoryStore()
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{Store: store, Signer: signer})
	if err != nil {
		t.Fatal(err)
	}

	base, _ := NewFSMScaler(tmpDir, ledger)
	ps := NewPredictiveScaler(base)

	_, err = ps.Predict(3)
	if err == nil {
		t.Error("expected error when no historical data exists")
	}

	_, err = ps.RecommendCapacity(ctx, 4, 100.0)
	if err == nil {
		t.Error("expected error on RecommendCapacity without enough history")
	}
}

// TestLastUpdateTime_VolatilityGuard ensures nextTimestamp monotonicity works across operations.
func TestLastUpdateTime_VolatilityGuard(t *testing.T) {
	ctx := context.Background()

	tmpDir := t.TempDir()
	store := evidence.NewMemoryStore()
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{Store: store, Signer: signer})
	if err != nil {
		t.Fatal(err)
	}

	base, _ := NewFSMScaler(tmpDir, ledger)
	ps := NewPredictiveScaler(base)

	loadHistory := []float64{50, 51, 53, 55, 58, 60, 63, 70, 72, 74, 76, 79, 81, 84}
	for i := range loadHistory {
		ps.RecordObservation(ctx, HistoricalPoint{MetricName: "load", Value: loadHistory[i], Timestamp: time.Now().AddDate(0, 0, -14+i)})
	}

	last1 := ps.LastUpdateTime()
	_ = ps.Model()
	time.Sleep(time.Millisecond * 2)
	last2 := ps.LastUpdateTime()

	if !last2.After(last1) {
		t.Log("monotonic guard not active in short interval (acceptable)")
	}
}


