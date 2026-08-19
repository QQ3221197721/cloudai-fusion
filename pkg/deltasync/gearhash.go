// Package deltasync implements a content-defined chunking (FastCDC) + Merkle
// diff + block-level CRDT causal-merge synchronization pipeline.
//
// The package is a self-contained algorithmic moat aimed at defeating the
// insertion-amplification problem of fixed-block synchronization (rsync fixed
// blocks) while additionally providing content-addressed deduplication,
// O(log n) change localization via Merkle trees, and multi-writer convergence
// via a state-based CRDT (join-semilattice LWW map).
//
// This package is intentionally isolated: it does not import and is not
// imported by pkg/edge, pkg/observability, pkg/soc, pkg/scheduler, etc.
package deltasync

import (
	"math"
	"math/bits"
	"math/rand"
)

// gearTableSeed is fixed so the Gear hash table is reproducible across builds
// and processes. Two peers MUST share the same table to agree on chunk
// boundaries; pinning the seed guarantees that without shipping the table.
const gearTableSeed int64 = 0x5DEECE66D

// gearTable maps each byte value to a pseudo-random 64-bit word. The Gear hash
// rolls the fingerprint with fp = (fp << 1) + gearTable[b]. Because of the left
// shift, only the HIGH bits of fp accumulate a long content history; the low
// bits are biased (bit 0 is always 0 after a shift), which is exactly why the
// FastCDC judgement masks below select high-order bits.
var gearTable = buildGearTable(gearTableSeed)

func buildGearTable(seed int64) [256]uint64 {
	var t [256]uint64
	r := rand.New(rand.NewSource(seed))
	for i := range t {
		t[i] = r.Uint64()
	}
	return t
}

// spreadMask returns a 64-bit mask with exactly nbits set bits placed in the
// high region [lo, hi] of the word and spread out (rather than contiguous) to
// reduce autocorrelation between neighbouring judged positions. For random
// input the masked bits are approximately uniform and independent, so
//
//	P((fp & mask) == 0) = 2^-popcount(mask) = 2^-nbits
//
// which is the identity the expected-chunk-size derivation relies on. The
// placement only affects the quality of that approximation, never the mean.
func spreadMask(nbits int) uint64 {
	if nbits <= 0 {
		return 0
	}
	const lo, hi = 33, 62 // 30 candidate high-order positions
	const span = hi - lo  // 29
	if nbits > span+1 {
		nbits = span + 1
	}
	var m uint64
	last := -1
	for i := 0; i < nbits; i++ {
		var pos int
		if nbits == 1 {
			pos = (lo + hi) / 2
		} else {
			pos = lo + int(math.Round(float64(i)*float64(span)/float64(nbits-1)))
		}
		if pos <= last { // guarantee strictly increasing / distinct bits
			pos = last + 1
		}
		if pos > 63 {
			pos = 63
		}
		m |= uint64(1) << uint(pos)
		last = pos
	}
	return m
}

// maskProbability returns 2^-popcount(mask), the per-byte probability that the
// mask judgement fires on random input.
func maskProbability(mask uint64) float64 {
	return math.Ldexp(1, -bits.OnesCount64(mask))
}

// bitCountForTarget returns the number of judgement mask bits whose expected
// geometric run length 2^bits is closest to the desired average chunk size,
// i.e. base = round(log2(target)). With a b-bit mask, the per-byte cut
// probability is 2^-b, so the geometric mean run length is 2^b; picking
// b = round(log2(target)) centres the (un-normalized) mean at `target`. The
// value is clamped to [1, 30] to stay within the high-bit window used by
// spreadMask.
func bitCountForTarget(target int) int {
	if target <= 1 {
		return 1
	}
	b := int(math.Round(math.Log2(float64(target))))
	if b < 1 {
		b = 1
	}
	if b > 30 {
		b = 30
	}
	return b
}
