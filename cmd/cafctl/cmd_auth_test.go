// Package main - cafctl auth subcommand tests.
//
// These build parent-less command instances via the newXxxCmd() constructors
// (the run/verify-* pattern) and drive them through Execute with an in-memory
// auth.Service — no network, no database. The happy path signs a real JWT with
// auth.GenerateToken so check-token verifies a genuine token end to end.
package main

import (
	"strings"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mkToken signs a real JWT for the given secret/role using the same in-memory
// service the CLI uses, so check-token validates a genuine token.
func mkToken(t *testing.T, secret string, role auth.Role) string {
	t.Helper()
	svc, err := auth.NewService(auth.Config{JWTSecret: secret, JWTExpiry: time.Hour})
	require.NoError(t, err)
	resp, err := svc.GenerateToken(&auth.User{
		ID: "u-123", Username: "alice", Email: "alice@example.com",
		Role: role, Status: "active", CreatedAt: time.Now().UTC(),
	})
	require.NoError(t, err)
	return resp.AccessToken
}

// TestAuthCheckTokenCmd covers happy-path validation and error paths.
func TestAuthCheckTokenCmd(t *testing.T) {
	const secret = "test-secret-0123456789"
	valid := mkToken(t, secret, auth.RoleOperator)

	tests := []struct {
		name       string
		args       []string
		wantErr    bool
		wantOutSub string // substring expected in combined output
	}{
		{
			name:       "happy_path_valid_token",
			args:       []string{valid, "--secret", secret},
			wantErr:    false,
			wantOutSub: "token valid",
		},
		{
			name:       "wrong_secret",
			args:       []string{valid, "--secret", "the-wrong-secret"},
			wantErr:    true,
			wantOutSub: "invalid token",
		},
		{
			name:       "malformed_token",
			args:       []string{"not.a.jwt", "--secret", secret},
			wantErr:    true,
			wantOutSub: "invalid token",
		},
		{
			name:    "missing_secret_flag",
			args:    []string{valid},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newAuthCheckTokenCmd()
			buf := wireCmd(cmd)
			cmd.SetArgs(tt.args)
			err := cmd.Execute()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tt.wantOutSub != "" {
				assert.Contains(t, buf.String(), tt.wantOutSub)
			}
		})
	}
}

// TestAuthCheckTokenCmd_Claims proves genuine claims are surfaced.
func TestAuthCheckTokenCmd_Claims(t *testing.T) {
	const secret = "claims-secret-xyz"
	tok := mkToken(t, secret, auth.RoleAdmin)

	cmd := newAuthCheckTokenCmd()
	buf := wireCmd(cmd)
	cmd.SetArgs([]string{tok, "--secret", secret})
	require.NoError(t, cmd.Execute())

	s := buf.String()
	assert.Contains(t, s, "alice")
	assert.Contains(t, s, "alice@example.com")
	assert.Contains(t, s, string(auth.RoleAdmin))
	assert.Contains(t, s, "ExpiresAt:")
}

// TestAuthRolesCmd validates the RBAC matrix construction and content.
func TestAuthRolesCmd(t *testing.T) {
	cmd := newAuthRolesCmd()
	buf := wireCmd(cmd)
	require.NoError(t, cmd.Execute())

	s := buf.String()
	assert.Contains(t, s, "PERMISSION")
	assert.Contains(t, s, string(auth.RoleAdmin))
	assert.Contains(t, s, string(auth.RoleViewer))
	assert.Contains(t, s, string(auth.PermClusterRead))
	// admin has cluster:create, viewer does not — matrix must show at least one ✓ and one -
	assert.Contains(t, s, "✓")
	assert.Contains(t, s, "-")
}

// TestAuthRolesCmd_Deterministic runs the command repeatedly to guard against
// map-iteration nondeterminism (permissions are sorted before rendering).
func TestAuthRolesCmd_Deterministic(t *testing.T) {
	seen := map[string]struct{}{}
	for i := 0; i < 10; i++ {
		cmd := newAuthRolesCmd()
		buf := wireCmd(cmd)
		require.NoError(t, cmd.Execute())
		seen[buf.String()] = struct{}{}
	}
	assert.Len(t, seen, 1, "roles output must be byte-identical across runs")
}

// TestAuthRolesCmd_RejectsArgs ensures the NoArgs guard is wired.
func TestAuthRolesCmd_RejectsArgs(t *testing.T) {
	cmd := newAuthRolesCmd()
	wireCmd(cmd)
	cmd.SetArgs([]string{"unexpected"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "unknown command") ||
		strings.Contains(err.Error(), "arg"), "unexpected arg must be rejected")
}
