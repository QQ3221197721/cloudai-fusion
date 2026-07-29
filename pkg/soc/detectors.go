package soc

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
)

// detectors.go implements the L3-L7 detectors. Each is deterministic and
// rule-based (reported to capability as simulated) and, where relevant, consumes
// the L1 intelligence store so detection is driven by real IOC/CVE data.

// Detector is the common contract used for capability reporting and routing.
type Detector interface {
	Well() Well
	Name() string
	IsReal() bool
}

func newFinding(well Well, technique, asset, title string, sev Severity, ev map[string]any) Finding {
	return Finding{
		ID:         uuid.NewString(),
		Well:       well,
		WellName:   well.String(),
		Technique:  technique,
		Tactic:     tacticForTechnique(technique),
		Severity:   sev,
		Asset:      asset,
		Title:      title,
		Evidence:   ev,
		DetectedAt: time.Now().UTC(),
	}
}

// ---------------------------------------------------------------------------
// L3 Endpoint
// ---------------------------------------------------------------------------

// EndpointDetector (L3) matches observed file hashes against L1 IOC hashes.
type EndpointDetector struct{ intel IntelReader }

// NewEndpointDetector builds an L3 detector over the given intel reader.
func NewEndpointDetector(reader IntelReader) *EndpointDetector {
	return &EndpointDetector{intel: reader}
}

func (*EndpointDetector) Well() Well   { return WellEndpoint }
func (*EndpointDetector) Name() string { return "endpoint-ioc" }
func (*EndpointDetector) IsReal() bool { return false }

// Analyze flags any file hash that matches a known-malicious L1 IOC (sha256),
// mapping to T1204 (User Execution).
func (d *EndpointDetector) Analyze(_ context.Context, host string, fileHashes []string) ([]Finding, error) {
	if d.intel == nil || len(fileHashes) == 0 {
		return nil, nil
	}
	hits, err := d.intel.LookupIOCs("sha256", fileHashes)
	if err != nil {
		return nil, fmt.Errorf("soc: endpoint ioc lookup: %w", err)
	}
	out := make([]Finding, 0, len(hits))
	for _, h := range hits {
		out = append(out, newFinding(WellEndpoint, "T1204", host,
			fmt.Sprintf("malicious file hash observed on %s", host), sevOr(h.Severity, intel.SeverityHigh),
			map[string]any{"sha256": h.Value, "threat_actor": h.ThreatActor}))
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// L4 Network
// ---------------------------------------------------------------------------

// NetworkDetector (L4) matches observed connections against L1 IOC IPs/domains.
type NetworkDetector struct{ intel IntelReader }

// NewNetworkDetector builds an L4 detector over the given intel reader.
func NewNetworkDetector(reader IntelReader) *NetworkDetector { return &NetworkDetector{intel: reader} }

func (*NetworkDetector) Well() Well   { return WellNetwork }
func (*NetworkDetector) Name() string { return "network-ioc" }
func (*NetworkDetector) IsReal() bool { return false }

// Analyze flags connections to known-malicious IPs/domains, mapping to T1071
// (Application Layer Protocol / C2).
func (d *NetworkDetector) Analyze(_ context.Context, host string, ips, domains []string) ([]Finding, error) {
	if d.intel == nil {
		return nil, nil
	}
	out := make([]Finding, 0)
	for iocType, values := range map[string][]string{"ip": ips, "domain": domains} {
		if len(values) == 0 {
			continue
		}
		hits, err := d.intel.LookupIOCs(iocType, values)
		if err != nil {
			return nil, fmt.Errorf("soc: network %s lookup: %w", iocType, err)
		}
		for _, h := range hits {
			out = append(out, newFinding(WellNetwork, "T1071", host,
				fmt.Sprintf("connection to malicious %s %s", iocType, h.Value),
				sevOr(h.Severity, intel.SeverityHigh),
				map[string]any{iocType: h.Value, "threat_actor": h.ThreatActor}))
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// L5 Cloud Workload (CIS-style posture)
// ---------------------------------------------------------------------------

// WorkloadSpec is the subset of a Kubernetes workload's security context needed
// for posture checks.
type WorkloadSpec struct {
	Name                     string `json:"name"`
	Namespace                string `json:"namespace"`
	Privileged               bool   `json:"privileged"`
	HostNetwork              bool   `json:"host_network"`
	HostPID                  bool   `json:"host_pid"`
	RunAsRoot                bool   `json:"run_as_root"`
	AllowPrivilegeEscalation bool   `json:"allow_privilege_escalation"`
}

// WorkloadDetector (L5) runs CIS-style posture checks on workload specs.
type WorkloadDetector struct{}

// NewWorkloadDetector builds an L5 posture detector.
func NewWorkloadDetector() *WorkloadDetector { return &WorkloadDetector{} }

func (*WorkloadDetector) Well() Well   { return WellCloudWorkload }
func (*WorkloadDetector) Name() string { return "workload-cis" }
func (*WorkloadDetector) IsReal() bool { return false }

// Analyze emits a finding per posture violation. Privileged / privilege
// escalation map to container-escape (T1611); host namespaces and root map to
// escape-to-host (T1610).
func (d *WorkloadDetector) Analyze(_ context.Context, spec WorkloadSpec) ([]Finding, error) {
	asset := spec.Namespace + "/" + spec.Name
	out := make([]Finding, 0)
	add := func(tech, title string, sev Severity) {
		out = append(out, newFinding(WellCloudWorkload, tech, asset, title, sev,
			map[string]any{"namespace": spec.Namespace, "workload": spec.Name}))
	}
	if spec.Privileged {
		add("T1611", "privileged container ("+asset+")", intel.SeverityCritical)
	}
	if spec.AllowPrivilegeEscalation {
		add("T1611", "allowPrivilegeEscalation enabled ("+asset+")", intel.SeverityHigh)
	}
	if spec.HostNetwork {
		add("T1610", "hostNetwork enabled ("+asset+")", intel.SeverityHigh)
	}
	if spec.HostPID {
		add("T1610", "hostPID enabled ("+asset+")", intel.SeverityHigh)
	}
	if spec.RunAsRoot {
		add("T1610", "container runs as root ("+asset+")", intel.SeverityMedium)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// L7 Container Image
// ---------------------------------------------------------------------------

// ImageCVE is a CVE discovered in a container image.
type ImageCVE struct {
	ID   string  `json:"id"`
	CVSS float32 `json:"cvss"`
}

// ImageScan is the input to the L7 detector: an image reference and its CVEs.
type ImageScan struct {
	Reference string     `json:"reference"`
	CVEs      []ImageCVE `json:"cves"`
}

// ImageDetector (L7) triages container-image CVEs.
type ImageDetector struct{ minCVSS float32 }

// NewImageDetector builds an L7 detector flagging CVEs at/above minCVSS
// (<=0 defaults to 7.0, i.e. High and above).
func NewImageDetector(minCVSS float32) *ImageDetector {
	if minCVSS <= 0 {
		minCVSS = 7.0
	}
	return &ImageDetector{minCVSS: minCVSS}
}

func (*ImageDetector) Well() Well   { return WellImage }
func (*ImageDetector) Name() string { return "image-cve" }
func (*ImageDetector) IsReal() bool { return false }

// Analyze flags each CVE at/above the threshold, mapping to T1190 (Exploit
// Public-Facing Application) as the representative exposure technique.
func (d *ImageDetector) Analyze(_ context.Context, scan ImageScan) ([]Finding, error) {
	out := make([]Finding, 0, len(scan.CVEs))
	for _, cve := range scan.CVEs {
		if cve.CVSS < d.minCVSS {
			continue
		}
		out = append(out, newFinding(WellImage, "T1190", scan.Reference,
			fmt.Sprintf("image %s ships vulnerable %s (CVSS %.1f)", scan.Reference, cve.ID, cve.CVSS),
			severityFromCVSS(cve.CVSS),
			map[string]any{"image": scan.Reference, "cve": cve.ID, "cvss": cve.CVSS}))
	}
	return out, nil
}

// sevOr returns sev when set, else fallback.
func sevOr(sev, fallback Severity) Severity {
	if sev == "" {
		return fallback
	}
	return sev
}
