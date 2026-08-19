// Package main - `cafctl up --local`: zero-dependency local startup.
//
// The command boots the embedded local plane (localplane.go), prints the health
// endpoints, and then proves them reachable by probing them over real TCP from
// this process. Nothing here requires Kubernetes, a GPU, a database, Docker, or
// cloud credentials; anything that would require them is reported as degraded
// instead of being faked.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

var (
	upLocal   bool
	upPort    int
	upDir     string
	upSmoke   bool
	upTimeout time.Duration
	upJSON    bool
)

// newUpCmd builds the `up` command.
func newUpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "up [--local]",
		Short: "Start CloudAI Fusion locally with zero dependencies",
		Long: `Start a local CloudAI Fusion control plane with zero dependencies.

--local runs an embedded in-process plane:
  • compute backend: pkg/cloudprovider LocalMockProvider (real CRUD, no credentials)
  • evidence:        real Ed25519-signed hash chain over an in-memory store
  • storage:         none required — no database, no message broker, no cluster

What it is NOT: the full apiserver (cmd/apiserver). That binary requires a real
database, messaging and cluster backends before it boots, so it cannot honor the
zero-dependency promise. Subsystems that need real infrastructure are reported as
simulated on GET /api/v1/capabilities rather than pretended to be real.

After binding, 'up' probes /healthz and /readyz itself and prints the verdict, so
a green run is evidence the service actually answered — not just that a process
started.`,
		Example: `  # Boot the embedded plane and keep it running (Ctrl+C to stop)
  cafctl up --local

  # Boot on another port when 8080 is taken
  cafctl up --local --port 8099

  # CI / timing mode: boot, self-check, shut down, exit
  cafctl up --local --smoke

  # Machine-readable self-check result
  cafctl up --local --smoke --json`,
		RunE: runUp,
	}

	cmd.Flags().BoolVar(&upLocal, "local", true,
		"Run the embedded zero-dependency local plane")
	cmd.Flags().IntVar(&upPort, "port", defaultLocalPort,
		"TCP port to bind on 127.0.0.1")
	cmd.Flags().StringVarP(&upDir, "dir", "d", ".",
		"Project directory containing .caf/ (created by 'cafctl init')")
	cmd.Flags().BoolVar(&upSmoke, "smoke", false,
		"Start, self-check the health endpoints, shut down and exit (for CI and timing)")
	cmd.Flags().DurationVar(&upTimeout, "timeout", 10*time.Second,
		"How long to wait for the health endpoints to answer")
	cmd.Flags().BoolVar(&upJSON, "json", false,
		"Emit the self-check result as JSON")
	return cmd
}

// The init self-registration has been removed; newUpCmd is registered explicitly
// from main.go's init() to keep all command wiring in one place.

// upReport is the machine-readable outcome of a boot + self-check.
type upReport struct {
	Mode         string        `json:"mode"`
	BaseURL      string        `json:"base_url"`
	Ready        bool          `json:"ready"`
	BootMS       int64         `json:"boot_ms"`
	SignerSource string        `json:"signer_source"`
	Probes       []probeResult `json:"probes"`
	Simulated    []Backend     `json:"simulated_backends"`
}

func runUp(cmd *cobra.Command, _ []string) error {
	if !upLocal {
		// Honest refusal instead of a silent half-start: cafctl only owns the
		// local path; the full stack has its own documented entry points.
		PrintError("cafctl up currently implements the --local path only")
		PrintNextSteps(cmd.ErrOrStderr(), "To run the full stack instead:",
			"docker compose -f docker-compose.yml up -d   (needs Docker)",
			"make docker-up-fast                          (fastest full-stack dev profile)",
			"go run ./cmd/apiserver                       (needs DB/messaging/cluster config)",
			"or keep it dependency-free: cafctl up --local")
		return fmt.Errorf("unsupported: up without --local")
	}

	bootStart := time.Now()
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	total := 4
	out := cmd.OutOrStdout()

	PrintStep(out, 1, total, "Building embedded local plane (LocalMockProvider + evidence ledger)")
	plane, err := newLocalPlane(localPlaneConfig{Port: upPort, Dir: upDir})
	if err != nil {
		PrintError("failed to build local plane: %v", err)
		PrintNextSteps(cmd.ErrOrStderr(), "Try:",
			"cafctl doctor              — check Go version, ports and optional tools",
			"cafctl init --yes --force  — regenerate the project scaffold and keys")
		return err
	}
	PrintStepDone(out, "provider=localmock (zero credentials), signer="+plane.signerSource)

	PrintStep(out, 2, total, fmt.Sprintf("Binding %s", plane.BaseURL()))
	if err := plane.Start(ctx); err != nil {
		PrintError("%v", err)
		PrintNextSteps(cmd.ErrOrStderr(), "Try:",
			fmt.Sprintf("cafctl up --local --port %d", upPort+19),
			"cafctl doctor   — reports which ports are already in use")
		return err
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer shutdownCancel()
		_ = plane.Stop(shutdownCtx)
	}()
	PrintStepDone(out, "listening on "+plane.BaseURL())

	PrintStep(out, 3, total, "Self-checking health endpoints over TCP")
	probes := plane.SelfCheck(ctx, upTimeout)
	ready := true
	for _, pr := range probes {
		if !pr.OK() {
			ready = false
		}
	}
	bootMS := time.Since(bootStart).Milliseconds()

	caps := plane.localPlaneCapabilities()
	report := upReport{
		Mode:         "local-embedded",
		BaseURL:      plane.BaseURL(),
		Ready:        ready,
		BootMS:       bootMS,
		SignerSource: plane.signerSource,
		Probes:       probes,
		Simulated:    caps.Simulated,
	}

	if upJSON {
		fmt.Fprintln(out, ToJSON(report))
		if !ready {
			return fmt.Errorf("local plane self-check failed")
		}
		if !upSmoke {
			return waitForSignal(ctx, out, plane)
		}
		return nil
	}

	for _, pr := range probes {
		if pr.OK() {
			green.Fprintf(out, "      [ OK ] %s → %d in %dms\n", pr.URL, pr.StatusCode, pr.LatencyMS)
			continue
		}
		red.Fprintf(out, "      [FAIL] %s → %s\n", pr.URL, firstNonEmpty(pr.Err, fmt.Sprintf("HTTP %d", pr.StatusCode)))
	}
	if !ready {
		PrintError("local plane did not become healthy within %s", upTimeout)
		PrintNextSteps(cmd.ErrOrStderr(), "Try:",
			"cafctl doctor                        — port conflicts and environment issues",
			fmt.Sprintf("cafctl up --local --port %d --timeout 30s", upPort+19))
		return fmt.Errorf("local plane self-check failed")
	}
	PrintStepDone(out, fmt.Sprintf("healthy and ready in %dms", bootMS))

	PrintStep(out, 4, total, "Local plane ready")
	printLocalPlaneBanner(cmd, plane, caps, bootMS)

	if upSmoke {
		greenBold.Fprintf(out, "%sSmoke run complete — shutting down (started, verified, stopped in %dms)\n",
			OK(), time.Since(bootStart).Milliseconds())
		return nil
	}
	return waitForSignal(ctx, out, plane)
}

// printLocalPlaneBanner prints the endpoint list and the honest degradation
// notice. Endpoints come from localPlaneEndpoints() so the banner cannot drift
// from the routes actually served.
func printLocalPlaneBanner(cmd *cobra.Command, plane *localPlane, caps CapabilitiesResponse, bootMS int64) {
	out := cmd.OutOrStdout()
	base := plane.BaseURL()

	fmt.Fprintln(out, "")
	cyanBold.Fprintln(out, "CloudAI Fusion — local plane UP")
	fmt.Fprintln(out, Separator('─', 64))
	greenBold.Fprintf(out, "  Base URL:   %s\n", base)
	fmt.Fprintf(out, "  Boot time:  %dms\n", bootMS)
	fmt.Fprintf(out, "  Mode:       local-embedded (no K8s, no GPU, no DB, no cloud credentials)\n")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  Endpoints:")
	for _, e := range localPlaneEndpoints() {
		fmt.Fprintf(out, "    %-28s %s\n", base+e.Path, e.Desc)
	}

	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Backends (%d real / %d simulated):\n", len(caps.Backends)-caps.SimulatedCount, caps.SimulatedCount)
	for _, b := range caps.Backends {
		marker, colorFn := "[SIM ]", yellow
		if b.Mode == "real" {
			marker, colorFn = "[REAL]", green
		}
		colorFn.Fprintf(out, "    %s %-16s %s\n", marker, b.Component, b.Driver)
		if b.Detail != "" {
			defaultColor.Fprintf(out, "           %s\n", b.Detail)
		}
	}
	if caps.SimulatedCount > 0 {
		fmt.Fprintln(out, "")
		yellow.Fprintf(out, "  %s%d subsystem(s) are simulated in local mode — see %s/api/v1/capabilities\n",
			WARN(), caps.SimulatedCount, base)
	}

	fmt.Fprintln(out, "")
	PrintNextSteps(out, "Try it:",
		"curl "+base+"/healthz",
		"curl "+base+"/readyz",
		`curl -X POST `+base+`/api/v1/cloud/instances -d "{\"name\":\"demo\",\"type\":\"t3.micro\"}"`,
		"cafctl status                 — the existing status panel reads this plane",
		"cafctl doctor                 — environment self-check")
	fmt.Fprintln(out, "")
}

// waitForSignal blocks until SIGINT/SIGTERM, then shuts the plane down. This is
// the interactive path (no --smoke).
func waitForSignal(ctx context.Context, out interface{ Write([]byte) (int, error) }, plane *localPlane) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	yellow.Fprintf(out, "Serving %s — press Ctrl+C to stop.\n", plane.BaseURL())
	select {
	case <-sigCh:
		fmt.Fprintln(out, "")
		PrintInfo("Shutting down local plane...")
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := plane.Stop(shutdownCtx); err != nil {
		return err
	}
	greenBold.Fprintln(out, OK()+"Local plane stopped cleanly")
	return nil
}

// firstNonEmpty returns the first non-empty string, used for error fallbacks.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
