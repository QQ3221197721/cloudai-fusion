package deltasync

import (
	"testing"
)

// amplification_test.go runs the full amplification-factor study across ALL FOUR
// change modes (head insert / tail append / middle replace / random scatter).
//
// Statistical correctness fix (vs the earlier degenerate design): every mode is
// repeated ampRuns (>=100) times, and — crucially — each run uses an INDEPENDENT
// random base file. This is what produces genuine cross-run variance: because the
// Gear-hash retains only ~last 64 bytes in its high bits and the minimum chunk is
// 2 KiB, prepending a byte to a FIXED file yields a byte-identical chunk length
// every time (zero variance -> Welch df=0, p=1). Regenerating the base file per
// run makes the per-run retransmission a real random variable, so Welch's t-test
// has df >> 0 and a meaningful p-value.
//
// For each mode we collect a >=100-length per-run array for FastCDC and for the
// NaiveFixedBlock baseline, then compute Welch's unequal-variance t-test, Cohen's
// d, and 95% Student-t confidence intervals. rsync rolling-checksum, full transfer,
// and naive full-state CRDT baselines are measured per run and aggregated into the
// amplification table.

const (
	ampBaseSize    = 256 << 10 // 256 KiB base file
	ampBaseSeed    = uint64(42)
	ampBaselineLen = 4096 // fixed-block / rsync block length
	ampRuns        = 120  // >=100 runs per mode
	ampScatter     = 32   // number of scattered small edits in random-scatter mode
	ampScatterSpan = 64   // bytes touched per scatter edit
)

// changeSample is one experiment: an independent random base file and its
// modified copy, plus the theoretical-minimum number of changed bytes.
type changeSample struct {
	base     []byte
	modified []byte
	changed  int64
}

// makeSample builds an independent random base file (seeded by run) and applies
// the requested change mode to a copy of it.
func makeSample(mode ChangeMode, run int) changeSample {
	switch mode {
	case HeadInsert:
		base := newRandData(ampBaseSeed+uint64(run), ampBaseSize)
		mod := make([]byte, 0, len(base)+1)
		hdr := make([]byte, 1)
		fillRandom(hdr, uint64(run)*2654435761+1)
		mod = append(mod, hdr...)
		mod = append(mod, base...)
		return changeSample{base: base, modified: mod, changed: 1}

	case TailAppend:
		const appendLen = 1024
		base := newRandData(ampBaseSeed+uint64(run)*7+11, ampBaseSize)
		mod := make([]byte, len(base)+appendLen)
		copy(mod, base)
		fillRandom(mod[len(base):], uint64(run)*13+3)
		return changeSample{base: base, modified: mod, changed: appendLen}

	case MiddleReplace:
		const replaceLen = 1024
		base := newRandData(ampBaseSeed+uint64(run)*17+23, ampBaseSize)
		mod := make([]byte, len(base))
		copy(mod, base)
		// in-place replacement of a segment somewhere in the central half,
		// offset varying per run for real variance (no length change -> no shift)
		r := makeRand(uint64(run)*40503 + 29)
		lo := len(base) / 4
		hi := len(base) * 3 / 4
		off := lo + r.IntN(hi-lo)
		if off+replaceLen > len(base) {
			off = len(base) - replaceLen
		}
		repl := make([]byte, replaceLen)
		fillRandom(repl, uint64(run)*915488749+3)
		copy(mod[off:off+replaceLen], repl)
		return changeSample{base: base, modified: mod, changed: replaceLen}

	case RandomScatter:
		base := newRandData(ampBaseSeed+uint64(run)*19+31, ampBaseSize)
		mod := make([]byte, len(base))
		copy(mod, base)
		r := makeRand(uint64(run)*2246822519 + 37)
		positions := make(map[int]bool, ampScatter)
		var changed int64
		for len(positions) < ampScatter {
			pos := r.IntN(len(mod) - ampScatterSpan)
			if positions[pos] {
				continue
			}
			positions[pos] = true
			seg := make([]byte, ampScatterSpan)
			fillRandom(seg, uint64(run)*131+uint64(pos))
			copy(mod[pos:pos+ampScatterSpan], seg)
			changed += ampScatterSpan
		}
		return changeSample{base: base, modified: mod, changed: changed}
	}
	// default: no change
	return changeSample{base: newRandData(ampBaseSeed, ampBaseSize), modified: newRandData(ampBaseSeed, ampBaseSize), changed: 1}
}

func newRandData(seed uint64, size int) []byte {
	buf := make([]byte, size)
	fillRandom(buf, seed)
	return buf
}

// modeResult aggregates per-run measurements for a single change mode.
type modeResult struct {
	fcAmp    []float64 // FastCDC amplification per run
	nfbAmp   []float64 // NaiveFixedBlock amplification per run
	rsyncAmp []float64 // rsync literal-byte amplification per run
	fullAmp  []float64 // full-transfer amplification per run
	crdtAmp  []float64 // naive full-state CRDT amplification per run

	fcBytes    []float64 // FastCDC retransmitted bytes per run
	nfbBytes   []float64 // NaiveFixedBlock retransmitted bytes per run
	dedup      []float64 // FastCDC dedup rate (%) per run
	merkleRT   []float64 // Merkle round-trips per run (only shape-matching runs)
	rsyncRT    int       // rsync protocol round-trips (constant)
	shapeMatch int       // number of runs where old/new FastCDC chunk counts matched
}

func runMode(mode ChangeMode) modeResult {
	fc, _ := NewChunker(chunkMin, chunkNormal, chunkMax)
	nfb := NewNaiveFixedChunker(ampBaselineLen)
	var res modeResult
	res.rsyncRT = 2
	for run := 0; run < ampRuns; run++ {
		s := makeSample(mode, run)
		cf := float64(s.changed)

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

		res.rsyncRT = rsyncRT
		res.fcAmp = append(res.fcAmp, float64(fcRetrans)/cf)
		res.nfbAmp = append(res.nfbAmp, float64(nfbRetrans)/cf)
		res.rsyncAmp = append(res.rsyncAmp, float64(rsyncLit)/cf)
		res.fullAmp = append(res.fullAmp, float64(fullBytes)/cf)
		res.crdtAmp = append(res.crdtAmp, float64(crdtBytes)/cf)
		res.fcBytes = append(res.fcBytes, float64(fcRetrans))
		res.nfbBytes = append(res.nfbBytes, float64(nfbRetrans))
		res.dedup = append(res.dedup, DedupRate(origFC, newFC)*100)

		// Merkle round-trips only defined when the two trees share a shape.
		if len(origFC) == len(newFC) {
			if a, err := MerkleTreeFromChunks(origFC); err == nil {
				if b, err := MerkleTreeFromChunks(newFC); err == nil {
					if d, err := b.Diff(a); err == nil {
						res.merkleRT = append(res.merkleRT, float64(d.RoundTrips))
						res.shapeMatch++
					}
				}
			}
		}
	}
	return res
}

func TestAmplificationAcrossChangeModes(t *testing.T) {
	t.Logf("=== Amplification Factor Study — 4 Change Modes ===")
	t.Logf("base=%dKiB, FastCDC(min=%d,normal=%d,max=%d), fixed-block=%dB, runs/mode=%d (independent random file per run)",
		ampBaseSize>>10, chunkMin, chunkNormal, chunkMax, ampBaselineLen, ampRuns)

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
		mode                             string
		fcAmp, nfbAmp, rsAmp, fuAmp, crAmp float64
		welch                            TTestResult
		dedup                            float64
	}
	var rows []row

	for _, m := range modes {
		res := runMode(m.mode)

		fcS := Summarize(res.fcAmp)
		nfbS := Summarize(res.nfbAmp)
		rsS := Summarize(res.rsyncAmp)
		fuS := Summarize(res.fullAmp)
		crS := Summarize(res.crdtAmp)
		dedupS := Summarize(res.dedup)

		welch := WelchTTest(res.fcAmp, res.nfbAmp)
		fcLo, fcHi, fcMar := ConfidenceInterval95(res.fcAmp)
		nfbLo, nfbHi, nfbMar := ConfidenceInterval95(res.nfbAmp)

		t.Logf("")
		t.Logf("================ Mode: %s (%s) ================", string(m.mode), m.desc)
		t.Logf("N=%d runs. Amplification = retransmitted_bytes / changed_bytes (1.0 = optimal).", ampRuns)
		t.Logf("%-22s | %10s | %9s | %11s | %11s", "Method", "MeanAmp", "StdDev", "Min", "Max")
		t.Logf("%-22s | %10.2f | %9.2f | %11.2f | %11.2f", "FastCDC (ours)", fcS.Mean, fcS.StdDev, fcS.Min, fcS.Max)
		t.Logf("%-22s | %10.2f | %9.2f | %11.2f | %11.2f", "NaiveFixedBlock", nfbS.Mean, nfbS.StdDev, nfbS.Min, nfbS.Max)
		t.Logf("%-22s | %10.2f | %9.2f | %11.2f | %11.2f", "rsync rolling-cksum", rsS.Mean, rsS.StdDev, rsS.Min, rsS.Max)
		t.Logf("%-22s | %10.2f | %9.2f | %11.2f | %11.2f", "FullTransfer", fuS.Mean, fuS.StdDev, fuS.Min, fuS.Max)
		t.Logf("%-22s | %10.2f | %9.2f | %11.2f | %11.2f", "NaiveCRDT full-state", crS.Mean, crS.StdDev, crS.Min, crS.Max)
		t.Logf("FastCDC retransmitted bytes: mean=%.1f B (min=%.0f max=%.0f)", Summarize(res.fcBytes).Mean, Summarize(res.fcBytes).Min, Summarize(res.fcBytes).Max)
		t.Logf("FastCDC dedup rate: mean=%.2f%% (min=%.2f%% max=%.2f%%)", dedupS.Mean, dedupS.Min, dedupS.Max)
		t.Logf("Round-trips: FullTransfer=1, NaiveFixed/rsync=%d, NaiveCRDT=1 (broadcast).", res.rsyncRT)
		if res.shapeMatch > 0 {
			t.Logf("Merkle round-trips (FastCDC, shape-stable runs=%d): mean=%.2f", res.shapeMatch, Summarize(res.merkleRT).Mean)
		} else {
			t.Logf("Merkle round-trips: chunk count changed every run (shape shift), Merkle diff N/A this mode.")
		}
		t.Logf("--- Welch two-sided t-test: FastCDC vs NaiveFixedBlock ---")
		t.Logf("t=%.4f, df=%.2f, p=%.4e, Cohen's d=%.4f", welch.T, welch.DF, welch.PValue, welch.CohensD)
		t.Logf("95%% CI FastCDC amp: [%.2f, %.2f] (±%.2f)", fcLo, fcHi, fcMar)
		t.Logf("95%% CI NaiveFixed amp: [%.2f, %.2f] (±%.2f)", nfbLo, nfbHi, nfbMar)

		if welch.DF <= 0 {
			t.Errorf("[%s] Welch df=%.2f <= 0: degenerate zero-variance samples — statistic is empty", m.mode, welch.DF)
		}

		rows = append(rows, row{
			mode:  string(m.mode),
			fcAmp: fcS.Mean, nfbAmp: nfbS.Mean, rsAmp: rsS.Mean, fuAmp: fuS.Mean, crAmp: crS.Mean,
			welch: welch, dedup: dedupS.Mean,
		})
	}

	t.Logf("")
	t.Logf("================ Cross-Mode Amplification Summary ================")
	t.Logf("%-16s | %9s | %9s | %9s | %9s | %9s | %8s | %10s", "Mode", "FastCDC", "NaiveFix", "rsync", "FullXfer", "NaiveCRDT", "df", "p-value")
	for _, r := range rows {
		t.Logf("%-16s | %9.2f | %9.2f | %9.2f | %9.2f | %9.2f | %8.1f | %10.2e",
			r.mode, r.fcAmp, r.nfbAmp, r.rsAmp, r.fuAmp, r.crAmp, r.welch.DF, r.welch.PValue)
	}
	t.Logf("")
	t.Logf("Where FastCDC does NOT lead: any mode above where FastCDC amp exceeds a baseline amp.")
}
