// Package main - `cafctl infer` — the platform's inference service mesh commands
// (Module 15), the serving half of the AI/ML developer journey. Module 13
// registers models, Module 14 trains new versions — Module 15 deploys them into
// a mesh with weighted traffic routing and per-service load telemetry, and every
// step is a signed, hash-chained receipt.
//
// Every deploy/route-set/record/stop runs through the real pkg/evidence ledger
// (in-memory store + ephemeral signer when no backend is configured), so the
// receipt is genuinely signed and hash-chained — same wiring as `cafctl run`.
//
// Commands follow the newXxxCmd() constructor pattern used by model/train/run,
// so tests can build fresh, parent-less command instances and Execute them
// directly without cobra delegating up to the root command.
package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/inference"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/modelregistry"
	"github.com/spf13/cobra"
)

// defaultInferenceStore is the default mesh store location (services live in
// <store>/inference/), matching the .caf layout of training and other modules.
const defaultInferenceStore = "./.caf"

// newInferCmd builds the `infer` command group.
func newInferCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "infer",
		Short: "Inference Service Mesh — deploy, weighted routing, load telemetry, signed attestations",
		Long: `Inference Service Mesh (Module 15) — serving-side lock-in of the AI/ML layer.

Deploy any registered model ("name@version") as a mesh service, shift traffic
between versions with weight-based routing (must sum to 100), record load
statistics (requests/errors/latency p50-p95-p99/throughput), and stop services —
every write is a signed, hash-chained attestation through the real evidence
ledger. After months of routing decisions and telemetry receipts, walking away
means abandoning the serving history your auditors already trust.

Storage layout (--store, default ` + defaultInferenceStore + `):
  <store>/inference/services.json              service list
  <store>/inference/<service-id>/stats.jsonl   append-only load stats`,
		Example: `  cafctl infer deploy --name prod-api --model my-model@v3 --replicas 2
  cafctl infer list
  cafctl infer show inf-a1b2c3d4e5f60718
  cafctl infer route-set inf-a1b2c3d4e5f60718 --weights v3=90,v4=10
  cafctl infer record inf-a1b2c3d4e5f60718 --requests 1000 --errors 12 \
      --latency-p50 8 --latency-p95 25 --latency-p99 60
  cafctl infer stats inf-a1b2c3d4e5f60718 --limit 5
  cafctl infer stop inf-a1b2c3d4e5f60718`,
	}
	cmd.AddCommand(
		newInferDeployCmd(),
		newInferListCmd(),
		newInferShowCmd(),
		newInferRouteSetCmd(),
		newInferRecordCmd(),
		newInferStatsCmd(),
		newInferStopCmd(),
	)
	return cmd
}

// ----------------------------------------------------------------------------
// infer deploy
// ----------------------------------------------------------------------------

// newInferDeployCmd builds `cafctl infer deploy --name X --model name@v3`.
func newInferDeployCmd() *cobra.Command {
	var (
		store, output          string
		name, model, endpoint  string
		replicas               int
		registry               string
		noAttest               bool
	)
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy a model version as a mesh service (100% routed, attested)",
		Example: `  cafctl infer deploy --name prod-api --model my-model@v3 --replicas 2 \
      --endpoint https://infer.example.com/v3
  cafctl infer deploy --name canary --model my-model@v4 --replicas 1 \
      --registry .caf/models   # gate deploy on registry presence`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// CLI-level guard on replicas before opening mesh/store.
			if replicas <= 0 {
				err := fmt.Errorf("--replicas must be positive, got %d", replicas)
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			mesh, err := openInferenceMesh(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			// Optional Module 13 integration: when --registry is given the
			// model ref must resolve there before the service deploys.
			if registry != "" {
				validate, verr := modelRegistryValidator(cmd.Context(), cmd.ErrOrStderr(), registry)
				if verr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), verr)
					return verr
				}
				mesh.SetModelValidator(validate)
			}
			svc, err := mesh.Deploy(cmd.Context(), inference.DeployInput{
				Name:     name,
				ModelRef: model,
				Endpoint: endpoint,
				Replicas: replicas,
			})
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			if output == "json" {
				return writeJSON(cmd.OutOrStdout(), buildInferDeployResult(mesh, svc))
			}
			renderInferDeploy(cmd.OutOrStdout(), mesh, svc)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultInferenceStore, "Mesh store root (services under <store>/inference)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	cmd.Flags().StringVar(&name, "name", "", "Service name (required)")
	cmd.Flags().StringVar(&model, "model", "", "Model ref 'name@version', e.g. my-model@v3 (required)")
	cmd.Flags().IntVar(&replicas, "replicas", 1, "Number of replicas")
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "Service endpoint URL (auto-generated when empty)")
	cmd.Flags().StringVar(&registry, "registry", "", "Optional model registry root; gates deploy on model presence")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("model")
	return cmd
}

// inferDeployResult is the --output json payload for a successful deploy.
type inferDeployResult struct {
	ServiceID       string             `json:"service_id"`
	Name            string             `json:"name"`
	ModelRef        string             `json:"model_ref"`
	Endpoint        string             `json:"endpoint"`
	Status          string             `json:"status"`
	Replicas        int                `json:"replicas"`
	Routes          map[string]int     `json:"routes"`
	CreatedAt       string             `json:"created_at"`
	AttestationHash string             `json:"attestation_hash,omitempty"`
}

// buildInferDeployResult assembles the JSON payload from a service and the
// mesh's most recent receipt (empty when --no-attest).
func buildInferDeployResult(mesh *inference.FSMInferenceMesh, svc *inference.Service) inferDeployResult {
	r := inferDeployResult{
		ServiceID: svc.ID, Name: svc.Name, ModelRef: svc.ModelRef,
		Endpoint: svc.Endpoint, Status: string(svc.Status), Replicas: svc.Replicas,
		Routes: svc.Routes, CreatedAt: svc.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if last := mesh.LastAttestation(); last != nil {
		r.AttestationHash = last.Hash
	}
	return r
}

// ----------------------------------------------------------------------------
// infer list
// ----------------------------------------------------------------------------

// newInferListCmd builds `cafctl infer list`.
func newInferListCmd() *cobra.Command {
	var store string
	cmd := &cobra.Command{
		Use:           "list",
		Short:         "List mesh services (newest first)",
		Example:       "  cafctl infer list",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mesh, err := openInferenceMesh(store, false)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			services, err := mesh.ListServices()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			out := cmd.OutOrStdout()
			if len(services) == 0 {
				fmt.Fprintln(out, "No inference services deployed yet.")
				fmt.Fprintln(out, "Deploy your first service:")
				fmt.Fprintln(out, "  cafctl infer deploy --name my-svc --model my-model@v3 --replicas 2")
				return nil
			}
			renderInferList(out, services)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultInferenceStore, "Mesh store root")
	return cmd
}

// renderInferList prints the service table (tabwriter-aligned).
func renderInferList(out io.Writer, services []inference.Service) {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tMODEL\tSTATUS\tREPLICAS")
	for _, svc := range services {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\n",
			svc.ID, svc.Name, svc.ModelRef, svc.Status, svc.Replicas)
	}
	w.Flush()
}

// ----------------------------------------------------------------------------
// infer show
// ----------------------------------------------------------------------------

// newInferShowCmd builds `cafctl infer show <service-id>`.
func newInferShowCmd() *cobra.Command {
	var store string
	cmd := &cobra.Command{
		Use:           "show <service-id>",
		Short:         "Show one service: status, endpoint, routes",
		Args:          cobra.ExactArgs(1),
		Example:       "  cafctl infer show inf-a1b2c3d4e5f60718",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mesh, err := openInferenceMesh(store, false)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			svc, err := mesh.GetService(args[0])
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			renderInferShow(cmd.OutOrStdout(), svc)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultInferenceStore, "Mesh store root")
	return cmd
}

// renderInferShow prints the single-service detail view.
func renderInferShow(out io.Writer, svc *inference.Service) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintf(out, "  cafctl infer show · %s (%s)\n", svc.ID, svc.Name)
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Model:     %s\n", svc.ModelRef)
	fmt.Fprintf(out, "  Status:    %s\n", svc.Status)
	fmt.Fprintf(out, "  Endpoint:  %s\n", svc.Endpoint)
	fmt.Fprintf(out, "  Replicas:  %d\n", svc.Replicas)
	fmt.Fprintf(out, "  Created:   %s\n", svc.CreatedAt.Format("2006-01-02 15:04:05 UTC"))
	fmt.Fprintf(out, "  Updated:   %s\n", svc.UpdatedAt.Format("2006-01-02 15:04:05 UTC"))
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  Routes:")
	for _, v := range sortedKeys(svc.Routes) {
		fmt.Fprintf(out, "    %s → %d%%\n", v, svc.Routes[v])
	}
	fmt.Fprintln(out, "")
}

// ----------------------------------------------------------------------------
// infer route-set
// ----------------------------------------------------------------------------

// newInferRouteSetCmd builds `cafctl infer route-set <id> --weights v3=90,v4=10`.
func newInferRouteSetCmd() *cobra.Command {
	var store, weights string
	var noAttest bool
	cmd := &cobra.Command{
		Use:           "route-set <service-id>",
		Short:         "Replace traffic weights (must sum to 100, attested)",
		Args:          cobra.ExactArgs(1),
		Example:       "  cafctl infer route-set inf-a1b2c3d4e5f60718 --weights v3=90,v4=10",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			parsed, err := parseWeightFlags(weights)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			mesh, err := openInferenceMesh(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			if err := mesh.SetRoute(cmd.Context(), args[0], parsed); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			svc, err := mesh.GetService(args[0])
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl infer route-set · traffic shifted, attestation signed")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "  Service:  %s (%s)\n", svc.ID, svc.Name)
			fmt.Fprintf(out, "  Model:   %s\n", svc.ModelRef)
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "  New routes:")
			for _, v := range sortedKeys(svc.Routes) {
				fmt.Fprintf(out, "    %s → %d%%\n", v, svc.Routes[v])
			}
			fmt.Fprintln(out, "")
			if last := mesh.LastAttestation(); last != nil {
				greenBold.Fprintf(out, "%s routes committed for %s\n", OK(), svc.ID)
				fmt.Fprintf(out, "  Attestation: seq #%d hash %s\n", last.Seq, shortHex(last.Hash))
				fmt.Fprintln(out, "  Receipt signed & hash-chained — every traffic shift is offline-verifiable.")
			} else {
				greenBold.Fprintf(out, "%s routes committed for %s\n", OK(), svc.ID)
				fmt.Fprintln(out, "  Attestation: skipped (--no-attest; dev only, no verifiable receipt)")
			}
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultInferenceStore, "Mesh store root")
	cmd.Flags().StringVar(&weights, "weights", "", "Comma-separated version=weight pairs, e.g. v3=90,v4=10 (required)")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	_ = cmd.MarkFlagRequired("weights")
	return cmd
}

// parseWeightFlags parses "v3=90,v4=10" into map[string]int. Values must parse
// as integers; the sum=100 check happens in the mesh package.
func parseWeightFlags(spec string) (map[string]int, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("--weights is required, e.g. v3=90,v4=10")
	}
	out := make(map[string]int)
	for _, pair := range strings.Split(spec, ",") {
		pair = strings.TrimSpace(pair)
		k, v, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, fmt.Errorf("invalid weight %q: expected version=weight", pair)
		}
		w, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return nil, fmt.Errorf("invalid weight %q: %v", pair, err)
		}
		k = strings.TrimSpace(k)
		if _, dup := out[k]; dup {
			return nil, fmt.Errorf("duplicate version %q in weights", k)
		}
		out[k] = w
	}
	return out, nil
}

// ----------------------------------------------------------------------------
// infer record
// ----------------------------------------------------------------------------

// newInferRecordCmd builds `cafctl infer record <id>` with load flags.
func newInferRecordCmd() *cobra.Command {
	var (
		store                   string
		requests, errCount      int64
		p50, p95, p99, rps      float64
		noAttest                bool
	)
	cmd := &cobra.Command{
		Use:   "record <service-id>",
		Short: "Append one load-stat record (latency triple p50<=p95<=p99, attested)",
		Args:  cobra.ExactArgs(1),
		Example: `  cafctl infer record inf-a1b2c3d4e5f60718 --requests 1000 --errors 12 \
      --latency-p50 8 --latency-p95 25 --latency-p99 60 --throughput 830`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mesh, err := openInferenceMesh(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			stat := inference.LoadStat{
				ServiceID:     args[0],
				Requests:      requests,
				Errors:        errCount,
				LatencyP50Ms:  p50,
				LatencyP95Ms:  p95,
				LatencyP99Ms:  p99,
				ThroughputRPS: rps,
			}
			if err := mesh.RecordStat(cmd.Context(), args[0], stat); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			out := cmd.OutOrStdout()
			greenBold.Fprintf(out, "%s stat recorded for %s\n", OK(), args[0])
			fmt.Fprintf(out, "  requests=%d errors=%d p50=%.1fms p95=%.1fms p99=%.1fms rps=%.1f\n",
				requests, errCount, p50, p95, p99, rps)
			if last := mesh.LastAttestation(); last != nil {
				fmt.Fprintf(out, "  Attestation: seq #%d hash %s\n", last.Seq, shortHex(last.Hash))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultInferenceStore, "Mesh store root")
	cmd.Flags().Int64Var(&requests, "requests", 0, "Request count in the window")
	cmd.Flags().Int64Var(&errCount, "errors", 0, "Error count in the window")
	cmd.Flags().Float64Var(&p50, "latency-p50", 0, "Latency p50 in milliseconds")
	cmd.Flags().Float64Var(&p95, "latency-p95", 0, "Latency p95 in milliseconds")
	cmd.Flags().Float64Var(&p99, "latency-p99", 0, "Latency p99 in milliseconds")
	cmd.Flags().Float64Var(&rps, "throughput", 0, "Throughput in requests/second")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	return cmd
}

// ----------------------------------------------------------------------------
// infer stats
// ----------------------------------------------------------------------------

// newInferStatsCmd builds `cafctl infer stats <id> [--limit N]`.
func newInferStatsCmd() *cobra.Command {
	var store string
	var limit int
	cmd := &cobra.Command{
		Use:           "stats <service-id>",
		Short:         "Show recent load stats (newest first)",
		Args:          cobra.ExactArgs(1),
		Example:       "  cafctl infer stats inf-a1b2c3d4e5f60718 --limit 5",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mesh, err := openInferenceMesh(store, false)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			stats, err := mesh.Stats(args[0], limit)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			out := cmd.OutOrStdout()
			if len(stats) == 0 {
				fmt.Fprintf(out, "No stats recorded for %s yet.\n", args[0])
				fmt.Fprintln(out, "Record one:")
				fmt.Fprintf(out, "  cafctl infer record %s --requests 100 --errors 1 --latency-p50 5 --latency-p95 10 --latency-p99 20\n", args[0])
				return nil
			}
			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "TIMESTAMP\tREQUESTS\tERRORS\tP50(ms)\tP95(ms)\tP99(ms)\tRPS")
			for _, s := range stats {
				fmt.Fprintf(w, "%s\t%d\t%d\t%.1f\t%.1f\t%.1f\t%.1f\n",
					s.Timestamp.Format("2006-01-02 15:04:05"),
					s.Requests, s.Errors,
					s.LatencyP50Ms, s.LatencyP95Ms, s.LatencyP99Ms, s.ThroughputRPS)
			}
			w.Flush()
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultInferenceStore, "Mesh store root")
	cmd.Flags().IntVar(&limit, "limit", 10, "Max entries to show (newest first)")
	return cmd
}

// ----------------------------------------------------------------------------
// infer stop
// ----------------------------------------------------------------------------

// newInferStopCmd builds `cafctl infer stop <id>`.
func newInferStopCmd() *cobra.Command {
	var store string
	var noAttest bool
	cmd := &cobra.Command{
		Use:           "stop <service-id>",
		Short:         "Stop a service (serving/degraded → stopped, attested)",
		Args:          cobra.ExactArgs(1),
		Example:       "  cafctl infer stop inf-a1b2c3d4e5f60718",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mesh, err := openInferenceMesh(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			if err := mesh.Stop(cmd.Context(), args[0]); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			svc, err := mesh.GetService(args[0])
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl infer stop · service drained, attestation signed")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "  Service:  %s (%s)\n", svc.ID, svc.Name)
			fmt.Fprintf(out, "  Status:   %s\n", svc.Status)
			fmt.Fprintf(out, "  Model:    %s\n", svc.ModelRef)
			fmt.Fprintln(out, "")
			if last := mesh.LastAttestation(); last != nil {
				greenBold.Fprintf(out, "%s service %s is stopped\n", OK(), svc.ID)
				fmt.Fprintf(out, "  Attestation: seq #%d hash %s\n", last.Seq, shortHex(last.Hash))
				fmt.Fprintln(out, "  Receipt signed & hash-chained — the drain is offline-verifiable.")
			} else {
				greenBold.Fprintf(out, "%s service %s is stopped\n", OK(), svc.ID)
				fmt.Fprintln(out, "  Attestation: skipped (--no-attest; dev only, no verifiable receipt)")
			}
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultInferenceStore, "Mesh store root")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	return cmd
}

// ----------------------------------------------------------------------------
// Shared helpers
// ----------------------------------------------------------------------------

// openInferenceMesh opens (creating if needed) an inference mesh; when attest is
// true a fresh MemoryStore+EphemeralSigner ledger is wired in, exactly the
// pattern `cafctl run` and the other module commands use, so receipts are
// genuinely signed and hash-chained.
func openInferenceMesh(path string, attest bool) (*inference.FSMInferenceMesh, error) {
	if path == "" {
		path = defaultInferenceStore
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
	return inference.NewFSMInferenceMesh(path, ledger)
}

// modelRegistryValidator opens the Module 13 registry and returns a
// ValidateModelFunc that resolves "name@version" refs against it. This is the
// optional integration seam between Modules 13 and 15 — the inference package
// itself stays decoupled from modelregistry.
func modelRegistryValidator(ctx context.Context, errOut io.Writer, registryPath string) (inference.ValidateModelFunc, error) {
	if registryPath == "" {
		return nil, nil
	}
	reg, err := modelregistry.NewFSRegistry(registryPath, nil)
	if err != nil {
		return nil, fmt.Errorf("open registry: %w", err)
	}
	return func(modelName, version string) error {
		if _, gerr := reg.Get(ctx, modelName, version); gerr != nil {
			fmt.Fprintf(errOut, "%sregistry check failed for %s@%s: %v\n", WARN(), modelName, version, gerr)
			return gerr
		}
		return nil
	}, nil
}

// renderInferDeploy prints the human-facing deployment receipt.
func renderInferDeploy(out io.Writer, mesh *inference.FSMInferenceMesh, svc *inference.Service) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "  cafctl infer deploy · service serving, attestation signed")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Service:   %s (%s)\n", svc.ID, svc.Name)
	fmt.Fprintf(out, "  Model:     %s\n", svc.ModelRef)
	fmt.Fprintf(out, "  Status:    %s\n", svc.Status)
	fmt.Fprintf(out, "  Endpoint:  %s\n", svc.Endpoint)
	fmt.Fprintf(out, "  Replicas:  %d\n", svc.Replicas)
	fmt.Fprintf(out, "  Created:   %s\n", svc.CreatedAt.Format("2006-01-02 15:04:05 UTC"))
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  Routes:")
	for _, v := range sortedKeys(svc.Routes) {
		fmt.Fprintf(out, "    %s → %d%%\n", v, svc.Routes[v])
	}
	fmt.Fprintln(out, "")
	if last := mesh.LastAttestation(); last != nil {
		greenBold.Fprintf(out, "%s deployed %s → %s\n", OK(), svc.Name, svc.ModelRef)
		fmt.Fprintf(out, "  Attestation: seq #%d hash %s\n", last.Seq, shortHex(last.Hash))
		fmt.Fprintln(out, "  Receipt signed & hash-chained — the deployment is offline-verifiable.")
	} else {
		greenBold.Fprintf(out, "%s deployed %s → %s\n", OK(), svc.Name, svc.ModelRef)
		fmt.Fprintln(out, "  Attestation: skipped (--no-attest; dev only, no verifiable receipt)")
	}
	fmt.Fprintln(out, "")
}
