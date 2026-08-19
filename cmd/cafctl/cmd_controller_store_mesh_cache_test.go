// Package main - cafctl controller, store, mesh & cache subcommand tests
package main

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestControllerQueueCmd(t *testing.T) {
	cmd := newControllerQueueCmd()
	buf := wireCmd(cmd)
	require.NoError(t, cmd.Execute())

	s := buf.String()
	assert.Contains(t, s, "reconciliation")
	assert.Contains(t, s, "Work queue initialized")
}

func TestControllerQueueCmd_RejectsArgs(t *testing.T) {
	cmd := newControllerQueueCmd()
	wireCmd(cmd)
	cmd.SetArgs([]string{"extra"})
	assert.Error(t, cmd.Execute())
}

func TestStoreStatsCmd(t *testing.T) {
	cmd := newStoreStatsCmd()
	buf := wireCmd(cmd)
	require.NoError(t, cmd.Execute())

	s := buf.String()
	assert.Contains(t, s, "data operations")
	assert.Contains(t, s, "Query predictor active")
}

func TestStoreStatsCmd_RejectsArgs(t *testing.T) {
	cmd := newStoreStatsCmd()
	wireCmd(cmd)
	cmd.SetArgs([]string{"extra"})
	assert.Error(t, cmd.Execute())
}

func TestMeshRoutesCmd(t *testing.T) {
	cmd := newMeshRoutesCmd()
	buf := wireCmd(cmd)
	require.NoError(t, cmd.Execute())

	s := buf.String()
	assert.Contains(t, s, "service mesh")
	assert.Contains(t, s, "Endpoint registry created")
	assert.Contains(t, s, "round-robin")
}

func TestMeshRoutesCmd_RejectsArgs(t *testing.T) {
	cmd := newMeshRoutesCmd()
	wireCmd(cmd)
	cmd.SetArgs([]string{"extra"})
	assert.Error(t, cmd.Execute())
}

func TestCacheInfoCmd(t *testing.T) {
	cmd := newCacheInfoCmd()
	buf := wireCmd(cmd)
	require.NoError(t, cmd.Execute())

	s := buf.String()
	assert.Contains(t, s, "caching system")
	assert.Contains(t, s, "Adaptive TTL manager configured")
}

func TestCacheInfoCmd_RejectsArgs(t *testing.T) {
	cmd := newCacheInfoCmd()
	wireCmd(cmd)
	cmd.SetArgs([]string{"extra"})
	assert.Error(t, cmd.Execute())
}

// TestControllerStoreMeshCache_Deterministic pins the offline output of all four
// data-plane inspectors so repeated invocations stay byte-identical.
func TestControllerStoreMeshCache_Deterministic(t *testing.T) {
	builders := map[string]func() *cobra.Command{
		"controller queue": newControllerQueueCmd,
		"store stats":       newStoreStatsCmd,
		"mesh routes":       newMeshRoutesCmd,
		"cache info":        newCacheInfoCmd,
	}
	run := func(build func() *cobra.Command) string {
		cmd := build()
		buf := wireCmd(cmd)
		require.NoError(t, cmd.Execute())
		return buf.String()
	}
	for name, build := range builders {
		t.Run(name, func(t *testing.T) {
			first := run(build)
			for i := 0; i < 9; i++ {
				assert.Equal(t, first, run(build), "output must be deterministic")
			}
		})
	}
}
