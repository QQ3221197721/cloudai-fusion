// Package main - cafctl billing subcommand tests
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBillingUsageCmd(t *testing.T) {
	cmd := newBillingUsageCmd()
	buf := wireCmd(cmd)
	require.NoError(t, cmd.Execute())

	s := buf.String()
	assert.Contains(t, s, "compute")
	assert.Contains(t, s, "storage")
	assert.Contains(t, s, "bandwidth")
	assert.Contains(t, s, "gpu")
}

func TestBillingUsageCmd_Filter(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		filter string
	}{
		{"no filter", []string{}, ""},
		{"compute only", []string{"--resource", "compute"}, "compute"},
		{"gpu only", []string{"--resource", "gpu"}, "gpu"},
		{"invalid resource", []string{"--resource", "invalid"}, "invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newBillingUsageCmd()
			buf := wireCmd(cmd)
			cmd.SetArgs(tt.args)
			require.NoError(t, cmd.Execute())

			s := buf.String()
			if tt.filter != "" {
				assert.Contains(t, s, tt.filter)
			}
		})
	}
}

func TestBillingUsageCmd_Deterministic(t *testing.T) {
	results := make(map[string]bool)
	for i := 0; i < 10; i++ {
		cmd := newBillingUsageCmd()
		buf := wireCmd(cmd)
		require.NoError(t, cmd.Execute())
		results[buf.String()] = true
	}
	assert.Len(t, results, 1, "repeated runs must be identical")
}

func TestBillingUsageCmd_RejectsArgs(t *testing.T) {
	cmd := newBillingUsageCmd()
	wireCmd(cmd)
	cmd.SetArgs([]string{"extra", "arg"})
	err := cmd.Execute()
	assert.Error(t, err)
}
