// Package main - cafctl edge subcommand tests (M24 CRDT Conflict Resolution + M25 Discovery + M26 Provisioning).
package main

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEdgeCmds is a table-driven test over all three leaf commands.
func TestEdgeCmds(t *testing.T) {
	tests := []struct {
		name         string
		newCmd       func() *cobra.Command
		wantContains []string
	}{
		{
			name:   "edge resolve reports CRDT demo results",
			newCmd: newEdgeResolveCmd,
			wantContains: []string{
				"CRDT conflict resolution engine (M24)",
				"G-Counter",
				"PN-Counter",
				"LWW Register",
				"OR-Set",
				"converged correctly",
				"Conflict resolution demonstration complete",
			},
		},
		{
			name:   "edge discover reports discovery summary and devices",
			newCmd: newEdgeDiscoverCmd,
			wantContains: []string{
				"edge device discovery (M25)",
				"Discovery Summary:",
				"Total:",
				"Active:",
				"Discovered Devices:",
			},
		},
		{
			name:   "edge provision reports provisioning result and hardware spec",
			newCmd: newEdgeProvisionCmd,
			wantContains: []string{
				"remote provisioning (M26)",
				"Node ID:",
				"Hardware Specification:",
				"CPU:",
				"Memory:",
				"GPU:",
				"Next Steps:",
				"Provisioning successful",
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
				assert.Contains(t, s, want, "expected output to contain %q in:\n%s", want, s)
			}
		})
	}
}

// TestEdgeCmds_Deterministic ensures repeated runs are byte-identical across multiple invocations.
func TestEdgeCmds_Deterministic(t *testing.T) {
	factories := map[string]func() *cobra.Command{
		"edge-resolve":      newEdgeResolveCmd,
		"edge-discover":     newEdgeDiscoverCmd,
		"edge-provision":    newEdgeProvisionCmd,
	}
	for name, factory := range factories {
		t.Run(name+"-deterministic", func(t *testing.T) {
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

// TestEdgeCmds_RejectsArgs verifies each leaf command rejects extra args.
func TestEdgeCmds_RejectsArgs(t *testing.T) {
	factories := map[string]func() *cobra.Command{
		"edge-resolve":      newEdgeResolveCmd,
		"edge-discover":     newEdgeDiscoverCmd,
		"edge-provision":    newEdgeProvisionCmd,
	}
	for name, factory := range factories {
		t.Run(name+"-rejects-extra-args", func(t *testing.T) {
			cmd := factory()
			wireCmd(cmd)
			cmd.SetArgs([]string{"extra", "arg"})
			assert.Error(t, cmd.Execute(), "should reject extra arguments")
		})
	}
}

// TestEdgeParentCmd verifies the parent command wires up its leaf subcommands.
func TestEdgeParentCmd(t *testing.T) {
	cmd := edgeCmd
	assert.Equal(t, "edge", cmd.Use)
	assert.NotNil(t, cmd.Short)

	expected := map[string]bool{"resolve": false, "discover": false, "provision": false}
	for _, sub := range cmd.Commands() {
		if _, ok := expected[sub.Use]; ok {
			expected[sub.Use] = true
		}
	}
	for leaf, found := range expected {
		assert.True(t, found, "expected 'edge' to have subcommand %q", leaf)
	}
}
