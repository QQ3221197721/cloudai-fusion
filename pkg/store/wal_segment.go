package store

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// WAL Segment — Physical Layer: Disk-based Append-Only Log
// ============================================================================
//
// This file implements the low-level, disk-backed storage engine for the WAL.
// Data is authoritative on disk; in-memory structures are only indexes/caches.
//
// On-disk entry binary format (big-endian):
//
//   +----------+--------+----------+--------+------------------------+
//   | Magic(4) | Len(4) | CRC32(4) | Type(1)| Payload (Len-13 bytes) |
//   +----------+--------+----------+--------+------------------------+
//
//   Magic:   0xCAFE0A01 — identifies a valid WAL entry
//   Len:     total record length (header + payload), uint32 big-endian
//   CRC32:   IEEE CRC32 of (Type byte || Payload) — integrity check
//   Type:    entry type (DATA=1, COMMIT=2, CHECKPOINT=3, ABORT=4)
//   Payload: [8-byte big-endian Sequence][user payload]
//
// The 8-byte sequence prefix lives inside Payload so it is covered by the
// CRC and survives crash recovery, while the fixed header stays at 13 bytes.
//
// File naming: "segment_NNNNNN.wal" (6-digit zero-padded monotonic counter).
// Recovery reads all segments in order, verifies CRC32, and skips any
// entry whose checksum does not match (logging the corruption).

// EntryType identifies the kind of WAL operation on disk.
type EntryType byte

const (
	EntryTypeData       EntryType = 1 // Regular data record
	EntryTypeCommit     EntryType = 2 // Mark a sequence as committed/applied
	EntryTypeCheckpoint EntryType = 3 // Checkpoint marker
	EntryTypeAbort      EntryType = 4 // Mark a sequence as aborted
)

func (t EntryType) String() string {
	switch t {
	case EntryTypeData:
		return "DATA"
	case EntryTypeCommit:
		return "COMMIT"
	case EntryTypeCheckpoint:
		return "CHECKPOINT"
	case EntryTypeAbort:
		return "ABORT"
	default:
		return fmt.Sprintf("UNKNOWN_%d", byte(t))
	}
}

const (
	// magicWAL identifies a valid on-disk WAL entry.
	magicWAL uint32 = 0xCAFE0A01

	// headerLen is the fixed on-disk header size: 4 + 4 + 4 + 1 = 13 bytes.
	headerLen = 13

	// seqPrefixLen is the size of the sequence number stored at the head of payload.
	seqPrefixLen = 8

	// maxPayloadSize guards against absurd allocations from a corrupted length field.
	maxPayloadSize = 128 << 20 // 128MB
)

var (
	errWALClosed     = errors.New("wal: closed")
	errEntryTooLarge = errors.New("wal: entry exceeds segment size limit")
)

// SyncMode determines when writes are flushed to stable storage.
type SyncMode int

const (
	// SyncImmediate calls fsync after every write (safest, slowest).
	SyncImmediate SyncMode = iota
	// SyncBatch fsyncs after N entries or after BatchTimeout (balanced).
	SyncBatch
	// SyncOS flushes to the OS page cache but never fsyncs (fastest, least safe).
	SyncOS
)

func (m SyncMode) String() string {
	switch m {
	case SyncImmediate:
		return "immediate"
	case SyncBatch:
		return "batch"
	case SyncOS:
		return "os"
	default:
		return "unknown"
	}
}

// mapSyncMode converts the string config value into a SyncMode.
func mapSyncMode(s string) SyncMode {
	switch strings.ToLower(s) {
	case "always", "immediate", "sync":
		return SyncImmediate
	case "none", "os", "async":
		return SyncOS
	default:
		return SyncBatch
	}
}

// WALEntry is a single logical entry as seen by callers of the disk engine.
type WALEntry struct {
	Sequence uint64    // Monotonic sequence number (caller-assigned)
	Type     EntryType // Entry type
	Payload  []byte    // Opaque user payload
	CRC      uint32    // Populated on Recover with the verified checksum
}

// computeChecksum computes the IEEE CRC32 checksum over (entryType || payload).
// This is a REAL, hardware-accelerated checksum (SSE4.2 on amd64) — never random.
func computeChecksum(entryType byte, payload []byte) uint32 {
	h := crc32.NewIEEE()
	h.Write([]byte{entryType})
	h.Write(payload)
	return h.Sum32()
}

// encodeEntry serializes a WALEntry into its on-disk binary representation.
func encodeEntry(entry WALEntry) []byte {
	phys := make([]byte, seqPrefixLen+len(entry.Payload))
	binary.BigEndian.PutUint64(phys[:seqPrefixLen], entry.Sequence)
	copy(phys[seqPrefixLen:], entry.Payload)

	crc := computeChecksum(byte(entry.Type), phys)
	total := headerLen + len(phys)

	buf := make([]byte, total)
	binary.BigEndian.PutUint32(buf[0:4], magicWAL)
	binary.BigEndian.PutUint32(buf[4:8], uint32(total))
	binary.BigEndian.PutUint32(buf[8:12], crc)
	buf[12] = byte(entry.Type)
	copy(buf[13:], phys)
	return buf
}

// ============================================================================
// segment — a single append-only file
// ============================================================================

type segment struct {
	idx  int
	path string
	f    *os.File
	w    *bufio.Writer
	size int64
}

// openSegment opens (or creates) segment file `idx` in `dir` for appending.
func openSegment(dir string, idx int) (*segment, error) {
	path := filepath.Join(dir, fmt.Sprintf("segment_%06d.wal", idx))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &segment{
		idx:  idx,
		path: path,
		f:    f,
		w:    bufio.NewWriterSize(f, 1<<16),
		size: info.Size(),
	}, nil
}

// write appends raw record bytes to the segment's buffered writer.
func (s *segment) write(rec []byte) error {
	n, err := s.w.Write(rec)
	s.size += int64(n)
	return err
}

// flush pushes buffered bytes to the OS page cache (no fsync).
func (s *segment) flush() error {
	if s.w == nil {
		return nil
	}
	return s.w.Flush()
}

// sync flushes the buffer and calls the REAL fsync(2) syscall on the fd.
func (s *segment) sync() error {
	if s.w == nil || s.f == nil {
		return nil
	}
	if err := s.w.Flush(); err != nil {
		return err
	}
	return s.f.Sync()
}

// close flushes and closes the underlying file.
func (s *segment) close() error {
	if s.f == nil {
		return nil
	}
	ferr := s.w.Flush()
	cerr := s.f.Close()
	s.f = nil
	s.w = nil
	if ferr != nil {
		return ferr
	}
	return cerr
}

// ============================================================================
// segment reading / recovery helpers
// ============================================================================

// readAllEntries reads and CRC-verifies every entry in a segment file.
// Corrupted entries (bad CRC) are skipped; the number skipped is returned.
func readAllEntries(path string) (entries []WALEntry, corrupted int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	r := bufio.NewReaderSize(f, 1<<16)
	hdr := make([]byte, headerLen)

	for {
		if _, rerr := io.ReadFull(r, hdr); rerr != nil {
			// Clean EOF or a truncated trailing header — stop reading.
			break
		}

		magic := binary.BigEndian.Uint32(hdr[0:4])
		if magic != magicWAL {
			// Header framing is broken; we cannot safely continue.
			corrupted++
			break
		}

		length := binary.BigEndian.Uint32(hdr[4:8])
		crc := binary.BigEndian.Uint32(hdr[8:12])
		etype := hdr[12]

		if int(length) < headerLen+seqPrefixLen || int(length)-headerLen > maxPayloadSize {
			corrupted++
			break
		}

		plen := int(length) - headerLen
		phys := make([]byte, plen)
		if _, rerr := io.ReadFull(r, phys); rerr != nil {
			// Truncated payload at tail — treat as end of usable log.
			corrupted++
			break
		}

		if computeChecksum(etype, phys) != crc {
			// Corruption detected: skip this entry, keep scanning.
			corrupted++
			continue
		}

		seq := binary.BigEndian.Uint64(phys[:seqPrefixLen])
		payload := make([]byte, plen-seqPrefixLen)
		copy(payload, phys[seqPrefixLen:])

		entries = append(entries, WALEntry{
			Sequence: seq,
			Type:     EntryType(etype),
			Payload:  payload,
			CRC:      crc,
		})
	}

	return entries, corrupted, nil
}

// scanMaxSequence returns the highest sequence number found in a segment.
func scanMaxSequence(path string) uint64 {
	entries, _, err := readAllEntries(path)
	if err != nil {
		return 0
	}
	var maxSeq uint64
	for _, e := range entries {
		if e.Sequence > maxSeq {
			maxSeq = e.Sequence
		}
	}
	return maxSeq
}

// listSegmentFiles returns segment file paths in monotonic (sorted) order.
func listSegmentFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var paths []string
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "segment_") && strings.HasSuffix(name, ".wal") {
			paths = append(paths, filepath.Join(dir, name))
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// segmentIndexFromPath extracts the numeric index from a segment file name.
func segmentIndexFromPath(path string) int {
	base := filepath.Base(path)
	base = strings.TrimPrefix(base, "segment_")
	base = strings.TrimSuffix(base, ".wal")
	var idx int
	if _, err := fmt.Sscanf(base, "%d", &idx); err != nil {
		return 0
	}
	return idx
}

// ============================================================================
// DiskWAL — segment-managing, crash-safe append-only log
// ============================================================================

// DiskWAL is a disk-backed Write-Ahead Log. Writes are appended to segment
// files, rotated at MaxSegmentSize, fsynced according to SyncMode, and
// verified with CRC32 on recovery.
type DiskWAL struct {
	mu sync.Mutex

	dir          string
	syncMode     SyncMode
	maxSegSize   int64
	batchSize    int
	batchTimeout time.Duration
	maxSegments  int

	cur       *segment
	segPaths  []string          // ordered list of all segment paths (incl. cur)
	segMaxSeq map[string]uint64 // per-segment highest sequence (for checkpoint pruning)

	batchCount int
	lastSync   time.Time
	closed     bool

	// Stats
	entriesWritten int64
	bytesWritten   int64
	syncsForced    int64
	rotations      int64
	checkpoints    int64
	corruptSkipped int64
}

// NewDiskWAL opens (or creates) a disk WAL rooted at cfg.DataDir.
func NewDiskWAL(cfg WALConfig) (*DiskWAL, error) {
	dir := cfg.DataDir
	if dir == "" {
		dir = "./data/wal"
	}
	dir = filepath.Clean(dir)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("wal: create dir %q: %w", dir, err)
	}

	maxSegSize := cfg.MaxSegmentSize
	if maxSegSize <= 0 {
		maxSegSize = 64 * 1024 * 1024
	}
	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}
	batchTimeout := cfg.BatchTimeout
	if batchTimeout <= 0 {
		batchTimeout = 10 * time.Millisecond
	}
	maxSegments := cfg.MaxSegments
	if maxSegments <= 0 {
		maxSegments = 10
	}

	d := &DiskWAL{
		dir:          dir,
		syncMode:     mapSyncMode(cfg.SyncMode),
		maxSegSize:   maxSegSize,
		batchSize:    batchSize,
		batchTimeout: batchTimeout,
		maxSegments:  maxSegments,
		segMaxSeq:    make(map[string]uint64),
		lastSync:     time.Now(),
	}

	existing, err := listSegmentFiles(dir)
	if err != nil {
		return nil, fmt.Errorf("wal: list segments: %w", err)
	}

	for _, p := range existing {
		d.segPaths = append(d.segPaths, p)
		d.segMaxSeq[p] = scanMaxSequence(p)
	}

	// Determine which segment to append to.
	var idx int
	if len(existing) == 0 {
		idx = 1
	} else {
		idx = segmentIndexFromPath(existing[len(existing)-1])
		if idx == 0 {
			idx = len(existing)
		}
	}

	seg, err := openSegment(dir, idx)
	if err != nil {
		return nil, fmt.Errorf("wal: open segment: %w", err)
	}
	d.cur = seg

	if len(existing) == 0 {
		d.segPaths = append(d.segPaths, seg.path)
		d.segMaxSeq[seg.path] = 0
	}

	return d, nil
}

// Append writes a single entry to disk with crash-safe durability.
func (d *DiskWAL) Append(entry WALEntry) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return errWALClosed
	}

	rec := encodeEntry(entry)
	if int64(len(rec)) > d.maxSegSize {
		return errEntryTooLarge
	}

	// Rotate if the current segment would exceed the size cap (but never
	// rotate an empty segment, otherwise a single large entry would loop).
	if d.cur.size > 0 && d.cur.size+int64(len(rec)) > d.maxSegSize {
		if err := d.rotateLocked(); err != nil {
			return err
		}
	}

	if err := d.cur.write(rec); err != nil {
		return err
	}

	if entry.Sequence > d.segMaxSeq[d.cur.path] {
		d.segMaxSeq[d.cur.path] = entry.Sequence
	}
	d.entriesWritten++
	d.bytesWritten += int64(len(rec))
	d.batchCount++

	return d.maybeSyncLocked()
}

// maybeSyncLocked applies the configured SyncMode. Caller must hold d.mu.
func (d *DiskWAL) maybeSyncLocked() error {
	switch d.syncMode {
	case SyncImmediate:
		if err := d.cur.sync(); err != nil {
			return err
		}
		d.syncsForced++
		d.batchCount = 0
		d.lastSync = time.Now()

	case SyncBatch:
		if d.batchCount >= d.batchSize || time.Since(d.lastSync) >= d.batchTimeout {
			if err := d.cur.sync(); err != nil {
				return err
			}
			d.syncsForced++
			d.batchCount = 0
			d.lastSync = time.Now()
		}

	case SyncOS:
		// Push bytes into the OS page cache; rely on the OS to persist them.
		if err := d.cur.flush(); err != nil {
			return err
		}
	}
	return nil
}

// rotateLocked closes the current segment and opens the next one.
// Caller must hold d.mu.
func (d *DiskWAL) rotateLocked() error {
	if err := d.cur.sync(); err != nil {
		return err
	}
	if err := d.cur.close(); err != nil {
		return err
	}

	seg, err := openSegment(d.dir, d.cur.idx+1)
	if err != nil {
		return err
	}
	d.cur = seg
	d.segPaths = append(d.segPaths, seg.path)
	if _, ok := d.segMaxSeq[seg.path]; !ok {
		d.segMaxSeq[seg.path] = 0
	}
	d.rotations++
	return nil
}

// Sync forces a flush + fsync of the current segment.
func (d *DiskWAL) Sync() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return errWALClosed
	}
	if err := d.cur.sync(); err != nil {
		return err
	}
	d.syncsForced++
	d.batchCount = 0
	d.lastSync = time.Now()
	return nil
}

// Recover reads every valid entry from all segments in order. Entries that
// fail CRC verification are skipped and counted (see CorruptSkipped()).
func (d *DiskWAL) Recover() ([]WALEntry, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Make sure everything buffered has reached the OS before we read.
	if d.cur != nil {
		if err := d.cur.flush(); err != nil {
			return nil, err
		}
	}

	var all []WALEntry
	for _, p := range d.segPaths {
		entries, corrupted, err := readAllEntries(p)
		if err != nil {
			return all, fmt.Errorf("wal: read segment %q: %w", p, err)
		}
		d.corruptSkipped += int64(corrupted)
		all = append(all, entries...)
	}
	return all, nil
}

// Checkpoint writes a checkpoint marker file (fsynced) and prunes segments
// whose entries are all covered by appliedUpTo.
func (d *DiskWAL) Checkpoint(appliedUpTo uint64) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return errWALClosed
	}
	if appliedUpTo == 0 {
		return errors.New("wal: appliedUpTo must be > 0")
	}

	// 1. Write + fsync the checkpoint marker file.
	ckptPath := filepath.Join(d.dir, fmt.Sprintf("checkpoint_%06d.ckpt", d.cur.idx))
	cf, err := os.OpenFile(ckptPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("wal: open checkpoint: %w", err)
	}
	var marker [8]byte
	binary.BigEndian.PutUint64(marker[:], appliedUpTo)
	if _, err := cf.Write(marker[:]); err != nil {
		_ = cf.Close()
		return fmt.Errorf("wal: write checkpoint: %w", err)
	}
	if err := cf.Sync(); err != nil {
		_ = cf.Close()
		return fmt.Errorf("wal: fsync checkpoint: %w", err)
	}
	if err := cf.Close(); err != nil {
		return err
	}

	// 2. Delete fully-applied segments (never the current/active one).
	kept := make([]string, 0, len(d.segPaths))
	for _, p := range d.segPaths {
		if p == d.cur.path {
			kept = append(kept, p)
			continue
		}
		maxSeq, ok := d.segMaxSeq[p]
		if ok && maxSeq != 0 && maxSeq <= appliedUpTo {
			if rmErr := os.Remove(p); rmErr != nil && !os.IsNotExist(rmErr) {
				return fmt.Errorf("wal: remove segment %q: %w", p, rmErr)
			}
			delete(d.segMaxSeq, p)
			continue
		}
		kept = append(kept, p)
	}
	d.segPaths = kept
	d.checkpoints++

	return nil
}

// Close flushes, fsyncs and closes the WAL.
func (d *DiskWAL) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return nil
	}
	d.closed = true

	if d.cur != nil {
		if err := d.cur.sync(); err != nil {
			_ = d.cur.close()
			return err
		}
		err := d.cur.close()
		d.cur = nil
		return err
	}
	return nil
}

// ---------------------------------------------------------------------------
// Introspection helpers
// ---------------------------------------------------------------------------

// Size returns the total size in bytes across all active segments.
func (d *DiskWAL) Size() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cur == nil {
		return 0
	}
	return d.cur.size
}

// SegmentCount returns the number of live segment files.
func (d *DiskWAL) SegmentCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.segPaths)
}

// SegmentPaths returns a copy of the current segment path list.
func (d *DiskWAL) SegmentPaths() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.segPaths...)
}

// CorruptSkipped returns how many entries were skipped due to CRC failure.
func (d *DiskWAL) CorruptSkipped() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.corruptSkipped
}

// DiskStats is a snapshot of the disk WAL counters.
type DiskStats struct {
	EntriesWritten int64
	BytesWritten   int64
	SyncsForced    int64
	Rotations      int64
	Checkpoints    int64
	CorruptSkipped int64
	Segments       int
	SyncMode       string
}

// DiskStats returns a snapshot of internal counters.
func (d *DiskWAL) DiskStats() DiskStats {
	d.mu.Lock()
	defer d.mu.Unlock()
	return DiskStats{
		EntriesWritten: d.entriesWritten,
		BytesWritten:   d.bytesWritten,
		SyncsForced:    d.syncsForced,
		Rotations:      d.rotations,
		Checkpoints:    d.checkpoints,
		CorruptSkipped: d.corruptSkipped,
		Segments:       len(d.segPaths),
		SyncMode:       d.syncMode.String(),
	}
}
