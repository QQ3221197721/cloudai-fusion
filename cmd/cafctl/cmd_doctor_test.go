// Package main - `cafctl doctor` CLI tests.
//
// doctor deliberately splits decision logic from I/O: collectDoctorChecks wires
// the real OS probes into a set of pure check functions (checkGoToolchain,
// checkPortAvailable, checkWorkspaceWritable, checkProjectScaffold,
// checkOptionalTool). Those pure functions take injected probes, so the bulk of
// the coverage here is deterministic table-driven unit tests with fakes — no
// real ports, files, PATH lookups or network. A handful of integration tests
// then drive the real newDoctorCmd() against a temp dir and capture its buffer.
package main

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ----------------------------------------------------------------------------
// Test helpers
// ----------------------------------------------------------------------------

// fakeListenerOK returns a listen func that binds an ephemeral loopback port,
// modelling "port is free". checkPortAvailable closes the returned listener.
func fakeListenerOK() func(network, address string) (net.Listener, error) {
	return func(network, address string) (net.Listener, error) {
		return net.Listen("tcp", "127.0.0.1:0")
	}
}

// fakeListenerBusy returns a listen func that always reports the port is taken.
func fakeListenerBusy() func(network, address string) (net.Listener, error) {
	return func(network, address string) (net.Listener, error) {
		return nil, fmt.Errorf("listen %s: bind: address already in use", address)
	}
}

// runDoctorCmd executes the real doctor command with args, capturing combined
// stdout+stderr. Not parallel-safe: newDoctorCmd binds package globals.
func runDoctorCmd(args ...string) (string, error) {
	cmd := newDoctorCmd()
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

func TestDoctor_CommandConstruction(t *testing.T) {
	t.Parallel()

	cmd := newDoctorCmd()
	require.NotNil(t, cmd)
	assert.Equal(t, "doctor", cmd.Use)
	require.NotNil(t, cmd.RunE)

	flags := cmd.Flags()
	for _, name := range []string{"port", "dir", "json", "strict"} {
		assert.NotNil(t, flags.Lookup(name), "flag --%s must exist", name)
	}
}

// ----------------------------------------------------------------------------
// Test suite: parseGoVersion (pure)
// ----------------------------------------------------------------------------

func TestDoctor_ParseGoVersion(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in          string
		major       int
		minor       int
		ok          bool
	}{
		{"go1.26.5", 1, 26, true},
		{"go1.21", 1, 21, true},
		{"go1.24rc1", 1, 24, true},
		{"  go1.22-devel  ", 1, 22, true},
		{"not-a-version", 0, 0, false},
		{"go1", 0, 0, false},
	}
	for _, tc := range cases {
		major, minor, ok := parseGoVersion(tc.in)
		assert.Equal(t, tc.ok, ok, "ok for %q", tc.in)
		if tc.ok {
			assert.Equal(t, tc.major, major, "major for %q", tc.in)
			assert.Equal(t, tc.minor, minor, "minor for %q", tc.in)
		}
	}
}

// ----------------------------------------------------------------------------
// Test suite: checkGoToolchain (pure)
// ----------------------------------------------------------------------------

func TestDoctor_CheckGoToolchain(t *testing.T) {
	t.Parallel()

	assert.Equal(t, doctorPass, checkGoToolchain("go1.26.5").Status, "current Go passes")
	assert.Equal(t, doctorPass, checkGoToolchain("go1.21.0").Status, "floor version passes")

	old := checkGoToolchain("go1.20.9")
	assert.Equal(t, doctorFail, old.Status, "pre-floor Go fails")
	assert.Contains(t, old.Detail, "older than the go1.21 build floor")

	bad := checkGoToolchain("totally-bogus")
	assert.Equal(t, doctorWarn, bad.Status, "unparseable version warns, never fails")
}

// ----------------------------------------------------------------------------
// Test suite: checkPortAvailable (pure, injected listener)
// ----------------------------------------------------------------------------

func TestDoctor_CheckPortAvailable(t *testing.T) {
	t.Parallel()

	free := checkPortAvailable(8080, fakeListenerOK())
	assert.Equal(t, doctorPass, free.Status, "free port passes")
	assert.Contains(t, free.Detail, "is free")

	busy := checkPortAvailable(8080, fakeListenerBusy())
	assert.Equal(t, doctorWarn, busy.Status, "busy port warns (never fails: --port moves the plane)")
	assert.Contains(t, busy.Fix, "--port", "fix suggests another port")
}

// ----------------------------------------------------------------------------
// Test suite: checkWorkspaceWritable (pure, injected probe)
// ----------------------------------------------------------------------------

func TestDoctor_CheckWorkspaceWritable(t *testing.T) {
	t.Parallel()

	ok := checkWorkspaceWritable(t.TempDir(), func(string) error { return nil })
	assert.Equal(t, doctorPass, ok.Status, "writable dir passes")

	denied := checkWorkspaceWritable("/some/dir", func(string) error { return os.ErrPermission })
	assert.Equal(t, doctorFail, denied.Status, "unwritable workspace is a hard failure")
	assert.Contains(t, denied.Fix, "--dir")
}

// ----------------------------------------------------------------------------
// Test suite: checkProjectScaffold (pure, injected stat)
// ----------------------------------------------------------------------------

func TestDoctor_CheckProjectScaffold(t *testing.T) {
	t.Parallel()

	// Both .caf and the key resolve -> pass.
	all := checkProjectScaffold(".", func(string) (os.FileInfo, error) { return nil, nil })
	assert.Equal(t, doctorPass, all.Status, "scaffold + key present passes")

	// Nothing resolves -> warn about missing .caf.
	none := checkProjectScaffold(".", func(string) (os.FileInfo, error) { return nil, os.ErrNotExist })
	assert.Equal(t, doctorWarn, none.Status, "missing scaffold warns, never fails")
	assert.Contains(t, none.Detail, "not initialized")
	assert.Contains(t, none.Fix, "cafctl init")

	// .caf resolves but the key does not -> warn about the missing key.
	calls := 0
	keyMissing := checkProjectScaffold(".", func(string) (os.FileInfo, error) {
		calls++
		if calls == 1 {
			return nil, nil // .caf dir exists
		}
		return nil, os.ErrNotExist // key missing
	})
	assert.Equal(t, doctorWarn, keyMissing.Status, "missing signing key warns")
	assert.Contains(t, keyMissing.Detail, "signing key is missing")
}

// ----------------------------------------------------------------------------
// Test suite: checkOptionalTool (pure, injected lookPath)
// ----------------------------------------------------------------------------

func TestDoctor_CheckOptionalTool(t *testing.T) {
	t.Parallel()

	tool := optionalTool{Binary: "docker", Unlock: "the full-stack profile", Fix: "install Docker"}

	present := checkOptionalTool(tool, func(string) (string, error) { return "/usr/bin/docker", nil })
	assert.Equal(t, doctorPass, present.Status, "present tool passes")

	missing := checkOptionalTool(tool, func(string) (string, error) { return "", fmt.Errorf("not found") })
	assert.Equal(t, doctorWarn, missing.Status, "missing optional tool warns, never fails")
	assert.Contains(t, missing.Detail, "not on PATH")
}

// ----------------------------------------------------------------------------
// Test suite: newDoctorReport (pure tally)
// ----------------------------------------------------------------------------

func TestDoctor_NewDoctorReport(t *testing.T) {
	t.Parallel()

	report := newDoctorReport([]doctorCheck{
		{Status: doctorPass},
		{Status: doctorPass},
		{Status: doctorWarn},
		{Status: doctorFail},
	})
	assert.Equal(t, 2, report.Passed)
	assert.Equal(t, 1, report.Warned)
	assert.Equal(t, 1, report.Failed)
	assert.Len(t, report.Checks, 4)
}

// ----------------------------------------------------------------------------
// Test suite: real command integration (buffer capture)
// ----------------------------------------------------------------------------

func TestDoctor_Integration_TextReport(t *testing.T) {
	// Not parallel: exercises package globals via newDoctorCmd.
	out, err := runDoctorCmd("--dir", t.TempDir())
	require.NoError(t, err, "doctor without --strict must not fail on a bare temp dir; out=%s", out)
	assert.Contains(t, out, "[ OK ]", "at least one passing check (Go toolchain)")
	assert.Contains(t, out, "passed", "summary line present")
}

func TestDoctor_Integration_JSONReport(t *testing.T) {
	out, err := runDoctorCmd("--dir", t.TempDir(), "--json")
	require.NoError(t, err, "json report must not fail; out=%s", out)
	assert.Contains(t, out, `"checks":`, "json has checks array")
	assert.Contains(t, out, `"passed":`, "json has passed count")
}

func TestDoctor_Integration_StrictFailsOnWarning(t *testing.T) {
	// A fresh temp dir has no .caf scaffold -> guaranteed warning -> --strict
	// turns that warning into a non-zero exit.
	_, err := runDoctorCmd("--dir", t.TempDir(), "--strict")
	require.Error(t, err, "strict mode must fail when warnings exist")
	assert.Contains(t, err.Error(), "warning")
}

func TestDoctor_Integration_MultiplePorts(t *testing.T) {
	out, err := runDoctorCmd("--dir", t.TempDir(), "--port", "18080", "--port", "19090")
	require.NoError(t, err, "multi-port check must not fail; out=%s", out)
	assert.Contains(t, out, "port 18080", "first port checked")
	assert.Contains(t, out, "port 19090", "second port checked")
}
