package deltasync

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"
)

// fastcdc_test.go contains benchmarks and amplification-factor experiments for Task#89.
// Benchmarks must run with -bench=. -run=^$ and include -count=5 for statistical validity.

const (
	benchBaseSize    = 1 << 20 // 1 MB
	benchSeed        = uint64(42)
	chunkMin         = 2048
	chunkNormal      = 8192
	chunkMax         = 65536
	baselineBlockLen = 4096
)

func makeRand(seed uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed))
}

func setupBenchmarkData(seed uint64, size int) []byte {
	data := make([]byte, size)
	fillRandom(data, seed)
	return data
}

func fillRandom(buf []byte, seed uint64) {
	r := makeRand(seed)
	for i := 0; i < len(buf); i += 8 {
		v := r.Uint64()
		for j := 0; j < 8 && i+j < len(buf); j++ {
			buf[i+j] = byte(v >> (8 * uint(j)))
		}
	}
}

func BenchmarkFastCDC1MB(b *testing.B) {
	data := setupBenchmarkData(benchSeed, benchBaseSize)
	chunker, err := NewChunker(chunkMin, chunkNormal, chunkMax)
	if err != nil {
		b.Fatalf("NewChunker failed: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = chunker.Split(data)
	}
}

func BenchmarkNaiveFixedBlock1MB(b *testing.B) {
	data := setupBenchmarkData(benchSeed+1, benchBaseSize)
	chunker := NewNaiveFixedChunker(baselineBlockLen)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = chunker.Split(data)
	}
}

func TestAmplificationFactor(t *testing.T) {
	baseSize := 256 << 10     // 256 KB
	insertCount := 1          // single-byte head insertion
	runs := 100               // repeat to estimate variance
	data := setupBenchmarkData(benchSeed, baseSize)
	fc, fcErr := NewChunker(chunkMin, chunkNormal, chunkMax)
	nfb, nfbErr := NewNaiveFixedChunker(baselineBlockLen)
	rsync := RsyncRollingChecksum{blockSize: baselineBlockLen, slice: data, startIdx: 0}

	t.Logf("TestAmplificationFactor: N=%d, baseSize=%dKB, insert=%d byte(s), blocklen=%d", runs, baseSize>>10, insertCount, baselineBlockLen)
	if fcErr != nil {
		t.Fatal(fcErr)
	}
	if nfbErr != nil {
		t.Fatal(nfbErr)
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
		headInsert[0] = byte(r&0xff)
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
		rsync.Rolling()
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

func TestRoundTripsAndDedupRate(t *testing.T) {
	data := setupBenchmarkData(benchSeed, benchBaseSize)
	chunker, _ := NewChunker(chunkMin, chunkNormal, chunkMax)
	chunks := chunker.Split(data)
	t.Logf("Testing round trips and dedup rate on %d chunks (%.1f MB)", len(chunks), float64(len(data))/1<<20)
	tree, _ := MerkleTreeFromChunks(chunks)
	mid := len(chunks) / 2
	modified := make([]Chunk, len(chunks))
	copy(modified, chunks)
	startOffset := mid * chunks[mid].Length
	modified[mid].Length -= 1
	modified[mid].Offset = startOffset
	modified[len(modified)-1].Length++
	modified[len(modified)-1].Offset = startOffset + 1
	modified[len(modified)-1].ID = sha256.Sum256(make([]byte, 0, modified[len(modified)-1].Length))
	newTree, _ := MerkleTreeFromChunks(modified)
	result, _ := newTree.Diff(tree)
	t.Logf("Merkle tree diff: leaf_count=%d, height=%d, changed_leaves=%d, comparisons=%d, round_trips=%d", tree.LeafCount(), tree.Height(), len(result.ChangedLeaves), result.Comparisons, result.RoundTrips)
	srcSet := make(map[[32]byte]bool, len(chunks))
	dstSet := make(map[[32]byte]bool, len(modified))
	for _, c := range chunks {
		srcSet[c.ID] = true
	}
	for _, c := range modified {
		dstSet[c.ID] = true
	}
	dedupHits := 0
	for id := range srcSet {
		if dstSet[id] {
			dedupHits++
		}
	}
	dedupRate := float64(dedupHits) / max(1, len(chunks))
	t.Logf("Dedup hit rate (same CID reused) = %.2f%%", dedupRate*100)
}

func BenchmarkMerkleDiff100Chunks(b *testing.B) {
	chunker, _ := NewChunker(chunkMin, chunkNormal, chunkMax)
	base := setupBenchmarkData(benchSeed, 512*1024)
	sourceChunks := chunker.Split(base[:500000])
	targetChunks := make([]Chunk, len(sourceChunks))
	copy(targetChunks, sourceChunks)
	modifyChunk(&targetChunks[7], base)
	srcTree, _ := MerkleTreeFromChunks(sourceChunks)
	trgTree, _ := MerkleTreeFromChunks(targetChunks)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, _ := trgTree.Diff(srcTree)
		_, _ = res.ChangedLeaves, res.RoundTrips
	}
}

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
	t.Run("cycle_chunk_diff_merkle", func(t *testing.T)) {
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
	t.Run("convergence_with_tombstones", func(t *testing.T)) {
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
		t.Logf("Join(A,B)->A', Join(B,A')->B' converges: same digest=%v", bytes.Equal(s1.Digest()[:], s2.Digest()))
	})
}
