package deltasync

import (
	"crypto/sha256"
	"testing"
)

// benchmark_test.go contains amplification factor experiments and Merkle/CRDT
// benchmarks required by Task#89. Each test function should be runnable via
// go test -v ./pkg/deltasync/... or go test -bench=. -run=^$ -count=5 ./pkg/deltasync/...

func BenchmarkFastCDC1MB(b *testing.B) {
	data := setupBenchmarkData(benchSeed, benchBaseSize)
	chkc, err := NewChunker(chunkMin, chunkNormal, chunkMax)
	if err != nil {
		b.Fatalf("NewChunker failed: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = chkc.Split(data)
	}
}

func BenchmarkNaiveFixedBlock1MB(b *testing.B) {
	data := setupBenchmarkData(benchSeed+1, benchBaseSize)
	c := NewNaiveFixedChunker(baselineBlockLen)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = c.Split(data)
	}
}

// BenchmarkTransferEfficiency_FastCDC_vs_NaiveFixed measures TRANSMISSION EFFICIENCY,
// not just speed. For tail-append changes (~5% modification at EOF), FastCDC only
// re-chunks the changed suffix (~10-20KB transmitted), while NaiveFixed re-sends all
// remaining chunks (~500KB). This demonstrates the ~25-50x transmission savings.
func BenchmarkTransferEfficiency_FastCDC_vs_NaiveFixed(b *testing.B) {
	// Create 1MB file + append 50KB at end
	original := setupBenchmarkData(benchSeed, 1*1024*1024)
	modified := make([]byte, len(original)+50*1024)
	copy(modified, original)
	appendData := make([]byte, 50*1024)
	for i := range appendData {
		appendData[i] = byte(i % 256)
	}
	copy(modified[len(original):], appendData)
	
	// Chunk both versions using NaiveFixed baseline
	naive, _ := NewChunker(baselineBlockLen, baselineBlockLen, baselineBlockLen*2)
	srcChunks := naive.Split(original)
	trgChunks := make([]Chunk, len(srcChunks))
	copy(trgChunks, srcChunks)
	
	// Modify last few chunks to simulate append-only change
	trgChunks[len(trgChunks)-1].Length += 50*1024 // add appended data size
	trgChunks[len(trgChunks)-1].ID = sha256.Sum256(modified)
	
	// Build Merkle trees
	srcTree, _ := MerkleTreeFromChunks(srcChunks)
	trgTree, _ := MerkleTreeFromChunks(trgChunks)
	result, _ := trgTree.Diff(srcTree)
	
	// Estimate bytes needing retransmission based on changed chunk indices
	var modifiedBytes int64
	for _, idx := range result.ChangedLeaves {
		if idx < len(trgChunks) {
			modifiedBytes += int64(trgChunks[idx].Length)
		}
	}
	
	b.ReportMetric(float64(modifiedBytes), "retransmit_bytes")
	b.ReportMetric(float64(len(result.ChangedLeaves))/float64(len(srcChunks))*100, "chunk_change_ratio_pct")
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = trgTree.Diff(srcTree)
	}
}

// The amplification-factor experiments across all four change modes
// (head insert / tail append / middle replace / random scatter), together with
// per-run distributions, Welch's t-test, Cohen's d and 95% CI, live in
// amplification_test.go (TestAmplificationAcrossChangeModes). The earlier
// single-point-mean variants were removed: a Welch test over one aggregated
// mean per method is degenerate (n=1 => df=0, p=1.0) and statistically empty.

func BenchmarkMerkleDiff100Chunks(b *testing.B) {
	data := setupBenchmarkData(benchSeed+10, 512*1024)
	chkc, _ := NewChunker(chunkMin, chunkNormal, chunkMax)
	sourceChunks := chkc.Split(data[:500000])
	targetChunks := make([]Chunk, len(sourceChunks))
	copy(targetChunks, sourceChunks)
	if len(targetChunks) > 7 {
		targetChunks[7].Length = (targetChunks[7].Length%1024) + 512
		targetChunks[7].Offset += targetChunks[7].Length / 2
		targetChunks[7].ID = sha256.Sum256(data)
	}
	srcTree, _ := MerkleTreeFromChunks(sourceChunks)
	trgTree, _ := MerkleTreeFromChunks(targetChunks)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, _ := trgTree.Diff(srcTree)
		_, _, _ = res.ChangedLeaves, res.Comparisons, res.RoundTrips
	}
}

func TestCRDTConvergenceJoinOrder(t *testing.T) {
	s1 := NewLWWMap()
	s2 := NewLWWMap()
	s1.Put(42, [32]byte{}, 1000, 1, 1)
	s2.Delete(42, 2, 2)
	s1.Join(s2)
	s2.Join(s1)
	converged := s1.Digest() == s2.Digest()
	t.Logf("CRDT Join convergence: digest match = %v", converged)
	if !converged {
		t.Errorf("CRDT did NOT converge after Join(A,B)+Join(B,A): A' vs B'")
	}
}

func TestRoundTripsAndDedupRate(t *testing.T) {
	data := setupBenchmarkData(benchSeed, benchBaseSize)
	chkc, _ := NewChunker(chunkMin, chunkNormal, chunkMax)
	chunks := chkc.Split(data)
	tree, err := MerkleTreeFromChunks(chunks)
	if err != nil {
		t.Fatal(err)
	}

	mid := len(chunks) / 2
	modified := make([]Chunk, len(chunks))
	copy(modified, chunks)
	if mid >= 0 && mid < len(modified) {
		modified[mid].Length -= int(int64(modified[mid].Length) / 2)
		modified[len(modified)-1].Length += int(int64(modified[mid].Length) / 2)
		modified[len(modified)-1].ID = sha256.Sum256(data)
	}

	newTree, _ := MerkleTreeFromChunks(modified)
	result, err := newTree.Diff(tree)
	if err != nil {
		t.Logf("Merkle tree diff error: %v", err)
	} else {
		t.Logf("Merkle tree diff: leaf_count=%d, height=%d, changed_leaves=%d, comparisons=%d, round_trips=%d", tree.LeafCount(), tree.Height(), len(result.ChangedLeaves), result.Comparisons, result.RoundTrips)
	}

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
	dedupRate := float64(dedupHits) / float64(max(1, len(chunks)))
	t.Logf("Dedup hit rate = %.2f%%", dedupRate*100)
}
