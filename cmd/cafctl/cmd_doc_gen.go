// Package main - cafctl doc subcommand with M43 Documentation Generator gen subcommand.
//
// This exposes real, offline documentation generation capability:
//
//   - doc gen (M43, pkg/docgen) — parses a real Go package directory via the
//     docgen.ParseAndGen pipeline (go/ast + go/doc walker) and writes deterministic
//     Markdown documentation files (index.md, types.md) to the output directory.
//
// Read-only against source (writes only into the chosen output directory) and
// requires no network access.
package main

import (
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/docgen"
	"github.com/spf13/cobra"
)

var docCmd = &cobra.Command{
	Use:   "doc",
	Short: "Documentation generation tools (M43)",
}

func init() {
	rootCmd.AddCommand(docCmd)
	docCmd.AddCommand(newDocGenCmd())
}

// newDocGenCmd parses a real Go package directory via docgen.ParseAndGen and writes
// Markdown documentation to the output directory. The package path is a positional arg.
func newDocGenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "gen",
		Short:         "Generate Markdown API docs from a Go package (offline)",
		Args:          cobra.ExactArgs(1),
		Example:       "  cafctl doc gen ./pkg/docgen -o ./docs\n  cafctl doc gen ./pkg/apiclientgen -o ./out --title 'API Client Gen Reference'",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			pkgPath := args[0]
			outputDir, _ := cmd.Flags().GetString("output")
			title, _ := cmd.Flags().GetString("title")

			pkg, err := docgen.ParseAndGen(pkgPath, outputDir, title)
			if err != nil {
				return fmt.Errorf("generate docs for %q: %w", pkgPath, err)
			}

			resolvedTitle := title
			if resolvedTitle == "" {
				resolvedTitle = pkg.Name + " Documentation"
			}
			resolvedOut := outputDir
			if resolvedOut == "" {
				resolvedOut = "./docs"
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl doc gen · documentation generator (M43)")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			fmt.Fprintf(out, "Package:   %s\n", pkgPath)
			fmt.Fprintf(out, "Title:     %s\n", resolvedTitle)
			fmt.Fprintf(out, "Output:    %s\n", resolvedOut)
			fmt.Fprintln(out, "")

			fmt.Fprintln(out, "## Summary")
			fmt.Fprintf(out, "- Functions:  %d\n", len(pkg.Funcs))
			fmt.Fprintf(out, "- Types:      %d\n", len(pkg.Types))
			fmt.Fprintf(out, "- Constants:  %d\n", len(pkg.Consts))
			fmt.Fprintf(out, "- Variables:  %d\n", len(pkg.Vars))
			fmt.Fprintf(out, "- Total symbols: %d\n", pkg.SymbolCount())
			fmt.Fprintln(out, "")

			fmt.Fprintf(out, "%s Documentation generated — %d symbols from package %q written to %s (index.md, types.md).\n",
				OK(), pkg.SymbolCount(), pkg.Name, resolvedOut)
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringP("output", "o", "./docs", "Output directory for generated docs")
	cmd.Flags().StringP("title", "t", "", "Document title (default derived from package name)")
	return cmd
}
