package deltasync

import (
	"crypto/sha256"
	"math"
)

// Chunk is one content-defined chunk produced by FastCDC. It is content
// addressed: ID = SHA-256(content). Offset/Length locate it within the source.
type Chunk struct {
	Offset int      `json:"offset"`
	Length int      `json:"length"`
	ID     [32]byte `json:"id"` // SHA-256 content hash (content address)
}

// Chunker implements FastCDC with normalized chunking. It is safe for
// concurrent use because it holds only immutable parameters; per-call state
// lives on the stack.
type Chunker struct {
	min    int
	normal int
	max    int

	maskS uint64 // region 1 mask: MORE set bits => smaller cut prob => resists early cuts
	maskL uint64 // region 2 mask: FEWER set bits => larger cut prob => forces timely cuts

	pS float64 // per-byte cut probability in region 1 = 2^-popcount(maskS)
	pL float64 // per-byte cut probability in region 2 = 2^-popcount(maskL)
}

// NewChunker builds a FastCDC chunker for the given size bounds. The
// normalization level is fixed at NC=2 bits (maskS = base+2 bits, maskL =
// base-2 bits) which is the value recommended by Xia et al. (FastCDC, USENIX
// ATC'16) as the best chunking-speed / dedup trade-off.
func NewChunker(min, normal, max int) (*Chunker, error) {
	if min <= 0 || normal <= 0 || max <= 0 || min > normal || normal > max {
		return nil, ErrInvalidChunkSize
	}
	const nc = 2
	base := bitCountForTarget(normal)
	bitsS := base + nc
	bitsL := base - nc
	if bitsL < 1 {
		bitsL = 1
	}
	c := &Chunker{
		min:    min,
		normal: normal,
		max:    max,
		maskS:  spreadMask(bitsS),
		maskL:  spreadMask(bitsL),
	}
	c.pS = maskProbability(c.maskS)
	c.pL = maskProbability(c.maskL)
	return c, nil
}

// nextCut returns the length of the next chunk starting at data[0], applying
// normalized chunking:
//
//	i in [min, normal): judge with maskS (hard to cut => grows chunk toward normal)
//	i in [normal, max): judge with maskL (easy to cut  => prevents overshoot)
//	i == max:           forced cut
//
// The rolling Gear fingerprint fp = (fp<<1)+gear[b] uses high-order bits only.
func (c *Chunker) nextCut(data []byte) int {
	n := len(data)
	if n <= c.min {
		return n
	}
	limit := c.max
	if n < limit {
		limit = n
	}
	normal := c.normal
	if normal > n {
		normal = n
	}

	var fp uint64
	i := c.min
	// Region 1: [min, normal) with the strict mask.
	for ; i < normal; i++ {
		fp = (fp << 1) + gearTable[data[i]]
		if fp&c.maskS == 0 {
			return i
		}
	}
	// Region 2: [normal, limit) with the relaxed mask.
	for ; i < limit; i++ {
		fp = (fp << 1) + gearTable[data[i]]
		if fp&c.maskL == 0 {
			return i
		}
	}
	return limit
}

// Split scans data once and returns its content-defined chunks in order. The
// concatenation of chunk byte ranges reconstructs data exactly.
func (c *Chunker) Split(data []byte) []Chunk {
	var chunks []Chunk
	off := 0
	for off < len(data) {
		l := c.nextCut(data[off:])
		if l <= 0 {
			l = len(data) - off
		}
		chunks = append(chunks, Chunk{
			Offset: off,
			Length: l,
			ID:     sha256.Sum256(data[off : off+l]),
		})
		off += l
	}
	return chunks
}

// ExpectedChunkSize returns the exact expected chunk length for the two-region
// normalized model under uniform-random content, derived by conditional
// expectation over two truncated geometric cut processes:
//
//	R1 = normal-min, R2 = max-normal, qS = 1-pS, qL = 1-pL
//	E[len] = min
//	       + (1 - qS^R1)/pS                      // expected bytes scanned in region 1
//	       + qS^R1 * (1 - qL^R2)/pL              // survive region 1, then region 2
//
// Region 1 rarely cuts (small pS) so most chunks reach `normal` and then cut
// quickly in region 2 (large pL), concentrating the length distribution near
// `normal` — the core benefit of normalized chunking over classic CDC.
func (c *Chunker) ExpectedChunkSize() float64 {
	r1 := float64(c.normal - c.min)
	r2 := float64(c.max - c.normal)
	qS := 1 - c.pS
	qL := 1 - c.pL
	term1 := (1 - math.Pow(qS, r1)) / c.pS
	term2 := math.Pow(qS, r1) * (1 - math.Pow(qL, r2)) / c.pL
	return float64(c.min) + term1 + term2
}

// Params exposes the resolved parameters (for docs/tests/reporting).
func (c *Chunker) Params() (min, normal, max int, pS, pL float64) {
	return c.min, c.normal, c.max, c.pS, c.pL
}
