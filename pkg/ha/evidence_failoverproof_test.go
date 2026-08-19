package ha

import (
	"testing"
	"time"
)

func TestEvidenceHAEngine_ReceiptSigned(t *testing.T) {
	e := NewEvidenceHAEngine()
	start := time.Now()
	end := start.Add(10 * time.Second)
	res, err := e.RecordFailover(start, end)
	if err != nil {
		t.Fatalf("RecordFailover: %v", err)
	}
	if res.Receipt == nil || !res.Receipt.Verify() {
		t.Fatal("expected a verifiable receipt")
	}
	if res.Receipt.Module != "ha" {
		t.Errorf("unexpected module %q", res.Receipt.Module)
	}
}

func TestEvidenceHAEngine_RTOTracking(t *testing.T) {
	e := NewEvidenceHAEngine()
	e.SetPromisedRTO(30 * time.Second)

	// Within RTO => green
	start := time.Now()
	end := start.Add(20 * time.Second)
	res, _ := e.RecordFailover(start, end)
	if res.SLARiskStatus != "green" {
		t.Errorf("expected green, got %q (actual RTO=%v)", res.SLARiskStatus, res.ActualRTO)
	}

	// Beyond RTO => red
	start = time.Now()
	end = start.Add(40 * time.Second)
	res2, _ := e.RecordFailover(start, end)
	if res2.SLARiskStatus != "red" {
		t.Errorf("expected red, got %q", res2.SLARiskStatus)
	}
	if !res2.AlertOnBreach {
		t.Error("must alert on red/yellow breach")
	}
}

func TestEvidenceHAEngine_SLAYellowAlert(t *testing.T) {
	e := NewEvidenceHAEngine()
	e.SetPromisedRTO(10 * time.Second)

	// 20% above RTO => yellow
	start := time.Now()
	end := start.Add(12 * time.Second)
	res, _ := e.RecordFailover(start, end)
	if res.SLARiskStatus != "yellow" {
		t.Errorf("expected yellow, got %q", res.SLARiskStatus)
	}
}
