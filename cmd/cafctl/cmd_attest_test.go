// Package main - `cafctl attest` CLI tests.
//
// runAttest emits its human-readable receipt straight to os.Stdout via fmt/color
// (and errors to os.Stderr), so buffer capture on a cobra command cannot see it.
// The command is therefore verified through its observable, deterministic
// contract instead: the returned error on bad input and the evidence chain file
// it persists to ./.caf/evidence.chain. runAttest reads the attest* package
// globals and ignores its *cobra.Command argument, so tests set the globals
// directly and chdir into an isolated temp dir. Because they mutate globals and
// the process working directory, these tests are not parallel.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ----------------------------------------------------------------------------
// Test helpers
// ----------------------------------------------------------------------------

// chdirTemp switches the process working directory to a fresh temp dir for the
// duration of the test and restores it on cleanup. attest persists its chain to
// ./.caf/evidence.chain relative to the cwd, so an isolated cwd keeps the test
// hermetic. Not parallel-safe: os.Chdir is process-global.
func chdirTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(old) })
	return dir
}

// resetAttestGlobals restores the attest flag globals to a known baseline so
// each test starts clean regardless of execution order.
func resetAttestGlobals() {
	attestStatement = ""
	attestKeyPath = ""
	attestJSON = false
}

// ----------------------------------------------------------------------------
// Test suite: command construction & flag defaults
// ----------------------------------------------------------------------------

func TestAttest_CommandConstruction(t *testing.T) {
	t.Parallel()

	require.NotNil(t, attestCmd, "attest command must exist")
	assert.Equal(t, "attest [--statement TEXT]", attestCmd.Use)
	require.NotNil(t, attestCmd.RunE)

	flags := attestCmd.Flags()
	for _, name := range []string{"statement", "key", "json"} {
		assert.NotNil(t, flags.Lookup(name), "flag --%s must exist", name)
	}
	assert.Equal(t, "", flags.Lookup("statement").DefValue, "default statement is empty")
	assert.Equal(t, "false", flags.Lookup("json").DefValue, "default json is off")
}

// ----------------------------------------------------------------------------
// Test suite: happy path (side effect: persisted evidence chain)
// ----------------------------------------------------------------------------

func TestAttest_HappyPath_PersistsChain(t *testing.T) {
	dir := chdirTemp(t)
	resetAttestGlobals()
	attestStatement = "deployed v2.3.1 to production cluster gpu-prod-01"

	require.NoError(t, runAttest(nil, nil), "attest with a statement must succeed")

	chainPath := filepath.Join(dir, ".caf", "evidence.chain")
	info, err := os.Stat(chainPath)
	require.NoError(t, err, "attestation must persist the evidence chain")
	assert.Positive(t, info.Size(), "persisted chain must not be empty")
}

func TestAttest_JSON_PersistsChain(t *testing.T) {
	dir := chdirTemp(t)
	resetAttestGlobals()
	attestStatement = "CI/CD pipeline passed"
	attestJSON = true

	require.NoError(t, runAttest(nil, nil), "attest --json must succeed")

	_, err := os.Stat(filepath.Join(dir, ".caf", "evidence.chain"))
	require.NoError(t, err, "json mode still persists the chain")
}

// ----------------------------------------------------------------------------
// Test suite: error paths
// ----------------------------------------------------------------------------

func TestAttest_Error_MissingStatement(t *testing.T) {
	resetAttestGlobals() // empty statement

	err := runAttest(nil, nil)
	require.Error(t, err, "attest without a statement must fail")
	assert.Contains(t, err.Error(), "attestation statement required")
}

func TestAttest_Error_InvalidKeyPath(t *testing.T) {
	chdirTemp(t)
	resetAttestGlobals()
	attestStatement = "signed release"
	attestKeyPath = filepath.Join(t.TempDir(), "does-not-exist.pem")

	err := runAttest(nil, nil)
	require.Error(t, err, "a missing --key file must fail")
	assert.Contains(t, err.Error(), "read key", "error identifies the key read failure")
}

// ----------------------------------------------------------------------------
// Test suite: determinism
// ----------------------------------------------------------------------------

func TestAttest_Determinism_RepeatedAttest(t *testing.T) {
	old, err := os.Getwd()
	require.NoError(t, err)
	// Restore the working directory before returning so no temp dir is the
	// process cwd when t.TempDir's removal cleanups fire — on Windows an
	// in-use cwd cannot be deleted.
	defer func() { _ = os.Chdir(old) }()

	for i := 0; i < 5; i++ {
		dir := t.TempDir()
		require.NoError(t, os.Chdir(dir))
		resetAttestGlobals()
		attestStatement = fmt.Sprintf("attestation %d", i)

		require.NoError(t, runAttest(nil, nil), "iteration %d must succeed", i)
		_, statErr := os.Stat(filepath.Join(dir, ".caf", "evidence.chain"))
		require.NoError(t, statErr, "iteration %d must persist its chain", i)

		// Leave the temp dir before the next iteration / cleanup.
		require.NoError(t, os.Chdir(old))
	}
}
