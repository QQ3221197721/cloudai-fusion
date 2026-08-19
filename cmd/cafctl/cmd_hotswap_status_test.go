// Package main - cafctl hotswap subcommand tests (M52 Hot-swap State Migration).
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHotswapCmd tests the hotswap status command.
func TestHotswapCmd(t *testing.T) {
	cmd := newHotswapStatusCmd()
	buf := wireCmd(cmd)
	require.NoError(t, cmd.Execute())

	s := buf.String()
	t.Run("reports-version-management", func(t *testing.T) {
		assert.Contains(t, s, "component version management (M52)")
	})
	t.Run("shows-orchestrator-config", func(t *testing.T) {
		assert.Contains(t, s, "Orchestrator Configuration:")
		assert.Contains(t, s, "Drain Timeout:")
		assert.Contains(t, s, "Rollback Support:")
	})
	t.Run("shows-current-status", func(t *testing.T) {
		assert.Contains(t, s, "Current Orchestrator Status:")
		assert.Contains(t, s, "Current Component:")
		assert.Contains(t, s, "In-flight Requests:")
		assert.Contains(t, s, "Swap History:")
	})
	t.Run("reports-completion", func(t *testing.T) {
		assert.Contains(t, s, "Status report complete")
	})
}

// TestHotswapCmd_Deterministic ensures repeated runs are byte-identical across multiple invocations.
func TestHotswapCmd_Deterministic(t *testing.T) {
	results := make(map[string]bool)
	for i := 0; i < 10; i++ {
		cmd := newHotswapStatusCmd()
		buf := wireCmd(cmd)
		require.NoError(t, cmd.Execute())
		results[buf.String()] = true
	}
	assert.Len(t, results, 1, "repeated runs must be identical")
}

// TestHotswapCmd_RejectsArgs verifies the leaf command rejects extra args.
func TestHotswapCmd_RejectsArgs(t *testing.T) {
	cmd := newHotswapStatusCmd()
	wireCmd(cmd)
	cmd.SetArgs([]string{"extra", "arg"})
	assert.Error(t, cmd.Execute(), "should reject extra arguments")
}

// TestHotswapParentCmd verifies the parent command wires up its leaf subcommand.
func TestHotswapParentCmd(t *testing.T) {
	cmd := newHotswapCmd()
	assert.Equal(t, "hotswap", cmd.Use)
	assert.NotNil(t, cmd.Short)

	found := false
	for _, sub := range cmd.Commands() {
		if sub.Use == "status" || sub.Name() == "status" {
			found = true
		}
	}
	assert.True(t, found, "expected 'hotswap' to have subcommand 'status'")
}
