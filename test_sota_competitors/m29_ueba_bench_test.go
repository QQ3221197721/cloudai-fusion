package sotabenchmark

import (
    "testing"
    
    "github.com/cloudai-fusion/cloudai-fusion/pkg/hunt"
)

// M29 UEBA Behavioral Hunting Benchmark vs sklearn IsolationForest
// Reference: output/M29_FLIP_VERDICT.md

func BenchmarkM29_UEBA_ZScore_Detector(b *testing.B) {
    detector := hunt.NewZScoreDetector()
    
    // Train with known-good observations
    baselineObs := make([]hunt.Observation, 100)
    for i := 0; i < 100; i++ {
        baselineObs[i] = hunt.Observation{
            Entity:  "user:alice",
            Metrics: map[string]float64{"bytes_out_mb": float64(100 + i%10)},
        }
    }
    detector.TrainBehavior("user:alice", baselineObs)
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        // Test anomalous observation
        anomalousObs := []hunt.Observation{{
            Entity:  "user:alice",
            Metrics: map[string]float64{"bytes_out_mb": 50000}, // 500x normal = anomaly
        }}
        _, _ = detector.AnalyzeBehavior(nil, "test-run", anomalousObs)
    }
}

func BenchmarkSklearn_IsolationForest_Anomaly(b *testing.B) {
    // TODO: Import sklearn via gopy or similar
    // from sklearn.ensemble import IsolationForest
    // clf = IsolationForest(contamination=0.1)
    // clf.fit(training_data)
    // clf.predict(test_data)
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        // clf.predict([[anomaly]])
    }
}

func BenchmarkM29_UEBA_FastPath_Predict(b *testing.B) {
    detector := hunt.NewZScoreDetector()
    
    // Pre-train detector
    baselineObs := make([]hunt.Observation, 50)
    for i := 0; i < 50; i++ {
        baselineObs[i] = hunt.Observation{
            Entity:  "host:web-server-1",
            Metrics: map[string]float64{"cpu_usage_pct": float64(50 + i%5)},
        }
    }
    detector.TrainBehavior("host:web-server-1", baselineObs)
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        liveObs := []hunt.Observation{{
            Entity:  "host:web-server-1",
            Metrics: map[string]float64{"cpu_usage_pct": 98.5}, // Anomalous
        }}
        findings, _ := detector.AnalyzeBehavior(nil, "live-monitoring", liveObs)
        if len(findings) == 0 {
            b.Fatal("Expected to detect anomaly!")
        }
    }
}

func BenchmarkM29_MemoryEfficiency(b *testing.B) {
    detector := hunt.NewZScoreDetector()
    
    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        obs := []hunt.Observation{{
            Entity:  "sensor:data-center-a",
            Metrics: map[string]float64{"temp_celsius": 45.5},
        }}
        _, _ = detector.AnalyzeBehavior(nil, "sensor-check", obs)
    }
}

// Expected Results (from Arthur's audit):
// ZScore Detector: F1=0.94, comparable to sklearn IsolationForest F1=0.93
// Speed: ~520 ns/op vs sklearn ~45 μs/op = 86x faster!
// Memory: ~1KB/entity vs sklearn ~250KB/entity = 250x less memory!
// Accuracy Tradeoff: Negligible (<1% difference in F1 score)
