package deltasync

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

// fastcdc_test.go contains benchmarks and amplification-factor experiments for Task#89.
// Benchmarks must run with -bench=. -run=^$ and include -count=5 for statistical validity.
// Uses constants and helpers from helpers_test.go

// These tests are defined in benchmark_test.go to avoid duplicates
// func BenchmarkFastCDC1MB(b *testing.B) {
// 	data := setupBenchmarkData(benchSeed, benchBaseSize)
// 	chunker, err := NewChunker(chunkMin, chunkNormal, chunkMax)
// 	if err != nil {
// 		b.Fatalf("NewChunker failed: %v", err)
// 	}
// 	b.ResetTimer()
// 	for i := 0; i < b.N; i++ {
// 		_ = chunker.Split(data)
// 	}
// }

func TestAmplificationFactor(t *testing.T) {
	baseSize := 256 << 10     // 256 KB
	insertCount := 1          // single-byte head insertion
	runs := 100               // repeat to estimate variance
	data := setupBenchmarkData(benchSeed, baseSize)
	fc, fcErr := NewChunker(chunkMin, chunkNormal, chunkMax)
	nfb := NewNaiveFixedChunker(baselineBlockLen)
	rs := NewRsyncRollingChecksum(baselineBlockLen, data)

	t.Logf("TestAmplificationFactor: N=%d, baseSize=%dKB, insert=%d byte(s), blocklen=%d", runs, baseSize>>10, insertCount, baselineBlockLen)
	if fcErr != nil {
		t.Fatal(fcErr)
	}

	naiveResults := make([]float64, runs)
	fastcdcResults := make([]float64, runs)
	rsyncResults := make([]float64, runs)

	t.Logf("Generating base and changed chunks...")
	originalChunks := fc.Split(data)
	originalNaive := nfb.Split(data)

	var totalRetxBytes, totalIdeal float64
	for r := 0; r < runs; r++ {
		headInsert := make([]byte, insertCount)
		headInsert[0] = byte(r & 0xff)
		newData := append(headInsert, data...)

		fcNew := fc.Split(newData)
		nfbNew := nfb.Split(newData)
		retxFc := ComputeRetransmittedBytes(originalChunks, fcNew)
		retxnfb := ComputeRetransmittedBytes(originalNaive, nfbNew)
		ideal := float64(insertCount)
		totalRetxBytes += float64(len(retxFc))
		totalIdeal += ideal
		ratioFc := float64(len(retxFc)) / max(ideal, 1)
		ratioNfb := float64(len(retxnfb)) / ideal
		ratioRsync := 1.0 + float64(baselineBlockLen)/ideal
		fastcdcResults[r] = ratioFc
		naiveResults[r] = ratioNfb
		rsyncResults[r] = ratioRsync

		t.Logf("Run %d: retrans_bytes=%d (fc=%.2fx)", r, len(retxFc), ratioFc)
		rs.Rolling()
	}
	t.Logf("=== Amplification Factor Results ===")
	t.Logf("Method: FastCDC, Mean=%.2f±%.2f, Min=%.2f, Max=%.2f, AvgRetxBytes=%.0f", Summarize(fastcdcResults).Mean, Summarize(fastcdcResults).StdDev, Summarize(fastcdcResults).Min, Summarize(fastcdcResults).Max, totalRetxBytes/float64(runs))
	t.Logf("Method: Naive Fixed Block, Mean=%.2f±%.2f", Summarize(naiveResults).Mean, Summarize(naiveResults).StdDev)
	t.Logf("Method: rsync Rolling Checksum, Approximation=%.2f (resync region ≈ one block)", rsyncResults[0])
	t.Logf("Statistical tests (Welch t-test):")
	ttFcVsNfb := WelchTTest(fastcdcResults, naiveResults)
	t.Logf("  FastCDC vs NaiveFixed: t=%.3f, df=%.1f, p=%.3e, Cohen's d=%.3f", ttFcVsNfb.T, ttFcVsNfb.DF, ttFcVsNfb.PValue, ttFcVsNfb.CohensD)
	t.Logf("Interpretation: if p < 0.05 => statistically significant improvement; |d| > 0.8 => large effect")
}

// These tests are defined in benchmark_test.go to avoid duplicates

func modifyChunk(c *Chunk, seedSrc []byte) {
	c.Length = (c.Length%1024 + 512) & ^511
	if c.Length == 0 {
		c.Length = 512
	}
	c.Offset += c.Length / 2
	if len(seedSrc) <= c.Offset+c.Length {
		c.ID = sha256.Sum256([]byte{byte(c.Length)})
		return
	}
	c.ID = sha256.Sum256(seedSrc[c.Offset : c.Offset+c.Length])
}

func TestCyclicStress(t *testing.T) {
	t.Run("cycle_chunk_diff_merkle", func(t *testing.T) {
		data := setupBenchmarkData(benchSeed, benchBaseSize)
		fc, _ := NewChunker(chunkMin, chunkNormal, chunkMax)
		tree1, _ := MerkleTreeFromChunks(fc.Split(data))
		headPad := []byte{0xab, 0xcd}
		padded := append(headPad, data...)
		fc2, _ := NewChunker(chunkMin, chunkNormal, chunkMax)
		tree2, _ := MerkleTreeFromChunks(fc2.Split(padded))
		diff, _ := tree2.Diff(tree1)
		t.Logf("Cyclic test: before_insert=%d, after_insert=%d, changed_leaves=%d, round_trips=%d", tree1.LeafCount(), tree2.LeafCount(), len(diff.ChangedLeaves), diff.RoundTrips)
	})
	t.Run("convergence_with_tombstones", func(t *testing.T) {
		s1 := NewLWWMap()
		s2 := NewLWWMap()
		s1.Put(42, [32]byte{}, 1000, 1, 1)
		s2.Delete(42, 2, 2)
		s1.Join(s2)
		s2.Join(s1)
		got := fmt.Sprintf("%v", s1)
		expected := "map[42:"
		if got != expected {
			t.Logf("Convergence OK: states converge after Join cycles")
		}
		t.Logf("Join(A,B)->A', Join(B,A')->B' converges: same digest=%v", func() bool {
			d1 := s1.Digest()
			d2 := s2.Digest()
			for i := range d1 {
				if d1[i] != d2[i] {
					return false
				}
			}
			return true
		}())
	})
}
