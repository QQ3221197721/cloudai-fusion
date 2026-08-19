// Package reporting - content-addressed delta export for incremental snapshot
// synchronization.
//
// DeltaExporter computes a minimal diff between two Report snapshots by
// comparing per-group content hashes. Only groups whose aggregates have
// actually changed are included in the export payload. This reduces export
// bandwidth from O(groups) to O(changed) — critical for FinOps pipelines that
// poll snapshots at sub-second intervals where typically <1% of groups change
// between consecutive snapshots.
//
// Hash scheme: FNV-1a 64-bit over the canonical (sorted-key) representation
// of each AggRow. FNV is chosen for its branch-free single-pass computation
// and zero-allocation property when fed from stack buffers — SHA/BLAKE would
// be overkill for non-cryptographic content addressing within a trusted process
// boundary.
package reporting

import (
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
)

// ChangedGroup represents one group that differs between two snapshots.
type ChangedGroup struct {
	// Key is the composite dimension key for this group.
	Key string `json:"key"`
	// DimValues holds the individual dimension key→value pairs.
	DimValues map[string]string `json:"dim_values"`
	// Op describes the change type.
	Op DeltaOp `json:"op"`
	// Row is the current aggregate for this group (nil for Removed).
	Row *AggRow `json:"row,omitempty"`
}

// DeltaOp categorizes a change between snapshots.
type DeltaOp string

const (
	DeltaOpAdded    DeltaOp = "added"
	DeltaOpRemoved  DeltaOp = "removed"
	DeltaOpModified DeltaOp = "modified"
)

// SnapshotDigest is a content-addressed index of a Report snapshot. It maps
// each group's composite key to the FNV-1a hash of its aggregate values.
type SnapshotDigest struct {
	// hashes maps composite-key → fnv64 of the row's metric values.
	hashes map[string]uint64
	// dims is the dimension order used to reconstruct composite keys.
	dims []string
}

// NewSnapshotDigest computes the content-addressed digest of a Report.
// Cost: O(groups) — one hash per row, no sorting beyond what Report already
// guarantees.
func NewSnapshotDigest(report *Report) *SnapshotDigest {
	if report == nil {
		return &SnapshotDigest{hashes: make(map[string]uint64)}
	}
	sd := &SnapshotDigest{
		hashes: make(map[string]uint64, len(report.Rows)),
		dims:   report.Dimensions,
	}
	for i := range report.Rows {
		row := &report.Rows[i]
		key := compositeKeyFromMap(row.Keys, sd.dims)
		sd.hashes[key] = hashRow(row)
	}
	return sd
}

// DeltaExporter computes incremental diffs between snapshots using
// content-addressed hashing.
type DeltaExporter struct {
	prev *SnapshotDigest
}

// NewDeltaExporter creates an exporter initialized with the previous snapshot
// digest. The first export will treat all current groups as "added".
func NewDeltaExporter(prev *SnapshotDigest) *DeltaExporter {
	if prev == nil {
		prev = &SnapshotDigest{hashes: make(map[string]uint64)}
	}
	return &DeltaExporter{prev: prev}
}

// Export computes the delta between the previous snapshot and the current
// Report. It returns only the changed groups and advances the internal state
// so subsequent calls diff against `current`.
//
// Performance: O(|current| + |prev|) hash comparisons with no sorting —
// dominated by the map iteration and FNV computation which are both
// branch-free linear scans.
func (de *DeltaExporter) Export(current *Report) []ChangedGroup {
	if current == nil {
		// Everything removed.
		changes := make([]ChangedGroup, 0, len(de.prev.hashes))
		for key := range de.prev.hashes {
			changes = append(changes, ChangedGroup{
				Key: key,
				Op:  DeltaOpRemoved,
			})
		}
		de.prev = &SnapshotDigest{hashes: make(map[string]uint64)}
		return changes
	}

	curDigest := NewSnapshotDigest(current)
	changes := make([]ChangedGroup, 0, 64)

	// Build a row lookup for the current report.
	rowIndex := make(map[string]*AggRow, len(current.Rows))
	for i := range current.Rows {
		row := &current.Rows[i]
		key := compositeKeyFromMap(row.Keys, current.Dimensions)
		rowIndex[key] = row
	}

	// Find added and modified groups.
	for key, curHash := range curDigest.hashes {
		prevHash, exists := de.prev.hashes[key]
		if !exists {
			row := rowIndex[key]
			changes = append(changes, ChangedGroup{
				Key:       key,
				DimValues: row.Keys,
				Op:        DeltaOpAdded,
				Row:       row,
			})
		} else if curHash != prevHash {
			row := rowIndex[key]
			changes = append(changes, ChangedGroup{
				Key:       key,
				DimValues: row.Keys,
				Op:        DeltaOpModified,
				Row:       row,
			})
		}
	}

	// Find removed groups.
	for key := range de.prev.hashes {
		if _, exists := curDigest.hashes[key]; !exists {
			changes = append(changes, ChangedGroup{
				Key: key,
				Op:  DeltaOpRemoved,
			})
		}
	}

	// Advance state.
	de.prev = curDigest
	return changes
}

// ExportDigest is a stateless variant: given two digests and the current report,
// compute the delta without mutating any state.
func ExportDigest(prev, cur *SnapshotDigest, current *Report) []ChangedGroup {
	if prev == nil {
		prev = &SnapshotDigest{hashes: make(map[string]uint64)}
	}
	if cur == nil {
		cur = &SnapshotDigest{hashes: make(map[string]uint64)}
	}

	changes := make([]ChangedGroup, 0, 64)

	rowIndex := make(map[string]*AggRow, len(current.Rows))
	if current != nil {
		for i := range current.Rows {
			row := &current.Rows[i]
			key := compositeKeyFromMap(row.Keys, current.Dimensions)
			rowIndex[key] = row
		}
	}

	for key, curHash := range cur.hashes {
		prevHash, exists := prev.hashes[key]
		if !exists {
			if row := rowIndex[key]; row != nil {
				changes = append(changes, ChangedGroup{
					Key:       key,
					DimValues: row.Keys,
					Op:        DeltaOpAdded,
					Row:       row,
				})
			}
		} else if curHash != prevHash {
			if row := rowIndex[key]; row != nil {
				changes = append(changes, ChangedGroup{
					Key:       key,
					DimValues: row.Keys,
					Op:        DeltaOpModified,
					Row:       row,
				})
			}
		}
	}

	for key := range prev.hashes {
		if _, exists := cur.hashes[key]; !exists {
			changes = append(changes, ChangedGroup{
				Key: key,
				Op:  DeltaOpRemoved,
			})
		}
	}

	return changes
}

// ---------------------------------------------------------------------------
// Internal hashing helpers
// ---------------------------------------------------------------------------

// hashRow computes a FNV-1a 64-bit hash of the row's metric values. The hash
// is computed over a canonical byte representation: count|quantity|cost encoded
// as fixed-format strings separated by NUL bytes.
func hashRow(row *AggRow) uint64 {
	h := fnv.New64a()
	var buf [32]byte

	// Count
	b := strconv.AppendInt(buf[:0], row.Count, 10)
	h.Write(b)
	h.Write([]byte{0})

	// Quantity
	b = strconv.AppendInt(buf[:0], row.Quantity, 10)
	h.Write(b)
	h.Write([]byte{0})

	// Cost — 6 decimal places for determinism.
	b = strconv.AppendFloat(buf[:0], row.Cost, 'f', 6, 64)
	h.Write(b)

	return h.Sum64()
}

// compositeKeyFromMap builds a composite key from a dimension map in
// deterministic order.
func compositeKeyFromMap(keys map[string]string, dims []string) string {
	if len(dims) == 0 {
		// Fallback: sort keys for determinism.
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		var b strings.Builder
		for i, k := range sorted {
			if i > 0 {
				b.WriteByte(0)
			}
			b.WriteString(keys[k])
		}
		return b.String()
	}
	if len(dims) == 1 {
		return keys[dims[0]]
	}
	var b strings.Builder
	for i, d := range dims {
		if i > 0 {
			b.WriteByte(0)
		}
		b.WriteString(keys[d])
	}
	return b.String()
}
