// Package main - cafctl gitops subcommand tests
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitopsDriftCmd(t *testing.T) {
	cmd := newGitopsDriftCmd()
	buf := wireCmd(cmd)
	require.NoError(t, cmd.Execute())

	s := buf.String()
	assert.Contains(t, s, "Deployment")
	assert.Contains(t, s, "drift")
}

func TestGitopsDriftCmd_AppName(t *testing.T) {
	tests := []struct{
		name string
		args []string
		contains string
	}{
		{"no app", []string{}, "-"}, // orDash placeholder
		{"with app", []string{"--app", "my-app"}, "my-app"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newGitopsDriftCmd()
			buf := wireCmd(cmd)
			cmd.SetArgs(tt.args)
			require.NoError(t, cmd.Execute())

			s := buf.String()
			assert.Contains(t, s, tt.contains)
		})
	}
}

func TestGitopsDriftCmd_Deterministic(t *testing.T) {
	results := make(map[string]bool)
	for i := 0; i < 10; i++ {
		cmd := newGitopsDriftCmd()
		buf := wireCmd(cmd)
		require.NoError(t, cmd.Execute())
		results[buf.String()] = true
	}
	assert.Len(t, results, 1, "repeated runs must be identical")
}

func TestGitopsDriftCmd_RejectsArgs(t *testing.T) {
	cmd := newGitopsDriftCmd()
	wireCmd(cmd)
	cmd.SetArgs([]string{"extra", "arg"})
	err := cmd.Execute()
	assert.Error(t, err)
}
