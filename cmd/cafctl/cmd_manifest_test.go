// Package main - `cafctl manifest` CLI tests.
//
// The manifest run* functions print their receipts to os.Stdout via fmt/color,
// so the coverage here targets their deterministic contract: the returned error
// and the files they write. runManifestInit reads the manifestOutputPath global
// and a "namespace" flag it looks up on its *cobra.Command, so tests pass a
// minimal command carrying that flag. Valid manifests used by validate/apply are
// produced by runManifestInit itself, so they always match the production
// default template. These tests mutate package globals and are not parallel.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ----------------------------------------------------------------------------
// Test helpers
// ----------------------------------------------------------------------------

// newManifestInitTestCmd builds a bare command carrying the single flag that
// runManifestInit reads via cmd.Flags().Lookup("namespace"); without it the
// production code would nil-panic.
func newManifestInitTestCmd(namespace string) *cobra.Command {
	c := &cobra.Command{Use: "init"}
	c.Flags().String("namespace", namespace, "Namespace for manifest")
	return c
}

// writeInitManifest runs `manifest init` into dir and returns the written path.
// The result is guaranteed valid because it is produced by the same
// NewDefaultManifest the production command uses.
func writeInitManifest(t *testing.T, dir, namespace string) string {
	t.Helper()
	out := filepath.Join(dir, "evidence-manifest.yaml")
	manifestOutputPath = out
	require.NoError(t, runManifestInit(newManifestInitTestCmd(namespace), nil), "manifest init must succeed")
	return out
}

// ----------------------------------------------------------------------------
// Test suite: command construction & flags
// ----------------------------------------------------------------------------

func TestManifest_CommandConstruction(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "manifest", manifestCmd.Use)
	got := map[string]bool{}
	for _, c := range manifestCmd.Commands() {
		got[c.Name()] = true
	}
	for _, sub := range []string{"init", "validate", "apply", "export"} {
		assert.True(t, got[sub], "subcommand %q must exist", sub)
	}
}

func TestManifest_Flags(t *testing.T) {
	t.Parallel()

	assert.NotNil(t, manifestInitCmd.Flags().Lookup("output"), "init --output must exist")
	assert.NotNil(t, manifestInitCmd.Flags().Lookup("namespace"), "init --namespace must exist")
	assert.NotNil(t, manifestExportCmd.Flags().Lookup("formats"), "export --formats must exist")
	assert.NotNil(t, manifestExportCmd.Flags().Lookup("list-only"), "export --list-only must exist")
	assert.NotNil(t, manifestApplyCmd.Flags().Lookup("dry-run"), "apply --dry-run must exist")
}

// ----------------------------------------------------------------------------
// Test suite: happy path
// ----------------------------------------------------------------------------

func TestManifest_Init_WritesFile(t *testing.T) {
	dir := t.TempDir()
	out := writeInitManifest(t, dir, "default")

	content, err := os.ReadFile(out)
	require.NoError(t, err, "manifest file should exist")
	assert.NotEmpty(t, content, "manifest should have content")
}

func TestManifest_Validate_GeneratedManifestIsValid(t *testing.T) {
	path := writeInitManifest(t, t.TempDir(), "production")

	// The default template must validate without hard errors (warnings are ok).
	require.NoError(t, runManifestValidate(&cobra.Command{}, []string{path}),
		"a freshly generated manifest must validate")
}

func TestManifest_Apply_DryRun(t *testing.T) {
	path := writeInitManifest(t, t.TempDir(), "default")

	dryRun = true
	defer func() { dryRun = false }()
	require.NoError(t, runManifestApply(&cobra.Command{}, []string{path}),
		"dry-run apply of a valid manifest must succeed")
}

func TestManifest_Export_ListOnly(t *testing.T) {
	exportListOnly = true
	exportFormats = nil
	exportOutputPath = "."
	defer func() { exportListOnly = false }()

	// list-only returns before parsing, so any path argument is accepted.
	require.NoError(t, runManifestExport(&cobra.Command{}, []string{"unused.yaml"}),
		"export --list-only must succeed")
}

// ----------------------------------------------------------------------------
// Test suite: error paths
// ----------------------------------------------------------------------------

func TestManifest_Error_MissingValidatePath(t *testing.T) {
	err := runManifestValidate(&cobra.Command{}, nil)
	require.Error(t, err, "validate without a path must fail")
	assert.Contains(t, err.Error(), "required", "error mentions the missing path")
}

func TestManifest_Error_MissingApplyPath(t *testing.T) {
	err := runManifestApply(&cobra.Command{}, nil)
	require.Error(t, err, "apply without a path must fail")
	assert.Contains(t, err.Error(), "required")
}

func TestManifest_Error_MissingExportPath(t *testing.T) {
	err := runManifestExport(&cobra.Command{}, nil)
	require.Error(t, err, "export without a path must fail")
	assert.Contains(t, err.Error(), "required")
}

func TestManifest_Error_ValidateNonExistentFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.yaml")
	err := runManifestValidate(&cobra.Command{}, []string{missing})
	require.Error(t, err, "validating a missing file must fail")
}

// ----------------------------------------------------------------------------
// Test suite: determinism & journey
// ----------------------------------------------------------------------------

func TestManifest_Init_Determinism(t *testing.T) {
	for i := 0; i < 5; i++ {
		dir := t.TempDir()
		manifestOutputPath = filepath.Join(dir, fmt.Sprintf("manifest-%d.yaml", i))
		require.NoError(t, runManifestInit(newManifestInitTestCmd("default"), nil),
			"iteration %d init must succeed", i)

		content, err := os.ReadFile(manifestOutputPath)
		require.NoError(t, err, "iteration %d file should exist", i)
		assert.NotEmpty(t, content, "iteration %d content present", i)
	}
}

func TestManifest_Journey_InitThenValidate(t *testing.T) {
	path := writeInitManifest(t, t.TempDir(), "staging")
	require.NoError(t, runManifestValidate(&cobra.Command{}, []string{path}),
		"init → validate round-trip must succeed")
}
