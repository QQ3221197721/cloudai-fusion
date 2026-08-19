// Package main - cafctl audit subcommand tests
package main

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tsColumn matches the second-precision timestamp column emitted by
// `audit export`, which is real runtime data (time.Now) and therefore varies
// between runs. Determinism assertions canonicalize it away.
var tsColumn = regexp.MustCompile(`\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}`)

func TestAuditExportCmd(t *testing.T) {
	cmd := newAuditExportCmd()
	buf := wireCmd(cmd)
	require.NoError(t, cmd.Execute())
	
	s := buf.String()
	assert.Contains(t, s, "login")
	assert.Contains(t, s, "create_workload")
	assert.Contains(t, s, "delete_cluster")
}

func TestAuditExportCmd_Repeated(t *testing.T) {
	results := make(map[string]bool)
	for i := 0; i < 10; i++ {
		cmd := newAuditExportCmd()
		buf := wireCmd(cmd)
		require.NoError(t, cmd.Execute())
		// Strip the timestamp column so we compare the stable event schema.
		results[tsColumn.ReplaceAllString(buf.String(), "<ts>")] = true
	}
	assert.Len(t, results, 1, "repeated runs must be identical modulo timestamps")
}

func TestAuditQueryCmd(t *testing.T) {
	tests := []struct{
		name string
		args []string
	}{
		{"no_filter", []string{}},
		{"auth_category", []string{"--category", "authentication"}},
		{"workload_category", []string{"--category", "workload"}},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newAuditQueryCmd()
			wireCmd(cmd)
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			assert.NoError(t, err)
		})
	}
}
