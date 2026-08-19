// Package main - cafctl wasm subcommand tests (M50 + M51).
package main

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWasmCmds is a table-driven test over both WASM subcommands.
func TestWasmCmds(t *testing.T) {
	tests := []struct {
		name         string
		newCmd       func() *cobra.Command
		wantContains []string
	}{
		{
			name:   "wasm validate reports binary validation results",
			newCmd: newWasmValidateCmd,
			wantContains: []string{
				"WASM binary validation engine",
				"Valid MVP module (8 bytes)",
				"Valid:   true",
				"Version: 1",
				"Invalid magic (0xDEADBEEF)",
				"Too-small binary (2 bytes)",
				"Validation engine operational",
			},
		},
		{
			name:   "wasm caps reports capability grants and escape vectors",
			newCmd: newWasmCapsCmd,
			wantContains: []string{
				"capability security model",
				"Default grant (deny-all)",
				"Filesystem: false",
				"Network:    false",
				"GPU:        false",
				"Path rule evaluation",
				"/app/data/models/v1.bin",
				"Escape vector coverage",
				"Blocked:",
				"Coverage:",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := tc.newCmd()
			buf := wireCmd(cmd)
			require.NoError(t, cmd.Execute())

			s := buf.String()
			for _, want := range tc.wantContains {
				assert.Contains(t, s, want)
			}
		})
	}
}

// TestWasmCmds_Deterministic ensures repeated runs are byte-identical.
func TestWasmCmds_Deterministic(t *testing.T) {
	factories := map[string]func() *cobra.Command{
		"wasm-validate": newWasmValidateCmd,
		"wasm-caps":     newWasmCapsCmd,
	}
	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			results := make(map[string]bool)
			for i := 0; i < 10; i++ {
				cmd := factory()
				buf := wireCmd(cmd)
				require.NoError(t, cmd.Execute())
				results[buf.String()] = true
			}
			assert.Len(t, results, 1, "repeated runs must be identical")
		})
	}
}

// TestWasmCmds_RejectsArgs verifies each leaf command rejects extra args.
func TestWasmCmds_RejectsArgs(t *testing.T) {
	factories := map[string]func() *cobra.Command{
		"wasm-validate": newWasmValidateCmd,
		"wasm-caps":     newWasmCapsCmd,
	}
	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			cmd := factory()
			wireCmd(cmd)
			cmd.SetArgs([]string{"extra", "arg"})
			assert.Error(t, cmd.Execute())
		})
	}
}

// TestWasmParentCmd verifies the parent command wires up its leaf subcommands.
func TestWasmParentCmd(t *testing.T) {
	cmd := newWasmCmd()
	assert.Equal(t, "wasm", cmd.Use)

	expected := map[string]bool{"validate": false, "caps": false}
	for _, sub := range cmd.Commands() {
		if _, ok := expected[sub.Use]; ok {
			expected[sub.Use] = true
		}
	}
	for leaf, found := range expected {
		assert.True(t, found, "expected wasm to have subcommand %q", leaf)
	}
}
