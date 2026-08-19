package gitops

// evidence_driftproof.go signs reconciliation events and adds an independent
// innovation: drift severity scoring based on impact radius.
//
// Innovation — Drift Severity Scoring:
// Each detected drift is scored by multiplying the number of dependent services
// that would be affected × a criticality factor per service. A larger impact
// radius or higher criticality raises the severity, prioritizing remediation.

import (
	"crypto/ed25519"
	"crypto/rand"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

const evidenceCriticalityMultiplier = 10.0

// EvidenceDriftSeverity captures the severity analysis for a drift event.
type EvidenceDriftSeverity struct {
	Service        string   `json:"service"`
	DriftType      string   `json:"drift_type"` // "config"/"policy"/"resource"
	AffectedCount  int      `json:"affected_count"`
	Criticality    float64  `json:"criticality"`
	SeverityScore  float64  `json:"severity_score"`
	Critical       bool     `json:"critical"`
	ImpactServices []string `json:"impact_services,omitempty"`
}

// EvidenceReconcileResult is the signed outcome of a GitOps reconciliation.
type EvidenceReconcileResult struct {
	Reconciled     bool              `json:"reconciled"`
	DriftDetected  bool              `json:"drift_detected"`
	Severity       *EvidenceDriftSeverity `json:"severity,omitempty"`
	Receipt        *evidence.Receipt `json:"receipt"`
}

// EvidenceGitopsEngine wraps GitOps reconciliation with receipts and drift
// severity scoring via impact radius × criticality.
type EvidenceGitopsEngine struct {
	receiptBuilder     *evidence.ReceiptBuilder
	serviceImpactGraph map[string][]string // service -> list of downstream dependents
	serviceCriticality map[string]float64
	windowSeconds      time.Duration
}

// NewEvidenceGitopsEngine constructs an engine with fresh Ed25519 key.
func NewEvidenceGitopsEngine() *EvidenceGitopsEngine {
	_, privKey, _ := ed25519.GenerateKey(rand.Reader)
	return &EvidenceGitopsEngine{
		receiptBuilder:     evidence.NewReceiptBuilder("gitops", privKey),
		serviceImpactGraph: make(map[string][]string),
		serviceCriticality: make(map[string]float64),
		windowSeconds:      5 * time.Minute,
	}
}

// AddDownstreamDependent registers that `downstream` depends on `upstream`.
func (e *EvidenceGitopsEngine) AddDownstreamDependent(upstream, downstream string) {
	e.serviceImpactGraph[upstream] = append(e.serviceImpactGraph[upstream], downstream)
}

// SetCriticality sets the criticality multiplier for a service (0-1 scale internally).
func (e *EvidenceGitopsEngine) SetCriticality(service string, c float64) {
	if c > 1 {
		c = 1
	} else if c < 0 {
		c = 0
	}
	e.serviceCriticality[service] = c
}

// Reconcile attests a GitOps reconciliation and computes drift severity.
func (e *EvidenceGitopsEngine) Reconcile(shaBefore, shaAfter string, drifted bool, driftedService string) (*EvidenceReconcileResult, error) {
	var severity *EvidenceDriftSeverity
	if drifted && driftedService != "" {
		impactRadius := e.computeImpactRadius(driftedService)
		baseCrit := e.serviceCriticality[driftedService]
		if baseCrit == 0 {
			baseCrit = 0.5 // neutral default
		}
		score := float64(impactRadius) * baseCrit * evidenceCriticalityMultiplier
		danger := score > 25 || impactRadius >= 5

		severity = &EvidenceDriftSeverity{
			Service:       driftedService,
			DriftType:     "config",
			AffectedCount: impactRadius,
			Criticality:   baseCrit,
			SeverityScore: score,
			Critical:      danger,
			ImpactServices: e.collectImpactedServices(driftedService),
		}
	}

	input := map[string]interface{}{
		"sha_before":      shaBefore,
		"sha_after":       shaAfter,
		"drifted":         drifted,
		"drifted_service": driftedService,
		"severity":        severity,
	}
	output := map[string]interface{}{"severity": severity}
	receipt, err := e.receiptBuilder.Build("gitops.reconcile", input, output)
	if err != nil {
		return nil, err
	}

	return &EvidenceReconcileResult{
		Reconciled:    true,
		DriftDetected: drifted,
		Severity:      severity,
		Receipt:       receipt,
	}, nil
}

func (e *EvidenceGitopsEngine) computeImpactRadius(service string) int {
	visited := make(map[string]bool)
	var queue []string
	queue = append(queue, service)
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if visited[cur] {
			continue
		}
		visited[cur] = true
		for _, dep := range e.serviceImpactGraph[cur] {
			if !visited[dep] {
				queue = append(queue, dep)
			}
		}
	}
	delete(visited, service)
	return len(visited)
}

func (e *EvidenceGitopsEngine) collectImpactedServices(service string) []string {
	visited := make(map[string]bool)
	var queue []string
	var result []string
	queue = append(queue, service)
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if visited[cur] {
			continue
		}
		visited[cur] = true
		if cur != service {
			result = append(result, cur)
		}
		for _, dep := range e.serviceImpactGraph[cur] {
			if !visited[dep] {
				queue = append(queue, dep)
			}
		}
	}
	return result
}
