// Package main - `cafctl doctor`: environment self-check with actionable fixes.
//
// Every check answers two questions: "is this a problem?" and "what exactly do I
// type to fix it?". Checks that are merely optional (Docker, kubectl, a GPU) are
// reported as warnings with the concrete capability you lose, never as failures —
// the zero-dependency local path must stay green on a bare machine.
//
// The decision logic is split from the I/O so it can be unit-tested with injected
// probes (same pattern as env_detect.go).
package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// Status values for a doctor check.
const (
	doctorPass = "pass"
	doctorWarn = "warn"
	doctorFail = "fail"
)

// minGoMajor/minGoMinor is the toolchain floor the module needs to build.
const (
	minGoMajor = 1
	minGoMinor = 21
)

// doctorCheck is one diagnostic result.
type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // pass | warn | fail
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

// doctorReport aggregates checks and their counts.
type doctorReport struct {
	Checks []doctorCheck `json:"checks"`
	Passed int           `json:"passed"`
	Warned int           `json:"warned"`
	Failed int           `json:"failed"`
}

// newDoctorReport tallies statuses so callers do not re-count.
func newDoctorReport(checks []doctorCheck) doctorReport {
	r := doctorReport{Checks: checks}
	for _, c := range checks {
		switch c.Status {
		case doctorPass:
			r.Passed++
		case doctorWarn:
			r.Warned++
		case doctorFail:
			r.Failed++
		}
	}
	return r
}

var (
	doctorPorts  []int
	doctorDir    string
	doctorJSON   bool
	doctorStrict bool
)

func newDoctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check this machine can run CloudAI Fusion, with fixes for what it can't",
		Long: `Diagnose the local environment before you run anything:

  • Go toolchain version (build floor: go1.21)
  • Port availability for the local plane
  • Project scaffold state (.caf/ config + signing key)
  • Workspace writability
  • Optional tooling (docker, kubectl, helm, git, nvidia-smi) and the exact
    capability each missing tool costs you

Optional tooling never fails the run: 'cafctl up --local' is designed to work with
none of it. Exit code is non-zero only when a required check fails (or when
--strict is set and any warning exists).`,
		Example: `  cafctl doctor
  cafctl doctor --port 8080 --port 9090
  cafctl doctor --json
  cafctl doctor --strict   # warnings become failures (CI gate)`,
		RunE: runDoctor,
	}
	cmd.Flags().IntSliceVar(&doctorPorts, "port", []int{defaultLocalPort},
		"Port(s) to check for availability (repeatable)")
	cmd.Flags().StringVarP(&doctorDir, "dir", "d", ".",
		"Project directory to inspect")
	cmd.Flags().BoolVar(&doctorJSON, "json", false,
		"Emit the report as JSON")
	cmd.Flags().BoolVar(&doctorStrict, "strict", false,
		"Treat warnings as failures (useful in CI)")
	return cmd
}

// The init self-registration has been removed; newDoctorCmd is registered explicitly
// from main.go's init() to keep all command wiring in one place.

func runDoctor(cmd *cobra.Command, _ []string) error {
	checks := collectDoctorChecks(doctorDir, doctorPorts)
	report := newDoctorReport(checks)
	out := cmd.OutOrStdout()

	if doctorJSON {
		fmt.Fprintln(out, ToJSON(report))
	} else {
		printDoctorReport(cmd, report)
	}

	if report.Failed > 0 {
		return fmt.Errorf("doctor: %d required check(s) failed", report.Failed)
	}
	if doctorStrict && report.Warned > 0 {
		return fmt.Errorf("doctor: %d warning(s) with --strict", report.Warned)
	}
	return nil
}

// collectDoctorChecks wires the real OS probes into the pure check functions.
func collectDoctorChecks(dir string, ports []int) []doctorCheck {
	checks := []doctorCheck{
		checkGoToolchain(runtime.Version()),
	}
	for _, port := range ports {
		checks = append(checks, checkPortAvailable(port, net.Listen))
	}
	checks = append(checks,
		checkWorkspaceWritable(dir, probeWriteFile),
		checkProjectScaffold(dir, os.Stat),
	)
	for _, tool := range optionalTools() {
		checks = append(checks, checkOptionalTool(tool, execLookPath))
	}
	return checks
}

// checkGoToolchain validates the Go version string the binary was built with.
func checkGoToolchain(version string) doctorCheck {
	c := doctorCheck{Name: "go toolchain"}
	major, minor, ok := parseGoVersion(version)
	if !ok {
		c.Status = doctorWarn
		c.Detail = "could not parse Go version " + strconv.Quote(version)
		c.Fix = "run 'go version'; CloudAI Fusion needs go1.21 or newer to build"
		return c
	}
	if major < minGoMajor || (major == minGoMajor && minor < minGoMinor) {
		c.Status = doctorFail
		c.Detail = fmt.Sprintf("%s is older than the go%d.%d build floor", version, minGoMajor, minGoMinor)
		c.Fix = fmt.Sprintf("install go%d.%d+ from https://go.dev/dl and re-run 'go build ./cmd/cafctl'", minGoMajor, minGoMinor)
		return c
	}
	c.Status = doctorPass
	c.Detail = fmt.Sprintf("%s (%s/%s)", version, runtime.GOOS, runtime.GOARCH)
	return c
}

// parseGoVersion extracts major/minor from strings like "go1.26.5" or "go1.22".
func parseGoVersion(version string) (int, int, bool) {
	v := strings.TrimPrefix(strings.TrimSpace(version), "go")
	// Trim pre-release/dev suffixes such as "1.24rc1" or "1.22-devel".
	for i, r := range v {
		if (r < '0' || r > '9') && r != '.' {
			v = v[:i]
			break
		}
	}
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return 0, 0, false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return major, minor, true
}

// checkPortAvailable tries to bind the port the local plane would use. A busy
// port is a warning, not a failure: --port moves the plane out of the way.
func checkPortAvailable(port int, listen func(network, address string) (net.Listener, error)) doctorCheck {
	c := doctorCheck{Name: fmt.Sprintf("port %d", port)}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := listen("tcp", addr)
	if err != nil {
		c.Status = doctorWarn
		c.Detail = addr + " is already in use: " + err.Error()
		c.Fix = fmt.Sprintf("start on another port: cafctl up --local --port %d", port+19)
		return c
	}
	_ = ln.Close()
	c.Status = doctorPass
	c.Detail = addr + " is free"
	return c
}

// probeWriteFile writes and removes a temp file to prove the directory is usable.
func probeWriteFile(dir string) error {
	f, err := os.CreateTemp(dir, ".cafctl-doctor-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}

// checkWorkspaceWritable is a hard requirement: init/up must persist keys and
// evidence exports.
func checkWorkspaceWritable(dir string, probe func(string) error) doctorCheck {
	c := doctorCheck{Name: "workspace writable"}
	if err := probe(dir); err != nil {
		c.Status = doctorFail
		c.Detail = "cannot create files in " + dir + ": " + err.Error()
		c.Fix = "run from a directory you own, or pass --dir <writable-path>"
		return c
	}
	c.Status = doctorPass
	c.Detail = dir + " accepts writes"
	return c
}

// checkProjectScaffold reports whether `cafctl init` has been run here. Missing
// scaffold is a warning: 'up --local' still works with an ephemeral signer.
func checkProjectScaffold(dir string, stat func(string) (os.FileInfo, error)) doctorCheck {
	c := doctorCheck{Name: "project scaffold"}
	cafDir := filepath.Join(dir, ".caf")
	if _, err := stat(cafDir); err != nil {
		c.Status = doctorWarn
		c.Detail = "no .caf/ directory in " + dir + " (project not initialized)"
		c.Fix = "cafctl init --yes"
		return c
	}
	keyPath := filepath.Join(cafDir, "keys", "private.pem")
	if _, err := stat(keyPath); err != nil {
		c.Status = doctorWarn
		c.Detail = ".caf/ exists but the signing key is missing — local evidence will use an ephemeral key"
		c.Fix = "cafctl init --yes --force   (regenerates .caf/keys/private.pem)"
		return c
	}
	c.Status = doctorPass
	c.Detail = ".caf/ present with signing key (" + filepath.ToSlash(keyPath) + ")"
	return c
}

// optionalTool describes a tool that unlocks extra capability when present.
type optionalTool struct {
	Binary string
	Unlock string // what the tool enables
	Fix    string // how to get it
}

// optionalTools lists the tools whose absence degrades features but never blocks
// the local path.
func optionalTools() []optionalTool {
	return []optionalTool{
		{"go", "building cafctl and running the Go test suite from source",
			"install Go from https://go.dev/dl (a prebuilt cafctl binary needs no Go)"},
		{"docker", "the full-stack profile (docker compose up / make docker-up-fast)",
			"install Docker Desktop or the docker engine; not needed for 'cafctl up --local'"},
		{"kubectl", "real cluster operations (cafctl deploy run against Kubernetes)",
			"install kubectl and point $KUBECONFIG at a cluster; local mode stays simulated without it"},
		{"helm", "chart-based deployment (deploy/helm/cloudai-fusion)",
			"install helm 3; only needed for Kubernetes installs"},
		{"git", "provenance metadata (commit SHA) on attested operations",
			"install git; attestations still work, just without commit context"},
		{"nvidia-smi", "real GPU topology/MIG scheduling",
			"install NVIDIA drivers on a GPU host; CPU-only development is fully supported"},
	}
}

// checkOptionalTool resolves one optional tool on PATH.
func checkOptionalTool(tool optionalTool, lookPath func(string) (string, error)) doctorCheck {
	c := doctorCheck{Name: "optional: " + tool.Binary}
	path, err := lookPath(tool.Binary)
	if err != nil || path == "" {
		c.Status = doctorWarn
		c.Detail = tool.Binary + " not on PATH — unavailable: " + tool.Unlock
		c.Fix = tool.Fix
		return c
	}
	c.Status = doctorPass
	c.Detail = path
	return c
}

// execLookPath is the real PATH probe, injected into checkOptionalTool so tests
// can supply a fake without touching the machine.
func execLookPath(binary string) (string, error) { return exec.LookPath(binary) }

// printDoctorReport renders the report with textual markers so it stays readable
// without color, and prints a consolidated fix list at the end.
func printDoctorReport(cmd *cobra.Command, report doctorReport) {
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "")
	cyanBold.Fprintln(out, "cafctl doctor — environment self-check")
	fmt.Fprintln(out, Separator('─', 64))

	for _, c := range report.Checks {
		marker, colorFn := "[WARN]", yellow
		switch c.Status {
		case doctorPass:
			marker, colorFn = "[ OK ]", green
		case doctorFail:
			marker, colorFn = "[FAIL]", red
		}
		colorFn.Fprintf(out, "  %s %-22s %s\n", marker, c.Name, c.Detail)
	}

	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  %d passed, %d warning(s), %d failure(s)\n", report.Passed, report.Warned, report.Failed)

	fixes := make([]string, 0, len(report.Checks))
	for _, c := range report.Checks {
		if c.Status != doctorPass && c.Fix != "" {
			fixes = append(fixes, c.Name+": "+c.Fix)
		}
	}
	if len(fixes) > 0 {
		fmt.Fprintln(out, "")
		PrintNextSteps(out, "Actionable fixes:", fixes...)
	}

	fmt.Fprintln(out, "")
	if report.Failed == 0 {
		greenBold.Fprintf(out, "%sReady for 'cafctl up --local' (no optional tool is required)\n", OK())
	} else {
		redBold.Fprintf(out, "%sFix the [FAIL] items above before running 'cafctl up --local'\n", ERROR())
	}
	fmt.Fprintln(out, "")
}
