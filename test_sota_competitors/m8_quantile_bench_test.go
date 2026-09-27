package sotabenchmark

import (
    "testing"
    
    "github.com/cloudai-fusion/cloudai-fusion/pkg/metrics"
)

// M8 HybridQuantile vs Google PolyPhase Benchmark
// Reference: output/M8_FLIP_VERDICT.md

func BenchmarkM8_HybridQuantile_Insert(b *testing.B) {
    hq := metrics.NewHybridQuantile()
    rng := rand.New(rand.NewSource(42))
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        hq.Insert(rng.Float64())
    }
}

func BenchmarkM8_Google_PolyPhase_Insert(b *testing.B) {
    // TODO: Import google/sketches polyphase
    // gs := sketches.NewPolySketch(0.001)
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        // gs.Add(rng.Float64())
    }
}

func BenchmarkM8_HybridQuantile_QueryP50(b *testing.B) {
    hq := metrics.NewHybridQuantile()
    
    // Pre-populate with samples
    for i := 0; i < 10000; i++ {
        hq.Insert(float64(i) / 10000.0)
    }
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _ = hq.P50()
    }
}

func BenchmarkM8_HybridQuantile_QueryP95(b *testing.B) {
    hq := metrics.NewHybridQuantile()
    
    for i := 0; i < 10000; i++ {
        hq.Insert(float64(i) / 10000.0)
    }
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _ = hq.P95()
    }
}

func BenchmarkM8_HybridQuantile_Memory(b *testing.B) {
    hq := metrics.NewHybridQuantile()
    
    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        hq.Insert(0.5)
        _ = hq.P50()
    }
}
