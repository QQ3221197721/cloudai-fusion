package mesh

import (
	"strconv"
	"sync"
	"testing"
)

// naive competitor: RWMutex-guarded map (typical hand-rolled registry)
type mutexRegistry struct {
	mu sync.RWMutex
	m  map[string]*EndpointSet
}

func newMutexRegistry() *mutexRegistry { return &mutexRegistry{m: make(map[string]*EndpointSet)} }
func (r *mutexRegistry) Register(s string, set *EndpointSet) {
	r.mu.Lock()
	r.m[s] = set
	r.mu.Unlock()
}
func (r *mutexRegistry) Lookup(s string) *EndpointSet {
	r.mu.RLock()
	v := r.m[s]
	r.mu.RUnlock()
	return v
}

func benchSetup(n int) (*Registry, *mutexRegistry, []string) {
	cow := NewRegistry()
	mtx := newMutexRegistry()
	keys := make([]string, n)
	for i := 0; i < n; i++ {
		k := "svc-" + strconv.Itoa(i)
		keys[i] = k
		set := NewEndpointSet(NewEndpoint("e0", "10.0.0.1:80", 1))
		cow.Register(k, set)
		mtx.Register(k, set)
	}
	return cow, mtx, keys
}

func benchCOW(b *testing.B, n int) {
	cow, _, keys := benchSetup(n)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cow.Lookup(keys[i%n])
	}
}
func benchMTX(b *testing.B, n int) {
	_, mtx, keys := benchSetup(n)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mtx.Lookup(keys[i%n])
	}
}

func BenchmarkCOWLookup_100(b *testing.B)   { benchCOW(b, 100) }
func BenchmarkCOWLookup_1k(b *testing.B)     { benchCOW(b, 1000) }
func BenchmarkCOWLookup_10k(b *testing.B)    { benchCOW(b, 10000) }
func BenchmarkMutexLookup_100(b *testing.B) { benchMTX(b, 100) }
func BenchmarkMutexLookup_1k(b *testing.B)   { benchMTX(b, 1000) }
func BenchmarkMutexLookup_10k(b *testing.B)  { benchMTX(b, 10000) }

// concurrent read contention: COW (lock-free) should pull ahead
func BenchmarkCOWLookupParallel_1k(b *testing.B) {
	cow, _, keys := benchSetup(1000)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_ = cow.Lookup(keys[i%1000])
			i++
		}
	})
}
func BenchmarkMutexLookupParallel_1k(b *testing.B) {
	_, mtx, keys := benchSetup(1000)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_ = mtx.Lookup(keys[i%1000])
			i++
		}
	})
}
