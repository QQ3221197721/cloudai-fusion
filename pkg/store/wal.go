package store

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"sync"
	"sync/atomic"
	"time"
)

// ============================================================================
// Write-Ahead Log (WAL) — Crash Recovery & Durability
// ============================================================================
//
// The WAL ensures durability by writing all state changes to a sequential,
// on-disk log BEFORE applying them to the main data store. If the system
// crashes mid-operation, the WAL can be replayed on restart to recover to a
// consistent state.
//
// This is a REAL disk-backed implementation:
//   - Records are serialized and appended to segment files on disk.
//   - fsync(2) is invoked according to the configured SyncMode.
//   - CRC32 checksums guard every entry against corruption.
//   - Recovery replays entries from disk, skipping corrupted records.
//
// The disk engine lives in wal_segment.go (segment / DiskWAL). This file
// keeps the higher-level, record-oriented API and an in-memory index used
// for fast queries and record-state (PENDING/APPLIED/ABORTED) tracking.
// Disk is authoritative; the in-memory index is rebuilt from disk on open.
//
// WAL record lifecycle:
//   1. Application writes a WAL record (state: PENDING) — persisted to disk.
//   2. The record is fsynced according to SyncMode (guarantees durability).
//   3. The operation is applied to the main data store.
//   4. A COMMIT marker is appended to disk and the record marked APPLIED.
//   5. On checkpoint, applied segments are compacted / deleted.
//
// On crash recovery:
//   1. Read all WAL entries from disk (CRC-verified).
//   2. Re-apply DATA records still in PENDING state.
//   3. COMMIT/ABORT markers restore APPLIED/ABORTED states.

// ============================================================================
// WAL Record Types
// ============================================================================

// WALRecordState represents the state of a WAL record.
type WALRecordState int

const (
	WALRecordPending WALRecordState = iota
	WALRecordApplied
	WALRecordAborted
)

func (s WALRecordState) String() string {
	switch s {
	case WALRecordPending:
		return "PENDING"
	case WALRecordApplied:
		return "APPLIED"
	case WALRecordAborted:
		return "ABORTED"
	default:
		return "UNKNOWN"
	}
}

// WALRecord represents a single logical entry in the Write-Ahead Log.
type WALRecord struct {
	// Sequence is the monotonically increasing sequence number.
	Sequence uint64 `json:"sequence"`

	// Type identifies the operation type.
	Type string `json:"type"` // "insert", "update", "delete", "txn_begin", "txn_commit", "txn_abort"

	// Table is the target table/collection.
	Table string `json:"table"`

	// Key is the primary key of the affected record.
	Key string `json:"key"`

	// Data contains the operation payload (new value for insert/update).
	Data json.RawMessage `json:"data,omitempty"`

	// OldData contains the previous value (for update/delete, enables undo).
	OldData json.RawMessage `json:"old_data,omitempty"`

	// TxnID links this record to a transaction (for 2PC integration).
	TxnID string `json:"txn_id,omitempty"`

	// State of this WAL record.
	State WALRecordState `json:"state"`

	// Timestamp when the record was written.
	Timestamp time.Time `json:"timestamp"`

	// CRC is the IEEE CRC32 checksum of the record's identifying fields.
	CRC uint32 `json:"crc"`

	// Checksum is the hex-encoded form of CRC (kept for backward compatibility).
	Checksum string `json:"checksum"`
}

// ============================================================================
// WAL Configuration
// ============================================================================

// WALConfig configures the Write-Ahead Log.
type WALConfig struct {
	// MaxSegmentSize is the maximum size of a single WAL segment.
	// When exceeded, a new segment is created. Default: 64MB
	MaxSegmentSize int64

	// MaxSegments is the maximum number of WAL segments to keep.
	// Older segments are removed after checkpoint. Default: 10
	MaxSegments int

	// SyncMode determines when writes are fsynced:
	//   "always"/"immediate" — fsync after every write (safest, slowest)
	//   "batch"              — fsync after a batch of writes (balanced)
	//   "none"               — rely on OS page cache (fastest, least durable)
	// Default: "batch"
	SyncMode string

	// BatchSize is the number of records to batch before fsyncing.
	// Only used when SyncMode is "batch". Default: 100
	BatchSize int

	// BatchTimeout is the maximum time to wait before fsyncing a batch.
	// Default: 10ms
	BatchTimeout time.Duration

	// CheckpointInterval is how often to run checkpointing.
	// Default: 5 minutes
	CheckpointInterval time.Duration

	// RetentionDuration is how long to keep applied WAL records before compaction.
	// Default: 1 hour
	RetentionDuration time.Duration

	// DataDir is the directory for WAL segment files.
	// Default: "./data/wal"
	DataDir string
}

// DefaultWALConfig returns production-ready defaults.
func DefaultWALConfig() WALConfig {
	return WALConfig{
		MaxSegmentSize:     64 * 1024 * 1024, // 64MB
		MaxSegments:        10,
		SyncMode:           "batch",
		BatchSize:          100,
		BatchTimeout:       10 * time.Millisecond,
		CheckpointInterval: 5 * time.Minute,
		RetentionDuration:  1 * time.Hour,
		DataDir:            "./data/wal",
	}
}

// ============================================================================
// WAL Writer (disk-backed)
// ============================================================================

// WAL provides write-ahead logging functionality backed by disk segments.
// Durability is provided by the embedded DiskWAL; the in-memory index is a
// cache/state-tracker rebuilt from disk on open.
type WAL struct {
	config WALConfig

	// disk is the authoritative on-disk log.
	disk *DiskWAL

	// In-memory index for queries and record-state tracking.
	records  []WALRecord
	seqIndex map[uint64]int
	sequence atomic.Uint64

	// Checkpoint tracking
	lastCheckpoint  uint64
	lastCompactedAt time.Time

	// Stats
	stats WALStats

	mu sync.Mutex
}

// NewWAL creates (or opens) a disk-backed Write-Ahead Log and replays any
// existing on-disk entries to rebuild the in-memory index.
func NewWAL(cfg WALConfig) (*WAL, error) {
	if cfg.MaxSegmentSize <= 0 {
		cfg.MaxSegmentSize = 64 * 1024 * 1024
	}
	if cfg.MaxSegments <= 0 {
		cfg.MaxSegments = 10
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.BatchTimeout <= 0 {
		cfg.BatchTimeout = 10 * time.Millisecond
	}
	if cfg.CheckpointInterval <= 0 {
		cfg.CheckpointInterval = 5 * time.Minute
	}
	if cfg.RetentionDuration <= 0 {
		cfg.RetentionDuration = 1 * time.Hour
	}
	if cfg.SyncMode == "" {
		cfg.SyncMode = "batch"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = "./data/wal"
	}

	disk, err := NewDiskWAL(cfg)
	if err != nil {
		return nil, fmt.Errorf("wal: open disk log: %w", err)
	}

	w := &WAL{
		config:   cfg,
		disk:     disk,
		records:  make([]WALRecord, 0, 4096),
		seqIndex: make(map[uint64]int),
	}

	if err := w.replayFromDisk(); err != nil {
		_ = disk.Close()
		return nil, fmt.Errorf("wal: replay: %w", err)
	}

	return w, nil
}

// replayFromDisk rebuilds the in-memory index from the on-disk log.
func (w *WAL) replayFromDisk() error {
	entries, err := w.disk.Recover()
	if err != nil {
		return err
	}

	var maxSeq uint64
	for _, e := range entries {
		if e.Sequence > maxSeq {
			maxSeq = e.Sequence
		}

		switch e.Type {
		case EntryTypeData:
			var rec WALRecord
			if uerr := json.Unmarshal(e.Payload, &rec); uerr != nil {
				// Undecodable payload — treat as skipped, disk still authoritative.
				continue
			}
			if idx, ok := w.seqIndex[rec.Sequence]; ok {
				w.records[idx] = rec
			} else {
				w.records = append(w.records, rec)
				w.seqIndex[rec.Sequence] = len(w.records) - 1
			}

		case EntryTypeCommit:
			if len(e.Payload) >= 8 {
				target := binary.BigEndian.Uint64(e.Payload[:8])
				if idx, ok := w.seqIndex[target]; ok {
					w.records[idx].State = WALRecordApplied
				}
			}

		case EntryTypeAbort:
			if len(e.Payload) >= 8 {
				target := binary.BigEndian.Uint64(e.Payload[:8])
				if idx, ok := w.seqIndex[target]; ok {
					w.records[idx].State = WALRecordAborted
				}
			}

		case EntryTypeCheckpoint:
			if len(e.Payload) >= 8 {
				w.lastCheckpoint = binary.BigEndian.Uint64(e.Payload[:8])
			}
		}
	}

	w.sequence.Store(maxSeq)
	w.stats.RecordsWritten = int64(len(w.records))
	return nil
}

// ============================================================================
// Write Operations
// ============================================================================

// Append writes a new record to the WAL. Returns the sequence number.
func (w *WAL) Append(recordType, table, key string, data interface{}) (uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	seq := w.sequence.Add(1)

	var dataBytes json.RawMessage
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return 0, fmt.Errorf("failed to marshal WAL data: %w", err)
		}
		dataBytes = b
	}

	record := WALRecord{
		Sequence:  seq,
		Type:      recordType,
		Table:     table,
		Key:       key,
		Data:      dataBytes,
		State:     WALRecordPending,
		Timestamp: time.Now(),
	}
	record.CRC = recordChecksum(record)
	record.Checksum = fmt.Sprintf("%08x", record.CRC)

	if err := w.persistRecordLocked(record); err != nil {
		return 0, err
	}

	w.appendIndexLocked(record)
	w.stats.RecordsWritten++
	w.stats.BytesWritten += int64(len(dataBytes))

	return seq, nil
}

// AppendWithOldData writes a WAL record that includes the old value for undo.
func (w *WAL) AppendWithOldData(recordType, table, key string, newData, oldData interface{}) (uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	seq := w.sequence.Add(1)

	var newDataBytes, oldDataBytes json.RawMessage

	if newData != nil {
		b, err := json.Marshal(newData)
		if err != nil {
			return 0, fmt.Errorf("failed to marshal new data: %w", err)
		}
		newDataBytes = b
	}

	if oldData != nil {
		b, err := json.Marshal(oldData)
		if err != nil {
			return 0, fmt.Errorf("failed to marshal old data: %w", err)
		}
		oldDataBytes = b
	}

	record := WALRecord{
		Sequence:  seq,
		Type:      recordType,
		Table:     table,
		Key:       key,
		Data:      newDataBytes,
		OldData:   oldDataBytes,
		State:     WALRecordPending,
		Timestamp: time.Now(),
	}
	record.CRC = recordChecksum(record)
	record.Checksum = fmt.Sprintf("%08x", record.CRC)

	if err := w.persistRecordLocked(record); err != nil {
		return 0, err
	}

	w.appendIndexLocked(record)
	w.stats.RecordsWritten++

	return seq, nil
}

// AppendTxn writes a transaction marker record.
func (w *WAL) AppendTxn(recordType, txnID string) (uint64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	seq := w.sequence.Add(1)

	record := WALRecord{
		Sequence:  seq,
		Type:      recordType,
		TxnID:     txnID,
		State:     WALRecordPending,
		Timestamp: time.Now(),
	}
	record.CRC = recordChecksum(record)
	record.Checksum = fmt.Sprintf("%08x", record.CRC)

	if err := w.persistRecordLocked(record); err != nil {
		return 0, err
	}

	w.appendIndexLocked(record)
	w.stats.RecordsWritten++

	return seq, nil
}

// persistRecordLocked serializes a record and appends it to disk as a DATA
// entry. Caller must hold w.mu.
func (w *WAL) persistRecordLocked(record WALRecord) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("wal: marshal record: %w", err)
	}
	entry := WALEntry{
		Sequence: record.Sequence,
		Type:     EntryTypeData,
		Payload:  payload,
	}
	if err := w.disk.Append(entry); err != nil {
		return fmt.Errorf("wal: disk append: %w", err)
	}
	if w.config.SyncMode == "batch" {
		w.stats.BatchesFlushed++
	}
	return nil
}

// appendIndexLocked adds a record to the in-memory index. Caller holds w.mu.
func (w *WAL) appendIndexLocked(record WALRecord) {
	w.records = append(w.records, record)
	w.seqIndex[record.Sequence] = len(w.records) - 1
}

// markLocked appends a COMMIT/ABORT marker to disk and updates in-memory
// state. Caller must hold w.mu.
func (w *WAL) markLocked(sequence uint64, markerType EntryType, state WALRecordState) error {
	idx, ok := w.seqIndex[sequence]
	if !ok {
		return fmt.Errorf("WAL record with sequence %d not found", sequence)
	}

	markerSeq := w.sequence.Add(1)
	var payload [8]byte
	binary.BigEndian.PutUint64(payload[:], sequence)

	entry := WALEntry{
		Sequence: markerSeq,
		Type:     markerType,
		Payload:  payload[:],
	}
	if err := w.disk.Append(entry); err != nil {
		return fmt.Errorf("wal: append marker: %w", err)
	}

	w.records[idx].State = state
	return nil
}

// MarkApplied marks a WAL record as successfully applied to the data store.
func (w *WAL) MarkApplied(sequence uint64) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.markLocked(sequence, EntryTypeCommit, WALRecordApplied)
}

// MarkAborted marks a WAL record as aborted.
func (w *WAL) MarkAborted(sequence uint64) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.markLocked(sequence, EntryTypeAbort, WALRecordAborted)
}

// ============================================================================
// Batch Flush (Group Commit)
// ============================================================================

// FlushSync forces an immediate flush + fsync of all pending writes to disk.
func (w *WAL) FlushSync() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.disk.Sync(); err != nil {
		return err
	}
	w.stats.SyncsForceed++
	return nil
}

// ============================================================================
// Recovery — Replay pending records
// ============================================================================

// RecoverPending returns all WAL records that are still in PENDING state.
// These need to be re-applied to the data store after a crash.
func (w *WAL) RecoverPending() []WALRecord {
	w.mu.Lock()
	defer w.mu.Unlock()

	pending := make([]WALRecord, 0)
	for _, record := range w.records {
		if record.State == WALRecordPending {
			pending = append(pending, record)
		}
	}

	w.stats.RecoveryCycles++
	return pending
}

// RecoverPendingByTxn returns pending records grouped by transaction ID.
func (w *WAL) RecoverPendingByTxn() map[string][]WALRecord {
	w.mu.Lock()
	defer w.mu.Unlock()

	result := make(map[string][]WALRecord)
	for _, record := range w.records {
		if record.State == WALRecordPending && record.TxnID != "" {
			result[record.TxnID] = append(result[record.TxnID], record)
		}
	}

	return result
}

// RecoverSince returns all records after the given sequence number.
func (w *WAL) RecoverSince(afterSequence uint64) []WALRecord {
	w.mu.Lock()
	defer w.mu.Unlock()

	records := make([]WALRecord, 0)
	for _, record := range w.records {
		if record.Sequence > afterSequence {
			records = append(records, record)
		}
	}

	return records
}

// ============================================================================
// Checkpoint & Compaction
// ============================================================================

// Checkpoint compacts the WAL by removing old applied/aborted records from the
// in-memory index and pruning fully-applied segments from disk. Returns the
// number of records compacted from memory.
func (w *WAL) Checkpoint() (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	cutoff := time.Now().Add(-w.config.RetentionDuration)

	// Highest sequence that is safely applied (used as the disk checkpoint bound).
	var appliedUpTo uint64

	retained := make([]WALRecord, 0, len(w.records)/2+1)
	compacted := 0

	for _, record := range w.records {
		terminal := record.State == WALRecordApplied || record.State == WALRecordAborted
		if terminal && record.Timestamp.Before(cutoff) {
			if record.State == WALRecordApplied && record.Sequence > appliedUpTo {
				appliedUpTo = record.Sequence
			}
			compacted++
			continue
		}
		retained = append(retained, record)
	}

	w.records = retained
	w.rebuildIndexLocked()
	w.lastCheckpoint = w.sequence.Load()
	w.lastCompactedAt = time.Now()
	w.stats.CheckpointsDone++
	w.stats.RecordsCompacted += int64(compacted)

	// Persist a disk checkpoint marker and prune old segments.
	if appliedUpTo > 0 {
		if err := w.disk.Checkpoint(appliedUpTo); err != nil {
			return compacted, fmt.Errorf("wal: disk checkpoint: %w", err)
		}
	}

	return compacted, nil
}

// rebuildIndexLocked recomputes the seqIndex after records slice changes.
// Caller must hold w.mu.
func (w *WAL) rebuildIndexLocked() {
	w.seqIndex = make(map[uint64]int, len(w.records))
	for i := range w.records {
		w.seqIndex[w.records[i].Sequence] = i
	}
}

// ============================================================================
// Read Operations
// ============================================================================

// GetRecord returns a specific WAL record by sequence number.
func (w *WAL) GetRecord(sequence uint64) (*WALRecord, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if idx, ok := w.seqIndex[sequence]; ok {
		rec := w.records[idx]
		return &rec, true
	}
	return nil, false
}

// GetRecordsByTable returns all records for a specific table.
func (w *WAL) GetRecordsByTable(table string) []WALRecord {
	w.mu.Lock()
	defer w.mu.Unlock()

	result := make([]WALRecord, 0)
	for _, record := range w.records {
		if record.Table == table {
			result = append(result, record)
		}
	}
	return result
}

// Len returns the number of records in the in-memory index.
func (w *WAL) Len() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.records)
}

// Close flushes and closes the underlying disk log.
func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.disk == nil {
		return nil
	}
	return w.disk.Close()
}

// DiskWAL returns the underlying disk engine (for advanced use / testing).
func (w *WAL) DiskWAL() *DiskWAL {
	return w.disk
}

// ============================================================================
// Stats
// ============================================================================

// WALStats holds runtime statistics for the WAL.
type WALStats struct {
	RecordsWritten   int64  `json:"records_written"`
	RecordsCompacted int64  `json:"records_compacted"`
	BytesWritten     int64  `json:"bytes_written"`
	BatchesFlushed   int64  `json:"batches_flushed"`
	SyncsForceed     int64  `json:"syncs_forced"`
	CheckpointsDone  int64  `json:"checkpoints_done"`
	RecoveryCycles   int64  `json:"recovery_cycles"`
	CurrentSize      int    `json:"current_size"`
	LastSequence     uint64 `json:"last_sequence"`
}

// Stats returns the current WAL statistics.
func (w *WAL) Stats() WALStats {
	w.mu.Lock()
	defer w.mu.Unlock()

	stats := w.stats
	stats.CurrentSize = len(w.records)
	stats.LastSequence = w.sequence.Load()
	return stats
}

// ============================================================================
// WAL-Integrated Store Operations
// ============================================================================

// WALStore wraps a data store with WAL for crash-safe operations.
// Every write operation is first logged to the WAL, then applied to the store.
type WALStore struct {
	wal   *WAL
	apply func(record WALRecord) error
}

// NewWALStore creates a new WAL-integrated store.
// The apply function is called to apply each WAL record to the underlying store.
func NewWALStore(wal *WAL, applyFn func(record WALRecord) error) *WALStore {
	return &WALStore{
		wal:   wal,
		apply: applyFn,
	}
}

// Write performs a WAL-protected write operation:
// 1. Append to WAL (durably on disk)
// 2. Apply to store
// 3. Mark WAL record as applied (COMMIT marker on disk)
func (ws *WALStore) Write(opType, table, key string, data interface{}) error {
	// Step 1: Write to WAL
	seq, err := ws.wal.Append(opType, table, key, data)
	if err != nil {
		return fmt.Errorf("WAL append failed: %w", err)
	}

	// Step 2: Apply to store
	record, _ := ws.wal.GetRecord(seq)
	if record != nil && ws.apply != nil {
		if err := ws.apply(*record); err != nil {
			_ = ws.wal.MarkAborted(seq)
			return fmt.Errorf("store apply failed: %w", err)
		}
	}

	// Step 3: Mark applied
	if err := ws.wal.MarkApplied(seq); err != nil {
		return fmt.Errorf("WAL mark applied failed: %w", err)
	}

	return nil
}

// Recover replays all pending WAL records to bring the store to a consistent state.
func (ws *WALStore) Recover() (int, error) {
	pending := ws.wal.RecoverPending()
	applied := 0

	for _, record := range pending {
		if ws.apply != nil {
			if err := ws.apply(record); err != nil {
				_ = ws.wal.MarkAborted(record.Sequence)
				continue
			}
		}

		_ = ws.wal.MarkApplied(record.Sequence)
		applied++
	}

	return applied, nil
}

// Checkpoint triggers WAL compaction and segment pruning.
// Returns the number of records compacted from the in-memory index.
func (ws *WALStore) Checkpoint() (int, error) {
	return ws.wal.Checkpoint()
}

// WALRef returns the underlying WAL for direct access.
func (ws *WALStore) WALRef() *WAL {
	return ws.wal
}

// Close closes the underlying WAL and its disk segments.
func (ws *WALStore) Close() error {
	return ws.wal.Close()
}

// recordChecksum computes a REAL IEEE CRC32 over the record's identifying
// fields (never random bytes). Used for integrity verification.
func recordChecksum(rec WALRecord) uint32 {
	h := crc32.NewIEEE()
	var seqBytes [8]byte
	binary.BigEndian.PutUint64(seqBytes[:], rec.Sequence)
	h.Write(seqBytes[:])
	h.Write([]byte(rec.Type))
	h.Write([]byte(rec.Table))
	h.Write([]byte(rec.Key))
	h.Write(rec.Data)
	h.Write(rec.OldData)
	h.Write([]byte(rec.TxnID))
	return h.Sum32()
}
