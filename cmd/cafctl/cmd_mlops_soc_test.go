// Package main - cafctl mlops & soc subcommand tests
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMlopsMonitorCmd(t *testing.T) {
	cmd := newMlopsMonitorCmd()
	buf := wireCmd(cmd)
	require.NoError(t, cmd.Execute())

	s := buf.String()
	assert.Contains(t, s, "accuracy")
	assert.Contains(t, s, "drift")
}

func TestMlopsMonitorCmd_Deterministic(t *testing.T) {
	results := make(map[string]bool)
	for i := 0; i < 10; i++ {
		cmd := newMlopsMonitorCmd()
		buf := wireCmd(cmd)
		require.NoError(t, cmd.Execute())
		results[buf.String()] = true
	}
	assert.Len(t, results, 1, "repeated runs must be identical")
}

func TestMlopsMonitorCmd_RejectsArgs(t *testing.T) {
	cmd := newMlopsMonitorCmd()
	wireCmd(cmd)
	cmd.SetArgs([]string{"extra", "arg"})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestSocScanCmd(t *testing.T) {
	cmd := newSocScanCmd()
	buf := wireCmd(cmd)
	require.NoError(t, cmd.Execute())

	s := buf.String()
	assert.Contains(t, s, "endpoint")
	assert.Contains(t, s, "network")
	assert.Contains(t, s, "workload")
	assert.Contains(t, s, "identity")
	assert.Contains(t, s, "image")
}

func TestSocScanCmd_Deterministic(t *testing.T) {
	results := make(map[string]bool)
	for i := 0; i < 10; i++ {
		cmd := newSocScanCmd()
		buf := wireCmd(cmd)
		require.NoError(t, cmd.Execute())
		results[buf.String()] = true
	}
	assert.Len(t, results, 1, "repeated runs must be identical")
}

func TestSocScanCmd_RejectsArgs(t *testing.T) {
	cmd := newSocScanCmd()
	wireCmd(cmd)
	cmd.SetArgs([]string{"extra", "arg"})
	err := cmd.Execute()
	assert.Error(t, err)
}
