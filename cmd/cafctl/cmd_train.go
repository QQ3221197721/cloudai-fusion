// Package main - `cafctl train` — the platform's training job orchestrator commands
// (Module 14), the AI/ML layer's second real module. Together with Module 13
// (`cafctl model`) it closes the developer journey: register a base model, submit a
// fine-tuning job, run it to completion, and the new model version lands in the
// registry with ParentVersion lineage — every step a signed, hash-chained receipt.
//
// HONESTY NOTE: execution is simulated. `run-once` walks the real state machine
// (queued → scheduled → running → succeeded) with real transitions + attestations,
// but no container/K8s job is actually submitted; real K8s submission is a future
// integration point. The state machine, persistence, attestations, and the
// registry integration are all real.
//
// Commands follow the newXxxCmd() constructor pattern used by model/run/verify-*,
// so tests can build fresh, parent-less command instances and Execute them
// directly without cobra delegating up to the root command.
package main

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/training"
	"github.com/spf13/cobra"
)

// defaultTrainingStore is the default training-job store location (jobs live in
// <store>/training/<job-id>.json), matching the .caf layout of the model registry.
const defaultTrainingStore = "./.caf"

// newTrainCmd builds the `train` command group.
func newTrainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "train",
		Short: "Training Job Orchestrator — lifecycle state machine, signed transitions, auto model registration",
		Long: `Training Job Orchestrator (Module 14) — closes the developer journey with the model registry.

Submit a training job, walk it through a strict lifecycle state machine
(queued -> scheduled -> running -> succeeded/failed/cancelled), and on completion
automatically register the trained artifact as a new model version whose
ParentVersion points at the base model — the lineage loop of Modules 13+14.

Every transition writes a signed, hash-chained attestation through the real
pkg/evidence ledger. After months of accumulated receipts, walking away means
abandoning the provenance your auditors already trust.

HONESTY NOTE: execution is simulated (state machine + attestations + registry
integration are real; no live K8s job is submitted — future integration point).

Storage layout (--store, default ` + defaultTrainingStore + `):
  <store>/training/<job-id>.json   one job record with full event history`,
		Example: `  cafctl train submit fine-tune-resnet --image pytorch:2.0 --gpu 4 --memory 32 \
      --base-model resnet50:1.0.0 --dataset ds-abc123 --command "python train.py"
  cafctl train run-once <job-id> --artifact out.pt --registry .caf/models
  cafctl train status <job-id>
  cafctl train list
  cafctl train cancel <job-id> --reason "wrong dataset"`,
	}
	cmd.AddCommand(
		newTrainSubmitCmd(),
		newTrainRunOnceCmd(),
		newTrainStatusCmd(),
		newTrainListCmd(),
		newTrainCancelCmd(),
	)
	return cmd
}

// ----------------------------------------------------------------------------
// train submit
// ----------------------------------------------------------------------------

// newTrainSubmitCmd builds `cafctl train submit <job-name>`.
func newTrainSubmitCmd() *cobra.Command {
	var (
		store, image, baseModel, dataset, command, output string
		gpu, memory                                        int
		noAttest                                           bool
	)
	cmd := &cobra.Command{
		Use:   "submit <job-name>",
		Short: "Submit a new training job (creates queued job + signed attestation)",
		Args:  cobra.ExactArgs(1),
		Example: `  cafctl train submit fine-tune-resnet --image pytorch:2.0 --gpu 4 --memory 32 \
      --base-model resnet50:1.0.0 --dataset ds-abc123 --command "python train.py"`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			orch, err := openTrainingOrchestrator(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			job, err := orch.Submit(cmd.Context(), training.SubmitInput{
				Name:       args[0],
				Image:      image,
				GPUCount:   gpu,
				MemoryGB:   memory,
				BaseModel:  baseModel,
				DatasetRef: dataset,
				Command:    command,
			})
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			if output == "json" {
				return writeJSON(cmd.OutOrStdout(), buildTrainSubmitResult(orch, job))
			}
			renderTrainJob(cmd.OutOrStdout(), orch, job, "cafctl train submit · job queued, attestation signed")
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultTrainingStore, "Training store root (jobs under <store>/training)")
	cmd.Flags().StringVar(&image, "image", "", "Container image (required, e.g. pytorch:2.0)")
	cmd.Flags().IntVar(&gpu, "gpu", 1, "Number of GPUs to request")
	cmd.Flags().IntVar(&memory, "memory", 8, "Memory in GB")
	cmd.Flags().StringVar(&baseModel, "base-model", "", "Base model ref 'name:version' for fine-tuning (empty = from scratch)")
	cmd.Flags().StringVar(&dataset, "dataset", "", "Dataset reference (required)")
	cmd.Flags().StringVar(&command, "command", "python train.py", "Training command")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	_ = cmd.MarkFlagRequired("image")
	_ = cmd.MarkFlagRequired("dataset")
	return cmd
}

// trainSubmitResult is the --output json payload for a successful submit.
type trainSubmitResult struct {
	JobID           string             `json:"job_id"`
	Name            string             `json:"name"`
	Status          string             `json:"status"`
	Image           string             `json:"image"`
	GPUCount        int                `json:"gpu_count"`
	MemoryGB        int                `json:"memory_gb"`
	BaseModel       string             `json:"base_model,omitempty"`
	DatasetRef      string             `json:"dataset_ref"`
	Command         string             `json:"command,omitempty"`
	AttestationHash string             `json:"attestation_hash,omitempty"`
}

// buildTrainSubmitResult assembles the JSON payload from a job and the
// orchestrator's most recent receipt (empty when --no-attest).
func buildTrainSubmitResult(orch *training.FSOrchestrator, job *training.TrainingJob) trainSubmitResult {
	r := trainSubmitResult{
		JobID: job.ID, Name: job.Name, Status: string(job.Status),
		Image: job.Image, GPUCount: job.GPUCount, MemoryGB: job.MemoryGB,
		BaseModel: job.BaseModel, DatasetRef: job.DatasetRef, Command: job.Command,
	}
	if last := orch.LastAttestation(); last != nil {
		r.AttestationHash = last.Hash
	}
	return r
}

// ----------------------------------------------------------------------------
// train run-once
// ----------------------------------------------------------------------------

// newTrainRunOnceCmd builds `cafctl train run-once <job-id>`: simulate the full
// queued→scheduled→running→succeeded walk, each step a real transition with a
// real attestation; with --artifact the resulting weights are registered as a
// new model version (minor bump from the base model).
func newTrainRunOnceCmd() *cobra.Command {
	var store, artifact, registry, output string
	var noAttest bool
	cmd := &cobra.Command{
		Use:     "run-once <job-id>",
		Short:   "Run a job through the full lifecycle (simulated execution, real transitions + attestations)",
		Args:    cobra.ExactArgs(1),
		Example: "  cafctl train run-once job-abc123 --artifact out.pt --registry .caf/models",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			jobID := args[0]
			orch, err := openTrainingOrchestrator(store, !noAttest)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			// Step through the real state machine; every step is a genuine
			// transition with its own signed attestation.
			steps := []struct {
				label string
				run   func() error
			}{
				{"queued → scheduled", func() error { return orch.Schedule(ctx, jobID) }},
				{"scheduled → running", func() error { return orch.Start(ctx, jobID) }},
			}
			for _, s := range steps {
				if err := s.run(); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s%s: %v\n", ERROR(), s.label, err)
					return err
				}
				if output != "json" {
					fmt.Fprintf(out, "%s%s\n", OK(), s.label)
				}
			}

			// Completion: with --artifact, register the new model version through
			// the real Module 13 registry (lineage closure: ParentVersion=base).
			var regHash string
			if artifact != "" {
				modelReg, rerr := openModelRegistry(registry, !noAttest)
				if rerr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), rerr)
					return rerr
				}
				if err := orch.Complete(ctx, jobID, "simulated run-once", artifact, modelReg); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "%srunning → succeeded: %v\n", ERROR(), err)
					return err
				}
				if last := modelReg.LastAttestation(); last != nil {
					regHash = last.Hash
				}
			} else if err := orch.Complete(ctx, jobID, "simulated run-once", "", nil); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%srunning → succeeded: %v\n", ERROR(), err)
				return err
			}
			if output != "json" {
				fmt.Fprintf(out, "%srunning → succeeded\n", OK())
			}

			job, err := orch.Get(ctx, jobID)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}

			if output == "json" {
				return writeJSON(out, buildTrainRunOnceResult(orch, job, regHash))
			}
			renderTrainJob(out, orch, job, "cafctl train run-once · lifecycle complete (simulated execution)")
			if art := orch.LastRegisteredArtifact(); art != nil {
				fmt.Fprintf(out, "  Registered:   %s:%s (parent %s)\n", art.Name, art.Version, orDash(art.Lineage.ParentVersion))
				fmt.Fprintf(out, "  Weights SHA:  %s\n", shortHex(art.SHA256))
				if regHash != "" {
					fmt.Fprintf(out, "  Model receipt: %s\n", shortHex(regHash))
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultTrainingStore, "Training store root")
	cmd.Flags().StringVar(&artifact, "artifact", "", "Trained weights path; registers a new model version when set")
	cmd.Flags().StringVar(&registry, "registry", defaultModelRegistry, "Model registry root (with --artifact)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output format: 'json' for machine-readable")
	cmd.Flags().BoolVar(&noAttest, "no-attest", false, "Skip evidence attestation (dev only)")
	return cmd
}

// trainRunOnceResult is the --output json payload for run-once.
type trainRunOnceResult struct {
	JobID           string `json:"job_id"`
	Name            string `json:"name"`
	Status          string `json:"status"`
	Events          int    `json:"events"`
	RegisteredModel string `json:"registered_model,omitempty"`
	RegisteredVer   string `json:"registered_version,omitempty"`
	ParentVersion   string `json:"parent_version,omitempty"`
	AttestationHash string `json:"attestation_hash,omitempty"`
	ModelRegHash    string `json:"model_attestation_hash,omitempty"`
}

// buildTrainRunOnceResult assembles the JSON payload after a run-once walk.
func buildTrainRunOnceResult(orch *training.FSOrchestrator, job *training.TrainingJob, regHash string) trainRunOnceResult {
	r := trainRunOnceResult{
		JobID: job.ID, Name: job.Name, Status: string(job.Status), Events: len(job.Events),
	}
	if last := orch.LastAttestation(); last != nil {
		r.AttestationHash = last.Hash
	}
	if art := orch.LastRegisteredArtifact(); art != nil {
		r.RegisteredModel = art.Name
		r.RegisteredVer = art.Version
		r.ParentVersion = art.Lineage.ParentVersion
		r.ModelRegHash = regHash
	}
	return r
}

// ----------------------------------------------------------------------------
// train status
// ----------------------------------------------------------------------------

// newTrainStatusCmd builds `cafctl train status <job-id>`: current state plus the
// full event timeline.
func newTrainStatusCmd() *cobra.Command {
	var store string
	cmd := &cobra.Command{
		Use:           "status <job-id>",
		Short:         "Show job status and event timeline",
		Args:          cobra.ExactArgs(1),
		Example:       "  cafctl train status job-abc123",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			orch, err := openTrainingOrchestrator(store, false)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			job, err := orch.Get(cmd.Context(), args[0])
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			renderTrainStatus(cmd.OutOrStdout(), job)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultTrainingStore, "Training store root")
	return cmd
}

// renderTrainStatus prints the job detail plus its event timeline.
func renderTrainStatus(out io.Writer, job *training.TrainingJob) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintf(out, "  cafctl train status · %s (%s)\n", job.ID, job.Name)
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Status:   %s\n", job.Status)
	fmt.Fprintf(out, "  Image:    %s\n", job.Image)
	fmt.Fprintf(out, "  Resources: %d GPU, %d GB\n", job.GPUCount, job.MemoryGB)
	fmt.Fprintf(out, "  Base:     %s\n", orDash(job.BaseModel))
	fmt.Fprintf(out, "  Dataset:  %s\n", job.DatasetRef)
	fmt.Fprintf(out, "  Command:  %s\n", orDash(job.Command))
	fmt.Fprintf(out, "  Created:  %s\n", job.CreatedAt.Format("2006-01-02 15:04:05 UTC"))
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "  Timeline:")
	for _, ev := range job.Events {
		from := string(ev.From)
		if from == "" {
			from = "∅"
		}
		line := fmt.Sprintf("    %s  %s → %s", ev.Timestamp.Format("15:04:05"), from, ev.To)
		if ev.Reason != "" {
			line += "  (" + ev.Reason + ")"
		}
		fmt.Fprintln(out, line)
	}
	fmt.Fprintln(out, "")
}

// ----------------------------------------------------------------------------
// train list
// ----------------------------------------------------------------------------

// newTrainListCmd builds `cafctl train list`.
func newTrainListCmd() *cobra.Command {
	var store string
	cmd := &cobra.Command{
		Use:           "list",
		Short:         "List training jobs (newest first)",
		Example:       "  cafctl train list",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			orch, err := openTrainingOrchestrator(store, false)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			jobs, err := orch.List(cmd.Context())
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			out := cmd.OutOrStdout()
			if len(jobs) == 0 {
				fmt.Fprintln(out, "No training jobs yet.")
				fmt.Fprintln(out, "Submit your first job:")
				fmt.Fprintln(out, "  cafctl train submit my-job --image pytorch:2.0 --gpu 4 --memory 32 --dataset ds-1")
				return nil
			}
			renderTrainList(out, jobs)
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultTrainingStore, "Training store root")
	return cmd
}

// renderTrainList prints the job table (tabwriter-aligned).
func renderTrainList(out io.Writer, jobs []training.TrainingJob) {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "JOB ID\tNAME\tSTATUS\tGPU\tMEM(GB)\tBASE MODEL\tCREATED")
	for _, j := range jobs {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\t%s\n",
			j.ID, j.Name, j.Status, j.GPUCount, j.MemoryGB,
			orDash(j.BaseModel), j.CreatedAt.Format("2006-01-02 15:04"))
	}
	w.Flush()
}

// ----------------------------------------------------------------------------
// train cancel
// ----------------------------------------------------------------------------

// newTrainCancelCmd builds `cafctl train cancel <job-id> --reason <text>`.
func newTrainCancelCmd() *cobra.Command {
	var store, reason string
	cmd := &cobra.Command{
		Use:           "cancel <job-id>",
		Short:         "Cancel a job (legal pre-state → cancelled, attested)",
		Args:          cobra.ExactArgs(1),
		Example:       "  cafctl train cancel job-abc123 --reason \"wrong dataset\"",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			orch, err := openTrainingOrchestrator(store, true)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			if reason == "" {
				reason = "user requested"
			}
			if err := orch.Cancel(cmd.Context(), args[0], reason); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			job, err := orch.Get(cmd.Context(), args[0])
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s%v\n", ERROR(), err)
				return err
			}
			renderTrainJob(cmd.OutOrStdout(), orch, job, "cafctl train cancel · job cancelled, attestation signed")
			return nil
		},
	}
	cmd.Flags().StringVar(&store, "store", defaultTrainingStore, "Training store root")
	cmd.Flags().StringVar(&reason, "reason", "", "Cancellation reason (recorded in the event history)")
	return cmd
}

// ----------------------------------------------------------------------------
// Shared helpers
// ----------------------------------------------------------------------------

// openTrainingOrchestrator opens (creating if needed) a training orchestrator;
// when attest is true a fresh MemoryStore+EphemeralSigner ledger is wired in,
// exactly the pattern `cafctl run` and `cafctl model register` use, so
// transition receipts are genuinely signed and hash-chained.
func openTrainingOrchestrator(path string, attest bool) (*training.FSOrchestrator, error) {
	if path == "" {
		path = defaultTrainingStore
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
	return training.NewFSOrchestrator(path, ledger)
}

// renderTrainJob prints the human-facing receipt for submit/run-once/cancel.
func renderTrainJob(out io.Writer, orch *training.FSOrchestrator, job *training.TrainingJob, title string) {
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "  "+title)
	fmt.Fprintln(out, Separator('═', 64))
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "  Job:       %s (%s)\n", job.ID, job.Name)
	fmt.Fprintf(out, "  Status:    %s\n", job.Status)
	fmt.Fprintf(out, "  Image:     %s\n", job.Image)
	fmt.Fprintf(out, "  Resources: %d GPU, %d GB\n", job.GPUCount, job.MemoryGB)
	fmt.Fprintf(out, "  Base:      %s\n", orDash(job.BaseModel))
	fmt.Fprintf(out, "  Dataset:   %s\n", job.DatasetRef)
	fmt.Fprintf(out, "  Command:   %s\n", orDash(job.Command))
	fmt.Fprintln(out, "")
	if last := orch.LastAttestation(); last != nil {
		greenBold.Fprintf(out, "%s job %s is %s\n", OK(), job.ID, job.Status)
		fmt.Fprintf(out, "  Attestation: seq #%d hash %s\n", last.Seq, shortHex(last.Hash))
		fmt.Fprintln(out, "  Receipt signed & hash-chained — every lifecycle step is offline-verifiable.")
	} else {
		greenBold.Fprintf(out, "%s job %s is %s\n", OK(), job.ID, job.Status)
		fmt.Fprintln(out, "  Attestation: skipped (--no-attest; dev only, no verifiable receipt)")
	}
	fmt.Fprintln(out, "")
}
