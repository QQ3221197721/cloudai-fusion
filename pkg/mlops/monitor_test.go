package mlops

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// ---------------------------------------------------------------------------
// M20 Model Performance Monitor — correctness tests
// ---------------------------------------------------------------------------

// makeNormal returns n samples from a normal distribution with a fixed seed.
func makeNormal(r *rand.Rand, n int, mean, std float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = r.NormFloat64()*std + mean
	}
	return out
}

func TestPSINoDriftIsLow(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	ref := makeNormal(r, 5000, 0, 1)
	live := makeNormal(r, 5000, 0, 1) // same distribution

	m := NewMonitor()
	slo := FeatureSLO{Feature: "x", Method: MethodPSI, WarnThreshold: 0.1, BreachThreshold: 0.25, Bins: 10}
	if err := m.RegisterBaseline(slo, ref); err != nil {
		t.Fatalf("RegisterBaseline: %v", err)
	}
	res, err := m.Score("x", live)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if res.Score >= 0.1 {
		t.Fatalf("expected low PSI for same distribution, got %f", res.Score)
	}
	if res.Severity != SeverityStable {
		t.Fatalf("expected STABLE, got %s", res.Severity)
	}
}

func TestPSIDetectsShift(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	ref := makeNormal(r, 5000, 0, 1)
	live := makeNormal(r, 5000, 2.0, 1) // shifted mean

	m := NewMonitor()
	slo := FeatureSLO{Feature: "x", Method: MethodPSI, WarnThreshold: 0.1, BreachThreshold: 0.25, Bins: 10}
	_ = m.RegisterBaseline(slo, ref)
	res, err := m.Score("x", live)
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	if res.Score < 0.25 {
		t.Fatalf("expected significant PSI for shifted distribution, got %f", res.Score)
	}
	if res.Severity != SeverityBreach {
		t.Fatalf("expected BREACH, got %s (score=%f)", res.Severity, res.Score)
	}
}

func TestKSDetectsShift(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	ref := makeNormal(r, 3000, 0, 1)
	same := makeNormal(r, 3000, 0, 1)
	shifted := makeNormal(r, 3000, 1.5, 1)

	m := NewMonitor()
	slo := FeatureSLO{Feature: "x", Method: MethodKS, WarnThreshold: 0.1, BreachThreshold: 0.2}
	_ = m.RegisterBaseline(slo, ref)

	resSame, _ := m.Score("x", same)
	resShift, _ := m.Score("x", shifted)

	if resSame.Score >= resShift.Score {
		t.Fatalf("KS should be larger for shifted (%f) than same (%f)", resShift.Score, resSame.Score)
	}
	if resShift.Severity != SeverityBreach {
		t.Fatalf("expected BREACH for shifted KS, got %s (score=%f)", resShift.Severity, resShift.Score)
	}
	if resSame.Score < 0 || resSame.Score > 1 {
		t.Fatalf("KS statistic out of [0,1]: %f", resSame.Score)
	}
}

func TestKSKnownValue(t *testing.T) {
	// Two disjoint uniform-ish sets: ref all < 0, live all > 0 => KS = 1.0.
	m := NewMonitor()
	_ = m.RegisterBaseline(FeatureSLO{Feature: "x", Method: MethodKS}, []float64{-3, -2, -1})
	res, _ := m.Score("x", []float64{1, 2, 3})
	if res.Score != 1.0 {
		t.Fatalf("disjoint samples should give KS=1.0, got %f", res.Score)
	}
}

func TestScoreErrors(t *testing.T) {
	m := NewMonitor()
	if _, err := m.Score("missing", []float64{1}); err == nil {
		t.Fatal("expected error for unregistered feature")
	}
	if err := m.RegisterBaseline(FeatureSLO{Feature: "x"}, nil); err == nil {
		t.Fatal("expected error for empty reference")
	}
	_ = m.RegisterBaseline(FeatureSLO{Feature: "x"}, []float64{1, 2, 3})
	if _, err := m.Score("x", nil); err == nil {
		t.Fatal("expected error for empty live sample")
	}
}

// ---------------------------------------------------------------------------
// M20 Prometheus export tests
// ---------------------------------------------------------------------------

func TestExporterEmitsMetrics(t *testing.T) {
	exp := NewDriftExporter("cloudai")
	exp.Observe(DriftResult{
		Feature: "amount", Method: MethodPSI, Score: 0.42,
		WarnAt: 0.1, BreachAt: 0.25, Severity: SeverityBreach, LiveCount: 1000,
	})

	expected := `
# HELP cloudai_model_drift_score Most recent drift score (PSI or KS statistic) per feature.
# TYPE cloudai_model_drift_score gauge
cloudai_model_drift_score{feature="amount",method="PSI"} 0.42
`
	if err := testutil.CollectAndCompare(exp.score, strings.NewReader(expected), "cloudai_model_drift_score"); err != nil {
		t.Fatalf("unexpected score metric: %v", err)
	}

	// Severity gauge should read 2 (breach).
	if got := testutil.ToFloat64(exp.severity.WithLabelValues("amount", "PSI")); got != 2 {
		t.Fatalf("expected severity gauge 2, got %f", got)
	}
}

func TestExporterHandlerServes(t *testing.T) {
	exp := NewDriftExporter("cloudai")
	exp.Observe(DriftResult{Feature: "f", Method: MethodKS, Score: 0.3, Severity: SeverityWarning, LiveCount: 10})
	if exp.Handler() == nil {
		t.Fatal("nil handler")
	}
	// Registry must gather without error.
	if _, err := exp.Registry().Gather(); err != nil {
		t.Fatalf("Gather: %v", err)
	}
}

// ---------------------------------------------------------------------------
// M20 benchmarks
// ---------------------------------------------------------------------------

func benchMonitor(b *testing.B, method DriftMethod, refN int) *Monitor {
	b.Helper()
	r := rand.New(rand.NewSource(42))
	ref := makeNormal(r, refN, 0, 1)
	m := NewMonitor()
	if err := m.RegisterBaseline(FeatureSLO{Feature: "x", Method: method, WarnThreshold: 0.1, BreachThreshold: 0.25}, ref); err != nil {
		b.Fatalf("RegisterBaseline: %v", err)
	}
	return m
}

func BenchmarkPSIScore1k(b *testing.B) {
	m := benchMonitor(b, MethodPSI, 10000)
	r := rand.New(rand.NewSource(7))
	live := makeNormal(r, 1000, 0.2, 1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Score("x", live); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkKSScore1k(b *testing.B) {
	m := benchMonitor(b, MethodKS, 10000)
	r := rand.New(rand.NewSource(8))
	live := makeNormal(r, 1000, 0.2, 1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Score("x", live); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPSIScore10k(b *testing.B) {
	m := benchMonitor(b, MethodPSI, 10000)
	r := rand.New(rand.NewSource(9))
	live := makeNormal(r, 10000, 0.2, 1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := m.Score("x", live); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExporterObserve(b *testing.B) {
	exp := NewDriftExporter("cloudai")
	res := DriftResult{Feature: "x", Method: MethodPSI, Score: 0.15, WarnAt: 0.1, BreachAt: 0.25, Severity: SeverityWarning, LiveCount: 1000}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		exp.Observe(res)
	}
}
