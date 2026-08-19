package provenance

// perf_bench_test.go benchmarks the REAL cryptographic provenance path for
// Modules 33-36 performance validation:
//   - DatasetManifest build (RFC6962 Merkle root over N records + Ed25519 sign)
//   - SLSA-style ModelProvenance sign / verify (Ed25519)
//   - ZKP-style checkpoint record / verify (MinHash fingerprint + signed receipt)
//
// These exercise genuine Ed25519 signing/verification and Merkle hashing via
// pkg/evidence; there is no simulated crypto in this path.

import (
	"bytes"
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

func benchLedger(b *testing.B, seed byte) *evidence.Ledger {
	b.Helper()
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{seed}, 32))
	if err != nil {
		b.Fatalf("signer: %v", err)
	}
	l, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		b.Fatalf("ledger: %v", err)
	}
	return l
}

func benchRecords(b *testing.B, l *evidence.Ledger, n int) []*evidence.Evidence {
	b.Helper()
	out := make([]*evidence.Evidence, 0, n)
	for i := 0; i < n; i++ {
		ev, err := l.Record(context.Background(), evidence.RecordInput{
			Actor: "trainer", Action: "trace.sample", Subject: "s", Payload: map[string]any{"i": i},
		})
		if err != nil {
			b.Fatalf("record: %v", err)
		}
		out = append(out, ev)
	}
	return out
}

// BenchmarkBuildDatasetManifest measures manifest generation (Merkle root over a
// fixed-size record set + Ed25519 signature) at several corpus sizes. This is
// our closest analog to "committing a fixed dependency/artifact set".
func BenchmarkBuildDatasetManifest(b *testing.B) {
	sizes := []int{10, 100, 1000}
	for _, n := range sizes {
		l := benchLedger(b, 0x41)
		corpus := benchRecords(b, l, n)
		signer := l.Signer()
		b.Run(sizeName(n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := BuildDatasetManifest(signer, "redteam-dpo", corpus, nil); err != nil {
					b.Fatalf("build manifest: %v", err)
				}
			}
		})
	}
}

// BenchmarkSignProvenance measures a single Ed25519 provenance signature
// (SLSA-for-models attestation generation).
func BenchmarkSignProvenance(b *testing.B) {
	l := benchLedger(b, 0x42)
	signer := l.Signer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		p := &ModelProvenance{
			BaseModelHash:   "sha256:base",
			DatasetManifest: "sha256:manifest",
			TrainConfigHash: "sha256:cfg",
			WeightsHash:     "sha256:weights",
			Method:          "dpo",
			Trainer:         "trainer.py",
		}
		if err := SignProvenance(signer, p); err != nil {
			b.Fatalf("sign: %v", err)
		}
	}
}

// BenchmarkVerifyModelProvenance measures the full auditor verification path:
// two Ed25519 signature checks (manifest + provenance) plus the manifest-hash
// binding check (SLSA provenance verification).
func BenchmarkVerifyModelProvenance(b *testing.B) {
	l := benchLedger(b, 0x43)
	signer := l.Signer()
	pub := signer.PublicKey()

	corpus := benchRecords(b, l, 100)
	manifest, err := BuildDatasetManifest(signer, "corpus", corpus, nil)
	if err != nil {
		b.Fatalf("manifest: %v", err)
	}
	mh, _ := ManifestHash(manifest)
	prov := &ModelProvenance{
		BaseModelHash: "b", DatasetManifest: mh, WeightsHash: "w", Method: "sft",
	}
	if err := SignProvenance(signer, prov); err != nil {
		b.Fatalf("sign: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := VerifyModelProvenance(manifest, prov, pub); err != nil {
			b.Fatalf("verify: %v", err)
		}
	}
}

// BenchmarkZKPRecordCheckpoint measures training-lineage receipt generation
// (MinHash-128 dataset fingerprint + two signed Ed25519 receipts).
func BenchmarkZKPRecordCheckpoint(b *testing.B) {
	rec := NewZKPProvenanceRecorder()
	cp := TrainingCheckpoint{
		CheckpointID:    "ckpt-1",
		DatasetID:       "dataset-alpha",
		Hyperparameters: map[string]float64{"lr": 0.001, "batch": 32},
		TrainingMetrics: map[string]float64{"loss": 0.12, "acc": 0.94},
		EpochsTrained:   10,
		LearningRate:    0.001,
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := rec.RecordCheckpoint(cp); err != nil {
			b.Fatalf("record checkpoint: %v", err)
		}
	}
}

// BenchmarkZKPVerifyProvenance measures verification of a training-lineage proof.
func BenchmarkZKPVerifyProvenance(b *testing.B) {
	rec := NewZKPProvenanceRecorder()
	cp := TrainingCheckpoint{
		CheckpointID: "ckpt-1", DatasetID: "dataset-alpha", EpochsTrained: 10,
	}
	proof, err := rec.RecordCheckpoint(cp)
	if err != nil {
		b.Fatalf("record: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !rec.VerifyProvenance(proof) {
			b.Fatal("verify must succeed")
		}
	}
}

func sizeName(n int) string {
	switch n {
	case 10:
		return "records=10"
	case 100:
		return "records=100"
	case 1000:
		return "records=1000"
	default:
		return "records=?"
	}
}
