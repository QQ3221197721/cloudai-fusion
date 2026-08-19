// Package main - cafctl alerting & tracing subcommand tests
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAlertingListCmd(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		contains string
	}{
		{"no filter", []string{}, "Active routes: 4"},
		{"critical filter", []string{"--severity", "CRITICAL"}, "Active routes: 1"},
		{"high shorthand", []string{"-s", "HIGH"}, "pagerduty"},
		{"unknown filter", []string{"--severity", "ZZZ"}, "Active routes: 0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newAlertingListCmd()
			buf := wireCmd(cmd)
			cmd.SetArgs(tt.args)
			require.NoError(t, cmd.Execute())
			assert.Contains(t, buf.String(), tt.contains)
		})
	}
}

func TestAlertingListCmd_Deterministic(t *testing.T) {
	results := make(map[string]bool)
	for i := 0; i < 10; i++ {
		cmd := newAlertingListCmd()
		buf := wireCmd(cmd)
		require.NoError(t, cmd.Execute())
		results[buf.String()] = true
	}
	assert.Len(t, results, 1, "route listing must be deterministic")
}

func TestAlertingListCmd_RejectsArgs(t *testing.T) {
	cmd := newAlertingListCmd()
	wireCmd(cmd)
	cmd.SetArgs([]string{"extra"})
	assert.Error(t, cmd.Execute())
}

func TestTracingShowCmd(t *testing.T) {
	cmd := newTracingShowCmd()
	buf := wireCmd(cmd)
	require.NoError(t, cmd.Execute())

	s := buf.String()
	assert.Contains(t, s, "distributed tracing")
	assert.Contains(t, s, "Tracer active:")
	assert.Contains(t, s, "cafctl-demo")
}

func TestTracingShowCmd_RejectsArgs(t *testing.T) {
	cmd := newTracingShowCmd()
	wireCmd(cmd)
	cmd.SetArgs([]string{"extra"})
	assert.Error(t, cmd.Execute())
}
