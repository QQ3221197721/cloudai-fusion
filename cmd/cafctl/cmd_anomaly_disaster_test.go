// Package main - cafctl anomaly & disaster subcommand tests
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnomalyScanCmd(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		contains string
	}{
		{"default samples", []string{}, "Mahalanobis"},
		{"explicit samples", []string{"--samples", "12"}, "Scan complete."},
		{"single sample", []string{"--samples", "1"}, "anomaly scan"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newAnomalyScanCmd()
			buf := wireCmd(cmd)
			cmd.SetArgs(tt.args)
			require.NoError(t, cmd.Execute())
			assert.Contains(t, buf.String(), tt.contains)
		})
	}
}

// TestAnomalyScanCmd_Deterministic guards the fixed RNG seed (42): repeated runs
// with identical flags must produce byte-identical output.
func TestAnomalyScanCmd_Deterministic(t *testing.T) {
	results := make(map[string]bool)
	for i := 0; i < 10; i++ {
		cmd := newAnomalyScanCmd()
		buf := wireCmd(cmd)
		cmd.SetArgs([]string{"--samples", "20"})
		require.NoError(t, cmd.Execute())
		results[buf.String()] = true
	}
	assert.Len(t, results, 1, "seeded scan must be deterministic")
}

func TestAnomalyScanCmd_RejectsArgs(t *testing.T) {
	cmd := newAnomalyScanCmd()
	wireCmd(cmd)
	cmd.SetArgs([]string{"extra"})
	assert.Error(t, cmd.Execute())
}

func TestDisasterStatusCmd(t *testing.T) {
	cmd := newDisasterStatusCmd()
	buf := wireCmd(cmd)
	require.NoError(t, cmd.Execute())

	s := buf.String()
	assert.Contains(t, s, "backup & failover")
	assert.Contains(t, s, "Created demo backup:")
	assert.Contains(t, s, "Registered backups:")
}

func TestDisasterStatusCmd_RejectsArgs(t *testing.T) {
	cmd := newDisasterStatusCmd()
	wireCmd(cmd)
	cmd.SetArgs([]string{"extra"})
	assert.Error(t, cmd.Execute())
}
