package cloud

import "testing"

func TestEvidenceCloudEngine_ReceiptSigned(t *testing.T) {
	e := NewEvidenceCloudEngine()
	res, err := e.RecordCloudOperation("aws", "create_cluster", "prod-1", 1200.0)
	if err != nil {
		t.Fatalf("RecordCloudOperation: %v", err)
	}
	if res.Receipt == nil {
		t.Fatal("expected a receipt")
	}
	if !res.Receipt.Verify() {
		t.Error("receipt signature must verify")
	}
	if res.Receipt.Module != "cloud" || res.Receipt.Operation != "cloud.operation" {
		t.Errorf("unexpected receipt metadata: %+v", res.Receipt)
	}
}

func TestEvidenceCloudEngine_CostAnomalyZScore(t *testing.T) {
	e := NewEvidenceCloudEngine()
	// Establish a stable baseline around ~100/day.
	baseline := []float64{100, 102, 98, 101, 99, 103, 97}
	for _, s := range baseline {
		if _, err := e.RecordCloudOperation("gcp", "list", "n/a", s); err != nil {
			t.Fatalf("baseline: %v", err)
		}
	}

	// A normal day should not be flagged.
	normal, _ := e.RecordCloudOperation("gcp", "list", "n/a", 100.0)
	if normal.Anomaly.IsAnomaly {
		t.Errorf("normal spend flagged as anomaly (z=%.2f)", normal.Anomaly.ZScore)
	}

	// A runaway day (10x) must be flagged.
	spike, _ := e.RecordCloudOperation("gcp", "list", "n/a", 1000.0)
	if !spike.Anomaly.IsAnomaly {
		t.Errorf("spike not flagged (z=%.2f, mean=%.2f, std=%.2f)",
			spike.Anomaly.ZScore, spike.Anomaly.Mean, spike.Anomaly.StdDev)
	}
	if spike.Anomaly.ZScore <= evidenceCostZThreshold {
		t.Errorf("expected large z-score for spike, got %.2f", spike.Anomaly.ZScore)
	}
}

func TestEvidenceCloudEngine_InsufficientHistory(t *testing.T) {
	e := NewEvidenceCloudEngine()
	res, _ := e.RecordCloudOperation("azure", "create", "x", 500.0)
	if res.Anomaly.IsAnomaly {
		t.Error("must not flag anomaly without sufficient history")
	}
}
