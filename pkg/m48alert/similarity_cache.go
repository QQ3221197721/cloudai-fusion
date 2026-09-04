package m48alert

import (
	"hash/fnv"
	"sync"
)

// SimilarityCache stores pre-computed Jaccard scores for alert pairs.
type SimilarityCache struct {
	size    int
	mu      sync.RWMutex
	entries map[string]float64 // fingerprint pair → Jaccard score
	lru     []string           // eviction order
}

// NewSimilarityCache creates a new LRU-backed similarity cache.
func NewSimilarityCache(size int) *SimilarityCache {
	if size <= 0 {
		size = 10000
	}
	return &SimilarityCache{
		size:    size,
		entries: make(map[string]float64),
		lru:     make([]string, 0, size),
	}
}

// Get retrieves a cached similarity score.
func (sc *SimilarityCache) Get(fp1, fp2 string) (float64, bool) {
	if fp1 > fp2 {
		fp1, fp2 = fp2, fp1
	}
	key := sc.makeKey(fp1, fp2)

	sc.mu.RLock()
	defer sc.mu.RUnlock()

	score, ok := sc.entries[key]
	if ok {
		sc.moveToFront(key)
	}
	return score, ok
}

// Set stores a similarity score.
func (sc *SimilarityCache) Set(fp1, fp2 string, score float64) {
	if fp1 > fp2 {
		fp1, fp2 = fp2, fp1
	}
	key := sc.makeKey(fp1, fp2)

	sc.mu.Lock()
	defer sc.mu.Unlock()

	if _, exists := sc.entries[key]; exists {
		sc.moveToFront(key)
		sc.entries[key] = score
		return
	}

	// Evict if at capacity
	for len(sc.lru) >= sc.size {
		evict := sc.lru[len(sc.lru)-1]
		sc.lru = sc.lru[:len(sc.lru)-1]
		delete(sc.entries, evict)
	}

	sc.lru = append(sc.lru, key)
	sc.entries[key] = score
}

// makeKey creates deterministic pair key.
func (sc *SimilarityCache) makeKey(fp1, fp2 string) string {
	h := fnv.New64a()
	h.Write([]byte(fp1))
	h.Write([]byte(fp2))
	return string(h.Sum(nil))
}

// moveToFront updates LRU order.
func (sc *SimilarityCache) moveToFront(key string) {
	for i, k := range sc.lru {
		if k == key {
			sc.lru = append(sc.lru[:i], sc.lru[i+1:]...)
			sc.lru = append(sc.lru, key)
			break
		}
	}
}
