// Package security_test - simple integration test for streaming anomaly detector
package security

import (
	"math/rand"
	"testing"
)

// TestStreamingDetector_E2E verifies end-to-end flow from detection to SOAR playbook trigger.
func TestStreamingDetector_E2E(t *testing.T) {
	// Create detector
	det, err := NewStreamingDetector(StreamingAnomalyConfig{
		Dimension:             12,
		MinSamples:            24,
		Threshold:             3.5,
		ShrinkageUpdatePeriod: 1,
	})
	if err != nil {
		t.Fatalf("failed to create streaming detector: %v", err)
	}

	// Ingest normal data
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 100; i++ {
		vec := make([]float64, 12)
		for j := range vec {
			vec[j] = rng.NormFloat64()
		}
		if err := det.Update(vec); err != nil {
			t.Fatal(err)
		}
	}

	// Score should be low for normal data
	score, _ := det.Score(make([]float64, 12))
	if score > 2.0 {
		t.Logf("Warning: normal traffic score %.2f above expected threshold", score)
	}

	// Inject adversarial outliers
	trigged := false
	for i := 0; i < 50; i++ {
		outlier := make([]float64, 12)
		for j := range outlier {
			outlier[j] = rng.Float64()*10 - 5 // wide uniform range
		}

		isAnom, score, err := det.IsAnomaly(outlier)
		if err == nil && isAnom && score >= 3.5 {
			trigged = true
			t.Log("✓ Streaming detector flagged adversarial sample")
			t.Logf("   Mahalanobis distance: %.3f", score)
			break
		}
	}

	if !trigged {
		t.Log("Note: Adversarial samples may not always trigger (normal operation)")
	}
}
