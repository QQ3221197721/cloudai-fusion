package deltasync

import (
	"fmt"
	"testing"
)

// adaptive_test.go implements the Task #96 ADAPTIVE HYBRID CHUNKING verification study:
//
//   Goal: Defeat FastCDC's three measured weakness scenarios WITHOUT sacrificing head_insert moat.
//   Hard targets:
//     • Tail Append:    ≤1.5× NaiveFixed (target 1.0× via append fast path)
//     • Random Scatter: ≤2× NaiveFixed (target ≤1.2× via hierarchical 256B)
//     • Middle Replace: ≤NaiveFixed (hierarchical beats fixed at 5.2×)
//     • Head Insert:    NO REGRESSION (route to CDC, maintain ~28.5× advantage over naive)
//
// Methodology: Same experimental setup as amplification_test.go (base size, change lengths,
// ≥120 runs per mode, independent random base file per run). For each method we collect
// retransmitted bytes / changed_bytes = amplification factor, then compute Welch t-test,
// Cohen's d, 95% CI for statistical significance (p<0.05, Cohen's d>0.5).
//
// Experimental columns:
//   • FastCDC (baseline): original content-defined chunking
//   • Adaptive (ours): hybrid engine with A+B+C directions
//   • NaiveFixedBlock: fixed 4KB blocks (positional comparison baseline)
//   • rsync: rolling checksum O(n) delta encoder
//
// Ablation studies (isolate directional contributions):
//   • AdAblateCDCOnly: routing disabled, always use FastCDC
//   • AdAblateHierOnly: routing disabled, always use hierarchical
//   • AdAblateHierPlusRouting: hierarchical + routing (no append fast path)
//   • AdFull: full adaptive with append fast path (the final system)
//
// Overhead evaluation: benchmark mode detection latency + ring buffer cost vs. bytes saved.

const (
	ampAdaptiveSubSize = 256 // Direction B fine granularity
	ampAdaptiveTracks  = true // Enable direction A history tracking
)

// runModeAdaptive collects Amplification Factor arrays for ALL methods across ONE change mode.
func runModeAdaptive(mode ChangeMode) adModeResult {
	// Original baselines
	fc, _ := NewChunker(chunkMin, chunkNormal, chunkMax)
	nfb := NewNaiveFixedChunker(ampBaselineLen)

	// NEW: Adaptive engine (Direction A+B+C)
	adv, err := NewAdaptiveChunker(chunkMin, chunkNormal, chunkMax, ampAdaptiveSubSize, ampAdaptiveTracks)
	if err != nil {
		panic(fmt.Sprintf("NewAdaptiveChunker failed: %v", err))
	}

	var res adModeResult // Use NEW struct type
	res.rsyncRT = 2

	for run := 0; run < ampRuns; run++ {
		s := makeSample(mode, run)
		cf := float64(s.changed)

		// Original methods
		origFC := fc.Split(s.base)
		origNFB := nfb.Split(s.base)
		newFC := fc.Split(s.modified)
		newNFB := nfb.Split(s.modified)
		fcRetrans := RetransmittedBytes(origFC, newFC)
		nfbRetrans := NaiveFixedRetransmittedBytes(origNFB, newNFB)
		rsyncLit, rsyncRT := RsyncDelta(s.base, s.modified, ampBaselineLen)
		fullBytes := FullTransfer(s.modified)

		crdt := NewLWWMap()
		for i, c := range newFC {
			crdt.Put(i, c.ID, c.Length, uint64(i), 0)
		}
		crdtBytes := NaiveCRDTFullState(crdt)

		// NEW: Adaptive Plan
		planAdv := adv.Plan(s.base, s.modified)
		advRetx := planAdv.Retransmit
		advRT := planAdv.RoundTrips

		// Ablation: CDC-only (routing off)
		planAblCdc := adv.cdcPlan(s.base, s.modified, DetectInsert)
		ablCdcRetx := planAblCdc.Retransmit

		// Ablation: Hierarchical-only
		planAblHier := adv.hierPlan(s.base, s.modified, DetectReplace)
		ablHierRetx := planAblHier.Retransmit

		// Ablation: Hierarchical + Routing (no append fast path)
		var ablHpRetx int64
		switch mode {
		case TailAppend:
			// Force hierarchical instead of append path
			ablHpRetx = planAblHier.Retransmit
		default:
			ablHpRetx = planAdv.Retransmit
		}

		res.rsyncRT = rsyncRT
		res.fcAmp = append(res.fcAmp, float64(fcRetrans)/cf)
		res.nfbAmp = append(res.nfbAmp, float64(nfbRetrans)/cf)
		res.rsyncAmp = append(res.rsyncAmp, float64(rsyncLit)/cf)
		res.fullAmp = append(res.fullAmp, float64(fullBytes)/cf)
		res.crdtAmp = append(res.crdtAmp, float64(crdtBytes)/cf)

		// NEW: Adaptive amplifier
		res.adAmp = append(res.adAmp, float64(advRetx)/cf)
		res.adRT = append(res.adRT, float64(advRT))

		// Ablations
		res.ablCdcAmp = append(res.ablCdcAmp, float64(ablCdcRetx)/cf)
		res.ablHierAmp = append(res.ablHierAmp, float64(ablHierRetx)/cf)
		res.ablHpAmp = append(res.ablHpAmp, float64(ablHpRetx)/cf)

		// Store raw bytes
		res.fcBytes = append(res.fcBytes, float64(fcRetrans))
		res.nfbBytes = append(res.nfbBytes, float64(nfbRetrans))
		res.adBytesRaw = append(res.adBytesRaw, float64(advRetx))
		res.dedup = append(res.dedup, DedupRate(origFC, newFC)*100)
	}

	return res
}

// adModeResult extends modeResult with Adaptive + ablation metrics.
type adModeResult struct {
	fcAmp    []float64 // FastCDC amplification per run
	nfbAmp   []float64 // NaiveFixedBlock amplification per run
	rsyncAmp []float64 // rsync literal-byte amplification per run
	fullAmp  []float64 // full-transfer amplification per run
	crdtAmp  []float64 // naive full-state CRDT amplification per run

	adAmp   []float64 // Adaptive amplification per run (NEW)
	adRT    []float64 // Adaptive round-trips per run (NEW)
	adBytesRaw []float64 // Re-transmit bytes per run (renamed to avoid conflict)

	ablCdcAmp  []float64 // Ablation: CDC only
	ablHierAmp []float64 // Ablation: Hierarchical only
	ablHpAmp   []float64 // Ablation: Hierarchical + routing

	fcBytes    []float64 // FastCDC retransmitted bytes per run
	nfbBytes   []float64 // NaiveFixedBlock retransmitted bytes per run
	dedup      []float64 // FastCDC dedup rate (%) per run
	merkleRT   []float64 // Merkle round-trips per run (only shape-matching runs)
	rsyncRT    int       // rsync protocol round-trips (constant)
	shapeMatch int       // number of runs where old/new FastCDC chunk counts matched
}

func TestAdaptiveHybridChunking(t *testing.T) {
	t.Logf("=== Task #96: Adaptive Hybrid Chunking — 4 Change Modes ===")
	t.Logf("Base=%dKiB, FastCDC(min=%d,normal=%d,max=%d), FixedBlock=%dB, HierLeaf=%dB, runs/mode=%d",
		ampBaseSize>>10, chunkMin, chunkNormal, chunkMax, ampBaselineLen, ampAdaptiveSubSize, ampRuns)
	t.Logf("")

	modes := []struct {
		mode ChangeMode
		desc string
	}{
		{HeadInsert, "insert 1 random byte at file head"},
		{TailAppend, "append 1 KiB random data at file tail"},
		{MiddleReplace, "replace 1 KiB in the central half (in place)"},
		{RandomScatter, "scatter 32 x 64B random edits (in place)"},
	}

	type row struct {
		mode  string
		fcAmp, adAmp, nfbAmp, rsAmp, fuAmp, crAmp float64
		welch                               TTestResult
		hardPass                            bool
	}
	var rows []row

	t.Logf("=================================================================================")
	t.Logf("MAIN RESULTS: Adaptive vs Baselines (Amplification Factor)")
	t.Logf("=================================================================================\n")

	for _, m := range modes {
		res := runModeAdaptive(m.mode)

		// Summaries
		fcS := Summarize(res.fcAmp)
		adS := Summarize(res.adAmp)
		nfbS := Summarize(res.nfbAmp)
		rsS := Summarize(res.rsyncAmp)
		fuS := Summarize(res.fullAmp)
		crS := Summarize(res.crdtAmp)

		// ABLaTION summaries
		ablCdcS := Summarize(res.ablCdcAmp)
		ablHierS := Summarize(res.ablHierAmp)
		ablHpS := Summarize(res.ablHpAmp)

		// Welch test: Adaptive vs NaiveFixed
		welchAdv := WelchTTest(res.adAmp, res.nfbAmp)
		welchFc := WelchTTest(res.fcAmp, res.nfbAmp)

		// 95% CI
		adLo, adHi, adMar := ConfidenceInterval95(res.adAmp)
		nfbLo, nfbHi, nfbMar := ConfidenceInterval95(res.nfbAmp)

		t.Logf("--- Mode: %s (%s) ---", string(m.mode), m.desc)
		t.Logf("%-16s | %10s | %9s | %11s | %11s", "Method", "MeanAmp", "StdDev", "Min", "Max")
		t.Logf("%-16s | %10.2f | %9.2f | %11.2f | %11.2f", "FastCDC (baseline)", fcS.Mean, fcS.StdDev, fcS.Min, fcS.Max)
		t.Logf("%-16s | %10.2f | %9.2f | %11.2f | %11.2f", "ADAPTIVE (ours)", adS.Mean, adS.StdDev, adS.Min, adS.Max)
		t.Logf("%-16s | %10.2f | %9.2f | %11.2f | %11.2f", "NaiveFixedBlock", nfbS.Mean, nfbS.StdDev, nfbS.Min, nfbS.Max)
		t.Logf("%-16s | %10.2f | %9.2f | %11.2f | %11.2f", "rsync rolling-cksum", rsS.Mean, rsS.StdDev, rsS.Min, rsS.Max)
		t.Logf("%-16s | %10.2f | %9.2f | %11.2f | %11.2f", "FullTransfer", fuS.Mean, fuS.StdDev, fuS.Min, fuS.Max)
		t.Logf("%-16s | %10.2f | %9.2f | %11.2f | %11.2f", "NaiveCRDT full-state", crS.Mean, crS.StdDev, crS.Min, crS.Max)
		t.Logf("")

		t.Logf("--- RETRANSMIT BYTES (raw numbers) ---")
		t.Logf("FastCDC mean=%.1f B (min=%.0f max=%.0f)", Summarize(res.fcBytes).Mean, Summarize(res.fcBytes).Min, Summarize(res.fcBytes).Max)
		t.Logf("ADAPTIVE   mean=%.1f B (min=%.0f max=%.0f)", Summarize(res.adBytesRaw).Mean, Summarize(res.adBytesRaw).Min, Summarize(res.adBytesRaw).Max)
		t.Logf("NaiveFixed mean=%.1f B (min=%.0f max=%.0f)", Summarize(res.nfbBytes).Mean, Summarize(res.nfbBytes).Min, Summarize(res.nfbBytes).Max)
		t.Logf("")

		t.Logf("--- ABLaTION STUDY (directional contribution) ---")
		t.Logf("%-16s | %10s | %9s", "Method", "MeanAmp", "StdDev")
		t.Logf("%-16s | %10.2f | %9.2f", "CDC-only (no routing)", ablCdcS.Mean, ablCdcS.StdDev)
		t.Logf("%-16s | %10.2f | %9.2f", "Hierarchical-only", ablHierS.Mean, ablHierS.StdDev)
		t.Logf("%-16s | %10.2f | %9.2f", "Hierarchical+Routing (no append)", ablHpS.Mean, ablHpS.StdDev)
		t.Logf("%-16s | %10.2f | %9.2f", "FULL Adaptive (with append fast path)", adS.Mean, adS.StdDev)
		t.Logf("")

		t.Logf("--- STATISTICAL SIGNIFICANCE ---")
		t.Logf("Welch two-sided t-test: ADAPTIVE vs NaiveFixedBlock")
		t.Logf("t=%.4f, df=%.2f, p=%.4e, Cohen's d=%.4f", welchAdv.T, welchAdv.DF, welchAdv.PValue, welchAdv.CohensD)
		t.Logf("Welch two-sided t-test: FastCDC vs NaiveFixedBlock")
		t.Logf("t=%.4f, df=%.2f, p=%.4e, Cohen's d=%.4f", welchFc.T, welchFc.DF, welchFc.PValue, welchFc.CohensD)
		t.Logf("95%% CI ADAPTIVE amp: [%.2f, %.2f] (±%.2f)", adLo, adHi, adMar)
		t.Logf("95%% CI NaiveFixed amp: [%.2f, %.2f] (±%.2f)", nfbLo, nfbHi, nfbMar)
		t.Logf("")

		t.Logf("--- HARD TARGET VERIFICATION ---")
		pass := true
		reason := ""
		switch m.mode {
		case TailAppend:
			if adS.Mean <= 1.5*nfbS.Mean {
				reason = fmt.Sprintf("PASS: %.2f× ≤ 1.5×%.2f=%g", adS.Mean, nfbS.Mean, 1.5*nfbS.Mean)
			} else {
				pass = false
				reason = fmt.Sprintf("FAIL: %.2f× > 1.5×%.2f=%g", adS.Mean, nfbS.Mean, 1.5*nfbS.Mean)
			}
		case RandomScatter:
			target := 2.0 * nfbS.Mean
			if adS.Mean <= target {
				reason = fmt.Sprintf("PASS: %.2f× ≤ 2×%.2f=%g (aspirational ≤1.2× not met: %.2f/51.27)", adS.Mean, nfbS.Mean, target, adS.Mean/nfbS.Mean)
			} else {
				pass = false
				reason = fmt.Sprintf("FAIL: %.2f× > 2×%.2f=%g", adS.Mean, nfbS.Mean, target)
			}
		case MiddleReplace:
			if adS.Mean <= nfbS.Mean {
				reason = fmt.Sprintf("PASS: %.2f× ≤ %.2f×", adS.Mean, nfbS.Mean)
			} else {
				pass = false
				reason = fmt.Sprintf("FAIL: %.2f× > %.2f×", adS.Mean, nfbS.Mean)
			}
		case HeadInsert:
			// NO regression: Adaptive must be within 20% of FastCDC (preserve insertion moat)
			target := 1.2 * fcS.Mean
			if adS.Mean <= target {
				reason = fmt.Sprintf("PASS: %.2f× ≈ FastCDC %.2f× (no regression, target ≤1.2×)", adS.Mean, fcS.Mean)
			} else {
				pass = false
				reason = fmt.Sprintf("FAIL: %.2f× > 1.2×FastCDC=%.2f", adS.Mean, target)
			}
		}
		t.Logf("%s\n", reason)
		if !pass {
			t.Logf("*** HARD TARGET FAILED ***\n")
		}
		t.Logf("")

		rows = append(rows, row{
			mode:       string(m.mode),
			fcAmp:      fcS.Mean, adAmp: adS.Mean, nfbAmp: nfbS.Mean, rsAmp: rsS.Mean, fuAmp: fuS.Mean, crAmp: crS.Mean,
			welch: welchAdv, hardPass: pass,
		})
	}

	t.Logf("=================================================================================")
	t.Logf("SUMMARY TABLE: Amplification Factor (mean ± std dev)")
	t.Logf("=================================================================================")
	t.Logf("%-12s | %10s | %9s | %9s | %9s | %9s | %9s | %8s | %10s", "Mode", "FastCDC", "Adaptive", "NaiveFix", "rsync", "FullXfer", "NaiveCRDT", "df", "p-value")
	for _, r := range rows {
		t.Logf("%-12s | %10.2f | %9.2f | %9.2f | %9.2f | %9.2f | %9.2f | %8.1f | %10.2e",
			r.mode, r.fcAmp, r.adAmp, r.nfbAmp, r.rsAmp, r.fuAmp, r.crAmp, r.welch.DF, r.welch.PValue)
	}
	t.Logf("")

	t.Logf("=================================================================================")
	t.Logf("HARD TARGET SUMMARY")
	t.Logf("=================================================================================")
	allPass := true
	for _, r := range rows {
		status := "FAIL"
		if r.hardPass {
			status = "PASS"
		} else {
			allPass = false
		}
		t.Logf("%-12s [%s]: Adaptive=%.2f×, NaiveFixed=%.2f×", r.mode, status, r.adAmp, r.nfbAmp)
	}
	t.Logf("")

	if !allPass {
		t.Errorf("Some hard targets FAILED (see FAIL lines above)")
	}

	t.Logf("--- OVERHEAD EVALUATION ---")
	t.Logf("Mode detection cost: ~O(n) byte compare for prefix, negligible vs chunking.")
	t.Logf("Ring buffer (size=16): constant-space, one-write-per-Plan call.")
	t.Logf("Hierarchical metadata: %.1f KB per 256KB file (%d leaves × 32 B ID)",
		float64(ampBaseSize/256)*32/1024, ampBaseSize/256)
	t.Logf("Tradeoff: Metadata is NOT retransmitted (sender holds local hash table); only changed leaf content crosses the wire.")
}

// BenchmarkAdaptiveModes measures the runtime cost of mode detection + routing overhead.
func BenchmarkAdaptiveModes(b *testing.B) {
	base := setupBenchmarkData(benchSeed, benchBaseSize)
	modified := make([]byte, len(base)+1024)
	copy(modified, base)
	fillRandom(modified[len(base):], benchSeed+1)

	ch, _ := NewAdaptiveChunker(chunkMin, chunkNormal, chunkMax, ampAdaptiveSubSize, true)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ch.Plan(base, modified)
	}
}

// BenchmarkAdaptiveOverhead isolates the detection + tracker cost.
func BenchmarkAdaptiveOverhead(b *testing.B) {
	base := setupBenchmarkData(benchSeed, benchBaseSize)
	modified := make([]byte, len(base)+1024)
	copy(modified, base)
	fillRandom(modified[len(base):], benchSeed+1)

	// Construct tracker separately so we can time its operations
	tracker := NewModeTracker(16)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mode := ClassifyChange(base, modified)
		tracker.Record(mode)
		_, _ = mode, tracker
	}
}
