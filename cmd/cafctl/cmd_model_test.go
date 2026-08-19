// Package main - `cafctl model` CLI tests.
//
// Each test builds fresh, parent-less command instances via the newXxxCmd()
// constructors (the run/verify-* pattern) so Execute runs the command
// directly — cobra would otherwise delegate a parented subcommand up to the
// root. The full developer journey (register -> list -> show -> lineage ->
// rollback) runs against a real temp registry.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wireCmd attaches a shared buffer to a command and returns the buffer.
func wireCmd(cmd *cobra.Command) *bytes.Buffer {
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	return buf
}

// writeModelWeights writes fake model weights into a temp file.
func writeModelWeights(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "weights.pt")
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	return p
}

// mustRegister registers one version through the real CLI command.
func mustRegister(t *testing.T, reg, name, version, artifact string, extra ...string) {
	t.Helper()
	cmd := newModelRegisterCmd()
	wireCmd(cmd)
	args := append([]string{artifact, "--name", name, "--version", version, "--registry", reg}, extra...)
	cmd.SetArgs(args)
	require.NoError(t, cmd.Execute(), "register %s:%s must succeed", name, version)
}

// TestModelRegisterCmd_ShowAndLineage walks the core developer journey:
// root version, fine-tuned child, show, and recursive lineage resolution.
func TestModelRegisterCmd_ShowAndLineage(t *testing.T) {
	reg := filepath.Join(t.TempDir(), "models")
	rootW := writeModelWeights(t, "root weights")
	tunedW := writeModelWeights(t, "fine-tuned weights")

	// register 1.0.0 (root)
	cmd1 := newModelRegisterCmd()
	out1 := wireCmd(cmd1)
	cmd1.SetArgs([]string{
		rootW, "--name", "resnet50", "--version", "1.0.0",
		"--registry", reg,
		"--dataset", "sha256:ds1", "--code", "git:abc1234",
		"--metric", "accuracy=0.94",
		"--task", "classification", "--framework", "pytorch",
	})
	require.NoError(t, cmd1.Execute())
	assert.Contains(t, out1.String(), "resnet50")
	assert.Contains(t, out1.String(), "1.0.0")
	assert.Contains(t, out1.String(), "Attestation:")

	// register 1.1.0 (fine-tuned from 1.0.0)
	cmd2 := newModelRegisterCmd()
	out2 := wireCmd(cmd2)
	cmd2.SetArgs([]string{
		tunedW, "--name", "resnet50", "--version", "1.1.0", "--parent", "1.0.0",
		"--registry", reg, "--metric", "accuracy=0.97",
	})
	require.NoError(t, cmd2.Execute())
	assert.Contains(t, out2.String(), "resnet50:1.1.0")

	// show the child version with its model card
	show := newModelShowCmd()
	showBuf := wireCmd(show)
	show.SetArgs([]string{"resnet50:1.1.0", "--registry", reg})
	require.NoError(t, show.Execute())
	s := showBuf.String()
	assert.Contains(t, s, "resnet50")
	assert.Contains(t, s, "1.1.0")
	assert.Contains(t, s, "Model card")
	assert.Contains(t, s, "1.0.0", "parent version must appear in show output")

	// lineage resolves both hops
	lin := newModelLineageCmd()
	linBuf := wireCmd(lin)
	lin.SetArgs([]string{"resnet50:1.1.0", "--registry", reg})
	require.NoError(t, lin.Execute())
	l := linBuf.String()
	assert.Contains(t, l, "resnet50:1.1.0")
	assert.Contains(t, l, "resnet50:1.0.0")
	assert.Contains(t, l, "depth 2")
}

// TestModelRegisterCmd_JSON emits machine-readable output for CI pipelines.
func TestModelRegisterCmd_JSON(t *testing.T) {
	reg := filepath.Join(t.TempDir(), "models")
	art := writeModelWeights(t, "json weights")

	cmd := newModelRegisterCmd()
	buf := wireCmd(cmd)
	cmd.SetArgs([]string{
		art, "--name", "jsonmodel", "--version", "1.0.0",
		"--registry", reg, "--output", "json",
	})
	require.NoError(t, cmd.Execute())

	var result map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result), "output must be valid JSON")
	assert.Equal(t, "jsonmodel", result["name"])
	assert.Equal(t, "1.0.0", result["version"])
	assert.NotEmpty(t, result["sha256"], "content hash must be present")
	assert.NotEmpty(t, result["attestation_hash"], "attestation hash must be present")
}

// TestModelRegisterCmd_DuplicateFails proves version immutability at the CLI.
func TestModelRegisterCmd_DuplicateFails(t *testing.T) {
	reg := filepath.Join(t.TempDir(), "models")
	art := writeModelWeights(t, "immutable weights")

	mustRegister(t, reg, "imm", "1.0.0", art)

	cmd := newModelRegisterCmd()
	errBuf := wireCmd(cmd)
	cmd.SetArgs([]string{art, "--name", "imm", "--version", "1.0.0", "--registry", reg})
	err := cmd.Execute()
	require.Error(t, err, "re-registering the same version must fail")
	assert.Contains(t, err.Error(), "already registered")
	assert.Contains(t, errBuf.String(), "already registered")
}

// TestModelRollbackCmd covers list, rollback, and latest pointer resolution.
func TestModelRollbackCmd(t *testing.T) {
	reg := filepath.Join(t.TempDir(), "models")
	w1 := writeModelWeights(t, "weights one")
	w2 := writeModelWeights(t, "weights two")

	mustRegister(t, reg, "rb", "1.0.0", w1)
	mustRegister(t, reg, "rb", "1.1.0", w2)

	// list shows both versions, newest first
	list := newModelListCmd()
	listBuf := wireCmd(list)
	list.SetArgs([]string{"--registry", reg})
	require.NoError(t, list.Execute())
	ls := listBuf.String()
	assert.Contains(t, ls, "1.0.0")
	assert.Contains(t, ls, "1.1.0")
	assert.Less(t, bytes.Index([]byte(ls), []byte("1.1.0")), bytes.Index([]byte(ls), []byte("1.0.0")),
		"newest version must be listed first")

	// rollback to 1.0.0
	rb := newModelRollbackCmd()
	rbBuf := wireCmd(rb)
	rb.SetArgs([]string{"rb", "--to", "1.0.0", "--registry", reg})
	require.NoError(t, rb.Execute())
	assert.Contains(t, rbBuf.String(), "1.0.0")
	assert.Contains(t, rbBuf.String(), "Attestation:")

	// latest now resolves to 1.0.0
	show := newModelShowCmd()
	showBuf := wireCmd(show)
	show.SetArgs([]string{"rb:latest", "--registry", reg})
	require.NoError(t, show.Execute())
	assert.Contains(t, showBuf.String(), "1.0.0")

	// a stale --from must be rejected (optimistic concurrency guard)
	rb2 := newModelRollbackCmd()
	wireCmd(rb2)
	rb2.SetArgs([]string{"rb", "--from", "1.1.0", "--to", "1.1.0", "--registry", reg})
	err := rb2.Execute()
	require.Error(t, err, "rollback with a stale --from must fail")
	assert.Contains(t, err.Error(), "conflict")
}

// TestModelRegisterCmd_NoAttest honors the dev-only skip flag.
func TestModelRegisterCmd_NoAttest(t *testing.T) {
	reg := filepath.Join(t.TempDir(), "models")
	art := writeModelWeights(t, "no-attest weights")

	cmd := newModelRegisterCmd()
	buf := wireCmd(cmd)
	cmd.SetArgs([]string{art, "--name", "dev", "--version", "1.0.0", "--registry", reg, "--no-attest"})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "skipped (--no-attest")
}
