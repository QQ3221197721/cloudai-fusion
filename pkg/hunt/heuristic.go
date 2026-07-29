package hunt

import (
	"context"
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
)

// HeuristicReasoner is the built-in, rule-based Reasoner. It requires no external
// model, so it is the honest default and always available in CI. It is reported
// to the capability registry as a simulated (non-LLM) backend.
//
// Rules (deterministic, explainable):
//   - Each CVE carrying an ATT&CK technique tag emits a finding for that technique;
//     confidence scales with CVSS. CVEs with no tag map to T1190 (exploit
//     public-facing application) as a conservative default.
//   - Each IOC hit emits a finding mapped by indicator type (network → C2, hash →
//     malware execution), with confidence scaled by the indicator severity.
type HeuristicReasoner struct{}

// Name returns "heuristic".
func (HeuristicReasoner) Name() string { return "heuristic" }

// IsLLM returns false: the heuristic is rule-based, not an LLM.
func (HeuristicReasoner) IsLLM() bool { return false }

// Reason applies the rule set to the gathered signals.
func (HeuristicReasoner) Reason(_ context.Context, q Query, s Signals) ([]Finding, error) {
	findings := make([]Finding, 0, len(s.CVEs)+len(s.IOCHits))

	for _, c := range s.CVEs {
		tech := primaryTechnique(c.MitreTags)
		findings = append(findings, Finding{
			ID:         "cve:" + c.CVEID,
			Technique:  tech,
			Tactic:     defaultTacticFor(tech),
			Severity:   severityFromCVSS(c.CVSSv3Score),
			Title:      fmt.Sprintf("Vulnerability %s (CVSS %.1f) matches technique %s", c.CVEID, c.CVSSv3Score, tech),
			Evidence:   map[string]any{"cve_id": c.CVEID, "cvss": c.CVSSv3Score, "tags": c.MitreTags},
			Confidence: confidenceFromCVSS(c.CVSSv3Score),
			DetectedAt: c.PublishedAt,
		})
	}

	for _, h := range s.IOCHits {
		tech := techniqueForIOC(h.IOCType)
		findings = append(findings, Finding{
			ID:         fmt.Sprintf("ioc:%s:%s", h.IOCType, h.Value),
			Technique:  tech,
			Tactic:     defaultTacticFor(tech),
			Severity:   h.Severity,
			Title:      fmt.Sprintf("IOC hit %s=%s mapped to technique %s", h.IOCType, h.Value, tech),
			Evidence:   map[string]any{"ioc_type": h.IOCType, "value": h.Value, "actor": h.ThreatActor},
			Confidence: confidenceFromSeverity(h.Severity),
			DetectedAt: h.FirstSeenAt,
		})
	}

	return findings, nil
}

// primaryTechnique returns the first ATT&CK technique tag, or a conservative
// default (T1190) when none is present.
func primaryTechnique(tags []string) string {
	for _, t := range tags {
		if len(t) > 1 && (t[0] == 'T') {
			return t
		}
	}
	return "T1190"
}

// techniqueForIOC maps an indicator type to a representative ATT&CK technique.
func techniqueForIOC(iocType string) string {
	switch iocType {
	case "ip", "domain", "url":
		return "T1071" // Application Layer Protocol (C2)
	case "sha256", "md5", "sha1":
		return "T1204" // User Execution (malware)
	default:
		return "T1190"
	}
}

// defaultTacticFor maps a technique to its primary tactic when the L1 knowledge
// graph has not (yet) enriched it.
func defaultTacticFor(technique string) string {
	switch technique {
	case "T1190", "T1133", "T1566":
		return "TA0001" // Initial Access
	case "T1204":
		return "TA0002" // Execution
	case "T1071", "T1105":
		return "TA0011" // Command and Control
	default:
		return "TA0001"
	}
}

func severityFromCVSS(score float32) intel.Severity {
	switch {
	case score >= 9.0:
		return intel.SeverityCritical
	case score >= 7.0:
		return intel.SeverityHigh
	case score >= 4.0:
		return intel.SeverityMedium
	default:
		return intel.SeverityLow
	}
}

func confidenceFromCVSS(score float32) Confidence {
	c := Confidence(score / 10.0)
	if c > 1 {
		c = 1
	}
	if c < 0 {
		c = 0
	}
	return c
}

func confidenceFromSeverity(sev intel.Severity) Confidence {
	switch sev {
	case intel.SeverityCritical:
		return 0.95
	case intel.SeverityHigh:
		return 0.8
	case intel.SeverityMedium:
		return 0.55
	default:
		return 0.3
	}
}
