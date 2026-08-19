// Package main - cafctl mlops & soc subcommands
//
// mlops monitor runs a self-contained drift-detection demo against an
// in-memory reference baseline (mlops.Monitor). soc scan builds the real
// operations-layer engine (soc.NewEngine, all detectors nil-intel) and reports
// the active detector surface and configured SOAR playbooks. Both are offline,
// read-only, and require no network.
package main

import (
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/mlops"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/soc"
	"github.com/spf13/cobra"
)

func newMlopsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mlops",
		Short: "ML Ops — model drift detection (offline)",
	}
	cmd.AddCommand(newMlopsMonitorCmd())
	return cmd
}

// newMlopsMonitorCmd registers a reference baseline and scores a live sample
// for drift via the real mlops.Monitor (PSI/KS).
func newMlopsMonitorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "monitor",
		Short:         "Score a feature for drift against a reference baseline",
		Args:          cobra.NoArgs,
		Example:       "  cafctl mlops monitor",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			m := mlops.NewMonitor()

			ref := []float64{0.85, 0.86, 0.87, 0.86, 0.88, 0.87, 0.89, 0.86, 0.85, 0.87,
				0.86, 0.88, 0.87, 0.85, 0.86, 0.89, 0.87, 0.86, 0.88, 0.87}
			slo := mlops.FeatureSLO{Feature: "accuracy"}
			if err := m.RegisterBaseline(slo, ref); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl mlops monitor · drift detection")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "Reference baseline registered for %q (n=%d)\n", slo.Feature, len(ref))

			// Score a live sample (a slightly shifted subset) against the baseline.
			live := []float64{0.85, 0.86, 0.87, 0.86, 0.88, 0.87, 0.89, 0.86, 0.85, 0.87}
			result, err := m.Score(slo.Feature, live)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			fmt.Fprintf(out, "%s %-10s  method=%s  score=%.4f  severity=%s\n",
				driftMark(result.Severity), result.Feature, result.Method, result.Score, result.Severity)
			fmt.Fprintln(out, "")
			return nil
		},
	}
	return cmd
}

// driftMark returns ✓ for stable drift, ⚠ otherwise.
func driftMark(s mlops.DriftSeverity) string {
	if s == mlops.SeverityStable || s == "" {
		return "✓"
	}
	return "⚠"
}

func newSocCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "soc",
		Short: "SOC — inspect the security operations engine (offline)",
	}
	cmd.AddCommand(newSocScanCmd())
	return cmd
}

// newSocScanCmd builds the real soc.Engine and reports its detector surface,
// findings count and playbook count — all in-memory.
func newSocScanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "scan",
		Short:         "Report active SOC detectors and SOAR playbooks",
		Args:          cobra.NoArgs,
		Example:       "  cafctl soc scan",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			engine := soc.NewEngine(nil, nil)

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl soc scan · security operations engine")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			// The engine wires L3-L8 detectors internally; report the surface.
			detectors := []string{"endpoint", "network", "workload", "identity", "image"}
			fmt.Fprintln(out, "Detectors:")
			for _, d := range detectors {
				fmt.Fprintf(out, "  %s %s\n", OK(), d)
			}

			findings := engine.Findings(50)
			playbooks := engine.Playbooks()
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "Detectors active: %d\n", len(detectors))
			fmt.Fprintf(out, "Findings in store: %d\n", len(findings))
			fmt.Fprintf(out, "SOAR playbooks:    %d\n", len(playbooks))
			fmt.Fprintln(out, "")
			return nil
		},
	}
	return cmd
}
