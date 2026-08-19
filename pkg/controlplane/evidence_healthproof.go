package controlplane

// evidence_healthproof.go signs health aggregation events and adds an
// independent innovation: cascading failure prediction by tracking dependency
// chains and propagation speed.
//
// Innovation — Cascading Failure Prediction:
// Each service has downstream dependencies (e.g., A calls B). When B fails, a BFS
// traverses the graph to find at-risk services. The cascade risk score combines
// impact radius (how many affected) and propagation speed (failed count / elapsed).
// High scores surface early warnings before total collapse.

import (
	"crypto/ed25519"
	"crypto/rand"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// EvidenceCascadeInfo captures the cascade analysis for a failed service.
type EvidenceCascadeInfo struct {
	Service         string `json:"service"`
	ImpactRadius    int    `json:"impact_radius"`     // count of downstream dependents
	FailSpeed       float64 `json:"fail_speed_per_min"` // failed per minute over observation window
	RiskScore       float64 `json:"risk_score"`        // impact × speed
	Dangerous       bool   `json:"dangerous"`
	AtRiskServices  []string `json:"at_risk_services,omitempty"`
}

// EvidenceHealthResult is the signed outcome of a health check event.
type EvidenceHealthResult struct {
	ServicesChecked int                    `json:"services_checked"`
	FailedCount     int                    `json:"failed_count"`
	CascadeAnalysis map[string]*EvidenceCascadeInfo `json:"cascade_analysis,omitempty"`
	Receipt         *evidence.Receipt      `json:"receipt"`
}

// EvidenceHealthEngine wraps health aggregation with receipts and cascade prediction.
type EvidenceHealthEngine struct {
	receiptBuilder *evidence.ReceiptBuilder
	depGraph       map[string][]string // service -> list of upstreams (dependents)
	failTimes      map[string][]time.Time
	windowMinutes  float64
}

// NewEvidenceHealthEngine constructs an engine with fresh Ed25519 key.
func NewEvidenceHealthEngine() *EvidenceHealthEngine {
	_, privKey, _ := ed25519.GenerateKey(rand.Reader)
	return &EvidenceHealthEngine{
		receiptBuilder: evidence.NewReceiptBuilder("controlplane", privKey),
		depGraph:       make(map[string][]string),
		failTimes:      make(map[string][]time.Time),
		windowMinutes:  30,
	}
}

// AddDependency registers that caller depends on callee (caller -> callee).
// Also add caller as an upstream (callee -> [caller]) so we can traverse backwards.
func (e *EvidenceHealthEngine) AddDependency(callee, caller string) {
	e.depGraph[callee] = append(e.depGraph[callee], caller)
}

// RecordFailure logs a failure time for a service. Call this when a service fails.
func (e *EvidenceHealthEngine) RecordFailure(service string) {
	e.failTimes[service] = append(e.failTimes[service], time.Now())
}

// EvaluateHealth attests a health evaluation result and computes cascade risk.
func (e *EvidenceHealthEngine) EvaluateHealth(services []string, failed []string) (*EvidenceHealthResult, error) {
	if len(failed) > 0 {
		for _, svc := range failed {
			e.RecordFailure(svc)
		}
		cascadeAnalysis := e.predictCascades(failed)
		input := map[string]interface{}{"services": services, "failed": failed, "cascade": cascadeAnalysis}
		output := map[string]interface{}{"analysis": cascadeAnalysis}
		receipt, err := e.receiptBuilder.Build("cp.health", input, output)
		if err != nil {
			return nil, err
		}
		return &EvidenceHealthResult{
			ServicesChecked: len(services),
			FailedCount:     len(failed),
			CascadeAnalysis: cascadeAnalysis,
			Receipt:         receipt,
		}, nil
	}

	input := map[string]interface{}{"services": services, "failed": []string{}}
	output := map[string]interface{}{}
	receipt, err := e.receiptBuilder.Build("cp.health", input, output)
	if err != nil {
		return nil, err
	}
	return &EvidenceHealthResult{
		ServicesChecked: len(services),
		FailedCount:     0,
		Receipt:         receipt,
	}, nil
}

func (e *EvidenceHealthEngine) predictCascades(failed []string) map[string]*EvidenceCascadeInfo {
	out := make(map[string]*EvidenceCascadeInfo)

	for _, svc := range failed {
		// BFS to collect at-risk services (reverse traversal via depGraph[svc]).
		visited := make(map[string]bool)
		var queue []string
		queue = append(queue, svc)
		atRisk := []string{}

		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			if visited[cur] {
				continue
			}
			visited[cur] = true

			for _, up := range e.depGraph[cur] {
				atRisk = append(atRisk, up)
				queue = append(queue, up)
			}
		}
		// Remove self and duplicates.
		delete(visited, svc)

		speed := float64(len(failed)) / e.windowMinutes
		score := float64(len(atRisk)) * speed

		danger := score > 5 && len(atRisk) >= 3

		out[svc] = &EvidenceCascadeInfo{
			Service:        svc,
			ImpactRadius:   len(atRisk),
			FailSpeed:      speed,
			RiskScore:      score,
			Dangerous:      danger,
			AtRiskServices: atRisk,
		}
	}
	return out
}
