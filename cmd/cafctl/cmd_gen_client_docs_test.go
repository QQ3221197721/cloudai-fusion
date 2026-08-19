// Package main - cafctl gen subcommand tests (M40 API Client Generator + M43 Doc Generator).
package main

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenCmds is a table-driven test over both gen subcommands.
func TestGenCmds(t *testing.T) {
	tests := []struct {
		name         string
		newCmd       func() *cobra.Command
		args         []string
		wantContains []string
	}{
		{
			name:   "gen client reports language, supported targets, and generated files",
			newCmd: newGenClientCmd,
			wantContains: []string{
				"API client generator (M40)",
				"Language:",
				"go",
				"Spec:",
				"built-in demo spec",
				"Supported:",
				"typescript",
				"python",
				"client.go",
				"Generation complete",
			},
		},
		{
			name:   "gen docs reports symbol counts and parses package structure",
			newCmd: newGenDocsCmd,
			// CWD during tests is the cmd/cafctl package dir, so document it directly.
			args: []string{"--dir", "."},
			wantContains: []string{
				"documentation generator (M43)",
				"# ",
				"Package:",
				"Functions:",
				"Types:",
				"Constants:",
				"Variables:",
				"Documentation generated",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := tc.newCmd()
			buf := wireCmd(cmd)
			cmd.SetArgs(tc.args)
			require.NoError(t, cmd.Execute())

			s := buf.String()
			for _, want := range tc.wantContains {
				assert.Contains(t, s, want, "expected output to contain %q in:\n%s", want, s)
			}
		})
	}
}

// TestGenCmds_Deterministic ensures repeated runs are byte-identical across multiple invocations.
func TestGenCmds_Deterministic(t *testing.T) {
	cases := []struct {
		name    string
		factory func() *cobra.Command
		args    []string
	}{
		{"gen-client", newGenClientCmd, nil},
		{"gen-docs", newGenDocsCmd, []string{"--dir", "."}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			results := make(map[string]bool)
			for i := 0; i < 10; i++ {
				cmd := tc.factory()
				buf := wireCmd(cmd)
				cmd.SetArgs(tc.args)
				require.NoError(t, cmd.Execute())
				results[buf.String()] = true
			}
			assert.Len(t, results, 1, "repeated runs must be identical")
		})
	}
}

// TestGenCmds_RejectsArgs verifies each leaf command rejects extra args.
func TestGenCmds_RejectsArgs(t *testing.T) {
	factories := map[string]func() *cobra.Command{
		"gen-client": newGenClientCmd,
		"gen-docs":   newGenDocsCmd,
	}
	for name, factory := range factories {
		t.Run(name+"-rejects-extra-args", func(t *testing.T) {
			cmd := factory()
			wireCmd(cmd)
			cmd.SetArgs([]string{"extra", "arg"})
			assert.Error(t, cmd.Execute(), "should reject extra arguments")
		})
	}
}

// TestGenParentCmd verifies the parent command wires up its leaf subcommands.
func TestGenParentCmd(t *testing.T) {
	cmd := newGenCmd()
	assert.Equal(t, "gen", cmd.Use)
	assert.NotNil(t, cmd.Short)

	expected := map[string]bool{"client": false, "docs": false}
	for _, sub := range cmd.Commands() {
		if _, ok := expected[sub.Use]; ok {
			expected[sub.Use] = true
		}
	}
	for leaf, found := range expected {
		assert.True(t, found, "expected 'gen' to have subcommand %q", leaf)
	}
}
