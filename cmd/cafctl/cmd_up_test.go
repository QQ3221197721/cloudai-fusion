// Package main - `cafctl up` CLI tests.
//
// runUp writes every byte through cmd.OutOrStdout()/cmd.ErrOrStderr(), so these
// tests drive the real newUpCmd() with a captured buffer. The embedded local
// plane binds a real loopback socket, but only on ephemeral ports obtained from
// the OS (127.0.0.1:0), so nothing depends on a fixed port being free and no
// external network is touched. newUpCmd binds package-level flag globals, so the
// command-driving tests are deliberately NOT parallel.
package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ----------------------------------------------------------------------------
// Test helpers
// ----------------------------------------------------------------------------

// freePort binds an ephemeral loopback port, closes it, and returns the number.
// There is a tiny race between close and re-bind, but on loopback in a test it
// is reliable enough and avoids hard-coding a port that might be in use.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "must bind an ephemeral port")
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return port
}

// runUpCmd executes a freshly-constructed up command with args, capturing
// combined stdout+stderr. Not parallel-safe: newUpCmd binds package globals.
func runUpCmd(args ...string) (string, error) {
	cmd := newUpCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

// ----------------------------------------------------------------------------
// Test suite: command construction
// ----------------------------------------------------------------------------

func TestUp_CommandConstruction(t *testing.T) {
	cmd := newUpCmd()
	require.NotNil(t, cmd, "up command must be constructed")
	assert.Equal(t, "up [--local]", cmd.Use)
	require.NotNil(t, cmd.RunE)

	flags := cmd.Flags()
	for _, name := range []string{"local", "port", "dir", "smoke", "timeout", "json"} {
		assert.NotNil(t, flags.Lookup(name), "flag --%s must exist", name)
	}
	// Documented defaults: local mode on, port matches the status probe.
	assert.Equal(t, "true", flags.Lookup("local").DefValue, "default --local")
	assert.Equal(t, fmt.Sprint(defaultLocalPort), flags.Lookup("port").DefValue, "default --port")
}

// ----------------------------------------------------------------------------
// Test suite: happy path (--smoke starts, self-checks, and shuts down)
// ----------------------------------------------------------------------------

func TestUp_Smoke_HappyPath(t *testing.T) {
	port := freePort(t)
	out, err := runUpCmd("--local", "--smoke", "--port", fmt.Sprint(port), "--dir", t.TempDir())
	require.NoError(t, err, "smoke run must succeed; out=%s", out)
	assert.Contains(t, out, "Smoke run complete", "smoke completion banner shown")
	assert.Contains(t, out, fmt.Sprintf("http://127.0.0.1:%d", port), "bound base URL shown")
}

func TestUp_Smoke_JSONOutput(t *testing.T) {
	port := freePort(t)
	out, err := runUpCmd("--local", "--smoke", "--json", "--port", fmt.Sprint(port), "--dir", t.TempDir())
	require.NoError(t, err, "smoke --json must succeed; out=%s", out)
	for _, field := range []string{`"mode"`, `"base_url"`, `"boot_ms"`, `"signer_source"`, `"probes"`} {
		assert.Contains(t, out, field, "JSON report contains %s", field)
	}
}

// ----------------------------------------------------------------------------
// Test suite: error paths
// ----------------------------------------------------------------------------

func TestUp_Error_NoLocal(t *testing.T) {
	out, err := runUpCmd("--local=false", "--dir", t.TempDir())
	require.Error(t, err, "up without --local must fail")
	assert.Contains(t, err.Error(), "unsupported", "error explains the refusal")
	assert.Contains(t, out, "--local", "next-steps point back at the supported path")
}

func TestUp_Error_PortInUse(t *testing.T) {
	// Occupy a port for the whole test, then ask up to bind the same one.
	// localPlane.Start binds synchronously before serving, so the conflict
	// surfaces as a returned error rather than a background failure.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port

	out, err := runUpCmd("--local", "--smoke", "--port", fmt.Sprint(port), "--dir", t.TempDir())
	require.Error(t, err, "binding an occupied port must fail")
	assert.Contains(t, err.Error(), "bind", "error identifies the bind failure; out=%s", out)
}

// ----------------------------------------------------------------------------
// Test suite: determinism & lifecycle
// ----------------------------------------------------------------------------

func TestUp_Determinism_RepeatedSmoke(t *testing.T) {
	for i := 0; i < 3; i++ {
		port := freePort(t)
		out, err := runUpCmd("--local", "--smoke", "--port", fmt.Sprint(port), "--dir", t.TempDir())
		require.NoError(t, err, "iteration %d must succeed; out=%s", i, out)
		assert.Contains(t, out, "Smoke run complete", "iteration %d completes cleanly", i)
	}
}

// TestUp_LocalPlane_Lifecycle drives the plane directly (no globals) to assert
// the start → self-check → stop lifecycle the command depends on.
func TestUp_LocalPlane_Lifecycle(t *testing.T) {
	t.Parallel()

	port := freePort(t)
	plane, err := newLocalPlane(localPlaneConfig{Port: port, Dir: t.TempDir()})
	require.NoError(t, err, "newLocalPlane must succeed")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, plane.Start(ctx), "Start must bind and serve")
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer shutdownCancel()
		_ = plane.Stop(shutdownCtx)
	}()

	probes := plane.SelfCheck(ctx, 5*time.Second)
	require.Len(t, probes, 2, "two health probes expected (/healthz, /readyz)")
	for _, pr := range probes {
		assert.True(t, pr.OK(), "probe %s should pass", pr.URL)
	}
}
