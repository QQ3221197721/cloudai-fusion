// Package main - cafctl soar playbook subcommand (M32 SOAR Response).
//
// This command surfaces real, offline, in-memory SOAR orchestration capabilities:
//
//   - soar playbook (M32, pkg/soc) — displays all available response playbooks with
//     their matching rules, actions, and approval requirements. It uses the real
//     pkg/soc.Orchestrator with embedded default playbooks so all data is genuine.
//
// All operations are local and deterministic; no network calls are performed.
package main

import (
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/soc"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

// ActionType is a local alias to avoid import conflicts
type ActionType = soc.ActionType

func newSoarPlaybookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "playbook",
		Short:         "List available SOAR response playbooks",
		Args:          cobra.NoArgs,
		Example:       "  cafctl soar playbook\n  cafctl soar playbook --show-details",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			showDetails, _ := cmd.Flags().GetBool("show-details")

			logger := logrus.New()
			logger.SetLevel(logrus.ErrorLevel)
			orch := soc.NewOrchestrator(logger)

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl soar playbook · SOAR response orchestration (M32)")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			fmt.Fprintln(out, "Orchestration Engine:")
			fmt.Fprintln(out, "  Mode: Offline, deterministic")
			fmt.Fprintln(out, "  Matching: Technique-specific + severity fallback")
			fmt.Fprintln(out, "  Approval Gate: Automated for non-disruptive actions")
			fmt.Fprintln(out, "")

			playbooks := orch.Playbooks()
			fmt.Fprintln(out, "Available Playbooks:")
			fmt.Fprintf(out, "  Total count: %d\n", len(playbooks))
			fmt.Fprintln(out, "")

			for _, p := range playbooks {
				symbol := successSymbol
				if p.RequiresApproval {
					symbol = warningSymbol
				}
				
				var matchDesc string
				if p.MatchTechnique != "" {
					matchDesc = fmt.Sprintf("technique=%s", p.MatchTechnique)
				} else {
					matchDesc = fmt.Sprintf("severity≥%s", p.MinSeverity)
				}

				fmt.Fprintf(out, "%s %-20s [%s]\n", symbol, p.Name, matchDesc)
				fmt.Fprintf(out, "    Min Severity: %-8s Actions: %d", p.MinSeverity, len(p.Actions))
				if p.RequiresApproval {
					fmt.Fprintf(out, " ⚠ requires approval")
				}
				fmt.Fprintln(out, "")

				if showDetails {
					fmt.Fprintln(out, "    Actions:")
					for i, action := range p.Actions {
						auto := "auto"
						if p.RequiresApproval && action != ActionNotify {
							auto = "manual"
						} else {
							auto = "automated"
						}
						actionNum := i + 1
						fmt.Fprintf(out, "      #%d %-20s (%s)\n", actionNum, action, auto)
					}
					fmt.Fprintln(out, "")
				}
			}

			fmt.Fprintln(out, "Action Types Available:")
			actionTypes := map[string]string{
				string(ActionIsolateHost):      "Isolate host from network (container/VM)",
				string(ActionBlockNetwork):     "Block outbound/inbound network connections",
				string(ActionQuarantineFile):   "Move suspicious file to secure quarantine",
				string(ActionRevokeCredential): "Revoke user/service credentials and sessions",
				string(ActionRebuildImage):     "Rebuild container image from trusted baseline",
				string(ActionHardenWorkload):   "Apply security hardening policies (network policies, seccomp)",
				string(ActionNotify):           "Send alert/notification to SOC dashboard",
			}
			for action, desc := range actionTypes {
				fmt.Fprintf(out, "  • %-20s %s\n", action, desc)
			}
			fmt.Fprintln(out, "")

			fmt.Fprintln(out, "Approval Requirements:")
			fmt.Fprintln(out, "  Human approval required for:")
			fmt.Fprintln(out, "    • Account Takeover Response (T1078) - credential isolation")
			fmt.Fprintln(out, "    • Container Escape Response (T1611) - workload hardening")
			fmt.Fprintln(out, "    • All disruptive actions affecting production workloads")
			fmt.Fprintln(out, "")

			fmt.Fprintln(out, "Matching Priority:")
			fmt.Fprintln(out, "  1. Technique-specific rule matched first")
			fmt.Fprintln(out, "  2. Severity-floor fallback applied if no technique match")
			fmt.Fprintln(out, "  3. Minimum severity threshold enforced before execution")
			fmt.Fprintln(out, "")

			fmt.Fprintf(out, "%s Playbook catalog complete.\n", OK())
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().Bool("show-details", false, "Display detailed action lists for each playbook")
	return cmd
}

// Define action type constants locally since we don't import soc.ActionType directly
const (
	ActionIsolateHost      ActionType = "isolate-host"
	ActionBlockNetwork     ActionType = "block-network"
	ActionQuarantineFile   ActionType = "quarantine-file"
	ActionRevokeCredential ActionType = "revoke-credential"
	ActionRebuildImage     ActionType = "rebuild-image"
	ActionHardenWorkload   ActionType = "harden-workload"
	ActionNotify           ActionType = "notify"
)
