// Package main - cafctl sandbox subcommand tests (M42 Sandbox Security Scanner).
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSandboxCmd tests the sandbox run command.
func TestSandboxCmd(t *testing.T) {
	cmd := newSandboxRunCmd()
	buf := wireCmd(cmd)
	require.NoError(t, cmd.Execute())

	s := buf.String()
	t.Run("reports-scanner-engine", func(t *testing.T) {
		assert.Contains(t, s, "sandbox security scanning engine (M42)")
	})
	t.Run("shows-configuration", func(t *testing.T) {
		assert.Contains(t, s, "Scan Configuration:")
		assert.Contains(t, s, "Artifact Name:")
		assert.Contains(t, s, "Profile:")
		assert.Contains(t, s, "Memory Limit:")
		assert.Contains(t, s, "CPU Limit:")
	})
	t.Run("lists-artifacts", func(t *testing.T) {
		assert.Contains(t, s, "Artifact List:")
		assert.Contains(t, s, "/app/plugins/")
		assert.Contains(t, s, "SHA256:")
	})
	t.Run("reports-static-analysis", func(t *testing.T) {
		assert.Contains(t, s, "Static Analysis Results:")
		assert.Contains(t, s, "No dangerous imports detected")
		assert.Contains(t, s, "No banned patterns found")
	})
	t.Run("checks-permission-boundaries", func(t *testing.T) {
		assert.Contains(t, s, "Permission Boundary Check:")
		assert.Contains(t, s, "Role:")
		assert.Contains(t, s, "Denied:")
	})
	t.Run("shows-execution-isolation", func(t *testing.T) {
		assert.Contains(t, s, "Execution Isolation Status:")
	})
	t.Run("summarizes-report", func(t *testing.T) {
		assert.Contains(t, s, "Report Summary:")
		assert.Contains(t, s, "Total Findings:")
		assert.Contains(t, s, "Secure:")
		assert.Contains(t, s, "Pass:")
	})
	t.Run("reports-success", func(t *testing.T) {
		assert.Contains(t, s, "Security scan complete")
		assert.Contains(t, s, OK(), "success symbol not found in output")
	})
}

// TestSandboxCmd_Deterministic ensures repeated runs are byte-identical across multiple invocations.
func TestSandboxCmd_Deterministic(t *testing.T) {
	results := make(map[string]bool)
	for i := 0; i < 10; i++ {
		cmd := newSandboxRunCmd()
		buf := wireCmd(cmd)
		require.NoError(t, cmd.Execute())
		results[buf.String()] = true
	}
	assert.Len(t, results, 1, "repeated runs must be identical")
}

// TestSandboxCmd_RejectsArgs verifies the leaf command rejects extra args.
func TestSandboxCmd_RejectsArgs(t *testing.T) {
	cmd := newSandboxRunCmd()
	wireCmd(cmd)
	cmd.SetArgs([]string{"extra", "arg"})
	assert.Error(t, cmd.Execute(), "should reject extra arguments")
}
