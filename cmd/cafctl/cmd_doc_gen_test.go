// Package main - cafctl doc gen subcommand tests (M43 Documentation Generator).
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDocGenCmd is a table-driven test over various M43 documentation generation scenarios.
func TestDocGenCmd(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantContains []string
		wantErr      bool
	}{
		{
			name:   "doc gen with default pkg/docgen reports symbol counts and parsed package structure",
			args:   []string{"../../pkg/docgen"},
			wantContains: []string{
				"documentation generator (M43)",
				"# ",
				"Package:",
				"Functions:",
				"Types:",
				"Constants:",
				"Variables:",
				"Total symbols:",
				"Documentation generated",
				"index.md",
				"types.md",
			},
			wantErr: false,
		},
		{
			name: "doc gen with custom title",
			args: []string{"--title", "Custom Title", "../../pkg/apiclientgen"},
			wantContains: []string{
				"Custom Title",
			},
			wantErr: false,
		},
		{
			name:   "doc gen with custom output directory",
			args:   []string{"--output", filepath.Join(os.TempDir(), "test-docs"), "../../pkg/docgen"},
			wantContains: []string{
				"test-docs",
			},
			wantErr: false,
		},
		{
			name:   "doc gen rejects non-existent directory",
			args:   []string{"./nonexistent/package"},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newDocGenCmd()
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

// TestDocGen_Deterministic ensures repeated runs are byte-identical across multiple invocations.
func TestDocGen_Deterministic(t *testing.T) {
	cases := []struct {
		name    string
		factory func() *cobra.Command
		args    []string
	}{
		{"doc-docs-to-default-out", newDocGenCmd, []string{"../../pkg/docgen"}},
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

// TestDocGen_RejectsArgs verifies the leaf command rejects wrong number of args.
func TestDocGen_RejectsArgs(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantErr  bool
	}{
		{"too few args", []string{}, true},
		{"too many args", []string{"arg1", "arg2"}, true},
		{"just right", []string{"../../pkg/docgen"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newDocGenCmd()
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

// TestDocGen_OutputFiles creates a temp directory and verifies files are written there.
func TestDocGen_OutputFiles(t *testing.T) {
	tmpDir := t.TempDir()

	cmd := newDocGenCmd()
	
	testPkgPath := "../../pkg/docgen"
	cmd.SetArgs([]string{"--output", tmpDir, testPkgPath})
	
	err := cmd.Execute()
	assert.NoError(t, err)

	// Verify that index.md and types.md were created in the temp dir
	indexPath := filepath.Join(tmpDir, "index.md")
	typesPath := filepath.Join(tmpDir, "types.md")

	_, err = os.Stat(indexPath)
	assert.NoError(t, err, "index.md should exist in output directory")

	_, err = os.Stat(typesPath)
	assert.NoError(t, err, "types.md should exist in output directory")

	// Verify content is non-empty
	content, err := os.ReadFile(indexPath)
	assert.NoError(t, err)
	assert.Greater(t, len(content), 100, "index.md should have substantial content")

	assert.Contains(t, string(content), "# ", "index.md should contain markdown header")
}

// TestDocGen_SymbolCounts verifies symbol extraction accuracy for a known package.
func TestDocGen_SymbolCounts(t *testing.T) {
	cmd := newDocGenCmd()
	buf := wireCmd(cmd)
	cmd.SetArgs([]string{"../../pkg/docgen"})
	
	err := cmd.Execute()
	assert.NoError(t, err)

	s := buf.String()
	assert.Contains(t, s, "Functions:", "should report function count")
	assert.Contains(t, s, "Types:", "should report type count")
	assert.Contains(t, s, "Constants:", "should report constant count")
	assert.Contains(t, s, "Variables:", "should report variable count")
	assert.Contains(t, s, "Total symbols:", "should report total symbol count")

	// Extract total symbols and verify it's greater than zero
	lines := strings.Split(s, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "Total symbols:") {
			var total int
			_, err := fmt.Sscanf(line, "Total symbols: %d", &total)
			assert.NoError(t, err)
			assert.Greater(t, total, 0, "docgen package should have documented symbols")
		}
	}
}
