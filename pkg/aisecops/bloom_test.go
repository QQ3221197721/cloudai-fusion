package aisecops_test

import (
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/aisecops"
)

func TestBloomFilter_AddAndMayContain(t *testing.T) {
	bf := aisecops.NewBloomFilter(100, 0.01)
	if bf == nil {
		t.Fatal("NewBloomFilter returned nil")
	}

	bf.Add([]byte("hello"))
	bf.Add([]byte("world"))

	// Added items must always be reported present (no false negatives).
	if !bf.MayContain([]byte("hello")) {
		t.Error("Expected 'hello' to be in bloom filter")
	}
	if !bf.MayContain([]byte("world")) {
		t.Error("Expected 'world' to be in bloom filter")
	}
}

func TestBloomFilter_NoFalseNegatives(t *testing.T) {
	bf := aisecops.NewBloomFilter(1000, 0.01)

	items := [][]byte{
		[]byte("malicious-pattern-1"),
		[]byte("sql-injection"),
		[]byte("xss-attempt"),
		[]byte("path-traversal"),
	}
	for _, item := range items {
		bf.Add(item)
	}

	for _, item := range items {
		if !bf.MayContain(item) {
			t.Errorf("False negative for %q — bloom filters must never have false negatives", item)
		}
	}
}

func TestBloomFilter_FalsePositiveRate(t *testing.T) {
	bf := aisecops.NewBloomFilter(1000, 0.01)
	for i := 0; i < 500; i++ {
		bf.Add([]byte{byte(i), byte(i >> 8)})
	}

	fpr := bf.FalsePositiveRate()
	if fpr < 0 || fpr > 1.0 {
		t.Errorf("False positive rate out of range: %f", fpr)
	}
}

// BenchmarkBloomFilter_Add measures throughput of inserting items into the filter.
func BenchmarkBloomFilter_Add(b *testing.B) {
	bf := aisecops.NewBloomFilter(b.N+1, 0.01)
	item := []byte("malicious-pattern-benchmark")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bf.Add(item)
	}
}

// BenchmarkBloomFilter_MayContain measures the pre-screening lookup hot path.
func BenchmarkBloomFilter_MayContain(b *testing.B) {
	bf := aisecops.NewBloomFilter(10000, 0.01)
	for i := 0; i < 1000; i++ {
		bf.Add([]byte{byte(i), byte(i >> 8)})
	}
	probe := []byte("sql-injection")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bf.MayContain(probe)
	}
}
