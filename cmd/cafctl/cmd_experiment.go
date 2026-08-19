// Package main - `cafctl experiment` — the AI/ML layer's fourth module
// (Module 19), the Experiment Tracking System. Together with Module 13 (model
// registry), Module 14 (training orchestrator), and Module 20 (performance
// monitor) it completes the MLOps loop:
//
//	register → train → monitor → experiment compare → pick the winner to deploy.
//
// Commands follow the newXxxCmd() constructor pattern used by model/train/monitor,
// so tests can build fresh, parent-less command instances and Execute them
// directly. NOTE: each Use field starts with the literal subcommand name
// ("start <name>", "metric <exp-id>", …) — cobra routes on the first word.
//
// Every mutation (start/metric/complete/fail) writes a genuine signed,
// hash-chained attestation through the real pkg/evidence ledger (MemoryStore +
// EphemeralSigner + SimulatedAnchorer — the exact wiring `cafctl train` uses).
package main

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/experiment"
	"github.com/spf13/cobra"
)

// defaultExperimentStore is the default experiment store root (experiments live
// in <store>/experiments/<exp-id>.json), matching the .caf layout of the other
// AI/ML modules.
const defaultExperimentStore = "./.caf"

// newExperimentCmd builds the `experiment` command group.
func newExperimentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "experiment",
		Short: "Experiment Tracking — hyperparams, metric streams, signed receipts, A/B compare",
		Long: `Experiment Tracking System (Module 19) — completes the MLOps loop:
register -> train -> monitor -> experiment compare -> pick the winner.

Start an experiment with hyperparameters, stream metrics while it runs,
complete it with a model version reference (or fail it with a reason), then
compare two experiments head-to-head: hyperparameter diffs (only what changed)
and metric tables with honest Δ% = (B-A)/|A|*100 math.

Every mutation writes a signed, hash-chained attestation through pkg/evidence.
Duplicate experiment names are allowed — identity is the unique exp-<hex> ID.

Storage layout (--store, default ` + defaultExperimentStore + `):
  <store>/experiments/<exp-id>.json   one experiment record with full metric history

Examples:
  cafctl experiment start cifar-lr-sweep --hp lr=0.001,batch=32,epochs=50
  cafctl experiment metric exp-abc123 --metric accuracy=0.94,loss=0.12
  cafctl experiment complete exp-abc123 --model resnet50:1.1.0
  cafctl experiment fail exp-def456 --reason "OOM"
  cafctl experiment list
  cafctl experiment show exp-abc123
  cafctl experiment compare exp-abc123 exp-def456`,
	}
	cmd.AddCommand(
		newExperimentStartCmd(),
		newExperimentMetricCmd(),
		newExperimentCompleteCmd(),
		newExperimentFailCmd(),
		newExperimentListCmd(),
		newExperimentShowCmd(),
		newExperimentCompareCmd(),
	)
	return cmd
}

// ----------------------------------------------------------------------------
// experiment start
// ----------------------------------------------------------------------------

// newExperimentStartCmd builds `cafctl experiment start <name>`.
func newExperimentStartCmd() *cobra.Command {
	var (
		store, hp, job, output string
		noAttest               bool
	)
	cmd := &cobra.Command{
		Use:   "start <name>",
		Short: "Start a new experiment (running + signed attestation)",
		Args:  cobra.ExactArgs(1),
		Example: `  cafctl experiment start cifar-lr-sweep --hp lr=0.001,batch=32,epochs=50 \
      --job job-abc123def4567890`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			tracker, err := openExperimentTracker(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			hyperparams, perr := parseStringPairs(hp)
			if perr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), perr)
				return perr
			}
			exp, err := tracker.Start(cmd.Context(), experiment.StartInput{
				Name:           args[0],
				Hyperparams:    hyperparams,
				TrainingJobRef: job,
			})
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			if output == "json" {
				return writeJSON(cmd.OutOrStdout(), buildExperimentStartResult(tracker, exp))
			}
			renderExperimentStarted(cmd.OutOrStdout(), tracker, exp)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultExperimentStore, "Experiment store root")
	cmd.Flags().StringVar(&hp, "hp", "", "Hyperparameters as comma-separated key=value pairs (e.g. lr=0.001,batch=32)")
	cmd.Flags().StringVar(&job, "job", "", "Optional training job reference (Module 14 job id)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	return cmd
}

// experimentStartResult is the --output json payload for a successful start.
type experimentStartResult struct {
	ExperimentID    string            `json:"experiment_id"`
	Name            string            `json:"name"`
	Status          string            `json:"status"`
	Hyperparams     map[string]string `json:"hyperparams,omitempty"`
	TrainingJobRef  string            `json:"training_job_ref,omitempty"`
	AttestationHash string            `json:"attestation_hash,omitempty"`
}

// buildExperimentStartResult assembles the JSON payload; the attestation hash is
// empty when --no-attest was used.
func buildExperimentStartResult(tracker *experiment.FSTracker, exp *experiment.Experiment) experimentStartResult {
	r := experimentStartResult{
		ExperimentID:   exp.ID,
		Name:           exp.Name,
		Status:         string(exp.Status),
		Hyperparams:    exp.Hyperparams,
		TrainingJobRef: exp.TrainingJobRef,
	}
	if last := tracker.LastAttestation(); last != nil {
		r.AttestationHash = last.Hash
	}
	return r
}

// renderExperimentStarted prints the human-facing start receipt.
func renderExperimentStarted(out io.Writer, tracker *experiment.FSTracker, exp *experiment.Experiment) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintf(out, "  cafctl experiment start · %s (%s)\n", exp.ID, exp.Name)
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Status:   %s\n", exp.Status)
	if len(exp.Hyperparams) > 0 {
		fmt.Fprintf(out, "  Params:   %s\n", formatHyperparams(exp.Hyperparams))
	} else {
		fmt.Fprintf(out, "  Params:   %s\n", orDash(""))
	}
	if exp.TrainingJobRef != "" {
		fmt.Fprintf(out, "  Job ref:  %s\n", exp.TrainingJobRef)
	}
	fmt.Fprintf(out, "  Store:    %s\n", exp.ID+".json")
	fmt.Fprintln(out, "")
	if last := tracker.LastAttestation(); last != nil {
		greenBold.Fprintf(out, "%s experiment %s is running\n", OK(), exp.ID)
		fmt.Fprintf(out, "  Attestation: seq #%d hash %s\n", last.Seq, shortHex(last.Hash))
		fmt.Fprintln(out, "  Receipt signed & hash-chained — the hypothesis is now part of your record.")
	} else {
		greenBold.Fprintf(out, "%s experiment %s is running\n", OK(), exp.ID)
		fmt.Fprintln(out, "  Attestation: skipped (--no-attest; dev only, no verifiable receipt)")
	}
	fmt.Fprintln(out, "")
}

// ----------------------------------------------------------------------------
// experiment metric
// ----------------------------------------------------------------------------

// newExperimentMetricCmd builds `cafctl experiment metric <exp-id>`.
func newExperimentMetricCmd() *cobra.Command {
	var (
		store, metrics, output string
		noAttest               bool
	)
	cmd := &cobra.Command{
		Use:           "metric <exp-id>",
		Short:         "Append metric values (running experiments only)",
		Args:          cobra.ExactArgs(1),
		Example:       "  cafctl experiment metric exp-abc123 --metric accuracy=0.94,loss=0.12",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			tracker, err := openExperimentTracker(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			pairs, perr := parseFloatPairs(metrics)
			if perr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), perr)
				return perr
			}
			if len(pairs) == 0 {
				err := fmt.Errorf("experiment: --metric requires at least one name=value pair (e.g. accuracy=0.94)")
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			// Deterministic order so attestations and output are reproducible.
			names := make([]string, 0, len(pairs))
			for k := range pairs {
				names = append(names, k)
			}
			sort.Strings(names)
			for _, name := range names {
				if err := tracker.LogMetric(cmd.Context(), args[0], name, pairs[name]); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
					return err
				}
			}
			exp, err := tracker.Get(cmd.Context(), args[0])
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			out := cmd.OutOrStdout()
			if output == "json" {
				return writeJSON(out, buildExperimentMetricResult(tracker, exp, pairs))
			}
			renderExperimentMetric(out, tracker, exp, pairs)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultExperimentStore, "Experiment store root")
	cmd.Flags().StringVar(&metrics, "metric", "", "Metrics as comma-separated name=value pairs (e.g. accuracy=0.94,loss=0.12)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	return cmd
}

// experimentMetricResult is the --output json payload for a metric append.
type experimentMetricResult struct {
	ExperimentID    string             `json:"experiment_id"`
	Logged          map[string]float64 `json:"logged"`
	Latest          map[string]float64 `json:"latest"`
	PointsTotal     int                `json:"points_total"`
	Status          string             `json:"status"`
	AttestationHash string             `json:"attestation_hash,omitempty"`
}

func buildExperimentMetricResult(tracker *experiment.FSTracker, exp *experiment.Experiment, logged map[string]float64) experimentMetricResult {
	r := experimentMetricResult{
		ExperimentID: exp.ID,
		Logged:       logged,
		Latest:       exp.Metrics,
		PointsTotal:  len(exp.MetricHistory),
		Status:       string(exp.Status),
	}
	if last := tracker.LastAttestation(); last != nil {
		r.AttestationHash = last.Hash
	}
	return r
}

func renderExperimentMetric(out io.Writer, tracker *experiment.FSTracker, exp *experiment.Experiment, logged map[string]float64) {
	fmt.Fprintln(out, "")
	names := make([]string, 0, len(logged))
	for k := range logged {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, fmt.Sprintf("%s=%.6g", n, logged[n]))
	}
	greenBold.Fprintf(out, "%s logged %s → %s (%d history points total)\n", OK(), strings.Join(parts, ", "), exp.ID, len(exp.MetricHistory))
	if last := tracker.LastAttestation(); last != nil {
		fmt.Fprintf(out, "  Attestation: seq #%d hash %s\n", last.Seq, shortHex(last.Hash))
	}
}

// ----------------------------------------------------------------------------
// experiment complete
// ----------------------------------------------------------------------------

// newExperimentCompleteCmd builds `cafctl experiment complete <exp-id>`.
func newExperimentCompleteCmd() *cobra.Command {
	var (
		store, model, output string
		noAttest             bool
	)
	cmd := &cobra.Command{
		Use:           "complete <exp-id>",
		Short:         "Complete an experiment (running → completed, optionally linking a model version)",
		Args:          cobra.ExactArgs(1),
		Example:       "  cafctl experiment complete exp-abc123 --model resnet50:1.1.0",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			tracker, err := openExperimentTracker(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			if err := tracker.Complete(cmd.Context(), args[0], model); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			exp, err := tracker.Get(cmd.Context(), args[0])
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			if output == "json" {
				return writeJSON(cmd.OutOrStdout(), buildExperimentTerminalResult(tracker, exp))
			}
			renderExperimentTerminal(cmd.OutOrStdout(), tracker, exp, "complete")
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultExperimentStore, "Experiment store root")
	cmd.Flags().StringVar(&model, "model", "", "Model version reference to link (e.g. resnet50:1.1.0; optional)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	return cmd
}

// ----------------------------------------------------------------------------
// experiment fail
// ----------------------------------------------------------------------------

// newExperimentFailCmd builds `cafctl experiment fail <exp-id>`.
func newExperimentFailCmd() *cobra.Command {
	var (
		store, reason, output string
		noAttest              bool
	)
	cmd := &cobra.Command{
		Use:           "fail <exp-id>",
		Short:         "Fail an experiment (running → failed, reason recorded)",
		Args:          cobra.ExactArgs(1),
		Example:       `  cafctl experiment fail exp-abc123 --reason "OOM"`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			tracker, err := openExperimentTracker(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			if reason == "" {
				reason = "unspecified"
			}
			if err := tracker.Fail(cmd.Context(), args[0], reason); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			exp, err := tracker.Get(cmd.Context(), args[0])
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			if output == "json" {
				return writeJSON(cmd.OutOrStdout(), buildExperimentTerminalResult(tracker, exp))
			}
			renderExperimentTerminal(cmd.OutOrStdout(), tracker, exp, "fail")
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultExperimentStore, "Experiment store root")
	cmd.Flags().StringVar(&reason, "reason", "", "Failure reason (recorded in the experiment)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	return cmd
}

// experimentTerminalResult is the --output json payload for complete/fail.
type experimentTerminalResult struct {
	ExperimentID    string             `json:"experiment_id"`
	Name            string             `json:"name"`
	Status          string             `json:"status"`
	ModelVersionRef string             `json:"model_version_ref,omitempty"`
	FailReason      string             `json:"fail_reason,omitempty"`
	Latest          map[string]float64 `json:"latest_metrics,omitempty"`
	PointsTotal     int                `json:"points_total"`
	AttestationHash string             `json:"attestation_hash,omitempty"`
}

func buildExperimentTerminalResult(tracker *experiment.FSTracker, exp *experiment.Experiment) experimentTerminalResult {
	r := experimentTerminalResult{
		ExperimentID:    exp.ID,
		Name:            exp.Name,
		Status:          string(exp.Status),
		ModelVersionRef: exp.ModelVersionRef,
		FailReason:      exp.FailReason,
		Latest:          exp.Metrics,
		PointsTotal:     len(exp.MetricHistory),
	}
	if last := tracker.LastAttestation(); last != nil {
		r.AttestationHash = last.Hash
	}
	return r
}

// renderExperimentTerminal prints the human-facing receipt for complete/fail.
func renderExperimentTerminal(out io.Writer, tracker *experiment.FSTracker, exp *experiment.Experiment, verb string) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintf(out, "  cafctl experiment %s · %s (%s)\n", verb, exp.ID, exp.Name)
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Status:  %s\n", exp.Status)
	if exp.ModelVersionRef != "" {
		fmt.Fprintf(out, "  Model:   %s\n", exp.ModelVersionRef)
	}
	if exp.FailReason != "" {
		fmt.Fprintf(out, "  Reason:  %s\n", exp.FailReason)
	}
	if len(exp.Metrics) > 0 {
		fmt.Fprintf(out, "  Final:   %s\n", formatMetricMap(exp.Metrics))
	}
	if !exp.CompletedAt.IsZero() {
		fmt.Fprintf(out, "  Ended:   %s\n", exp.CompletedAt.Format("2006-01-02 15:04:05 UTC"))
	}
	fmt.Fprintln(out, "")
	if last := tracker.LastAttestation(); last != nil {
		greenBold.Fprintf(out, "%s experiment %s is %s\n", OK(), exp.ID, exp.Status)
		fmt.Fprintf(out, "  Attestation: seq #%d hash %s\n", last.Seq, shortHex(last.Hash))
		fmt.Fprintln(out, "  Receipt signed & hash-chained — terminal state is immutable and provable.")
	} else {
		greenBold.Fprintf(out, "%s experiment %s is %s\n", OK(), exp.ID, exp.Status)
		fmt.Fprintln(out, "  Attestation: skipped (--no-attest; dev only, no verifiable receipt)")
	}
	fmt.Fprintln(out, "")
}

// ----------------------------------------------------------------------------
// experiment list
// ----------------------------------------------------------------------------

// newExperimentListCmd builds `cafctl experiment list`.
func newExperimentListCmd() *cobra.Command {
	var store string
	cmd := &cobra.Command{
		Use:           "list",
		Short:         "List experiments (newest first)",
		Example:       "  cafctl experiment list",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			tracker, err := openExperimentTracker(store, false)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			exps := tracker.List(cmd.Context())
			out := cmd.OutOrStdout()
			if len(exps) == 0 {
				fmt.Fprintln(out, "No experiments yet.")
				fmt.Fprintln(out, "Start your first one:")
				fmt.Fprintln(out, "  cafctl experiment start my-sweep --hp lr=0.001,batch=32")
				return nil
			}
			renderExperimentList(out, exps)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultExperimentStore, "Experiment store root")
	return cmd
}

// renderExperimentList prints the experiment table (ID/NAME/STATUS/METRICS/CREATED).
func renderExperimentList(out io.Writer, exps []experiment.Experiment) {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tSTATUS\tMETRICS\tCREATED")
	for _, e := range exps {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n",
			e.ID, e.Name, e.Status, len(e.Metrics), e.CreatedAt.Format("2006-01-02 15:04"))
	}
	w.Flush()
}

// ----------------------------------------------------------------------------
// experiment show
// ----------------------------------------------------------------------------

// newExperimentShowCmd builds `cafctl experiment show <exp-id>`.
func newExperimentShowCmd() *cobra.Command {
	var store string
	var history int
	cmd := &cobra.Command{
		Use:           "show <exp-id>",
		Short:         "Show experiment detail (hyperparams, latest metrics, history tail)",
		Args:          cobra.ExactArgs(1),
		Example:       "  cafctl experiment show exp-abc123",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			tracker, err := openExperimentTracker(store, false)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			exp, err := tracker.Get(cmd.Context(), args[0])
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			renderExperimentShow(cmd.OutOrStdout(), exp, history)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultExperimentStore, "Experiment store root")
	cmd.Flags().IntVar(&history, "history", 8, "Number of metric-history tail entries to show")
	return cmd
}

// renderExperimentShow prints the detail view: hyperparameter table, latest
// metrics, and the tail of the metric history.
func renderExperimentShow(out io.Writer, exp *experiment.Experiment, history int) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintf(out, "  cafctl experiment show · %s (%s)\n", exp.ID, exp.Name)
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Status:  %s\n", exp.Status)
	fmt.Fprintf(out, "  Created: %s\n", exp.CreatedAt.Format("2006-01-02 15:04:05 UTC"))
	if exp.TrainingJobRef != "" {
		fmt.Fprintf(out, "  Job ref: %s\n", exp.TrainingJobRef)
	}
	if exp.ModelVersionRef != "" {
		fmt.Fprintf(out, "  Model:   %s\n", exp.ModelVersionRef)
	}
	if exp.FailReason != "" {
		fmt.Fprintf(out, "  Reason:  %s\n", exp.FailReason)
	}
	if !exp.CompletedAt.IsZero() {
		fmt.Fprintf(out, "  Ended:   %s\n", exp.CompletedAt.Format("2006-01-02 15:04:05 UTC"))
	}

	if len(exp.Hyperparams) > 0 {
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "  Hyperparameters:")
		w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		for _, k := range sortedKeys(exp.Hyperparams) {
			fmt.Fprintf(w, "    %s\t= %s\n", k, exp.Hyperparams[k])
		}
		w.Flush()
	}
	if len(exp.Metrics) > 0 {
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "  Latest metrics:")
		w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		for _, k := range sortedKeys(exp.Metrics) {
			fmt.Fprintf(w, "    %s\t= %.6g\n", k, exp.Metrics[k])
		}
		w.Flush()
	}
	if len(exp.MetricHistory) > 0 {
		fmt.Fprintln(out, "")
		tail := exp.MetricHistory
		if len(tail) > history {
			tail = tail[len(tail)-history:]
		}
		fmt.Fprintf(out, "  Metric history (last %d of %d):\n", len(tail), len(exp.MetricHistory))
		w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		for _, m := range tail {
			fmt.Fprintf(w, "    %s\t%s\t%+.6g\n", m.At.Format("15:04:05"), m.Name, m.Value)
		}
		w.Flush()
	}
	fmt.Fprintln(out, "")
}

// ----------------------------------------------------------------------------
// experiment compare
// ----------------------------------------------------------------------------

// newExperimentCompareCmd builds `cafctl experiment compare <exp-a> <exp-b>`:
// hyperparameter diff table (only differences) + metric comparison table with
// honest Δ% math, colored by direction and missing-side annotations.
func newExperimentCompareCmd() *cobra.Command {
	var store string
	cmd := &cobra.Command{
		Use:           "compare <exp-a> <exp-b>",
		Short:         "Compare two experiments: hyperparam diffs + metric Δ% table",
		Args:          cobra.ExactArgs(2),
		Example:       "  cafctl experiment compare exp-abc123 exp-def456",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			tracker, err := openExperimentTracker(store, false)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			res, err := tracker.Compare(cmd.Context(), args[0], args[1])
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			renderExperimentCompare(cmd.OutOrStdout(), res)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultExperimentStore, "Experiment store root")
	return cmd
}

// renderExperimentCompare prints the colored head-to-head comparison.
func renderExperimentCompare(out io.Writer, res *experiment.CompareResult) {
	a, b := res.A, res.B
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintf(out, "  cafctl experiment compare\n")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  A: %s (%s) · status %s\n", a.ID, a.Name, a.Status)
	fmt.Fprintf(out, "  B: %s (%s) · status %s\n", b.ID, b.Name, b.Status)

	// Hyperparameters — differences only.
	fmt.Fprintln(out, "  Hyperparameters (differences only):")
	if len(res.HyperparamDiff) == 0 {
		green.Fprintf(out, "    ✓ identical\n")
	} else {
		w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "    PARAM\tA\tB")
		for _, k := range sortedPairKeys(res.HyperparamDiff) {
			va, vb := res.HyperparamDiff[k][0], res.HyperparamDiff[k][1]
			if va == "" {
				va = "—"
			}
			if vb == "" {
				vb = "—"
			}
			fmt.Fprintf(w, "    %s\t%s\t%s\n", k, va, vb)
		}
		w.Flush()
	}
	fmt.Fprintln(out, "")

	// Metrics — union with Δ% = (B-A)/|A|*100.
	fmt.Fprintln(out, "  Metrics (Δ% = (B-A)/|A|*100):")
	if len(res.MetricCompare) == 0 {
		fmt.Fprintln(out, "    — no metrics logged on either side")
	} else {
		w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "    METRIC\tA\tB\tΔ%")
		for _, k := range sortedMetricCompareKeys(res.MetricCompare) {
			va, vb := res.MetricCompare[k][0], res.MetricCompare[k][1]
			aCell := fmt.Sprintf("%.6g", va)
			if _, ok := a.Metrics[k]; !ok {
				aCell = "—"
			}
			bCell := fmt.Sprintf("%.6g", vb)
			if _, ok := b.Metrics[k]; !ok {
				bCell = "—"
			}
			deltaCell := formatDeltaPct(res.MetricDeltaPct[k])
			fmt.Fprintf(w, "    %s\t%s\t%s\t%s\n", k, aCell, bCell, deltaCell)
		}
		w.Flush()
		for _, k := range sortedMetricCompareKeys(res.MetricCompare) {
			if _, ok := a.Metrics[k]; !ok {
				yellow.Fprintf(out, "    ⚠ %s missing-in-A (reads as 0 in Δ%%)\n", k)
			}
			if _, ok := b.Metrics[k]; !ok {
				yellow.Fprintf(out, "    ⚠ %s missing-in-B (reads as 0 in Δ%%)\n", k)
			}
		}
	}
	fmt.Fprintln(out, "")
}

// ----------------------------------------------------------------------------
// Shared helpers
// ----------------------------------------------------------------------------

// openExperimentTracker opens (creating if needed) an experiment tracker rooted
// at path/experiments. When attest is true a fresh MemoryStore+EphemeralSigner
// ledger is wired in — exactly the pattern `cafctl train` and `cafctl model`
// use — so receipts are genuinely signed and hash-chained.
func openExperimentTracker(path string, attest bool) (*experiment.FSTracker, error) {
	if path == "" {
		path = defaultExperimentStore
	}
	var ledger *evidence.Ledger
	if attest {
		signer, serr := evidence.GenerateEphemeralSigner()
		if serr != nil {
			return nil, fmt.Errorf("generate signer: %w", serr)
		}
		l, lerr := evidence.NewLedger(evidence.LedgerConfig{
			Store:    evidence.NewMemoryStore(),
			Signer:   signer,
			Anchorer: evidence.NewSimulatedAnchorer(),
		})
		if lerr != nil {
			return nil, fmt.Errorf("build ledger: %w", lerr)
		}
		ledger = l
	}
	return experiment.NewFSTracker(path, ledger)
}

// parseStringPairs parses comma-separated key=value pairs ("lr=0.001,batch=32")
// into a map. Empty input yields an empty map. Values may themselves contain
// '=' (split on the first '=').
func parseStringPairs(s string) (map[string]string, error) {
	out := map[string]string{}
	if strings.TrimSpace(s) == "" {
		return out, nil
	}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		idx := strings.Index(part, "=")
		if idx <= 0 {
			return nil, fmt.Errorf("experiment: malformed pair %q (expected key=value)", part)
		}
		key := strings.TrimSpace(part[:idx])
		val := strings.TrimSpace(part[idx+1:])
		if key == "" || val == "" {
			return nil, fmt.Errorf("experiment: malformed pair %q (key and value must be non-empty)", part)
		}
		out[key] = val
	}
	return out, nil
}

// parseFloatPairs parses comma-separated name=number pairs ("accuracy=0.94,loss=0.12").
func parseFloatPairs(s string) (map[string]float64, error) {
	strs, err := parseStringPairs(s)
	if err != nil {
		return nil, err
	}
	out := make(map[string]float64, len(strs))
	for k, v := range strs {
		f, ferr := strconv.ParseFloat(v, 64)
		if ferr != nil {
			return nil, fmt.Errorf("experiment: metric %q value %q is not a number", k, v)
		}
		out[k] = f
	}
	return out, nil
}

// formatHyperparams renders a hyperparameter map as "k=v k=v" (sorted).
func formatHyperparams(hp map[string]string) string {
	parts := make([]string, 0, len(hp))
	for _, k := range sortedKeys(hp) {
		parts = append(parts, fmt.Sprintf("%s=%s", k, hp[k]))
	}
	return strings.Join(parts, " ")
}

// formatMetricMap renders a metric map as "k=v k=v" (sorted).
func formatMetricMap(m map[string]float64) string {
	parts := make([]string, 0, len(m))
	for _, k := range sortedKeys(m) {
		parts = append(parts, fmt.Sprintf("%s=%.6g", k, m[k]))
	}
	return strings.Join(parts, " ")
}

// formatDeltaPct renders one Δ% cell: +Inf guarded, sign always shown.
func formatDeltaPct(v float64) string {
	if math.IsInf(v, 1) {
		return "+Inf%"
	}
	return fmt.Sprintf("%+.2f%%", v)
}

// statusString was removed — tables render the raw status ("running"/"completed"/
// "failed") exactly like renderTrainList does, keeping tabwriter alignment intact.

// sortedPairKeys returns HyperparamDiff keys sorted for deterministic tables.
func sortedPairKeys(m map[string][2]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedMetricCompareKeys returns MetricCompare keys sorted for deterministic tables.
func sortedMetricCompareKeys(m map[string][2]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
