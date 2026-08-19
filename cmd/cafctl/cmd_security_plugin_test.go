// Package main - cafctl security & plugin subcommand tests
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecurityScanCmd(t *testing.T) {
	cmd := newSecurityScanCmd()
	buf := wireCmd(cmd)
	require.NoError(t, cmd.Execute())

	s := buf.String()
	assert.Contains(t, s, "WAF engine")
	assert.Contains(t, s, "Compliance")
}

func TestSecurityScanCmd_Deterministic(t *testing.T) {
	results := make(map[string]bool)
	for i := 0; i < 10; i++ {
		cmd := newSecurityScanCmd()
		buf := wireCmd(cmd)
		require.NoError(t, cmd.Execute())
		results[buf.String()] = true
	}
	assert.Len(t, results, 1, "repeated runs must be identical")
}

func TestSecurityScanCmd_RejectsArgs(t *testing.T) {
	cmd := newSecurityScanCmd()
	wireCmd(cmd)
	cmd.SetArgs([]string{"extra"})
	assert.Error(t, cmd.Execute())
}

func TestPluginListCmd(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		contains string
	}{
		{"no filter", []string{}, "chain"},
		{"admission filter", []string{"--filter", "admission"}, "ADMISSION-CHAIN"},
		{"unknown filter", []string{"--filter", "nonexistent"}, "no chains match"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newPluginListCmd()
			buf := wireCmd(cmd)
			cmd.SetArgs(tt.args)
			require.NoError(t, cmd.Execute())
			assert.Contains(t, buf.String(), tt.contains)
		})
	}
}

func TestPluginListCmd_RejectsArgs(t *testing.T) {
	cmd := newPluginListCmd()
	wireCmd(cmd)
	cmd.SetArgs([]string{"extra"})
	assert.Error(t, cmd.Execute())
}
