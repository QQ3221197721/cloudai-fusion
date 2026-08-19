// Package main - `cafctl cost` — Module 17: Cost-aware Scheduling.
//
// Completes the scheduling decision loop
// (RL optimization + Monitor drift detection → Cost optimization):
//
//	estimate  price a job on candidate nodes with exact integer-cent math
//	config    update a node's cost model (persisted + attested)
//	report    historical decision cost, joined with Monitor performance
//	          observations into a cost ↔ quality trade-off view
//	optimize  pick the best node by the 0.7:0.3 RL:cost mix score
//
// Commands follow the newXxxCmd() constructor pattern used by
// model/train/monitor, and attestations run through the same real
// MemoryStore + EphemeralSigner + Ledger.Record wiring as `cafctl run`.
//
// Storage layout (--store, default ./.caf):
//
//	<store>/scheduler/cost_models.json    one CostModel per line (JSONL)
//	<store>/scheduler/cost_history.jsonl  append-only decision history
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/modelmonitor"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
	"github.com/spf13/cobra"
)

// defaultCostStore is the cost store root; models/history live under
// <store>/scheduler, matching the .caf layout of monitor/training.
const defaultCostStore = "./.caf"

func newCostCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cost",
		Short: "Cost-aware Scheduling — exact per-node pricing, budget checks, RL:cost 0.7:0.3 node choice",
		Long: `Cost-aware Scheduling (Module 17) — closes the scheduling decision loop:
RL optimization + Monitor drift detection → Cost optimization.

Price jobs with integer-cent exactness (no float drift), enforce budgets
before anything runs, and pick nodes with the 0.7:0.3 RL:cost mix score.
Every estimate/config/optimize decision writes a signed, hash-chained
attestation through pkg/evidence — the same wiring as cafctl run.

Default pricing rules (per GPU-hour, on-demand):
  a100 $8.50 · h100 $12.00 · v100 $4.50 · a10g $2.85 · l40s $5.20
  spot = 40% of on-demand · cpu $0.10/core-hr · memory $0.01/GB-hr

Storage layout (--store, default ` + defaultCostStore + `):
  <store>/scheduler/cost_models.json    one node model per line
  <store>/scheduler/cost_history.jsonl  decision history (feeds report)

Example:
  cafctl cost estimate --job training-job.yaml --nodes gpu-a,gpu-b --budget 50
  cafctl cost config gpu-a --gpu-cost 9.5 --spot-discount 0.4 --cpu-cost 0.1
  cafectl cost report
  cafctl cost optimize --job training-job.yaml`,
	}
	cmd.AddCommand(
		newCostEstimateCmd(),
		newCostConfigCmd(),
		newCostReportCmd(),
		newCostOptimizeCmd(),
	)
	return cmd
}

// ----------------------------------------------------------------------------
// Shared helpers
// ----------------------------------------------------------------------------

// openCostStore opens (creating if needed) the JSONL cost-model store at
// <store>/scheduler.
func openCostStore(store string) *scheduler.CostModelStore {
	if store == "" {
		store = defaultCostStore
	}
	abs, err := filepath.Abs(store)
	if err != nil {
		abs = store
	}
	return scheduler.NewCostModelStore(filepath.Join(abs, "scheduler"))
}

// costJobYAML is the minimal job spec read from a `key: value` workload file.
// Parsed by hand (same approach as cmd_run.go) to avoid a YAML dependency.
type costJobYAML struct {
	Name          string
	GPUCount      int
	GPUType       string
	CPUCores      int
	MemoryGB      int
	DurationHours float64
	Budget        float64
}

// parseDurationHours accepts "2h", "90m", "1.5", "45m30s" style durations.
func parseDurationHours(v string) (float64, error) {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" {
		return 0, fmt.Errorf("empty duration")
	}
	// Pure number = hours.
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		return f, nil
	}
	// Compound Go-style duration.
	if d, err := time.ParseDuration(v); err == nil {
		return d.Hours(), nil
	}
	// Bare suffix forms: 2h / 30m / 45s.
	unit := v[len(v)-1]
	num := strings.TrimRight(v, "hms")
	f, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, fmt.Errorf("unrecognized duration %q", v)
	}
	switch unit {
	case 'h':
		return f, nil
	case 'm':
		return f / 60, nil
	case 's':
		return f / 3600, nil
	}
	return 0, fmt.Errorf("unrecognized duration %q", v)
}

// parseCostJobSpec reads the flat `key: value` job file used by estimate and
// optimize.
func parseCostJobSpec(content string) (costJobYAML, error) {
	spec := costJobYAML{GPUCount: 1}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:idx]))
		val := strings.Trim(strings.TrimSpace(line[idx+1:]), `"'`)
		var err error
		switch key {
		case "name":
			spec.Name = val
		case "gpus", "gpu", "gpu_count":
			spec.GPUCount, err = strconv.Atoi(val)
		case "gpu_type", "gpu-type":
			spec.GPUType = val
		case "cpu_cores", "cpu-cores", "cpus":
			spec.CPUCores, err = strconv.Atoi(val)
		case "memory_gb", "memory-gb", "memory", "mem":
			if strings.HasSuffix(strings.ToLower(val), "gb") {
				val = strings.TrimSuffix(strings.ToLower(val), "gb")
			}
			n, perr := strconv.ParseFloat(val, 64)
			spec.MemoryGB, err = int(n), perr
		case "duration_hours", "duration-hours", "duration", "estimated_duration":
			spec.DurationHours, err = parseDurationHours(val)
		case "budget", "budget_usd":
			spec.Budget, err = strconv.ParseFloat(val, 64)
		}
		if err != nil {
			return spec, fmt.Errorf("job spec field %q: %w", key, err)
		}
	}
	return spec, nil
}

// loadCostJob reads a job spec file and applies CLI overrides.
func loadCostJob(path string, budgetOverride float64) (costJobYAML, scheduler.JobSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return costJobYAML{}, scheduler.JobSpec{}, fmt.Errorf("read job spec %q: %w", path, err)
	}
	y, err := parseCostJobSpec(string(data))
	if err != nil {
		return y, scheduler.JobSpec{}, err
	}
	if y.Name == "" {
		y.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if y.DurationHours <= 0 {
		y.DurationHours = 1.0
	}
	if budgetOverride > 0 {
		y.Budget = budgetOverride
	}
	job := scheduler.JobSpec{
		Name: y.Name, GPUCount: y.GPUCount, GPUType: y.GPUType,
		CPUCores: y.CPUCores, MemoryGB: y.MemoryGB,
		DurationHours: y.DurationHours, Budget: y.Budget,
	}
	return y, job, nil
}

// recordCostAttestation signs one cost decision through the real evidence
// ledger (in-memory store + ephemeral signer — identical pattern to
// recordRunAttestation in cmd_run.go). Returns the receipt hash.
func recordCostAttestation(ctx context.Context, action, subject string, input, output map[string]any) (string, error) {
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		return "", fmt.Errorf("generate signer: %w", err)
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{
		Store:    evidence.NewMemoryStore(),
		Signer:   signer,
		Anchorer: evidence.NewSimulatedAnchorer(),
	})
	if err != nil {
		return "", fmt.Errorf("build ledger: %w", err)
	}
	ev, err := ledger.Record(ctx, evidence.RecordInput{
		Actor:   "cafctl",
		Action:  action,
		Subject: subject,
		Input:   input,
		Output:  output,
		Payload: map[string]any{"namespace": "scheduler", "module": "17-cost-aware-scheduling", "recorded_at": time.Now().UTC()},
	})
	if err != nil {
		return "", fmt.Errorf("record attestation: %w", err)
	}
	if ev == nil {
		return "", nil
	}
	return ev.Hash, nil
}

// ----------------------------------------------------------------------------
// cost estimate
// ----------------------------------------------------------------------------

func newCostEstimateCmd() *cobra.Command {
	var jobPath, nodesFlag, store, output string
	var budget float64
	var noAttest bool
	cmd := &cobra.Command{
		Use:     "estimate --job <spec.yaml> --nodes <a,b>",
		Short:   "Price a job on each candidate node (exact cents) and check the budget",
		Args:    cobra.NoArgs,
		Example: "  cafctl cost estimate --job training-job.yaml --nodes gpu-a,gpu-b --budget 50",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			y, job, err := loadCostJob(jobPath, budget)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			st := openCostStore(store)
			models, err := st.LoadCostModels()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			opt := scheduler.NewDefaultCostOptimizer(models)

			nodeIDs := splitCSV(nodesFlag)
			if len(nodeIDs) == 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s--nodes is required (comma-separated node ids)\n", ERROR())
				return fmt.Errorf("--nodes is required")
			}

			rows := make([]estimateRow, 0, len(nodeIDs))
			for _, id := range nodeIDs {
				est, err := opt.Estimate(job, id)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
					return err
				}
				rows = append(rows, estimateRow{est: est, deflted: models[id] == nil})
				hash := ""
				if !noAttest {
					hash, _ = recordCostAttestation(cmd.Context(), "cost.estimate", job.Name,
						map[string]any{"job": job.Name, "node": id, "gpus": job.GPUCount, "duration_hours": job.DurationHours},
						map[string]any{"total_cost": est.TotalCost, "budget_exceeded": est.BudgetExceeded})
				}
				if err := st.AppendCostHistory(scheduler.CostHistoryEntry{
					JobName: job.Name, NodeID: id, DurationHours: job.DurationHours,
					TotalCost: est.TotalCost, Budget: job.Budget,
					BudgetExceeded: est.BudgetExceeded, Attestation: hash,
				}); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", WARN(), err)
				}
			}

			if output == "json" {
				results := make([]estimateJSON, 0, len(rows))
				for _, r := range rows {
					results = append(results, estimateJSON{
						NodeID: r.est.NodeID, DefaultRules: r.deflted, Estimate: r.est,
					})
				}
				return writeJSON(out, map[string]any{
					"job":     job,
					"nodes":   nodeIDs,
					"results": results,
				})
			}
			renderCostEstimate(out, y, job, rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&jobPath, "job", "", "Job spec file (key: value YAML)")
	cmd.Flags().StringVar(&nodesFlag, "nodes", "", "Comma-separated candidate node ids")
	cmd.Flags().Float64Var(&budget, "budget", 0, "Budget override in USD (0 = use spec value)")
	cmd.Flags().StringVar(&store, "store", defaultCostStore, "Cost store root (models under <store>/scheduler)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	_ = cmd.MarkFlagRequired("job")
	_ = cmd.MarkFlagRequired("nodes")
	return cmd
}

// renderCostEstimate prints the per-node estimate table plus breakdowns.
func renderCostEstimate(out io.Writer, y costJobYAML, job scheduler.JobSpec, rows []estimateRow) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintf(out, "  cafctl cost estimate · %s (%d× %s, %.1fh)\n",
		job.Name, job.GPUCount, orDash(job.GPUType), job.DurationHours)
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NODE\tPLAN\tDURATION\tTOTAL\tBUDGET")
	for _, r := range rows {
		budgetCell := "—"
		if job.Budget > 0 {
			if r.est.BudgetExceeded {
				budgetCell = fmt.Sprintf("✗ over $%.2f", r.est.TotalCost-job.Budget)
			} else {
				budgetCell = fmt.Sprintf("✓ $%.2f left", job.Budget-r.est.TotalCost)
			}
		}
		plan := r.est.Breakdown[0].Detail
		fmt.Fprintf(w, "%s\t%s\t%.1fh\t$%.2f\t%s\n",
			r.est.NodeID, plan, r.est.DurationHours, r.est.TotalCost, budgetCell)
	}
	w.Flush()
	fmt.Fprintln(out, "")

	cheapest := rows[0]
	for _, r := range rows[1:] {
		if r.est.TotalCost < cheapest.est.TotalCost {
			cheapest = r
		}
	}
	greenBold.Fprintf(out, "%s cheapest plan: %s at $%.2f\n", OK(), cheapest.est.NodeID, cheapest.est.TotalCost)
	for _, r := range rows {
		parts := make([]string, 0, len(r.est.Breakdown))
		for _, b := range r.est.Breakdown {
			parts = append(parts, fmt.Sprintf("%s $%.2f (%s)", b.Component, b.Amount, b.Detail))
		}
		fmt.Fprintf(out, "  %s: %s\n", r.est.NodeID, strings.Join(parts, " · "))
		if r.deflted {
			yellow.Fprintf(out, "    ⚠ node not configured — priced with default rules (run `cafctl cost config %s`)\n", r.est.NodeID)
		}
		if r.est.Message != "" {
			if r.est.BudgetExceeded {
				redBold.Fprintf(out, "    ✗ %s\n", r.est.Message)
			} else {
				fmt.Fprintf(out, "    %s\n", r.est.Message)
			}
		}
	}
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  History: %s\n", "cost_history.jsonl (view with `cafctl cost report`)")
	fmt.Fprintln(out, "")
}

// estimateRow pairs one node's estimate with whether it fell back to the
// default pricing rules (node not present in cost_models.json).
type estimateRow struct {
	est     *scheduler.CostEstimate
	deflted bool
}

// estimateJSON is the machine-readable projection of estimateRow (the raw
// struct has unexported fields and would marshal to {}).
type estimateJSON struct {
	NodeID       string                  `json:"node_id"`
	DefaultRules bool                    `json:"default_rules"`
	Estimate     *scheduler.CostEstimate `json:"estimate"`
}

// splitCSV splits a comma-separated flag value, trimming spaces.
func splitCSV(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ----------------------------------------------------------------------------
// cost config
// ----------------------------------------------------------------------------

func newCostConfigCmd() *cobra.Command {
	var gpuCost, spotDiscount, cpuCost, memPrice float64
	var gpuType, store, output string
	var gpus int
	var spot bool
	var noAttest bool
	cmd := &cobra.Command{
		Use:     "config <node-id>",
		Short:   "Create/update a node's cost model (persisted JSONL + signed attestation)",
		Args:    cobra.ExactArgs(1),
		Example: "  cafctl cost config gpu-a --gpu-cost 9.5 --spot-discount 0.4 --cpu-cost 0.1",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			nodeID := args[0]
			st := openCostStore(store)

			upd := scheduler.CostModelUpdate{}
			if gpuCost > 0 {
				upd.GPUCost = &gpuCost
			}
			if spotDiscount > 0 {
				upd.SpotDiscount = &spotDiscount
			}
			if cpuCost > 0 {
				upd.CPUCost = &cpuCost
			}
			if memPrice > 0 {
				upd.MemoryPrice = &memPrice
			}
			if gpuType != "" {
				upd.GPUType = &gpuType
			}
			if gpus > 0 {
				upd.GPUCount = &gpus
			}
			if spot {
				upd.UseSpot = &spot
			}

			model, _, err := st.UpdateCostModel(nodeID, upd)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			hash := ""
			if !noAttest {
				hash, err = recordCostAttestation(cmd.Context(), "cost.config", nodeID,
					map[string]any{"node": nodeID},
					map[string]any{"gpu_cost": gpuCost, "spot_discount": spotDiscount,
						"cpu_cost": cpuCost, "memory_price": memPrice, "use_spot": spot})
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
					return err
				}
			}

			if output == "json" {
				return writeJSON(out, map[string]any{
					"node_id":          model.NodeID,
					"gpu_count":        model.GPUCount,
					"gpu_types":        model.GPUTypes,
					"gpu_cost_per_hour": model.GPUCostPerHour,
					"spot_discount":    model.SpotDiscount,
					"cpu_cost_per_hour": model.CPUCostPerHour,
					"memory_gb_price":  model.MemoryGBPrice,
					"use_spot":         model.UseSpot,
					"persisted":        st.ModelsPath(),
					"attestation_hash": hash,
				})
			}
			renderCostConfig(out, model, st, hash)
			return nil
		},
	}
	cmd.Flags().Float64Var(&gpuCost, "gpu-cost", 0, "GPU price per hour (USD, applied to the node's GPU type)")
	cmd.Flags().Float64Var(&spotDiscount, "spot-discount", 0, "Spot price as fraction of on-demand (0.4 = pay 40%)")
	cmd.Flags().Float64Var(&cpuCost, "cpu-cost", 0, "CPU cost per core-hour (USD)")
	cmd.Flags().Float64Var(&memPrice, "memory-price", 0, "Memory price per GB-hour (USD)")
	cmd.Flags().StringVar(&gpuType, "gpu-type", "", "GPU type (a100/h100/v100/a10g/l40s)")
	cmd.Flags().IntVar(&gpus, "gpus", 0, "GPU capacity of the node")
	cmd.Flags().BoolVar(&spot, "spot", false, "Enable spot pricing for estimates on this node")
	cmd.Flags().StringVar(&store, "store", defaultCostStore, "Cost store root")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json'")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	return cmd
}

func renderCostConfig(out io.Writer, m *scheduler.CostModel, st *scheduler.CostModelStore, hash string) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintf(out, "  cafctl cost config · %s cost model updated\n", m.NodeID)
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  GPUs:         %d × %s\n", m.GPUCount, strings.Join(m.GPUTypes, ","))
	for _, t := range m.GPUTypes {
		fmt.Fprintf(out, "  GPU price:    $%.2f/hr (%s, on-demand)\n", m.GPUCostPerHour[t], t)
	}
	fmt.Fprintf(out, "  Spot:         %.0f%% of on-demand (%s)\n", m.SpotDiscount*100, spotWord(m.UseSpot))
	fmt.Fprintf(out, "  CPU:          $%.2f/core-hr\n", m.CPUCostPerHour)
	fmt.Fprintf(out, "  Memory:       $%.4f/GB-hr\n", m.MemoryGBPrice)
	fmt.Fprintf(out, "  Updated:      %s\n", m.UpdatedAt.Format("2006-01-02 15:04:05 UTC"))
	fmt.Fprintln(out, "")
	greenBold.Fprintf(out, "%s model persisted to %s\n", OK(), st.ModelsPath())
	if hash != "" {
		fmt.Fprintf(out, "  Attestation:  %s\n", shortHex(hash))
		fmt.Fprintln(out, "  Receipt signed & hash-chained — pricing changes are offline-verifiable.")
	} else {
		fmt.Fprintln(out, "  Attestation:  skipped (--no-attest; dev only)")
	}
	fmt.Fprintln(out, "")
}

func spotWord(on bool) string {
	if on {
		return "enabled for estimates"
	}
	return "on file, off by default"
}

// ----------------------------------------------------------------------------
// cost report — decision history joined with Monitor observations
// ----------------------------------------------------------------------------

// monitorSnapshot is the latest Monitor record for one model version.
type monitorSnapshot struct {
	ModelVersion string  `json:"model_version"`
	LatencyP95MS float64 `json:"latency_p95_ms"`
	Accuracy     float64 `json:"accuracy"`
	DriftPct     float64 `json:"drift_pct"` // p95 drift vs pinned baseline (0 if none)
	HasBaseline  bool    `json:"has_baseline"`
	Timestamp    string  `json:"timestamp"`
}

func newCostReportCmd() *cobra.Command {
	var store, registry, output string
	cmd := &cobra.Command{
		Use:     "report",
		Short:   "Historical scheduling cost vs Monitor performance (cost ↔ quality trade-off)",
		Args:    cobra.NoArgs,
		Example: "  cafctl cost report\n  cafctl cost report --registry .caf/models",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			st := openCostStore(store)
			hist, err := st.LoadCostHistory()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			if len(hist) == 0 {
				fmt.Fprintln(out, "")
				fmt.Fprintln(out, "No cost decisions recorded yet.")
				fmt.Fprintln(out, "Start with:")
				fmt.Fprintln(out, "  cafctl cost estimate --job training-job.yaml --nodes gpu-a,gpu-b --budget 50")
				return nil
			}

			snaps := loadMonitorSnapshots(st)

			var verified []string
			if registry != "" {
				verified = verifyCostRegistry(cmd.Context(), registry, hist)
			}

			if output == "json" {
				return writeJSON(out, map[string]any{
					"history":            hist,
					"monitor_snapshots":  snaps,
					"registry_verified":  verified,
				})
			}
			renderCostReport(out, st, hist, snaps, verified, registry != "")
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultCostStore, "Cost store root")
	cmd.Flags().StringVar(&registry, "registry", "", "Model registry path; verifies job model refs when set")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json'")
	return cmd
}

// verifyCostRegistry checks each distinct job name against the model registry
// and returns the refs that resolve.
func verifyCostRegistry(ctx context.Context, registry string, hist []scheduler.CostHistoryEntry) []string {
	reg, err := openModelRegistry(registry, false)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var verified []string
	for _, e := range hist {
		name, ver, ok := strings.Cut(e.JobName, ":")
		if !ok || seen[e.JobName] {
			continue
		}
		seen[e.JobName] = true
		if art, err := reg.Get(ctx, name, ver); err == nil && art != nil {
			verified = append(verified, e.JobName)
		}
	}
	sort.Strings(verified)
	return verified
}

// loadMonitorSnapshots reads the sibling Monitor JSONL store
// (<store-root>/monitor/*.jsonl) and returns the latest record per version,
// plus baseline drift when available.
func loadMonitorSnapshots(st *scheduler.CostModelStore) []monitorSnapshot {
	monDir := filepath.Join(filepath.Dir(st.Dir()), "monitor")
	entries, err := os.ReadDir(monDir)
	if err != nil {
		return nil
	}
	var snaps []monitorSnapshot
	for _, e := range entries {
		if e.IsDir() || (!strings.HasSuffix(e.Name(), ".jsonl")) {
			continue
		}
		path := filepath.Join(monDir, e.Name())
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		var last, baseline *modelmonitor.PerformanceRecord
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var rec modelmonitor.PerformanceRecord
			if json.Unmarshal([]byte(line), &rec) == nil && rec.ModelVersion != "" {
				if last == nil {
					baseline = &rec // first record of the file ≈ pinned baseline era
				}
				cp := rec
				last = &cp
			}
		}
		_ = f.Close()
		if last == nil {
			continue
		}
		snap := monitorSnapshot{
			ModelVersion: last.ModelVersion,
			LatencyP95MS: last.LatencyP95MS,
			Accuracy:     last.Accuracy,
			Timestamp:    last.Timestamp.Format(time.RFC3339),
		}
		if baseline != nil && baseline.LatencyP95MS > 0 && last.ModelVersion == baseline.ModelVersion {
			snap.HasBaseline = true
			snap.DriftPct = (last.LatencyP95MS - baseline.LatencyP95MS) / baseline.LatencyP95MS * 100
		}
		snaps = append(snaps, snap)
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].ModelVersion < snaps[j].ModelVersion })
	return snaps
}

// renderCostReport prints the decision history and the cost ↔ quality view.
func renderCostReport(out io.Writer, st *scheduler.CostModelStore, hist []scheduler.CostHistoryEntry, snaps []monitorSnapshot, verified []string, registryOn bool) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "  cafctl cost report · scheduling cost history & trade-off")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")

	total := 0.0
	overBudget := 0
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "WHEN\tJOB\tNODE\tDURATION\tCOST\tBUDGET")
	for _, e := range hist {
		total += e.TotalCost
		budgetCell := "—"
		if e.Budget > 0 {
			if e.BudgetExceeded {
				budgetCell = "✗ exceeded"
				overBudget++
			} else {
				budgetCell = "✓ ok"
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%.1fh\t$%.2f\t%s\n",
			e.Timestamp.Format("01-02 15:04"), e.JobName, e.NodeID, e.DurationHours, e.TotalCost, budgetCell)
	}
	w.Flush()
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Total estimated spend: $%.2f across %d decisions", total, len(hist))
	if overBudget > 0 {
		redBold.Fprintf(out, " · %d over budget", overBudget)
	}
	fmt.Fprintln(out, "")

	// Cost ↔ quality trade-off: join history jobs with monitor snapshots.
	snapByVer := make(map[string]monitorSnapshot, len(snaps))
	for _, s := range snaps {
		snapByVer[s.ModelVersion] = s
	}
	verSet := map[string]bool{}
	var matched []string
	for _, e := range hist {
		if _, ok := snapByVer[e.JobName]; ok && !verSet[e.JobName] {
			verSet[e.JobName] = true
			matched = append(matched, e.JobName)
		}
	}
	sort.Strings(matched)

	fmt.Fprintln(out, "")
	if len(matched) == 0 {
		yellow.Fprintf(out, "  ⚠ no Monitor observations matched recorded jobs\n")
		fmt.Fprintln(out, "    Link them by naming the job after a monitored model version:")
		fmt.Fprintln(out, "    cafctl monitor record resnet50:1.1.0 --latency-p95 120 ...")
		fmt.Fprintln(out, "    cafctl cost estimate --job resnet-job.yaml ...   (name: resnet50:1.1.0)")
	} else {
		cyanBold.Fprintf(out, "  Cost ↔ quality trade-off (Monitor drift × scheduling cost)\n")
		fmt.Fprintln(out, "")
		w2 := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w2, "MODEL VERSION\tSPEND\tAVG $/JOB\tP95 LATENCY\tACCURACY\tP95 DRIFT")
		for _, ver := range matched {
			s := snapByVer[ver]
			spend, count := 0.0, 0
			for _, e := range hist {
				if e.JobName == ver {
					spend += e.TotalCost
					count++
				}
			}
			drift := "—"
			if s.HasBaseline {
				if s.DriftPct >= 25 {
					drift = fmt.Sprintf("✗ +%.1f%%", s.DriftPct)
				} else if s.DriftPct >= 0 {
					drift = fmt.Sprintf("+%.1f%%", s.DriftPct)
				} else {
					drift = fmt.Sprintf("%.1f%%", s.DriftPct)
				}
			}
			fmt.Fprintf(w2, "%s\t$%.2f\t$%.2f\t%.1f ms\t%.4f\t%s\n",
				ver, spend, spend/float64(count), s.LatencyP95MS, s.Accuracy, drift)
		}
		w2.Flush()
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "  High drift + high spend = re-run `cafctl cost optimize` before paying again.")
	}

	if registryOn {
		if len(verified) > 0 {
			green.Fprintf(out, "\n  Registry: verified %s\n", strings.Join(verified, ", "))
		} else {
			yellow.Fprintf(out, "\n  Registry: no job matched a registered model version\n")
		}
	}
	fmt.Fprintln(out, "")
}

// ----------------------------------------------------------------------------
// cost optimize
// ----------------------------------------------------------------------------

func newCostOptimizeCmd() *cobra.Command {
	var jobPath, nodesFlag, store, output string
	var rlWeight, costWeight, rlScore float64
	var noAttest bool
	cmd := &cobra.Command{
		Use:     "optimize --job <spec.yaml>",
		Short:   "Recommend the best node by the RL:cost mix score (default 0.7:0.3)",
		Args:    cobra.NoArgs,
		Example: "  cafctl cost optimize --job training-job.yaml",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			_, job, err := loadCostJob(jobPath, 0)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			st := openCostStore(store)
			models, err := st.LoadCostModels()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			if rlWeight <= 0 {
				rlWeight = 0.7
			}
			if costWeight <= 0 {
				costWeight = 0.3
			}

			// Candidate nodes: --nodes filter, else all configured models.
			ids := splitCSV(nodesFlag)
			if len(ids) == 0 {
				for id := range models {
					ids = append(ids, id)
				}
				sort.Strings(ids)
			}
			if len(ids) == 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "%sno candidate nodes: configure some first (cafctl cost config <node>) or pass --nodes\n", ERROR())
				return fmt.Errorf("no candidate nodes")
			}

			nodes := make([]scheduler.NodeScore, 0, len(ids))
			for _, id := range ids {
				m := models[id]
				ns := scheduler.NodeScore{NodeName: id, Score: 80, GPUFreeCount: 8, CostModel: m}
				if m != nil {
					ns.GPUFreeCount = m.GPUCount
					if len(m.GPUTypes) > 0 {
						ns.GPUType = m.GPUTypes[0]
					}
				}
				if rlScore > 0 {
					ns.Score = rlScore * 100
				}
				nodes = append(nodes, ns)
			}

			choice := scheduler.NewDefaultCostOptimizer(models).Compare(nodes, job)
			if choice.Estimate == nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%s\n", ERROR(), choice.Reason)
				return fmt.Errorf("%s", choice.Reason)
			}

			hash := ""
			if !noAttest {
				hash, _ = recordCostAttestation(cmd.Context(), "cost.optimize", job.Name,
					map[string]any{"job": job.Name, "candidates": len(nodes), "rl_weight": rlWeight, "cost_weight": costWeight},
					map[string]any{"node": choice.NodeID, "mix_score": choice.MixScore, "total_cost": choice.Estimate.TotalCost})
			}
			if err := st.AppendCostHistory(scheduler.CostHistoryEntry{
				JobName: job.Name, NodeID: choice.NodeID, DurationHours: job.DurationHours,
				TotalCost: choice.Estimate.TotalCost, Budget: job.Budget,
				BudgetExceeded: choice.Estimate.BudgetExceeded, Chosen: true, Attestation: hash,
			}); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", WARN(), err)
			}

			if output == "json" {
				return writeJSON(out, map[string]any{
					"job":    job,
					"choice": choice,
				})
			}
			renderCostOptimize(out, job, choice, rlWeight, costWeight, hash)
			return nil
		},
	}
	cmd.Flags().StringVar(&jobPath, "job", "", "Job spec file (key: value YAML)")
	cmd.Flags().StringVar(&nodesFlag, "nodes", "", "Restrict candidates to these node ids (default: all configured)")
	cmd.Flags().Float64Var(&rlWeight, "rl-weight", 0.7, "RL score weight in the mix (with cost-weight must sum to 1.0)")
	cmd.Flags().Float64Var(&costWeight, "cost-weight", 0.3, "Cost score weight in the mix")
	cmd.Flags().Float64Var(&rlScore, "rl-score", 0, "Override uniform RL score for all nodes (0-1, default 0.8)")
	cmd.Flags().StringVar(&store, "store", defaultCostStore, "Cost store root")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json'")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	_ = cmd.MarkFlagRequired("job")
	return cmd
}

func renderCostOptimize(out io.Writer, job scheduler.JobSpec, choice *scheduler.BestCostChoice, rlW, costW float64, hash string) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintf(out, "  cafctl cost optimize · %s (%d× %s, %.1fh)\n", job.Name, job.GPUCount, orDash(job.GPUType), job.DurationHours)
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	greenBold.Fprintf(out, "%s recommended node: %s\n", OK(), choice.NodeID)
	fmt.Fprintf(out, "  Mix score:   %.4f  = rl %.3f × %.1f + cost %.3f × %.1f\n",
		choice.MixScore, choice.RLScore, rlW, choice.CostScore, costW)
	fmt.Fprintf(out, "  Total cost:  $%.2f over %.1fh\n", choice.Estimate.TotalCost, choice.Estimate.DurationHours)
	for _, b := range choice.Estimate.Breakdown {
		fmt.Fprintf(out, "    · %-6s $%.2f (%s)\n", b.Component, b.Amount, b.Detail)
	}
	if job.Budget > 0 {
		if choice.Estimate.BudgetExceeded {
			redBold.Fprintf(out, "  ✗ %s\n", choice.Estimate.Message)
		} else {
			green.Fprintf(out, "  ✓ %s\n", choice.Estimate.Message)
		}
	}
	if len(choice.Alternatives) > 0 {
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "  Rejected alternatives:")
		for _, alt := range choice.Alternatives {
			fmt.Fprintf(out, "    · %-12s $%.2f  %s\n", alt.NodeID, alt.TotalCost, orDash(alt.Message))
		}
	}
	if hash != "" {
		fmt.Fprintln(out, "")
		fmt.Fprintf(out, "  Attestation: %s\n", shortHex(hash))
		fmt.Fprintln(out, "  Receipt signed & hash-chained — the placement choice is offline-verifiable.")
	}
	fmt.Fprintln(out, "")
}
