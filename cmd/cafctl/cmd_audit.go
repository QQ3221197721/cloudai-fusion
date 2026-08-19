// Package main - cafctl audit subcommand
// Provides safe read-only access to audit event inspection functionality
package main

import (
	"context"
	"fmt"
	"text/tabwriter"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/audit"
	"github.com/spf13/cobra"
)

func newAuditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Audit management commands",
		Long: `Audit inspection commands for querying events without network access.`,
	}
	cmd.AddCommand(
		newAuditExportCmd(),
		newAuditQueryCmd(),
	)
	return cmd
}

// newAuditExportCmd exports audit events from an in-memory manager
func newAuditExportCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:     "export",
		Short:   "Export recent audit events (in-memory demo)",
		Args:    cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := audit.NewManager(audit.ManagerConfig{MaxEvents: 100})
			ctx := context.Background()
			
			// Record demo events
			events := []*audit.AuditEvent{
				{Action: "login", Resource: "user", Result: "success", Category: audit.CategoryAuth},
				{Action: "create_workload", Resource: "workload", Result: "success", Category: audit.CategoryWorkload},
				{Action: "delete_cluster", Resource: "cluster", Result: "failure", Category: audit.CategoryAdmin},
				{Action: "update_policy", Resource: "policy", Result: "success", Category: audit.CategoryConfig},
			}
			for _, e := range events {
				_ = mgr.RecordEvent(ctx, e)
			}
			
			all := mgr.QueryEvents(audit.EventFilter{Limit: limit})
			if len(all) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No events recorded.")
				return nil
			}
			
			out := cmd.OutOrStdout()
			w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "TIMESTAMP\tACTION\tRESOURCE\tRESULT\tCATEGORY")
			for _, e := range all {
				ts := e.Timestamp.Format("2006-01-02 15:04:05")
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", ts, e.Action, e.Resource, e.Result, e.Category)
			}
			w.Flush()
			return nil
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "l", 20, "Maximum events to export")
	return cmd
}

// newAuditQueryCmd queries audit events by filter
func newAuditQueryCmd() *cobra.Command {
	var category string
	cmd := &cobra.Command{
		Use:     "query [--category <cat>] [--limit N]",
		Short:   "Query audit events by category",
		Example: "  cafctl audit query --category authentication --limit 10",
		Args:    cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := audit.NewManager(audit.ManagerConfig{MaxEvents: 100})
			
			filter := audit.EventFilter{}
			if category != "" {
				filter.Category = audit.EventCategory(category)
			}
			
			events := mgr.QueryEvents(filter)
			count := mgr.GetEventCount()
			
			fmt.Fprintf(cmd.OutOrStdout(), "Total events: %d, matching: %d\n", count, len(events))
			for i, e := range events {
				if i >= 10 { break }
				fmt.Fprintf(cmd.OutOrStdout(), "  #%d %s [%s] %s\n", i+1, e.Action, e.Category, e.Result)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&category, "category", "c", "", "Event category filter")
	return cmd
}
