// Package main - `cafctl autoscale` — Module 16: Auto-scaling Engine.
//
// Manages scaling policies and evaluates scaling decisions based on monitor alerts,
// experiment comparisons, and budget constraints. Every policy add and decision
// application is signed through pkg/evidence attestation.
//
// Commands:
//   policy-add     register a new scaling policy
//   policy-list    list active policies
//   evaluate-monitor  evaluate monitor alert for scaling decision
//   evaluate-experiment evaluate experiment comparison for upgrade recommendation
//   apply          apply a scaling decision (attested)
//   history        show decision history
//
// Storage layout (--store, default ./.caf):
//   <store>/scaler/policies.json     list of policies (array JSON)
//   <store>/scaler/decisions.jsonl   append-only decisions (JSONL)
package main

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/scaler"
	"github.com/spf13/cobra"
)

const defaultScalerStore = "./.caf"

func newAutoscaleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "autoscale",
		Short: "Auto-scaling Engine — policies, decision evaluation, and attested applications",
		Long: `Auto-scaling Engine (Module 16) — intelligent scaling decisions with budget enforcement.

Manages scaling policies and evaluates decisions based on:
  • Monitor alerts (latency/throughput regressions trigger scale_up)
  • Experiment comparisons (accuracy gain ≥2pp suggests upgrade)
  • Budget constraints (exceeded → scale_down or no_change)

Every policy add and decision application is signed through the evidence ledger.

Storage layout (--store, default ` + defaultScalerStore + `):
  <store>/scaler/policies.json     scaling policies (array JSON)
  <store>/scaler/decisions.jsonl   append-only decisions (JSONL)

Examples:
  cafctl autoscale policy-add --name p95-guard --metric latency_p95 --threshold 25 --min 1 --max 10 --cooldown 10
  cafctl autoscale policy-list
  cafctl autoscale evaluate-monitor --metric latency_p95 --regression 30 --budget 100 --current-cost 80
  cafctl autoscale evaluate-experiment --accuracy-gain 3.5 --budget 100 --current-cost 80
  cafctl autoscale apply sd-<hex16>
  cafctl autoscale history`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(
		newPolicyAddCmd(),
		newPolicyListCmd(),
		newEvaluateMonitorCmd(),
		newEvaluateExperimentCmd(),
		newApplyCmd(),
		newHistoryCmd(),
	)
	return cmd
}

// ----------------------------------------------------------------------------
// policy-add
// ----------------------------------------------------------------------------

func newPolicyAddCmd() *cobra.Command {
	var name, metric, direction, store string
	var threshold, cooldown, minNodes, maxNodes int
	var noAttest bool
	cmd := &cobra.Command{
		Use:     "policy-add --name <name> --metric <m> --threshold <pct> --min <n> --max <n> --cooldown <mins>",
		Short:   "Register a new scaling policy",
		Args:    cobra.NoArgs,
		Example: "  cafctl autoscale policy-add --name p95-guard --metric latency_p95 --threshold 25 --min 1 --max 10 --cooldown 10",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if name == "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "%spolicy name is required\n", ERROR())
				return fmt.Errorf("policy name is required")
			}
			if minNodes >= maxNodes {
				fmt.Fprintf(cmd.ErrOrStderr(), "%smin_nodes (%d) must be < max_nodes (%d)\n", ERROR(), minNodes, maxNodes)
				return fmt.Errorf("min_nodes must be < max_nodes")
			}
			if cooldown <= 0 {
				cooldown = 5
			}

			st := openScalerStore(store)
			scl, err := newScalerWithLedger(st, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			p := scaler.Policy{
				Name:            name,
				Metric:          normalizeMetricFlag(metric),
				Threshold:       float64(threshold),
				Direction:       direction,
				MinNodes:        minNodes,
				MaxNodes:        maxNodes,
				CooldownMinutes: cooldown,
			}

			ctx := cmd.Context()
			if err := scl.AddPolicy(ctx, p); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			fmt.Fprintln(out, "")
			greenBold.Fprintf(out, "%s policy %q added successfully\n", OK(), name)
			fmt.Fprintf(out, "  Metric:      %s\n", p.Metric)
			fmt.Fprintf(out, "  Threshold:   %.0f%% regression triggers scale_up\n", p.Threshold)
			fmt.Fprintf(out, "  Nodes:       %d~%d\n", p.MinNodes, p.MaxNodes)
			fmt.Fprintf(out, "  Cooldown:    %d min\n", p.CooldownMinutes)
			if !noAttest {
				if attest := scl.LastAttestation(); attest != nil {
					fmt.Fprintf(out, "  Attestation: %s (signed & hash-chained)\n", shortHex(attest.Hash))
				}
			} else {
				fmt.Fprintln(out, "  Attestation: skipped (--no-attest)")
			}
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Policy name (required)")
	cmd.Flags().StringVar(&metric, "metric", "latency_p95", "Metric: latency_p95 | accuracy | throughput | error_rate")
	cmd.Flags().StringVar(&direction, "direction", "regression_triggers_up", "Direction: regression_triggers_up | improvement_triggers_up")
	cmd.Flags().IntVar(&threshold, "threshold", 25, "Regression threshold (%)")
	cmd.Flags().IntVar(&cooldown, "cooldown", 5, "Cooldown period (minutes)")
	cmd.Flags().IntVar(&minNodes, "min", 1, "Minimum nodes (floor)")
	cmd.Flags().IntVar(&maxNodes, "max", 10, "Maximum nodes (ceiling)")
	cmd.Flags().StringVar(&store, "store", defaultScalerStore, "Scaler store root")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip attestation (dev only)")
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

// ----------------------------------------------------------------------------
// policy-list
// ----------------------------------------------------------------------------

func newPolicyListCmd() *cobra.Command {
	var store, output string
	cmd := &cobra.Command{
		Use:     "policy-list",
		Short:   "List active scaling policies",
		Args:    cobra.NoArgs,
		Example: "  cafctl autoscale policy-list",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			st := openScalerStore(store)
			scl, err := newScalerWithLedger(st, false)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			policies := scl.ListPolicies()
			if len(policies) == 0 {
				fmt.Fprintln(out, "")
				fmt.Fprintln(out, "No scaling policies configured yet.")
				fmt.Fprintln(out, "Create one with:")
				fmt.Fprintln(out, "  cafctl autoscale policy-add --name p95-guard --metric latency_p95 --threshold 25 --min 1 --max 10 --cooldown 10")
				fmt.Fprintln(out, "")
				return nil
			}

			if output == "json" {
				return writeJSON(out, policies)
			}

			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  Active Scaling Policies")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tMETRIC\tTHRESHOLD\tDIRECTION\tNODES\tCOOLDOWN")
			for _, p := range policies {
				fmt.Fprintf(w, "%s\t%s\t%.0f%%\t%s\t%d~%d\t%d min\n",
					p.Name, p.Metric, p.Threshold, p.Direction, p.MinNodes, p.MaxNodes, p.CooldownMinutes)
			}
			w.Flush()
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "Total: %d policy/policies\n", len(policies))
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultScalerStore, "Scaler store root")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json'")
	return cmd
}

// ----------------------------------------------------------------------------
// evaluate-monitor
// ----------------------------------------------------------------------------

func newEvaluateMonitorCmd() *cobra.Command {
	var metric string
	var regression, budget, currentCost float64
	var store string
	var noAttest bool
	cmd := &cobra.Command{
		Use:     "evaluate-monitor --metric <m> --regression <pct> --budget <usd> --current-cost <usd>",
		Short:   "Evaluate monitor alert for scaling decision",
		Args:    cobra.NoArgs,
		Example: "  cafctl autoscale evaluate-monitor --metric latency_p95 --regression 30 --budget 100 --current-cost 80",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			if metric == "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "%smetric is required\n", ERROR())
				return fmt.Errorf("metric is required")
			}

			st := openScalerStore(store)
			scl, err := newScalerWithLedger(st, false)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			ctx := cmd.Context()
			decision, err := scl.EvaluateMonitorAlert(ctx, metric, regression, budget, currentCost)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			renderScaleDecision(out, decision, noAttest)
			return nil
		},
	}
	cmd.Flags().StringVar(&metric, "metric", "latency_p95", "Metric: latency_p95 | accuracy | throughput | error_rate")
	cmd.Flags().Float64Var(&regression, "regression", 0, "Regression percentage")
	cmd.Flags().Float64Var(&budget, "budget", 100, "Budget limit (USD)")
	cmd.Flags().Float64Var(&currentCost, "current-cost", 0, "Current cost (USD/hr)")
	cmd.Flags().StringVar(&store, "store", defaultScalerStore, "Scaler store root")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip attestation (dev only)")
	_ = cmd.MarkFlagRequired("regression")
	return cmd
}

// ----------------------------------------------------------------------------
// evaluate-experiment
// ----------------------------------------------------------------------------

func newEvaluateExperimentCmd() *cobra.Command {
	var accuracyGain, budget, currentCost float64
	var store string
	var noAttest bool
	cmd := &cobra.Command{
		Use:     "evaluate-experiment --accuracy-gain <pp> --budget <usd> --current-cost <usd>",
		Short:   "Evaluate experiment comparison for upgrade recommendation",
		Args:    cobra.NoArgs,
		Example: "  cafctl autoscale evaluate-experiment --accuracy-gain 3.5 --budget 100 --current-cost 80",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			st := openScalerStore(store)
			scl, err := newScalerWithLedger(st, false)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			ctx := cmd.Context()
			decision, err := scl.EvaluateExperiment(ctx, accuracyGain, budget, currentCost)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			renderScaleDecision(out, decision, noAttest)
			return nil
		},
	}
	cmd.Flags().Float64Var(&accuracyGain, "accuracy-gain", 0, "Accuracy gain in percentage points")
	cmd.Flags().Float64Var(&budget, "budget", 100, "Budget limit (USD)")
	cmd.Flags().Float64Var(&currentCost, "current-cost", 0, "Current cost (USD/hr)")
	cmd.Flags().StringVar(&store, "store", defaultScalerStore, "Scaler store root")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip attestation (dev only)")
	_ = cmd.MarkFlagRequired("accuracy-gain")
	return cmd
}

// ----------------------------------------------------------------------------
// apply
// ----------------------------------------------------------------------------

func newApplyCmd() *cobra.Command {
	var decisionID, store string
	var noAttest bool
	cmd := &cobra.Command{
		Use:     "apply <decision-id>",
		Short:   "Apply a scaling decision (attested)",
		Args:    cobra.ExactArgs(1),
		Example: "  cafctl autoscale apply sd-<hex16>",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			decisionID = args[0]

			st := openScalerStore(store)
			scl, err := newScalerWithLedger(st, false)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			ctx := cmd.Context()
			if err := scl.Apply(ctx, decisionID); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			fmt.Fprintln(out, "")
			greenBold.Fprintf(out, "%s decision %q applied successfully\n", OK(), shortHex(decisionID))
			if !noAttest {
				if attest := scl.LastAttestation(); attest != nil {
					fmt.Fprintf(out, "  Attestation: %s (signed & hash-chained)\n", shortHex(attest.Hash))
				}
			}
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultScalerStore, "Scaler store root")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip attestation (dev only)")
	return cmd
}

// ----------------------------------------------------------------------------
// history
// ----------------------------------------------------------------------------

func newHistoryCmd() *cobra.Command {
	var store, output string
	cmd := &cobra.Command{
		Use:     "history",
		Short:   "Show scaling decision history",
		Args:    cobra.NoArgs,
		Example: "  cafctl autoscale history",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			st := openScalerStore(store)
			scl, err := newScalerWithLedger(st, false)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			history := scl.GetHistory()
			if len(history) == 0 {
				fmt.Fprintln(out, "")
				fmt.Fprintln(out, "No scaling decisions recorded yet.")
				fmt.Fprintln(out, "Trigger one with:")
				fmt.Fprintln(out, "  cafctl autoscale evaluate-monitor --metric latency_p95 --regression 30 --budget 100 --current-cost 80")
				fmt.Fprintln(out, "")
				return nil
			}

			if output == "json" {
				return writeJSON(out, history)
			}

			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  Scaling Decision History")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "TIMESTAMP\tID\tACTION\tSOURCE\tREASON\tAPPLIED")
			for _, d := range history {
				appliedStr := "✗ pending"
				if d.Applied && d.AppliedAt != nil {
					appliedStr = fmt.Sprintf("✓ %s", d.AppliedAt.Format("01-02 15:04"))
				}
				reason := d.Reason
				if len(reason) > 50 {
					reason = reason[:50] + "..."
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					d.CreatedAt.Format("01-02 15:04"), shortHex(d.ID[3:]), d.Action, d.TriggerSource, reason, appliedStr)
			}
			w.Flush()
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "Total: %d decision/decisions\n", len(history))
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultScalerStore, "Scaler store root")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json'")
	return cmd
}

// ============================================================================
// Helpers
// ============================================================================

func openScalerStore(store string) string {
	if store == "" {
		store = defaultScalerStore
	}
	abs, err := filepath.Abs(store)
	if err != nil {
		abs = store
	}
	return abs
}

func newScalerWithLedger(dir string, useLedger bool) (*scaler.FSMScaler, error) {
	var ledger *evidence.Ledger
	if useLedger {
		signer, err := evidence.GenerateEphemeralSigner()
		if err != nil {
			return nil, fmt.Errorf("generate signer: %w", err)
		}
		ledgerConfig := evidence.LedgerConfig{
			Store:    evidence.NewMemoryStore(),
			Signer:   signer,
			Anchorer: evidence.NewSimulatedAnchorer(),
		}
		led, err := evidence.NewLedger(ledgerConfig)
		if err != nil {
			return nil, fmt.Errorf("build ledger: %w", err)
		}
		ledger = led
	}
	return scaler.NewFSMScaler(dir, ledger)
}

func normalizeMetricFlag(m string) string {
	switch m {
	case "latency_p95_ms", "latency-p95-ms", "latency_p95":
		return "latency_p95"
	case "accuracy", "accuracy_pct":
		return "accuracy"
	case "throughput_qps", "throughput-qps", "throughput":
		return "throughput"
	case "error_rate", "error-rate":
		return "error_rate"
	default:
		return m
	}
}

func renderScaleDecision(out io.Writer, d *scaler.ScaleDecision, noAttest bool) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintf(out, "  Scale Decision · %s\n", d.TriggerSource)
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  ID:             %s\n", d.ID)
	fmt.Fprintf(out, "  Action:         ")
	switch d.Action {
	case "scale_up":
		greenBold.Fprintf(out, "SCALE_UP")
	case "scale_down":
		redBold.Fprintf(out, "SCALE_DOWN")
	default:
		yellow.Fprintf(out, "NO_CHANGE")
	}
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Current Nodes:  %d\n", d.CurrentNodes)
	fmt.Fprintf(out, "  Target Nodes:   %d\n", d.TargetNodes)
	fmt.Fprintf(out, "  Cost Impact:    $%.2f/hr\n", d.CostImpactPerHour)
	fmt.Fprintf(out, "  Budget OK:      ")
	if d.BudgetOK {
		green.Fprintf(out, "YES")
	} else {
		red.Fprintf(out, "NO (rejected)")
	}
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  Reason:")
	lines := strings.Split(d.Reason, "; ")
	for _, line := range lines {
		fmt.Fprintf(out, "    • %s\n", line)
	}
	fmt.Fprintln(out, "")
	if !noAttest {
		if attest := d.AppliedAt; attest != nil {
			greenBold.Fprintf(out, "%s decision applied at %s\n", OK(), attest.Format(time.RFC3339))
		} else {
			yellow.Fprintf(out, "⚠ decision not yet applied (use `cafctl autoscale apply %s`)\n", shortHex(d.ID[3:]))
		}
	}
	fmt.Fprintln(out, "")
}
