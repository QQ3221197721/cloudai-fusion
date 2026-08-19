package devsecops

// evidence_gate.go adds two independent barriers to CI/CD security gating:
//
//  1. Evidence-native barrier — each gate decision is sealed in a signed,
//     offline-verifiable evidence.Receipt binding the artifact + findings to the
//     pass/block outcome. An auditor can later prove a build was (or was not)
//     gated without trusting the pipeline logs.
//
//  2. Independent-innovation barrier — an AutoRemediationEngine turns findings
//     into concrete, ranked fix actions via a CVE/pattern knowledge base
//     (upgrade to a known-fixed version, remove a leaked secret, reconfigure a
//     misconfiguration), so the gate does not merely block: it tells you exactly
//     how to get green.
//
// Note: this file intentionally uses Evidence-prefixed types (EvidenceFinding,
// EvidenceGateResult) because the package already defines a legacy Finding
// struct and an int-typed GateResult in pipeline.go.

import (
	"crypto/ed25519"
	"strings"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// EvidenceSecurityGate evaluates pipeline gates with receipts + auto-remediation.
type EvidenceSecurityGate struct {
	receiptBuilder    *evidence.ReceiptBuilder
	remediationEngine *AutoRemediationEngine
	maxCritical       int // max critical findings tolerated before blocking
}

// NewEvidenceSecurityGate builds a gate signing with privKey that blocks once
// the number of critical findings exceeds maxCritical.
func NewEvidenceSecurityGate(privKey ed25519.PrivateKey, maxCritical int) *EvidenceSecurityGate {
	if maxCritical < 0 {
		maxCritical = 0
	}
	return &EvidenceSecurityGate{
		receiptBuilder:    evidence.NewReceiptBuilder("devsecops", privKey),
		remediationEngine: NewAutoRemediationEngine(),
		maxCritical:       maxCritical,
	}
}

// EvidenceFinding is a scan result carrying enough context for remediation.
type EvidenceFinding struct {
	ID       string
	Severity string // "critical", "high", "medium", "low"
	Category string // "cve", "secret", "misconfig"
	Package  string // affected package
	Version  string // affected version
}

// EvidenceGateResult is the signed outcome of a gate evaluation.
type EvidenceGateResult struct {
	Passed      bool
	Blocked     int // number of critical findings that blocked
	Suggestions []RemediationSuggestion
	Receipt     *evidence.Receipt
}

// EvaluateGate checks findings against the critical threshold and generates a
// signed proof of the decision along with remediation suggestions.
func (g *EvidenceSecurityGate) EvaluateGate(artifactID string, findings []EvidenceFinding) (*EvidenceGateResult, error) {
	critical := 0
	for _, f := range findings {
		if strings.EqualFold(f.Severity, "critical") {
			critical++
		}
	}

	passed := critical <= g.maxCritical
	blocked := 0
	if !passed {
		blocked = critical
	}

	result := &EvidenceGateResult{
		Passed:      passed,
		Blocked:     blocked,
		Suggestions: g.remediationEngine.SuggestFixes(findings),
	}

	input := map[string]interface{}{"artifact": artifactID, "findings": findings}
	output := map[string]interface{}{"passed": passed, "blocked": blocked, "critical": critical}
	receipt, err := g.receiptBuilder.Build("evaluate_gate", input, output)
	if err != nil {
		return nil, err
	}
	result.Receipt = receipt
	return result, nil
}

// AutoRemediationEngine (INNOVATION) maps findings to concrete fixes using a
// pattern → fixed-version knowledge base.
type AutoRemediationEngine struct {
	knowledgeBase map[string]string // package pattern → first fixed version
}

// RemediationSuggestion is a single actionable fix for a finding.
type RemediationSuggestion struct {
	FindingID   string
	Action      string  // "upgrade", "remove", "configure", "review"
	Target      string  // e.g., "lodash"
	FixVersion  string  // e.g., "4.17.22"
	Confidence  float64
	AutoFixable bool
}

// NewAutoRemediationEngine seeds the knowledge base with well-known vulnerable
// packages and their first fixed versions.
func NewAutoRemediationEngine() *AutoRemediationEngine {
	return &AutoRemediationEngine{
		knowledgeBase: map[string]string{
			"lodash":           "4.17.22",
			"log4j-core":       "2.17.1",
			"log4j":            "2.17.1",
			"jackson-databind": "2.13.4.2",
			"commons-text":     "1.10.0",
			"spring-core":      "5.3.20",
			"openssl":          "3.0.7",
			"moment":           "2.29.4",
			"axios":            "1.6.0",
			"minimist":         "1.2.6",
		},
	}
}

// SuggestFixes returns one ranked remediation per finding. Category drives the
// action; CVE findings that match a known vulnerable package become
// auto-fixable upgrades to the first fixed version.
func (e *AutoRemediationEngine) SuggestFixes(findings []EvidenceFinding) []RemediationSuggestion {
	out := make([]RemediationSuggestion, 0, len(findings))
	for _, f := range findings {
		s := RemediationSuggestion{FindingID: f.ID, Target: f.Package}
		switch strings.ToLower(f.Category) {
		case "cve":
			if fix, ok := e.lookup(f.Package); ok {
				s.Action = "upgrade"
				s.FixVersion = fix
				s.Confidence = 0.95
				s.AutoFixable = true
			} else {
				// Known CVE class but no mapped fix version: still upgrade, lower confidence.
				s.Action = "upgrade"
				s.Confidence = 0.5
				s.AutoFixable = false
			}
		case "secret":
			s.Action = "remove"
			s.Confidence = 0.9
			s.AutoFixable = false
		case "misconfig":
			s.Action = "configure"
			s.Confidence = 0.7
			s.AutoFixable = false
		default:
			s.Action = "review"
			s.Confidence = 0.3
			s.AutoFixable = false
		}
		out = append(out, s)
	}
	return out
}

// lookup resolves a package name to a fixed version using exact then substring
// matching (so "lodash.merge" still maps to the lodash advisory).
func (e *AutoRemediationEngine) lookup(pkg string) (string, bool) {
	name := strings.ToLower(strings.TrimSpace(pkg))
	if name == "" {
		return "", false
	}
	if fix, ok := e.knowledgeBase[name]; ok {
		return fix, true
	}
	for pattern, fix := range e.knowledgeBase {
		if strings.Contains(name, pattern) {
			return fix, true
		}
	}
	return "", false
}
