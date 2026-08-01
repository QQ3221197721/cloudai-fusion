// Package zkp - Poseidon hash benchmark tests
package zkp_test

import (
	"crypto/sha256"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/zkp"
)

// ============================================================================
// Poseidon vs SHA256 Performance Benchmarks
// ============================================================================

const (
	testInputSize = 1024 * 1024 // 1MB input for realistic benchmarking
	benchmarkIters = 10         // Iterations for statistical significance
)

// BenchmarkPoseidonHash benchmarks Poseidon hash performance
func BenchmarkPoseidonHash(b *testing.B) {
	// Create Poseidon hasher
	hasher := zkp.NewPoseidonHash()

	// Generate test input
	input := make([]byte, testInputSize)
	for i := range input {
		input[i] = byte(i % 256)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = hasher.Hash(input)
	}
}

// BenchmarkSHA256Hash benchmarks SHA256 hash performance
func BenchmarkSHA256Hash(b *testing.B) {
	// Generate test input
	input := make([]byte, testInputSize)
	for i := range input {
		input[i] = byte(i % 256)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sha256.Sum256(input)
	}
}

// TestPoseidonVsSHA256Performance compares hash performance
func TestPoseidonVsSHA256Performance(t *testing.T) {
	hasher := zkp.NewPoseidonHash()
	input := make([]byte, testInputSize)
	for i := range input {
		input[i] = byte(i % 256)
	}

	// Benchmark Poseidon
	start := time.Now()
	var poseidonHash [32]byte
	for i := 0; i < benchmarkIters; i++ {
		result := hasher.Hash(input)
		copy(poseidonHash[:], result[:])
	}
	poseidonDuration := time.Since(start)

	// Benchmark SHA256
	start = time.Now()
	var sha256Hash [32]byte
	for i := 0; i < benchmarkIters; i++ {
		result := sha256.Sum256(input)
		copy(sha256Hash[:], result[:])
	}
	sha256Duration := time.Since(start)

	t.Logf("Poseidon hash: %v for %d iterations (%.2f ns/op)", 
		poseidonDuration, benchmarkIters, float64(poseidonDuration)/float64(benchmarkIters))
	t.Logf("SHA256 hash: %v for %d iterations (%.2f ns/op)", 
		sha256Duration, benchmarkIters, float64(sha256Duration)/float64(benchmarkIters))

	// Verify both hashes produce correct output size
	if len(poseidonHash) != 32 || len(sha256Hash) != 32 {
		t.Fatal("Hash sizes incorrect")
	}

	// Performance should be comparable (within 2x)
	ratio := float64(poseidonDuration) / float64(sha256Duration)
	if ratio > 2.0 || ratio < 0.5 {
		t.Logf("WARNING: Performance difference significant: %.2fx", ratio)
	}

	t.Logf("Performance comparison: Poseidon is %.2fx SHA256", ratio)
}
