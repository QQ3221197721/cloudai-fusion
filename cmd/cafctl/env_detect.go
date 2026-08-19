// Package main - Local environment detection for the `cafctl init` wizard.
//
// The init wizard needs to answer one honest question for a first-time user:
// "given what's on this machine, can CloudAI Fusion run against real backends,
// or will it fall back to simulation?" The detection logic here is deliberately
// split from any I/O so it can be unit-tested with fake inputs: the pure
// decision functions take injected probes (getenv/stat/lookPath) and the
// exported scanEnvironment() wires in the real ones.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// EnvCapability describes one detected local capability that influences the
// recommended run mode.
type EnvCapability struct {
	Name      string // "kubeconfig", "docker", "gpu"
	Available bool   // whether a real backend for this capability was found
	Detail    string // human-readable detail (path, version, count)
	Hint      string // actionable hint shown when unavailable
}

// EnvReport is the full local environment scan used by `cafctl init`.
type EnvReport struct {
	Kubeconfig EnvCapability
	Docker     EnvCapability
	GPU        EnvCapability
}

// Capabilities returns the report as an ordered slice for rendering.
func (r EnvReport) Capabilities() []EnvCapability {
	return []EnvCapability{r.Kubeconfig, r.Docker, r.GPU}
}

// RealBackendCount returns how many real infra backends were detected.
func (r EnvReport) RealBackendCount() int {
	n := 0
	for _, c := range r.Capabilities() {
		if c.Available {
			n++
		}
	}
	return n
}

// RecommendedRunMode decides the run mode from what was detected. We never
// auto-select production: it forbids simulated backends and fails fast at boot,
// so promoting to production must be an explicit human decision. When a real
// orchestrator (kubeconfig) is present we recommend "degraded" (real preferred,
// simulated surfaced loudly); otherwise "simulation".
func (r EnvReport) RecommendedRunMode() string {
	if r.Kubeconfig.Available {
		return "degraded"
	}
	return "simulation"
}

// detectKubeconfig looks for a usable kubeconfig via $KUBECONFIG then ~/.kube/config.
func detectKubeconfig(getenv func(string) string, statFile func(string) (os.FileInfo, error), homeDir string) EnvCapability {
	cap := EnvCapability{
		Name: "kubeconfig",
		Hint: "no kubeconfig found — cluster features run simulated. Set $KUBECONFIG or create ~/.kube/config to use a real cluster.",
	}
	if kc := strings.TrimSpace(getenv("KUBECONFIG")); kc != "" {
		// $KUBECONFIG may be a list; take the first entry.
		first := kc
		if idx := strings.IndexAny(kc, string(os.PathListSeparator)); idx >= 0 {
			first = kc[:idx]
		}
		if _, err := statFile(first); err == nil {
			cap.Available = true
			cap.Detail = "$KUBECONFIG=" + first
			return cap
		}
	}
	if homeDir != "" {
		def := filepath.Join(homeDir, ".kube", "config")
		if _, err := statFile(def); err == nil {
			cap.Available = true
			cap.Detail = def
			return cap
		}
	}
	return cap
}

// detectDocker checks whether a Docker CLI is on PATH. Presence of the CLI does
// not guarantee a running daemon, so the detail is explicit about that.
func detectDocker(lookPath func(string) (string, error)) EnvCapability {
	cap := EnvCapability{
		Name: "docker",
		Hint: "docker CLI not found — Compose/full-stack quickstart unavailable. Install Docker Desktop or the docker engine.",
	}
	if p, err := lookPath("docker"); err == nil && p != "" {
		cap.Available = true
		cap.Detail = "docker CLI at " + p + " (daemon state not probed)"
	}
	return cap
}

// detectGPU checks for nvidia-smi and, if present, tries to count devices.
func detectGPU(lookPath func(string) (string, error), queryGPU func() ([]byte, error)) EnvCapability {
	cap := EnvCapability{
		Name: "gpu",
		Hint: "no NVIDIA GPU detected — GPU scheduling/MIG features run simulated (CPU-only is fine for dev).",
	}
	if _, err := lookPath("nvidia-smi"); err != nil {
		return cap
	}
	cap.Available = true
	cap.Detail = "nvidia-smi present"
	if queryGPU != nil {
		if out, err := queryGPU(); err == nil {
			lines := 0
			for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
				if strings.TrimSpace(l) != "" {
					lines++
				}
			}
			if lines > 0 {
				cap.Detail = "nvidia-smi present, " + itoa(lines) + " GPU(s)"
			}
		}
	}
	return cap
}

// scanEnvironment performs the real detection using the OS.
func scanEnvironment() EnvReport {
	home, _ := os.UserHomeDir()
	return EnvReport{
		Kubeconfig: detectKubeconfig(os.Getenv, os.Stat, home),
		Docker:     detectDocker(exec.LookPath),
		GPU: detectGPU(exec.LookPath, func() ([]byte, error) {
			return exec.Command("nvidia-smi", "--query-gpu=name", "--format=csv,noheader").Output()
		}),
	}
}

// itoa is a tiny int-to-string helper to avoid pulling strconv into this file's
// hot path; it only ever handles small non-negative counts.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
