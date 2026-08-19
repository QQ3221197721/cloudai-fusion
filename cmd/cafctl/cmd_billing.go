// Package main - cafctl billing subcommand
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newBillingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "billing",
		Short: "Billing — resource pricing & usage (offline)",
	}
	cmd.AddCommand(newBillingUsageCmd())
	return cmd
}

// newBillingUsageCmd displays the default resource pricing model.
func newBillingUsageCmd() *cobra.Command {
	var resource string
	cmd := &cobra.Command{
		Use:           "usage [--resource compute|storage|bandwidth|gpu]",
		Short:         "Show resource usage statistics & pricing",
		Example:       "  cafctl billing usage\n  cafctl billing usage --resource gpu",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl billing usage · resource pricing & estimates")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			type row struct{ name, price string }
			rows := []row{
				{"compute", "$0.05/vCPU-hour"},
				{"storage", "$0.10/GB-month (tiered discount available)"},
				{"bandwidth", "$0.09/GB-transferred"},
				{"gpu", "$1.00/GPU-hour"},
			}
			fmt.Fprintln(out, "Available resources:")
			shown := 0
			for _, r := range rows {
				if resource != "" && r.name != resource {
					continue
				}
				fmt.Fprintf(out, "  • %-10s: %s\n", r.name, r.price)
				shown++
			}
			if shown == 0 {
				fmt.Fprintf(out, "  (no resource named %q)\n", resource)
			}
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringVar(&resource, "resource", "", "Resource type filter")
	return cmd
}
