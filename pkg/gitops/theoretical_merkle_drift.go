package gitops

// theoretical_merkle_drift.go — Task #267 (M39 GitOps T3 MoAT).
//
// This file is NEW, additive, and never referenced by the production drift path
// (manager.go / drift_detector.go). It exists to make the *Merkle path
// compression optimality theorem* executable and falsifiable: it models cluster
// drift detection over a Helm chart's versioned history as a diff between two
// content-addressed Merkle trees, and instruments the exact quantities the
// theorem bounds — hash comparisons, pruned subtrees, and level-synchronous
// round trips — so the adversarial tests in theoretical_merkle_drift_test.go can
// measure them against a naive O(n) full re-scan baseline.
//
// The production scanner (DiffStates in drift_detector.go) is a map-based full
// diff: it is honest and correct, but it reads every field of every resource on
// every scan — Θ(n). This file proves what a Merkle-structured detector buys,
// and — crucially — is explicit about the amortization assumption that makes the
// advantage real (the tree is built once at Helm-release commit time, then each
// incremental drift check reuses cached digests).
//
// Design goal: identical-shape trees. Both the desired (Git) snapshot and the
// live (cluster) snapshot are flattened into config leaves keyed by
// "<resourceKey>\x00<field>". We build both trees over the *sorted union* of leaf
// keys; a leaf absent from a snapshot gets a sentinel "absent" hash. This yields
// two trees of identical shape where an unchanged leaf hashes equal in both —
// the precondition for single-comparison subtree pruning.

import (
	"crypto/sha256"
	"sort"
)

// ============================================================================
// Config-leaf model
// ============================================================================

// leafDigest is the 32-byte content hash of one (resourceKey, field, value) cell.
type leafDigest = [32]byte

// absentLeaf is the sentinel digest for a leaf key that is present in one
// snapshot but not the other (whole-resource add/remove, or a field appearing/
// disappearing). Domain-separated with prefix 0x02.
func absentLeafHash() leafDigest {
	return sha256.Sum256([]byte{0x02, 'A', 'B', 'S', 'E', 'N', 'T'})
}

// leafHash hashes a present config cell. Prefix 0x00 domain-separates leaves
// from internal nodes and from the absent sentinel.
func leafHash(resourceKey, field, value string) leafDigest {
	h := sha256.New()
	h.Write([]byte{0x00})
	h.Write([]byte(resourceKey))
	h.Write([]byte{0}) // NUL separator
	h.Write([]byte(field))
	h.Write([]byte{0})
	h.Write([]byte(value))
	var out leafDigest
	copy(out[:], h.Sum(nil))
	return out
}

// internalHash combines two child digests. Prefix 0x01 domain-separates internal
// nodes so an internal digest can never be passed off as a leaf (second-preimage
// hardening, matching pkg/deltasync/merkle.go's convention).
func internalNodeHash(l, r leafDigest) leafDigest {
	buf := make([]byte, 1+32+32)
	buf[0] = 0x01
	copy(buf[1:], l[:])
	copy(buf[33:], r[:])
	return sha256.Sum256(buf)
}

// ============================================================================
// Snapshot flattening
// ============================================================================

// flattenSnapshot turns a resource list into a map of config leaves keyed by
// "<Kind/Namespace/Name>\x00<field>". Whole-resource presence is captured by a
// synthetic "*" field so add/remove of an entire resource surfaces as a changed
// leaf just like a field change does.
func flattenSnapshot(states []ResourceState) map[string]string {
	leaves := make(map[string]string)
	for _, r := range states {
		rk := r.key()
		leaves[rk+"\x00*"] = "present"
		for f, v := range r.Fields {
			leaves[rk+"\x00"+f] = v
		}
	}
	return leaves
}

// ============================================================================
// DriftMerkleTree — content-addressed tree over sorted config leaves
// ============================================================================

// DriftMerkleTree is a binary hash tree over an ordered leaf-key vector. Two
// trees built from the same ordered key vector share shape, so their Diff can
// prune identical subtrees with a single comparison per pruned root.
type DriftMerkleTree struct {
	keys   []string     // sorted leaf keys (shared shape across compared trees)
	levels [][]leafDigest // levels[0] = leaves; last level = single root
}

// LeafCount returns the number of leaves (n).
func (t *DriftMerkleTree) LeafCount() int {
	if len(t.levels) == 0 {
		return 0
	}
	return len(t.levels[0])
}

// Height returns the number of levels above the leaves (root-to-leaf edges).
func (t *DriftMerkleTree) Height() int {
	if len(t.levels) == 0 {
		return 0
	}
	return len(t.levels) - 1
}

// Root returns the Merkle root digest (zero digest for an empty tree).
func (t *DriftMerkleTree) Root() leafDigest {
	if len(t.levels) == 0 {
		return leafDigest{}
	}
	top := t.levels[len(t.levels)-1]
	return top[0]
}

// buildLevels constructs the internal levels bottom-up from a leaf vector.
// Unpaired nodes are promoted unchanged (no duplication) to avoid the
// duplicate-leaf second-preimage ambiguity.
func buildLevels(leaves []leafDigest) [][]leafDigest {
	if len(leaves) == 0 {
		return nil
	}
	level := make([]leafDigest, len(leaves))
	copy(level, leaves)
	levels := [][]leafDigest{level}
	for len(level) > 1 {
		next := make([]leafDigest, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			if i+1 < len(level) {
				next = append(next, internalNodeHash(level[i], level[i+1]))
			} else {
				next = append(next, level[i]) // promote unpaired node
			}
		}
		levels = append(levels, next)
		level = next
	}
	return levels
}

// BuildDriftMerklePair builds two identical-shape Merkle trees over the sorted
// union of leaf keys drawn from the desired and live snapshots. Leaves present
// in only one snapshot use the absent sentinel on the other side, so any
// add/remove/modify manifests as a leaf-hash inequality at a known index.
//
// Returned trueChanged is the ground-truth number of differing leaves (k),
// computed directly from the flattened maps — the tests assert the Merkle diff
// recovers exactly this set, guarding against a pruning bug silently dropping
// changes.
func BuildDriftMerklePair(desired, live []ResourceState) (dt, lt *DriftMerkleTree, trueChanged int) {
	dmap := flattenSnapshot(desired)
	lmap := flattenSnapshot(live)

	keySet := make(map[string]struct{}, len(dmap)+len(lmap))
	for k := range dmap {
		keySet[k] = struct{}{}
	}
	for k := range lmap {
		keySet[k] = struct{}{}
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	absent := absentLeafHash()
	dLeaves := make([]leafDigest, len(keys))
	lLeaves := make([]leafDigest, len(keys))
	for i, k := range keys {
		if v, ok := dmap[k]; ok {
			// Split "<resourceKey>\x00<field>" back for domain-separated hashing.
			rk, field := splitLeafKey(k)
			dLeaves[i] = leafHash(rk, field, v)
		} else {
			dLeaves[i] = absent
		}
		if v, ok := lmap[k]; ok {
			rk, field := splitLeafKey(k)
			lLeaves[i] = leafHash(rk, field, v)
		} else {
			lLeaves[i] = absent
		}
		if dLeaves[i] != lLeaves[i] {
			trueChanged++
		}
	}

	dt = &DriftMerkleTree{keys: keys, levels: buildLevels(dLeaves)}
	lt = &DriftMerkleTree{keys: keys, levels: buildLevels(lLeaves)}
	return dt, lt, trueChanged
}

func splitLeafKey(k string) (resourceKey, field string) {
	for i := 0; i < len(k); i++ {
		if k[i] == 0 {
			return k[:i], k[i+1:]
		}
	}
	return k, ""
}

// ============================================================================
// Instrumented diff results
// ============================================================================

// MerkleDriftResult reports one Merkle-pruned diff, instrumented with exactly the
// quantities the optimality theorem bounds.
type MerkleDriftResult struct {
	ChangedKeys []string `json:"changed_keys"` // leaf keys that differ (the localized L)
	Comparisons int      `json:"comparisons"`  // node-hash equality checks performed
	RoundTrips  int      `json:"round_trips"`  // level-synchronous network rounds (O(log n))
	NodesPruned int      `json:"nodes_pruned"` // identical subtrees skipped in one comparison
	LeafCount   int      `json:"leaf_count"`   // n
	Height      int      `json:"height"`       // log2 n (rounded up)
}

// FullDiffResult reports the naive O(n) baseline: it re-reads and compares every
// leaf, exactly as a full YAML re-parse / map diff (DiffStates) must.
type FullDiffResult struct {
	ChangedKeys []string `json:"changed_keys"`
	Comparisons int      `json:"comparisons"` // == LeafCount, always
	LeafCount   int      `json:"leaf_count"`
}

// DiffMerkle locates the differing leaves between two equal-shape trees using
// hierarchical pruning: starting from the root, only the children of nodes
// already known to differ are compared at the next level. An identical subtree
// is discarded with a single comparison (NodesPruned++), which is the source of
// the O(k·log n) bound. RoundTrips models a level-synchronous reconciliation
// protocol (one network round per level that still holds a differing node),
// bounded by Height()+1 = O(log n) regardless of k.
func DiffMerkle(a, b *DriftMerkleTree) *MerkleDriftResult {
	res := &MerkleDriftResult{
		LeafCount: a.LeafCount(),
		Height:    a.Height(),
	}
	if a.LeafCount() == 0 || a.LeafCount() != b.LeafCount() {
		// Shape mismatch cannot happen for pairs from BuildDriftMerklePair, but
		// guard defensively rather than panic.
		return res
	}

	top := len(a.levels) - 1
	res.Comparisons++
	res.RoundTrips++ // root exchange
	if a.levels[top][0] == b.levels[top][0] {
		return res // roots equal => no drift, whole tree pruned in one comparison
	}

	// Node indices (within their level) currently known to differ.
	diffNodes := []int{0}
	for lvl := top - 1; lvl >= 0; lvl-- {
		var nextDiff []int
		levelHadDiff := false
		for _, parent := range diffNodes {
			for _, child := range [2]int{parent * 2, parent*2 + 1} {
				if child >= len(a.levels[lvl]) {
					continue // promoted (unpaired) node has no second child
				}
				res.Comparisons++
				if a.levels[lvl][child] != b.levels[lvl][child] {
					levelHadDiff = true
					nextDiff = append(nextDiff, child)
				} else {
					res.NodesPruned++ // identical subtree skipped
				}
			}
		}
		if levelHadDiff {
			res.RoundTrips++
		}
		diffNodes = nextDiff
		if lvl == 0 {
			for _, idx := range diffNodes {
				res.ChangedKeys = append(res.ChangedKeys, a.keys[idx])
			}
		}
	}
	sort.Strings(res.ChangedKeys)
	return res
}

// NaiveFullDiff is the O(n) baseline: it compares every leaf digest pairwise,
// exactly modelling a full re-parse + map diff that has no precomputed
// hierarchical digests to prune with. Comparisons always equals n.
func NaiveFullDiff(a, b *DriftMerkleTree) *FullDiffResult {
	res := &FullDiffResult{LeafCount: a.LeafCount()}
	if a.LeafCount() != b.LeafCount() {
		return res
	}
	leavesA := a.levels[0]
	leavesB := b.levels[0]
	for i := range leavesA {
		res.Comparisons++
		if leavesA[i] != leavesB[i] {
			res.ChangedKeys = append(res.ChangedKeys, a.keys[i])
		}
	}
	sort.Strings(res.ChangedKeys)
	return res
}
