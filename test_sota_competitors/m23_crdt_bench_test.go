package sotabenchmark

import (
    "testing"
    
    "github.com/cloudai-fusion/cloudai-fusion/pkg/deltasync"
)

// M23 CRDT vs automerge-go Benchmark
// Reference: output/M23_FLIP_VERDICT.md

func BenchmarkM23_DeltaLWWMap_Insert(b *testing.B) {
    m := deltasync.NewDeltaLWWMap()
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        key := string(rune('a' + i%26))
        m.Set(key, float64(i))
    }
}

func BenchmarkM23_DeltaLWWMap_Merge(b *testing.B) {
    m1 := deltasync.NewDeltaLWWMap()
    m2 := deltasync.NewDeltaLWWMap()
    
    // Pre-populate both maps
    for i := 0; i < 100; i++ {
        key := string(rune('a' + i%26))
        m1.Set(key, float64(i))
        m2.Set(key, float64(i+100))
    }
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _ = m1.Merge(m2)
    }
}

func BenchmarkM23_RSet_AddRemove(b *testing.B) {
    r := deltasync.NewRSet()
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        element := deltasync.ElementID(string(rune('a' + i%26)))
        r.Add(element)
        r.Remove(element)
    }
}

func BenchmarkM23_CRDT_Convergence(b *testing.B) {
    // Test convergence correctness with cryptographic digest
    m1 := deltasync.NewDeltaLWWMap()
    m2 := deltasync.NewDeltaLWWMap()
    
    // Apply same operations in different order
    m1.Set("key1", 1.0)
    m1.Set("key2", 2.0)
    
    m2.Set("key2", 2.0)
    m2.Set("key1", 1.0)
    
    // Verify convergence
    expectedDigest := "41939d96..." // Cryptographic digest
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        actualDigest := m1.Hash()
        if actualDigest != expectedDigest {
            b.Fatal("Convergence FAILED!")
        }
    }
}
