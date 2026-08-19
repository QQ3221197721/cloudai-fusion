// Package modelregistry — performance benchmarks for Module 13 Model Registry.
// This file provides comprehensive performance benchmarks covering the four
// critical paths: registration throughput, query latency, lineage verification
// (including Ed25519 signature verification), and large-scale listing.
//
// Run: go test ./pkg/modelregistry/ -bench=. -benchmem -count=5
package modelregistry

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// benchSeeds provides 10 distinct artifact payloads so that the content-addressed
// store exercises both dedup hits and unique writes across b.N iterations.
var benchSeeds = [10][]byte{
	[]byte("seed-0-weights-payload-Aa1Bb2Cc3Dd4"),
	[]byte("seed-1-weights-payload-Ee5Ff6Gg7Hh8"),
	[]byte("seed-2-weights-payload-Ii9Jj0Kk1Ll2"),
	[]byte("seed-3-weights-payload-Mm3Nn4Oo5Pp6"),
	[]byte("seed-4-weights-payload-Qq7Rr8Ss9Tt0"),
	[]byte("seed-5-weights-payload-Uu1Vv2Ww3Xx4"),
	[]byte("seed-6-weights-payload-Yy5Zz6Aa7Bb8"),
	[]byte("seed-7-weights-payload-Cc9Dd0Ee1Ff2"),
	[]byte("seed-8-weights-payload-Gg3Hh4Ii5Jj6"),
	[]byte("seed-9-weights-payload-Kk7Ll8Mm9Nn0"),
}

// benchRegistry builds a registry with a real Ed25519-signing ledger so the
// benchmarked Register/Rollback latencies include the true attestation cost.
func benchRegistry(b *testing.B) *FSRegistry {
	b.Helper()
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		b.Fatalf("signer: %v", err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{
		Store:    evidence.NewMemoryStore(),
		Signer:   signer,
		Anchorer: evidence.NewSimulatedAnchorer(),
	})
	if err != nil {
		b.Fatalf("ledger: %v", err)
	}
	reg, err := NewFSRegistry(b.TempDir(), ledger)
	if err != nil {
		b.Fatalf("new registry: %v", err)
	}
	return reg
}

// semver returns a valid MAJOR.MINOR.PATCH string for index i (1.<i>.0).
func semver(i int) string {
	// keep segments free of leading zeros to satisfy validateVersion
	return "1." + itoa(i) + ".0"
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}

// BenchmarkRegisterModel measures end-to-end registration throughput including
// content-addressing (sha256), JSON record persistence, current-pointer update,
// and Ed25519-signed attestation. 10 distinct artifact payloads cycle to vary
// dedup behavior; b.N handles repetition.
func BenchmarkRegisterModel(b *testing.B) {
	reg := benchRegistry(b)
	ctx := context.Background()
	dir := b.TempDir()
	// Pre-create 10 seed artifacts in one temp dir.
	paths := make([]string, 10)
	for i := 0; i < 10; i++ {
		paths[i] = filepath.Join(dir, fmt.Sprintf("seed-%d.pt", i))
		if err := os.WriteFile(paths[i], benchSeeds[i], 0o644); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := reg.Register(ctx, RegisterInput{
			Name:         "benchreg",
			Version:      semver(i),
			ArtifactPath: paths[i%10],
			DatasetRef:   "sha256:dataset-ref",
			CodeRef:      "git:abc123",
		}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGetModel measures the query latency for a specific version: file read +
// JSON unmarshal. "latest" resolution (pointer read) is excluded to isolate the
// record-access path. 10 pre-registered versions cycle across b.N iterations.
func BenchmarkGetModel(b *testing.B) {
	reg := benchRegistry(b)
	ctx := context.Background()
	dir := b.TempDir()
	// Pre-register 10 versions.
	for i := 0; i < 10; i++ {
		p := filepath.Join(dir, fmt.Sprintf("v%d.pt", i))
		if err := os.WriteFile(p, benchSeeds[i], 0o644); err != nil {
			b.Fatal(err)
		}
		if _, err := reg.Register(ctx, RegisterInput{
			Name: "getmodel", Version: semver(i), ArtifactPath: p,
		}); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := reg.Get(ctx, "getmodel", semver(i%10)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkVerifyLineage measures the full cryptographic integrity check:
// (1) recompute sha256(blob) and compare to content address,
// (2) recompute sha256(canonical(record)) and compare to sealed attestation digest,
// (3) re-verify the ENTIRE Ed25519-signed hash-chained attestation ledger offline.
// A 10-deep fine-tune lineage chain is pre-built; the leaf version is verified each iter.
func BenchmarkVerifyLineage(b *testing.B) {
	reg := benchRegistry(b)
	ctx := context.Background()
	dir := b.TempDir()
	// Build a 10-deep fine-tune chain (1.0.0 → 1.1.0 → ... → 1.9.0).
	prev := ""
	for i := 0; i < 10; i++ {
		v := semver(i)
		p := filepath.Join(dir, v+".pt")
		if err := os.WriteFile(p, benchSeeds[i], 0o644); err != nil {
			b.Fatal(err)
		}
		if _, err := reg.Register(ctx, RegisterInput{
			Name: "vchain", Version: v, ArtifactPath: p, ParentVersion: prev,
		}); err != nil {
			b.Fatal(err)
		}
		prev = v
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rep, err := reg.Verify(ctx, "vchain", semver(9))
		if err != nil {
			b.Fatal(err)
		}
		if rep.Tampered {
			b.Fatal("unexpected tamper flag on clean record")
		}
	}
}

// BenchmarkListModels measures large-scale listing: enumerate all model
// directories, read+parse every version record JSON, sort newest-first.
// 10 models × 10 versions = 100 records.
func BenchmarkListModels(b *testing.B) {
	reg := benchRegistry(b)
	ctx := context.Background()
	dir := b.TempDir()
	// Register 10 models × 10 versions = 100 records.
	for m := 0; m < 10; m++ {
		name := fmt.Sprintf("list-m%d", m)
		prev := ""
		for v := 0; v < 10; v++ {
			ver := semver(v)
			p := filepath.Join(dir, fmt.Sprintf("%s-%s.pt", name, ver))
			if err := os.WriteFile(p, benchSeeds[v], 0o644); err != nil {
				b.Fatal(err)
			}
			if _, err := reg.Register(ctx, RegisterInput{
				Name: name, Version: ver, ArtifactPath: p, ParentVersion: prev,
			}); err != nil {
				b.Fatal(err)
			}
			prev = ver
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		arts, err := reg.List(ctx, "")
		if err != nil {
			b.Fatal(err)
		}
		if len(arts) != 100 {
			b.Fatalf("expected 100 records, got %d", len(arts))
		}
	}
}
