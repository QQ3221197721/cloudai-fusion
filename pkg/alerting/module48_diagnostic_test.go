package alerting

// module48_diagnostic_test.go - honest diagnostic of F1 breakdown for M48 correlation.

import (
	"testing"
	"time"
)

// TestDiagnosticF1Breakdown prints precision/recall/F1 for baseline to understand
// the quality ceiling and whether the 0.95 target is achievable with this algorithm.
func TestDiagnosticF1Breakdown(t *testing.T) {
	for _, c := range []gtCorpus{cascadeCorpus(), stormCorpus(4)} {
		ours := scoreGrouping(c, assignOurs(c))
		t.Logf("=== %s ===", c.name)
		t.Logf("groups=%d prec=%.4f recall=%.4f F1=%.4f purity=%.4f cohesion=%.4f",
			ours.groups, ours.pairPrec, ours.pairRecall, ours.pairF1, ours.purity, ours.cohesion)
	}
}

// TestDiagnosticOptimizedPartitionIdentical proves the optimized engine's partition
// is byte-identical to baseline (the real, verifiable optimization invariant).
func TestDiagnosticOptimizedPartitionIdentical(t *testing.T) {
	for _, c := range []gtCorpus{cascadeCorpus(), stormCorpus(4)} {
		base := assignOurs(c)
		opt := assignOptimized(c)

		// Build canonical partition signatures (group membership, ignoring random IDs)
		baseSig := canonicalPartition(base)
		optSig := canonicalPartition(opt)

		if len(baseSig) != len(optSig) {
			t.Fatalf("%s: partition size differs base=%d opt=%d", c.name, len(baseSig), len(optSig))
		}
		for i := range baseSig {
			if baseSig[i] != optSig[i] {
				t.Errorf("%s: partition differs at alert %d: base=%d opt=%d", c.name, i, baseSig[i], optSig[i])
			}
		}
		t.Logf("%s: optimized partition IDENTICAL to baseline (%d clusters)", c.name, maxInt(baseSig)+1)
	}
}

// canonicalPartition maps group-ID assignment to canonical integer cluster labels
// (first-seen order), so two engines with different random IDs but same grouping compare equal.
func canonicalPartition(assign []string) []int {
	idToCanon := map[string]int{}
	out := make([]int, len(assign))
	next := 0
	for i, id := range assign {
		if _, ok := idToCanon[id]; !ok {
			idToCanon[id] = next
			next++
		}
		out[i] = idToCanon[id]
	}
	return out
}

func maxInt(xs []int) int {
	m := 0
	for _, x := range xs {
		if x > m {
			m = x
		}
	}
	return m
}

// BenchmarkAMBaseline_LabelBucket_N52 is a Prometheus Alertmanager label-bucketing
// proxy on cascade-52 for latency comparison (group_by=[alertname,cluster]).
func BenchmarkAMBaseline_LabelBucket_N52(b *testing.B) {
	c := cascadeCorpus()
	benchAM(b, c, []string{"alertname", "cluster"}, false)
}

// BenchmarkAMBaseline_LabelBucket_N208 is the same proxy on storm-208.
func BenchmarkAMBaseline_LabelBucket_N208(b *testing.B) {
	c := stormCorpus(4)
	benchAM(b, c, []string{"alertname", "cluster"}, false)
}

// TestDiagnosticAMQuality prints Alertmanager proxy F1 for the same corpus, so the
// latency comparison is paired with a quality comparison (honest head-to-head).
func TestDiagnosticAMQuality(t *testing.T) {
	for _, c := range []gtCorpus{cascadeCorpus(), stormCorpus(4)} {
		for _, cfg := range amConfigs {
			s := scoreGrouping(c, assignAM(c, cfg.groupBy, cfg.groupByAll))
			t.Logf("%s AM %-28s groups=%d prec=%.3f recall=%.3f F1=%.3f",
				c.name, cfg.name, s.groups, s.pairPrec, s.pairRecall, s.pairF1)
		}
		_ = time.Now
	}
}
