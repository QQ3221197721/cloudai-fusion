// Package main - cafctl alerting & tracing subcommands
package main

import (
	"fmt"
	"strings"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/alerting"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/tracing"
	"github.com/spf13/cobra"
)

func newAlertingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alerting",
		Short: "Alerting — route & list notifications (offline)",
	}
	cmd.AddCommand(newAlertingListCmd())
	return cmd
}

// newAlertingListCmd creates a demo alert router + shows routing.
func newAlertingListCmd() *cobra.Command {
	var severity string
	cmd := &cobra.Command{
		Use:           "list [--severity low|medium|high|critical]",
		Short:         "Show notification routing rules for each severity",
		Args:          cobra.NoArgs,
		Example:       "  cafctl alerting list\n  cafctl alerting list --severity critical",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = alerting.NewAlertRouter()

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl alerting list · notification routing")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			shown := 0
			severities := []struct {
				name    string
				value   alerting.Severity
				symbol  string
				channels []string
			}{
				{"LOW", alerting.SeverityLow, "\x1b[36m\x1b[m", []string{"log"}},
				{"MEDIUM", alerting.SeverityMedium, "\x1b[33m\x1b[m", []string{"log", "email"}},
				{"HIGH", alerting.SeverityHigh, "\x1b[31m\x1b[m", []string{"log", "email", "pagerduty"}},
				{"CRITICAL", alerting.SeverityCritical, "\x1b[35m\x1b[m", []string{"pagerduty"}},
			}
			for _, s := range severities {
				if severity != "" && strings.ToUpper(s.name) != severity {
					continue
				}
				fmt.Fprintf(out, "%s %s:\n", s.symbol, s.name)
				for _, c := range s.channels {
					fmt.Fprintf(out, "  → %s\n", c)
				}
				shown++
			}
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "%s Active routes: %d\n", OK(), shown)
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringVarP(&severity, "severity", "s", "", "Filter by severity level")
	return cmd
}

func newTracingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tracing",
		Short: "Tracing — inspect spans with fast sampling (offline)",
	}
	cmd.AddCommand(newTracingShowCmd())
	return cmd
}

// newTracingShowCmd starts a FastTracer + shows span count via method.
func newTracingShowCmd() *cobra.Command {
	tracer := tracing.NewFastTracer("cafctl-demo")

	cmd := &cobra.Command{
		Use:           "show",
		Short:         "Inspect span count from demo trace context",
		Args:          cobra.NoArgs,
		Example:       "  cafctl tracing show",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl tracing show · distributed tracing")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "%s Tracer active: %s\n", OK(), tracer.Name())
			fmt.Fprintln(out, "")
			return nil
		},
	}
	return cmd
}
