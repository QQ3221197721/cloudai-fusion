package deltasync

import (
	"crypto/sha256"
	"os/exec"
)

// baselines.go implements the four comparison baselines required by Task#89:
//   1. Full transfer            - send the entire new file.
//   2. Naive fixed-block        - positional block compare (boundary-shift victim).
//   3. rsync rolling-checksum   - real weak(rolling)+strong two-tier delta encoder.
//   4. xdelta3                  - external byte-level delta (invoked if present).
// Naive full-state CRDT sync lives in crdt.go (NaiveCRDTFullState).

// FullTransfer is the trivial baseline: reconstruct dst by shipping all of it.
func FullTransfer(dst []byte) int64 { return int64(len(dst)) }

// NaiveFixedChunker splits data into equal-sized blocks with NO content-defined
// boundaries. An insertion anywhere shifts every subsequent boundary — the
// failure mode FastCDC is designed to defeat.
type NaiveFixedChunker struct {
	blockSize int
}

// NewNaiveFixedChunker builds a fixed-block splitter.
func NewNaiveFixedChunker(blockSize int) *NaiveFixedChunker {
	return &NaiveFixedChunker{blockSize: blockSize}
}

// Split cuts data into fixed-size blocks (last block may be short).
func (c *NaiveFixedChunker) Split(data []byte) []Chunk {
	if c.blockSize <= 0 {
		return nil
	}
	var chunks []Chunk
	for off := 0; off < len(data); off += c.blockSize {
		end := off + c.blockSize
		if end > len(data) {
			end = len(data)
		}
		chunks = append(chunks, Chunk{
			Offset: off,
			Length: end - off,
			ID:     sha256.Sum256(data[off:end]),
		})
	}
	return chunks
}

const rsyncMod = 1 << 16 // rsync weak-checksum modulus M

// weakChecksum computes rsync's rolling checksum a + b*M over buf.
func weakChecksum(buf []byte) (a, b, s uint32) {
	l := len(buf)
	for i := 0; i < l; i++ {
		a += uint32(buf[i])
		b += uint32(l-i) * uint32(buf[i])
	}
	a %= rsyncMod
	b %= rsyncMod
	return a, b, a + b*rsyncMod
}

// RsyncDelta runs the real rsync algorithm to reconstruct new from old and
// returns the number of LITERAL bytes that must be transmitted (the delta
// payload) plus the protocol round-trips.
//
// Protocol (2 round-trips): receiver hashes its fixed blocks and sends the
// (weak,strong) checksum list; sender rolls a window over new, and on a
// weak+strong match emits a COPY token (advancing one block) else emits one
// literal byte (advancing one byte). A head insertion costs ~1 literal byte
// then re-synchronizes — the whole point of the rolling checksum.
func RsyncDelta(old, newData []byte, blockSize int) (literalBytes int64, roundTrips int) {
	roundTrips = 2
	if blockSize <= 0 || len(old) == 0 {
		return int64(len(newData)), roundTrips
	}
	// Index old blocks by weak checksum -> strong hash.
	type entry struct{ strong [32]byte }
	index := make(map[uint32][]entry)
	for off := 0; off < len(old); off += blockSize {
		end := off + blockSize
		if end > len(old) {
			end = len(old)
		}
		if end-off < blockSize {
			break // only full blocks participate (rsync's convention)
		}
		_, _, s := weakChecksum(old[off:end])
		index[s] = append(index[s], entry{strong: sha256.Sum256(old[off:end])})
	}

	n := len(newData)
	if n < blockSize {
		return int64(n), roundTrips
	}
	// Rolling scan: maintain the weak checksum of window [i, i+blockSize)
	// incrementally so the whole pass is O(n) rather than O(n*blockSize).
	a, b, _ := weakChecksum(newData[0:blockSize])
	i := 0
	for i+blockSize <= n {
		s := a + b*rsyncMod
		matched := false
		if cands, ok := index[s]; ok {
			strong := sha256.Sum256(newData[i : i+blockSize])
			for _, e := range cands {
				if e.strong == strong {
					matched = true
					break
				}
			}
		}
		if matched {
			i += blockSize // COPY: reference existing block
			if i+blockSize <= n {
				a, b, _ = weakChecksum(newData[i : i+blockSize])
			}
		} else {
			literalBytes++ // one literal byte, then slide window by 1
			out := uint32(newData[i])
			if i+blockSize < n {
				in := uint32(newData[i+blockSize])
				a = (a - out + in) % rsyncMod
				b = (b - uint32(blockSize)*out + a) % rsyncMod
			} else {
				// last bytes before trailing remainder: just update with out
				a = (a - out) % rsyncMod
				b = (b - uint32(blockSize)*out) % rsyncMod
			}
			i++
		}
	}
	literalBytes += int64(n - i) // trailing bytes shorter than a block
	return literalBytes, roundTrips
}

// Xdelta3Available reports whether the xdelta3 binary can be invoked. Task#89
// requires honest reporting: if false, xdelta3 numbers are NOT produced.
func Xdelta3Available() bool {
	_, err := exec.LookPath("xdelta3")
	return err == nil
}
