// Package main - cafctl tutorial commands (Module 44: Interactive Tutorial Engine)
//
// This file implements the tutorial command group for interacting with the
// interactive tutorial engine. It exposes three subcommands:
//
//   - list: Lists all steps in a tutorial (from tutorial.go)
//   - status: Shows completion progress for all steps (from progress.go)
//   - verify <cert.json>: Offline Ed25519 certificate verification (from certificate.go)
//
// The implementation uses only the standard library + cobra + tabwriter; no network calls.

package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/tutorial"
	"github.com/spf13/cobra"
)

// newTutorialCmd creates the `tutorial` command group.
func newTutorialCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tutorial",
		Short: "Interactive Tutorial Engine (M44)",
		Long: `Interactive Tutorial Engine (Module 44) — step definitions, progress tracking, and offline-verifiable certificates.

This engine powers the frontend InteractiveTutorial.tsx component and implements:
  • Tutorial/DAG: Steps form a DAG with prerequisites; Kahn topological sort detects cycles.
  • Progress: Concurrency-safe state machine (NotStarted → InProgress → Completed).
  • Certificates: Ed25519-signed proofs with SHA-256 step hash chain (offline verifiable).

No credentials or network access required. All operations are read-only and deterministic.`,
		Example: `  cafctl tutorial list --tutorial basic-linux
  cafctl tutorial status --tutorial basic-linux
  cafctl tutorial verify ./certificate.json`,
	}

	cmd.AddCommand(
		newTutorialListCmd(),
		newTutorialStatusCmd(),
		newTutorialVerifyCmd(),
	)

	return cmd
}

// ----------------------------------------------------------------------------
// tutorial list
// ----------------------------------------------------------------------------

func newTutorialListCmd() *cobra.Command {
	var tutorialID string
	var outputFormat string // json|table
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List steps in a tutorial (from embedded spec)",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Use a built-in tutorial JSON for immediate runnability
			const basicLinuxTut = `{
  "id": "basic-linux",
  "title": "Basic Linux Operations",
  "steps": [
    {"id": "intro", "title": "Introduction", "instruction": "Read this section.", "validator_type": "always_pass"},
    {"id": "ls", "title": "List Files", "instruction": "Run ls -la and check directory contents.", "prerequisites": ["intro"], "validator_type": "command_output", "validator_params": {"command": "ls -la", "pattern": "total.*"}}
  ]
}`

			tut, err := tutorial.LoadTutorialJSON([]byte(basicLinuxTut))
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%sFailed to load tutorial: %v\n", ERROR(), err)
				return err
			}

			if outputFormat == "json" {
				type StepSummary struct {
					ID              string            `json:"id"`
					Title           string            `json:"title"`
					Prerequisites   []string          `json:"prerequisites,omitempty"`
					ValidatorType   string            `json:"validator_type"`
					ValidatorParams map[string]string `json:"validator_params,omitempty"`
				}
				type TutorialJSON struct {
					ID    string        `json:"id"`
					Title string        `json:"title"`
					Steps []StepSummary `json:"steps"`
				}
				out := TutorialJSON{
					ID:    tut.ID,
					Title: tut.Title,
					Steps: make([]StepSummary, len(tut.Steps)),
				}
				for i, s := range tut.Steps {
					out.Steps[i] = StepSummary{
						ID:              s.ID,
						Title:           s.Title,
						Prerequisites:   s.Prerequisites,
						ValidatorType:   string(s.ValidatorType),
						ValidatorParams: s.ValidatorParams,
					}
				}
				w := cmd.OutOrStdout()
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 72))
			fmt.Fprintln(out, "  cafctl tutorial list · module 44 tutorial engine")
			fmt.Fprintln(out, Separator('═', 72))
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "Tutorial ID: %s\n", tut.ID)
			fmt.Fprintf(out, "Title: %s\n", tut.Title)
			fmt.Fprintf(out, "Total Steps: %d\n", len(tut.Steps))

			order, err := tut.TopologicalOrder()
			if err != nil {
				fmt.Fprintf(out, "Warning: Could not compute order: %v\n", err)
			} else {
				fmt.Fprintf(out, "Topological Order: %s\n", strings.Join(order, " → "))
			}

			fmt.Fprintln(out, "")
			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "STEP_ID\tTITLE\tPREREQUISITES\tVALIDATOR")
			for _, s := range tut.Steps {
				preStr := "-"
				if len(s.Prerequisites) > 0 {
					preStr = strings.Join(s.Prerequisites, ",")
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.ID, s.Title, preStr, s.ValidatorType)
			}
			w.Flush()

			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "  Notes:")
			fmt.Fprintln(out, "    • Prerequisite steps must be Completed before entering this step.")
			fmt.Fprintln(out, "    • validators are checked when marking Complete (not Start).")
			fmt.Fprintln(out, "")

			return nil
		},
	}
	cmd.Flags().StringVar(&tutorialID, "tutorial", "", "Tutorial ID (built-in for now)")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "Output format: table or json")
	return cmd
}

// ----------------------------------------------------------------------------
// tutorial status
// ----------------------------------------------------------------------------

func newTutorialStatusCmd() *cobra.Command {
	var tutorialID string
	var outputFormat string // json|table
	cmd := &cobra.Command{
		Use:     "status",
		Short:   "Show progress for a tutorial (state per step)",
		Args:    cobra.NoArgs,
		SilenceUsage: true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Reuse same built-in tutorial
			const basicLinuxTut = `{
  "id": "basic-linux",
  "title": "Basic Linux Operations",
  "steps": [
    {"id": "intro", "title": "Introduction", "instruction": "Read this section.", "validator_type": "always_pass"},
    {"id": "ls", "title": "List Files", "instruction": "Run ls -la and check directory contents.", "prerequisites": ["intro"], "validator_type": "command_output", "validator_params": {"command": "ls -la", "pattern": "total.*"}}
  ]
}`

			tut, err := tutorial.LoadTutorialJSON([]byte(basicLinuxTut))
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%sFailed to load tutorial: %v\n", ERROR(), err)
				return err
			}

			progress, err := tutorial.NewProgress(tut)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%sFailed to initialize progress: %v\n", ERROR(), err)
				return err
			}

			if outputFormat == "json" {
				type StepStatus struct {
					ID          string `json:"step_id"`
					Title       string `json:"title"`
					State       string `json:"state"`
					CompletedAt string `json:"completed_at,omitempty"`
				}
				out := make([]StepStatus, 0, len(tut.Steps))
				for _, s := range tut.Steps {
					st, err := progress.State(s.ID)
					if err != nil {
						continue
					}
					ss := StepStatus{ID: s.ID, Title: s.Title, State: string(st)}
					ts, ok := progress.CompletedAt(s.ID)
					if ok {
						ss.CompletedAt = ts.Format("2006-01-02T15:04:05Z07:00")
					}
					out = append(out, ss)
				}
				w := cmd.OutOrStdout()
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 72))
			fmt.Fprintln(out, "  cafctl tutorial status · progress snapshot")
			fmt.Fprintln(out, Separator('═', 72))
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "Tutorial: %s (%s)\n", tut.ID, tut.Title)

			done, total := progress.CompletedCount()
			fmt.Fprintf(out, "Progress: %d/%d completed (%.1f%%)\n", done, total, float64(done)/float64(total)*100)

			fmt.Fprintln(out, "")
			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "STEP_ID\tSTATE\tCOMPLETED_AT\tAVAILABLE")
			for _, s := range tut.Steps {
				st, _ := progress.State(s.ID)
				ts, ok := progress.CompletedAt(s.ID)
				tsStr := "-"
				if ok {
					tsStr = ts.Format("2006-01-02 15:04:05")
				}
				canEnter, _ := progress.CanEnter(s.ID)
				avail := "no"
				if canEnter && st != tutorial.StateCompleted {
					avail = "yes"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.ID, st, tsStr, avail)
			}
			w.Flush()

			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "  States:")
			fmt.Fprintln(out, "    not_started — Initial state; prerequisite gating applies.")
			fmt.Fprintln(out, "    in_progress — User has entered the step.")
			fmt.Fprintln(out, "    completed — Validator passed; unlocks dependent steps.")
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, "  Available — Can Enter immediately without blocking by prerequisites.")
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringVar(&tutorialID, "tutorial", "", "Tutorial ID (built-in for now)")
	cmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "Output format: table or json")
	return cmd
}

// ----------------------------------------------------------------------------
// tutorial verify
// ----------------------------------------------------------------------------

func newTutorialVerifyCmd() *cobra.Command {
	var publicKeyHex string // override cert's embedded public key
	cmd := &cobra.Command{
		Use:     "verify <certificate.json>",
		Short:   "Offline Ed25519 certificate verification (SHA-256 chain + signature)",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			certPath := args[0]
			f, err := os.Open(certPath)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%sCannot open certificate %q: %v\n", ERROR(), certPath, err)
				return err
			}
			defer f.Close()

			data, err := io.ReadAll(f)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%sCannot read certificate %q: %v\n", ERROR(), certPath, err)
				return err
			}

			var cert tutorial.Certificate
			if err := json.Unmarshal(data, &cert); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%sInvalid JSON in certificate: %v\n", ERROR(), err)
				return err
			}

			// Verify using embedded public key
			valid, err := tutorial.VerifyCertificate(&cert)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%sVerification failed: %v\n", ERROR(), err)
				return err
			}
			if !valid {
				fmt.Fprintln(cmd.OutOrStdout(), redBold.Sprint("✗ Certificate verification FAILED"))
				fmt.Fprintln(cmd.OutOrStdout(), "")
				fmt.Fprintln(cmd.OutOrStdout(), "  Reasons:")
				fmt.Fprintln(cmd.OutOrStdout(), "    • Tampering detected in any field (learner, time, steps, signature)")
				fmt.Fprintln(cmd.OutOrStdout(), "    • Invalid Ed25519 signature (wrong issuer key)")
				fmt.Fprintln(cmd.OutOrStdout(), "    • Malformed step hash chain")
				fmt.Fprintln(cmd.OutOrStdout(), "")
				return fmt.Errorf("certificate invalid")
			}

			// If override provided, verify against that key too
			if publicKeyHex != "" {
				pubBytes, err := decodeHex(publicKeyHex)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "%sInvalid hex public key: %v\n", ERROR(), err)
					return err
				}
				valid, err := tutorial.VerifyCertificateWithKey(&cert, pubBytes)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "%sKeyed verification failed: %v\n", ERROR(), err)
					return err
				}
				if !valid {
					fmt.Fprintln(cmd.OutOrStdout(), yellow.Sprint("⚠ Certificate does NOT match override public key"))
				} else {
					fmt.Fprintln(cmd.OutOrStdout(), greenBold.Sprint("✓ Also verified against override public key"))
				}
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 72))
			fmt.Fprintln(out, "  Certificate Verification Summary")
			fmt.Fprintln(out, Separator('═', 72))
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, greenBold.Sprint("✓ Signature valid"))
			fmt.Fprintln(out, "• Algorithm: Ed25519")
			fmt.Fprintln(out, "• Payload: SHA-256(canonical JSON excluding signature)")
			fmt.Fprintln(out, "• Chain: SHA-256 step hash chain in topological order")
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "Tutorial:      %s (%s)\n", cert.TutorialID, cert.TutorialTitle)
			fmt.Fprintf(out, "Learner ID:    %s\n", cert.LearnerID)
			fmt.Fprintf(out, "Completed At:  %s\n", cert.CompletedAt.Format("2006-01-02 15:04:05 MST"))
			fmt.Fprintf(out, "Public Key:    %.16s...\n", cert.PublicKey[:8])
			fmt.Fprintf(out, "Signature:     %.16s...\n", cert.Signature[:8])
			fmt.Fprintf(out, "Step Chain:    %d entries\n", len(cert.StepHashChain))
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, greenBold.Sprint("✓ Certificate is authentic — verifiable offline without network/DB"))
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringVar(&publicKeyHex, "override-pubkey", "", "Override public key in hex for custom issuer (optional)")
	return cmd
}

// Helper: decode hex string to bytes
func decodeHex(s string) ([]byte, error) {
	s = strings.TrimPrefix(s, "0x")
	return hex.DecodeString(s)
}
