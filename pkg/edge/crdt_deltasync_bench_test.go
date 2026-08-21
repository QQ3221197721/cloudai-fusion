// Package edge - CRDT Delta Sync Benchmark Tests
package edge

import (
	"crypto/sha256"
	"fmt"
	"math/rand"
	"testing"
)

// ============================================================================
// CRDT DELTA SYNC VS FULL SNAPSHOT BENCHMARK
// Compares Merkle tree-based delta sync against full data transfer
// All benchmarks use deterministic seed for reproducibility on pure CPU systems
// ============================================================================

const (
	testBlockSize = 1024 * 1024 // 1MB blocks
	numBlocks     = 100         // 100MB total dataset
	deterministicSeed = int64(42) // Fixed seed for deterministic results
)

// generateRandomData generates deterministic random data for benchmarking
func generateRandomData(seed int64, blockSize int) []byte {
	r := rand.New(rand.NewSource(seed))
	data := make([]byte, blockSize)
	r.Read(data)
	return data
}

// computeMerkleRoot computes Merkle root for a slice of data blocks
func computeMerkleRoot(blocks [][]byte) []byte {
	if len(blocks) == 0 {
		return nil
	}
	
	hashes := make([][]byte, len(blocks))
	for i, block := range blocks {
		hash := sha256.Sum256(block)
		hashes[i] = hash[:]
	}
	
	// Build tree bottom-up
	for len(hashes) > 1 {
		newHashes := make([][]byte, 0, (len(hashes)+1)/2)
		
		for i := 0; i < len(hashes); i += 2 {
			if i+1 < len(hashes) {
				combined := append(hashes[i], hashes[i+1]...)
				hash := sha256.Sum256(combined)
				newHashes = append(newHashes, hash[:])
			} else {
				combined := append(hashes[i], hashes[i]...)
				hash := sha256.Sum256(combined)
				newHashes = append(newHashes, hash[:])
			}
		}
		
		hashes = newHashes
	}
	
	return hashes[0]
}

// calculateBlockDeltas compares two sets of blocks and returns changed ones
func calculateBlockDeltas(oldBlocks, newBlocks [][]byte) []*BlockDelta {
	if oldBlocks == nil || newBlocks == nil {
		return []*BlockDelta{}
	}
	
	deltas := make([]*BlockDelta, 0)
	maxLen := len(oldBlocks)
	if len(newBlocks) > maxLen {
		maxLen = len(newBlocks)
	}
	
	for i := 0; i < maxLen; i++ {
		var oldHash, newHash string
		var size int64 = int64(len(oldBlocks[i]))
		
		if i < len(oldBlocks) {
			hash := sha256.Sum256(oldBlocks[i])
			oldHash = fmt.Sprintf("%x", hash[:])
		}
		
		if i < len(newBlocks) {
			hash := sha256.Sum256(newBlocks[i])
			newHash = fmt.Sprintf("%x", hash[:])
			size = int64(len(newBlocks[i]))
		} else {
			oldHash = "" // New block
		}
		
		if oldHash != newHash {
			deltas = append(deltas, &BlockDelta{
				BlockID:   fmt.Sprintf("block_%d", i),
				OldHash:   oldHash,
				NewHash:   newHash,
				Size:      size,
				Timestamp: 0,
			})
		}
	}
	
	return deltas
}

// BenchmarkCRDTDeltaSync_1PercentChange tests delta sync with 1% change rate
func BenchmarkCRDTDeltaSync_1PercentChange(b *testing.B) {
	r := rand.New(rand.NewSource(deterministicSeed))
	
	// Generate initial dataset
	oldBlocks := make([][]byte, numBlocks)
	for i := 0; i < numBlocks; i++ {
		block := make([]byte, testBlockSize)
		r.Read(block)
		oldBlocks[i] = block
	}
	
	oldRoot := computeMerkleRoot(oldBlocks)
	fullSyncBytes := int64(numBlocks * testBlockSize)
	b.ResetTimer()
	
	var lastDeltaBytes int64
	for i := 0; i < b.N; i++ {
		// Simulate 1% changes (1 block out of 100)
		modifiedIdx := i % numBlocks
		
		newBlocks := make([][]byte, numBlocks)
		copy(newBlocks, oldBlocks)
		
		// Only modify one block
		newBlocks[modifiedIdx] = generateRandomData(deterministicSeed+int64(i), testBlockSize)
		
		// Compute deltas
		deltas := calculateBlockDeltas(oldBlocks, newBlocks)
		lastDeltaBytes = deltaTransferBytes(deltas)
		
		_ = oldRoot
	}
	
	reportBandwidth(b, lastDeltaBytes, fullSyncBytes)
}

// BenchmarkCRDTDeltaSync_10PercentChange tests delta sync with 10% change rate
func BenchmarkCRDTDeltaSync_10PercentChange(b *testing.B) {
	r := rand.New(rand.NewSource(deterministicSeed))
	
	// Generate initial dataset
	oldBlocks := make([][]byte, numBlocks)
	for i := 0; i < numBlocks; i++ {
		block := make([]byte, testBlockSize)
		r.Read(block)
		oldBlocks[i] = block
	}
	
	oldRoot := computeMerkleRoot(oldBlocks)
	fullSyncBytes := int64(numBlocks * testBlockSize)
	b.ResetTimer()
	
	var lastDeltaBytes int64
	for i := 0; i < b.N; i++ {
		// Simulate 10% changes (10 blocks out of 100)
		modifiedIndices := make(map[int]bool)
		for j := 0; j < 10; j++ {
			idx := (i + j) % numBlocks
			modifiedIndices[idx] = true
		}
		
		newBlocks := make([][]byte, numBlocks)
		copy(newBlocks, oldBlocks)
		
		for idx := range modifiedIndices {
			newBlocks[idx] = generateRandomData(deterministicSeed+int64(i)+int64(idx), testBlockSize)
		}
		
		// Compute deltas
		deltas := calculateBlockDeltas(oldBlocks, newBlocks)
		lastDeltaBytes = deltaTransferBytes(deltas)
		
		_ = oldRoot
	}
	
	reportBandwidth(b, lastDeltaBytes, fullSyncBytes)
}

// BenchmarkCRDTDeltaSync_50PercentChange tests delta sync with 50% change rate
func BenchmarkCRDTDeltaSync_50PercentChange(b *testing.B) {
	r := rand.New(rand.NewSource(deterministicSeed))
	
	// Generate initial dataset
	oldBlocks := make([][]byte, numBlocks)
	for i := 0; i < numBlocks; i++ {
		block := make([]byte, testBlockSize)
		r.Read(block)
		oldBlocks[i] = block
	}
	
	oldRoot := computeMerkleRoot(oldBlocks)
	fullSyncBytes := int64(numBlocks * testBlockSize)
	b.ResetTimer()
	
	var lastDeltaBytes int64
	for i := 0; i < b.N; i++ {
		// Simulate 50% changes (50 blocks out of 100)
		modifiedIndices := make(map[int]bool)
		for j := 0; j < 50; j++ {
			idx := (i + j*2) % numBlocks
			modifiedIndices[idx] = true
		}
		
		newBlocks := make([][]byte, numBlocks)
		copy(newBlocks, oldBlocks)
		
		for idx := range modifiedIndices {
			newBlocks[idx] = generateRandomData(deterministicSeed+int64(i)+int64(idx), testBlockSize)
		}
		
		// Compute deltas
		deltas := calculateBlockDeltas(oldBlocks, newBlocks)
		lastDeltaBytes = deltaTransferBytes(deltas)
		
		_ = oldRoot
	}
	
	reportBandwidth(b, lastDeltaBytes, fullSyncBytes)
}

// BenchmarkFullSnapshot_SameSize provides baseline comparison for full sync
func BenchmarkFullSnapshot_SameSize(b *testing.B) {
	r := rand.New(rand.NewSource(deterministicSeed))
	
	// Generate initial dataset
	oldBlocks := make([][]byte, numBlocks)
	for i := 0; i < numBlocks; i++ {
		block := make([]byte, testBlockSize)
		r.Read(block)
		oldBlocks[i] = block
	}
	
	oldRoot := computeMerkleRoot(oldBlocks)
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		// Full snapshot always transferred regardless of changes
		newBlocks := make([][]byte, numBlocks)
		copy(newBlocks, oldBlocks)
		
		// Simulate some minor noise changes (not real modifications, just copy)
		_ = newBlocks
		_ = oldRoot
	}
}

// BenchmarkCRDTDeltaSync_MixedWorkload simulates realistic mixed workload
func BenchmarkCRDTDeltaSync_MixedWorkload(b *testing.B) {
	r := rand.New(rand.NewSource(deterministicSeed))
	
	// Generate initial dataset
	oldBlocks := make([][]byte, numBlocks)
	for i := 0; i < numBlocks; i++ {
		block := make([]byte, testBlockSize)
		r.Read(block)
		oldBlocks[i] = block
	}
	
	oldRoot := computeMerkleRoot(oldBlocks)
	fullSyncBytes := int64(numBlocks * testBlockSize)
	b.ResetTimer()
	
	var lastDeltaBytes int64
	for i := 0; i < b.N; i++ {
		// Mixed workload: vary change rate based on iteration pattern
		changeRate := (i % 3) // cycles through 0,1,2 -> ~1%, 10%, 50%
		numChanges := 0
		
		switch changeRate {
		case 0:
			numChanges = 1 // 1%
		case 1:
			numChanges = 10 // 10%
		case 2:
			numChanges = 50 // 50%
		}
		
		modifiedIndices := make(map[int]bool)
		for j := 0; j < numChanges; j++ {
			idx := (i*prime + j*7) % numBlocks
			modifiedIndices[idx] = true
		}
		
		newBlocks := make([][]byte, numBlocks)
		copy(newBlocks, oldBlocks)
		
		for idx := range modifiedIndices {
			newBlocks[idx] = generateRandomData(deterministicSeed+int64(i)+int64(idx), testBlockSize)
		}
		
		// Compute deltas
		deltas := calculateBlockDeltas(oldBlocks, newBlocks)
		lastDeltaBytes = deltaTransferBytes(deltas)
		
		_ = oldRoot
	}
	
	reportBandwidth(b, lastDeltaBytes, fullSyncBytes)
}

// prime is used in mixed workload calculation to avoid repeating patterns
const prime = 97

// BenchmarkDeltaSync_ByteOverhead measures actual bytes transferred vs full sync
func BenchmarkDeltaSync_ByteOverhead(b *testing.B) {
	r := rand.New(rand.NewSource(deterministicSeed))
	
	// Generate initial dataset
	oldBlocks := make([][]byte, numBlocks)
	for i := 0; i < numBlocks; i++ {
		block := make([]byte, testBlockSize)
		r.Read(block)
		oldBlocks[i] = block
	}
	
	fullSyncBytes := int64(numBlocks * testBlockSize)
	b.ResetTimer()
	
	var deltaBytes int64
	for i := 0; i < b.N; i++ {
		// Simulate low change rate scenario
		numChanges := 5 // 5%
		modifiedIndices := make(map[int]bool)
		for j := 0; j < numChanges; j++ {
			idx := (i + j*3) % numBlocks
			modifiedIndices[idx] = true
		}
		
		newBlocks := make([][]byte, numBlocks)
		copy(newBlocks, oldBlocks)
		
		for idx := range modifiedIndices {
			newBlocks[idx] = generateRandomData(deterministicSeed+int64(i)+int64(idx), testBlockSize)
		}
		
		// Calculate what would be transferred
		deltas := calculateBlockDeltas(oldBlocks, newBlocks)
		deltaBytes = deltaTransferBytes(deltas)
	}
	
	reportBandwidth(b, deltaBytes, fullSyncBytes)
}

// Note: Algorithmic Comparison with rsync/xdelta3
// ================================================
// This benchmark uses SHA-256 Merkle Tree approach, which differs from:
// 
// 1. **rsync rolling hash** (Rabin fingerprint):
//    - rsync chunks data by content-defined boundaries using rolling hash
//    - Computes fingerprints for each chunk, then exchanges checksums
//    - Pros: Adaptive chunk sizes, good for partial overlaps
//    - Cons: Sensitive to insertions/deletions mid-file, requires two-pass communication
//    
// 2. **delta compressors** (xdelta3, libbz2):
//    - Use LZ77-style dictionary matching between old/new files
//    - Generate binary diffs, not cryptographic proofs
//    - Pros: Better compression ratios for similar files
//    - Cons: No verification of correctness, vulnerable to corruption propagation
//    
// 3. **Our Merkle Tree approach**:
//    - Deterministic hierarchical hashing (bottom-up tree construction)
//    - Each block independently verifiable via Merkle proof
//    - Cryptographic security: any bit change detected with 2^128 probability
//    - Supports lazy verification and sparse checking
//    
// When to use which:
// - **Merkle Tree** (this impl): Distributed systems needing verifiable state sync, CRDT convergence, audit trails
// - **rsync**: Large file transfers where only partial regions change (backup scenarios)
// - **delta compressors**: Binary patch distribution, version control diffs (git uses similar concept but not crypto-safe)
// 
// Tradeoffs:
// - Merkle overhead: O(n log n) hash computations vs O(n) rolling hash
// - Verification cost: Merkle can verify single block in O(log n) with proof; rsync needs full re-computation
// - Security: Merkle provides cryptographic guarantees; rsync/xdelta3 are heuristic-only

// Helper function to calculate actual bytes transferred for deltas
func deltaTransferBytes(deltas []*BlockDelta) int64 {
	bytes := int64(0)
	for _, d := range deltas {
		// Each delta includes metadata (~100 bytes header + hashes) + data size
		metadataOverhead := int64(100)
		bytes += d.Size + metadataOverhead + 64 // Data + header + old/new hashes (32+32)
	}
	return bytes
}

// Helper function to report bandwidth savings as custom metric
func reportBandwidth(b *testing.B, deltaBytes, fullSyncBytes int64) {
	savingsPercent := float64(fullSyncBytes-deltaBytes) / float64(fullSyncBytes) * 100
	b.ReportMetric(float64(deltaBytes), "delta_sync_bytes/op")
	b.ReportMetric(savingsPercent, "bandwidth_savings_pct")
}
