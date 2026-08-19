package store

import (
	"fmt"
	"testing"
	"time"
)

// ============================================================================
// WAL Benchmarks - Real Disk I/O Performance
// ============================================================================
//
// Run with:  go test ./pkg/store/ -bench=BenchmarkWAL -benchmem -run=^$
//
// These benchmarks exercise the REAL disk path (segment files + fsync).
// They report entries/sec via b.ReportMetric so the durability/throughput
// trade-off of each SyncMode is measurable, not faked.

const benchEntrySize = 256 // realistic WAL entry payload size

func makePayload(size int) []byte {
	p := make([]byte, size)
	for i := range p {
		p[i] = byte(i % 251) // deterministic, avoids RNG overhead in the hot loop
	}
	return p
}

// BenchmarkWAL_SequentialWrite_SyncBatch measures batched-fsync throughput.
// Target: >100K entries/sec (256B entries, fsync every 100 entries).
func BenchmarkWAL_SequentialWrite_SyncBatch(b *testing.B) {
	dir := b.TempDir()
	cfg := WALConfig{
		DataDir:        dir,
		MaxSegmentSize: 256 * 1024 * 1024,
		MaxSegments:    1000,
		BatchSize:      100,
		BatchTimeout:   50 * time.Millisecond,
		SyncMode:       "batch",
	}

	disk, err := NewDiskWAL(cfg)
	if err != nil {
		b.Fatalf("NewDiskWAL failed: %v", err)
	}
	defer disk.Close()

	payload := makePayload(benchEntrySize)

	b.SetBytes(int64(benchEntrySize))
	b.ResetTimer()
	start := time.Now()

	for i := 0; i < b.N; i++ {
		if err := disk.Append(WALEntry{Sequence: uint64(i + 1), Type: EntryTypeData, Payload: payload}); err != nil {
			b.Fatalf("Append failed: %v", err)
		}
	}
	// Ensure the final batch reaches disk before we stop the clock.
	_ = disk.Sync()

	b.StopTimer()
	reportThroughput(b, b.N, time.Since(start))
}

// BenchmarkWAL_SequentialWrite_SyncImmediate measures fsync-per-write throughput.
// Target: >10K entries/sec (fsync per write is intentionally expensive).
func BenchmarkWAL_SequentialWrite_SyncImmediate(b *testing.B) {
	dir := b.TempDir()
	cfg := WALConfig{
		DataDir:        dir,
		MaxSegmentSize: 256 * 1024 * 1024,
		MaxSegments:    1000,
		BatchSize:      1,
		BatchTimeout:   time.Millisecond,
		SyncMode:       "immediate",
	}

	disk, err := NewDiskWAL(cfg)
	if err != nil {
		b.Fatalf("NewDiskWAL failed: %v", err)
	}
	defer disk.Close()

	payload := makePayload(benchEntrySize)

	b.SetBytes(int64(benchEntrySize))
	b.ResetTimer()
	start := time.Now()

	for i := 0; i < b.N; i++ {
		if err := disk.Append(WALEntry{Sequence: uint64(i + 1), Type: EntryTypeData, Payload: payload}); err != nil {
			b.Fatalf("Append failed: %v", err)
		}
	}

	b.StopTimer()
	reportThroughput(b, b.N, time.Since(start))
}

// BenchmarkWAL_SequentialWrite_SyncOS measures OS-page-cache throughput
// (fastest, least safe — no fsync).
func BenchmarkWAL_SequentialWrite_SyncOS(b *testing.B) {
	dir := b.TempDir()
	cfg := WALConfig{
		DataDir:        dir,
		MaxSegmentSize: 256 * 1024 * 1024,
		MaxSegments:    1000,
		BatchSize:      1000,
		BatchTimeout:   time.Second,
		SyncMode:       "none",
	}

	disk, err := NewDiskWAL(cfg)
	if err != nil {
		b.Fatalf("NewDiskWAL failed: %v", err)
	}
	defer disk.Close()

	payload := makePayload(benchEntrySize)

	b.SetBytes(int64(benchEntrySize))
	b.ResetTimer()
	start := time.Now()

	for i := 0; i < b.N; i++ {
		if err := disk.Append(WALEntry{Sequence: uint64(i + 1), Type: EntryTypeData, Payload: payload}); err != nil {
			b.Fatalf("Append failed: %v", err)
		}
	}

	b.StopTimer()
	reportThroughput(b, b.N, time.Since(start))
}

// BenchmarkWAL_Recovery_1M_Entries measures sequential read/replay throughput.
// Target: recover 1M entries quickly (>2M entries/sec read, <500ms for 1M).
func BenchmarkWAL_Recovery_1M_Entries(b *testing.B) {
	dir := b.TempDir()
	cfg := WALConfig{
		DataDir:        dir,
		MaxSegmentSize: 256 * 1024 * 1024,
		MaxSegments:    10000,
		BatchSize:      1000,
		BatchTimeout:   time.Second,
		SyncMode:       "none", // fast population; we fsync once at the end
	}

	const total = 1_000_000

	// One-time population (not timed).
	disk, err := NewDiskWAL(cfg)
	if err != nil {
		b.Fatalf("NewDiskWAL failed: %v", err)
	}
	payload := makePayload(benchEntrySize)
	for i := 0; i < total; i++ {
		if err := disk.Append(WALEntry{Sequence: uint64(i + 1), Type: EntryTypeData, Payload: payload}); err != nil {
			b.Fatalf("populate Append failed: %v", err)
		}
	}
	if err := disk.Close(); err != nil {
		b.Fatalf("Close failed: %v", err)
	}

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		disk2, err := NewDiskWAL(cfg)
		if err != nil {
			b.Fatalf("reopen failed: %v", err)
		}
		start := time.Now()
		entries, err := disk2.Recover()
		elapsed := time.Since(start)
		if err != nil {
			b.Fatalf("Recover failed: %v", err)
		}
		if len(entries) != total {
			b.Fatalf("expected %d entries, recovered %d", total, len(entries))
		}
		if n == 0 {
			b.ReportMetric(float64(total)/elapsed.Seconds(), "entries/sec")
			b.ReportMetric(float64(elapsed.Milliseconds()), "ms/1M")
		}
		disk2.Close()
	}
}

// BenchmarkWAL_CRC32_Verification measures raw checksum throughput.
// Target: >10M verifications/sec (hardware-accelerated CRC32).
func BenchmarkWAL_CRC32_Verification(b *testing.B) {
	payload := makePayload(benchEntrySize)
	et := byte(EntryTypeData)

	b.SetBytes(int64(benchEntrySize))
	b.ResetTimer()
	start := time.Now()

	var sink uint32
	for i := 0; i < b.N; i++ {
		sink = computeChecksum(et, payload)
	}

	b.StopTimer()
	_ = sink
	reportThroughput(b, b.N, time.Since(start))
	// Rename the metric to reflect verification semantics.
	b.ReportMetric(float64(b.N)/time.Since(start).Seconds(), "verifications/sec")
}

// BenchmarkWAL_InMemory_Baseline is the pure encode + in-RAM append baseline
// (no disk). This is the theoretical ceiling to compare disk modes against.
func BenchmarkWAL_InMemory_Baseline(b *testing.B) {
	payload := makePayload(benchEntrySize)
	buffer := make([][]byte, 0, b.N)

	b.SetBytes(int64(benchEntrySize))
	b.ResetTimer()
	start := time.Now()

	for i := 0; i < b.N; i++ {
		enc := encodeEntry(WALEntry{Sequence: uint64(i + 1), Type: EntryTypeData, Payload: payload})
		buffer = append(buffer, enc)
	}

	b.StopTimer()
	_ = buffer
	reportThroughput(b, b.N, time.Since(start))
}

// BenchmarkWAL_HighLevel_Write benchmarks the record-oriented WAL API,
// which serializes WALRecord to JSON before writing to disk.
func BenchmarkWAL_HighLevel_Write(b *testing.B) {
	dir := b.TempDir()
	cfg := DefaultWALConfig()
	cfg.DataDir = dir
	cfg.SyncMode = "batch"
	cfg.BatchSize = 100

	wal, err := NewWAL(cfg)
	if err != nil {
		b.Fatalf("NewWAL failed: %v", err)
	}
	defer wal.Close()

	data := map[string]interface{}{
		"id":    0,
		"name":  "benchmark-record",
		"value": 3.14159,
		"tags":  []string{"a", "b", "c"},
	}

	b.ResetTimer()
	start := time.Now()

	for i := 0; i < b.N; i++ {
		data["id"] = i
		if _, err := wal.Append("insert", "bench", fmt.Sprintf("k_%d", i), data); err != nil {
			b.Fatalf("Append failed: %v", err)
		}
	}
	_ = wal.FlushSync()

	b.StopTimer()
	reportThroughput(b, b.N, time.Since(start))
}

// reportThroughput reports entries/sec for a benchmark run.
func reportThroughput(b *testing.B, ops int, elapsed time.Duration) {
	b.Helper()
	if elapsed <= 0 || ops == 0 {
		return
	}
	b.ReportMetric(float64(ops)/elapsed.Seconds(), "entries/sec")
}
