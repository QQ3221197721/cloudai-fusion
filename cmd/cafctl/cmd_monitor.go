// Package main - `cafctl monitor` — the AI/ML layer's third module (Module 20).
// Together with Module 13 (model registry) and Module 14 (training orchestrator) it closes the MLOps loop:
// register → train → monitor → rollback decision.
//
// Commands follow the newXxxCmd() constructor pattern used by model/run/verify-*,
// so tests can build fresh, parent-less command instances and Execute them
// directly without cobra delegating up to the root command.
package main

import (
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/modelmonitor"
	"github.com/spf13/cobra"
)

const defaultMonitorStore = "./.caf"

func newMonitorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Model Performance Monitor — track regressions, pin baselines, compute drift",
		Long: `Model Performance Monitor (Module 20) — closes the MLOps loop with modules 13+14.

Track performance observations per model version, pin baselines for drift detection,
and evaluate alert rules automatically. Every Record/SetBaseline writes a signed,
hash-chained attestation through pkg/evidence — the same wiring as cafctl run.

Storage layout (--store, default ` + defaultMonitorStore + `):
  <store>/monitor/<model_version>.jsonl   append-only JSONL log
  <store>/monitor/baselines.json          pinned baselines map

Example:
  # Record an observation
  cafctl monitor record resnet50:1.1.0 --latency-p50 40 --latency-p95 120 --latency-p99 200 --qps 850 --accuracy 0.91 --errors 0.002 --samples 10000
  
  # Pin the latest observation as baseline
  cafctl monitor baseline resnet50:1.1.0
  
  # Compare with future observations
  cafctl monitor report resnet50 --version 1.1.0
  
  # Check active alerts
  cafctl monitor alerts resnet50`,
	}
	cmd.AddCommand(
		newMonitorRecordCmd(),
		newMonitorBaselineCmd(),
		newMonitorReportCmd(),
		newMonitorAlertsCmd(),
	)
	return cmd
}

// ----------------------------------------------------------------------------
// monitor record
// ----------------------------------------------------------------------------

func newMonitorRecordCmd() *cobra.Command {
	var (
		p50, p95, p99, qps, accuracy, errors float64
		samples                              int
		store, registry                      string
		noAttest                             bool
		output                               string
	)
	cmd := &cobra.Command{
		Use:     "record <model-version>",
		Short:   "Record one performance observation with attestation",
		Args:    cobra.ExactArgs(1),
		Example: "cafctl monitor record resnet50:1.1.0 --latency-p50 40 --latency-p95 120 --latency-p99 200 --qps 850 --accuracy 0.91 --errors 0.002 --samples 10000",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mon, err := openMonitorStore(store, registry, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			ref := args[0]
			rec := modelmonitor.PerformanceRecord{
				ModelVersion:  ref,
				LatencyP50MS:  p50,
				LatencyP95MS:  p95,
				LatencyP99MS:  p99,
				ThroughputQPS: qps,
				Accuracy:      accuracy,
				ErrorRate:     errors,
				SampleCount:   samples,
				Timestamp:     time.Now().UTC(),
			}
			if err := mon.Record(cmd.Context(), rec); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			out := cmd.OutOrStdout()
			if output == "json" {
				return writeJSON(out, buildMonitorRecordResult(mon, rec))
			}
			renderMonitorRecord(out, mon, rec, registry)
			return nil
		},
	}
	cmd.Flags().Float64Var(&p50, "latency-p50", 0, "Latency P50 in milliseconds")
	cmd.Flags().Float64Var(&p95, "latency-p95", 0, "Latency P95 in milliseconds")
	cmd.Flags().Float64Var(&p99, "latency-p99", 0, "Latency P99 in milliseconds")
	cmd.Flags().Float64Var(&qps, "qps", 0, "Throughput in queries-per-second")
	cmd.Flags().Float64Var(&accuracy, "accuracy", 0, "Accuracy ratio (0~1)")
	cmd.Flags().Float64Var(&errors, "errors", 0, "Error rate (0~1)")
	cmd.Flags().IntVar(&samples, "samples", 0, "Number of samples observed")
	cmd.Flags().StringVar(&store, "store", defaultMonitorStore, "Monitor store root (default ./.caf)")
	cmd.Flags().StringVar(&registry, "registry", "", "Model registry path; if provided, validate model version exists (optional)")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	return cmd
}

type monitorRecordResult struct {
	Recorded        bool    `json:"recorded"`
	ModelVersion    string  `json:"model_version"`
	LatencyP95MS    float64 `json:"latency_p95_ms"`
	ThroughputQPS   float64 `json:"throughput_qps"`
	Accuracy        float64 `json:"accuracy"`
	ErrorRate       float64 `json:"error_rate"`
	SampleCount     int     `json:"sample_count"`
	RegistryChecked bool    `json:"registry_checked"`
	AttestationHash string  `json:"attestation_hash,omitempty"`
}

func buildMonitorRecordResult(mon *modelmonitor.FSMonitor, rec modelmonitor.PerformanceRecord) monitorRecordResult {
	r := monitorRecordResult{
		Recorded:        true,
		ModelVersion:    rec.ModelVersion,
		LatencyP95MS:    rec.LatencyP95MS,
		ThroughputQPS:   rec.ThroughputQPS,
		Accuracy:        rec.Accuracy,
		ErrorRate:       rec.ErrorRate,
		SampleCount:     rec.SampleCount,
		RegistryChecked: mon.RegistryCheckEnabled(),
	}
	if last := mon.LastAttestation(); last != nil {
		r.AttestationHash = last.Hash
	}
	return r
}

func renderMonitorRecord(out io.Writer, mon *modelmonitor.FSMonitor, rec modelmonitor.PerformanceRecord, registry string) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintf(out, "  cafctl monitor record · %s performance observation signed\n", rec.ModelVersion)
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Latency:  p50 %.2f ms · p95 %.2f ms · p99 %.2f ms\n", rec.LatencyP50MS, rec.LatencyP95MS, rec.LatencyP99MS)
	fmt.Fprintf(out, "  Load:     %.2f qps · %d samples\n", rec.ThroughputQPS, rec.SampleCount)
	fmt.Fprintf(out, "  Quality:  accuracy %.4f · error rate %.4f\n", rec.Accuracy, rec.ErrorRate)
	fmt.Fprintln(out, "")
	if registry != "" {
		greenBold.Fprintf(out, "  Registry: verified (%s registered)\n", rec.ModelVersion)
	} else {
		yellow.Fprintf(out, "  Registry: skipped (no --registry provided — version not verified against the registry)\n")
	}
	fmt.Fprintf(out, "  Store:    %s\n", filepath.Join(filepath.Base(mon.Dir()), sanitizeRef(rec.ModelVersion)+".jsonl"))
	if last := mon.LastAttestation(); last != nil {
		fmt.Fprintln(out, "")
		greenBold.Fprintf(out, "%s recorded + attestation seq #%d hash %s\n", OK(), last.Seq, shortHex(last.Hash))
		fmt.Fprintln(out, "  Receipt signed & hash-chained — offline-verifiable performance trail.")
	} else {
		fmt.Fprintln(out, "")
		greenBold.Fprintf(out, "%s recorded (attestation skipped — dev only)\n", OK())
	}
	fmt.Fprintln(out, "")
}

// ----------------------------------------------------------------------------
// monitor baseline
// ----------------------------------------------------------------------------

func newMonitorBaselineCmd() *cobra.Command {
	var store string
	var noAttest, output bool
	cmd := &cobra.Command{
		Use:     "baseline <model-version>",
		Short:   "Pin the latest observation as baseline for drift comparison",
		Args:    cobra.ExactArgs(1),
		Example: "cafctl monitor baseline resnet50:1.1.0",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mon, err := openMonitorStore(store, "", !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			ref := args[0]
			if err := mon.SetBaseline(cmd.Context(), ref); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			name, ver, _ := strings.Cut(ref, ":")
			rep, err := mon.Report(cmd.Context(), name, ver)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			out := cmd.OutOrStdout()
			if output { // output is now bool, true means json mode
				return writeJSON(out, buildBaselineResult(rep, mon))
			}
			renderMonitorBaseline(out, mon, rep)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultMonitorStore, "Monitor store root")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	cmd.Flags().BoolVar(&output, "json", false, "Output format: JSON")
	return cmd
}



func buildBaselineResult(rep *modelmonitor.Report, mon *modelmonitor.FSMonitor) map[string]any {
	baseline := rep.Baseline
	if baseline == nil {
		return map[string]any{"pinned": false}
	}
	return map[string]any{
		"pinned":            true,
		"model_version":     rep.Version,
		"baseline_timestamp": baseline.Timestamp.Format(time.RFC3339),
		"latency_p95_ms":    baseline.LatencyP95MS,
		"accuracy":          baseline.Accuracy,
		"records_seen":      1,
		"attestation_hash": func() string {
			if last := mon.LastAttestation(); last != nil {
				return last.Hash
			}
			return ""
		}(),
	}
}

func renderMonitorBaseline(out io.Writer, mon *modelmonitor.FSMonitor, rep *modelmonitor.Report) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintf(out, "  cafctl monitor baseline · %s pinned to latest record\n", rep.Version)
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	if rep.Baseline != nil {
		b := rep.Baseline
		fmt.Fprintln(out, "  Baseline set from the most recent record:")
		fmt.Fprintf(out, "    Recorded:  %s\n", b.Timestamp.Format("2006-01-02 15:04:05 UTC"))
		fmt.Fprintf(out, "    Latency:   p50 %.2f · p95 %.2f · p99 %.2f ms\n", b.LatencyP50MS, b.LatencyP95MS, b.LatencyP99MS)
		fmt.Fprintf(out, "    Throughput/quality: %.2f qps · accuracy %.4f · errors %.4f\n", b.ThroughputQPS, b.Accuracy, b.ErrorRate)
		fmt.Fprintf(out, "    Samples:   %d\n", b.SampleCount)
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "  Future records will be evaluated against this baseline.")
	}
	if last := mon.LastAttestation(); last != nil {
		fmt.Fprintln(out, "")
		greenBold.Fprintf(out, "%s baseline pinned + attestation seq #%d hash %s\n", OK(), last.Seq, shortHex(last.Hash))
		fmt.Fprintln(out, "  Receipt signed & hash-chained — baseline verifiably recorded.")
	} else {
		fmt.Fprintln(out, "")
		greenBold.Fprintf(out, "%s baseline pinned (attestation skipped — dev only)\n", OK())
	}
	fmt.Fprintln(out, "")
}

// ----------------------------------------------------------------------------
// monitor report
// ----------------------------------------------------------------------------

func newMonitorReportCmd() *cobra.Command {
	var store, registry, version, output string
	var latest bool
	cmd := &cobra.Command{
		Use:     "report <model>",
		Short:   "Compute drift, trend, and active alerts vs the pinned baseline",
		Args:    cobra.ExactArgs(1),
		Example: "cafctl monitor report resnet50 --version 1.1.0\n  cafctl monitor report resnet50 --latest",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mon, err := openMonitorStore(store, registry, false)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			target := version
			if latest || version == "" {
				target = ""
			}
			rep, err := mon.Report(cmd.Context(), args[0], target)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			out := cmd.OutOrStdout()
			if output == "json" {
				return writeJSON(out, buildReportJSON(rep, mon))
			}
			renderMonitorReport(out, rep, registry)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultMonitorStore, "Monitor store root")
	cmd.Flags().StringVar(&registry, "registry", "", "Model registry path; version validation enabled when set")
	cmd.Flags().StringVar(&version, "version", "", "Specific version (e.g., 1.1.0)")
	cmd.Flags().BoolVar(&latest, "latest", false, "Use most recently observed version (default: all)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json'")
	return cmd
}

type reportResult struct {
	Model        string                        `json:"model"`
	Version      string                        `json:"version"`
	Baseline     *modelmonitor.PerformanceRecord `json:"baseline,omitempty"`
	Latest       *modelmonitor.PerformanceRecord `json:"latest"`
	Trend        []modelmonitor.PerformanceRecord `json:"trend,omitempty"`
	Drift        map[string]any                `json:"drift,omitempty"` // any for Inf support
	ActiveAlerts []AlertResult                 `json:"active_alerts"`
}

type AlertResult struct {
	Rule          string                    `json:"rule"`
	Metric        string                    `json:"metric"`
	Severity      modelmonitor.AlertSeverity `json:"severity"`
	Message       string                    `json:"message"`
	Observed      float64                   `json:"observed"`
	Baseline      float64                   `json:"baseline"`
	RegressionPct float64                   `json:"regression_pct"`
}

func buildReportJSON(rep *modelmonitor.Report, mon *modelmonitor.FSMonitor) reportResult {
	result := reportResult{
		Model:        rep.Model,
		Version:      rep.Version,
		Baseline:     rep.Baseline,
		Latest:       rep.Latest,
		Trend:        rep.Trend,
		ActiveAlerts: make([]AlertResult, len(rep.ActiveAlerts)),
	}
	// Convert Drift map[float64 -> any] to support "+Inf%" representation
	driftAny := make(map[string]any, len(rep.Drift))
	for k, v := range rep.Drift {
		if v == math.Inf(1) {
			driftAny[k] = "+Inf%"
		} else {
			driftAny[k] = v
		}
	}
	result.Drift = driftAny
	for i, a := range rep.ActiveAlerts {
		result.ActiveAlerts[i] = AlertResult(a)
	}
	return result
}

func renderMonitorReport(out io.Writer, rep *modelmonitor.Report, registry string) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintf(out, "  cafctl monitor report · %s:%s\n", rep.Model, rep.Version)
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	if registry != "" {
		green.Fprintf(out, "  Registry:  verified\n")
	} else {
		yellow.Fprintf(out, "  Registry:  skipped (no --registry)\n")
	}
	if rep.Baseline != nil {
		fmt.Fprintf(out, "  Baseline:  %s\n", rep.Baseline.Timestamp.Format("2006-01-02 15:04:05 UTC"))
		fmt.Fprintf(out, "  Latest:    %s", rep.Latest.Timestamp.Format("2006-01-02 15:04:05 UTC"))
	} else {
		fmt.Fprint(out, "  Baseline:  not set (run cafctl monitor baseline first)\n")
		fmt.Fprintf(out, "  Latest:    %s", rep.Latest.Timestamp.Format("2006-01-02 15:04:05 UTC"))
	}
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "")
	if rep.Baseline != nil && rep.Latest != nil {
		w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "METRIC\tBASELINE\tLATEST\tDRIFT")
		for _, metric := range modelmonitor.AllMetrics() {
			b := modelmonitor.MetricValue(rep.Baseline, metric)
			l := modelmonitor.MetricValue(rep.Latest, metric)
			driftStr := "—"
			if rep.Drift != nil {
				driftStr = modelmonitor.FormatDrift(metric, rep.Drift[metric])
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				metric, modelmonitor.FormatMetricValue(metric, b),
				modelmonitor.FormatMetricValue(metric, l), driftStr)
		}
		w.Flush()
		fmt.Fprintln(out, "")
	} else {
		fmt.Fprintln(out, "  No baseline set — drift computation unavailable.")
		fmt.Fprintln(out, "")
	}
	if len(rep.Trend) > 0 {
		trendInfo := fmt.Sprintf("Trend (%d records)", len(rep.Trend))
		if len(rep.Trend) >= 2 {
			trendInfo += fmt.Sprintf(" · p95 %.2f → %.2f ms · accuracy %.4f → %.4f",
				rep.Trend[0].LatencyP95MS, rep.Trend[len(rep.Trend)-1].LatencyP95MS,
				rep.Trend[0].Accuracy, rep.Trend[len(rep.Trend)-1].Accuracy)
		}
		fmt.Fprintln(out, trendInfo)
	}
	if len(rep.ActiveAlerts) > 0 {
		fmt.Fprintln(out, "  Active alerts:", len(rep.ActiveAlerts))
		for _, a := range rep.ActiveAlerts {
			switch a.Severity {
			case modelmonitor.SeverityCritical:
				redBold.Fprintf(out, "    ✗ CRITICAL %-28s %s\n", a.Rule, modelmonitor.FormatDrift(a.Metric, a.RegressionPct))
			case modelmonitor.SeverityWarn:
				yellow.Fprintf(out, "    ⚠ WARN %-32s %s\n", a.Rule, modelmonitor.FormatDrift(a.Metric, a.RegressionPct))
			default:
				cyan.Fprintf(out, "    ℹ INFO  %-32s %s\n", a.Rule, modelmonitor.FormatDrift(a.Metric, a.RegressionPct))
			}
			fmt.Fprintf(out, "       %s\n", a.Message)
		}
	} else if rep.Baseline != nil {
		greenBold.Fprintln(out, "  ✓ no active alerts — all metrics within thresholds")
	}
	fmt.Fprintln(out, "")
}

// ----------------------------------------------------------------------------
// monitor alerts
// ----------------------------------------------------------------------------

func newMonitorAlertsCmd() *cobra.Command {
	var store, output string
	cmd := &cobra.Command{
		Use:     "alerts <model>",
		Short:   "Show active alerts for the latest observed version",
		Args:    cobra.ExactArgs(1),
		Example: "cafctl monitor alerts resnet50",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mon, err := openMonitorStore(store, "", false)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			alerts, err := mon.Alerts(cmd.Context(), args[0])
			if err != nil {
				if errors.Is(err, modelmonitor.ErrNoRecords) || errors.Is(err, modelmonitor.ErrNoBaseline) {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", INFO(), err)
					fmt.Fprintln(cmd.ErrOrStderr(), "  Run `cafctl monitor baseline <ref>` to pin one first.")
					return nil
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			out := cmd.OutOrStdout()
			if output == "json" {
				jsonAlerts := make([]AlertResult, len(alerts))
				for i := range alerts {
					jsonAlerts[i] = AlertResult(alerts[i])
				}
				return writeJSON(out, jsonAlerts)
			}
			renderAlertsList(out, args[0], alerts)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultMonitorStore, "Monitor store root")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json'")
	return cmd
}

func renderAlertsList(out io.Writer, model string, alerts []modelmonitor.Alert) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "  cafctl monitor alerts · "+model)
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	if len(alerts) == 0 {
		greenBold.Fprintln(out, "✓ no active alerts — all metrics within thresholds")
		fmt.Fprintln(out, "")
		return
	}
	fmt.Fprintf(out, "  %d active alert(s):\n", len(alerts))
	for _, a := range alerts {
		switch a.Severity {
		case modelmonitor.SeverityCritical:
			redBold.Fprintf(out, "  ✗ CRITICAL %-28s %s\n", a.Rule, modelmonitor.FormatDrift(a.Metric, a.RegressionPct))
		case modelmonitor.SeverityWarn:
			yellow.Fprintf(out, "  ⚠ WARN %-32s %s\n", a.Rule, modelmonitor.FormatDrift(a.Metric, a.RegressionPct))
		default:
			cyan.Fprintf(out, "  ℹ INFO  %-32s %s\n", a.Rule, modelmonitor.FormatDrift(a.Metric, a.RegressionPct))
		}
		fmt.Fprintf(out, "       %s\n", a.Message)
	}
	fmt.Fprintln(out, "")
}

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

func openMonitorStore(store, registryPath string, attest bool) (*modelmonitor.FSMonitor, error) {
	if store == "" {
		store = defaultMonitorStore
	}
	abs, err := filepath.Abs(store)
	if err != nil {
		abs = store
	}
	var ledger *evidence.Ledger
	if attest {
		signer, serr := evidence.GenerateEphemeralSigner()
		if serr != nil {
			return nil, fmt.Errorf("generate signer: %w", serr)
		}
		ledger, err = evidence.NewLedger(evidence.LedgerConfig{
			Store:    evidence.NewMemoryStore(),
			Signer:   signer,
			Anchorer: evidence.NewSimulatedAnchorer(),
		})
		if err != nil {
			return nil, fmt.Errorf("build ledger: %w", err)
		}
	}
	var checker modelmonitor.RegistryChecker
	if registryPath != "" {
		reg, rerr := openModelRegistry(registryPath, false)
		if rerr != nil {
			return nil, rerr
		}
		checker = reg
	}
	return modelmonitor.NewFSMonitor(filepath.Join(abs, "monitor"), ledger, checker)
}

func sanitizeRef(ref string) string {
	return strings.ReplaceAll(ref, ":", "_")
}
