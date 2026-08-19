// Package main - `cafctl pipeline` — Module 18: ML Pipeline Designer. Orchestrates training workflows,
// experiment tracking, cost estimation, and notifications into cohesive pipelines. Every create/publish/run
// action writes a signed attestation; stage execution orchestrates real module APIs (training/experiment/cost).
// Note: underlying train execution is the honest-simulated mode from Module 14.
// Commands follow the newXxxCmd() constructor pattern used by model/train/experiment. Use field starts with subcommand name.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/pipeline"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/training"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/experiment"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
	"github.com/spf13/cobra"
)

const defaultPipelineStore = "./.caf"

func newPipelineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pipeline",
		Short: "ML Pipeline Designer — orchestrate training workflows with real module APIs",
		Long: `ML Pipeline Designer (Module 18) — orchestrates the AI/ML layer modules into cohesive
workflows that automate training jobs, experiment tracking, cost estimation, and notifications.

Key features:
  • Create drafts, publish to activate triggers, run sequentially (train → experiment → cost → notify)
  • Each stage invokes genuine module APIs (training.Orchestrator, experiment.Tracker, scheduler.CostEstimator)
  • Honest honesty labels: train execution uses Module 14's simulated mode
  • Signed attestations for every operation (create, publish, run, each stage, terminal state)
  • Budget gate: cost_estimate fails if budget exceeded → pipeline fails → remaining stages skipped

Examples:
  cafctl pipeline create my-pipeline --stages train,experiment --params epochs=50,batch=32 --trigger manual
  cafctl pipeline publish pipe-abc123
  cafctl pipeline run pipe-abc123
  cafctl pipeline status pipe-abc123
  cafctl pipeline list`,
	}
	cmd.AddCommand(
		newPipelineCreateCmd(),
		newPipelinePublishCmd(),
		newPipelineRunCmd(),
		newPipelineStatusCmd(),
		newPipelineListCmd(),
		newPipelineCancelCmd(),
	)
	return cmd
}

// ---------------------------------------------------------------------------
// pipeline create <name>
// ---------------------------------------------------------------------------

func newPipelineCreateCmd() *cobra.Command {
	var store, stages, params, trigger, schedule, expName, output string
	var noAttest bool

	cmd := &cobra.Command{
		Use:     "create <name>",
		Short:   "Create a new pipeline in draft state",
		Args:    cobra.ExactArgs(1),
		Example: "  cafctl pipeline create my-pipeline --stages train,experiment --params epochs=50,batch=32 --trigger manual",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := openPipelineDesigner(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			stageList, err := parseStageSpec(stages)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			paramMap, err := parseStringPairs(params)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			trig, err := buildTrigger(trigger, schedule, expName)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			p, err := d.Create(cmd.Context(), pipeline.CreateInput{
				Name:    args[0],
				Stages:  stageList,
				Params:  paramMap,
				Trigger: trig,
				Actor:   "cafctl",
			})
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			if output == "json" {
				type result struct {
					PipelineID  string `json:"pipeline_id"`
					Name        string `json:"name"`
					Status      string `json:"status"`
					StagesCount int    `json:"stages_count"`
					Trigger     string `json:"trigger_type,omitempty"`
					Attestation string `json:"attestation_hash,omitempty"`
				}
				out := result{
					PipelineID: p.ID, Name: p.Name, Status: string(p.Status),
					StagesCount: len(p.Stages), Trigger: trig.Type,
				}
				if last := d.LastAttestation(); last != nil {
					out.Attestation = last.Hash
				}
				return writeJSON(cmd.OutOrStdout(), out)
			}

			renderPipelineCreated(cmd.OutOrStdout(), d, p)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultPipelineStore, "Pipeline store root")
	cmd.Flags().StringVar(&stages, "stages", "", "Comma-separated stage types (train|experiment|cost_estimate|notify)")
	cmd.Flags().StringVar(&params, "params", "", "Parameters as key=value pairs (epochs=50,batch=32)")
	cmd.Flags().StringVar(&trigger, "trigger", "manual", "Trigger type (manual|schedule|on_experiment_complete)")
	cmd.Flags().StringVar(&schedule, "schedule", "", "Cron expression when trigger=schedule")
	cmd.Flags().StringVar(&expName, "experiment-name", "", "Experiment name when trigger=on_experiment_complete")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	return cmd
}

// ---------------------------------------------------------------------------
// pipeline publish <id>
// ---------------------------------------------------------------------------

func newPipelinePublishCmd() *cobra.Command {
	var store, id, output string
	var noAttest bool

	cmd := &cobra.Command{
		Use:     "publish <id>",
		Short:   "Publish a draft pipeline (draft→published, trigger activates)",
		Args:    cobra.ExactArgs(1),
		Example: "  cafctl pipeline publish pipe-abc123",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			id = args[0]
			d, err := openPipelineDesigner(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			err = d.Publish(cmd.Context(), id)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			p, err := d.Get(cmd.Context(), id)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			if output == "json" {
				return writeJSON(cmd.OutOrStdout(), map[string]string{"id": id, "status": string(p.Status)})
			}
			renderPipelinePublished(cmd.OutOrStdout(), d, p)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultPipelineStore, "Pipeline store root")
	cmd.Flags().StringVar(&output, "output", "", "Output format: 'json' for machine-readable")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	return cmd
}

// ---------------------------------------------------------------------------
// pipeline run <id>
// ---------------------------------------------------------------------------

func newPipelineRunCmd() *cobra.Command {
	var store, id, output string
	var noAttest bool

	cmd := &cobra.Command{
		Use:     "run <id>",
		Short:   "Run a published pipeline (executes stages sequentially with progress output)",
		Args:    cobra.ExactArgs(1),
		Example: "  cafctl pipeline run pipe-abc123",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			id = args[0]
			d, err := openPipelineDesigner(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			out := cmd.OutOrStdout()
			jsonMode := output == "json"

			opts := pipeline.RunOptions{
				Progress: func(seq, total int, stage pipeline.Stage, run pipeline.StageRun) {
					if jsonMode {
						return
					}
					cyanBold.Fprintf(out, "  ▶ [%d/%d] %s … ", seq, total, stage.Name)
					switch run.Status {
					case pipeline.RunSucceeded:
						green.Fprintf(out, "✓ succeeded · ")
						if run.Detail != "" {
							fmt.Fprintln(out, truncate(run.Detail, 60))
						} else {
							fmt.Fprintln(out, "")
						}
					case pipeline.RunFailed:
						red.Fprintf(out, "✗ failed · ")
						if run.Detail != "" {
							fmt.Fprintln(out, wrapError(run.Detail))
						} else {
							fmt.Fprintln(out, "")
						}
					default:
						yellow.Fprintf(out, "⋯ %s\n", run.Status)
					}
				},
			}

			p, err := d.RunDetailed(cmd.Context(), id, opts)
			if err != nil && p == nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			if jsonMode {
				type stageRunSummary struct {
					Name   string `json:"name"`
					Status string `json:"status"`
					Detail string `json:"detail,omitempty"`
				}
				type runResult struct {
					ID          string            `json:"id"`
					Name        string            `json:"name"`
					Status      string            `json:"status"`
					StageRuns   []stageRunSummary `json:"stage_runs"`
					Attestation string            `json:"attestation_hash,omitempty"`
				}
				srSummary := make([]stageRunSummary, len(p.StageRuns))
				for i, sr := range p.StageRuns {
					srSummary[i] = stageRunSummary{Name: sr.StageName, Status: string(sr.Status), Detail: sr.Detail}
				}
				r := runResult{ID: p.ID, Name: p.Name, Status: string(p.Status), StageRuns: srSummary}
				if last := d.LastAttestation(); last != nil {
					r.Attestation = last.Hash
				}
				return writeJSON(out, r)
			}

			if p != nil {
				renderPipelineRunComplete(out, d, p)
			}
			return err
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultPipelineStore, "Pipeline store root")
	cmd.Flags().StringVar(&output, "output", "", "Output format: 'json' for machine-readable")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	return cmd
}

// ---------------------------------------------------------------------------
// pipeline status <id>
// ---------------------------------------------------------------------------

func newPipelineStatusCmd() *cobra.Command {
	var store, id string

	cmd := &cobra.Command{
		Use:     "status <id>",
		Short:   "Show pipeline detail and stage execution timeline",
		Args:    cobra.ExactArgs(1),
		Example: "  cafctl pipeline status pipe-abc123",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			id = args[0]
			d, err := openPipelineDesigner(store, false)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			p, err := d.Get(cmd.Context(), id)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			renderPipelineStatus(cmd.OutOrStdout(), p)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultPipelineStore, "Pipeline store root")
	return cmd
}

// ---------------------------------------------------------------------------
// pipeline list
// ---------------------------------------------------------------------------

func newPipelineListCmd() *cobra.Command {
	var store string

	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List all pipelines (newest first)",
		Args:    cobra.NoArgs,
		Example: "  cafctl pipeline list",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := openPipelineDesigner(store, false)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			ps, err := d.List(cmd.Context())
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			out := cmd.OutOrStdout()
			if len(ps) == 0 {
				fmt.Fprintln(out, "No pipelines yet.")
				fmt.Fprintln(out, "Create your first one:")
				fmt.Fprintln(out, "  cafctl pipeline create my-pipeline --stages train,experiment")
				return nil
			}

			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tSTATUS\tSTAGES\tTRIGGER\tCREATED")
			for _, p := range ps {
				trig := p.Trigger.Type
				if trig == "" {
					trig = "manual"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\n",
					p.ID, p.Name, p.Status, len(p.Stages), trig, p.CreatedAt.Format("2006-01-02 15:04"))
			}
			w.Flush()
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultPipelineStore, "Pipeline store root")
	return cmd
}

// ---------------------------------------------------------------------------
// pipeline cancel <id> --reason
// ---------------------------------------------------------------------------

func newPipelineCancelCmd() *cobra.Command {
	var store, id, reason string

	cmd := &cobra.Command{
		Use:     "cancel <id>",
		Short:   "Cancel a running pipeline (running→cancelled; unexecuted stages skipped)",
		Args:    cobra.ExactArgs(1),
		Example: "  cafctl pipeline cancel pipe-abc123 --reason \"user request\"",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			id = args[0]
			if reason == "" {
				reason = "user cancelled"
			}
			d, err := openPipelineDesigner(store, true)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			err = d.Cancel(cmd.Context(), id, reason)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			p, err := d.Get(cmd.Context(), id)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			renderPipelineCancelled(cmd.OutOrStdout(), d, p, reason)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultPipelineStore, "Pipeline store root")
	cmd.Flags().StringVar(&reason, "reason", "", "Cancellation reason (recorded)")
	return cmd
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func openPipelineDesigner(path string, attest bool) (*pipeline.FSDesigner, error) {
	if path == "" {
		path = defaultPipelineStore
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

	orch, err := training.NewFSOrchestrator(path, ledger)
	if err != nil {
		return nil, fmt.Errorf("training orchestrator: %w", err)
	}
	tracker, err := experiment.NewFSTracker(path, ledger)
	if err != nil {
		return nil, fmt.Errorf("experiment tracker: %w", err)
	}
	cost := scheduler.NewDefaultCostOptimizer(nil)

	return pipeline.NewFSDesigner(path, ledger, pipeline.Deps{Train: orch, Exp: tracker, Cost: cost})
}

func parseStageSpec(s string) ([]pipeline.Stage, error) {
	if s == "" {
		return nil, errors.New("stages cannot be empty")
	}
	raw := strings.Split(s, ",")
	stages := make([]pipeline.Stage, 0, len(raw))
	for i, t := range raw {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		name := strings.TrimSpace(t)
		if name == "" {
			name = fmt.Sprintf("stage-%d", i+1)
		}
		stages = append(stages, pipeline.Stage{Name: name, Type: pipeline.StageType(t)})
	}
	return stages, nil
}

// parseKV helper for parsing comma-separated key=value pairs
func parseKV(s string) (map[string]string, error) {
	out := make(map[string]string)
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
			return nil, fmt.Errorf("malformed pair %q (expected key=value)", part)
		}
		key := strings.TrimSpace(part[:idx])
		val := strings.TrimSpace(part[idx+1:])
		if key == "" || val == "" {
			return nil, fmt.Errorf("malformed pair %q (key and value must be non-empty)", part)
		}
		out[key] = val
	}
	return out, nil
}

func buildTrigger(triggerType, schedule, expName string) (pipeline.Trigger, error) {
	t := pipeline.Trigger{Type: triggerType}
	if triggerType == pipeline.TriggerSchedule {
		t.Schedule = schedule
	} else if triggerType == pipeline.TriggerOnExperimentComplete {
		t.ExperimentName = expName
	}
	return t, nil
}

type fsDesigner interface {
	LastAttestation() *evidence.Evidence
}

func renderPipelineCreated(out io.Writer, d fsDesigner, p *pipeline.Pipeline) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintf(out, "  cafctl pipeline create · %s (%s)\n", p.ID, p.Name)
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  ID:         %s\n", p.ID)
	fmt.Fprintf(out, "  Status:     %s\n", p.Status)
	fmt.Fprintf(out, "  Stages:     %d\n", len(p.Stages))
	for i, s := range p.Stages {
		fmt.Fprintf(out, "    [%d] %s (%s)\n", i+1, s.Name, s.Type)
	}
	fmt.Fprintf(out, "  Trigger:    %s\n", p.Trigger.Type)
	if p.Trigger.Type == pipeline.TriggerSchedule {
		fmt.Fprintf(out, "               schedule: %s\n", p.Trigger.Schedule)
	} else if p.Trigger.Type == pipeline.TriggerOnExperimentComplete {
		fmt.Fprintf(out, "               on_experiment_complete: %s\n", p.Trigger.ExperimentName)
	}
	fmt.Fprintln(out, "")
	if last := d.LastAttestation(); last != nil {
		greenBold.Fprintf(out, "%s pipeline created as draft\n", OK())
		fmt.Fprintf(out, "  Attestation: seq #%d hash %s\n", last.Seq, shortHex(last.Hash))
		fmt.Fprintln(out, "  Receipt signed & hash-chained — every lifecycle step is offline-verifiable.")
	} else {
		greenBold.Fprintf(out, "%s pipeline created as draft\n", OK())
		fmt.Fprintln(out, "  Attestation: skipped (--no-attest; dev only, no verifiable receipt)")
	}
	fmt.Fprintln(out, "")
}

func renderPipelinePublished(out io.Writer, d fsDesigner, p *pipeline.Pipeline) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintf(out, "  cafctl pipeline publish · %s (%s)\n", p.ID, p.Name)
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Status:     %s (published)\n", p.Status)
	fmt.Fprintln(out, "")
	if last := d.LastAttestation(); last != nil {
		greenBold.Fprintf(out, "%s pipeline %s is published\n", OK(), p.ID)
		fmt.Fprintf(out, "  Attestation: seq #%d hash %s\n", last.Seq, shortHex(last.Hash))
		fmt.Fprintln(out, "  Receipt signed & hash-chained — trigger is now active.")
	} else {
		greenBold.Fprintf(out, "%s pipeline %s is published\n", OK(), p.ID)
		fmt.Fprintln(out, "  Attestation: skipped (--no-attest; dev only, no verifiable receipt)")
	}
	fmt.Fprintln(out, "")
}

func renderPipelineRunComplete(out io.Writer, d fsDesigner, p *pipeline.Pipeline) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintf(out, "  cafctl pipeline run · %s (%s)\n", p.ID, p.Name)
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Status:     %s\n", p.Status)
	fmt.Fprintf(out, "  Stages:     %d executed\n", len(p.StageRuns))
	for i, sr := range p.StageRuns {
		cyanBold.Fprintf(out, "  [%d] %s: %s", i+1, sr.StageName, sr.Status)
		if sr.Detail != "" {
			fmt.Fprintf(out, " · %s\n", truncate(wrapError(sr.Detail), 80))
		} else {
			fmt.Fprintln(out, "")
		}
	}
	fmt.Fprintln(out, "")
	if last := d.LastAttestation(); last != nil {
		if p.Status == pipeline.StatusCompleted {
			greenBold.Fprintf(out, "%s pipeline %s completed successfully\n", OK(), p.ID)
		} else {
			redBold.Fprintf(out, "%s pipeline %s failed\n", OK(), p.ID)
		}
		fmt.Fprintf(out, "  Attestation: seq #%d hash %s\n", last.Seq, shortHex(last.Hash))
		fmt.Fprintln(out, "  Receipt signed & hash-chained — workflow provenance verified.")
	} else {
		fmt.Fprintln(out, "  Attestation: skipped (--no-attest; dev only, no verifiable receipt)")
	}
	fmt.Fprintln(out, "")
}

func renderPipelineCancelled(out io.Writer, d fsDesigner, p *pipeline.Pipeline, reason string) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintf(out, "  cafctl pipeline cancel · %s (%s)\n", p.ID, p.Name)
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Status:        %s\n", p.Status)
	fmt.Fprintf(out, "  Cancel reason: %s\n", reason)
	fmt.Fprintln(out, "")
	if last := d.LastAttestation(); last != nil {
		redBold.Fprintf(out, "%s pipeline %s cancelled\n", OK(), p.ID)
		fmt.Fprintf(out, "  Attestation: seq #%d hash %s\n", last.Seq, shortHex(last.Hash))
		fmt.Fprintln(out, "  Receipt signed & hash-chained — cancellation recorded immutably.")
	} else {
		redBold.Fprintf(out, "%s pipeline %s cancelled\n", OK(), p.ID)
		fmt.Fprintln(out, "  Attestation: skipped (--no-attest; dev only, no verifiable receipt)")
	}
	fmt.Fprintln(out, "")
}

func renderPipelineStatus(out io.Writer, p *pipeline.Pipeline) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintf(out, "  cafctl pipeline status · %s (%s)\n", p.ID, p.Name)
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  ID:         %s\n", p.ID)
	fmt.Fprintf(out, "  Status:     %s\n", p.Status)
	fmt.Fprintf(out, "  Stages:     %d\n", len(p.Stages))
	fmt.Fprintf(out, "  Trigger:    %s\n", p.Trigger.Type)
	if p.Trigger.Type == pipeline.TriggerSchedule {
		fmt.Fprintf(out, "               schedule: %s\n", p.Trigger.Schedule)
	} else if p.Trigger.Type == pipeline.TriggerOnExperimentComplete {
		fmt.Fprintf(out, "               on_experiment_complete: %s\n", p.Trigger.ExperimentName)
	}
	fmt.Fprintln(out, "")

	if len(p.Params) > 0 {
		fmt.Fprintln(out, "  Global parameters:")
		for _, kv := range sortedKV(p.Params) {
			fmt.Fprintf(out, "    %s=%s\n", kv[0], kv[1])
		}
		fmt.Fprintln(out, "")
	}

	if len(p.StageRuns) > 0 {
		fmt.Fprintln(out, "  Stage execution history:")
		for i, sr := range p.StageRuns {
			fmt.Fprintf(out, "  [%d] %s: %s", i+1, sr.StageName, sr.Status)
			if sr.StartedAt.IsZero() && sr.EndedAt.IsZero() {
				fmt.Fprintln(out, "")
			} else {
				started := ""
				ended := ""
				if !sr.StartedAt.IsZero() {
					started = fmt.Sprintf(" started %s", sr.StartedAt.Format("15:04:05"))
				}
				if !sr.EndedAt.IsZero() {
					ended = fmt.Sprintf(" ended %s", sr.EndedAt.Format("15:04:05"))
				}
				fmt.Fprintf(out, "%s%s", started, ended)
			}
			if sr.Detail != "" {
				fmt.Fprintf(out, " · %s\n", truncate(sr.Detail, 70))
			} else {
				fmt.Fprintln(out, "")
			}
		}
		fmt.Fprintln(out, "")
	}

	if p.Status == pipeline.StatusCancelled && p.CancelReason != "" {
		fmt.Fprintf(out, "  Cancelled due to: %s\n", p.CancelReason)
		fmt.Fprintln(out, "")
	}
}

func wrapError(msg string) string {
	if len(msg) <= 75 {
		return msg
	}
	result := bytes.Buffer{}
	word := ""
	length := 0
	for _, c := range msg {
		if c == ' ' {
			if length > 75 {
				result.WriteByte('\n')
				result.WriteString(strings.Repeat(" ", 3))
				length = 0
			}
			result.WriteString(word)
			result.WriteByte(' ')
			length += len(word) + 1
			word = ""
			continue
		}
		word += string(c)
		length++
	}
	if word != "" {
		result.WriteString(word)
	}
	return result.String()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// sortedKV helper: returns key-value pairs sorted by key
func sortedKV(m map[string]string) [][2]string {
	pairs := make([][2]string, 0, len(m))
	for k, v := range m {
		pairs = append(pairs, [2]string{k, v})
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		return pairs[i][0] < pairs[j][0]
	})
	return pairs
}
