// Package devsecops implements DevSecOps security gates that evaluate pipeline
// findings and enforce signing/SBOM compliance for supply chain integrity.
package devsecops

import (
	"fmt"
	"time"
)

// Finding represents a single security finding from any source.
type Finding struct {
	ID        string
	Type      string // "SAST", "DAST", "Dependency", "SupplyChain"
	Severity  Severity
	Location  string // file:line range or endpoint path
	Message   string
	Timestamp time.Time
}

// Severity classifies risk level.
type Severity int

const (
	SeverityLow Severity = iota
	SeverityMedium
	SeverityHigh
	SeverityCritical
)

func (s Severity) String() string {
	switch s {
	case SeverityCritical:
		return "critical"
	case SeverityHigh:
		return "high"
	case SeverityMedium:
		return "medium"
	case SeverityLow:
		return "low"
	default:
		return "unknown"
	}
}

// GateResult classifies a gate's evaluation.
type GateResult int

const (
	GatePass GateResult = iota
	GateFail
	GateWarn
)

func (r GateResult) String() string {
	switch r {
	case GatePass:
		return "pass"
	case GateFail:
		return "fail"
	case GateWarn:
		return "warn"
	default:
		return "unknown"
	}
}

// SecurityGate evaluates findings and returns Pass/Fail/Warn decision.
type SecurityGate interface {
	Name() string
	Evaluate(findings []Finding) GateResult
	Details(result GateResult, findings []Finding) map[string]interface{}
}

// PipelineResult aggregates all gate evaluations.
type PipelineResult struct {
	PassedAt          time.Time
	GatesPass         []string
	GatesFail         []string
	GatesWarn         []string
	AggregatedFindings []Finding
	Blocked           bool
	Reason            string
}

// EvaluateAll runs all gates and collects results.
func EvaluateAll(gates []SecurityGate, findings []Finding) PipelineResult {
	result := PipelineResult{PassedAt: time.Now()}
	for _, g := range gates {
		res := g.Evaluate(findings)
		switch res {
		case GatePass:
			result.GatesPass = append(result.GatesPass, g.Name())
		case GateFail:
			result.GatesFail = append(result.GatesFail, g.Name())
		case GateWarn:
			result.GatesWarn = append(result.GatesWarn, g.Name())
		}
	}
	result.AggregatedFindings = findings
	if len(result.GatesFail) > 0 || (len(result.GatesWarn) > 0 && shouldBlockOnWarn(findings)) {
		result.Blocked = true
		result.Reason = fmt.Sprintf("failed/unsafe gates: %v", result.GatesFail)
	}
	return result
}

func shouldBlockOnWarn(findings []Finding) bool {
	criticalCount := 0
	highCount := 0
	for _, f := range findings {
		switch f.Severity {
		case SeverityCritical:
			criticalCount++
		case SeverityHigh:
			highCount++
		}
	}
	return criticalCount > 0 || highCount >= 3
}

// ---------------------------------------------------------------------------
// Gates
// ---------------------------------------------------------------------------

// SASTGate evaluates static analysis findings. Blocks on too many critical bugs.
type SASTGate struct {
	CriticalThreshold int
	KnownVulnCount    int
}

func (g *SASTGate) Name() string { return "sast" }

func (g *SASTGate) Evaluate(findings []Finding) GateResult {
	critical := 0
	known := 0
	for _, f := range findings {
		if f.Type == "SAST" {
			if f.Severity == SeverityCritical {
				critical++
			}
			if f.ID != "" && isKnownVuln(f.ID) {
				known++
			}
		}
	}
	if critical >= g.CriticalThreshold || known >= g.KnownVulnCount {
		return GateFail
	}
	if critical+known > 0 {
		return GateWarn
	}
	return GatePass
}

func (g *SASTGate) Details(result GateResult, findings []Finding) map[string]interface{} {
	return map[string]interface{}{"gate": g.Name(), "result": result.String()}
}

// DASTGate evaluates dynamic test findings on endpoints. Fails if high severity exists.
type DASTGate struct {
	MaxHighEndpoints int
}

func (g *DASTGate) Name() string { return "dast" }

func (g *DASTGate) Evaluate(findings []Finding) GateResult {
	highEndpoints := make(map[string]bool)
	for _, f := range findings {
		if f.Type == "DAST" && f.Severity == SeverityHigh {
			highEndpoints[f.Location] = true
		}
	}
	count := len(highEndpoints)
	if count > g.MaxHighEndpoints {
		return GateFail
	}
	if count > 0 {
		return GateWarn
	}
	return GatePass
}

func (g *DASTGate) Details(result GateResult, findings []Finding) map[string]interface{} {
	return map[string]interface{}{"gate": g.Name(), "result": result.String()}
}

// DependencyGate checks CVEs in dependencies. Severities listed in
// AllowedSeverities are tolerated; any dependency finding whose severity is not
// tolerated counts as a violation. More than MaxCVEs violations fails the gate.
type DependencyGate struct {
	MaxCVEs           int
	AllowedSeverities []string
}

func (g *DependencyGate) Name() string { return "dependency" }

func (g *DependencyGate) Evaluate(findings []Finding) GateResult {
	var blocked []Finding
	for _, f := range findings {
		if f.Type == "Dependency" {
			// A dependency CVE is a violation unless its severity is tolerated.
			if !matchesSeverityFilter(f.Severity, g.AllowedSeverities) {
				blocked = append(blocked, f)
			}
		}
	}
	count := len(blocked)
	if count > g.MaxCVEs {
		return GateFail
	}
	if count > 0 {
		return GateWarn
	}
	return GatePass
}

func (g *DependencyGate) Details(result GateResult, findings []Finding) map[string]interface{} {
	return map[string]interface{}{"gate": g.Name(), "result": result.String()}
}

func matchesSeverityFilter(severity Severity, allowList []string) bool {
	for i := 0; i < len(allowList); i++ {
		l := allowList[i]
		switch l {
		case "critical":
			if severity == SeverityCritical {
				return true
			}
		case "high":
			if severity == SeverityHigh {
				return true
			}
		case "medium":
			if severity == SeverityMedium {
				return true
			}
		case "low":
			if severity == SeverityLow {
				return true
			}
		}
	}
	return false
}

// Artifact describes a build artifact subject to supply-chain verification.
type Artifact struct {
	Name       string
	Digest     string   // sha256 digest of the artifact
	Signatures []string // detached cosign signatures (base64)
	SBOM       *SBOM    // software bill of materials, if attached
}

// SBOM is a minimal software bill of materials.
type SBOM struct {
	Format     string // "spdx", "cyclonedx"
	Components []SBOMComponent
}

// SBOMComponent is one dependency entry.
type SBOMComponent struct {
	Name    string
	Version string
	License string
}

// SignatureVerifier verifies a cosign signature over an artifact digest. The
// production implementation shells out to cosign / uses the sigstore libraries;
// tests inject a fake.
type SignatureVerifier interface {
	Verify(artifact Artifact) (bool, error)
}

// keylessVerifier verifies that at least one signature is present and that the
// digest is well-formed. It is a conservative default when no external verifier
// is wired; real deployments should supply a sigstore-backed verifier.
type keylessVerifier struct{}

func (keylessVerifier) Verify(a Artifact) (bool, error) {
	if a.Digest == "" {
		return false, fmt.Errorf("supply-chain: artifact %q has no digest", a.Name)
	}
	return len(a.Signatures) > 0, nil
}

// SupplyChainVerifier checks cosign signatures and SBOM compliance.
type SupplyChainVerifier struct {
	Required          bool
	IgnoreMissingSBOM bool
	Artifacts         []Artifact
	// Verifier performs the signature check; nil uses keylessVerifier.
	Verifier SignatureVerifier
}

func (v *SupplyChainVerifier) Name() string { return "supply-chain" }

// Evaluate verifies every configured artifact's signature and SBOM presence.
func (v *SupplyChainVerifier) Evaluate(findings []Finding) GateResult {
	verifier := v.Verifier
	if verifier == nil {
		verifier = keylessVerifier{}
	}

	// With no artifacts to inspect, honour the Required flag.
	if len(v.Artifacts) == 0 {
		if v.Required {
			return GateFail
		}
		return GateWarn
	}

	allSigned := true
	sbomOK := true
	for _, art := range v.Artifacts {
		ok, err := verifier.Verify(art)
		if err != nil || !ok {
			allSigned = false
		}
		if art.SBOM == nil || len(art.SBOM.Components) == 0 {
			sbomOK = false
		}
	}

	if !allSigned {
		if v.Required {
			return GateFail
		}
		return GateWarn
	}
	if !sbomOK && !v.IgnoreMissingSBOM {
		return GateWarn
	}
	return GatePass
}

func (v *SupplyChainVerifier) Details(result GateResult, findings []Finding) map[string]interface{} {
	return map[string]interface{}{"gate": v.Name(), "result": result.String()}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func isKnownVuln(id string) bool {
	// Known-vulnerability identifiers follow the CVE-YYYY-NNNN naming scheme.
	return len(id) > 4 && id[:4] == "CVE-"
}
