package ha

// evidence_failoverproof.go signs failover events and tracks actual RTO vs promised
// SLA risk using timestamps recorded during each failover.
//
// Innovation — Recovery Time Objective Tracking:
// Each failover records start and end times; the difference gives actual recovery time.
// This is compared against a promised RTO threshold to compute SLA risk: green if
// safe, yellow approaching limit, red at/beyond breach. Alerts on any red/yellow.

import (
	"crypto/ed25519"
	"crypto/rand"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

const evidenceDefaultRTO = 30 * time.Second

// EvidenceFailoverResult is the signed outcome of a failover event.
type EvidenceFailoverResult struct {
	EventID        string            `json:"event_id"`
	PromisedRTO    time.Duration     `json:"promised_rto"`
	ActualRTO      time.Duration     `json:"actual_rto"`
	SLARiskStatus  string            `json:"sla_risk_status"` // "green"/"yellow"/"red"
	AlertOnBreach  bool              `json:"alert_on_breach"`
	Receipt        *evidence.Receipt `json:"receipt"`
}

// EvidenceHAEngine wraps failover events with receipts and RTO tracking.
type EvidenceHAEngine struct {
	receiptBuilder *evidence.ReceiptBuilder
	promisedRTO    time.Duration
	lastEventID    string
	nextID         int
	windowMinutes  float64
}

// NewEvidenceHAEngine constructs an engine with fresh Ed25519 key.
func NewEvidenceHAEngine() *EvidenceHAEngine {
	_, privKey, _ := ed25519.GenerateKey(rand.Reader)
	return &EvidenceHAEngine{
		receiptBuilder: evidence.NewReceiptBuilder("ha", privKey),
		promisedRTO:    evidenceDefaultRTO,
		nextID:         1,
		windowMinutes:  60,
	}
}

// SetPromisedRTO sets the expected maximum recovery time for RTO comparison.
func (e *EvidenceHAEngine) SetPromisedRTO(rto time.Duration) {
	e.promisedRTO = rto
}

// RecordFailover attests a failover event by recording timestamps and comparing
// actual recovery time against the promised RTO.
func (e *EvidenceHAEngine) RecordFailover(startTime, endTime time.Time) (*EvidenceFailoverResult, error) {
	actualRTO := endTime.Sub(startTime)
	var status string
	// 120% threshold using seconds calculation
	yellowThreshold := time.Duration(float64(e.promisedRTO.Seconds())*1.2)*time.Second
	if actualRTO <= e.promisedRTO {
		status = "green"
	} else if actualRTO <= yellowThreshold {
		status = "yellow"
	} else {
		status = "red"
	}

	alertOnBreach := status == "red" || status == "yellow"

	eventID := "ev-" + e.nextIDToString()
	e.nextID++

	input := map[string]interface{}{
		"start":   startTime.Format(time.RFC3339),
		"end":     endTime.Format(time.RFC3339),
		"actual_ms": actualRTO.Milliseconds(),
		"promised_ms": e.promisedRTO.Milliseconds(),
		"risk": status,
	}
	output := map[string]interface{}{"status": status, "alert": alertOnBreach}
	receipt, err := e.receiptBuilder.Build("ha.failover", input, output)
	if err != nil {
		return nil, err
	}

	return &EvidenceFailoverResult{
		EventID:       eventID,
		PromisedRTO:   e.promisedRTO,
		ActualRTO:     actualRTO,
		SLARiskStatus: status,
		AlertOnBreach: alertOnBreach,
		Receipt:       receipt,
	}, nil
}

func (e *EvidenceHAEngine) nextIDToString() string {
	if e.nextID < 1000 {
		return "000" + itoa(e.nextID)
	}
	return itoa(e.nextID)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []rune{}
	for n := i; n > 0; n /= 10 {
		digits = append([]rune{rune('0' + n%10)}, digits...)
	}
	return string(digits)
}
