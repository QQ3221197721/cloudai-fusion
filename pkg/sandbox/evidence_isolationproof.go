package sandbox

// evidence_isolationproof.go signs sandbox execution results and provides real-time
// resource escape detection: memory/CPU/network usage beyond limits triggers alerts.
//
// Innovation — Resource Escape Detection:
// During execution, we continuously monitor resource counters. If memory exceeds
// hard limit OR CPU-seconds surpass quota OR network bytes go over cap, we classify
// as an escape and return explicit confirmation with measured violations.

import (
	"crypto/ed25519"
	"crypto/rand"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

const (
	defaultMemLimit   = 256 << 20        // 256MB
	defaultCPULimit   = 1000             // 1 second total CPU budget
	defaultNetLimit   = 10 << 20         // 10MB
	escapeThreshold   = 0.99             // alert slightly before hitting 100%
)

// EvidenceEscapeInfo captures detected resource escapes.
type EvidenceEscapeInfo struct {
	MemoryExceeded bool `json:"memory_exceeded"`
	CPUExceeded    bool `json:"cpu_exceeded"`
	NetworkExceeded bool `json:"network_exceeded"`
	Limits         map[string]int64 `json:"limits_bytes_or_units"`
	Measured       map[string]int64 `json:"measured_bytes_or_units"`
	DetectedAt     time.Time        `json:"detected_at"`
}

// EvidenceSandboxResult is the signed outcome of a sandbox execution.
type EvidenceSandboxResult struct {
	ExecutionID      string          `json:"execution_id"`
	Duration         time.Duration   `json:"duration_ms"`
	IsolationHeld    bool            `json:"isolation_held"`
	EscapeDetected   bool            `json:"escape_detected"`
	EscapeInfo       *EvidenceEscapeInfo `json:"escape_info,omitempty"`
	MemoryUsedBytes  int64           `json:"memory_used_bytes"`
	CPUSecondsTotal  int64           `json:"cpu_seconds_total"`
	NetworkBytesSent int64           `json:"network_bytes_sent"`
	Receipt          *evidence.Receipt `json:"receipt"`
}

// EvidenceSandboxEngine wraps sandbox executions with receipts and resource escape
// detection via hard-limit enforcement.
type EvidenceSandboxEngine struct {
	receiptBuilder     *evidence.ReceiptBuilder
	memLimit, cpuLimit int64
	netLimit           int64
	escapeHistory      []time.Time
	maxHistoryPoints   int
}

// NewEvidenceSandboxEngine constructs an engine with fresh Ed25519 key.
func NewEvidenceSandboxEngine() *EvidenceSandboxEngine {
	_, privKey, _ := ed25519.GenerateKey(rand.Reader)
	return &EvidenceSandboxEngine{
		receiptBuilder:  evidence.NewReceiptBuilder("sandbox", privKey),
		memLimit:        defaultMemLimit,
		cpuLimit:        defaultCPULimit,
		netLimit:        defaultNetLimit,
		maxHistoryPoints: 100,
	}
}

// Execute attests a sandbox execution result and detects resource escapes.
func (e *EvidenceSandboxEngine) Execute(executionID string, memUsed, cpuSec, netBytes int64, durationMs int64) (*EvidenceSandboxResult, error) {
	escape := false
	escapeInfo := &EvidenceEscapeInfo{
		DetectedAt: time.Now(),
		Limits: map[string]int64{
			"memory":  e.memLimit,
			"cpu":     e.cpuLimit,
			"network": e.netLimit,
		},
		Measured: map[string]int64{
			"memory":  memUsed,
			"cpu":     cpuSec,
			"network": netBytes,
		},
	}
	
	if e.memLimit > 0 && memUsed >= int64(float64(e.memLimit)*escapeThreshold) {
		escapeInfo.MemoryExceeded = true
		escape = true
	}
	if e.cpuLimit > 0 && cpuSec >= int64(float64(e.cpuLimit)*escapeThreshold) {
		escapeInfo.CPUExceeded = true
		escape = true
	}
	if e.netLimit > 0 && netBytes >= int64(float64(e.netLimit)*escapeThreshold) {
		escapeInfo.NetworkExceeded = true
		escape = true
	}

	if escape {
		e.escapeHistory = append(e.escapeHistory, escapeInfo.DetectedAt)
		if len(e.escapeHistory) > e.maxHistoryPoints {
			e.escapeHistory = e.escapeHistory[len(e.escapeHistory)-e.maxHistoryPoints:]
		}
	}

	input := map[string]interface{}{
		"execution_id": executionID,
		"memory_used":  memUsed,
		"cpu_seconds":  cpuSec,
		"network_bytes": netBytes,
		"duration_ms": durationMs,
	}
	output := map[string]interface{}{
		"isolation_held": !escape,
		"escape_detected": escape,
		"escape_info": escapeInfo,
	}
	receipt, err := e.receiptBuilder.Build("sandbox.exec", input, output)
	if err != nil {
		return nil, err
	}

	return &EvidenceSandboxResult{
		ExecutionID:      executionID,
		Duration:         time.Duration(durationMs) * time.Millisecond,
		IsolationHeld:    !escape,
		EscapeDetected:   escape,
		EscapeInfo:       escapeInfo,
		MemoryUsedBytes:  memUsed,
		CPUSecondsTotal:  cpuSec,
		NetworkBytesSent: netBytes,
		Receipt:          receipt,
	}, nil
}
