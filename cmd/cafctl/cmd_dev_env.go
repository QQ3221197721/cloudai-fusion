// Package main - cafctl dev subcommands (M41 Local Development Environment).
//
// These commands manage a local development environment (mock mode):
//
//   - dev env start (M41) — starts a local development environment with the
//     required components (API server, Redis, Postgres, monitoring stack).
//     In mock mode, it reports what would be started without actual container ops.
//
// All operations are local; the stub backend does not require Docker or network.
package main

import (
	"fmt"
	"sort"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/devenv"
	"github.com/spf13/cobra"
)

func newDevCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Local development environment operations",
	}
	cmd.AddCommand(newDevEnvCmd())
	return cmd
}

func newDevEnvCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage local development environments",
	}
	cmd.AddCommand(newDevEnvStartCmd())
	cmd.AddCommand(newDevEnvListCmd())
	cmd.AddCommand(newDevEnvStatusCmd())
	return cmd
}

// newDevEnvStartCmd implements `cafctl dev env start`
func newDevEnvStartCmd() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "start [--name <env-name>]",
		Short: "Start a local development environment",
		Args:  cobra.NoArgs,
		Example: `  cafctl dev env start
  cafctl dev env start --name local-dev`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl dev env start · local development environment (M41)")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			starter := devenv.NewStarter()
			env, err := starter.Start(name)
			if err != nil {
				return fmt.Errorf("start environment: %w", err)
			}

			fmt.Fprintln(out, "Environment Configuration:")
			fmt.Fprintf(out, "  Name:    %s\n", env.Name)
			fmt.Fprintf(out, "  Status:  %s\n", formatEnvStatus(env.Status))
			fmt.Fprintln(out, "")

			fmt.Fprintln(out, "Components:")
			for _, comp := range env.Components {
				fmt.Fprintf(out, "  ✓ %s\n", comp)
			}
			fmt.Fprintln(out, "")

			fmt.Fprintln(out, "Service Endpoints:")
			// Sort port keys for deterministic output
			keys := make([]string, 0, len(env.Ports))
			for k := range env.Ports {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(out, "  %-12s http://localhost:%d\n", k+":", env.Ports[k])
			}
			fmt.Fprintln(out, "")

			fmt.Fprintln(out, "[MOCK MODE] No actual containers were started.")
			fmt.Fprintln(out, "In production, this would launch the components via docker-compose.")
			fmt.Fprintln(out, "")

			fmt.Fprintf(out, "%s Development environment %q ready.\n", OK(), env.Name)
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Environment name (default: 'default')")
	return cmd
}

// newDevEnvListCmd implements `cafctl dev env list`
func newDevEnvListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "list",
		Short:         "List available development environments",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl dev env list · available environments (M41)")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			starter := devenv.NewStarter()
			envs, err := starter.List()
			if err != nil {
				return fmt.Errorf("list environments: %w", err)
			}

			if len(envs) == 0 {
				fmt.Fprintln(out, "No development environments found.")
				fmt.Fprintln(out, "")
				return nil
			}

			fmt.Fprintf(out, "Environments (%d total):\n", len(envs))
			fmt.Fprintln(out, "")
			for _, env := range envs {
				fmt.Fprintf(out, "  %-14s %s (%d components)\n",
					env.Name, formatEnvStatus(env.Status), len(env.Components))
			}
			fmt.Fprintln(out, "")

			fmt.Fprintf(out, "%s List complete.\n", OK())
			fmt.Fprintln(out, "")
			return nil
		},
	}
	return cmd
}

// newDevEnvStatusCmd implements `cafctl dev env status`
func newDevEnvStatusCmd() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "status [--name <env-name>]",
		Short: "Show status of a development environment",
		Args:  cobra.NoArgs,
		Example: `  cafctl dev env status
  cafctl dev env status --name local-dev`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl dev env status · environment status (M41)")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			starter := devenv.NewStarter()
			env, err := starter.Status(name)
			if err != nil {
				return fmt.Errorf("get status: %w", err)
			}

			fmt.Fprintf(out, "  Name:    %s\n", env.Name)
			fmt.Fprintf(out, "  Status:  %s\n", formatEnvStatus(env.Status))
			fmt.Fprintf(out, "  Components: %d active\n", len(env.Components))
			fmt.Fprintln(out, "")

			fmt.Fprintf(out, "%s Status check complete.\n", OK())
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Environment name (default: 'default')")
	return cmd
}

func formatEnvStatus(status string) string {
	switch status {
	case "running":
		return greenBold.Sprint("● running")
	case "stopped":
		return redBold.Sprint("○ stopped")
	default:
		return status
	}
}
