// Package main - cafctl hunt / detect / soar subcommands (AISecOps deep wells).
//
// These three commands surface real, offline, in-memory engines from the
// AISecOps platform so an operator gets instant value with no network:
//
//   - hunt run     (M29, pkg/hunt)  — UEBA behavioral analysis: trains a
//     statistical baseline from known-good observations then scores an
//     anomalous sample, emitting MITRE ATT&CK-mapped findings.
//   - detect sigma (M30, pkg/detect) — evaluates a sample event against the
//     built-in (embedded) Sigma rule set and reports the matches.
//   - soar trigger (M32, pkg/soc)   — runs the SOAR orchestrator: maps a
//     finding to a response playbook and reports the selected actions.
//
// All three are read-only, deterministic (timestamps/IDs are intentionally not
// printed), and require no external dependencies.
package main

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/detect"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/hunt"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/soc"
)

// quietLogger returns a logger that only emits errors, keeping command output
// deterministic and free of engine info-level noise.
func quietLogger() *logrus.Logger {
	l := logrus.New()
	l.SetLevel(logrus.ErrorLevel)
	return l
}

// ----------------------------------------------------------------------------
// hunt run (M29) — behavioral analysis engine (UEBA)
// ----------------------------------------------------------------------------

func newHuntCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hunt",
		Short: "Threat Hunting (L2) — behavioral analytics (offline)",
	}
	cmd.AddCommand(newHuntRunCmd())
	return cmd
}

// newHuntRunCmd trains the UEBA baseline from known-good observations and then
// scores an anomalous sample through the real hunt.Engine behavioral analyzer.
func newHuntRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "run",
		Short:         "Run UEBA behavioral analysis and report anomalies",
		Args:          cobra.NoArgs,
		Example:       "  cafctl hunt run",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			engine := hunt.NewEngine(intel.NewMemoryStore(), nil, quietLogger())

			// Warm the baseline for one entity with 30 known-good samples that
			// carry a small, non-zero variance (bytes_out ≈ 100 ± 5).
			const entity = "user:alice"
			baseline := make([]hunt.Observation, 0, 30)
			for i := 0; i < 30; i++ {
				baseline = append(baseline, hunt.Observation{
					Entity:  entity,
					Metrics: map[string]float64{"bytes_out_mb": 100 + float64(i%5)},
				})
			}
			engine.TrainBehavior(baseline)

			// Score a strongly anomalous exfiltration sample.
			live := []hunt.Observation{{
				Entity:  entity,
				Metrics: map[string]float64{"bytes_out_mb": 50000},
			}}
			findings, err := engine.AnalyzeBehavior(context.Background(), "cli-hunt", live)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl hunt run · UEBA behavioral analysis")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "Baseline trained: %d observations for %q\n", len(baseline), entity)
			fmt.Fprintf(out, "Anomalies found:  %d\n", len(findings))
			fmt.Fprintln(out, "")
			for _, f := range findings {
				fmt.Fprintf(out, "%s %-9s technique=%s severity=%s confidence=%.2f\n",
					WARN(), f.Technique, f.Technique, f.Severity, float64(f.Confidence))
				fmt.Fprintf(out, "    %s\n", f.Title)
			}
			fmt.Fprintln(out, "")
			return nil
		},
	}
	return cmd
}

// ----------------------------------------------------------------------------
// detect sigma (M30) — Sigma rule detection
// ----------------------------------------------------------------------------

func newDetectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "detect",
		Short: "Detection (L3) — Sigma rule evaluation (offline)",
	}
	cmd.AddCommand(newDetectSigmaCmd())
	return cmd
}

// newDetectSigmaCmd loads the embedded Sigma rule set and evaluates a sample
// process-creation event through the real detect.Engine.
func newDetectSigmaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "sigma",
		Short:         "Evaluate a sample event against the embedded Sigma rules",
		Args:          cobra.NoArgs,
		Example:       "  cafctl detect sigma",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			engine, err := detect.NewEmbeddedEngine()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			// A malicious PowerShell encoded-command event (matches the built-in
			// "PowerShell EncodedCommand Execution" rule, ATT&CK T1059.001).
			event := map[string]any{
				"Image":       `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
				"CommandLine": `powershell.exe -enc SQBFAFgAKABJAFcAUgApAA==`,
			}
			matches := engine.Eval("process_creation", event)

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl detect sigma · Sigma rule evaluation")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "Rules loaded: %d (embedded)\n", engine.Len())
			fmt.Fprintf(out, "Matches:      %d\n", len(matches))
			fmt.Fprintln(out, "")
			for _, m := range matches {
				fmt.Fprintf(out, "%s %-9s level=%-8s %s\n", OK(), m.Technique, m.Level, m.Title)
			}
			fmt.Fprintln(out, "")
			return nil
		},
	}
	return cmd
}

// ----------------------------------------------------------------------------
// soar trigger (M32) — SOAR response orchestration
// ----------------------------------------------------------------------------

func newSoarCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "soar",
		Short: "SOAR (L8) — response orchestration (offline)",
	}
	cmd.AddCommand(newSoarTriggerCmd())
	return cmd
}

// newSoarTriggerCmd feeds a finding to the real soc.Orchestrator and reports the
// selected playbook and its response actions.
func newSoarTriggerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "trigger",
		Short:         "Trigger a SOAR playbook for a sample finding",
		Args:          cobra.NoArgs,
		Example:       "  cafctl soar trigger",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			orch := soc.NewOrchestrator(quietLogger())

			// A high-severity C2 egress finding (ATT&CK T1071) selects the
			// built-in "c2-egress" playbook.
			finding := soc.Finding{
				ID:        "cli-finding-1",
				Well:      soc.WellNetwork,
				Technique: "T1071",
				Severity:  intel.SeverityHigh,
				Asset:     "host-9",
				Title:     "C2 beacon to known malicious endpoint",
			}
			resp := orch.Respond(finding)

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl soar trigger · response orchestration")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "Finding:  %s (%s, %s)\n", finding.Title, finding.Technique, finding.Severity)
			fmt.Fprintf(out, "Playbook: %s\n", resp.Playbook)
			fmt.Fprintf(out, "Executed: %v\n", resp.Executed)
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "Actions:")
			for _, a := range resp.Actions {
				auto := "manual"
				if a.Automated {
					auto = "automated"
				}
				fmt.Fprintf(out, "  %s %-18s target=%s (%s)\n", OK(), a.Type, a.Target, auto)
			}
			fmt.Fprintln(out, "")
			return nil
		},
	}
	return cmd
}
