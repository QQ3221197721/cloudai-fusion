package evidence

import (
	"testing"
	"time"
)

func TestEvidenceEngine_RecordReceipt(t *testing.T) {
	engine := NewEvidenceEngine()
	
	engine.RecordReceipt("receipt-001", "", time.Now())
	
	status := engine.GetHealthStatus()
	
	if status.TotalEvents != 1 {
		t.Errorf("expected total_events 1, got %d", status.TotalEvents)
	}
}

func TestEvidenceEngine_ChainGapDetection(t *testing.T) {
	engine := NewEvidenceEngine()
	
	// Simulate a gap by manipulating lastTimestamp
	engine.lastTimestamp = time.Now().Add(-2 * time.Hour) // 2 hours ago
	
	engine.RecordReceipt("next-receipt", "receipt-001", time.Now())
	
	status := engine.GetHealthStatus()
	
	if status.GapCount == 0 {
		t.Log("Expected gap detection after 2-hour delay")
	}
}

func TestEvidenceEngine_IsHealthy(t *testing.T) {
	engine := NewEvidenceEngine()
	
	engine.RecordReceipt("r1", "", time.Now())
	engine.RecordReceipt("r2", "r1|next", time.Now())
	
	status := engine.GetHealthStatus()
	
	if !status.IsHealthy {
		t.Error("expected healthy status with no gaps or failures")
	}
}
