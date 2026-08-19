// Package sandbox provides plugin security scanning, permission boundaries, and
// resource limit enforcement for isolated execution environments.
package sandbox

import (
	"fmt"
	"sort"
	"strings"
)

// Permission enumerates allowed operations for a plugin.
type Permission int

const (
	PermRead       Permission = iota // filesystem read access
	PermWrite                        // filesystem write access
	PermNetworkOutbound              // outbound HTTP/TCP connections
	PermNetworkInbound               // listen on ports
	PermExec                         // spawn subprocesses
	PermEnvVar                       // read specific environment variables
	PermNone Permission = -1         // deny all
)

func (p Permission) String() string {
	switch p {
	case PermRead:
		return "fs-read"
	case PermWrite:
		return "fs-write"
	case PermNetworkOutbound:
		return "net-outbound"
	case PermNetworkInbound:
		return "net-inbound"
	case PermExec:
		return "exec"
	case PermEnvVar:
		return "env-var"
	case PermNone:
		return "none"
	default:
		return "unknown"
	}
}

// SandboxProfile describes resource constraints and permission sets for an
// isolated plugin container/process.
type SandboxProfile struct {
	Name        string
	MemoryLimit int // MB
	CPULimit    float64 // CPU cores
	Network     NetworkPolicy
	Permissions []Permission
	AllowedSyscalls []string // Linux syscalls permitted; empty=all are blocked by default
	BannedImports   []string // Go import paths forbidden in static analysis
}

// NetworkPolicy specifies network behavior controls.
type NetworkPolicy struct {
	AllowOutbound bool
	AllowInbound  bool
	BlockPrivateIPs bool
	PortRangeStart  int
	PortRangeEnd    int // inclusive; zero means unrestricted
}

// SecurityReport summarizes the result of a plugin security scan.
type SecurityReport struct {
	Pass             bool
	TotalFindings    int
	DangerousImports []string
	DangerousCalls   []string
	BannedFeatures   []string
	Secure           bool
}

// PermissionBoundary enforces a capability allowlist (fs-read, fs-write,
// net-outbound, ...). A permission is granted only if it appears in Allowed.
type PermissionBoundary struct {
	Role    string       // e.g., "network-client", "filesystem-reader"
	Allowed []Permission // granted capabilities
}

// Allows reports whether the boundary grants the requested permission.
func (b *PermissionBoundary) Allows(perm Permission) bool {
	for _, p := range b.Allowed {
		if p == perm {
			return true
		}
	}
	return false
}

// Check validates that every requested permission is within the boundary. It
// returns the sorted list of denied permissions; an empty slice means all
// requests are permitted.
func (b *PermissionBoundary) Check(requested []Permission) []Permission {
	var denied []Permission
	for _, req := range requested {
		if !b.Allows(req) {
			denied = append(denied, req)
		}
	}
	sort.Slice(denied, func(i, j int) bool { return denied[i] < denied[j] })
	return denied
}

// Capabilities returns the human-readable capability names granted.
func (b *PermissionBoundary) Capabilities() []string {
	out := make([]string, 0, len(b.Allowed))
	for _, p := range b.Allowed {
		out = append(out, p.String())
	}
	sort.Strings(out)
	return out
}

// PluginScanner scans a plugin artifact and reports security findings.
type PluginScanner interface {
	ScanPlugin(name string, artifacts ArtifactList) SecurityReport
}

// Validate verifies that all required fields are configured.
func (p *SandboxProfile) Validate() error {
	if p.MemoryLimit <= 0 {
		return fmt.Errorf("sandbox: memory limit must be > 0")
	}
	if p.CPULimit <= 0 {
		return fmt.Errorf("sandbox: CPU limit must be > 0")
	}
	return nil
}

// StaticAnalysisScanner performs static analysis on source/bytecode to detect
// unsafe patterns like dangerous imports and raw syscall usage.
type StaticAnalysisScanner struct {
	BannedPatterns []string
	UnsafeImports  []string
}

// ScanPlugin inspects a binary's symbol table or imports list (via build flags,
// ldd output, etc.) and returns a report. This is simplified but can be extended
// to use `objdump`, `readelf` or Go-specific tools.
func (s *StaticAnalysisScanner) ScanPlugin(name string, artifacts ArtifactList) SecurityReport {
	report := SecurityReport{Pass: true}

	for _, art := range artifacts.Files {
		for _, imp := range s.UnsafeImports {
			if strings.Contains(art.ImportPath, imp) || strings.Contains(art.Path, imp) {
				report.DangerousImports = append(report.DangerousImports, fmt.Sprintf("%s@%s: %s", name, art.Path, imp))
				report.Pass = false
			}
		}
		for _, pat := range s.BannedPatterns {
			if strings.Contains(art.Path, pat) {
				report.DangerousCalls = append(report.DangerousCalls, fmt.Sprintf("%s: banned pattern %s", name, pat))
				report.Pass = false
			}
		}
	}

	if len(report.DangerousImports)+len(report.DangerousCalls) > 0 {
		report.TotalFindings = len(report.DangerousImports) + len(report.DangerousCalls)
	} else {
		report.TotalFindings = 0
		report.Secure = true
	}
	return report
}

// ExecutionIsolator enforces resource limits via cgroups/system calls. In
// production this would interact with container runtime APIs or cgroup v2.
type ExecutionIsolator struct {
	memoryLimitMB int
	cpuShares     float64
}

// EnforceConfigures system resources using platform-specific mechanisms (cgroups).
func (e *ExecutionIsolator) EnforceConfig(memoryLimitMB int, cpuShares float64) error {
	if memoryLimitMB <= 0 {
		return fmt.Errorf("isolation: invalid memory limit %d", memoryLimitMB)
	}
	if cpuShares <= 0 {
		return fmt.Errorf("isolation: invalid CPU shares %.2f", cpuShares)
	}
	e.memoryLimitMB = memoryLimitMB
	e.cpuShares = cpuShares
	// In production: call cgroupv2 set-memory-max, cpu.max, etc.
	return nil
}

// Enforce runs against the given artifact and applies isolation policies.
func (e *ExecutionIsolator) Enforce(name string, artifact Artifact, profile *SandboxProfile) SecurityReport {
	report := SecurityReport{Pass: true}
	if profile == nil {
		report.BannedFeatures = append(report.BannedFeatures, "no profile provided")
		report.Pass = false
		return report
	}

	// Check memory/CPU limits
	if profile.MemoryLimit < e.memoryLimitMB {
		report.BannedFeatures = append(report.BannedFeatures, "memory below minimum threshold")
		report.Pass = false
	}
	if profile.CPULimit < e.cpuShares {
		report.BannedFeatures = append(report.BannedFeatures, "CPU below minimum threshold")
		report.Pass = false
	}

	return report
}

// Artifact describes a plugin artifact being scanned.
type Artifact struct {
	Path       string
	Checksum   string
	ImportPath string
	Platform   string
	SizeBytes  int64
}

// ArtifactList groups multiple artifacts for batch scanning.
type ArtifactList struct {
	Files []Artifact
	Err   error
}
