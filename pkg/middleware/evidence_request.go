package middleware

// evidence_middleware.go layers two independent barriers over HTTP request processing:
//
//  1. Evidence-native barrier — each request processed through middleware is sealed into
//     a signed, offline-verifiable evidence.Receipt binding (method,path,status,time).
//     We can prove "request R completed at time X with Y".
//
//  2. Independent-innovation barrier — adaptive rate shaping throttles requests based on
//     live server health metrics (CPU/memory). When system stress is high, rate limits
//     are lowered automatically; when healthy, they relax to restore throughput.

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

type MiddlewareReceipt struct {
	Method    string            `json:"method"`
	Path      string            `json:"path"`
	Status    int               `json:"status"`
	Duration  float64           `json:"duration_ms"`
	Adaptive  bool              `json:"adaptive"` // true if rate limited by dynamic logic
	Receipt   *evidence.Receipt `json:"receipt,omitempty"`
}

type ServerHealth struct {
	CPU        float64 // 0..1
	Memory     float64 // 0..1
	LatencyAvg float64 // ms
}

type EvidenceMiddlewareEngine struct {
	receiptBuilder *evidence.ReceiptBuilder

	mu              sync.Mutex
	currentLimit    int
	highestLimit    int
	lastAdjustTime  time.Time
	minAdaptInterval time.Duration

	baseRateLimit   int // requests per window (default: 1000)
	windowMs        int // sliding window in ms
}

func NewEvidenceMiddlewareEngine() *EvidenceMiddlewareEngine {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	return &EvidenceMiddlewareEngine{
		receiptBuilder:   evidence.NewReceiptBuilder("middleware", priv),
		currentLimit:     1000,
		highestLimit:     1000,
		minAdaptInterval: 10 * time.Second,
		baseRateLimit:    1000,
		windowMs:         60 * 1000,
	}
}

func (e *EvidenceMiddlewareEngine) ProcessRequest(method, path string, status int, durationMs float64, stats ServerHealth) (*MiddlewareReceipt, error) {
	if method == "" || path == "" {
		return nil, fmt.Errorf("middleware: method and path must not be empty")
	}
	if durationMs < 0 {
		return nil, fmt.Errorf("middleware: duration must be non-negative, got %.2f", durationMs)
	}

	e.mu.Lock()
	limit := e.currentLimit
	e.mu.Unlock()

	adaptive := false
	dynamicLimit := e.adaptLimit(stats)
	if dynamicLimit != limit && time.Since(e.lastAdjustTime) > e.minAdaptInterval {
		e.mu.Lock()
		e.currentLimit = dynamicLimit
		e.lastAdjustTime = time.Now()
		e.mu.Unlock()
		adaptive = true
	}

	result := &MiddlewareReceipt{
		Method:    method,
		Path:      path,
		Status:    status,
		Duration:  durationMs,
		Adaptive:  adaptive,
	}

	input := struct {
		Method   string  `json:"method"`
		Path     string  `json:"path"`
		Status   int     `json:"status"`
		Duration float64 `json:"duration_ms"`
	}{method, path, status, durationMs}
	receipt, err := e.receiptBuilder.Build("middleware.process", input, result)
	if err != nil {
		return nil, fmt.Errorf("middleware: seal request: %w", err)
	}
	result.Receipt = receipt
	return result, nil
}

// GetCurrentLimit returns the current adaptive rate limit (requests per window).
func (e *EvidenceMiddlewareEngine) GetCurrentLimit() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.currentLimit
}

func (e *EvidenceMiddlewareEngine) adaptLimit(health ServerHealth) int {
	stress := (health.CPU + health.Memory + normalizeLatency(health.LatencyAvg)) / 3

	newLimit := e.baseRateLimit
	if stress >= 0.8 {
		newLimit = e.baseRateLimit * 50 / 100
	} else if stress >= 0.6 {
		newLimit = e.baseRateLimit * 70 / 100
	} else if stress >= 0.4 {
		newLimit = e.baseRateLimit * 85 / 100
	} else if stress <= 0.2 {
		newLimit = e.baseRateLimit * 120 / 100
	}
	if newLimit < 100 {
		newLimit = 100
	}
	if newLimit > e.highestLimit+200 {
		newLimit = e.highestLimit + 200
	}
	return newLimit
}

func normalizeLatency(ms float64) float64 {
	if ms <= 0 {
		return 0
	}
	norm := ms / 1000.0
	if norm > 1 {
		norm = 1
	}
	return norm
}
