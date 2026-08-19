package quantile

// tdigest.go implements a merging t-digest (Dunning & Ertl) with the k1 scale
// function k(q) = (δ/2π)·arcsin(2q−1). Incoming points are buffered and, on
// flush, merged with the existing centroids: two adjacent centroids coalesce
// while their combined span satisfies k(q_hi) − k(q_lo) ≤ 1. Because arcsin has
// steep slope near q=0 and q=1, centroids stay small in the tails and large in
// the body — t-digest's signature strength. It is still bounded memory (~δ
// centroids) with eps rank error; it is not exact.

import (
	"math"
	"sort"
	"strconv"
)

// TDigest is a centroid-based quantile estimator (merging variant).
type TDigest struct {
	compression float64 // δ: larger => more centroids, finer accuracy
	centroids   []centroid
	buffer      []float64
	bufCap      int
	totalWeight float64
}

type centroid struct {
	mean   float64
	weight float64
}

// NewTDigest creates a t-digest with the given compression δ (typical 100–200).
func NewTDigest(compression float64) *TDigest {
	if compression < 20 {
		compression = 20
	}
	bufCap := int(compression) * 10
	if bufCap < 128 {
		bufCap = 128
	}
	return &TDigest{
		compression: compression,
		buffer:      make([]float64, 0, bufCap),
		bufCap:      bufCap,
	}
}

// Name implements Sketch.
func (t *TDigest) Name() string {
	return "t-digest(delta=" + strconv.FormatFloat(t.compression, 'f', 0, 64) + ")"
}

// Count implements Sketch.
func (t *TDigest) Count() int { return int(math.Round(t.totalWeight)) }

// Add buffers x and flushes into centroids when the buffer fills.
func (t *TDigest) Add(x float64) {
	if math.IsNaN(x) {
		return
	}
	t.buffer = append(t.buffer, x)
	t.totalWeight++
	if len(t.buffer) >= t.bufCap {
		t.flush()
	}
}

// kScale is the k1 scale function; its inverse spacing concentrates resolution
// in the tails.
func (t *TDigest) kScale(q float64) float64 {
	if q < 0 {
		q = 0
	}
	if q > 1 {
		q = 1
	}
	return t.compression / (2 * math.Pi) * math.Asin(2*q-1)
}

// flush merges the buffered raw points with existing centroids under the k1
// size limit, rebuilding the centroid list.
func (t *TDigest) flush() {
	if len(t.buffer) == 0 {
		return
	}
	all := make([]centroid, 0, len(t.centroids)+len(t.buffer))
	all = append(all, t.centroids...)
	for _, x := range t.buffer {
		all = append(all, centroid{mean: x, weight: 1})
	}
	t.buffer = t.buffer[:0]

	sort.Slice(all, func(i, j int) bool { return all[i].mean < all[j].mean })

	var total float64
	for _, c := range all {
		total += c.weight
	}
	if total == 0 {
		return
	}

	merged := make([]centroid, 0, len(all))
	merged = append(merged, all[0])
	wSoFar := 0.0 // committed weight before the current last centroid

	for i := 1; i < len(all); i++ {
		c := all[i]
		last := &merged[len(merged)-1]
		proposed := last.weight + c.weight
		q0 := wSoFar / total
		q2 := (wSoFar + proposed) / total
		if t.kScale(q2)-t.kScale(q0) <= 1 {
			// Merge c into last (weighted mean update).
			last.mean += (c.mean - last.mean) * c.weight / proposed
			last.weight = proposed
		} else {
			wSoFar += last.weight
			merged = append(merged, c)
		}
	}

	t.centroids = merged
	t.totalWeight = total
}

// Quantile returns the nearest-rank q-quantile by walking the centroid CDF.
// Each centroid is treated as covering [cumBefore, cumBefore+weight) in rank
// space and centred at its mean; we return the mean of the centroid whose rank
// interval brackets the target rank.
func (t *TDigest) Quantile(q float64) float64 {
	t.flush()
	if len(t.centroids) == 0 {
		return math.NaN()
	}
	if q <= 0 {
		return t.centroids[0].mean
	}
	if q >= 1 {
		return t.centroids[len(t.centroids)-1].mean
	}
	target := math.Ceil(q * t.totalWeight)
	if target < 1 {
		target = 1
	}
	var cum float64
	for _, c := range t.centroids {
		cum += c.weight
		if cum >= target {
			return c.mean
		}
	}
	return t.centroids[len(t.centroids)-1].mean
}

// SizeBytes reports centroid payload plus the flush buffer.
func (t *TDigest) SizeBytes() int {
	const centroidBytes = 16 // mean(8) + weight(8)
	return cap(t.centroids)*centroidBytes + cap(t.buffer)*8
}
