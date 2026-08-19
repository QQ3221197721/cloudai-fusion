// Package main - cafctl controller, store, mesh & cache subcommands
package main

import (
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/controller"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/store"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/mesh"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/cache"
	"github.com/spf13/cobra"
)

func newControllerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "controller",
		Short: "Controller — reconciliation & queue inspector (offline)",
	}
	cmd.AddCommand(newControllerQueueCmd())
	return cmd
}

func newControllerQueueCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "queue",
		Short:         "Show controller work queue status",
		Args:          cobra.NoArgs,
		Example:       "  cafctl controller queue",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = controller.NewWorkQueue()
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl controller queue · reconciliation")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "%s Work queue initialized\n", OK())
			fmt.Fprintln(out, "")
			return nil
		},
	}
}

func newStoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "store",
		Short: "Store — query optimizer + sharding stats (offline)",
	}
	cmd.AddCommand(newStoreStatsCmd())
	return cmd
}

func newStoreStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "stats",
		Short:         "Show store statistics & query optimizer stats",
		Args:          cobra.NoArgs,
		Example:       "  cafctl store stats",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = store.NewQueryPredictor()
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl store stats · data operations")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "%s Query predictor active\n", OK())
			fmt.Fprintln(out, "")
			return nil
		},
	}
}

func newMeshCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mesh",
		Short: "Mesh — service mesh routing & load balancing (offline)",
	}
	cmd.AddCommand(newMeshRoutesCmd())
	return cmd
}

func newMeshRoutesCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "routes",
		Short:         "Show mesh routing & load balancer status",
		Args:          cobra.NoArgs,
		Example:       "  cafctl mesh routes",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = mesh.NewRegistry()
			lb := mesh.NewRoundRobin()
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl mesh routes · service mesh")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "%s Endpoint registry created\n", OK())
			fmt.Fprintf(out, "%s Load balancer: round-robin\n", OK())
			fmt.Fprintf(out, "%s Strategy active: %s\n", OK(), lb.Name())
			fmt.Fprintln(out, "")
			return nil
		},
	}
}

func newCacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Cache — multi-level caching & optimization (offline)",
	}
	cmd.AddCommand(newCacheInfoCmd())
	return cmd
}

func newCacheInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "info",
		Short:         "Show adaptive TTL manager configuration",
		Args:          cobra.NoArgs,
		Example:       "  cafctl cache info",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			mgr := cache.NewAdaptiveTTLManager(cache.DefaultAdaptiveTTLConfig())
			defer mgr.Close()
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl cache info · caching system")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")
			fmt.Fprintf(out, "%s Adaptive TTL manager configured\n", OK())
			fmt.Fprintln(out, "")
			return nil
		},
	}
}
