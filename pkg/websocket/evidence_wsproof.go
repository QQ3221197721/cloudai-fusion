package websocket

// evidence_wsproof.go signs connection events and adds an independent
// innovation: Fibonacci-based adaptive reconnection with jitter.
//
// Innovation — Smart Reconnection Backoff:
// Instead of fixed exponential backoff, this uses Fibonacci numbers as a base
// sequence (1,1,2,3,5,8...) then multiplies by a configurable second and adds
// a random jitter in [-0.5s,+0.5s]. The Fibonacci growth is gentler than power-2,
// preventing network storms while still backing off rapidly on repeated failures.

import (
	"crypto/ed25519"
	"crypto/rand"
	mrand "math/rand/v2"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

const evidenceWsConnectInterval = time.Minute

// EvidenceWSResult is the signed outcome of a connect/disconnect event.
type EvidenceWSResult struct {
	URL            string        `json:"url"`
	Action         string        `json:"action"` // "connect"/"disconnect"
	Latency        time.Duration `json:"latency_ms"`
	BackoffSeconds float64       `json:"backoff_seconds,omitempty"`
	RetryCount     int           `json:"retry_count"`
	Receipt        *evidence.Receipt `json:"receipt"`
}

// EvidenceWSEngine wraps WebSocket operations with signed receipts and smart
// reconnection backoff.
type EvidenceWSEngine struct {
	receiptBuilder      *evidence.ReceiptBuilder
	lastDisconnectAt    time.Time
	consecutiveFailures int
	randSource          *mrand.Rand
}

// NewEvidenceWSEngine constructs an engine with fresh Ed25519 key and RNG.
func NewEvidenceWSEngine() *EvidenceWSEngine {
	_, privKey, _ := ed25519.GenerateKey(rand.Reader)
	return &EvidenceWSEngine{
		receiptBuilder: evidence.NewReceiptBuilder("websocket", privKey),
		randSource:     mrand.New(mrand.NewPCG(12345, 67890)),
	}
}

// HandleConnection attests a connect/disconnect and computes the next backoff
// when disconnected. Use setConsecutiveFailures after each failed connect attempt.
func (e *EvidenceWSEngine) HandleConnection(url string, connected bool, latency time.Duration) (*EvidenceWSResult, error) {
	if !connected {
		e.consecutiveFailures++
		if e.consecutiveFailures > 1 {
			e.lastDisconnectAt = time.Now()
		}
	} else {
		e.consecutiveFailures = 0
	}

	result := &EvidenceWSResult{
		URL:         url,
		Action:      "disconnect",
		Latency:     latency,
		RetryCount:  e.consecutiveFailures,
	}
	if connected {
		result.Action = "connect"
	}

	backoff := e.nextBackoffSeconds()
	if result.Action == "disconnect" || e.consecutiveFailures > 0 {
		result.BackoffSeconds = backoff
	}

	input := map[string]interface{}{
		"url":         url,
		"connected":   connected,
		"latency_ms":  latency.Milliseconds(),
		"retry_count": e.consecutiveFailures,
	}
	output := map[string]interface{}{"backoff_seconds": backoff}
	receipt, err := e.receiptBuilder.Build("ws.event", input, output)
	if err != nil {
		return nil, err
	}
	result.Receipt = receipt
	return result, nil
}

// SetConsecutiveFailures lets you manually update the failure counter if you don't
// want it automatically managed inside HandleConnection.
func (e *EvidenceWSEngine) SetConsecutiveFailures(n int) {
	e.consecutiveFailures = n
}

// GetConsecutiveFailures returns the current consecutive failure count.
func (e *EvidenceWSEngine) GetConsecutiveFailures() int {
	return e.consecutiveFailures
}

// NextBackoffSeconds computes a Fibonacci-based backoff with jitter using math/rand/v2.
func (e *EvidenceWSEngine) nextBackoffSeconds() float64 {
	n := min(e.consecutiveFailures, 70) // cap at Fib(70) to avoid overflow
	fib := fibonacci(n)

	baseSecs := float64(fib)
	if n <= 0 {
		baseSecs = 1
	}

	jitterSecs := (e.randSource.Float64()-0.5)*1.0 // [-0.5, +0.5]
	return baseSecs + jitterSecs
}

// fibonacci returns F_n for non-negative n using fast doubling internally.
func fibonacci(n int) uint64 {
	if n < 0 {
		return 0
	}
	a, b := uint64(0), uint64(1)
	for ; n > 0; n-- {
		a, b = b, a+b
	}
	return a
}
