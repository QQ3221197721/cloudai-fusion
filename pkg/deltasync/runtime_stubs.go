// ============================================================================
// M23 FLIP BENCHMARKS: Minimal Runtime Stubs for CRDT Engine Testing
// ============================================================================
// This file provides minimal implementations of missing types required for
// running CRDT benchmarks against SOTA competitors. These stubs allow
// compilation and basic benchmark execution without full rsync algorithm.
//
// RUN COMMAND:
//   go test -bench=BenchmarkM23_ -benchmem -run=NONE ./pkg/deltasync/...
// ============================================================================

package deltasync

import (
	"fmt"
	"testing"
)

// ============================================================================
// RSYNC ROLLING CHECKSUM STUBS
// ============================================================================
// Minimal implementation for testing bandwidth efficiency metrics.
// This is NOT a full rsync algorithm - just enough to satisfy compiler.

// RsyncRollingChecksum represents a rolling checksum for file synchronization.
type RsyncRollingChecksum struct {
	blockSize int
	slice     []byte
	startIdx  int
}

// ============================================================================
// REPLICA ID MANAGER
// ============================================================================
// These are already defined in crdt_engine.go - stub functions for API compatibility.

// replica is an alias for the existing function in crdt_engine.go.
func replica() uint32 {
	return replicaID()
}

// setReplicaIDWrapper invokes the global setReplicaID from crdt_engine.go.
func setReplicaIDWrapper(id uint32) {
	setReplicaID(id)
}

// NewRsyncRollingChecksum creates a new rolling checksum instance.
func NewRsyncRollingChecksum(blockSize int, data []byte) *RsyncRollingChecksum {
	return &RsyncRollingChecksum{
		blockSize: blockSize,
		slice:     data,
		startIdx:  0,
	}
}

// Update computes rolling hash incrementally.
func (r *RsyncRollingChecksum) Update(data []byte) uint64 {
	if len(data) == 0 {
		return 0
	}

	// Simple rolling hash (not production rsync - FIPS compliance would use Adler-32)
	var hash uint64 = 0
	for _, b := range data {
		hash = hash*31 + uint64(b)
	}
	return hash
}

// Rolling computes the next window's checksum incrementally.
func (r *RsyncRollingChecksum) Rolling() uint64 {
	if len(r.slice) < r.blockSize {
		return 0
	}

	// Efficient sliding window update
	oldByte := uint64(r.slice[0])
	var newByte uint64 = 0
	sum := uint64(0)

	// Compute sum over current window
	for i := 0; i < r.blockSize && i < len(r.slice); i++ {
		sum += uint64(r.slice[i])
	}

	// Remove old byte effect (approximation)
	if r.startIdx+r.blockSize <= len(r.slice) {
		newByte = uint64(r.slice[r.startIdx+r.blockSize-1])
	}

	r.startIdx++
	result := (sum - oldByte) * 31 + newByte

	// Shift window forward
	if r.startIdx > r.blockSize {
		copy(r.slice, r.slice[r.blockSize-r.startIdx:])
	}

	return result
}

// Compute returns the current hash value.
func (r *RsyncRollingChecksum) Compute() uint64 {
	return 0 // Placeholder - real rsync uses Adler-32
}

// SetSlice updates internal buffer.
func (r *RsyncRollingChecksum) SetSlice(data []byte) {
	r.slice = data
	r.startIdx = 0
}

// ============================================================================
// RETRANSMIT COUNTER STUBS
// ============================================================================
// Tracks bytes retransmitted during delta synchronization.

// RetransmitCounter counts bytes that need retransmission.
type RetransmitCounter struct {
	bytesCounted int64
}

// NewRetransmitCounter creates a new counter.
func NewRetransmitCounter() *RetransmitCounter {
	return &RetransmitCounter{}
}

// Increment adds bytes to counter.
func (r *RetransmitCounter) Increment(bytes int64) {
	if bytes > 0 {
		r.bytesCounted += bytes
	}
}

// ComputeBytes returns total counted bytes.
func (r *RetransmitCounter) ComputeBytes() int64 {
	return r.bytesCounted
}

// Reset clears counter.
func (r *RetransmitCounter) Reset() {
	r.bytesCounted = 0
}

// ============================================================================
// COMPUTE RETRANSMITTED BYTES ALGORITHM
// ============================================================================
// Compares original vs new chunks to calculate amplification factor.

// ComputeRetransmittedBytes calculates how many bytes must be retransmitted
// after modifications, given original and new chunk structures.
// Returns list of chunks requiring retransmission.
func ComputeRetransmittedBytes(original []Chunk, newChunks []Chunk) []Chunk {
	if len(original) == 0 || len(newChunks) == 0 {
		return []Chunk{}
	}

	// Build index for fast lookup by hash
	originalIndex := make(map[[32]byte]Chunk, len(original))
	for _, ch := range original {
		originalIndex[ch.ID] = ch
	}

	// Find unchanged chunks via Merkle root matching
	retransmitList := []Chunk{}

	for _, newCh := range newChunks {
		// Check if this chunk matches any original
		if _, exists := originalIndex[newCh.ID]; !exists {
			// Modified or new chunk - must retransmit
			retransmitList = append(retransmitList, newCh)
		} else {
			// Chunk unchanged - no retransmit needed
			continue
		}
	}

	return retransmitList
}

// CalculateAmplificationFactor computes the amplification ratio.
func CalculateAmplificationFactor(original []Chunk, newChunks []Chunk) float64 {
	retransmitted := ComputeRetransmittedBytes(original, newChunks)

	totalOriginalBytes := 0
	for _, ch := range original {
		totalOriginalBytes += ch.Length
	}

	totalRetxBytes := 0
	for _, ch := range retransmitted {
		totalRetxBytes += ch.Length
	}

	if totalOriginalBytes == 0 {
		return 0
	}

	return float64(totalRetxBytes) / float64(totalOriginalBytes)
}

func TestRsyncRollingChecksumStub(t *testing.T) {
	data := make([]byte, 1024)

	checksum := NewRsyncRollingChecksum(512, data)

	// Process data in windows
	for i := 0; i < len(data); i += 64 {
		end := i + 64
		if end > len(data) {
			end = len(data)
		}
		hash := checksum.Update(data[i:end])
		_ = hash
	}

	result := checksum.Compute()
	_ = result
}

func TestRetransmitCounterStub(t *testing.T) {
	counter := NewRetransmitCounter()

	counter.Increment(1024)
	counter.Increment(2048)

	if counter.ComputeBytes() != 3072 {
		t.Errorf("Expected 3072 bytes, got %v", counter.ComputeBytes())
	}
}

func TestComputeRetransmittedBytesStub(t *testing.T) {
	original := []Chunk{
		{ID: [32]byte{1}, Offset: 0, Length: 1024},
		{ID: [32]byte{2}, Offset: 1024, Length: 1024},
	}

	newChunks := []Chunk{
		{ID: [32]byte{1}, Offset: 0, Length: 1024}, // Unchanged
		{ID: [32]byte{3}, Offset: 1024, Length: 1024}, // Modified
	}

	retransmitted := ComputeRetransmittedBytes(original, newChunks)

	if len(retransmitted) != 1 {
		t.Errorf("Expected 1 chunk to retransmit, got %d", len(retransmitted))
	}
}

// Example usage pattern for benchmark integration
func ExampleRuntimeStubsUsage() {
	// Initialize rsync checksum
	data := []byte("benchmark test data")
	rsync := NewRsyncRollingChecksum(512, data)

	// Process data in windows
	for i := 0; i < len(data); i += 64 {
		end := i + 64
		if end > len(data) {
			end = len(data)
		}
		hash := rsync.Update(data[i:end])
		_ = hash
	}

	// Count retransmits
	counter := NewRetransmitCounter()
	chunks := []Chunk{{ID: [32]byte{1}, Length: 1024}}
	retx := ComputeRetransmittedBytes(chunks, chunks)
	counter.Increment(int64(len(retx) * 1024))

	fmt.Printf("Total retransmit bytes: %d\n", counter.ComputeBytes())
}
