// Package main - `cafctl model` — the platform's model registry commands
// (Module 13), the AI/ML layer's first real module and the second ecosystem
// lock-in anchor: the model lineage format.
//
// Every register/rollback runs through the real pkg/evidence ledger
// (in-memory store + ephemeral signer when no backend is configured), so the
// receipt is genuinely signed and hash-chained — same wiring as `cafctl run`.
//
// Commands follow the newXxxCmd() constructor pattern used by run/verify-*,
// so tests can build fresh, parent-less command instances and Execute them
// directly without cobra delegating up to the root command.
package main

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/modelregistry"
	"github.com/spf13/cobra"
)

// defaultModelRegistry is the default registry location, created on demand.
const defaultModelRegistry = "./.caf/models"

// newModelCmd builds the `model` command group.
func newModelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "model",
		Short: "Model Registry — content-addressed versioning, lineage DAG, signed attestations",
		Long: `Model Registry (Module 13) — our second ecosystem lock-in anchor.

A registry is easy to replace on day one and prohibitively expensive on day 300:
by then every model version carries a content-addressed blob, a recursive lineage
DAG (dataset -> code -> parent versions), and signed hash-chained attestations.
Migrating means abandoning the provenance your auditors already trust — the
Dockerfile effect, for models.

Storage layout (--registry, default ` + defaultModelRegistry + `):
  <registry>/<name>/<version>.json   immutable version record
  <registry>/<name>/_current         current serving version pointer
  <registry>/blobs/<sha256>          deduplicated model weights`,
		Example: `  cafctl model register weights.pt --name resnet50 --version 1.0.0 \
      --dataset sha256:ds1 --code git:abc1234 --metric accuracy=0.94
  cafctl model list [--name resnet50]
  cafctl model show resnet50:1.0.0
  cafctl model lineage resnet50:1.2.0
  cafctl model rollback resnet50 --to 1.1.0`,
	}
	cmd.AddCommand(
		newModelRegisterCmd(),
		newModelListCmd(),
		newModelShowCmd(),
		newModelLineageCmd(),
		newModelRollbackCmd(),
	)
	return cmd
}

// ----------------------------------------------------------------------------
// model register
// ----------------------------------------------------------------------------

// newModelRegisterCmd builds `cafctl model register <artifact-path>`.
func newModelRegisterCmd() *cobra.Command {
	var (
		registry, output                 string
		noAttest                         bool
		name, version, createdBy         string
		dataset, codeRef, parent         string
		taskType, framework, summary     string
		metricFlags, paramFlags, tagFlags []string
	)
	cmd := &cobra.Command{
		Use:     "register <artifact-path>",
		Short:   "Register a new model version with signed lineage attestation",
		Args:    cobra.ExactArgs(1),
		Example: "  cafctl model register weights.pt --name resnet50 --version 1.1.0 --parent 1.0.0 --metric accuracy=0.97",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := openModelRegistry(registry, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			art, err := reg.Register(cmd.Context(), modelregistry.RegisterInput{
				Name:          name,
				Version:       version,
				ArtifactPath:  args[0],
				DatasetRef:    dataset,
				CodeRef:       codeRef,
				ParentVersion: parent,
				Hyperparams:   parseKVStrings(paramFlags),
				Metrics:       parseKVMetrics(cmd.ErrOrStderr(), metricFlags),
				Tags:          parseKVStrings(tagFlags),
				TaskType:      taskType,
				Framework:     framework,
				Summary:       summary,
				CreatedBy:     createdBy,
			})
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			if output == "json" {
				return writeJSON(cmd.OutOrStdout(), buildRegisterResult(reg, art))
			}
			renderModelRegister(cmd.OutOrStdout(), reg, art)
			return nil
		},
	}
	cmd.Flags().StringVar(&registry, "registry", defaultModelRegistry, "Registry root path")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	cmd.Flags().StringVar(&name, "name", "", "Model name (required)")
	cmd.Flags().StringVar(&version, "version", "", "Semantic version MAJOR.MINOR.PATCH (required)")
	cmd.Flags().StringVar(&createdBy, "created-by", "", "Actor recorded on the version and attestation")
	cmd.Flags().StringVar(&dataset, "dataset", "", "Dataset reference (sha256 or path)")
	cmd.Flags().StringVar(&codeRef, "code", "", "Training code commit hash")
	cmd.Flags().StringVar(&parent, "parent", "", "Parent version (fine-tune ancestor, same model)")
	cmd.Flags().StringArrayVar(&metricFlags, "metric", nil, "Model-card metric name=value (repeatable)")
	cmd.Flags().StringArrayVar(&paramFlags, "param", nil, "Hyperparameter name=value (repeatable)")
	cmd.Flags().StringArrayVar(&tagFlags, "tag", nil, "Tag name[=value] (repeatable)")
	cmd.Flags().StringVar(&taskType, "task", "", "Task type (classification/detection/generation)")
	cmd.Flags().StringVar(&framework, "framework", "", "Framework (pytorch/tensorflow)")
	cmd.Flags().StringVar(&summary, "summary", "", "Human-readable model summary")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("version")
	return cmd
}

// modelRegisterResult is the --output json payload for a successful register.
type modelRegisterResult struct {
	Name            string                  `json:"name"`
	Version         string                  `json:"version"`
	SHA256          string                  `json:"sha256"`
	SizeBytes       int64                   `json:"size_bytes"`
	Current         bool                    `json:"current"`
	Lineage         modelregistry.Lineage   `json:"lineage"`
	ModelCard       modelregistry.ModelCard `json:"model_card"`
	AttestationHash string                  `json:"attestation_hash,omitempty"`
}

// buildRegisterResult assembles the JSON payload from an artifact and the
// registry's most recent receipt (empty when --no-attest).
func buildRegisterResult(reg *modelregistry.FSRegistry, art *modelregistry.ModelArtifact) modelRegisterResult {
	r := modelRegisterResult{
		Name: art.Name, Version: art.Version, SHA256: art.SHA256,
		SizeBytes: art.SizeBytes, Current: true,
		Lineage: art.Lineage, ModelCard: art.ModelCard,
	}
	if last := reg.LastAttestation(); last != nil {
		r.AttestationHash = last.Hash
	}
	return r
}

// ----------------------------------------------------------------------------
// model list
// ----------------------------------------------------------------------------

// newModelListCmd builds `cafctl model list`.
func newModelListCmd() *cobra.Command {
	var registry, name string
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List model versions (newest first)",
		Example: "  cafctl model list --name resnet50",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := openModelRegistry(registry, false)
			if err != nil {
				return err
			}
			arts, err := reg.List(cmd.Context(), name)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			out := cmd.OutOrStdout()
			if len(arts) == 0 {
				if name != "" {
					fmt.Fprintf(out, "No versions registered for model %q.\n", name)
				} else {
					fmt.Fprintln(out, "Registry is empty — no models registered yet.")
				}
				fmt.Fprintln(out, "Register your first version:")
				fmt.Fprintln(out, "  cafctl model register weights.pt --name mymodel --version 1.0.0")
				return nil
			}
			renderModelList(out, arts)
			return nil
		},
	}
	cmd.Flags().StringVar(&registry, "registry", defaultModelRegistry, "Registry root path")
	cmd.Flags().StringVar(&name, "name", "", "Restrict to one model (default: all models)")
	return cmd
}

// renderModelList prints the version table (tabwriter-aligned).
func renderModelList(out io.Writer, arts []modelregistry.ModelArtifact) {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tVERSION\tCREATED\tPARENT\tSHA256(16)\tSIZE")
	for _, a := range arts {
		created := a.CreatedAt.Format("2006-01-02 15:04")
		parent := a.Lineage.ParentVersion
		if parent == "" {
			parent = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\n",
			a.Name, a.Version, created, parent, shortHex(a.SHA256), a.SizeBytes)
	}
	w.Flush()
}

// ----------------------------------------------------------------------------
// model show
// ----------------------------------------------------------------------------

// newModelShowCmd builds `cafctl model show <name>:<version>`.
func newModelShowCmd() *cobra.Command {
	var registry string
	cmd := &cobra.Command{
		Use:     "show <name>:<version>",
		Short:   "Show one model version and its model card",
		Args:    cobra.ExactArgs(1),
		Example: "  cafctl model show resnet50:1.0.0\n  cafctl model show resnet50:latest",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name, version, err := parseModelRef(args[0])
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			reg, err := openModelRegistry(registry, false)
			if err != nil {
				return err
			}
			art, err := reg.Get(cmd.Context(), name, version)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			renderModelShow(cmd.OutOrStdout(), art)
			return nil
		},
	}
	cmd.Flags().StringVar(&registry, "registry", defaultModelRegistry, "Registry root path")
	return cmd
}

// ----------------------------------------------------------------------------
// model lineage
// ----------------------------------------------------------------------------

// newModelLineageCmd builds `cafctl model lineage <name>:<version>`.
func newModelLineageCmd() *cobra.Command {
	var registry string
	cmd := &cobra.Command{
		Use:     "lineage <name>:<version>",
		Short:   "Walk the recursive lineage DAG (dataset -> code -> parent chain)",
		Args:    cobra.ExactArgs(1),
		Example: "  cafctl model lineage resnet50:1.2.0",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name, version, err := parseModelRef(args[0])
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			reg, err := openModelRegistry(registry, false)
			if err != nil {
				return err
			}
			graph, err := reg.Lineage(cmd.Context(), name, version)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			renderModelLineage(cmd.OutOrStdout(), graph)
			return nil
		},
	}
	cmd.Flags().StringVar(&registry, "registry", defaultModelRegistry, "Registry root path")
	return cmd
}

// ----------------------------------------------------------------------------
// model rollback
// ----------------------------------------------------------------------------

// newModelRollbackCmd builds `cafctl model rollback <name> --to <version>`.
func newModelRollbackCmd() *cobra.Command {
	var registry, to, from string
	var noAttest bool
	cmd := &cobra.Command{
		Use:     "rollback <name> --to <version>",
		Short:   "Roll the current serving version back (pointer move + attestation)",
		Args:    cobra.ExactArgs(1),
		Example: "  cafctl model rollback resnet50 --to 1.1.0",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			reg, err := openModelRegistry(registry, !noAttest)
			if err != nil {
				return err
			}
			name := args[0]
			// Resolve the current version so the rollback is an auditable,
			// conflict-checked from -> to move (from is authoritative when given).
			fromVer := from
			if fromVer == "" {
				cur, cerr := reg.Current(name)
				if cerr != nil {
					return cerr
				}
				fromVer = cur
			}
			if err := reg.Rollback(cmd.Context(), name, fromVer, to); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl model rollback · serving pointer repointed, data intact")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "  Model:       %s\n", name)
			fmt.Fprintf(out, "  Rolled back: %s → %s\n", fromVer, to)
			if last := reg.LastAttestation(); last != nil {
				fmt.Fprintf(out, "  Attestation: seq #%d hash %s\n", last.Seq, shortHex(last.Hash))
				fmt.Fprintln(out, "  Receipt signed & hash-chained — rollback verifiably recorded.")
			} else {
				fmt.Fprintln(out, "  Attestation:  skipped (--no-attest; dev only, no verifiable receipt)")
			}
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringVar(&registry, "registry", defaultModelRegistry, "Registry root path")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	cmd.Flags().StringVar(&to, "to", "", "Target version to roll back to (required)")
	cmd.Flags().StringVar(&from, "from", "", "Expected current version (conflict guard; default: auto)")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

// ----------------------------------------------------------------------------
// Shared helpers
// ----------------------------------------------------------------------------

// openModelRegistry opens (creating if needed) a model registry; when attest
// is true a fresh MemoryStore+EphemeralSigner ledger is wired in, exactly the
// pattern `cafctl run` uses, so receipts are genuinely signed.
func openModelRegistry(path string, attest bool) (*modelregistry.FSRegistry, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
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
	return modelregistry.NewFSRegistry(abs, ledger)
}

// parseModelRef splits "name:version" (version defaults to latest).
func parseModelRef(ref string) (name, version string, err error) {
	parts := strings.SplitN(ref, ":", 2)
	name = strings.TrimSpace(parts[0])
	if name == "" {
		return "", "", fmt.Errorf("invalid model ref %q: name is empty", ref)
	}
	version = modelregistry.LatestVersion
	if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
		version = strings.TrimSpace(parts[1])
	}
	return name, version, nil
}

// parseKVStrings turns ["k=v", "bare"] into {"k":"v","bare":""}.
func parseKVStrings(flags []string) map[string]string {
	if len(flags) == 0 {
		return nil
	}
	m := make(map[string]string, len(flags))
	for _, s := range flags {
		k, v, _ := strings.Cut(s, "=")
		m[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return m
}

// parseKVMetrics turns ["accuracy=0.94"] into {"accuracy":0.94}, warning on
// malformed entries instead of failing the whole registration.
func parseKVMetrics(errOut io.Writer, flags []string) map[string]float64 {
	if len(flags) == 0 {
		return nil
	}
	m := make(map[string]float64, len(flags))
	for _, s := range flags {
		k, v, ok := strings.Cut(s, "=")
		if !ok {
			fmt.Fprintf(errOut, "%signoring malformed metric %q (expected name=value)\n", WARN(), s)
			continue
		}
		f, perr := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if perr != nil {
			fmt.Fprintf(errOut, "%signoring unparseable metric %q: %v\n", WARN(), s, perr)
			continue
		}
		m[strings.TrimSpace(k)] = f
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// sortedKeys returns map keys in deterministic order for stable output.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ----------------------------------------------------------------------------
// Pretty renderers
// ----------------------------------------------------------------------------

// renderModelRegister prints the human-facing registration receipt.
func renderModelRegister(out io.Writer, reg *modelregistry.FSRegistry, art *modelregistry.ModelArtifact) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "  cafctl model register · content-addressed version + lineage attestation")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Model:     %s\n", art.Name)
	fmt.Fprintf(out, "  Version:   %s  (now current)\n", art.Version)
	fmt.Fprintf(out, "  SHA-256:   %s\n", shortHex(art.SHA256))
	fmt.Fprintf(out, "  Size:      %d bytes\n", art.SizeBytes)
	fmt.Fprintf(out, "  By:        %s at %s\n", art.CreatedBy, art.CreatedAt.Format("2006-01-02 15:04 UTC"))
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  Lineage:")
	fmt.Fprintf(out, "    Dataset:  %s\n", orDash(art.Lineage.DatasetRef))
	fmt.Fprintf(out, "    Code:     %s\n", orDash(art.Lineage.CodeRef))
	fmt.Fprintf(out, "    Parent:   %s\n", orDash(art.Lineage.ParentVersion))
	for _, k := range sortedKeys(art.Lineage.Hyperparams) {
		fmt.Fprintf(out, "    %s: %s\n", k, art.Lineage.Hyperparams[k])
	}
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  Model card:")
	fmt.Fprintf(out, "    Task:      %s\n", orDash(art.ModelCard.TaskType))
	fmt.Fprintf(out, "    Framework: %s\n", orDash(art.ModelCard.Framework))
	if art.ModelCard.Summary != "" {
		fmt.Fprintf(out, "    Summary:   %s\n", art.ModelCard.Summary)
	}
	for _, k := range sortedKeys(art.ModelCard.Metrics) {
		fmt.Fprintf(out, "    %s = %g\n", k, art.ModelCard.Metrics[k])
	}
	for _, k := range sortedKeys(art.Tags) {
		fmt.Fprintf(out, "    tag %s=%s\n", k, art.Tags[k])
	}
	fmt.Fprintln(out, "")
	if last := reg.LastAttestation(); last != nil {
		greenBold.Fprintf(out, "%s registered %s:%s\n", OK(), art.Name, art.Version)
		fmt.Fprintf(out, "  Attestation: seq #%d hash %s\n", last.Seq, shortHex(last.Hash))
		fmt.Fprintln(out, "  Receipt signed & hash-chained — offline-verifiable lineage lock-in.")
	} else {
		greenBold.Fprintf(out, "%s registered %s:%s\n", OK(), art.Name, art.Version)
		fmt.Fprintln(out, "  Attestation: skipped (--no-attest; dev only, no verifiable receipt)")
	}
	fmt.Fprintln(out, "")
}

// renderModelShow prints the single-version detail view.
func renderModelShow(out io.Writer, art *modelregistry.ModelArtifact) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintf(out, "  cafctl model show · %s:%s\n", art.Name, art.Version)
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  SHA-256:    %s\n", art.SHA256)
	fmt.Fprintf(out, "  Size:       %d bytes\n", art.SizeBytes)
	fmt.Fprintf(out, "  Created by: %s at %s\n", art.CreatedBy, art.CreatedAt.Format("2006-01-02 15:04:05 UTC"))
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  Lineage:")
	fmt.Fprintf(out, "    Dataset:    %s\n", orDash(art.Lineage.DatasetRef))
	fmt.Fprintf(out, "    Code:       %s\n", orDash(art.Lineage.CodeRef))
	fmt.Fprintf(out, "    Parent:     %s\n", orDash(art.Lineage.ParentVersion))
	for _, k := range sortedKeys(art.Lineage.Hyperparams) {
		fmt.Fprintf(out, "    %s: %s\n", k, art.Lineage.Hyperparams[k])
	}
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  Model card:")
	fmt.Fprintf(out, "    Task:       %s\n", orDash(art.ModelCard.TaskType))
	fmt.Fprintf(out, "    Framework:  %s\n", orDash(art.ModelCard.Framework))
	if art.ModelCard.Summary != "" {
		fmt.Fprintf(out, "    Summary:    %s\n", art.ModelCard.Summary)
	}
	for _, k := range sortedKeys(art.ModelCard.Metrics) {
		fmt.Fprintf(out, "    %s = %g\n", k, art.ModelCard.Metrics[k])
	}
	for _, k := range sortedKeys(art.Tags) {
		fmt.Fprintf(out, "    tag %s=%s\n", k, art.Tags[k])
	}
	fmt.Fprintln(out, "")
}

// renderModelLineage prints the recursive lineage chain, newest first.
func renderModelLineage(out io.Writer, graph *modelregistry.LineageGraph) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('─', 64))
	fmt.Fprintf(out, "  cafctl model lineage · %s (depth %d)\n", graph.Root, graph.Depth)
	fmt.Fprintln(out, Separator('─', 64))
	fmt.Fprintln(out, "")
	for i, node := range graph.Nodes {
		branch := "├──"
		if i == len(graph.Nodes)-1 {
			branch = "└──"
		}
		fmt.Fprintf(out, "%s %s:%s\n", branch, node.Name, node.Version)
		fmt.Fprintf(out, "    dataset=%s code=%s\n", orDash(node.Lineage.DatasetRef), orDash(node.Lineage.CodeRef))
		if node.Lineage.ParentVersion == "" {
			fmt.Fprintln(out, "    parent=root")
		}
	}
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  Chain:")
	for _, e := range graph.Edges {
		fmt.Fprintf(out, "    %s → %s\n", e.From, e.To)
	}
	fmt.Fprintln(out, "")
}

// orDash renders empty strings as an em dash for tidy output.
func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
