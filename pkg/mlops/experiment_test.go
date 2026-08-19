package mlops

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// M19 Experiment Tracking — correctness tests
// ---------------------------------------------------------------------------

func TestExperimentRunLifecycle(t *testing.T) {
	s := NewTrackingStore("")
	exp := s.CreateExperiment("fraud-model", map[string]string{"team": "risk"})
	if exp.ID == "" {
		t.Fatal("expected non-empty experiment id")
	}

	run, err := s.StartRun(exp.ID, "baseline", map[string]string{"lr": "0.01"})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if run.Status != RunRunning {
		t.Fatalf("expected RUNNING, got %s", run.Status)
	}

	if err := s.LogParam(run.ID, "epochs", "10"); err != nil {
		t.Fatalf("LogParam: %v", err)
	}
	for i := 0; i < 5; i++ {
		if err := s.LogMetric(run.ID, "auc", 0.8+float64(i)*0.01, int64(i)); err != nil {
			t.Fatalf("LogMetric: %v", err)
		}
	}
	if err := s.LogArtifact(run.ID, Artifact{Name: "model.pt", URI: "s3://b/model.pt", SizeBytes: 1024, SHA256: "abc"}); err != nil {
		t.Fatalf("LogArtifact: %v", err)
	}
	if err := s.FinishRun(run.ID, RunFinished); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}

	got, ok := s.GetRun(run.ID)
	if !ok {
		t.Fatal("GetRun missing")
	}
	if got.Status != RunFinished || got.EndTime == nil {
		t.Fatalf("run not finalized: %+v", got)
	}
	if got.Params["epochs"] != "10" || got.Params["lr"] != "0.01" {
		t.Fatalf("params wrong: %+v", got.Params)
	}
	if v, ok := s.LatestMetric(run.ID, "auc"); !ok || v < 0.8399 || v > 0.8401 {
		t.Fatalf("latest metric wrong: %v %v", v, ok)
	}
	if len(got.Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(got.Artifacts))
	}
}

func TestGetRunReturnsCopy(t *testing.T) {
	s := NewTrackingStore("")
	exp := s.CreateExperiment("e", nil)
	run, _ := s.StartRun(exp.ID, "r", map[string]string{"a": "1"})
	got, _ := s.GetRun(run.ID)
	got.Params["a"] = "mutated"
	fresh, _ := s.GetRun(run.ID)
	if fresh.Params["a"] != "1" {
		t.Fatalf("store state mutated via returned copy: %s", fresh.Params["a"])
	}
}

func TestListRunsMetricFilter(t *testing.T) {
	s := NewTrackingStore("")
	exp := s.CreateExperiment("e", nil)
	for i := 0; i < 10; i++ {
		r, _ := s.StartRun(exp.ID, "r", nil)
		_ = s.LogMetric(r.ID, "auc", 0.5+float64(i)*0.05, 0)
		_ = s.FinishRun(r.ID, RunFinished)
	}
	res := s.ListRuns(RunQuery{ExperimentID: exp.ID, MetricName: "auc", MetricOp: ">=", MetricValue: 0.9})
	if len(res) != 2 { // 0.90, 0.95
		t.Fatalf("expected 2 runs >= 0.9, got %d", len(res))
	}
	// verify newest-first ordering
	if len(res) >= 2 && res[0].StartTime.Before(res[1].StartTime) {
		t.Fatal("ListRuns not sorted newest-first")
	}
}

func TestPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "store.json")
	s := NewTrackingStore(path)
	exp := s.CreateExperiment("persisted", nil)
	run, _ := s.StartRun(exp.ID, "r", map[string]string{"k": "v"})
	_ = s.LogMetric(run.ID, "loss", 0.3, 1)
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("snapshot not written: %v", err)
	}

	s2 := NewTrackingStore(path)
	if err := s2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, ok := s2.GetRun(run.ID)
	if !ok {
		t.Fatal("run not restored")
	}
	if got.Params["k"] != "v" {
		t.Fatalf("params not restored: %+v", got.Params)
	}
	if v, ok := s2.LatestMetric(run.ID, "loss"); !ok || v != 0.3 {
		t.Fatalf("metric not restored: %v %v", v, ok)
	}
}

// ---------------------------------------------------------------------------
// M19 Ed25519 provenance tests
// ---------------------------------------------------------------------------

func newSealedRun(t *testing.T) (*Sealer, *Run) {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i)
	}
	sealer, err := NewSealerFromSeed(seed)
	if err != nil {
		t.Fatalf("NewSealerFromSeed: %v", err)
	}
	run := &Run{
		ID:           "run-1",
		ExperimentID: "exp-1",
		Name:         "r",
		Status:       RunFinished,
		Params:       map[string]string{"lr": "0.01", "epochs": "10"},
		Metrics:      map[string][]MetricPoint{"auc": {{Value: 0.9, Step: 1}}},
		Artifacts:    []Artifact{{Name: "m.pt", SHA256: "deadbeef"}},
	}
	if _, err := sealer.Seal(run); err != nil {
		t.Fatalf("Seal: %v", err)
	}
	return sealer, run
}

func TestProvenanceVerifyValid(t *testing.T) {
	_, run := newSealedRun(t)
	if err := Verify(run); err != nil {
		t.Fatalf("expected valid provenance, got %v", err)
	}
}

func TestProvenanceDetectsTampering(t *testing.T) {
	_, run := newSealedRun(t)
	// Tamper with a metric after sealing (the MLflow attack we defend against).
	run.Metrics["auc"] = []MetricPoint{{Value: 0.99, Step: 1}}
	if err := Verify(run); err == nil {
		t.Fatal("expected verification failure after metric tampering")
	}
}

func TestProvenanceDetectsParamTampering(t *testing.T) {
	_, run := newSealedRun(t)
	run.Params["lr"] = "0.5"
	if err := Verify(run); err == nil {
		t.Fatal("expected verification failure after param tampering")
	}
}

func TestProvenanceDeterministicAcrossMapOrder(t *testing.T) {
	// Two runs with the same content but params inserted in different order
	// must produce the same fingerprint.
	seed := make([]byte, ed25519.SeedSize)
	sealer, _ := NewSealerFromSeed(seed)
	a := &Run{ID: "x", Params: map[string]string{"a": "1", "b": "2", "c": "3"}}
	b := &Run{ID: "x", Params: map[string]string{"c": "3", "a": "1", "b": "2"}}
	pa, _ := sealer.Seal(a)
	pb, _ := sealer.Seal(b)
	if pa.Fingerprint != pb.Fingerprint {
		t.Fatalf("fingerprint depends on map order: %s vs %s", pa.Fingerprint, pb.Fingerprint)
	}
}

// ---------------------------------------------------------------------------
// M19 benchmarks
// ---------------------------------------------------------------------------

func BenchmarkLogMetricThroughput(b *testing.B) {
	s := NewTrackingStore("")
	exp := s.CreateExperiment("bench", nil)
	run, _ := s.StartRun(exp.ID, "r", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.LogMetric(run.ID, "auc", float64(i), int64(i))
	}
}

func BenchmarkStartRunThroughput(b *testing.B) {
	s := NewTrackingStore("")
	exp := s.CreateExperiment("bench", nil)
	params := map[string]string{"lr": "0.01", "opt": "adam"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.StartRun(exp.ID, "r", params)
	}
}

func BenchmarkMetricQueryLatency(b *testing.B) {
	s := NewTrackingStore("")
	exp := s.CreateExperiment("bench", nil)
	// Populate 1000 runs each with a final auc metric.
	for i := 0; i < 1000; i++ {
		r, _ := s.StartRun(exp.ID, "r", nil)
		_ = s.LogMetric(r.ID, "auc", float64(i%100)/100.0, 0)
		_ = s.FinishRun(r.ID, RunFinished)
	}
	q := RunQuery{ExperimentID: exp.ID, MetricName: "auc", MetricOp: ">=", MetricValue: 0.9}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.ListRuns(q)
	}
}

func BenchmarkSealRun(b *testing.B) {
	seed := make([]byte, ed25519.SeedSize)
	sealer, _ := NewSealerFromSeed(seed)
	run := &Run{
		ID:      "run",
		Params:  map[string]string{"lr": "0.01", "epochs": "10", "opt": "adam"},
		Metrics: map[string][]MetricPoint{"auc": {{Value: 0.9}}, "loss": {{Value: 0.1}}},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = sealer.Seal(run)
	}
}

func BenchmarkVerifyRun(b *testing.B) {
	seed := make([]byte, ed25519.SeedSize)
	sealer, _ := NewSealerFromSeed(seed)
	run := &Run{
		ID:      "run",
		Params:  map[string]string{"lr": "0.01", "epochs": "10", "opt": "adam"},
		Metrics: map[string][]MetricPoint{"auc": {{Value: 0.9}}, "loss": {{Value: 0.1}}},
	}
	_, _ = sealer.Seal(run)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Verify(run)
	}
}
