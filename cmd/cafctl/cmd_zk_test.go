// Package main - `cafctl zk-demo` CLI tests.
//
// zk-demo drives the REAL Groth16 + Poseidon2 prover/verifier from
// pkg/evidence/zk (no cryptography is faked). runZKDemoGenerate/Verify print
// their tour to os.Stdout, so — as with attest — these tests assert the
// deterministic contract: the returned error, the artifacts written to disk, and
// a full generate→verify roundtrip. Witness counts are kept small (2) so the
// real trusted setup stays fast even under -count=10. The command reads zkDemo*
// package globals, so the generating tests are not parallel; the pure-helper
// tests touch no globals and run in parallel.
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence/zk"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ----------------------------------------------------------------------------
// Test helpers
// ----------------------------------------------------------------------------

// genZK sets the generate globals and runs runZKDemoGenerate, returning the
// proof and vk paths plus the execute error. runZKDemoGenerate reads globals and
// uses cmd.Context(); a bare &cobra.Command{} yields context.Background().
func genZK(t *testing.T, dir string, count int, namespace string, jsonOut bool) (string, string, error) {
	t.Helper()
	proof := filepath.Join(dir, "proof.json")
	vk := filepath.Join(dir, "vk.bin")
	zkDemoOutputPath = proof
	zkDemoVKOutputPath = vk
	zkDemoCount = count
	zkDemoNamespace = namespace
	zkDemoJSON = jsonOut
	return proof, vk, runZKDemoGenerate(&cobra.Command{}, nil)
}

// verifyZKDemo runs runZKDemoVerify against the given artifacts.
func verifyZKDemo(proof, vk string, jsonOut bool) error {
	zkDemoJSON = jsonOut
	return runZKDemoVerify(&cobra.Command{}, []string{proof, vk})
}

// ----------------------------------------------------------------------------
// Test suite: command construction & flags
// ----------------------------------------------------------------------------

func TestZK_CommandConstruction(t *testing.T) {
	t.Parallel()

	require.NotNil(t, zkDemoCmd, "zk-demo command must exist")
	assert.Equal(t, "zk-demo", zkDemoCmd.Use)

	got := map[string]bool{}
	for _, c := range zkDemoCmd.Commands() {
		got[c.Name()] = true
	}
	assert.True(t, got["generate"], "generate subcommand must exist")
	assert.True(t, got["verify"], "verify subcommand must exist")
}

func TestZK_Generate_Flags(t *testing.T) {
	t.Parallel()

	flags := zkDemoGenerateCmd.Flags()
	for _, name := range []string{"output", "vk-output", "count", "namespace", "json"} {
		assert.NotNil(t, flags.Lookup(name), "generate flag --%s must exist", name)
	}
	assert.Equal(t, "10", flags.Lookup("count").DefValue, "default --count")
}

func TestZK_Verify_ArgsValidation(t *testing.T) {
	t.Parallel()
	require.NotNil(t, zkDemoVerifyCmd.Args, "verify must enforce an argument count")
}

// ----------------------------------------------------------------------------
// Test suite: happy path (real prover)
// ----------------------------------------------------------------------------

func TestZK_Generate_WritesArtifacts(t *testing.T) {
	dir := t.TempDir()
	proof, vk, err := genZK(t, dir, 2, "test-namespace", false)
	require.NoError(t, err, "generate must succeed")

	_, perr := os.Stat(proof)
	require.NoError(t, perr, "attestation JSON must be written")
	_, verr := os.Stat(vk)
	require.NoError(t, verr, "verifying key must be written")
}

func TestZK_GenerateAndVerify_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	proof, vk, err := genZK(t, dir, 2, "roundtrip", false)
	require.NoError(t, err, "generate must succeed")

	require.NoError(t, verifyZKDemo(proof, vk, false), "a freshly generated proof must verify offline")
}

func TestZK_Generate_JSON(t *testing.T) {
	dir := t.TempDir()
	proof, _, err := genZK(t, dir, 2, "json-test", true)
	require.NoError(t, err, "generate --json must succeed")
	_, perr := os.Stat(proof)
	require.NoError(t, perr, "json mode still writes the attestation")
}

// ----------------------------------------------------------------------------
// Test suite: error paths
// ----------------------------------------------------------------------------

func TestZK_Error_InvalidCount(t *testing.T) {
	dir := t.TempDir()
	_, _, err := genZK(t, dir, 0, "bad", false)
	require.Error(t, err, "count <= 0 must fail")
	assert.Contains(t, err.Error(), "invalid --count", "error identifies the bad flag")
}

func TestZK_Error_MissingProofFile(t *testing.T) {
	dir := t.TempDir()
	err := verifyZKDemo(filepath.Join(dir, "nope.json"), filepath.Join(dir, "nope.bin"), false)
	require.Error(t, err, "verifying a missing attestation must fail")
}

func TestZK_Error_MissingVKFile(t *testing.T) {
	dir := t.TempDir()
	proof, _, err := genZK(t, dir, 2, "missing-vk", false)
	require.NoError(t, err, "generate must succeed")

	err = verifyZKDemo(proof, filepath.Join(dir, "absent-vk.bin"), false)
	require.Error(t, err, "verifying with a missing key must fail")
	assert.Contains(t, err.Error(), "read vk", "error identifies the vk read failure")
}

// ----------------------------------------------------------------------------
// Test suite: pure helpers (no globals -> parallel-safe)
// ----------------------------------------------------------------------------

func TestZK_BuildDemoWitnesses(t *testing.T) {
	t.Parallel()

	const n = 5
	witnesses := buildDemoWitnesses(n, "test-ns")
	require.Len(t, witnesses, n, "correct witness count")
	for i, w := range witnesses {
		assert.Equal(t, uint64(i), w.Eidx, "monotonic index at %d", i)
		assert.True(t, w.InScope, "witness %d in scope", i)
	}
}

func TestZK_ShortHex(t *testing.T) {
	t.Parallel()

	long := hex.EncodeToString([]byte("0123456789012345678901234567890123456789"))
	short := shortHex(long)
	assert.Contains(t, short, "…", "long hex is abbreviated")
	assert.Less(t, len(short), len(long), "abbreviated form is shorter")

	assert.Equal(t, "abc123", shortHex("abc123"), "short hex is returned unchanged")
	assert.Equal(t, "not-hex-string!!!", shortHex("not-hex-string!!!"), "non-hex is returned unchanged")
}

func TestZK_WriteReadAttestation_Roundtrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "att.json")
	prover := zk.Groth16Prover{}
	att, _, err := prover.Prove(context.Background(), zk.StmtCompletePredicate, "all-in-scope", buildDemoWitnesses(2, "write-read"))
	require.NoError(t, err, "prove must succeed")

	require.NoError(t, writeAttestationJSON(path, att), "write must succeed")
	readBack, err := readAttestationJSON(path)
	require.NoError(t, err, "read must succeed")
	assert.Equal(t, att.Count, readBack.Count, "count roundtrips")
	assert.Equal(t, att.VKID, readBack.VKID, "VKID roundtrips")
}

// ----------------------------------------------------------------------------
// Test suite: determinism
// ----------------------------------------------------------------------------

func TestZK_Determinism_RepeatedRoundtrip(t *testing.T) {
	for i := 0; i < 2; i++ {
		dir := t.TempDir()
		proof, vk, err := genZK(t, dir, 2, fmt.Sprintf("iter-%d", i), false)
		require.NoError(t, err, "iteration %d generate", i)
		require.NoError(t, verifyZKDemo(proof, vk, false), "iteration %d verify", i)
	}
}
