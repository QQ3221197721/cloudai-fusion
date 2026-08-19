package deltasync

// ChangeMode describes how synthetic modifications are applied to a base file
// in the amplification experiments.
type ChangeMode string

const (
	HeadInsert    ChangeMode = "head_insert"
	TailAppend    ChangeMode = "tail_append"
	MiddleReplace ChangeMode = "middle_replace"
	RandomScatter ChangeMode = "random_scatter"
)

// SyncResult captures per-method measurements for a single change experiment.
type SyncResult struct {
	Method        string  `json:"method"`
	RetransBytes  int64   `json:"retrans_bytes"`  // bytes crossing the wire
	ChangedBytes  int64   `json:"changed_bytes"`  // actually-modified bytes (theoretical minimum)
	Amplification float64 `json:"amplification"`  // RetransBytes / ChangedBytes (1.0 = optimal)
	DedupRate     float64 `json:"dedup_rate"`     // fraction of dst chunks already present at src
	RoundTrips    int     `json:"round_trips"`    // protocol round-trips
	SrcBlocks     int     `json:"src_blocks"`
	DstBlocks     int     `json:"dst_blocks"`
}

// RetransmittedBytes returns the number of bytes that must actually cross the
// wire to reconstruct dst from src under content-addressed synchronization:
// the sum of lengths of dst chunks whose content ID is NOT already held by src.
// Chunks already present at src are referenced by COPY tokens (negligible cost).
func RetransmittedBytes(srcChunks, dstChunks []Chunk) int64 {
	srcSet := make(map[[32]byte]struct{}, len(srcChunks))
	for _, c := range srcChunks {
		srcSet[c.ID] = struct{}{}
	}
	var total int64
	for _, c := range dstChunks {
		if _, ok := srcSet[c.ID]; !ok {
			total += int64(c.Length)
		}
	}
	return total
}

// DedupRate returns the fraction of dst chunks whose content already exists at
// src (a content-addressed dedup hit). 1.0 means every dst chunk was reused.
func DedupRate(srcChunks, dstChunks []Chunk) float64 {
	if len(dstChunks) == 0 {
		return 0
	}
	srcSet := make(map[[32]byte]struct{}, len(srcChunks))
	for _, c := range srcChunks {
		srcSet[c.ID] = struct{}{}
	}
	hits := 0
	for _, c := range dstChunks {
		if _, ok := srcSet[c.ID]; ok {
			hits++
		}
	}
	return float64(hits) / float64(len(dstChunks))
}

// NaiveFixedRetransmittedBytes computes retransmitted bytes for the naive
// fixed-block scheme: it aligns blocks by POSITION (block i old vs block i new)
// and retransmits every block that differs. A head insertion shifts all content
// by one byte, so every positional block differs => full-file retransmission.
func NaiveFixedRetransmittedBytes(srcChunks, dstChunks []Chunk) int64 {
	var total int64
	n := len(dstChunks)
	for i := 0; i < n; i++ {
		if i >= len(srcChunks) || srcChunks[i].ID != dstChunks[i].ID {
			total += int64(dstChunks[i].Length)
		}
	}
	return total
}
