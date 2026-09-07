// Package main - cafctl client gen subcommand tests (M40 API Client Generator).
package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClientGenCmd is a table-driven test over various M40 client generation scenarios.
func TestClientGenCmd(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantContains []string
		wantErr      bool
	}{
		{
			name:   "client gen with default specs reports language, supported targets, and generated files",
			args:   []string{"https://example.com/openapi.yaml"},
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
			wantErr: false,
		},
		{
			name:   "client gen with typescript language",
			args:   []string{"--lang", "typescript", "--pkg", "mypkg", "https://example.com/openapi.yaml"},
			wantContains: []string{
				"Language:  typescript",
				"Package:   mypkg",
				"index.ts",
			},
			wantErr: false,
		},
		{
			name: "client gen with custom package name",
			args: []string{"--pkg", "myapiclient", "https://example.com/openapi.yaml"},
			wantContains: []string{
				"Package:   myapiclient",
			},
			wantErr: false,
		},
		{
			name:   "client gen rejects non-existent file",
			args:   []string{"./nonexistent/spec.yaml"},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newClientGenCmd()
			buf := wireCmd(cmd)
			cmd.SetArgs(tc.args)
			err := cmd.Execute()

			if tc.wantErr {
				assert.Error(t, err, "expected error but got none")
				return
			}

			require.NoError(t, err)
			s := buf.String()

			for _, want := range tc.wantContains {
				assert.Contains(t, s, want, "expected output to contain %q in:\n%s", want, s)
			}
		})
	}
}

// TestClientGen_Deterministic ensures repeated runs are byte-identical across multiple invocations.
func TestClientGen_Deterministic(t *testing.T) {
	// URL mode always falls back to the built-in demo spec, so output must be deterministic.
	cases := []struct {
		name    string
		factory func() *cobra.Command
		args    []string
	}{
		{"default-go", newClientGenCmd, []string{"https://example.com/openapi.yaml"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			results := make(map[string]bool)
			for i := 0; i < 5; i++ {
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

// TestClientGen_RejectsArgs verifies the leaf command rejects wrong number of args.
func TestClientGen_RejectsArgs(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantErr  bool
	}{
		{"too few args", []string{}, true},
		{"too many args", []string{"arg1", "arg2"}, true},
		{"just right", []string{"https://example.com/openapi.yaml"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newClientGenCmd()
			wireCmd(cmd)
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if tc.wantErr {
				assert.Error(t, err, "should reject extra arguments")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestClientGen_OutputFile creates a temp directory and verifies file is written there.
func TestClientGen_OutputFile(t *testing.T) {
	tmpDir := t.TempDir()

	cmd := newClientGenCmd()
	
	// URL form falls back to the built-in demo spec, so generation always succeeds.
	cmd.SetArgs([]string{"--output", tmpDir, "--lang", "go", "https://example.com/openapi.yaml"})
	
	err := cmd.Execute()
	assert.NoError(t, err)

	// Verify that client.go was created in the temp dir
	clientGoPath := filepath.Join(tmpDir, "client.go")
	_, err = os.Stat(clientGoPath)
	assert.NoError(t, err, "client.go should exist in output directory")
}
