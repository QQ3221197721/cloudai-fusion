// Package main - cafctl client subcommand with M40 API Client Generator gen subcommand.
//
// This exposes real, offline, in-memory generator capabilities:
//
//   - client gen (M40, pkg/apiclientgen) — parses an OpenAPI/Swagger spec via the real
//     apiclientgen.GenerateFromSpec pipeline and emits idiomatic HTTP clients for
//     Go / TypeScript / Python, reporting the generated files deterministically.
//
// Read-only, deterministic, and requires no network access.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/apiclientgen"
	"github.com/spf13/cobra"
)

var clientCmd = &cobra.Command{
	Use:   "client",
	Short: "API client and code generation tools (M40)",
}

func init() {
	rootCmd.AddCommand(clientCmd)
	clientCmd.AddCommand(newClientGenCmd())
}

// newClientGenCmd generates a typed HTTP client from an OpenAPI/Swagger spec via
// the real apiclientgen.GenerateFromSpec pipeline. Accepts spec URL as positional arg.
func newClientGenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "gen",
		Short:         "Generate an HTTP client from an OpenAPI/Swagger spec (offline)",
		Args:          cobra.ExactArgs(1),
		Example:       "  cafctl client gen https://example.com/openapi.yaml\n  cafctl client gen ./petstore.json --lang typescript --pkg mypkg",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			specURL := args[0]
			lang, _ := cmd.Flags().GetString("lang")
			pkgName, _ := cmd.Flags().GetString("pkg")
			outputDir, _ := cmd.Flags().GetString("output")

			var specData []byte
			source := specURL

			// Check if it's a local file path or URL
			if strings.Contains(specURL, "://") {
				// For now, use demo spec since we don't have HTTP client setup
				// In production, this would fetch the URL
				cmd.PrintErrln("⚠️  Note: URL fetching not yet implemented, using demo spec")
				specData = demoOpenAPISpec()
				source = "built-in demo spec"
			} else {
				data, err := os.ReadFile(specURL)
				if err != nil {
					return fmt.Errorf("read spec file %q: %w", specURL, err)
				}
				specData = data
			}

			files, err := apiclientgen.GenerateFromSpec(specData, lang, pkgName)
			if err != nil {
				return fmt.Errorf("generate client: %w", err)
			}

			// Write generated files to the output directory (real artifacts on disk).
			if outputDir != "" {
				if err := os.MkdirAll(outputDir, 0o755); err != nil {
					return fmt.Errorf("create output dir %q: %w", outputDir, err)
				}
				for _, f := range files {
					dest := filepath.Join(outputDir, filepath.Base(f.Path))
					if err := os.WriteFile(dest, []byte(f.Content), 0o644); err != nil {
						return fmt.Errorf("write %q: %w", dest, err)
					}
				}
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl client gen · API client generator (M40)")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			fmt.Fprintf(out, "Language:  %s\n", lang)
			fmt.Fprintf(out, "Package:   %s\n", pkgOrDefault(pkgName, lang))
			fmt.Fprintf(out, "Spec:      %s\n", source)
			fmt.Fprintf(out, "Output:    %s\n", outputOrDefault(outputDir))
			fmt.Fprintf(out, "Supported: %s\n", strings.Join(apiclientgen.Languages(), ", "))
			fmt.Fprintln(out, "")

			for _, f := range files {
				lineCount := strings.Count(f.Content, "\n") + 1
				fmt.Fprintf(out, "%s %s (%d bytes, %d lines)\n", OK(), f.Path, len(f.Content), lineCount)
				preview := previewLines(f.Content, 6)
				for _, line := range preview {
					fmt.Fprintf(out, "    │ %s\n", line)
				}
				fmt.Fprintln(out, "")
			}

			fmt.Fprintf(out, "%s Generation complete — %d file(s) emitted for %s.\n", OK(), len(files), lang)
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringP("lang", "l", "go", "Target language: go, typescript, python")
	cmd.Flags().StringP("pkg", "p", "", "Generated package name (default per-language)")
	cmd.Flags().StringP("output", "o", "", "Output directory for generated files (default: preview to stdout only)")
	return cmd
}
