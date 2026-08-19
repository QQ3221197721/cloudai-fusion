// Package experiment — benchmarks for Module 18 (Experiment Tracking System).
//
// Measures the honest, in-process cost of the experiment lifecycle as shipped.
// LogMetric/Start/Complete each persist a JSON record atomically AND write a
// signed, hash-chained attestation via pkg/evidence (real Ed25519 signing). The
// noLedger variants disable attestation to separate crypto cost from FS + logic.
//
// These are single-process, single-node numbers and include Windows temp-dir
// filesystem I/O. There is no tracking server, DB, or network round-trip — the
// architectural opposite of MLflow's client→REST→backend-store model.
package experiment

import (
	"context"
	"fmt"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

func benchTracker(b *testing.B, attest bool) *FSTracker {
	b.Helper()
	var ledger *evidence.Ledger
	if attest {
		signer, err := evidence.GenerateEphemeralSigner()
		if err != nil {
			b.Fatalf("generate signer: %v", err)
		}
		ledger, err = evidence.NewLedger(evidence.LedgerConfig{
			Store:    evidence.NewMemoryStore(),
			Signer:   signer,
			Anchorer: evidence.NewSimulatedAnchorer(),
		})
		if err != nil {
			b.Fatalf("build ledger: %v", err)
		}
	}
	trk, err := NewFSTracker(b.TempDir(), ledger)
	if err != nil {
		b.Fatalf("new tracker: %v", err)
	}
	return trk
}

// BenchmarkExperimentStart measures creating a running experiment (persist + attest).
func BenchmarkExperimentStart(b *testing.B) {
	trk := benchTracker(b, true)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := trk.Start(ctx, StartInput{
			Name:        "bench-exp",
			Hyperparams: map[string]string{"lr": "0.001", "batch": "32"},
		}); err != nil {
			b.Fatalf("start: %v", err)
		}
	}
}

// benchmarkLogMetric measures LogMetric throughput (append history + overwrite latest
// + persist + optionally attest). One long-lived experiment accumulates history.
func benchmarkLogMetric(b *testing.B, attest bool) {
	trk := benchTracker(b, attest)
	ctx := context.Background()
	exp, err := trk.Start(ctx, StartInput{Name: "bench-log"})
	if err != nil {
		b.Fatalf("start: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := trk.LogMetric(ctx, exp.ID, "accuracy", 0.9); err != nil {
			b.Fatalf("log metric: %v", err)
		}
	}
}

// BenchmarkExperimentLogMetric measures logging throughput WITH signed attestation.
func BenchmarkExperimentLogMetric(b *testing.B) { benchmarkLogMetric(b, true) }

// BenchmarkExperimentLogMetricNoLedger isolates FS + logic cost (attestation off).
func BenchmarkExperimentLogMetricNoLedger(b *testing.B) { benchmarkLogMetric(b, false) }

// BenchmarkExperimentGet measures single-experiment query (read + JSON parse) latency.
func BenchmarkExperimentGet(b *testing.B) {
	trk := benchTracker(b, true)
	ctx := context.Background()
	exp, err := trk.Start(ctx, StartInput{Name: "bench-get"})
	if err != nil {
		b.Fatalf("start: %v", err)
	}
	// Give it some metric history so parse cost is representative.
	for i := 0; i < 20; i++ {
		if err := trk.LogMetric(ctx, exp.ID, fmt.Sprintf("m%d", i), float64(i)); err != nil {
			b.Fatalf("log: %v", err)
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := trk.Get(ctx, exp.ID); err != nil {
			b.Fatalf("get: %v", err)
		}
	}
}

// BenchmarkExperimentCompare measures the honest head-to-head diff of two completed
// experiments (2 reads + union hyperparam/metric math).
func BenchmarkExperimentCompare(b *testing.B) {
	trk := benchTracker(b, true)
	ctx := context.Background()

	a, err := trk.Start(ctx, StartInput{Name: "cmp-a", Hyperparams: map[string]string{"lr": "0.001", "batch": "32"}})
	if err != nil {
		b.Fatalf("start a: %v", err)
	}
	bexp, err := trk.Start(ctx, StartInput{Name: "cmp-b", Hyperparams: map[string]string{"lr": "0.01", "batch": "32"}})
	if err != nil {
		b.Fatalf("start b: %v", err)
	}
	for _, m := range []struct {
		id   string
		vals map[string]float64
	}{
		{a.ID, map[string]float64{"accuracy": 0.90, "loss": 0.30}},
		{bexp.ID, map[string]float64{"accuracy": 0.94, "loss": 0.28, "f1": 0.88}},
	} {
		for k, v := range m.vals {
			if err := trk.LogMetric(ctx, m.id, k, v); err != nil {
				b.Fatalf("log: %v", err)
			}
		}
	}
	if err := trk.Complete(ctx, a.ID, ""); err != nil {
		b.Fatalf("complete a: %v", err)
	}
	if err := trk.Complete(ctx, bexp.ID, "resnet50:1.2.0"); err != nil {
		b.Fatalf("complete b: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := trk.Compare(ctx, a.ID, bexp.ID); err != nil {
			b.Fatalf("compare: %v", err)
		}
	}
}
