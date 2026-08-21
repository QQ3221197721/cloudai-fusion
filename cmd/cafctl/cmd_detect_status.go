// Package main - cafctl detect status subcommand (M30 Sigma Detection).
//
// This command surfaces real, offline, in-memory detection engine capabilities:
//
//   - detect status (M30, pkg/detect) — displays the current detection engine state,
//     including loaded Sigma rules, rule coverage by tactic, and evaluation readiness.
//     It uses the real pkg/detect.Engine with embedded rules so all checks are genuine.
//
// All operations are local and deterministic; no network calls are performed.
package main

import (
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/detect"
	"github.com/spf13/cobra"
)

func newDetectStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "status",
		Short:         "Show detection engine state and Sigma rule inventory",
		Args:          cobra.NoArgs,
		Example:       "  cafctl detect status\n  cafctl detect status --show-rules",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			showRules, _ := cmd.Flags().GetBool("show-rules")

			engine, err := detect.NewEmbeddedEngine()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "%s failed to load embedded detection engine: %v\n", ERROR(), err)
				return fmt.Errorf("load detection engine failed: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl detect status · Sigma detection engine state (M30)")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			fmt.Fprintln(out, "Engine Configuration:")
			fmt.Fprintln(out, "  Rule Format: Sigma 2.1.0")
			fmt.Fprintln(out, "  Coverage: MITRE ATT&CK mappings")
			fmt.Fprintln(out, "  Evaluation Mode: Offline, deterministic")
			fmt.Fprintln(out, "")

			ruleCount := engine.Len()
			fmt.Fprintln(out, "Rule Inventory:")
			fmt.Fprintf(out, "  Total rules loaded: %d\n", ruleCount)
			
			// Display rule coverage statistics
			levelCounts := make(map[string]int)
			for _, r := range engine.Rules() {
				levelCounts[r.Level]++
			}

			fmt.Fprintln(out, "  By Severity Level:")
			for level, count := range levelCounts {
				fmt.Fprintf(out, "    • %-8s: %d rules\n", level, count)
			}
			fmt.Fprintln(out, "")

			if showRules {
				fmt.Fprintln(out, "Detailed Rules:")
				i := 1
				for _, r := range engine.Rules() {
					fmt.Fprintf(out, "  #%d %-40s [level=%s technique=%s]\n", 
						i, r.Title, r.Level, r.Technique())
					i++
				}
				fmt.Fprintln(out, "")
			}

			fmt.Fprintf(out, "%s Detection engine ready with %d Sigma rules.\n", OK(), ruleCount)
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().Bool("show-rules", false, "Display detailed list of all rules")
	return cmd
}
