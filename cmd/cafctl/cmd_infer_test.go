// cmd_infer_test.go walks the real `cafctl infer` developer journeys:
// deploy → list → route-set → record → stats → stop, plus the key rejection
// paths (bad model ref, weight sum ≠ 100, stop idempotence). Uses the same
// wireCmd harness as the other cafctl command tests; assertions match the
// exact strings the renderers emit (OK() = "✓ ", tabs collapse in tabwriter).
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runInferCmd executes one infer subcommand and returns its captured output.
// Directly call the subcommand constructor so the parent's Help text never
// interferes with Find() routing (Use's first word IS the subcommand name).
func runInferCmd(t *testing.T, sub string, args ...string) string {
	t.Helper()
	var subCmd *cobra.Command
	switch sub {
	case "deploy":
		subCmd = newInferDeployCmd()
	case "list":
		subCmd = newInferListCmd()
	case "show":
		subCmd = newInferShowCmd()
	case "route-set":
		subCmd = newInferRouteSetCmd()
	case "record":
		subCmd = newInferRecordCmd()
	case "stats":
		subCmd = newInferStatsCmd()
	case "stop":
		subCmd = newInferStopCmd()
	default:
		t.Fatalf("unknown subcommand: %q", sub)
	}
	buf := wireCmd(subCmd)
	subCmd.SetArgs(args)
	require.NoError(t, subCmd.Execute(), "%s must succeed: %v", sub, args)
	return buf.String()
}

// runInferCmdErr executes one infer subcommand expected to FAIL and returns
// (output, error) so tests can assert both the error and the rendered message.
func runInferCmdErr(t *testing.T, sub string, args ...string) (string, error) {
	t.Helper()
	var subCmd *cobra.Command
	switch sub {
	case "deploy":
		subCmd = newInferDeployCmd()
	case "list":
		subCmd = newInferListCmd()
	case "show":
		subCmd = newInferShowCmd()
	case "route-set":
		subCmd = newInferRouteSetCmd()
	case "record":
		subCmd = newInferRecordCmd()
	case "stats":
		subCmd = newInferStatsCmd()
	case "stop":
		subCmd = newInferStopCmd()
	default:
		t.Fatalf("unknown subcommand: %q", sub)
	}
	buf := wireCmd(subCmd)
	subCmd.SetArgs(args)
	err := subCmd.Execute()
	require.Error(t, err, "%s must fail: %v", sub, args)
	return buf.String(), err
}

// deployInferSvc deploys a service and returns its ID extracted from the
// rendered output ("Service:   inf-xxxx ...").
func deployInferSvc(t *testing.T, store, name, modelRef string) string {
	t.Helper()
	out := runInferCmd(t, "deploy",
		"--name", name,
		"--model", modelRef,
		"--replicas", "2",
		"--store", store,
		"--no-attest",
	)
	return extractInferServiceID(out)
}

// extractInferServiceID pulls the "inf-<hex16>" ID out of rendered output.
func extractInferServiceID(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "Service:") {
			continue
		}
		parts := strings.SplitN(line, "inf-", 2)
		if len(parts) < 2 {
			continue
		}
		token := strings.TrimSpace(parts[1])
		fields := strings.Fields(token)
		if len(fields) == 0 {
			continue
		}
		return "inf-" + fields[0]
	}
	return ""
}

// TestInferDeployCmd_PersistsAndLists: deploy renders the receipt, persists
// services.json under <store>/inference/, and list shows the service row.
func TestInferDeployCmd_PersistsAndLists(t *testing.T) {
	store := t.TempDir()

	out := runInferCmd(t, "deploy",
		"--name", "demo-svc",
		"--model", "my-model@v3",
		"--replicas", "2",
		"--store", store,
	)
	assert.Contains(t, out, "cafctl infer deploy", "header line")
	assert.Contains(t, out, "demo-svc")
	assert.Contains(t, out, "my-model@v3")
	assert.Contains(t, out, "serving")
	assert.Contains(t, out, "deployed demo-svc → my-model@v3")
	assert.Contains(t, out, "v3 → 100%", "initial route renders as 100% to v3")

	// Path contract from Module 16's lesson: the store MUST be
	// <store>/inference/services.json, not <store>/services.json.
	servicesJSON := store + "/inference/services.json"
	assert.FileExists(t, servicesJSON, "services.json must live in the inference/ subdir")

	listOut := runInferCmd(t, "list", "--store", store)
	assert.Contains(t, listOut, "ID")
	assert.Contains(t, listOut, "NAME")
	assert.Contains(t, listOut, "MODEL")
	assert.Contains(t, listOut, "STATUS")
	assert.Contains(t, listOut, "REPLICAS")
	assert.Contains(t, listOut, "demo-svc")
	assert.Contains(t, listOut, "my-model@v3")
	assert.Contains(t, listOut, "serving")
}

// TestInferDeployCmd_RejectsInvalidModelRef: a ref without "@" fails with a
// clear error and nothing is persisted.
func TestInferDeployCmd_RejectsInvalidModelRef(t *testing.T) {
	store := t.TempDir()

	out, err := runInferCmdErr(t, "deploy",
		"--name", "bad",
		"--model", "no-version-tag",
		"--replicas", "1",
		"--store", store,
	)
	require.Error(t, err)
	assert.Contains(t, out, "invalid model ref")
	assert.Contains(t, out, `expected "name@version"`)

	listOut := runInferCmd(t, "list", "--store", store)
	assert.Contains(t, listOut, "No inference services deployed yet.", "rejected deploy must persist nothing")
}

// TestInferRouteSetCmd_ThenShow: a weight sum ≠ 100 is rejected with the
// actual total; a valid split replaces the routes and show renders them.
func TestInferRouteSetCmd_ThenShow(t *testing.T) {
	store := t.TempDir()
	svcID := deployInferSvc(t, store, "canary-svc", "my-model@v3")
	require.NotEmpty(t, svcID, "service ID must be extractable from deploy output")
	require.Regexp(t, `^inf-[0-9a-f]{16}$`, svcID)

	// Weight sum 99 → rejected with the sum surfaced.
	badOut, err := runInferCmdErr(t, "route-set", svcID,
		"--weights", "v3=70,v4=29",
		"--store", store, "--no-attest",
	)
	require.Error(t, err)
	assert.Contains(t, badOut, "must sum to 100")
	assert.Contains(t, badOut, "99", "the offending total is part of the message")

	// Valid 70/30 split commits.
	out := runInferCmd(t, "route-set", svcID,
		"--weights", "v3=70,v4=30",
		"--store", store, "--no-attest",
	)
	assert.Contains(t, out, "routes committed")
	assert.Contains(t, out, "v3 → 70%")
	assert.Contains(t, out, "v4 → 30%")

	// show reflects the new routes.
	showOut := runInferCmd(t, "show", svcID, "--store", store)
	assert.Contains(t, showOut, "cafctl infer show")
	assert.Contains(t, showOut, svcID)
	assert.Contains(t, showOut, "v3 → 70%")
	assert.Contains(t, showOut, "v4 → 30%")
}

// TestInferRecordAndStatsCmd: record appends a stat, stats renders the row;
// a p95 < p50 violation is rejected with the constraint in the message.
func TestInferRecordAndStatsCmd(t *testing.T) {
	store := t.TempDir()
	svcID := deployInferSvc(t, store, "telemetry-svc", "my-model@v1")
	require.NotEmpty(t, svcID)

	recOut := runInferCmd(t, "record", svcID,
		"--requests", "1000",
		"--errors", "12",
		"--latency-p50", "8",
		"--latency-p95", "25",
		"--latency-p99", "60",
		"--throughput", "830.5",
		"--store", store, "--no-attest",
	)
	assert.Contains(t, recOut, "stat recorded")
	assert.Contains(t, recOut, "requests=1000 errors=12")
	assert.Contains(t, recOut, "p50=8.0ms p95=25.0ms p99=60.0ms")

	statsOut := runInferCmd(t, "stats", svcID, "--limit", "5", "--store", store)
	assert.Contains(t, statsOut, "TIMESTAMP")
	assert.Contains(t, statsOut, "1000")
	assert.Contains(t, statsOut, "12")
	assert.Contains(t, statsOut, "830.5")

	// Latency triple violation rejected.
	badOut, err := runInferCmdErr(t, "record", svcID,
		"--requests", "10",
		"--latency-p50", "50",
		"--latency-p95", "20",
		"--latency-p99", "80",
		"--store", store, "--no-attest",
	)
	require.Error(t, err)
	assert.Contains(t, badOut, "p50<=p95<=p99")
}

// TestInferStopCmd_IdempotentReject: stop renders the receipt; a second stop
// fails with "already stopped"; the final list shows the stopped status.
func TestInferStopCmd_IdempotentReject(t *testing.T) {
	store := t.TempDir()
	svcID := deployInferSvc(t, store, "stop-svc", "my-model@v2")
	require.NotEmpty(t, svcID)

	out := runInferCmd(t, "stop", svcID, "--store", store)
	assert.Contains(t, out, "cafctl infer stop")
	assert.Contains(t, out, "stopped")
	assert.Contains(t, out, svcID)

	secondOut, err := runInferCmdErr(t, "stop", svcID, "--store", store)
	require.Error(t, err)
	assert.Contains(t, secondOut, "already stopped")

	listOut := runInferCmd(t, "list", "--store", store)
	assert.Contains(t, listOut, "stop-svc")
	assert.Contains(t, listOut, "stopped", "list reflects the stopped status")
}

// TestInferDeployCmd_RejectsNonPositiveReplicas: --replicas <= 0 rejected at CLI
// layer before opening mesh/store; inference/ subdir not created.
func TestInferDeployCmd_RejectsNonPositiveReplicas(t *testing.T) {
	store := t.TempDir()

	out, err := runInferCmdErr(t, "deploy",
		"--name", "blocked",
		"--model", "m@v1",
		"--replicas", "0",
		"--store", store,
	)
	require.Error(t, err)
	assert.Contains(t, out, "--replicas must be positive", "error mentions replicas guard")

	// Store directory MUST NOT have been created (pre-open rejection).
	inferencePath := filepath.Join(store, "inference")
	_, serr := os.Stat(inferencePath)
	assert.True(t, os.IsNotExist(serr), "inference/ must not exist after --replicas <= 0 rejection")
}

// TestInferRouteSetCmd_RejectsDuplicateWeights: duplicate version keys in
// --weights are rejected with a clear message.
func TestInferRouteSetCmd_RejectsDuplicateWeights(t *testing.T) {
	store := t.TempDir()
	svcID := deployInferSvc(t, store, "dup-test", "my-model@v1")
	require.NotEmpty(t, svcID)

	// Duplicate key rejected immediately.
	badOut, err := runInferCmdErr(t, "route-set", svcID,
		"--weights", "v1=50,v1=50",
		"--store", store, "--no-attest",
	)
	require.Error(t, err)
	assert.Contains(t, badOut, "duplicate version \"v1\" in weights", "error mentions duplicate version")

	// Route table unchanged: initial deploy had v1 → 100%.
	showOut := runInferCmd(t, "show", svcID, "--store", store)
	assert.Contains(t, showOut, "v1 → 100%", "routes unchanged after duplicate rejection")
}
