// Package main - `cafctl cloud` — the Multi-Cloud Unified Interface commands
// (Module 2): provider inventory across 6 clouds, aggregated cluster listing,
// connectivity pings, cost-optimal planning from the static GPU price tables,
// and the attested cluster lifecycle FSM history.
//
// Design notes:
//   - Providers with no credentials register in stub mode (honest reporting,
//     identical to pkg/cloud behavior). When no config file defines providers,
//     all 6 clouds are registered credential-less so `provider-list` and
//     `plan` work out of the box — the developer-experience core.
//   - `plan`/`estimate-cost` never call cloud APIs: prices come from the
//     hardcoded GetGPUPricing tables and every run is attested ("cloud.plan")
//     through the real evidence ledger (MemoryStore + EphemeralSigner).
//   - Output style matches cmd_pool.go (Separator banners, tabwriter tables).
package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/cloud"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/config"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/spf13/cobra"
)

const defaultCloudStore = "./.caf"

// defaultCloudProviders registers all 6 supported clouds with no credentials.
// Each provider degrades to stub mode (empty results on list, static pricing
// on plan) — matching Manager/NewProvider semantics exactly.
func defaultCloudProviders() []config.CloudProviderConfig {
	return []config.CloudProviderConfig{
		{Name: "aliyun", Type: string(common.CloudProviderAliyun), Region: "cn-hangzhou"},
		{Name: "aws", Type: string(common.CloudProviderAWS), Region: "us-east-1"},
		{Name: "azure", Type: string(common.CloudProviderAzure), Region: "eastus"},
		{Name: "gcp", Type: string(common.CloudProviderGCP), Region: "us-central1"},
		{Name: "huawei", Type: string(common.CloudProviderHuawei), Region: "cn-north-4"},
		{Name: "tencent", Type: string(common.CloudProviderTencent), Region: "ap-guangzhou"},
	}
}

// newCloudCmd builds the `cloud` command group.
func newCloudCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cloud",
		Short: "Multi-Cloud Unified Interface — providers, clusters, cost plans, lifecycle FSM",
		Long: `Multi-Cloud Unified Interface (Module 2) — one control surface over 6 clouds
(Alibaba ACK, AWS EKS, Azure AKS, GCP GKE, Huawei CCE, Tencent TKE).

Plan GPU clusters across all clouds from the static price tables — zero
credentials, zero API calls — then execute through the attested lifecycle FSM
(pending → provisioning → ready/failed; deleting → deleted). Every transition
is a signed, hash-chained receipt in the evidence ledger plus an append-only
row in operations.jsonl.

Without a config file, all 6 providers register in honest stub mode:
provider-list shows every cloud, plan prices every option, cluster-list
returns empty until credentials exist.

Storage layout (--store, default ` + defaultCloudStore + `):
  <store>/cloud/operations.jsonl   lifecycle events (last-write-wins per op ID)`,
		Example: `  cafctl cloud provider-list
  cafctl cloud cluster-list --provider aws
  cafctl cloud ping --all
  cafctl cloud plan --gpu-type nvidia-a100 --gpu-nodes 4 --duration-hours 24
  cafctl cloud estimate-cost --gpu-type nvidia-a100 --gpu-nodes 4 --duration-hours 24
  cafctl cloud operations --limit 10`,
	}
	cmd.AddCommand(
		newCloudProviderListCmd(),
		newCloudClusterListCmd(),
		newCloudPingCmd(),
		newCloudPlanCmd(),
		newCloudEstimateCostCmd(),
		newCloudOperationsCmd(),
	)
	return cmd
}

// ----------------------------------------------------------------------------
// Shared helpers
// ----------------------------------------------------------------------------

// openCloudManager builds a Manager: providers come from --config when the
// file defines any, otherwise all 6 clouds register credential-less (stub).
func openCloudManager(cmd *cobra.Command) (*cloud.Manager, error) {
	providers := defaultCloudProviders()
	if cfg, err := config.Load(cmd); err == nil && cfg != nil && len(cfg.CloudProviders) > 0 {
		providers = cfg.CloudProviders
	}
	return cloud.NewManager(cloud.ManagerConfig{Providers: providers})
}

// providerHasCredentials reports whether this provider config carries an AK/SK
// pair (GCP uses credentials_json in Extra instead).
func providerHasCredentials(p config.CloudProviderConfig) bool {
	if p.Type == string(common.CloudProviderGCP) || p.Type == string(common.CloudProviderAzure) {
		return p.Extra["credentials_json"] != "" || p.Extra["client_id"] != ""
	}
	return p.AccessKeyID != "" && p.AccessKeySecret != ""
}

// configuredProviderSet maps provider name → whether it has credentials, for
// real|stub MODE labeling in provider-list/ping.
func configuredProviderSet(cmd *cobra.Command) map[string]bool {
	out := map[string]bool{}
	if cfg, err := config.Load(cmd); err == nil && cfg != nil {
		for _, p := range cfg.CloudProviders {
			out[p.Name] = providerHasCredentials(p)
		}
	}
	return out
}

// openCloudTracker opens an OperationTracker under --store; when attest is
// true a fresh MemoryStore+EphemeralSigner ledger is wired (cmd_pool pattern).
func openCloudTracker(store string, attest bool) (*cloud.OperationTracker, error) {
	if store == "" {
		store = defaultCloudStore
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
	return cloud.NewOperationTracker(store, ledger)
}

// ----------------------------------------------------------------------------
// cloud provider-list
// ----------------------------------------------------------------------------

func newCloudProviderListCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:     "provider-list",
		Short:   "List registered cloud providers (NAME/TYPE/REGION/MODE)",
		Example: "  cafctl cloud provider-list",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := openCloudManager(cmd)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			creds := configuredProviderSet(cmd)
			providers := mgr.ListProviders()
			out := cmd.OutOrStdout()

			if output == "json" {
				rows := make([]map[string]any, 0, len(providers))
				for _, p := range providers {
					mode := "stub"
					if creds[p.Name()] {
						mode = "real"
					}
					rows = append(rows, map[string]any{
						"name": p.Name(), "type": string(p.Type()),
						"region": p.Region(), "mode": mode,
					})
				}
				return writeJSON(out, rows)
			}

			if len(providers) == 0 {
				fmt.Fprintln(out, "No cloud providers registered.")
				return nil
			}
			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tTYPE\tREGION\tMODE")
			for _, p := range providers {
				mode := "stub"
				if creds[p.Name()] {
					mode = "real"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.Name(), string(p.Type()), p.Region(), mode)
			}
			w.Flush()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "  stub = registered without credentials (list ops return empty, pricing still plans).")
			return nil
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	return cmd
}

// ----------------------------------------------------------------------------
// cloud cluster-list
// ----------------------------------------------------------------------------

func newCloudClusterListCmd() *cobra.Command {
	var providerFilter string
	cmd := &cobra.Command{
		Use:     "cluster-list",
		Short:   "List clusters across all providers (or one with --provider)",
		Example: "  cafctl cloud cluster-list --provider aws",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := openCloudManager(cmd)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			clusters, err := mgr.ListAllClusters(cmd.Context())
			if err != nil {
				// Stub providers return empty, not error; an error here means
				// every provider failed — surface honestly but keep going.
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
			}
			if providerFilter != "" {
				filtered := clusters[:0:0]
				for _, c := range clusters {
					if strings.EqualFold(string(c.Provider), providerFilter) || strings.EqualFold(c.Name, providerFilter) {
						filtered = append(filtered, c)
					}
				}
				clusters = filtered
			}

			out := cmd.OutOrStdout()
			if len(clusters) == 0 {
				fmt.Fprintln(out, "No clusters found (stub providers return empty until credentials are configured).")
				return nil
			}
			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tPROVIDER\tREGION\tSTATUS\tNODES\tGPU_NODES")
			for _, c := range clusters {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%d\n",
					c.ID, c.Name, string(c.Provider), c.Region, c.Status, c.NodeCount, c.GPUNodeCount)
			}
			w.Flush()
			return nil
		},
	}
	cmd.Flags().StringVarP(&providerFilter, "provider", "p", "", "Filter by provider type or name")
	return cmd
}

// ----------------------------------------------------------------------------
// cloud ping
// ----------------------------------------------------------------------------

func newCloudPingCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:     "ping",
		Short:   "Check provider connectivity (stub mode shows degraded, not error)",
		Example: "  cafctl cloud ping --all",
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr, err := openCloudManager(cmd)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			creds := configuredProviderSet(cmd)
			out := cmd.OutOrStdout()

			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "PROVIDER\tREGION\tRESULT\tDETAIL")
			anyFailed := false
			for _, p := range mgr.ListProviders() {
				pctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
				perr := p.Ping(pctx)
				cancel()

				result, detail := "ok", "reachable"
				if perr != nil {
					result, detail = "error", perr.Error()
					anyFailed = true
				} else if !creds[p.Name()] {
					// Stub mode pings nothing — report degraded honestly.
					result, detail = "degraded", "stub mode (no credentials configured)"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.Name(), p.Region(), result, detail)
			}
			w.Flush()
			fmt.Fprintln(out, "")
			if anyFailed && all {
				return fmt.Errorf("cloud: at least one credentialed provider failed its ping")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Exit non-zero if any provider errors (stub-degraded still passes)")
	return cmd
}

// ----------------------------------------------------------------------------
// cloud plan
// ----------------------------------------------------------------------------

func newCloudPlanCmd() *cobra.Command {
	var gpuType, region string
	var gpuNodes, durationHours int
	var store string
	var noAttest bool
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Cost-optimal multi-cloud plan from static price tables (no API calls)",
		Example: "  cafctl cloud plan --gpu-type nvidia-a100 --gpu-nodes 4 --duration-hours 24\n" +
			"  cafctl cloud plan --gpu-type nvidia-h100 --gpu-nodes 8 --duration-hours 168 --region us-east-1",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			spec := cloud.ResourceSpec{
				GPUType:       strings.TrimSpace(gpuType),
				GPUNodes:      gpuNodes,
				Region:        strings.TrimSpace(region),
				DurationHours: durationHours,
			}
			if err := spec.Validate(); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			mgr, err := openCloudManager(cmd)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			options, err := cloud.NewPlanEngine().Generate(cmd.Context(), mgr, spec)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			if len(options) == 0 {
				err := fmt.Errorf("no provider returned pricing for gpu type %q", spec.GPUType)
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			tracker, terr := openCloudTracker(store, !noAttest)
			if terr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), terr)
				return terr
			}
			attestHash, aerr := tracker.Attest(cmd.Context(), "cloud.plan", fmt.Sprintf("%s×%d×%dh", spec.GPUType, spec.GPUNodes, spec.DurationHours),
				map[string]any{"gpu_type": spec.GPUType, "gpu_nodes": spec.GPUNodes, "duration_hours": spec.DurationHours, "region": spec.Region},
				map[string]any{"options": len(options), "cheapest": options[0].Provider, "cheapest_total": options[0].TotalCost, "cheapest_currency": options[0].Currency})
			if aerr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), aerr)
				return aerr
			}
			renderCloudPlan(cmd.OutOrStdout(), spec, options, attestHash, noAttest)
			return nil
		},
	}
	cmd.Flags().StringVar(&gpuType, "gpu-type", "nvidia-a100", "GPU type (e.g. nvidia-a100, nvidia-h100)")
	cmd.Flags().IntVar(&gpuNodes, "gpu-nodes", 4, "Number of GPU nodes")
	cmd.Flags().IntVar(&durationHours, "duration-hours", 24, "Rental window in hours")
	cmd.Flags().StringVar(&region, "region", "", "Preferred region (hoists matching providers)")
	cmd.Flags().StringVar(&store, "store", defaultCloudStore, "Cloud store root")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	return cmd
}

func renderCloudPlan(out io.Writer, spec cloud.ResourceSpec, options []*cloud.PlanOption, attestHash string, noAttest bool) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 72))
	fmt.Fprintln(out, "  cafctl cloud plan · multi-cloud cost comparison")
	fmt.Fprintln(out, Separator('═', 72))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  GPU Type:         %s\n", spec.GPUType)
	fmt.Fprintf(out, "  GPU Nodes:        %d\n", spec.GPUNodes)
	fmt.Fprintf(out, "  Duration:         %d h\n", spec.DurationHours)
	if spec.Region != "" {
		fmt.Fprintf(out, "  Region:           %s (preferred)\n", spec.Region)
	}
	fmt.Fprintf(out, "  Providers:        %d compared\n", len(options))
	fmt.Fprintln(out, "")
	yellow.Fprintf(out, "  ~ plan-only, no cloud API calls. Prices are static on-demand table values\n")
	yellow.Fprintf(out, "    in each provider's native currency (see CUR column).\n")
	fmt.Fprintln(out, "")

	fmt.Fprintln(out, "  Cost-sorted options (cheapest first):")
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  #\tPROVIDER\tREGION\tINSTANCE\tHOURLY\tMONTHLY\tTOTAL\tCUR")
	for i, o := range options {
		inst := o.InstanceType
		if inst == "" {
			inst = "-"
		}
		fmt.Fprintf(w, "  %d\t%s\t%s\t%s\t%.2f\t%.0f\t%.2f\t%s\n",
			i+1, o.Provider, o.Region, inst, o.HourlyCost, o.MonthlyCost, o.TotalCost, o.Currency)
	}
	w.Flush()
	fmt.Fprintln(out, "")

	best := options[0]
	fmt.Fprintln(out, "  Recommended (lowest total in native currency):")
	fmt.Fprintf(out, "    %s — %s × %d nodes, %d h\n", best.Provider, spec.GPUType, spec.GPUNodes, spec.DurationHours)
	fmt.Fprintf(out, "    total %.2f %s (hourly %.2f / monthly ~%.0f)\n", best.TotalCost, best.Currency, best.HourlyCost, best.MonthlyCost)
	fmt.Fprintln(out, "    Pros:")
	for _, p := range best.Pros {
		fmt.Fprintf(out, "      ✓ %s\n", p)
	}
	fmt.Fprintln(out, "")
	greenBold.Fprintf(out, "%s plan generated for %d provider(s)\n", OK(), len(options))
	if attestHash != "" {
		fmt.Fprintf(out, "  Attestation: %s (action cloud.plan)\n", shortHex(attestHash))
		fmt.Fprintln(out, "  Receipt signed & hash-chained — the plan decision is offline-verifiable.")
	} else {
		fmt.Fprintln(out, "  Attestation: skipped (--no-attest; dev only, no verifiable receipt)")
	}
	fmt.Fprintln(out, "")
}

// ----------------------------------------------------------------------------
// cloud estimate-cost
// ----------------------------------------------------------------------------

func newCloudEstimateCostCmd() *cobra.Command {
	var gpuType string
	var gpuNodes, durationHours int
	var output string
	cmd := &cobra.Command{
		Use:     "estimate-cost",
		Short:   "One-table cost estimate for a GPU spec across all providers",
		Example: "  cafctl cloud estimate-cost --gpu-type nvidia-a100 --gpu-nodes 4 --duration-hours 24",
		RunE: func(cmd *cobra.Command, args []string) error {
			spec := cloud.ResourceSpec{
				GPUType:       strings.TrimSpace(gpuType),
				GPUNodes:      gpuNodes,
				DurationHours: durationHours,
			}
			if err := spec.Validate(); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			mgr, err := openCloudManager(cmd)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			options, err := cloud.NewPlanEngine().Generate(cmd.Context(), mgr, spec)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			out := cmd.OutOrStdout()
			if output == "json" {
				return writeJSON(out, options)
			}
			renderCloudEstimate(out, spec, options)
			return nil
		},
	}
	cmd.Flags().StringVar(&gpuType, "gpu-type", "nvidia-a100", "GPU type")
	cmd.Flags().IntVar(&gpuNodes, "gpu-nodes", 4, "Number of GPU nodes")
	cmd.Flags().IntVar(&durationHours, "duration-hours", 24, "Rental window in hours")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	return cmd
}

func renderCloudEstimate(out io.Writer, spec cloud.ResourceSpec, options []*cloud.PlanOption) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "  cafctl cloud estimate-cost")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Spec: %s × %d nodes × %d h  (static plan-only prices)\n", spec.GPUType, spec.GPUNodes, spec.DurationHours)
	fmt.Fprintln(out, "")
	if len(options) == 0 {
		fmt.Fprintln(out, "  No pricing available for this GPU type.")
		fmt.Fprintln(out, "")
		return
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  PROVIDER\tREGION\tHOURLY\tMONTHLY\tTOTAL\tCUR")
	for _, o := range options {
		fmt.Fprintf(w, "  %s\t%s\t%.2f\t%.0f\t%.2f\t%s\n",
			o.Provider, o.Region, o.HourlyCost, o.MonthlyCost, o.TotalCost, o.Currency)
	}
	w.Flush()
	fmt.Fprintln(out, "")
	minC, maxC := options[0], options[len(options)-1]
	fmt.Fprintf(out, "  Spread: cheapest %s %.2f ↔ priciest %s %.2f\n", minC.Provider, minC.TotalCost, maxC.Provider, maxC.TotalCost)
	fmt.Fprintln(out, "")
}

// ----------------------------------------------------------------------------
// cloud operations
// ----------------------------------------------------------------------------

func newCloudOperationsCmd() *cobra.Command {
	var store string
	var limit int
	var output string
	cmd := &cobra.Command{
		Use:     "operations",
		Short:   "Cluster lifecycle operation history (newest first)",
		Example: "  cafctl cloud operations --limit 10",
		RunE: func(cmd *cobra.Command, args []string) error {
			tracker, err := openCloudTracker(store, false)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			ops, err := tracker.List(limit)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			out := cmd.OutOrStdout()
			if output == "json" {
				return writeJSON(out, ops)
			}
			if len(ops) == 0 {
				fmt.Fprintln(out, "No lifecycle operations recorded yet.")
				fmt.Fprintln(out, "Operations appear here as clusters move through the FSM")
				fmt.Fprintln(out, "(pending → provisioning → ready/failed → deleting → deleted).")
				return nil
			}
			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tSTATE\tPROVIDER\tCLUSTER\tUPDATED\tEVIDENCE")
			for _, op := range ops {
				ev := "-"
				if op.EvidenceHash != "" {
					ev = shortHex(op.EvidenceHash)
				}
				cluster := op.ClusterID
				if cluster == "" {
					cluster = "-"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					op.ID, string(op.State), op.Provider, cluster,
					op.UpdatedAt.Format("2006-01-02 15:04"), ev)
			}
			w.Flush()
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultCloudStore, "Cloud store root")
	cmd.Flags().IntVar(&limit, "limit", 10, "Max entries to show (newest first; 0 = all)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	return cmd
}
