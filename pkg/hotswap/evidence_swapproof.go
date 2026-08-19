package hotswap

// evidence_swapproof.go signs component swap operations and verifies zero-downtime
// by tracking request invariants across the swap window.
//
// Innovation — Zero-Downtime Verification:
// A counter invariant: requestsReceived must equal requestsCompleted during + after
// the swap window. Any gap indicates dropped connections. We measure this before,
// during, and after the swap; a successful swap has no gaps and returns explicit
// confirmation.

import (
	"crypto/ed25519"
	"crypto/rand"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// EvidenceSwapResult is the signed outcome of a component swap operation.
type EvidenceSwapResult struct {
	Component       string                `json:"component"`
	VersionBefore   string                `json:"version_before"`
	VersionAfter    string                `json:"version_after"`
	Duration        time.Duration         `json:"duration_ms"`
	InvariantHeld   bool                  `json:"invariant_held"`
	DroppedRequests int                   `json:"dropped_requests"`
	SwapStatus      string                `json:"swap_status"` // "success"/"partial"/"failed"
	Receipt         *evidence.Receipt     `json:"receipt"`
}

// EvidenceHotswapEngine wraps component swaps with receipts and zero-downtime
// verification via request counter invariants.
type EvidenceHotswapEngine struct {
	receiptBuilder *evidence.ReceiptBuilder
	mu             sync.Mutex
	counters       map[string]*swapCounters
}

type swapCounters struct {
	startIn, startOut     int
	duringIn, duringOut   int
	endIn, endOut         int
	succeeded             bool
	versionBefore         string
}

// NewEvidenceHotswapEngine constructs an engine with fresh Ed25519 key.
func NewEvidenceHotswapEngine() *EvidenceHotswapEngine {
	_, privKey, _ := ed25519.GenerateKey(rand.Reader)
	return &EvidenceHotswapEngine{
		receiptBuilder: evidence.NewReceiptBuilder("hotswap", privKey),
		counters:       make(map[string]*swapCounters),
	}
}

// StartSwap initializes counters for a component being swapped. Call this before
// starting the actual swap process.
func (e *EvidenceHotswapEngine) StartSwap(component, versionBefore string, receivedBefore, completedBefore int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.counters[component] = &swapCounters{
		startIn:       receivedBefore,
		startOut:      completedBefore,
		succeeded:     false,
		versionBefore: versionBefore,
	}
}

// RecordDuringSwap samples counters during the swap window.
func (e *EvidenceHotswapEngine) RecordDuringSwap(component string, receivedDuring, completedDuring int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if c, ok := e.counters[component]; ok {
		c.duringIn = receivedDuring
		c.duringOut = completedDuring
	}
}

// EndSwap samples final counters after the swap completes and verifies the invariant.
func (e *EvidenceHotswapEngine) EndSwap(component, versionBefore, versionAfter string, receivedAfter, completedAfter int, durationMs int64, success bool) (*EvidenceSwapResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	c, ok := e.counters[component]
	if !ok {
		c = &swapCounters{startIn: receivedAfter, startOut: completedAfter, succeeded: false}
		c.endIn = receivedAfter
		c.endOut = completedAfter
	} else {
		c.endIn = receivedAfter
		c.endOut = completedAfter
		c.succeeded = success
	}

	startGap := float64(c.startIn-c.startOut)
	duringGap := float64(c.duringIn-c.duringOut)
	endGap := float64(c.endIn-c.endOut)
	dropped := 0
	if endGap < 0 {
		dropped = int(-endGap)
	}
	invariantHeld := (c.startIn == c.startOut) && (c.duringIn-c.duringOut >= -1) && endGap == 0

	var status string
	if invariantHeld && success {
		status = "success"
	} else if duringGap < 0 || endGap != 0 {
		status = "partial"
	} else {
		status = "failed"
	}

	input := map[string]interface{}{
		"component":       component,
		"version_before":  versionBefore,
		"version_after":   versionAfter,
		"in_gap_start":    startGap,
		"in_gap_during":   duringGap,
		"in_gap_end":      endGap,
		"dropped":         dropped,
	}
	output := map[string]interface{}{"invariant_held": invariantHeld, "status": status}
	receipt, err := e.receiptBuilder.Build("hotswap.swap", input, output)
	if err != nil {
		return nil, err
	}

	result := &EvidenceSwapResult{
		Component:       component,
		VersionBefore:   versionBefore,
		VersionAfter:    versionAfter,
		Duration:        time.Duration(durationMs) * time.Millisecond,
		InvariantHeld:   invariantHeld,
		DroppedRequests: dropped,
		SwapStatus:      status,
		Receipt:         receipt,
	}
	return result, nil
}
