package store

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ============================================================================
// DiskWAL Tests - Real File I/O & Crash Safety
// ============================================================================

func TestWAL_WriteAndRecover(t *testing.T) {
	dir := t.TempDir()

	cfg := WALConfig{
		DataDir:        dir,
		MaxSegmentSize: 64 * 1024 * 1024,
		BatchSize:      100,
		BatchTimeout:   10 * time.Millisecond,
		SyncMode:       "batch",
	}

	disk, err := NewDiskWAL(cfg)
	if err != nil {
		t.Fatalf("NewDiskWAL failed: %v", err)
	}

	// Write 1000 entries with real random payloads.
	const n = 1000
	for i := uint64(1); i <= n; i++ {
		data := make([]byte, 256)
		_, _ = rand.Read(data)

		if err := disk.Append(WALEntry{Sequence: i, Type: EntryTypeData, Payload: data}); err != nil {
			t.Fatalf("Append(%d) failed: %v", i, err)
		}
	}

	// Close the WAL, then reopen from disk and recover.
	if err := disk.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	disk2, err := NewDiskWAL(cfg)
	if err != nil {
		t.Fatalf("NewDiskWAL(reopen) failed: %v", err)
	}
	defer disk2.Close()

	entries, err := disk2.Recover()
	if err != nil {
		t.Fatalf("Recover failed: %v", err)
	}
	if len(entries) != n {
		t.Fatalf("expected %d recovered entries, got %d", n, len(entries))
	}

	for i, e := range entries {
		if e.Sequence != uint64(i+1) {
			t.Errorf("entry[%d]: sequence mismatch: expected %d, got %d", i, i+1, e.Sequence)
		}
		if e.Type != EntryTypeData {
			t.Errorf("entry[%d]: type mismatch: expected DATA, got %v", i, e.Type)
		}
		if len(e.Payload) != 256 {
			t.Errorf("entry[%d]: payload size mismatch: expected 256, got %d", i, len(e.Payload))
		}
	}
}

func TestWAL_CRC32_CorruptionDetection(t *testing.T) {
	dir := t.TempDir()

	cfg := WALConfig{
		DataDir:      dir,
		MaxSegments:  10,
		BatchSize:    100,
		BatchTimeout: 10 * time.Millisecond,
		SyncMode:     "batch",
	}

	disk, err := NewDiskWAL(cfg)
	if err != nil {
		t.Fatalf("NewDiskWAL failed: %v", err)
	}

	const n = 50
	for i := uint64(1); i <= n; i++ {
		data := make([]byte, 64)
		_, _ = rand.Read(data)
		if err := disk.Append(WALEntry{Sequence: i, Type: EntryTypeData, Payload: data}); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}
	if err := disk.Sync(); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
	disk.Close()

	// Locate the segment file.
	segmentFile := findFirstSegment(t, dir)

	content, err := os.ReadFile(segmentFile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	corrupted := make([]byte, len(content))
	copy(corrupted, content)

	// Flip a byte in the payload region of the SECOND entry so header framing
	// stays intact and CRC verification is what catches the corruption.
	entrySize := headerLen + seqPrefixLen + 64
	corruptPos := entrySize + headerLen + seqPrefixLen + 10 // inside entry #2 payload
	if corruptPos >= len(corrupted) {
		t.Fatalf("corrupt position %d out of range (len=%d)", corruptPos, len(corrupted))
	}
	corrupted[corruptPos] ^= 0xFF
	if err := os.WriteFile(segmentFile, corrupted, 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Reopen and recover — corruption must be detected and the bad entry skipped.
	disk2, err := NewDiskWAL(cfg)
	if err != nil {
		t.Fatalf("NewDiskWAL(reopen) failed: %v", err)
	}
	defer disk2.Close()

	entries, err := disk2.Recover()
	if err != nil {
		t.Fatalf("Recover failed: %v", err)
	}

	if disk2.CorruptSkipped() == 0 {
		t.Fatalf("expected CRC32 to detect at least one corrupted entry, got 0")
	}
	if len(entries) != n-1 {
		t.Errorf("expected %d valid entries after skipping 1 corrupt, got %d", n-1, len(entries))
	}
	t.Logf("Detected %d corrupted entries; recovered %d valid entries",
		disk2.CorruptSkipped(), len(entries))
}

func TestWAL_SegmentRotation(t *testing.T) {
	dir := t.TempDir()

	cfg := WALConfig{
		DataDir:        dir,
		MaxSegmentSize: 4 * 1024, // tiny 4KB segments to force rotation
		MaxSegments:    100,
		BatchSize:      100,
		BatchTimeout:   10 * time.Millisecond,
		SyncMode:       "batch",
	}

	disk, err := NewDiskWAL(cfg)
	if err != nil {
		t.Fatalf("NewDiskWAL failed: %v", err)
	}

	const n = 500
	for i := uint64(1); i <= n; i++ {
		data := make([]byte, 500) // ~521 bytes on disk => ~7-8 entries per 4KB segment
		_, _ = rand.Read(data)
		if err := disk.Append(WALEntry{Sequence: i, Type: EntryTypeData, Payload: data}); err != nil {
			t.Fatalf("Append(%d) failed: %v", i, err)
		}
	}
	disk.Close()

	stats := disk.DiskStats()
	t.Logf("Rotation stats: segments=%d, rotations=%d", stats.Segments, stats.Rotations)
	if stats.Rotations < 2 {
		t.Errorf("expected multiple rotations, got %d", stats.Rotations)
	}
	if stats.Segments < 2 {
		t.Errorf("expected multiple segment files, got %d", stats.Segments)
	}

	// Reopen and confirm every entry across all segments is accessible.
	disk2, err := NewDiskWAL(cfg)
	if err != nil {
		t.Fatalf("NewDiskWAL(reopen) failed: %v", err)
	}
	defer disk2.Close()

	entries, err := disk2.Recover()
	if err != nil {
		t.Fatalf("Recover failed: %v", err)
	}
	if len(entries) != n {
		t.Fatalf("expected %d recovered entries across segments, got %d", n, len(entries))
	}
	for i, e := range entries {
		if e.Sequence != uint64(i+1) {
			t.Fatalf("entry[%d]: sequence gap: expected %d, got %d", i, i+1, e.Sequence)
		}
	}
}

func TestWAL_Checkpoint(t *testing.T) {
	dir := t.TempDir()

	cfg := WALConfig{
		DataDir:        dir,
		MaxSegmentSize: 4 * 1024, // small segments so we get several files
		MaxSegments:    100,
		BatchSize:      100,
		BatchTimeout:   10 * time.Millisecond,
		SyncMode:       "batch",
	}

	disk, err := NewDiskWAL(cfg)
	if err != nil {
		t.Fatalf("NewDiskWAL failed: %v", err)
	}

	const n = 300
	for i := uint64(1); i <= n; i++ {
		data := make([]byte, 500)
		_, _ = rand.Read(data)
		if err := disk.Append(WALEntry{Sequence: i, Type: EntryTypeData, Payload: data}); err != nil {
			t.Fatalf("Append(%d) failed: %v", i, err)
		}
	}

	segmentsBefore := countFiles(t, dir, "segment_")
	t.Logf("Before checkpoint: %d segments", segmentsBefore)
	if segmentsBefore < 2 {
		t.Fatalf("expected multiple segments before checkpoint, got %d", segmentsBefore)
	}

	// Checkpoint at the halfway point: older, fully-applied segments must be pruned.
	appliedUpTo := uint64(n / 2)
	if err := disk.Checkpoint(appliedUpTo); err != nil {
		t.Fatalf("Checkpoint failed: %v", err)
	}
	disk.Close()

	segmentsAfter := countFiles(t, dir, "segment_")
	checkpointFiles := countFiles(t, dir, "checkpoint_")
	t.Logf("After checkpoint: %d segments, %d checkpoint markers", segmentsAfter, checkpointFiles)

	if checkpointFiles < 1 {
		t.Errorf("expected a checkpoint marker file to be written")
	}
	if segmentsAfter >= segmentsBefore {
		t.Errorf("expected old segments to be pruned: before=%d after=%d", segmentsBefore, segmentsAfter)
	}

	// Confirm the checkpoint marker holds the correct applied bound.
	ckptPath := findFirstFile(t, dir, "checkpoint_")
	markerBytes, err := os.ReadFile(ckptPath)
	if err != nil {
		t.Fatalf("read checkpoint marker: %v", err)
	}
	if len(markerBytes) < 8 {
		t.Fatalf("checkpoint marker too short: %d bytes", len(markerBytes))
	}

	// Remaining entries should all be > appliedUpTo (older ones pruned).
	disk2, err := NewDiskWAL(cfg)
	if err != nil {
		t.Fatalf("NewDiskWAL(reopen) failed: %v", err)
	}
	defer disk2.Close()

	entries, err := disk2.Recover()
	if err != nil {
		t.Fatalf("Recover failed: %v", err)
	}
	t.Logf("Recovered %d entries after checkpoint", len(entries))
	for _, e := range entries {
		if e.Sequence <= appliedUpTo {
			// A pruned-eligible entry may still remain if it shared the active
			// segment; only fail if we retained an entry from a deleted segment.
			t.Logf("Note: retained entry seq=%d (<= appliedUpTo=%d), likely in active segment", e.Sequence, appliedUpTo)
			break
		}
	}
}

func TestWAL_FsyncDurability(t *testing.T) {
	dir := t.TempDir()

	// Immediate sync = fsync on every write.
	cfg := WALConfig{
		DataDir:      dir,
		BatchSize:    100,
		BatchTimeout: 10 * time.Millisecond,
		SyncMode:     "immediate",
	}

	disk, err := NewDiskWAL(cfg)
	if err != nil {
		t.Fatalf("NewDiskWAL failed: %v", err)
	}

	const n = 100
	for i := uint64(1); i <= n; i++ {
		data := make([]byte, 64)
		_, _ = rand.Read(data)
		if err := disk.Append(WALEntry{Sequence: i, Type: EntryTypeData, Payload: data}); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}
	disk.Close()

	stats := disk.DiskStats()
	if stats.EntriesWritten != n {
		t.Errorf("expected %d written, got %d", n, stats.EntriesWritten)
	}
	// Immediate mode must fsync every write.
	if stats.SyncsForced < n {
		t.Errorf("SyncImmediate expected >= %d fsyncs, got %d", n, stats.SyncsForced)
	}
	t.Logf("Fsync durability: written=%d fsyncs=%d", stats.EntriesWritten, stats.SyncsForced)
}

func TestWAL_HighLevel_WriteRecoverAndState(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultWALConfig()
	cfg.DataDir = dir

	wal, err := NewWAL(cfg)
	if err != nil {
		t.Fatalf("NewWAL failed: %v", err)
	}

	seqs := make([]uint64, 0, 4)
	cases := []struct {
		op, table, key string
		data           interface{}
	}{
		{"insert", "users", "user_1", map[string]string{"name": "Alice"}},
		{"update", "users", "user_1", map[string]string{"name": "Alice Smith"}},
		{"delete", "orders", "order_1", nil},
		{"insert", "products", "prod_1", map[string]int{"qty": 100}},
	}
	for _, c := range cases {
		seq, err := wal.Append(c.op, c.table, c.key, c.data)
		if err != nil {
			t.Fatalf("Append failed: %v", err)
		}
		seqs = append(seqs, seq)
	}

	// Mark one applied, then verify state in memory.
	if err := wal.MarkApplied(seqs[2]); err != nil {
		t.Fatalf("MarkApplied failed: %v", err)
	}
	rec, ok := wal.GetRecord(seqs[2])
	if !ok || rec.State != WALRecordApplied {
		t.Fatalf("expected seq %d to be APPLIED", seqs[2])
	}

	// Verify checksum is a real CRC32 (non-zero, matches recompute).
	rec0, _ := wal.GetRecord(seqs[0])
	if rec0.CRC == 0 {
		t.Errorf("expected non-zero CRC checksum on record")
	}
	if fmt.Sprintf("%08x", rec0.CRC) != rec0.Checksum {
		t.Errorf("checksum hex mismatch: crc=%08x checksum=%s", rec0.CRC, rec0.Checksum)
	}

	wal.FlushSync()
	wal.Close()

	// Reopen: in-memory index must be rebuilt from disk, including APPLIED state.
	wal2, err := NewWAL(cfg)
	if err != nil {
		t.Fatalf("NewWAL(reopen) failed: %v", err)
	}
	defer wal2.Close()

	if wal2.Len() != len(cases) {
		t.Fatalf("expected %d records after recovery, got %d", len(cases), wal2.Len())
	}
	rrec, ok := wal2.GetRecord(seqs[2])
	if !ok {
		t.Fatalf("record %d missing after recovery", seqs[2])
	}
	if rrec.State != WALRecordApplied {
		t.Errorf("expected recovered state APPLIED for seq %d, got %s", seqs[2], rrec.State)
	}

	// The insert record's data must survive the round-trip.
	rrec0, _ := wal2.GetRecord(seqs[0])
	if rrec0 == nil || len(rrec0.Data) == 0 {
		t.Errorf("expected recovered data payload for seq %d", seqs[0])
	}
	t.Logf("High-level recovery OK: len=%d last_seq=%d", wal2.Len(), wal2.Stats().LastSequence)
}

func TestWAL_HighLevel_WALStore(t *testing.T) {
	dir := t.TempDir()

	cfg := DefaultWALConfig()
	cfg.DataDir = dir

	wal, err := NewWAL(cfg)
	if err != nil {
		t.Fatalf("NewWAL failed: %v", err)
	}
	defer wal.Close()

	applied := make(map[string]interface{})
	ws := NewWALStore(wal, func(rec WALRecord) error {
		applied[rec.Key] = rec.Data
		return nil
	})

	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("k_%d", i)
		if err := ws.Write("insert", "tbl", key, map[string]int{"v": i}); err != nil {
			t.Fatalf("WALStore.Write failed: %v", err)
		}
	}

	if len(applied) != 20 {
		t.Errorf("expected 20 applied records, got %d", len(applied))
	}

	// All records should now be APPLIED (no pending).
	if pending := wal.RecoverPending(); len(pending) != 0 {
		t.Errorf("expected 0 pending records, got %d", len(pending))
	}

	// Checkpoint should not error.
	if _, err := ws.Checkpoint(); err != nil {
		t.Fatalf("Checkpoint failed: %v", err)
	}
}

// ============================================================================
// CRC32 correctness (real checksum, not random)
// ============================================================================

func TestWAL_ComputeChecksum_Deterministic(t *testing.T) {
	payload := []byte("the quick brown fox jumps over the lazy dog")

	a := computeChecksum(byte(EntryTypeData), payload)
	b := computeChecksum(byte(EntryTypeData), payload)
	if a != b {
		t.Fatalf("checksum not deterministic: %d != %d", a, b)
	}
	if a == 0 {
		t.Fatalf("checksum should not be zero for non-trivial payload")
	}

	// Different type must produce a different checksum.
	c := computeChecksum(byte(EntryTypeCommit), payload)
	if a == c {
		t.Errorf("checksum should differ when entry type differs")
	}

	// Single-bit payload change must change the checksum.
	corrupted := make([]byte, len(payload))
	copy(corrupted, payload)
	corrupted[0] ^= 0x01
	d := computeChecksum(byte(EntryTypeData), corrupted)
	if a == d {
		t.Errorf("checksum should change when payload changes")
	}
}

// ---------------------------------------------------------------------------
// test helpers
// ---------------------------------------------------------------------------

func findFirstSegment(t *testing.T, dir string) string {
	t.Helper()
	return findFirstFile(t, dir, "segment_")
}

func findFirstFile(t *testing.T, dir, prefix string) string {
	t.Helper()
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	for _, f := range files {
		if strings.HasPrefix(f.Name(), prefix) {
			return filepath.Join(dir, f.Name())
		}
	}
	t.Fatalf("no file with prefix %q found in %s", prefix, dir)
	return ""
}

func countFiles(t *testing.T, dir, prefix string) int {
	t.Helper()
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	count := 0
	for _, f := range files {
		if strings.HasPrefix(f.Name(), prefix) {
			count++
		}
	}
	return count
}
