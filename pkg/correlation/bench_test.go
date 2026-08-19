package correlation

// bench_test.go provides -count=5 benchmark suite for all major algorithms:
// BuildGraph, Condense, Localize, Decide (end-to-end), Issue/Verify credential operations.
// The benchmark harness uses 120-scenario corpus; results are per-algorithm across all runs.

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"
)

func BenchmarkCorrelateCascade(b *testing.B) {
	corpus := buildCorpus()
	for _, sc := range corpus {
		if sc.Class == "cascade" {
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, err := Correlate(sc.Alerts, sc.Topo, NewLagProfile(), DefaultParams())
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	}
}

func BenchmarkCorrelatePartition(b *testing.B) {
	corpus := buildCorpus()
	for _, sc := range corpus {
		if sc.Class == "partition" {
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, err := Correlate(sc.Alerts, sc.Topo, NewLagProfile(), DefaultParams())
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	}
}

func BenchmarkCorrelateSPOF(b *testing.B) {
	corpus := buildCorpus()
	for _, sc := range corpus {
		if sc.Class == "spof" {
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, err := Correlate(sc.Alerts, sc.Topo, NewLagProfile(), DefaultParams())
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	}
}

func BenchmarkBuildGraph(b *testing.B) {
	sc := buildConcurrent(0, 0)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := BuildGraph(sc.Alerts, sc.Topo, NewLagProfile(), DefaultParams())
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCondense(b *testing.B) {
	sc := buildSPOF(0, 0)
	g, _ := BuildGraph(sc.Alerts, sc.Topo, NewLagProfile(), DefaultParams())
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := g.Condense()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLocalize(b *testing.B) {
	sc := buildMixed(0, 0)
	g, _ := BuildGraph(sc.Alerts, sc.Topo, NewLagProfile(), DefaultParams())
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := Localize(g)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecide(b *testing.B) {
	sc := buildPartition(0, 0)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := Correlate(sc.Alerts, sc.Topo, NewLagProfile(), DefaultParams())
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCredentialIssue(b *testing.B) {
	sc := buildCascade(0, 0)
	dec, _ := Correlate(sc.Alerts, sc.Topo, NewLagProfile(), DefaultParams())
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		b.Fatal(err)
	}
	data, _ := CanonicalForm(dec)
	cred := NewCredential(dec, hex.EncodeToString(pub), time.Hour)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = cred.Issue(data, priv, time.Now().Add(-time.Minute), time.Now().Add(time.Hour))
	}
}

func BenchmarkCredentialVerify(b *testing.B) {
	sc := buildSPOF(0, 0)
	dec, _ := Correlate(sc.Alerts, sc.Topo, NewLagProfile(), DefaultParams())
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	data, _ := CanonicalForm(dec)
	cred := NewCredential(dec, hex.EncodeToString(pub), time.Hour)
	cred.IncidentID = "inc-spof"
	cred.Issue(data, priv, time.Now().Add(-time.Minute), time.Now().Add(time.Hour))
	signedData, _ := CanonicalForm(dec)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = cred.Verify(signedData, priv)
	}
}
