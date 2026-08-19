// Package pipeline — benchmarks for Module 17 (ML Pipeline Designer).
//
// These benchmarks measure the REAL, honest cost of the in-process pipeline
// state machine as it ships: every mutating operation persists a JSON file
// atomically (tmp write + rename) AND writes a signed, hash-chained attestation
// through pkg/evidence (real Ed25519 signing via an ephemeral signer + in-memory
// store). Benchmarks that isolate the pure DAG transition check use no I/O.
//
// Honesty notes captured here (see docs/performance-validation-modules-17-20.md):
//   - Timings include Windows temp-dir filesystem I/O; they are single-process,
//     single-node numbers, NOT distributed-scheduler numbers.
//   - The "train" stage runs the training module's simulated execution
//     (queued→scheduled→running→succeeded); no real container/GPU work happens.
package pipeline

import (
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/experiment"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/training"
)

// benchDesigner builds a designer wired to real sub-module APIs and a signed ledger.
// When attest is false the ledger is nil (attestation disabled), isolating pure
// state-machine + filesystem cost from the crypto-signing cost.
func benchDesigner(b *testing.B, attest bool) *FSDesigner {
	b.Helper()
	tmp := b.TempDir()

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

	orch, err := training.NewFSOrchestrator(tmp, ledger)
	if err != nil {
		b.Fatalf("training orchestrator: %v", err)
	}
	tracker, err := experiment.NewFSTracker(tmp, ledger)
	if err != nil {
		b.Fatalf("experiment tracker: %v", err)
	}
	cost := scheduler.NewDefaultCostOptimizer(nil)

	d, err := NewFSDesigner(tmp, ledger, Deps{Train: orch, Exp: tracker, Cost: cost})
	if err != nil {
		b.Fatalf("pipeline designer: %v", err)
	}
	return d
}

func benchCreateInput(name string, stages ...Stage) CreateInput {
	if len(stages) == 0 {
		stages = []Stage{{Name: "notify", Type: StageNotify}}
	}
	return CreateInput{
		Name:    name,
		Stages:  stages,
		Params:  map[string]string{"epochs": "50", "batch": "32"},
		Trigger: Trigger{Type: TriggerManual},
		Actor:   "bench-runner",
	}
}

// BenchmarkTransitionCheckLegal measures the pure DAG legality check for a legal
// edge (draft→published). No I/O, no signing — this is the state-machine core.
func BenchmarkTransitionCheckLegal(b *testing.B) {
	b.ReportAllocs()
	var ok bool
	for i := 0; i < b.N; i++ {
		ok = canTransition(StatusDraft, StatusPublished)
	}
	_ = ok
}

// BenchmarkTransitionCheckIllegal measures the rejection cost for an illegal edge
// (completed→running). Terminal states have empty transition lists.
func BenchmarkTransitionCheckIllegal(b *testing.B) {
	b.ReportAllocs()
	var ok bool
	for i := 0; i < b.N; i++ {
		ok = canTransition(StatusCompleted, StatusRunning)
	}
	_ = ok
}

// BenchmarkPipelineCreate measures full create cost (validate + persist JSON + attest).
func BenchmarkPipelineCreate(b *testing.B) {
	d := benchDesigner(b, true)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := d.Create(ctx, benchCreateInput("bench-create")); err != nil {
			b.Fatalf("create: %v", err)
		}
	}
}

// BenchmarkPipelinePublish isolates the draft→published transition (persist + attest).
// Per-iteration Create runs with the timer stopped so only Publish is measured.
func BenchmarkPipelinePublish(b *testing.B) {
	d := benchDesigner(b, true)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		p, err := d.Create(ctx, benchCreateInput("bench-publish"))
		if err != nil {
			b.Fatalf("create: %v", err)
		}
		b.StartTimer()
		if err := d.Publish(ctx, p.ID); err != nil {
			b.Fatalf("publish: %v", err)
		}
	}
}

// BenchmarkPipelineRejectIllegalRun measures the end-to-end cost of a rejected
// illegal transition (Run on a draft pipeline → error before any stage executes).
func BenchmarkPipelineRejectIllegalRun(b *testing.B) {
	d := benchDesigner(b, true)
	ctx := context.Background()
	p, err := d.Create(ctx, benchCreateInput("bench-illegal"))
	if err != nil {
		b.Fatalf("create: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := d.Run(ctx, p.ID); err == nil {
			b.Fatal("expected illegal-transition rejection, got nil")
		}
	}
}

// BenchmarkPipelineRunNotify measures published→running→completed scheduling latency
// for a single notify stage (no sub-module calls) — this isolates the designer's own
// orchestration + persistence + attestation overhead per stage.
func BenchmarkPipelineRunNotify(b *testing.B) {
	d := benchDesigner(b, true)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		p, err := d.Create(ctx, benchCreateInput("bench-run-notify",
			Stage{Name: "notify", Type: StageNotify}))
		if err != nil {
			b.Fatalf("create: %v", err)
		}
		if err := d.Publish(ctx, p.ID); err != nil {
			b.Fatalf("publish: %v", err)
		}
		b.StartTimer()
		if err := d.Run(ctx, p.ID); err != nil {
			b.Fatalf("run: %v", err)
		}
	}
}

// BenchmarkPipelineRunTrainExp measures a realistic 2-stage run (train + experiment)
// end-to-end: submits a training job through its simulated state machine and starts,
// logs, and completes an experiment — all with attestation. This is the closest
// analogue to an Airflow "DAG run" for a small ML flow.
func BenchmarkPipelineRunTrainExp(b *testing.B) {
	d := benchDesigner(b, true)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		p, err := d.Create(ctx, benchCreateInput("bench-run-trainexp",
			Stage{Name: "train", Type: StageTrain},
			Stage{Name: "exp", Type: StageExperiment}))
		if err != nil {
			b.Fatalf("create: %v", err)
		}
		if err := d.Publish(ctx, p.ID); err != nil {
			b.Fatalf("publish: %v", err)
		}
		b.StartTimer()
		if err := d.Run(ctx, p.ID); err != nil {
			b.Fatalf("run: %v", err)
		}
	}
}
