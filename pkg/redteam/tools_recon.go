package redteam

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
)

// tools_recon.go wraps real recon binaries (nmap, httpx) as Tools. Safety:
//   - no shell is used; args are passed as an explicit argv (no injection),
//   - a per-invocation timeout bounds execution,
//   - if the binary is absent the tool runs in a reported SIMULATION (honest),
//     never fabricating scan results.
// Heavier isolation (running inside a range cluster / jail) is applied for
// higher-risk tiers in later milestones; recon here is read-only.

// CommandTool is a generic wrapper around an allow-listed external binary.
type CommandTool struct {
	name       string
	binary     string
	techniques []string
	timeout    time.Duration
	argsFn     func(target string, args map[string]any) []string
	parseFn    func(raw []byte) (summary string, finding *Finding)
}

// Name returns the tool name.
func (c *CommandTool) Name() string { return c.name }

// Techniques returns the MITRE ATT&CK IDs this tool exercises.
func (c *CommandTool) Techniques() []string { return c.techniques }

// Mode reports real when the binary is on PATH, else simulated.
func (c *CommandTool) Mode() capability.Mode {
	if _, err := exec.LookPath(c.binary); err == nil {
		return capability.ModeReal
	}
	return capability.ModeSimulated
}

// Invoke runs the binary (real) or returns a reported simulation (binary absent).
func (c *CommandTool) Invoke(ctx context.Context, in ToolInput) (ToolOutput, error) {
	if in.Target == "" {
		return ToolOutput{}, fmt.Errorf("redteam: %s requires a target", c.name)
	}
	if c.Mode() == capability.ModeSimulated {
		raw := []byte(fmt.Sprintf("[simulated %s: binary not installed; no scan performed] target=%s\n", c.binary, in.Target))
		return ToolOutput{
			Raw:     raw,
			Summary: fmt.Sprintf("%s simulated (binary %q not installed)", c.name, c.binary),
			Mode:    capability.ModeSimulated,
			Driver:  c.binary,
		}, nil
	}

	timeout := c.timeout
	if timeout <= 0 {
		timeout = time.Minute
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, c.binary, c.argsFn(in.Target, in.Args)...)
	raw, runErr := cmd.CombinedOutput()

	var summary string
	var finding *Finding
	if c.parseFn != nil {
		summary, finding = c.parseFn(raw)
	}
	out := ToolOutput{Raw: raw, Summary: summary, Finding: finding, Mode: capability.ModeReal, Driver: c.binary}

	// A context timeout is a real error. A non-zero exit with NO output is a real
	// failure; a non-zero exit WITH output is treated as data for recon (recorded,
	// not fatal - nmap/httpx routinely exit non-zero with useful output).
	if cctx.Err() != nil {
		return out, fmt.Errorf("redteam: %s timed out after %s", c.name, timeout)
	}
	if runErr != nil && len(raw) == 0 {
		return out, fmt.Errorf("redteam: %s failed: %w", c.name, runErr)
	}
	return out, nil
}

// NewNmapTool wraps nmap for network service discovery (MITRE T1046). Read-only:
// a TCP top-ports scan with host discovery disabled.
func NewNmapTool() *CommandTool {
	return &CommandTool{
		name:       "nmap",
		binary:     "nmap",
		techniques: []string{"T1046"},
		timeout:    2 * time.Minute,
		argsFn: func(target string, _ map[string]any) []string {
			return []string{"-Pn", "-T4", "--top-ports", "100", hostOnly(target)}
		},
		parseFn: parseNmap,
	}
}

// NewHTTPXTool wraps httpx for web reconnaissance / active scanning (MITRE T1595).
func NewHTTPXTool() *CommandTool {
	return &CommandTool{
		name:       "httpx",
		binary:     "httpx",
		techniques: []string{"T1595"},
		timeout:    time.Minute,
		argsFn: func(target string, _ map[string]any) []string {
			return []string{"-silent", "-status-code", "-title", "-tech-detect", "-u", target}
		},
		parseFn: parseHTTPX,
	}
}

// parseNmap counts open TCP ports and, if any, produces an informational finding.
func parseNmap(raw []byte) (string, *Finding) {
	open := 0
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, "/tcp") && strings.Contains(line, "open") {
			open++
		}
	}
	summary := fmt.Sprintf("nmap: %d open TCP port(s)", open)
	if open == 0 {
		return summary, nil
	}
	return summary, &Finding{
		Severity:  "info",
		Title:     summary,
		Technique: "T1046",
	}
}

// parseHTTPX summarizes the first non-empty httpx result line.
func parseHTTPX(raw []byte) (string, *Finding) {
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return "httpx: " + bounded(line, 200), nil
		}
	}
	return "httpx: no response", nil
}

// NewNucleiTool wraps nuclei for known-CVE / template-based vulnerability scanning
// (MITRE T1595). It is active scanning (probing, not exploitation), so it stays at
// the read-only risk tier; a real exploit is a separate, higher-tier action.
func NewNucleiTool() *CommandTool {
	return &CommandTool{
		name:       "nuclei",
		binary:     "nuclei",
		techniques: []string{"T1595"},
		timeout:    5 * time.Minute,
		argsFn: func(target string, _ map[string]any) []string {
			return []string{"-silent", "-severity", "low,medium,high,critical", "-u", target}
		},
		parseFn: parseNuclei,
	}
}

// parseNuclei counts nuclei findings and reports the worst observed severity.
func parseNuclei(raw []byte) (string, *Finding) {
	sevRank := map[string]int{"info": 1, "low": 2, "medium": 3, "high": 4, "critical": 5}
	count := 0
	worst, worstRank := "", 0
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		count++
		lower := strings.ToLower(line)
		for sev, rank := range sevRank {
			if rank > worstRank && strings.Contains(lower, "["+sev+"]") {
				worst, worstRank = sev, rank
			}
		}
	}
	if count == 0 {
		return "nuclei: no findings", nil
	}
	summary := fmt.Sprintf("nuclei: %d finding(s), worst severity %q", count, worst)
	if worst == "" {
		worst = "info"
	}
	return summary, &Finding{Severity: worst, Title: summary, Technique: "T1595"}
}
