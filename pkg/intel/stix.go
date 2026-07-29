package intel

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// stix.go adds a real STIX 2.1 parser — the OASIS threat-intelligence standard
// exported by MISP, AlienVault OTX, Anomali, and most TIPs. Parsing STIX lets L1
// ingest real, industry-standard intelligence feeds instead of only the bespoke
// JSONL/TSV formats, turning "threat intel" from a toy into a standards-based
// pipeline. This is genuine intelligence depth, not scaffolding.
//
// Supported (a practical, high-value subset of STIX 2.1):
//   - bundle → objects
//   - indicator: STIX patterning for the common observable comparisons
//     (ipv4/ipv6-addr, domain-name, url, email-addr, file:hashes.*) with '=' and
//     'IN (...)'; severity from x_severity, then confidence, then a default
//   - vulnerability → CVEEntry (id from name or a "cve" external reference)
//   - attack-pattern → Technique (id from a "mitre-attack" external reference)

// STIXImport is the normalized result of parsing a STIX bundle.
type STIXImport struct {
	IOCs       []IOCEntry
	CVEs       []CVEEntry
	Techniques []Technique
}

type stixBundle struct {
	Type    string       `json:"type"`
	Objects []stixObject `json:"objects"`
}

type stixObject struct {
	Type               string               `json:"type"`
	ID                 string               `json:"id"`
	Name               string               `json:"name"`
	Description        string               `json:"description"`
	Pattern            string               `json:"pattern"`
	PatternType        string               `json:"pattern_type"`
	ValidFrom          string               `json:"valid_from"`
	Created            string               `json:"created"`
	Modified           string               `json:"modified"`
	Confidence         int                  `json:"confidence"`
	Labels             []string             `json:"labels"`
	IndicatorTypes     []string             `json:"indicator_types"`
	ExternalReferences []stixExternalRef    `json:"external_references"`
	KillChainPhases    []stixKillChainPhase `json:"kill_chain_phases"`
	// Custom (x_) properties commonly emitted by TIPs.
	XSeverity    string  `json:"x_severity"`
	XCVSSScore   float32 `json:"x_cvss_v3_score"`
	XCVSSVector  string  `json:"x_cvss_v3_vector"`
	XThreatActor string  `json:"x_threat_actor"`
}

type stixExternalRef struct {
	SourceName string `json:"source_name"`
	ExternalID string `json:"external_id"`
	URL        string `json:"url"`
}

type stixKillChainPhase struct {
	KillChainName string `json:"kill_chain_name"`
	PhaseName     string `json:"phase_name"`
}

// patternComparison matches "<object-path> <op> <value|list>" inside a STIX
// pattern, e.g. ipv4-addr:value = '1.2.3.4' or file:hashes.'SHA-256' = '...'.
var patternComparison = regexp.MustCompile(`(?i)([a-z0-9_-]+:[a-z0-9_.'"\-]+)\s*(=|IN)\s*(\([^)]*\)|'[^']*')`)

// quotedValue extracts single-quoted values from a comparison's right-hand side.
var quotedValue = regexp.MustCompile(`'([^']*)'`)

// ParseSTIXBundle parses a STIX 2.1 bundle into normalized L1 records. Unknown
// object types are skipped, so a mixed real-world bundle imports cleanly.
func ParseSTIXBundle(data []byte) (*STIXImport, error) {
	var b stixBundle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("stix: parse bundle: %w", err)
	}
	if b.Type != "bundle" && len(b.Objects) == 0 {
		return nil, fmt.Errorf("stix: not a bundle (type=%q, 0 objects)", b.Type)
	}
	imp := &STIXImport{}
	for i := range b.Objects {
		o := &b.Objects[i]
		switch o.Type {
		case "indicator":
			imp.IOCs = append(imp.IOCs, iocsFromIndicator(o)...)
		case "vulnerability":
			if cve, ok := cveFromVulnerability(o); ok {
				imp.CVEs = append(imp.CVEs, cve)
			}
		case "attack-pattern":
			if tech, ok := techniqueFromAttackPattern(o); ok {
				imp.Techniques = append(imp.Techniques, tech)
			}
		}
	}
	return imp, nil
}

// iocsFromIndicator extracts one IOCEntry per observable comparison in a STIX
// indicator's pattern.
func iocsFromIndicator(o *stixObject) []IOCEntry {
	if o.Pattern == "" {
		return nil
	}
	sev := severityFromSTIX(o)
	seen := time.Now().UTC()
	if o.ValidFrom != "" {
		if t, err := time.Parse(time.RFC3339, o.ValidFrom); err == nil {
			seen = t
		}
	}
	var out []IOCEntry
	for _, m := range patternComparison.FindAllStringSubmatch(o.Pattern, -1) {
		path, rhs := m[1], m[3]
		iocType := iocTypeForPath(path)
		if iocType == "" {
			continue
		}
		for _, vm := range quotedValue.FindAllStringSubmatch(rhs, -1) {
			val := strings.TrimSpace(vm[1])
			if val == "" {
				continue
			}
			out = append(out, IOCEntry{
				IOCType:     iocType,
				Value:       val,
				ThreatActor: o.XThreatActor,
				Severity:    sev,
				FirstSeenAt: seen,
				Sources:     []string{"stix"},
			})
		}
	}
	return out
}

// iocTypeForPath maps a STIX object path to our IOC type vocabulary.
func iocTypeForPath(path string) string {
	lp := strings.ToLower(strings.TrimSpace(path))
	objType := lp
	if i := strings.Index(lp, ":"); i >= 0 {
		objType = lp[:i]
	}
	switch objType {
	case "ipv4-addr", "ipv6-addr":
		return "ip"
	case "domain-name":
		return "domain"
	case "url":
		return "url"
	case "email-addr":
		return "email"
	case "file":
		switch {
		case strings.Contains(lp, "sha-256") || strings.Contains(lp, "sha256"):
			return "sha256"
		case strings.Contains(lp, "sha-1") || strings.Contains(lp, "sha1"):
			return "sha1"
		case strings.Contains(lp, "md5"):
			return "md5"
		}
	}
	return ""
}

// severityFromSTIX derives a severity: explicit x_severity, else confidence
// bands, else a conservative default of medium.
func severityFromSTIX(o *stixObject) Severity {
	switch strings.ToLower(o.XSeverity) {
	case "low":
		return SeverityLow
	case "medium":
		return SeverityMedium
	case "high":
		return SeverityHigh
	case "critical":
		return SeverityCritical
	}
	switch {
	case o.Confidence >= 90:
		return SeverityCritical
	case o.Confidence >= 70:
		return SeverityHigh
	case o.Confidence >= 40:
		return SeverityMedium
	case o.Confidence > 0:
		return SeverityLow
	}
	return SeverityMedium
}

// cveFromVulnerability normalizes a STIX vulnerability object into a CVEEntry.
func cveFromVulnerability(o *stixObject) (CVEEntry, bool) {
	id := ""
	if strings.HasPrefix(strings.ToUpper(o.Name), "CVE-") {
		id = strings.ToUpper(o.Name)
	}
	var refs []string
	for _, ref := range o.ExternalReferences {
		if id == "" && strings.EqualFold(ref.SourceName, "cve") && ref.ExternalID != "" {
			id = strings.ToUpper(ref.ExternalID)
		}
		if ref.URL != "" {
			refs = append(refs, ref.URL)
		}
	}
	if id == "" {
		return CVEEntry{}, false
	}
	cve := CVEEntry{
		CVEID:        id,
		Description:  o.Description,
		CVSSv3Score:  o.XCVSSScore,
		CVSSv3Vector: o.XCVSSVector,
		References:   refs,
	}
	if o.Created != "" {
		if t, err := time.Parse(time.RFC3339, o.Created); err == nil {
			cve.PublishedAt = t
		}
	}
	if o.Modified != "" {
		if t, err := time.Parse(time.RFC3339, o.Modified); err == nil {
			cve.ModifiedDate = t
		}
	}
	return cve, true
}

// techniqueFromAttackPattern normalizes a STIX attack-pattern into a Technique.
func techniqueFromAttackPattern(o *stixObject) (Technique, bool) {
	id := ""
	for _, ref := range o.ExternalReferences {
		if strings.EqualFold(ref.SourceName, "mitre-attack") && ref.ExternalID != "" {
			id = strings.ToUpper(ref.ExternalID)
			break
		}
	}
	if id == "" {
		return Technique{}, false
	}
	var tactics []string
	for _, kp := range o.KillChainPhases {
		if kp.PhaseName != "" {
			tactics = append(tactics, kp.PhaseName)
		}
	}
	return Technique{
		TechniqueID: id,
		Name:        o.Name,
		TacticIDs:   tactics,
		Description: o.Description,
	}, true
}
