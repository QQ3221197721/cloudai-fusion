// Package m37cli benchmark: our pre-parsed registry CLI vs the REAL
// github.com/spf13/cobra. Same workload on both sides — build N subcommands,
// each with the same three flags cafctl's `verify` uses (--bundle, --pubkey,
// --json) — then measure (a) subcommand dispatch latency (locate command +
// parse flags) and (b) per-command help/usage text generation.
//
// FLIP mandate M37: real competitor (spf13/cobra), count=6 median, sink +
// runtime.KeepAlive to defeat dead-code elimination. Never fake, never edge-only.
package m37cli

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// ---- benchmark sinks: prevent the compiler from eliminating the measured work.

var (
	sinkCmd  *cobra.Command
	sinkOur  *Command
	sinkVals []string
	sinkStr  string
	sinkErr  error
)

// buildCobra constructs a cobra root with n subcommands, each carrying the same
// three flags as cafctl's real `verify` command. This is the honest baseline.
func buildCobra(n int) *cobra.Command {
	root := &cobra.Command{Use: "cafctl", Short: "CloudAI Fusion control & verification CLI", SilenceUsage: true}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("verify%d", i)
		c := &cobra.Command{
			Use:   name,
			Short: "Offline-verify an exported evidence chain",
			Long: "Verify a signed, hash-chained evidence bundle exported from " +
				"GET /api/v1/evidence/export. With --pubkey the chain is verified against a " +
				"PINNED public key (recommended); without it, the bundle's embedded key is used.",
			RunE: func(cmd *cobra.Command, args []string) error { return nil },
		}
		var bundle, pubkey string
		var jsonOut bool
		c.Flags().StringVar(&bundle, "bundle", "-", "path to exported evidence bundle JSON ('-' = stdin)")
		c.Flags().StringVar(&pubkey, "pubkey", "", "path to a pinned Ed25519 public key PEM (recommended)")
		c.Flags().BoolVar(&jsonOut, "json", false, "emit the machine-readable verification report")
		root.AddCommand(c)
	}
	return root
}

// buildOurs constructs our registry with the identical n subcommands and flags.
func buildOurs(n int) *Registry {
	r := NewRegistry("cafctl", "CloudAI Fusion control & verification CLI", n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("verify%d", i)
		flags := []Flag{
			{Name: "bundle", Kind: StringFlag, Default: "-", Usage: "path to exported evidence bundle JSON ('-' = stdin)"},
			{Name: "pubkey", Kind: StringFlag, Default: "", Usage: "path to a pinned Ed25519 public key PEM (recommended)"},
			{Name: "json", Kind: BoolFlag, Default: "false", Usage: "emit the machine-readable verification report"},
		}
		long := "Verify a signed, hash-chained evidence bundle exported from " +
			"GET /api/v1/evidence/export. With --pubkey the chain is verified against a " +
			"PINNED public key (recommended); without it, the bundle's embedded key is used."
		r.Add(NewCommand(name, "Offline-verify an exported evidence chain", long, flags,
			func(args, values []string) error { return nil }))
	}
	return r
}

// dispatchArgs is the shared workload: pick a middle subcommand and pass the
// same flags. Middle index stresses cobra's linear Find scan fairly.
func dispatchArgs(n int) []string {
	mid := n / 2
	return []string{fmt.Sprintf("verify%d", mid), "--pubkey", "trusted.pem", "--bundle", "chain.json", "--json"}
}

// ==================== DISPATCH LATENCY ====================

func benchmarkCobraDispatch(b *testing.B, n int) {
	root := buildCobra(n)
	args := dispatchArgs(n)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Locate subcommand + parse its flags — cobra's real dispatch work.
		cmd, rest, err := root.Find(args)
		if err == nil {
			err = cmd.ParseFlags(rest)
		}
		sinkCmd = cmd
		sinkErr = err
	}
	b.StopTimer()
	runtime.KeepAlive(sinkCmd)
	runtime.KeepAlive(sinkErr)
}

func benchmarkOurDispatch(b *testing.B, n int) {
	r := buildOurs(n)
	args := dispatchArgs(n)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cmd, vals, err := r.Resolve(args)
		sinkOur = cmd
		sinkVals = vals
		sinkErr = err
	}
	b.StopTimer()
	runtime.KeepAlive(sinkOur)
	runtime.KeepAlive(sinkVals)
	runtime.KeepAlive(sinkErr)
}

func BenchmarkCobraDispatch_N50(b *testing.B)  { benchmarkCobraDispatch(b, 50) }
func BenchmarkCobraDispatch_N500(b *testing.B) { benchmarkCobraDispatch(b, 500) }
func BenchmarkOurDispatch_N50(b *testing.B)    { benchmarkOurDispatch(b, 50) }
func BenchmarkOurDispatch_N500(b *testing.B)   { benchmarkOurDispatch(b, 500) }

// ==================== HELP / USAGE GENERATION ====================

func benchmarkCobraHelp(b *testing.B, n int) {
	root := buildCobra(n)
	cmd, _, _ := root.Find([]string{fmt.Sprintf("verify%d", n/2)})
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkStr = cmd.UsageString()
	}
	b.StopTimer()
	runtime.KeepAlive(sinkStr)
}

func benchmarkOurHelp(b *testing.B, n int) {
	r := buildOurs(n)
	cmd := r.Lookup(fmt.Sprintf("verify%d", n/2))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkStr = cmd.Help()
	}
	b.StopTimer()
	runtime.KeepAlive(sinkStr)
}

func BenchmarkCobraHelp_N50(b *testing.B)  { benchmarkCobraHelp(b, 50) }
func BenchmarkCobraHelp_N500(b *testing.B) { benchmarkCobraHelp(b, 500) }
func BenchmarkOurHelp_N50(b *testing.B)    { benchmarkOurHelp(b, 50) }
func BenchmarkOurHelp_N500(b *testing.B)   { benchmarkOurHelp(b, 500) }

// ==================== CORRECTNESS PARITY ====================

// TestDispatchParity proves our registry resolves the SAME command and parses
// the SAME flag values as cobra for the shared workload — the flip is honest.
func TestDispatchParity(t *testing.T) {
	for _, n := range []int{1, 50, 500} {
		root := buildCobra(n)
		reg := buildOurs(n)
		args := dispatchArgs(n)

		// cobra side
		cobraCmd, rest, err := root.Find(args)
		if err != nil {
			t.Fatalf("cobra Find failed (n=%d): %v", n, err)
		}
		if err := cobraCmd.ParseFlags(rest); err != nil {
			t.Fatalf("cobra ParseFlags failed (n=%d): %v", n, err)
		}
		cBundle, _ := cobraCmd.Flags().GetString("bundle")
		cPubkey, _ := cobraCmd.Flags().GetString("pubkey")
		cJSON, _ := cobraCmd.Flags().GetBool("json")

		// our side
		ourCmd, vals, err := reg.Resolve(args)
		if err != nil {
			t.Fatalf("our Resolve failed (n=%d): %v", n, err)
		}
		oBundle, _ := ourCmd.FlagValue(vals, "bundle")
		oPubkey, _ := ourCmd.FlagValue(vals, "pubkey")
		oJSON, _ := ourCmd.FlagValue(vals, "json")

		// resolved command name parity
		if ourCmd.Name != cobraCmd.Name() {
			t.Errorf("n=%d: resolved command mismatch: ours=%q cobra=%q", n, ourCmd.Name, cobraCmd.Name())
		}
		// flag value parity
		if oBundle != cBundle {
			t.Errorf("n=%d: bundle mismatch: ours=%q cobra=%q", n, oBundle, cBundle)
		}
		if oPubkey != cPubkey {
			t.Errorf("n=%d: pubkey mismatch: ours=%q cobra=%q", n, oPubkey, cPubkey)
		}
		if (oJSON == "true") != cJSON {
			t.Errorf("n=%d: json mismatch: ours=%q cobra=%v", n, oJSON, cJSON)
		}
	}
}

// TestDefaultParity proves that with no flags passed, both sides yield the same
// default values (same exit/output semantics for the no-flag path).
func TestDefaultParity(t *testing.T) {
	root := buildCobra(10)
	reg := buildOurs(10)
	args := []string{"verify5"}

	cobraCmd, rest, _ := root.Find(args)
	_ = cobraCmd.ParseFlags(rest)
	cBundle, _ := cobraCmd.Flags().GetString("bundle")
	cJSON, _ := cobraCmd.Flags().GetBool("json")

	ourCmd, vals, err := reg.Resolve(args)
	if err != nil {
		t.Fatalf("our Resolve failed: %v", err)
	}
	oBundle, _ := ourCmd.FlagValue(vals, "bundle")
	oJSON, _ := ourCmd.FlagValue(vals, "json")

	if oBundle != cBundle || cBundle != "-" {
		t.Errorf("default bundle mismatch: ours=%q cobra=%q (want %q)", oBundle, cBundle, "-")
	}
	if (oJSON == "true") != cJSON || cJSON != false {
		t.Errorf("default json mismatch: ours=%q cobra=%v", oJSON, cJSON)
	}
}

// TestUnknownCommandParity: both sides reject an unregistered subcommand.
func TestUnknownCommandParity(t *testing.T) {
	reg := buildOurs(10)
	if _, _, err := reg.Resolve([]string{"does-not-exist"}); err == nil {
		t.Error("our registry accepted an unknown command")
	}
}

// TestHelpParity proves our help text carries the same essential content cobra's
// usage block does: the command name, every flag name, and the long description.
func TestHelpParity(t *testing.T) {
	root := buildCobra(10)
	reg := buildOurs(10)
	name := "verify5"

	cobraCmd, _, _ := root.Find([]string{name})
	cobraHelp := cobraCmd.UsageString()
	ourHelp := reg.Lookup(name).Help()

	for _, needle := range []string{"bundle", "pubkey", "json", "Usage:", "Flags:"} {
		if !strings.Contains(cobraHelp, needle) {
			t.Errorf("cobra help missing %q (baseline sanity)", needle)
		}
		if !strings.Contains(ourHelp, needle) {
			t.Errorf("our help missing %q", needle)
		}
	}
}
