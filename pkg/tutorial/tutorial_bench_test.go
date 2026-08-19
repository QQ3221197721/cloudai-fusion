package tutorial

// tutorial_bench_test.go provides benchmarks for the five primary operations
// of the tutorial engine: tutorial loading, step progression, progress query,
// certificate issuance, and certificate verification.
//
// Run: go test ./pkg/tutorial -bench=. -benchmem -count=3 -benchtime=5x -run=^$

import (
	"encoding/json"
	"testing"
)

// 10-step linear tutorial used across benchmarks.
func benchTutorialJSON() []byte {
	tut := Tutorial{
		ID:    "bench-tutorial",
		Title: "Benchmark Tutorial",
		Steps: make([]Step, 10),
	}
	for i := range tut.Steps {
		id := stepID(i)
		tut.Steps[i] = Step{
			ID:            id,
			Title:         "Step " + id,
			Instruction:   "Do something for step " + id,
			ValidatorType: ValidatorAlwaysPass,
		}
		if i > 0 {
			tut.Steps[i].Prerequisites = []string{stepID(i - 1)}
		}
	}
	data, _ := json.Marshal(tut)
	return data
}

func stepID(i int) string {
	ids := []string{"s0", "s1", "s2", "s3", "s4", "s5", "s6", "s7", "s8", "s9"}
	if i < len(ids) {
		return ids[i]
	}
	return "s" + string(rune('0'+i))
}

// BenchmarkTutorialLoad measures JSON decode + DAG validation + topological sort.
func BenchmarkTutorialLoad(b *testing.B) {
	data := benchTutorialJSON()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := LoadTutorialJSON(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkStepProgression measures completing all steps in a 10-step chain.
func BenchmarkStepProgression(b *testing.B) {
	data := benchTutorialJSON()
	tut, _ := LoadTutorialJSON(data)
	order, _ := tut.TopologicalOrder()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		p, _ := NewProgress(tut)
		for _, id := range order {
			_ = p.Complete(id)
		}
	}
}

// BenchmarkProgressQuery measures AvailableSteps + IsComplete on a half-done
// tutorial.
func BenchmarkProgressQuery(b *testing.B) {
	data := benchTutorialJSON()
	tut, _ := LoadTutorialJSON(data)
	p, _ := NewProgress(tut)
	// Complete first 5 steps
	order, _ := tut.TopologicalOrder()
	for i := 0; i < 5; i++ {
		_ = p.Complete(order[i])
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = p.AvailableSteps()
		_ = p.IsComplete()
	}
}

// BenchmarkCertificateIssue measures certificate issuance (Ed25519 sign + hash
// chain construction).
func BenchmarkCertificateIssue(b *testing.B) {
	data := benchTutorialJSON()
	tut, _ := LoadTutorialJSON(data)
	p, _ := NewProgress(tut)
	order, _ := tut.TopologicalOrder()
	for _, id := range order {
		_ = p.Complete(id)
	}
	issuer := NewCertificateIssuer()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := issuer.IssueCertificate(p, "benchmark-user@example.com")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCertificateVerify measures offline certificate verification
// (Ed25519 verify + payload reconstruction).
func BenchmarkCertificateVerify(b *testing.B) {
	data := benchTutorialJSON()
	tut, _ := LoadTutorialJSON(data)
	p, _ := NewProgress(tut)
	order, _ := tut.TopologicalOrder()
	for _, id := range order {
		_ = p.Complete(id)
	}
	issuer := NewCertificateIssuer()
	cert, _ := issuer.IssueCertificate(p, "benchmark-user@example.com")
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ok, err := VerifyCertificate(cert)
		if err != nil || !ok {
			b.Fatalf("verify: ok=%v err=%v", ok, err)
		}
	}
}

// BenchmarkSnapshotRoundtrip measures marshal + unmarshal of progress state.
func BenchmarkSnapshotRoundtrip(b *testing.B) {
	data := benchTutorialJSON()
	tut, _ := LoadTutorialJSON(data)
	p, _ := NewProgress(tut)
	order, _ := tut.TopologicalOrder()
	for i := 0; i < 7; i++ {
		_ = p.Complete(order[i])
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		snap, err := p.MarshalSnapshot()
		if err != nil {
			b.Fatal(err)
		}
		_, err = RestoreProgress(tut, snap)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkTopologicalSort measures Kahn's algorithm on a 10-step chain.
func BenchmarkTopologicalSort(b *testing.B) {
	data := benchTutorialJSON()
	tut, _ := LoadTutorialJSON(data)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := tut.TopologicalOrder()
		if err != nil {
			b.Fatal(err)
		}
	}
}
