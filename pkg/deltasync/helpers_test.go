package deltasync

import (
	"math/rand/v2"
)

// helpers_test.go holds shared test/benchmark helpers used by both the CRDT
// property test and the benchmark/experiment files.

const (
	benchBaseSize    = 1 << 20 // 1 MB
	benchSeed        = uint64(42)
	chunkMin         = 2048
	chunkNormal      = 8192
	chunkMax         = 65536
	baselineBlockLen = 4096

	testChunkMin      = 2048
	testChunkNormal   = 8192
	testChunkMax      = 65536
	testNReplicas     = 4
	testOpsPerReplica = 30
	testSeedRuns      = 8
)

func makeRand(seed uint64) *rand.Rand { return rand.New(rand.NewPCG(seed, seed*2+1)) }

// fillRandom deterministically fills buf from a seeded PCG source (math/rand/v2
// has no Rand.Read, so we splice Uint64 words).
func fillRandom(buf []byte, seed uint64) {
	r := makeRand(seed)
	for i := 0; i < len(buf); i += 8 {
		v := r.Uint64()
		for j := 0; j < 8 && i+j < len(buf); j++ {
			buf[i+j] = byte(v >> (8 * uint(j)))
		}
	}
}

func setupBenchmarkData(seed uint64, size int) []byte {
	data := make([]byte, size)
	fillRandom(data, seed)
	return data
}

// OperationType enumerates CRDT ops.
type OperationType int

const (
	PutOp OperationType = iota
	DeleteOp
)

// Op is a single CRDT operation carrying a FIXED version so that applying the
// same op set in any order yields the same lattice state.
type Op struct {
	Type      OperationType
	Idx       int
	Chunk     Chunk
	Version   uint64
	ReplicaID uint32
}

// generateRandomOps builds a deterministic op stream (seeded by op count) over
// the given chunks: a mix of PUTs (reusing chunk IDs) and DELETEs.
func generateRandomOps(chunks []Chunk, replicas, opsEach int) []Op {
	if len(chunks) == 0 {
		return nil
	}
	r := makeRand(uint64(replicas*1000 + opsEach))
	ops := make([]Op, 0, replicas*opsEach)
	for rep := 0; rep < replicas; rep++ {
		for i := 0; i < opsEach; i++ {
			idx := r.IntN(len(chunks))
			op := Op{
				Type:      PutOp,
				Idx:       idx,
				Chunk:     chunks[idx],
				Version:   r.Uint64()&^(uint64(0xffff)<<48) | uint64(1)<<48, // keep above init versions
				ReplicaID: uint32(rep),
			}
			if r.Float32() < 0.15 {
				op.Type = DeleteOp
			}
			ops = append(ops, op)
		}
	}
	return ops
}

// generateShuffledOrders returns numOrders distinct random permutations of
// [0, opsLen). Each permutation drives a different application order.
func generateShuffledOrders(numOrders, opsLen int) [][]int {
	r := makeRand(uint64(numOrders*7919 + opsLen))
	orders := make([][]int, numOrders)
	for i := 0; i < numOrders; i++ {
		perm := make([]int, opsLen)
		for j := range perm {
			perm[j] = j
		}
		for j := opsLen - 1; j > 0; j-- {
			k := r.IntN(j + 1)
			perm[j], perm[k] = perm[k], perm[j]
		}
		orders[i] = perm
	}
	return orders
}

// newLWWMapFromChunks seeds a replica with an initial identical baseline state.
// All replicas MUST start identically (seed fixed) or untouched indices would
// spuriously diverge; only the op stream introduces controlled divergence.
func newLWWMapFromChunks(chunks []Chunk, seed int) *LWWMap {
	m := NewLWWMap()
	for i, c := range chunks {
		m.Put(i, c.ID, c.Length, uint64(i&0xffff), 0) // identical across replicas
	}
	return m
}

// applyOpsInOrder applies ops[order[k]] in sequence. Because each op carries a
// fixed (Version, ReplicaID), the resulting lattice state is independent of the
// order — this is exactly what the convergence property test verifies.
func applyOpsInOrder(m *LWWMap, order []int, ops []Op) {
	for _, oi := range order {
		op := ops[oi]
		switch op.Type {
		case PutOp:
			m.Put(op.Idx, op.Chunk.ID, op.Chunk.Length, op.Version, op.ReplicaID)
		case DeleteOp:
			m.Delete(op.Idx, op.Version, op.ReplicaID)
		}
	}
}
