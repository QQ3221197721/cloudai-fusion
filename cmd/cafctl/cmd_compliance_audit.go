// Package main - cafctl compliance subcommands (M36 Compliance Audit).
//
// These commands surface compliance audit capabilities:
//
//   - compliance audit (M36) — runs a compliance audit against a chosen framework
//     (SOC2, ISO27001, GDPR, PCI-DSS) and reports rule pass/fail/warn status.
//
// All operations are local and deterministic; no network calls are performed.
package main

import (
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/compliance"
	"github.com/spf13/cobra"
)

func newComplianceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compliance",
		Short: "Compliance audit operations (SOC2, ISO27001, GDPR, PCI-DSS)",
	}
	cmd.AddCommand(newComplianceAuditCmd())
	return cmd
}

// newComplianceAuditCmd implements `cafctl compliance audit`
func newComplianceAuditCmd() *cobra.Command {
	var framework string
	var listOnly bool

	cmd := &cobra.Command{
		Use:   "audit [--framework <name>] [--list]",
		Short: "Run a compliance audit against a framework",
		Args:  cobra.NoArgs,
		Example: `  cafctl compliance audit
  cafctl compliance audit --framework SOC2
  cafctl compliance audit --list`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl compliance audit · compliance audit engine (M36)")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			auditor := compliance.NewAuditor()

			// List-only mode: just list the compliance rules catalog
			if listOnly {
				fmt.Fprintln(out, "Supported Frameworks:")
				for _, fw := range auditor.Frameworks() {
					fmt.Fprintf(out, "  • %s\n", fw)
				}
				fmt.Fprintln(out, "")

				rules := auditor.ListRules(framework)
				fmt.Fprintf(out, "Compliance Rules Catalog (%d rules):\n", len(rules))
				fmt.Fprintln(out, "")
				for _, r := range rules {
					fmt.Fprintf(out, "  [%s] %s\n", r.ID, r.Category)
					fmt.Fprintf(out, "      %s\n", r.Description)
					fmt.Fprintf(out, "      Severity: %s\n", formatSeverity(r.Severity))
					fmt.Fprintln(out, "")
				}
				fmt.Fprintf(out, "%s Rule listing complete.\n", OK())
				fmt.Fprintln(out, "")
				return nil
			}

			// Run the audit
			if framework == "" {
				framework = "all"
			}
			result := auditor.Audit(framework)

			fmt.Fprintln(out, "Audit Configuration:")
			fmt.Fprintf(out, "  Framework: %s\n", framework)
			fmt.Fprintf(out, "  Total Rules: %d\n", result.TotalRules)
			fmt.Fprintln(out, "")

			fmt.Fprintln(out, "Rule Evaluation:")
			for _, r := range result.Rules {
				statusSym := formatComplianceStatus(r.Status)
				fmt.Fprintf(out, "  %-14s %s [%s] %s\n", statusSym, r.ID, r.Framework, r.Category)
			}
			fmt.Fprintln(out, "")

			fmt.Fprintln(out, "Audit Summary:")
			fmt.Fprintf(out, "  ✓ Passed:   %d\n", result.Passed)
			fmt.Fprintf(out, "  ⚠ Warnings: %d\n", result.Warnings)
			fmt.Fprintf(out, "  ✗ Failed:   %d\n", result.Failed)
			fmt.Fprintln(out, "")

			// Compute compliance score
			var score float64
			if result.TotalRules > 0 {
				score = float64(result.Passed) / float64(result.TotalRules) * 100
			}
			fmt.Fprintf(out, "  Compliance Score: %.1f%%\n", score)
			fmt.Fprintln(out, "")

			if result.Failed == 0 {
				fmt.Fprintf(out, "%s Audit complete: no critical failures.\n", OK())
			} else {
				fmt.Fprintf(out, "%s Audit complete: %d rule(s) failed, remediation required.\n", ERROR(), result.Failed)
			}
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringVar(&framework, "framework", "", "Compliance framework: SOC2, ISO27001, GDPR, PCI-DSS (default: all)")
	cmd.Flags().BoolVar(&listOnly, "list", false, "List compliance rules without running audit")
	return cmd
}

func formatSeverity(sev string) string {
	switch sev {
	case "high":
		return redBold.Sprint("HIGH")
	case "medium":
		return yellowBold.Sprint("MEDIUM")
	case "low":
		return cyan.Sprint("LOW")
	default:
		return sev
	}
}

func formatComplianceStatus(status string) string {
	switch status {
	case "pass":
		return greenBold.Sprint("✓ PASS")
	case "fail":
		return redBold.Sprint("✗ FAIL")
	case "warn":
		return yellowBold.Sprint("⚠ WARN")
	default:
		return status
	}
}
