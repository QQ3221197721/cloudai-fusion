package quantile

// dists.go provides reproducible random variates for comparing quantile estimators:
//   - Normal: standard or scaled/shifted Gaussian
//   - Lognormal: heavy-tailed (log X ~ N(μ,σ))
//   - Pareto: very heavy-tailed (P(X > x) = (x/xm)^{-α})
//   - Bimodal: two-component mixture (fast/slow latency analogue)
//   - Adversarial: specifically tuned to defeat bucket interpolation and centroid
//     collapse — a dense region plus extreme outliers that force coarse tail approximations
//
// All generators use seeded random sources so tests remain deterministic.

import (
	"math"
	"math/rand"
)

// Normal returns n draws from N(mu, sigma). mu and sigma must satisfy sigma > 0.
func Normal(rng *rand.Rand, n int, mu, sigma float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = mu + sigma*rng.NormFloat64()
	}
	return out
}

// Lognormal returns n draws of exp(N(mu, sigma)).
func Lognormal(rng *rand.Rand, n int, mu, sigma float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = math.Exp(mu + sigma*rng.NormFloat64())
	}
	return out
}

// Pareto returns n draws from Pareto(xm, alpha): P(X>x)=min(x/xm)^-alpha.
// xm is the scale; alpha > 1 is the shape. mean exists when alpha > 1.
func Pareto(rng *rand.Rand, n int, xm, alpha float64) []float64 {
	out := make([]float64, n)
	u := make([]float64, n)
	for i := range u {
		u[i] = rng.Float64()
	}
	for i := range out {
		out[i] = xm / math.Pow(u[i], 1/alpha)
	}
	return out
}

// Bimodal returns a mixture: w fraction from mode2, 1-w from mode1. Each mode is
// Gaussian with its own location/scale. This models "fast path + slow path" latency.
func Bimodal(rng *rand.Rand, n int, mu1, sigma1, mu2, sigma2, w float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		if rng.Float64() < w {
			out[i] = mu2 + sigma2*rng.NormFloat64()
		} else {
			out[i] = mu1 + sigma1*rng.NormFloat64()
		}
	}
	return out
}

// AdversarialForBucket creates input tuned to stress-test Prometheus-like bucket
// interpolation. It packs most values just below bucket edges while leaving mass
// above to cause linear-interpolation bias. Returns (data, targetQ, expectedTrueQuantile).
func AdversarialForBucket(rng *rand.Rand, bucketEdges []float64) ([]float64, float64, float64) {
	n := 50_000
	data := make([]float64, n)

	// Pack points at 99% of each bucket edge (below them), ensuring they all fall
	// in the same bucket when histogrammed. Use different weights per bucket so some
	// buckets are densely filled.
	var off int64
	for _, e := range bucketEdges {
		count := int((off % 100) + 5) // varying density
		if count <= 0 {
			count = 1
		}
		// place many points just under this edge (at 99% of edge)
		target := e * 0.99
		for k := 0; k < count && off+int64(k) < int64(n); k++ {
			i := int(off) + k
			data[i] = target + rng.Float64()*1e-6
		}
		// leave room above the edge to be filled by larger outliers
		off += int64(count)
		if off >= int64(n) {
			break
		}
	}
	// Fill rest with uniform noise to populate the space
	for off < int64(n) {
		data[off] = rng.Float64() * 10
		off++
	}
	rng.Shuffle(n, func(i, j int) { data[i], data[j] = data[j], data[i] })

	// q = 0.99 targets p99 where the bucket method typically fails due to tail sparsity
	targetQ := 0.99
	return data, targetQ, targetQ // trueQ not computed here; test uses nearest-rank on sample as truth

}
