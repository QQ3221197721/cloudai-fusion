// Package main - cafctl hunt / detect / soar subcommand tests.
package main

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSecurityWellCmds is a table-driven test over the three AISecOps deep-well
// subcommands. Each verifies the command runs cleanly and reports the real,
// engine-derived facts a user needs (matched rule, selected playbook, anomaly).
func TestSecurityWellCmds(t *testing.T) {
	tests := []struct {
		name         string
		newCmd       func() *cobra.Command
		wantContains []string
	}{
		{
			name:   "hunt run reports UEBA anomaly",
			newCmd: newHuntRunCmd,
			wantContains: []string{
				"UEBA behavioral analysis",
				"Baseline trained: 30",
				"Anomalies found:  1",
				"T1048",           // exfiltration technique for bytes_out anomaly
				"user:alice",      // entity in the finding title
			},
		},
		{
			name:   "detect sigma matches embedded rule",
			newCmd: newDetectSigmaCmd,
			wantContains: []string{
				"Sigma rule evaluation",
				"Rules loaded: 12",
				"Matches:      1",
				"T1059.001",                             // PowerShell encoded command
				"PowerShell EncodedCommand Execution",
			},
		},
		{
			name:   "soar trigger selects playbook",
			newCmd: newSoarTriggerCmd,
			wantContains: []string{
				"response orchestration",
				"Playbook: c2-egress",
				"Executed: true",
				"block-network",
				"isolate-host",
				"notify",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := tc.newCmd()
			buf := wireCmd(cmd)
			require.NoError(t, cmd.Execute())

			s := buf.String()
			for _, want := range tc.wantContains {
				assert.Contains(t, s, want)
			}
		})
	}
}

// TestSecurityWellCmds_Deterministic ensures repeated runs are byte-identical
// (timestamps and UUIDs are intentionally not printed).
func TestSecurityWellCmds_Deterministic(t *testing.T) {
	factories := map[string]func() *cobra.Command{
		"hunt-run":     newHuntRunCmd,
		"detect-sigma": newDetectSigmaCmd,
		"soar-trigger": newSoarTriggerCmd,
	}
	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			results := make(map[string]bool)
			for i := 0; i < 10; i++ {
				cmd := factory()
				buf := wireCmd(cmd)
				require.NoError(t, cmd.Execute())
				results[buf.String()] = true
			}
			assert.Len(t, results, 1, "repeated runs must be identical")
		})
	}
}

// TestSecurityWellCmds_RejectsArgs verifies each leaf command rejects extra args.
func TestSecurityWellCmds_RejectsArgs(t *testing.T) {
	factories := map[string]func() *cobra.Command{
		"hunt-run":     newHuntRunCmd,
		"detect-sigma": newDetectSigmaCmd,
		"soar-trigger": newSoarTriggerCmd,
	}
	for name, factory := range factories {
		t.Run(name, func(t *testing.T) {
			cmd := factory()
			wireCmd(cmd)
			cmd.SetArgs([]string{"extra", "arg"})
			assert.Error(t, cmd.Execute())
		})
	}
}

// TestSecurityWellParentCmds verifies the parent commands wire up their leaf
// subcommands (hunt→run, detect→sigma, soar→trigger).
func TestSecurityWellParentCmds(t *testing.T) {
	tests := []struct {
		parent func() *cobra.Command
		use    string
		leaf   string
	}{
		{newHuntCmd, "hunt", "run"},
		{newDetectCmd, "detect", "sigma"},
		{newSoarCmd, "soar", "trigger"},
	}
	for _, tc := range tests {
		t.Run(tc.use, func(t *testing.T) {
			cmd := tc.parent()
			assert.Equal(t, tc.use, cmd.Use)
			var found bool
			for _, sub := range cmd.Commands() {
				if sub.Use == tc.leaf {
					found = true
				}
			}
			assert.True(t, found, "expected %q to have subcommand %q", tc.use, tc.leaf)
		})
	}
}
