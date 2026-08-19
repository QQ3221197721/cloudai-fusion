package aisecops

import (
	"hash/fnv"
	"math"
	"sync"
)

// BloomFilter implements a simple bloom filter for security pre-screening
type BloomFilter struct {
	bits    []uint64
	k       int  // number of hash functions
	m       int  // bit array size (in bits)
	n       int  // expected number of items
	mu      sync.RWMutex
}

// NewBloomFilter creates a bloom filter with optimal parameters
func NewBloomFilter(expectedItems int, fpRate float64) *BloomFilter {
	if expectedItems < 1 {
		expectedItems = 1000
	}
	if fpRate <= 0 || fpRate > 0.5 {
		fpRate = 0.01 // Default 1% false positive rate
	}

	// Optimal m and k calculation
	m := int(-float64(expectedItems)*math.Log(fpRate)/math.Pow(2, 2))
	if m < 1 {
		m = 8192
	}

	k := int(math.Floor(float64(m)/float64(expectedItems) * math.Ln2))
	if k < 1 {
		k = 3
	}

	return &BloomFilter{
		bits: make([]uint64, m/64+1),
		k:    k,
		m:    m,
		n:    expectedItems,
	}
}

// Add adds an item to the bloom filter
func (bf *BloomFilter) Add(item []byte) {
	bf.mu.Lock()
	defer bf.mu.Unlock()

	for i := 0; i < bf.k; i++ {
		hash := bf.hash(item, uint64(i))
		bitPos := hash % uint64(bf.m)
		bf.setBit(bitPos)
	}
}

// MayContain returns true if item might exist (false positives possible)
func (bf *BloomFilter) MayContain(item []byte) bool {
	bf.mu.RLock()
	defer bf.mu.RUnlock()

	for i := 0; i < bf.k; i++ {
		hash := bf.hash(item, uint64(i))
		bitPos := hash % uint64(bf.m)
		if !bf.getBit(bitPos) {
			return false
		}
	}
	return true
}

// FalsePositiveRate returns the current false positive rate
func (bf *BloomFilter) FalsePositiveRate() float64 {
	bf.mu.RLock()
	defer bf.mu.RUnlock()

	if len(bf.bits) == 0 {
		return 0
	}

	zeroBits := bf.countZeroBits()
	rate := math.Pow(1.0-float64(zeroBits)/float64(bf.m), float64(bf.k))
	return rate
}

// countZeroBits counts how many bits are still zero
func (bf *BloomFilter) countZeroBits() int {
	var zeroCount int
	for _, word := range bf.bits {
		// Count zeros using population count trick
		wordCopy := word
		setBits := 0
		for wordCopy > 0 {
			wordCopy &= wordCopy - 1
			setBits++
		}
		zeroCount += 64 - setBits
	}
	return zeroCount
}

// hash generates k different hashes for an item
func (bf *BloomFilter) hash(item []byte, seed uint64) uint64 {
	h := fnv.New64a()
	h.Write(item)
	baseHash := h.Sum64()

	// FNV hash with seed mixing (double hashing technique)
	result := baseHash + seed
	result ^= result >> 33
	result *= 0xff51afd7ed558ccd
	result ^= result >> 33
	result *= 0xc4ceb9fe1a85ec53
	result ^= result >> 33

	return result
}

// setBit sets a specific bit
func (bf *BloomFilter) setBit(pos uint64) {
	wordIdx := pos / 64
	bitIdx := pos % 64
	bf.bits[wordIdx] |= (1 << bitIdx)
}

// getBit gets a specific bit
func (bf *BloomFilter) getBit(pos uint64) bool {
	wordIdx := pos / 64
	bitIdx := pos % 64
	return (bf.bits[wordIdx] & (1 << bitIdx)) != 0
}
