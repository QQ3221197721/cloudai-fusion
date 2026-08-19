package scaler

import (
	"context"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// newBenchPredictive builds a predictive scaler pre-seeded with `days` observations.
func newBenchPredictive(b *testing.B, days int) (*PredictiveScaler, context.Context) {
	b.Helper()
	tmpDir := b.TempDir()
	store := evidence.NewMemoryStore()
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		b.Fatalf("signer: %v", err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{Store: store, Signer: signer})
	if err != nil {
		b.Fatalf("ledger: %v", err)
	}
	base, err := NewFSMScaler(tmpDir, ledger)
	if err != nil {
		b.Fatalf("scaler: %v", err)
	}
	ps := NewPredictiveScaler(base)
	ctx := context.Background()
	for i := 0; i < days; i++ {
		val := 50.0 + float64(i)*1.5 + float64(i%7)*4.0
		if err := ps.RecordObservation(ctx, HistoricalPoint{
			MetricName: "load", Value: val, Timestamp: time.Now().AddDate(0, 0, -days+i),
		}); err != nil {
			b.Fatalf("record: %v", err)
		}
	}
	return ps, ctx
}

// BenchmarkPredictiveFit measures STL-like decomposition (fit) latency on 28 observations.
func BenchmarkPredictiveFit(b *testing.B) {
	ps, ctx := newBenchPredictive(b, 27) // 27 so the 28th triggers refit each loop
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := ps.RecordObservation(ctx, HistoricalPoint{
			MetricName: "load", Value: 100.0, Timestamp: time.Now(),
		}); err != nil {
			b.Fatalf("record: %v", err)
		}
	}
}

// BenchmarkPredictiveForecast measures forecast generation latency (3-step) with CI bounds.
func BenchmarkPredictiveForecast(b *testing.B) {
	ps, _ := newBenchPredictive(b, 28)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fc, err := ps.Predict(3)
		if err != nil {
			b.Fatalf("predict: %v", err)
		}
		if len(fc) != 3 {
			b.Fatalf("expected 3 points, got %d", len(fc))
		}
	}
}

// BenchmarkRecommendCapacity measures full capacity-planning latency (forecast → node math).
func BenchmarkRecommendCapacity(b *testing.B) {
	ps, ctx := newBenchPredictive(b, 28)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ps.RecommendCapacity(ctx, 4, 100.0); err != nil {
			b.Fatalf("recommend: %v", err)
		}
	}
}

// BenchmarkFeedbackUpdate measures online residual-smoothing feedback loop cost.
func BenchmarkFeedbackUpdate(b *testing.B) {
	ps, _ := newBenchPredictive(b, 28)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ps.UpdateFeedback(72.0, 70.0)
	}
}
