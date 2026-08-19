// Package main - cafctl correlation & cluster subcommand tests
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCorrelationGraphCmd(t *testing.T) {
	cmd := newCorrelationGraphCmd()
	buf := wireCmd(cmd)
	require.NoError(t, cmd.Execute())

	s := buf.String()
	assert.Contains(t, s, "service topology")
	assert.Contains(t, s, "Known services:")
	assert.Contains(t, s, "api")
}

func TestCorrelationGraphCmd_Deterministic(t *testing.T) {
	results := make(map[string]bool)
	for i := 0; i < 10; i++ {
		cmd := newCorrelationGraphCmd()
		buf := wireCmd(cmd)
		require.NoError(t, cmd.Execute())
		results[buf.String()] = true
	}
	assert.Len(t, results, 1, "topology rendering must be deterministic")
}

func TestCorrelationGraphCmd_RejectsArgs(t *testing.T) {
	cmd := newCorrelationGraphCmd()
	wireCmd(cmd)
	cmd.SetArgs([]string{"extra"})
	assert.Error(t, cmd.Execute())
}

func TestClusterStatusCmd(t *testing.T) {
	cmd := newClusterStatusCmd()
	buf := wireCmd(cmd)
	require.NoError(t, cmd.Execute())

	s := buf.String()
	assert.Contains(t, s, "multi-cluster orchestration")
	assert.Contains(t, s, "Cluster manager initialized")
}

func TestClusterStatusCmd_RejectsArgs(t *testing.T) {
	cmd := newClusterStatusCmd()
	wireCmd(cmd)
	cmd.SetArgs([]string{"extra"})
	assert.Error(t, cmd.Execute())
}
