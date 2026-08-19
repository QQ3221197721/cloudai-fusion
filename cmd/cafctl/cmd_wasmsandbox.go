// Package main - cafctl wasm subcommands (M50 WASM Engine + M51 Capability Security).
//
// These commands surface real, offline, in-memory WASM sandbox capabilities:
//
//   - wasm validate  (M50, pkg/wasm) — validates a synthetic Wasm binary via the
//     real ValidateWasmBinary parser, demonstrating magic number / version / section
//     scanning without network or filesystem dependencies.
//   - wasm caps      (M51, pkg/wasm) — evaluates the capability-based security model,
//     enumerating known escape vectors with their coverage status and demonstrating
//     grant evaluation for filesystem, network, and GPU access paths.
//
// Both are read-only, deterministic, and require no external dependencies.
package main

import (
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/wasm"
	"github.com/spf13/cobra"
)

// ----------------------------------------------------------------------------
// wasm (parent)
// ----------------------------------------------------------------------------

func newWasmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wasm",
		Short: "WASM Sandbox — validate modules & inspect capability grants (offline)",
	}
	cmd.AddCommand(newWasmValidateCmd())
	cmd.AddCommand(newWasmCapsCmd())
	return cmd
}

// ----------------------------------------------------------------------------
// wasm validate (M50) — Wasm binary validation engine
// ----------------------------------------------------------------------------

// newWasmValidateCmd validates a synthetic Wasm module through the real
// ValidateWasmBinary parser, showing section-level import/export scanning.
func newWasmValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "validate",
		Short:         "Validate a synthetic Wasm binary (magic, version, WASI imports)",
		Args:          cobra.NoArgs,
		Example:       "  cafctl wasm validate",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Construct a minimal valid Wasm module (8 bytes: magic + version 1).
			validModule := []byte{0x00, 0x61, 0x73, 0x6D, 0x01, 0x00, 0x00, 0x00}
			validResult := wasm.ValidateWasmBinary(validModule)

			// Construct an invalid binary (bad magic number).
			invalidModule := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x01, 0x00, 0x00, 0x00}
			invalidResult := wasm.ValidateWasmBinary(invalidModule)

			// Construct a too-small binary (< 8 bytes).
			tinyModule := []byte{0x00, 0x61}
			tinyResult := wasm.ValidateWasmBinary(tinyModule)

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl wasm validate · WASM binary validation engine")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			// Valid module
			fmt.Fprintf(out, "%s Valid MVP module (8 bytes):\n", OK())
			fmt.Fprintf(out, "    Valid:   %v\n", validResult.Valid)
			fmt.Fprintf(out, "    Version: %d\n", validResult.Version)
			fmt.Fprintf(out, "    Size:    %d bytes\n", validResult.Size)
			fmt.Fprintf(out, "    WASI:    %v\n", validResult.HasWASI)
			fmt.Fprintln(out, "")

			// Invalid magic
			fmt.Fprintf(out, "%s Invalid magic (0xDEADBEEF):\n", WARN())
			fmt.Fprintf(out, "    Valid:   %v\n", invalidResult.Valid)
			fmt.Fprintf(out, "    Error:   %s\n", invalidResult.ErrorMsg)
			fmt.Fprintln(out, "")

			// Too small
			fmt.Fprintf(out, "%s Too-small binary (2 bytes):\n", WARN())
			fmt.Fprintf(out, "    Valid:   %v\n", tinyResult.Valid)
			fmt.Fprintf(out, "    Error:   %s\n", tinyResult.ErrorMsg)
			fmt.Fprintln(out, "")

			fmt.Fprintf(out, "%s Validation engine operational — 3 test cases processed.\n", OK())
			fmt.Fprintln(out, "")
			return nil
		},
	}
	return cmd
}

// ----------------------------------------------------------------------------
// wasm caps (M51) — Capability-based security model
// ----------------------------------------------------------------------------

// newWasmCapsCmd evaluates the capability-based security model by demonstrating
// grant defaults, escape vector coverage, and path/net/GPU access control.
func newWasmCapsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "caps",
		Short:         "Inspect capability grants & escape vector coverage",
		Args:          cobra.NoArgs,
		Example:       "  cafctl wasm caps",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "  cafctl wasm caps · capability security model")
			fmt.Fprintln(out, Separator('═', 64))
			fmt.Fprintln(out, "")

			// Default grant is fully denied
			grant := wasm.NewDefaultGrant()
			fmt.Fprintf(out, "%s Default grant (deny-all):\n", OK())
			fmt.Fprintf(out, "    Filesystem: %v\n", grant.HasFilesystemAccess())
			fmt.Fprintf(out, "    Network:    %v\n", grant.HasNetworkAccess())
			fmt.Fprintf(out, "    GPU:        %v\n", grant.HasGPUAccess())
			fmt.Fprintln(out, "")

			// Demonstrate path rule evaluation
			pathRule := &wasm.PathRule{
				AllowedRoots: []string{"/app/data", "/tmp"},
				DeniedPaths:  []string{"/app/data/secrets"},
			}
			pathTests := []struct {
				path   string
				expect bool
			}{
				{"/app/data/models/v1.bin", true},
				{"/app/data/secrets/key.pem", false},
				{"/tmp/scratch.dat", true},
				{"/../etc/passwd", false},
				{"/app/data/%2e%2e/secrets/key", false},
			}
			fmt.Fprintf(out, "%s Path rule evaluation (roots=[/app/data, /tmp], deny=[/app/data/secrets]):\n", OK())
			for _, tc := range pathTests {
				result := pathRule.IsPathAllowed(tc.path)
				sym := successSymbol
				if !result {
					sym = errorSymbol
				}
				fmt.Fprintf(out, "    %s %-40s → %v\n", sym, tc.path, result)
			}
			fmt.Fprintln(out, "")

			// Escape vector coverage
			total, blocked, mitigated, notCovered := wasm.TotalEscapeVectors()
			fmt.Fprintf(out, "%s Escape vector coverage:\n", OK())
			fmt.Fprintf(out, "    Total vectors:  %d\n", total)
			fmt.Fprintf(out, "    Blocked:        %d\n", blocked)
			fmt.Fprintf(out, "    Mitigated:      %d\n", mitigated)
			fmt.Fprintf(out, "    Not covered:    %d\n", notCovered)
			fmt.Fprintf(out, "    Coverage:       %.1f%%\n", float64(blocked+mitigated)/float64(total)*100)
			fmt.Fprintln(out, "")

			return nil
		},
	}
	return cmd
}
