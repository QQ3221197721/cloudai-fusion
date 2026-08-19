// Package evidence_test contains performance benchmarks for the evidence chain.
//
// These run under `go test -bench` and report REAL numbers (no mocks):
//   - BenchmarkEvidenceAppend  target: >50K attestations/sec
//   - BenchmarkEvidenceVerify  target: full 10K-entry chain verification <100ms
//   - BenchmarkZKPProve        real Groth16-BN254-Poseidon2 proving time
//   - BenchmarkZKPVerify       target: <10ms per proof verification
//
// It is an EXTERNAL test package (evidence_test) so it can import
// pkg/evidence/zk — which itself imports pkg/evidence — without an import cycle.
package evidence_test

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence/zk"
)

// newBenchLedger builds an in-memory ledger with a deterministic Ed25519 signer.
func newBenchLedger(b *testing.B) *evidence.Ledger {
	b.Helper()
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		b.Fatalf("signer: %v", err)
	}
	l, err := evidence.NewLedger(evidence.LedgerConfig{
		Store:  evidence.NewMemoryStore(),
		Signer: signer,
	})
	if err != nil {
		b.Fatalf("ledger: %v", err)
	}
	return l
}

// benchRecordInput returns a representative attestation payload.
func benchRecordInput(i int) evidence.RecordInput {
	return evidence.RecordInput{
		Actor:   "bench",
		Action:  "deploy.update",
		Subject: "app=payments",
		Input:   map[string]any{"seq": i, "image": "payments:v2.3.1"},
		Output:  map[string]any{"status": "recorded"},
		Payload: map[string]any{"note": "benchmark attestation"},
	}
}

// BenchmarkEvidenceAppend measures signed, hash-chained appends per second.
// Divide b.N by the wall time (or read the ns/op) to get attestations/sec;
// target is >50,000/sec.
func BenchmarkEvidenceAppend(b *testing.B) {
	l := newBenchLedger(b)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := l.Record(ctx, benchRecordInput(i)); err != nil {
			b.Fatalf("record: %v", err)
		}
	}
	b.StopTimer()

	// Emit a human-readable throughput number alongside the standard ns/op.
	if elapsed := b.Elapsed().Seconds(); elapsed > 0 {
		b.ReportMetric(float64(b.N)/elapsed, "attest/sec")
	}
}

// BenchmarkEvidenceVerify measures full-chain verification of a 10,000-entry
// ledger. The ns/op reported is the time to verify the entire chain once;
// target is <100ms (i.e. <1e8 ns/op).
func BenchmarkEvidenceVerify(b *testing.B) {
	const chainLen = 10000
	l := newBenchLedger(b)
	ctx := context.Background()

	for i := 0; i < chainLen; i++ {
		if _, err := l.Record(ctx, benchRecordInput(i)); err != nil {
			b.Fatalf("seed record %d: %v", i, err)
		}
	}
	all, err := l.Store().All(ctx)
	if err != nil {
		b.Fatalf("load chain: %v", err)
	}
	if len(all) != chainLen {
		b.Fatalf("chain length = %d, want %d", len(all), chainLen)
	}
	pub := l.Signer().PublicKey()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rep, err := evidence.VerifyChain(all, pub)
		if err != nil {
			b.Fatalf("verify: %v", err)
		}
		if !rep.Valid || rep.Verified != chainLen {
			b.Fatalf("chain invalid: %+v", rep)
		}
	}
	b.StopTimer()

	if b.N > 0 {
		perEntry := float64(b.Elapsed().Nanoseconds()) / float64(b.N) / float64(chainLen)
		b.ReportMetric(perEntry, "ns/entry")
	}
}

// seedChain records chainLen receipts into a fresh in-memory ledger and returns
// the full chain plus its verifying public key.
func seedChain(b *testing.B, chainLen int) ([]*evidence.Evidence, ed25519.PublicKey) {
	b.Helper()
	l := newBenchLedger(b)
	ctx := context.Background()
	for i := 0; i < chainLen; i++ {
		if _, err := l.Record(ctx, benchRecordInput(i)); err != nil {
			b.Fatalf("seed record %d: %v", i, err)
		}
	}
	all, err := l.Store().All(ctx)
	if err != nil {
		b.Fatalf("load chain: %v", err)
	}
	if len(all) != chainLen {
		b.Fatalf("chain length = %d, want %d", len(all), chainLen)
	}
	return all, l.Signer().PublicKey()
}

// BenchmarkParallelVerify_10K measures full-chain verification of a 10,000-entry
// ledger using the PARALLEL verifier (auto-detecting CPU count). Compare its
// ns/entry against BenchmarkEvidenceVerify (sequential) to read the speedup;
// target is <100ms total on a multi-core machine (<1e8 ns/op).
func BenchmarkParallelVerify_10K(b *testing.B) {
	const chainLen = 10000
	all, pub := seedChain(b, chainLen)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rep, err := evidence.ParallelVerifyChain(all, pub, 0) // 0 => runtime.NumCPU()
		if err != nil {
			b.Fatalf("parallel verify: %v", err)
		}
		if !rep.Valid || rep.Verified != chainLen {
			b.Fatalf("chain invalid: %+v", rep)
		}
	}
	b.StopTimer()

	if b.N > 0 {
		perEntry := float64(b.Elapsed().Nanoseconds()) / float64(b.N) / float64(chainLen)
		b.ReportMetric(perEntry, "ns/entry")
	}
}

// BenchmarkParallelVerify_Scaling verifies the same 10K chain at fixed worker
// counts so the near-linear speedup is directly observable (1 vs N workers).
func BenchmarkParallelVerify_Scaling(b *testing.B) {
	const chainLen = 10000
	all, pub := seedChain(b, chainLen)

	for _, workers := range []int{1, 2, 4, 8, 0} {
		name := "workers=auto"
		if workers > 0 {
			name = fmt.Sprintf("workers=%d", workers)
		}
		b.Run(name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rep, err := evidence.ParallelVerifyChain(all, pub, workers)
				if err != nil {
					b.Fatalf("parallel verify: %v", err)
				}
				if !rep.Valid {
					b.Fatalf("chain invalid: %+v", rep)
				}
			}
			b.StopTimer()
			if b.N > 0 {
				perEntry := float64(b.Elapsed().Nanoseconds()) / float64(b.N) / float64(chainLen)
				b.ReportMetric(perEntry, "ns/entry")
			}
		})
	}
}

// BenchmarkBatchAppend_1K measures batched, signed, hash-chained appends via
// Ledger.BatchRecord (parallel content-hash precompute + sequential signing).
// Reports attest/sec; target is >50,000/sec.
func BenchmarkBatchAppend_1K(b *testing.B) {
	const batch = 1000
	l := newBenchLedger(b)
	ctx := context.Background()
	requests := make([]evidence.RecordInput, batch)
	for i := range requests {
		requests[i] = benchRecordInput(i)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := l.BatchRecord(ctx, requests); err != nil {
			b.Fatalf("batch append: %v", err)
		}
	}
	b.StopTimer()

	if elapsed := b.Elapsed().Seconds(); elapsed > 0 {
		b.ReportMetric(float64(b.N*batch)/elapsed, "attest/sec")
	}
}

// benchWitnesses builds n in-scope confidential members for the ZK circuit.
func benchWitnesses(ns string, n int) []zk.LeafWitness {
	nsFE := zk.FieldFromBytes([]byte(ns))
	ws := make([]zk.LeafWitness, n)
	for i := 0; i < n; i++ {
		ws[i] = zk.LeafWitness{
			Namespace:   nsFE,
			Eidx:        uint64(i),
			InScope:     true,
			PayloadHash: zk.FieldFromBytes([]byte{byte(i), 0xAB}),
		}
	}
	return ws
}

// BenchmarkZKPProve measures REAL Groth16 proving time (compile + trusted setup +
// prove) for a fixed-size membership statement. This is intentionally expensive;
// it establishes the true cost of a zero-knowledge attestation.
func BenchmarkZKPProve(b *testing.B) {
	ctx := context.Background()
	ws := benchWitnesses("bench/engagement/E1", 8)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		att, vk, err := zk.Groth16Prover{}.Prove(ctx, zk.StmtScopeCompliance, "in scope", ws)
		if err != nil {
			b.Fatalf("prove: %v", err)
		}
		if att == nil || len(vk) == 0 {
			b.Fatal("prove returned empty attestation/vk")
		}
	}
}

// BenchmarkZKPVerify measures per-proof verification time. The proof is generated
// once outside the timed loop; target is <10ms per verification (<1e7 ns/op).
func BenchmarkZKPVerify(b *testing.B) {
	ctx := context.Background()
	ws := benchWitnesses("bench/engagement/E2", 8)

	att, vk, err := zk.Groth16Prover{}.Prove(ctx, zk.StmtScopeCompliance, "in scope", ws)
	if err != nil {
		b.Fatalf("prove (setup): %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := zk.VerifyZK(att, vk); err != nil {
			b.Fatalf("verify: %v", err)
		}
	}
}
