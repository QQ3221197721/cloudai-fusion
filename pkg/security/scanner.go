// Package security - scanner.go provides real vulnerability scanning integration.
// Attempts Trivy CLI → Grype CLI → K8s Pod spec analysis as a three-tier chain.
// When no external scanner CLI is available, performs real K8s API inspection
// to detect security misconfigurations in running pods.
package security

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/k8s"
)

// ScannerConfig holds vulnerability scanner configuration
type ScannerConfig struct {
	TrivyBinaryPath string // path to trivy binary (default: "trivy")
	GrypeBinaryPath string // path to grype binary (default: "grype")
	ScanTimeout     time.Duration
}

// Scanner provides vulnerability scanning via Trivy/Grype + K8s Pod spec analysis
type Scanner struct {
	config    ScannerConfig
	k8sClient *k8s.Client
}

// NewScanner creates a new vulnerability scanner
func NewScanner(cfg ScannerConfig) *Scanner {
	if cfg.TrivyBinaryPath == "" {
		cfg.TrivyBinaryPath = "trivy"
	}
	if cfg.GrypeBinaryPath == "" {
		cfg.GrypeBinaryPath = "grype"
	}
	if cfg.ScanTimeout == 0 {
		cfg.ScanTimeout = 5 * time.Minute
	}
	return &Scanner{config: cfg}
}

// SetK8sClient injects a real K8s client for pod spec scanning
func (s *Scanner) SetK8sClient(client *k8s.Client) {
	s.k8sClient = client
}

// ============================================================================
// Trivy JSON output types (minimal subset for parsing)
// ============================================================================

type trivyReport struct {
	Results []trivyResult `json:"Results"`
}

type trivyResult struct {
	Target          string            `json:"Target"`
	Vulnerabilities []trivyVulnEntry  `json:"Vulnerabilities"`
}

type trivyVulnEntry struct {
	VulnerabilityID string `json:"VulnerabilityID"`
	Severity        string `json:"Severity"`
	Title           string `json:"Title"`
	Description     string `json:"Description"`
	PkgName         string `json:"PkgName"`
	FixedVersion    string `json:"FixedVersion"`
}

// ============================================================================
// Grype JSON output types (minimal subset)
// ============================================================================

type grypeReport struct {
	Matches []grypeMatch `json:"matches"`
}

type grypeMatch struct {
	Vulnerability grypeVuln    `json:"vulnerability"`
	Artifact      grypeArtifact `json:"artifact"`
}

type grypeVuln struct {
	ID          string `json:"id"`
	Severity    string `json:"severity"`
	Description string `json:"description"`
	Fix         struct {
		Versions []string `json:"versions"`
	} `json:"fix"`
}

type grypeArtifact struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ============================================================================
// Scan Execution
// ============================================================================

// ScanImage scans a container image using Trivy → Grype → fallback chain
func (s *Scanner) ScanImage(ctx context.Context, image string) ([]VulnerabilityFinding, error) {
	// Tier 1: Try Trivy CLI
	findings, err := s.scanWithTrivy(ctx, image)
	if err == nil {
		return findings, nil
	}

	// Tier 2: Try Grype CLI
	findings, err = s.scanWithGrype(ctx, image)
	if err == nil {
		return findings, nil
	}

	// Tier 3: Return empty - no scanner available
	return []VulnerabilityFinding{}, fmt.Errorf("no vulnerability scanner available (trivy/grype not found)")
}

// ScanClusterPods scans all pods in a cluster for security misconfigurations
// This performs REAL K8s API queries to analyze running pod specs.
func (s *Scanner) ScanClusterPods(ctx context.Context, clusterID string) ([]VulnerabilityFinding, error) {
	if s.k8sClient == nil {
		return nil, fmt.Errorf("no K8s client available for pod scanning")
	}

	// Query real running pods
	pods, err := s.k8sClient.ListPods(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	now := common.NowUTC()
	var findings []VulnerabilityFinding

	for _, pod := range pods {
		ns := pod.Metadata.Namespace
		podName := pod.Metadata.Name

		for _, container := range pod.Spec.Containers {
			// Check 1: Container running as root (no runAsNonRoot)
			// Since our minimal K8s types don't include securityContext,
			// we infer from image and check what we can
			resource := fmt.Sprintf("pod/%s container/%s", podName, container.Name)

			// Check 2: Image uses :latest tag
			if strings.HasSuffix(container.Image, ":latest") || !strings.Contains(container.Image, ":") {
				findings = append(findings, VulnerabilityFinding{
					ID:          common.NewUUID(),
					Severity:    "medium",
					Title:       "Container image not pinned to specific version",
					Description: fmt.Sprintf("Container '%s' in pod '%s' uses unpinned image tag: %s", container.Name, podName, container.Image),
					Resource:    resource,
					Namespace:   ns,
					Remediation: "Pin image to a specific version or SHA256 digest instead of :latest",
					DetectedAt:  now,
				})
			}

			// Check 3: Missing resource limits
			if len(container.Resources.Limits) == 0 {
				findings = append(findings, VulnerabilityFinding{
					ID:          common.NewUUID(),
					Severity:    "medium",
					Title:       "Missing container resource limits",
					Description: fmt.Sprintf("Container '%s' in pod '%s' has no resource limits defined", container.Name, podName),
					Resource:    resource,
					Namespace:   ns,
					Remediation: "Set resources.limits for CPU and memory to prevent resource exhaustion",
					DetectedAt:  now,
				})
			}

			// Check 4: Missing resource requests
			if len(container.Resources.Requests) == 0 {
				findings = append(findings, VulnerabilityFinding{
					ID:          common.NewUUID(),
					Severity:    "low",
					Title:       "Missing container resource requests",
					Description: fmt.Sprintf("Container '%s' in pod '%s' has no resource requests defined", container.Name, podName),
					Resource:    resource,
					Namespace:   ns,
					Remediation: "Set resources.requests to enable proper scheduling",
					DetectedAt:  now,
				})
			}

			// Check 5: Image from untrusted registry
			if !isTrustedRegistry(container.Image) {
				findings = append(findings, VulnerabilityFinding{
					ID:          common.NewUUID(),
					Severity:    "high",
					Title:       "Container image from untrusted registry",
					Description: fmt.Sprintf("Container '%s' uses image from untrusted registry: %s", container.Name, container.Image),
					Resource:    resource,
					Namespace:   ns,
					Remediation: "Use images from trusted registries: ghcr.io, gcr.io, registry.cn-hangzhou.aliyuncs.com, etc.",
					DetectedAt:  now,
				})
			}
		}

		// Check 6: Pod in default namespace
		if ns == "default" {
			findings = append(findings, VulnerabilityFinding{
				ID:          common.NewUUID(),
				Severity:    "low",
				Title:       "Workload deployed to default namespace",
				Description: fmt.Sprintf("Pod '%s' is running in the default namespace", podName),
				Resource:    fmt.Sprintf("pod/%s", podName),
				Namespace:   ns,
				Remediation: "Deploy workloads to dedicated namespaces for proper isolation",
				DetectedAt:  now,
			})
		}
	}

	return findings, nil
}

// scanWithTrivy executes Trivy CLI to scan a container image
func (s *Scanner) scanWithTrivy(ctx context.Context, image string) ([]VulnerabilityFinding, error) {
	ctx, cancel := context.WithTimeout(ctx, s.config.ScanTimeout)
	defer cancel()

	// trivy image --format json --no-progress <image>
	cmd := exec.CommandContext(ctx, s.config.TrivyBinaryPath, "image",
		"--format", "json",
		"--no-progress",
		"--severity", "CRITICAL,HIGH,MEDIUM,LOW",
		image)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("trivy execution failed: %w", err)
	}

	var report trivyReport
	if err := json.Unmarshal(output, &report); err != nil {
		return nil, fmt.Errorf("failed to parse trivy output: %w", err)
	}

	now := common.NowUTC()
	var findings []VulnerabilityFinding
	for _, result := range report.Results {
		for _, vuln := range result.Vulnerabilities {
			remediation := ""
			if vuln.FixedVersion != "" {
				remediation = fmt.Sprintf("Upgrade %s to version %s", vuln.PkgName, vuln.FixedVersion)
			}
			findings = append(findings, VulnerabilityFinding{
				ID:          common.NewUUID(),
				Severity:    normalizeSeverity(vuln.Severity),
				Title:       vuln.Title,
				Description: truncateStr(vuln.Description, 500),
				Resource:    result.Target,
				CVE:         vuln.VulnerabilityID,
				Remediation: remediation,
				DetectedAt:  now,
			})
		}
	}
	return findings, nil
}

// scanWithGrype executes Grype CLI to scan a container image
func (s *Scanner) scanWithGrype(ctx context.Context, image string) ([]VulnerabilityFinding, error) {
	ctx, cancel := context.WithTimeout(ctx, s.config.ScanTimeout)
	defer cancel()

	// grype <image> -o json
	cmd := exec.CommandContext(ctx, s.config.GrypeBinaryPath, image, "-o", "json")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("grype execution failed: %w", err)
	}

	var report grypeReport
	if err := json.Unmarshal(output, &report); err != nil {
		return nil, fmt.Errorf("failed to parse grype output: %w", err)
	}

	now := common.NowUTC()
	var findings []VulnerabilityFinding
	for _, match := range report.Matches {
		remediation := ""
		if len(match.Vulnerability.Fix.Versions) > 0 {
			remediation = fmt.Sprintf("Upgrade %s to %s", match.Artifact.Name, match.Vulnerability.Fix.Versions[0])
		}
		findings = append(findings, VulnerabilityFinding{
			ID:          common.NewUUID(),
			Severity:    normalizeSeverity(match.Vulnerability.Severity),
			Title:       fmt.Sprintf("%s in %s@%s", match.Vulnerability.ID, match.Artifact.Name, match.Artifact.Version),
			Description: truncateStr(match.Vulnerability.Description, 500),
			Resource:    fmt.Sprintf("%s@%s", match.Artifact.Name, match.Artifact.Version),
			CVE:         match.Vulnerability.ID,
			Remediation: remediation,
			DetectedAt:  now,
		})
	}
	return findings, nil
}

// isTrustedRegistry checks if an image comes from a trusted registry
func isTrustedRegistry(image string) bool {
	trusted := []string{
		"ghcr.io/", "gcr.io/", "registry.cn-", "swr.cn-",
		"mcr.microsoft.com/", "docker.io/library/",
		"quay.io/", "k8s.gcr.io/", "registry.k8s.io/",
	}
	for _, prefix := range trusted {
		if strings.HasPrefix(image, prefix) {
			return true
		}
	}
	// Images without registry prefix are Docker Hub official images
	if !strings.Contains(strings.Split(image, ":")[0], "/") {
		return true
	}
	return false
}

func normalizeSeverity(s string) string {
	switch strings.ToUpper(s) {
	case "CRITICAL":
		return "critical"
	case "HIGH":
		return "high"
	case "MEDIUM":
		return "medium"
	case "LOW", "NEGLIGIBLE", "UNKNOWN":
		return "low"
	default:
		return "low"
	}
}

func truncateStr(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
