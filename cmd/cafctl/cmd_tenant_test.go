// Package main - `cafctl tenant` CLI tests (Module 11 multi-tenant GPU sharing).
//
// The tenant subcommands are the cleanest to test: each is built by a
// parent-less constructor (newTenantCreateCmd, newTenantListCmd, ...) that keeps
// all flag state in local closure variables and writes every byte to
// cmd.OutOrStdout()/cmd.ErrOrStderr(). Tests therefore drive the real cobra
// command with SetArgs + a captured buffer — no globals, no network, no GPU.
// Stores live in t.TempDir(); attestations use ephemeral in-memory signers.
package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ----------------------------------------------------------------------------
// Test helpers
// ----------------------------------------------------------------------------

// runTenant executes a freshly-constructed tenant subcommand with args, capturing
// combined stdout+stderr. It returns the output and the Execute error (if any).
func runTenant(cmd *cobra.Command, args []string) (string, error) {
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

// mustCreatePool creates a pool in store and fails the test on error.
func mustCreatePool(t *testing.T, store, name, mode, gpuType, gpus string) string {
	t.Helper()
	out, err := runTenant(newTenantCreateCmd(), []string{
		"--store", store,
		"--name", name,
		"--mode", mode,
		"--gpu-type", gpuType,
		"--gpus", gpus,
	})
	require.NoError(t, err, "create pool must succeed; out=%s", out)
	return out
}

// ----------------------------------------------------------------------------
// Test suite: command construction
// ----------------------------------------------------------------------------

func TestTenant_CommandConstruction(t *testing.T) {
	t.Parallel()

	cmd := newTenantCmd()
	require.NotNil(t, cmd, "tenant command must be constructed")
	assert.Equal(t, "tenant", cmd.Use)

	expectedSubs := []string{
		"create", "list", "add-tenant", "allocate", "delete",
		"activate", "suspend", "resume", "delete-pool",
	}
	got := map[string]bool{}
	for _, c := range cmd.Commands() {
		got[c.Name()] = true
	}
	for _, sub := range expectedSubs {
		assert.True(t, got[sub], "subcommand %q must exist", sub)
	}
}

func TestTenant_Create_Flags(t *testing.T) {
	t.Parallel()

	flags := newTenantCreateCmd().Flags()
	for _, name := range []string{"store", "output", "pool", "name", "mode", "gpu-type", "gpus", "mig-profile", "slices", "node", "no-attest"} {
		assert.NotNil(t, flags.Lookup(name), "flag --%s must exist", name)
	}
	// Documented defaults.
	assert.Equal(t, "mps", flags.Lookup("mode").DefValue, "default mode")
	assert.Equal(t, "a100", flags.Lookup("gpu-type").DefValue, "default gpu-type")
	assert.Equal(t, "0", flags.Lookup("gpus").DefValue, "default gpus")
}

func TestTenant_Allocate_RequiredFlags(t *testing.T) {
	t.Parallel()

	flags := newTenantAllocateCmd().Flags()
	for _, name := range []string{"store", "pool", "tenant", "slices"} {
		assert.NotNil(t, flags.Lookup(name), "flag --%s must exist", name)
	}
}

// ----------------------------------------------------------------------------
// Test suite: happy path
// ----------------------------------------------------------------------------

func TestTenant_Create_HappyPath(t *testing.T) {
	t.Parallel()

	store := filepath.Join(t.TempDir(), "store")
	out := mustCreatePool(t, store, "test-pool", "mps", "a100", "0,1")

	assert.Contains(t, out, "Pool:", "pool receipt shown")
	assert.Contains(t, out, "test-pool", "name echoed back")
	assert.Contains(t, out, "pending", "new pool starts pending")
}

func TestTenant_List_Empty(t *testing.T) {
	t.Parallel()

	store := filepath.Join(t.TempDir(), "empty")
	out, err := runTenant(newTenantListCmd(), []string{"--store", store})
	require.NoError(t, err, "list on empty store must succeed; out=%s", out)
	assert.Contains(t, out, "(no pools", "empty-state message shown")
}

func TestTenant_AddTenant_AfterCreate(t *testing.T) {
	t.Parallel()

	store := filepath.Join(t.TempDir(), "store")
	mustCreatePool(t, store, "team-a", "mps", "h100", "0")

	out, err := runTenant(newTenantAddTenantCmd(), []string{
		"--store", store,
		"--pool", "team-a",
		"--name", "alice-project",
		"--mode", "mps-share",
	})
	require.NoError(t, err, "add-tenant must succeed; out=%s", out)
	assert.Contains(t, out, "Tenant:", "tenant receipt shown")
	assert.Contains(t, out, "alice-project", "tenant name echoed back")
}

func TestTenant_Create_JSONOutput(t *testing.T) {
	t.Parallel()

	store := filepath.Join(t.TempDir(), "store")
	out, err := runTenant(newTenantCreateCmd(), []string{
		"--store", store,
		"--name", "json-pool",
		"--mode", "mps",
		"--gpus", "0",
		"--output", "json",
	})
	require.NoError(t, err, "create --output json must succeed; out=%s", out)
	assert.Contains(t, out, `"name": "json-pool"`, "json contains name")
	assert.Contains(t, out, `"status":`, "json contains status")
}

// ----------------------------------------------------------------------------
// Test suite: error paths
// ----------------------------------------------------------------------------

func TestTenant_Error_MissingName(t *testing.T) {
	t.Parallel()

	out, err := runTenant(newTenantCreateCmd(), []string{"--store", t.TempDir()})
	require.Error(t, err, "must fail without --name")
	assert.Contains(t, err.Error(), "name", "error mentions the missing required flag; out=%s", out)
}

func TestTenant_Error_MissingPoolForAddTenant(t *testing.T) {
	t.Parallel()

	_, err := runTenant(newTenantAddTenantCmd(), []string{"--name", "orphan"})
	require.Error(t, err, "must fail without --pool")
	assert.Contains(t, err.Error(), "pool", "error mentions the missing required flag")
}

func TestTenant_Error_InvalidGPUIndices(t *testing.T) {
	t.Parallel()

	out, err := runTenant(newTenantCreateCmd(), []string{
		"--store", t.TempDir(),
		"--name", "bad-gpu-pool",
		"--mode", "mps",
		"--gpus", "abc,def",
	})
	require.Error(t, err, "invalid GPU indices must fail")
	assert.Contains(t, out, "invalid GPU index", "validation error shown on stderr")
}

func TestTenant_Error_AllocateNonExistentPool(t *testing.T) {
	t.Parallel()

	out, err := runTenant(newTenantAllocateCmd(), []string{
		"--store", t.TempDir(),
		"--pool", "nonexistent",
		"--tenant", "bob",
		"--slices", "1",
	})
	require.Error(t, err, "allocation to a missing pool must fail")
	assert.Contains(t, out, "not found", "error identifies the missing pool")
}

// ----------------------------------------------------------------------------
// Test suite: determinism & full journey
// ----------------------------------------------------------------------------

func TestTenant_Journey_CreateListAddTenant(t *testing.T) {
	t.Parallel()

	store := filepath.Join(t.TempDir(), "journey")

	createOut := mustCreatePool(t, store, "journey-pool", "mps", "a100", "0")
	assert.Contains(t, createOut, "journey-pool")

	listOut, err := runTenant(newTenantListCmd(), []string{"--store", store})
	require.NoError(t, err, "list must succeed; out=%s", listOut)
	assert.Contains(t, listOut, "journey-pool", "created pool appears in list")

	addOut, err := runTenant(newTenantAddTenantCmd(), []string{
		"--store", store, "--pool", "journey-pool", "--name", "svc-a",
	})
	require.NoError(t, err, "add-tenant must succeed; out=%s", addOut)
	assert.Contains(t, addOut, "svc-a")
}

func TestTenant_Determinism_RepeatedCreate(t *testing.T) {
	t.Parallel()

	for i := 0; i < 5; i++ {
		store := filepath.Join(t.TempDir(), fmt.Sprintf("store-%d", i))
		name := fmt.Sprintf("pool-%d", i)
		out := mustCreatePool(t, store, name, "mps", "a100", "0")
		assert.Contains(t, out, name, "iteration %d echoes its unique pool name", i)
	}
}

// Keep strings imported for future assertions on error text.
var _ = strings.TrimSpace
