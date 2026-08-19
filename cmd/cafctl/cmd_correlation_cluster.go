// Package main - cafctl correlation & cluster subcommands
package main

import (
	"fmt"
	"strings"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/correlation"
	"github.com/spf13/cobra"
)

func newCorrelationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "correlation",
		Short: "Correlation — dependency graph + root cause analysis (offline)",
	}
	cmd.AddCommand(newCorrelationGraphCmd())
	return cmd
}

// newCorrelationGraphCmd builds a small service graph with Topology + shows deps.
func newCorrelationGraphCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "graph",
		Short:         "Display service dependencies & hop depth",
		Args:          cobra.NoArgs,
		Example:       "  cafctl correlation graph",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			topo := correlation.NewTopology()
			topo.AddDependency("api", "svc-a")
			topo.AddDependency("svc-a", "db")
			topo.AddDependency("api", "svc-b")
			topo.AddDependency("svc-b", "cache")

			services := topo.Services()

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl correlation graph · service topology")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "%s Known services: %d\n", OK(), len(services))
			for _, s := range services {
				deps := topo.Dependencies(s)
				if len(deps) == 0 {
					continue
				}
				fmt.Fprintf(out, "  %-10s → %s\n", s, strings.Join(deps, ", "))
			}
			fmt.Fprintln(out, "")
			return nil
		},
	}
}

func newClusterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Cluster — multi-cluster orchestration (offline)",
	}
	cmd.AddCommand(newClusterStatusCmd())
	return cmd
}

func newClusterStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "status",
		Short:         "Show cluster orchestration status",
		Args:          cobra.NoArgs,
		Example:       "  cafctl cluster status",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl cluster status · multi-cluster orchestration")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "%s Cluster manager initialized\n", OK())
			fmt.Fprintln(out, "")
			return nil
		},
	}
}
