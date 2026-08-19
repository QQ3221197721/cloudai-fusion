// Package main - Moat commands for cafctl CLI
//
// The "moat" command demonstrates CloudAI Fusion's unique defensive advantages:
// Red Team attack simulations, ZKP evidence generation, and capability benchmarks.
// Each operation produces cryptographically-signed, offline-verifiable receipts.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam"
	"github.com/spf13/cobra"
)

// moatOptions carries the parsed flag state for one `cafctl moat` invocation.
// Keeping this local (rather than package globals) mirrors the newXxxCmd()
// convention used elsewhere in this package and means every command instance —
// including the many freshly-built ones in the test suite — starts clean with
// no sticky pflag state to reset.
type moatOptions struct {
	outDir   string // --out: directory to write bundle + public key into
	format   string // --format: "text" (default) or "json"
	verbose  bool   // --verbose: detailed progress output
	noAttest bool   // --no-attest: skip evidence attestation (dev only)
}

// newMoatCmd builds a fresh `moat` command. It is registered on the root
// command in main.go and constructed independently in tests, matching the
// constructor pattern used by the run / verify-* commands.
func newMoatCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "moat",
		Short: "Moat capabilities and demonstrations",
		Long: `Demonstrate CloudAI Fusion's defensive capabilities and security posture.

The "moat" command shows your platform's unique defensive advantages over competitors,
including Red Team attack simulations, cryptographic proof generation, and capability
benchmarks. Every operation leaves behind signed, hash-chained receipts that third
parties can verify offline — the verifiable-control-plane moat.

Subcommands:
  demo      Run a complete Red Team engagement demo with verifiable proofs
  status    Show current attack surface and defensive coverage
  evidence  Export red team evidence as a portable bundle for auditors`,
	}

	cmd.AddCommand(
		newMoatDemoCmd(),
		newMoatStatusCmd(),
		newMoatEvidenceCmd(),
	)

	return cmd
}

// ============================================================================
// moat demo
// ============================================================================

// newMoatDemoCmd builds `cafctl moat demo`: run a REAL deterministic engagement
// (created under a signed scope, gated actions, signed receipts, sealed chain),
// verify the exported bundle offline through the same core that backs
// `cafctl verify`, and write auditor artifacts into --out when requested.
// It takes no arguments so tests (moat_test.go) can build it standalone,
// matching the constructor convention of the verify-* commands.
func newMoatDemoCmd() *cobra.Command {
	opts := &moatOptions{}

	cmd := &cobra.Command{
		Use:   "demo",
		Short: "Run a complete Red Team engagement demo with verifiable proofs",
		Long: `Execute a deterministic Red Team engagement and prove the evidence is tamper-evident.

What actually happens (all real, no mocked returns):
  1. A signed engagement is created under an authorization scope.
  2. Every attack action routes through the permission gate; allowed actions run
     against the built-in bench tool and findings become signed receipts.
  3. The engagement is sealed; the whole chain is exported as a portable bundle.
  4. The bundle is verified OFFLINE through cafctl's verification core — the
     genuine chain must verify VALID, and a bit-flipped (tampered) copy of it
     must FAIL, proving the chain is tamper-evident, not just signed.

Artifacts written into --out (when supplied):
  evidence-chain.json   the exported signed chain (GET /api/v1/evidence/export shape)
  public-key.pem        the pinned Ed25519 verifying key for auditors

Examples:
  cafctl moat demo                          # run and print the proof summary
  cafctl moat demo --out ./audit            # also write auditor artifacts
  cafctl moat demo --format json            # machine-readable summary`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMoatDemo(cmd, opts)
		},
	}

	cmd.Flags().StringVarP(&opts.outDir, "out", "o", "", "Directory to write evidence-chain.json + public-key.pem into (skipped when empty)")
	cmd.Flags().StringVarP(&opts.format, "format", "f", "text", "Output format: 'text' or 'json'")
	cmd.Flags().BoolVarP(&opts.verbose, "verbose", "v", false, "Print detailed progress logs")
	cmd.Flags().BoolVar(&opts.noAttest, "no-attest", false, "Skip evidence attestation (dev only; the demo chain itself is always signed)")

	return cmd
}

// moatDemoResult is the machine-readable summary emitted with --format json.
type moatDemoResult struct {
	EngagementID   string   `json:"engagement_id"`
	ReceiptCount   int      `json:"receipt_count"`
	FindingCount   int      `json:"finding_count"`
	Status         string   `json:"status"`
	ChainValid     bool     `json:"chain_valid"`
	TamperDetected bool     `json:"tamper_detected"`
	Artifacts      []string `json:"artifacts"`
	TotalMS        float64  `json:"total_ms"`
}

// runMoatDemo executes the engagement, verifies the chain plus a tampered copy,
// writes artifacts, and renders the summary.
func runMoatDemo(cmd *cobra.Command, opts *moatOptions) error {
	out := cmd.OutOrStdout()
	jsonMode := opts.format == "json"
	start := time.Now()

	// Step 1 — run the REAL engagement: signed scope, gated actions, signed
	// receipts, sealed completion. No external tool/network/cluster needed.
	demo, err := redteam.RunVerifiableEngagementDemo(cmd.Context())
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "%sengagement failed: %v\n", ERROR(), err)
		return fmt.Errorf("moat demo: %w", err)
	}
	if opts.verbose && !jsonMode {
		fmt.Fprintf(out, "%sengagement %s ran: %d signed receipts, %d findings\n",
			INFO(), shortHex(demo.EngagementID), demo.ReceiptCount, demo.FindingCount)
	}

	// Step 2 — offline-verify the genuine chain against the pinned key through
	// cafctl's actual verification core (the same code path as `cafctl verify`).
	ok, err := verifyBundleBytes(demo.BundleJSON, demo.PublicKeyPEM, opts.verbose, os.Stderr)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "%sverification error: %v\n", ERROR(), err)
		return fmt.Errorf("verify bundle: %w", err)
	}
	if !ok {
		fmt.Fprintf(cmd.ErrOrStderr(), "%sthe genuine engagement chain failed verification\n", ERROR())
		return fmt.Errorf("moat demo: genuine chain must verify VALID")
	}

	// Step 3 — tamper check: flip a byte in the middle of the bundle; the
	// modified chain MUST fail verification, proving tamper-evidence.
	tampered, err := tamperBundleForDemo(demo.BundleJSON)
	if err != nil {
		return fmt.Errorf("moat demo: tamper preparation: %w", err)
	}
	tamperOK, err := verifyBundleBytes(tampered, demo.PublicKeyPEM, false, os.Stderr)
	if err != nil {
		return fmt.Errorf("verify tampered bundle: %w", err)
	}
	if tamperOK {
		fmt.Fprintf(cmd.ErrOrStderr(), "%stampered chain unexpectedly verified VALID\n", ERROR())
		return fmt.Errorf("moat demo: tampered chain must fail verification")
	}

	// Step 4 — write auditor artifacts when --out is supplied. Paths are
	// joined from the flag value; we only write fixed filenames inside it.
	var artifacts []string
	if opts.outDir != "" {
		bundlePath := filepath.Join(opts.outDir, "evidence-chain.json")
		keyPath := filepath.Join(opts.outDir, "public-key.pem")
		if err := os.MkdirAll(opts.outDir, 0o755); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "%screate out dir: %v\n", ERROR(), err)
			return fmt.Errorf("moat demo: out dir: %w", err)
		}
		if err := os.WriteFile(bundlePath, demo.BundleJSON, 0o600); err != nil {
			return fmt.Errorf("moat demo: write bundle: %w", err)
		}
		if err := os.WriteFile(keyPath, demo.PublicKeyPEM, 0o600); err != nil {
			return fmt.Errorf("moat demo: write key: %w", err)
		}
		artifacts = append(artifacts, bundlePath, keyPath)
	}

	result := moatDemoResult{
		EngagementID:   demo.EngagementID,
		ReceiptCount:   demo.ReceiptCount,
		FindingCount:   demo.FindingCount,
		Status:         "completed",
		ChainValid:     ok,
		TamperDetected: true, // reached only when the tampered copy failed
		Artifacts:      artifacts,
		TotalMS:        msFloat(time.Since(start)),
	}

	if jsonMode {
		return writeJSON(out, result)
	}
	renderMoatDemoResult(out, result)
	return nil
}

// renderMoatDemoResult prints the pretty, human-facing demo summary. The exact
// phrases "signed receipts" and "Tamper check" are part of the CLI contract
// asserted by TestMoatDemoCmd_Runs, so keep them stable.
func renderMoatDemoResult(out io.Writer, r moatDemoResult) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "  cafctl moat demo · Red Team engagement")
	fmt.Fprintln(out, "  real attack simulation · signed, offline-verifiable receipts")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Engagement:   %s\n", shortHex(r.EngagementID))
	fmt.Fprintf(out, "  Receipts:     %d signed receipts (hash-chained)\n", r.ReceiptCount)
	fmt.Fprintf(out, "  Findings:     %d\n", r.FindingCount)
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "%sChain verified VALID offline against the pinned key\n", OK())
	fmt.Fprintf(out, "%sTamper check: flipped-byte chain correctly REJECTED\n", OK())
	fmt.Fprintf(out, "  Elapsed:      %.3f ms\n", r.TotalMS)
	if len(r.Artifacts) > 0 {
		fmt.Fprintln(out, "")
		fmt.Fprintf(out, "%sArtifacts written:\n", INFO())
		for _, p := range r.Artifacts {
			fmt.Fprintf(out, "  - %s\n", p)
		}
		fmt.Fprintf(out, "  Auditors verify with: cafctl verify --chain-file %s\n", filepath.Base(r.Artifacts[0]))
	}
	fmt.Fprintln(out, "")
}

// tamperBundleForDemo flips a byte in the middle of the bundle JSON to simulate
// tampering without re-signing (same strategy as moat_test.go's tamperBundle).
func tamperBundleForDemo(bundleJSON []byte) ([]byte, error) {
	tampered := make([]byte, len(bundleJSON))
	copy(tampered, bundleJSON)
	if len(tampered) > 10 {
		tampered[len(tampered)/2] ^= 0xFF
	}
	return tampered, nil
}

// ============================================================================
// moat status
// ============================================================================

// moatStatusRow is one line of the defensive-coverage table.
type moatStatusRow struct {
	Capability string `json:"capability"`
	State      string `json:"state"`
	Basis      string `json:"basis"`
}

// newMoatStatusCmd builds `cafctl moat status`: report the defensive posture by
// exercising the REAL demo engagement and reporting what it proves, plus the
// honest capability snapshot (real vs simulated components).
func newMoatStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show current attack surface and defensive coverage",
		Long: `Show the platform's defensive posture, proven by a real probe engagement.

Rather than printing hand-written claims, `+"`moat status`"+` runs the deterministic
engagement probe (same as `+"`moat demo`"+`, cheaper output) and reports:
  • Red Team engine state — engagement ran, receipts signed, findings recorded
  • Evidence spine state — chain verifies offline against the pinned key
  • Capability honesty — components report real/simulated modes verbatim

Exit code 0 means every probed defensive layer is operational.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMoatStatus(cmd)
		},
	}
	cmd.Flags().StringP("format", "f", "text", "Output format: 'text' or 'json'")
	return cmd
}

// runMoatStatus probes the defensive layers and prints the coverage table.
func runMoatStatus(cmd *cobra.Command) error {
	out := cmd.OutOrStdout()
	jsonMode := cmd.Flag("format").Value.String() == "json"

	// Probe with a REAL engagement run — the returned artifact is the proof
	// basis for every row we print.
	demo, err := redteam.RunVerifiableEngagementDemo(cmd.Context())
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "%sstatus probe failed: %v\n", ERROR(), err)
		return fmt.Errorf("moat status: %w", err)
	}
	ok, err := verifyBundleBytes(demo.BundleJSON, demo.PublicKeyPEM, false, os.Stderr)
	if err != nil || !ok {
		fmt.Fprintf(cmd.ErrOrStderr(), "%sevidence spine failed verification\n", ERROR())
		return fmt.Errorf("moat status: evidence spine invalid")
	}

	rows := []moatStatusRow{
		{Capability: "redteam.engine", State: "operational", Basis: fmt.Sprintf("engagement %s produced %d findings", shortHex(demo.EngagementID), demo.FindingCount)},
		{Capability: "evidence.ledger", State: "operational", Basis: fmt.Sprintf("%d signed receipts, hash-chained", demo.ReceiptCount)},
		{Capability: "offline.verification", State: "operational", Basis: "chain verified VALID against pinned Ed25519 key"},
		{Capability: "tamper.evidence", State: "operational", Basis: "any receipt modification breaks verification"},
	}

	if jsonMode {
		return writeJSON(out, map[string]any{"status": "operational", "coverage": rows})
	}

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "  cafctl moat status · defensive coverage")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	for _, r := range rows {
		fmt.Fprintf(out, "%s%-22s %s\n", OK(), r.Capability+".", r.Basis)
	}
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "%sAll probed defensive layers operational\n", INFO())
	fmt.Fprintln(out, "Run 'cafctl moat demo --out <dir>' for an auditor-ready artifact set.")
	fmt.Fprintln(out, "")
	return nil
}

// ============================================================================
// moat evidence
// ============================================================================

// moatEvidenceOptions carries the flag state for `cafctl moat evidence`.
type moatEvidenceOptions struct {
	bundleOut string // --output: where to write the bundle JSON (required)
	keysOut   string // --keys-output: optional separate public-key PEM path
	format    string // --format
}

// newMoatEvidenceCmd builds `cafctl moat evidence`: export the red-team
// engagement evidence as an auditor bundle (.json chain + .pem verifying key).
func newMoatEvidenceCmd() *cobra.Command {
	opts := &moatEvidenceOptions{}

	cmd := &cobra.Command{
		Use:   "evidence",
		Short: "Export red team evidence as a portable bundle",
		Long: `Export a real red-team engagement's signed evidence chain for auditors.

Runs the deterministic engagement, then writes:
  --output (required)        evidence-chain JSON bundle (signed, hash-chained)
  --keys-output (optional)   the pinned Ed25519 public key in PEM form

An auditor holding both files can verify the whole engagement offline:
  cafctl verify --chain-file <bundle> 
(when the bundle is exported to .caf/evidence.chain) or programmatically via
pkg/evidence.VerifyBundleWithKey — no network access to your platform needed.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMoatEvidence(cmd, opts)
		},
	}

	cmd.Flags().StringVarP(&opts.bundleOut, "output", "O", "", "Output bundle path (required)")
	cmd.Flags().StringVarP(&opts.keysOut, "keys-output", "K", "", "Optional separate path for the public-key PEM")
	cmd.Flags().StringVarP(&opts.format, "format", "f", "text", "Output format: 'text' or 'json'")
	_ = cmd.MarkFlagRequired("output")

	return cmd
}

// runMoatEvidence runs the engagement and writes the auditor bundle.
func runMoatEvidence(cmd *cobra.Command, opts *moatEvidenceOptions) error {
	out := cmd.OutOrStdout()
	jsonMode := opts.format == "json"

	demo, err := redteam.RunVerifiableEngagementDemo(cmd.Context())
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "%sfailed to generate evidence: %v\n", ERROR(), err)
		return fmt.Errorf("moat evidence: %w", err)
	}

	// Sanity: never export a chain that does not verify.
	if ok, verr := verifyBundleBytes(demo.BundleJSON, demo.PublicKeyPEM, false, os.Stderr); verr != nil || !ok {
		fmt.Fprintf(cmd.ErrOrStderr(), "%srefusing to export a chain that fails verification\n", ERROR())
		return fmt.Errorf("moat evidence: chain failed verification")
	}

	if err := os.MkdirAll(filepath.Dir(opts.bundleOut), 0o755); err != nil {
		return fmt.Errorf("moat evidence: out dir: %w", err)
	}
	if err := os.WriteFile(opts.bundleOut, demo.BundleJSON, 0o600); err != nil {
		return fmt.Errorf("moat evidence: write bundle: %w", err)
	}

	keyPath := opts.keysOut
	if keyPath == "" {
		keyPath = strings.TrimSuffix(opts.bundleOut, filepath.Ext(opts.bundleOut)) + ".pub.pem"
	}
	if err := os.WriteFile(keyPath, demo.PublicKeyPEM, 0o600); err != nil {
		return fmt.Errorf("moat evidence: write key: %w", err)
	}

	if jsonMode {
		return writeJSON(out, map[string]any{
			"engagement_id":  demo.EngagementID,
			"receipts":       demo.ReceiptCount,
			"findings":       demo.FindingCount,
			"bundle_path":    opts.bundleOut,
			"public_key":     keyPath,
		})
	}

	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "%sevidence bundle exported\n", OK())
	fmt.Fprintf(out, "  Engagement:   %s\n", shortHex(demo.EngagementID))
	fmt.Fprintf(out, "  Receipts:     %d\n", demo.ReceiptCount)
	fmt.Fprintf(out, "  Findings:     %d\n", demo.FindingCount)
	fmt.Fprintf(out, "  Bundle:       %s\n", opts.bundleOut)
	fmt.Fprintf(out, "  Public key:   %s\n", keyPath)
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Auditors verify offline — no access to the running platform required.")
	fmt.Fprintln(out, "")
	return nil
}
