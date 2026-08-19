// Package main - cafctl gen subcommands (M40 API Client Generator + M43 Doc Generator).
//
// These commands surface real, offline, in-memory generator capabilities:
//
//   - gen client (M40, pkg/apiclientgen) — parses an OpenAPI/Swagger spec via the real
//     apiclientgen.GenerateFromSpec pipeline and emits idiomatic HTTP clients for
//     Go / TypeScript / Python, reporting the generated files deterministically.
//   - gen docs   (M43, pkg/docgen)       — parses a real Go package directory via the
//     docgen.ParseDir AST walker and renders a deterministic Markdown API summary.
//
// Both are read-only, deterministic, and require no network access. gen docs reads a
// Go source directory (read-only); gen client reads a spec file or a built-in demo spec.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/apiclientgen"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/docgen"
	"github.com/spf13/cobra"
)

// ----------------------------------------------------------------------------
// gen (parent)
// ----------------------------------------------------------------------------

func newGenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gen",
		Short: "Generate code artifacts from specifications (API clients, docs)",
	}
	cmd.AddCommand(newGenClientCmd())
	cmd.AddCommand(newGenDocsCmd())
	return cmd
}

// ----------------------------------------------------------------------------
// gen client (M40) — API client generator
// ----------------------------------------------------------------------------

// newGenClientCmd generates a typed HTTP client from an OpenAPI/Swagger spec via
// the real apiclientgen.GenerateFromSpec pipeline. With no --spec flag it uses a
// built-in demo spec so the command is instantly runnable and deterministic.
func newGenClientCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "client",
		Short:         "Generate an HTTP client from an OpenAPI/Swagger spec (offline)",
		Args:          cobra.NoArgs,
		Example:       "  cafctl gen client\n  cafctl gen client --lang typescript --pkg petstore\n  cafctl gen client --spec ./openapi.yaml --lang go",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			specFile, _ := cmd.Flags().GetString("spec")
			lang, _ := cmd.Flags().GetString("lang")
			pkgName, _ := cmd.Flags().GetString("pkg")

			specData := demoOpenAPISpec()
			source := "built-in demo spec"
			if specFile != "" {
				data, err := os.ReadFile(specFile)
				if err != nil {
					return fmt.Errorf("read spec file %q: %w", specFile, err)
				}
				specData = data
				source = specFile
			}

			files, err := apiclientgen.GenerateFromSpec(specData, lang, pkgName)
			if err != nil {
				return fmt.Errorf("generate client: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl gen client · API client generator (M40)")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			fmt.Fprintf(out, "Language:  %s\n", lang)
			fmt.Fprintf(out, "Package:   %s\n", pkgOrDefault(pkgName, lang))
			fmt.Fprintf(out, "Spec:      %s\n", source)
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
	cmd.Flags().String("spec", "", "OpenAPI/Swagger spec file (JSON or YAML); default uses a built-in demo spec")
	cmd.Flags().StringP("lang", "l", "go", "Target language: go, typescript, python")
	cmd.Flags().StringP("pkg", "p", "", "Generated package name (default per-language)")
	return cmd
}

// ----------------------------------------------------------------------------
// gen docs (M43) — documentation generator
// ----------------------------------------------------------------------------

// newGenDocsCmd parses a real Go package directory via docgen.ParseDir and prints
// a deterministic Markdown API summary. Defaults to ./pkg/docgen so the command is
// runnable from the repo root; use --dir to document any other package.
func newGenDocsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "docs",
		Short:         "Generate Markdown API docs from a Go package (offline)",
		Args:          cobra.NoArgs,
		Example:       "  cafctl gen docs\n  cafctl gen docs --dir ./pkg/docgen --title 'DocGen Reference'",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, _ := cmd.Flags().GetString("dir")
			title, _ := cmd.Flags().GetString("title")
			if dir == "" {
				dir = "./pkg/docgen"
			}

			pkg, err := docgen.ParseDir(dir)
			if err != nil {
				return fmt.Errorf("parse package %q: %w", dir, err)
			}
			if title == "" {
				title = pkg.Name + " Documentation"
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl gen docs · documentation generator (M43)")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			// Markdown header block (deterministic — no timestamps).
			fmt.Fprintf(out, "# %s\n\n", title)
			fmt.Fprintf(out, "Package: `%s`\n\n", pkg.Name)
			if pkg.Doc != "" {
				fmt.Fprintf(out, "%s\n\n", firstSentence(pkg.Doc))
			}
			fmt.Fprintln(out, "## Summary")
			fmt.Fprintf(out, "- Functions:  %d\n", len(pkg.Funcs))
			fmt.Fprintf(out, "- Types:      %d\n", len(pkg.Types))
			fmt.Fprintf(out, "- Constants:  %d\n", len(pkg.Consts))
			fmt.Fprintf(out, "- Variables:  %d\n", len(pkg.Vars))
			fmt.Fprintf(out, "- Total symbols: %d\n", pkg.SymbolCount())
			fmt.Fprintln(out, "")

			if len(pkg.Funcs) > 0 {
				fmt.Fprintln(out, "## Functions")
				for _, f := range pkg.Funcs {
					fmt.Fprintf(out, "### %s\n", f.Name)
					fmt.Fprintf(out, "`%s`\n", f.Signature)
					if syn := f.Synopsis(); syn != "" {
						fmt.Fprintf(out, "%s\n", syn)
					}
					fmt.Fprintln(out, "")
				}
			}

			if len(pkg.Types) > 0 {
				fmt.Fprintln(out, "## Types")
				for _, t := range pkg.Types {
					fmt.Fprintf(out, "### %s (%s)\n", t.Name, t.Kind)
					if syn := t.Synopsis(); syn != "" {
						fmt.Fprintf(out, "%s\n", syn)
					}
					if len(t.Fields) > 0 {
						fmt.Fprintf(out, "- fields: %d\n", len(t.Fields))
					}
					if len(t.Methods) > 0 {
						fmt.Fprintf(out, "- methods: %d\n", len(t.Methods))
					}
					fmt.Fprintln(out, "")
				}
			}

			fmt.Fprintf(out, "%s Documentation generated — parsed %d symbols from package %q.\n", OK(), pkg.SymbolCount(), pkg.Name)
			fmt.Fprintln(out, "")
			return nil
		},
	}
	cmd.Flags().StringP("dir", "d", "", "Go package directory to document (default ./pkg/docgen)")
	cmd.Flags().StringP("title", "t", "", "Document title (default derived from package name)")
	return cmd
}

// ----------------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------------

func pkgOrDefault(pkgName, lang string) string {
	if pkgName != "" {
		return pkgName
	}
	switch lang {
	case "go":
		return "apiclient (default)"
	default:
		return "(language default)"
	}
}

// previewLines returns up to n lines of s, truncating long lines for a compact preview.
func previewLines(s string, n int) []string {
	raw := strings.Split(s, "\n")
	if len(raw) > n {
		raw = raw[:n]
	}
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		if len(line) > 60 {
			line = line[:57] + "..."
		}
		out = append(out, line)
	}
	return out
}

// firstSentence returns the first sentence of a doc block for a compact summary.
func firstSentence(doc string) string {
	doc = strings.TrimSpace(doc)
	if idx := strings.Index(doc, ". "); idx >= 0 {
		return doc[:idx+1]
	}
	if idx := strings.IndexByte(doc, '\n'); idx >= 0 {
		return strings.TrimSpace(doc[:idx])
	}
	return doc
}

// demoOpenAPISpec returns a small, valid OpenAPI 3.0 document used when no --spec
// is supplied, so `gen client` is instantly runnable and produces deterministic output.
func demoOpenAPISpec() []byte {
	return []byte(`{
  "openapi": "3.0.3",
  "info": {"title": "Demo API", "version": "1.0.0"},
  "paths": {
    "/items": {
      "get": {
        "operationId": "listItems",
        "summary": "List all items",
        "responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/ItemList"}}}}}
      }
    },
    "/items/{id}": {
      "get": {
        "operationId": "getItem",
        "summary": "Get an item by ID",
        "parameters": [{"name": "id", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/Item"}}}}}
      }
    }
  },
  "components": {
    "schemas": {
      "Item": {
        "type": "object",
        "properties": {"id": {"type": "string"}, "name": {"type": "string"}, "price": {"type": "number"}},
        "required": ["id", "name"]
      },
      "ItemList": {
        "type": "object",
        "properties": {"items": {"type": "array", "items": {"$ref": "#/components/schemas/Item"}}, "total": {"type": "integer", "format": "int32"}},
        "required": ["items", "total"]
      }
    }
  }
}`)
}
