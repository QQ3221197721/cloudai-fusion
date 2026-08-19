// cmd_pool_test.go walks the real `cafctl pool` developer journeys:
// create → node-add ×2 → acquire (best-fit) → leases → evaluate (budget reject).
// Uses the same wireCmd harness as other cafctl command tests; assertions match the
// exact strings the renderers emit (OK() = "✓ ", tabs collapse in tabwriter).
package main

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runPoolCmd executes one pool subcommand and returns its captured output.
func runPoolCmd(t *testing.T, sub string, args ...string) string {
	t.Helper()
	var subCmd *cobra.Command
	switch sub {
	case "create":
		subCmd = newPoolCreateCmd()
	case "list":
		subCmd = newPoolListCmd()
	case "show":
		subCmd = newPoolShowCmd()
	case "node-add":
		subCmd = newPoolNodeAddCmd()
	case "acquire":
		subCmd = newPoolAcquireCmd()
	case "release":
		subCmd = newPoolReleaseCmd()
	case "leases":
		subCmd = newPoolLeasesCmd()
	case "evaluate":
		subCmd = newPoolEvaluateCmd()
	default:
		t.Fatalf("unknown subcommand: %q", sub)
	}
	buf := wireCmd(subCmd)
	subCmd.SetArgs(args)
	require.NoError(t, subCmd.Execute(), "%s must succeed: %v", sub, args)
	return buf.String()
}

// runPoolCmdErr executes one pool subcommand expected to FAIL and returns
// (output, error) so tests can assert both the error and the rendered message.
func runPoolCmdErr(t *testing.T, sub string, args ...string) (string, error) {
	t.Helper()
	var subCmd *cobra.Command
	switch sub {
	case "create":
		subCmd = newPoolCreateCmd()
	case "list":
		subCmd = newPoolListCmd()
	case "show":
		subCmd = newPoolShowCmd()
	case "node-add":
		subCmd = newPoolNodeAddCmd()
	case "acquire":
		subCmd = newPoolAcquireCmd()
	case "release":
		subCmd = newPoolReleaseCmd()
	case "leases":
		subCmd = newPoolLeasesCmd()
	case "evaluate":
		subCmd = newPoolEvaluateCmd()
	default:
		t.Fatalf("unknown subcommand: %q", sub)
	}
	buf := wireCmd(subCmd)
	subCmd.SetArgs(args)
	err := subCmd.Execute()
	require.Error(t, err, "%s must fail: %v", sub, args)
	return buf.String(), err
}

// createPool returns poolID from create output.
func createPool(t *testing.T, store, name, gpuType string) string {
	t.Helper()
	out := runPoolCmd(t, "create",
		"--name", name, "--gpu-type", gpuType,
		"--slots-per-node", "4", "--min-nodes", "1",
		"--max-nodes", "10", "--cost-per-node-hour", "2.0",
		"--store", store,
	)
	return extractPoolID(out)
}

func extractPoolID(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "Pool:") || !strings.Contains(line, "(") {
			continue
		}
		parts := strings.SplitN(line, "pool-", 2)
		if len(parts) < 2 {
			continue
		}
		token := strings.TrimSpace(parts[1])
		fields := strings.Fields(token)
		if len(fields) == 0 {
			continue
		}
		return "pool-" + fields[0]
	}
	return ""
}

// TestPoolCreateCmd_PersistsAndLists: create renders the receipt, persists
// pools.json under <store>/elasticpool/, and list shows the pool row.
func TestPoolCreateCmd_PersistsAndLists(t *testing.T) {
	store := t.TempDir()

	out := runPoolCmd(t, "create",
		"--name", "gpu-cluster", "--gpu-type", "A100-80G",
		"--slots-per-node", "8", "--min-nodes", "1",
		"--max-nodes", "10", "--cost-per-node-hour", "3.2",
		"--store", store, "--no-attest",
	)
	assert.Contains(t, out, "cafctl pool create", "header line")
	assert.Contains(t, out, "gpu-cluster")
	assert.Contains(t, out, "A100-80G")
	assert.Contains(t, out, "active")
	assert.Contains(t, out, "created")
	assert.Contains(t, out, "Slots per Node:   8")
	assert.Contains(t, out, "Cost/Node-Hour:   $3.20")

	// Path contract from Module 16's lesson
	poolsJSON := store + "/elasticpool/pools.json"
	assert.FileExists(t, poolsJSON, "pools.json must live in the elasticpool/ subdir")

	listOut := runPoolCmd(t, "list", "--store", store)
	assert.Contains(t, listOut, "ID")
	assert.Contains(t, listOut, "NAME")
	assert.Contains(t, listOut, "GPU")
	assert.Contains(t, listOut, "STATUS")
	assert.Contains(t, listOut, "MIN")
	assert.Contains(t, listOut, "MAX")
	assert.Contains(t, listOut, "gpu-cluster")
	assert.Contains(t, listOut, "A100-80G")
	assert.Contains(t, listOut, "active")
}

// TestPoolNodeAddAcquireLeasesCmd: node-add ×2 → acquire ×2 (best-fit: both
// leases land on the first node — free 4 → 1) → leases shows both rows held.
func TestPoolNodeAddAcquireLeasesCmd(t *testing.T) {
	store := t.TempDir()
	poolID := createPool(t, store, "bf-pool", "L4-24G")
	require.NotEmpty(t, poolID)

	// Add two nodes
	out1 := runPoolCmd(t, "node-add", poolID, "--store", store)
	assert.Contains(t, out1, "node added")
	nid1 := extractNodeID(out1)
	require.NotEmpty(t, nid1)

	time.Sleep(10 * time.Millisecond) // distinct JoinedAt

	out2 := runPoolCmd(t, "node-add", poolID, "--store", store)
	assert.Contains(t, out2, "node added")
	nid2 := extractNodeID(out2)
	require.NotEmpty(t, nid2)

	// Acquire 3 slots → both nodes empty, stable pick lands on first-joined node.
	lid1, node1 := acquireSlots(t, store, poolID, "inf-bestfit00000001", 3)
	assert.Contains(t, lid1, "lease-")
	assert.Equal(t, nid1, node1, "first acquire lands on the first-joined node")

	// Acquire 1 slot → best-fit picks the smallest satisfying free space:
	// node1 has free=1, node2 has free=4 → node1 wins.
	time.Sleep(10 * time.Millisecond)
	lid2, node2 := acquireSlots(t, store, poolID, "inf-bestfit00000002", 1)
	require.NotEmpty(t, lid2)
	assert.Equal(t, nid1, node2, "best-fit: second acquire also lands on node1 (free=1 smallest)")

	// List leases → see both rows, both held
	leasesOut := runPoolCmd(t, "leases", poolID, "--limit", "10", "--store", store)
	assert.Contains(t, leasesOut, "LEASE ID")
	assert.Contains(t, leasesOut, "SERVICE")
	assert.Contains(t, leasesOut, "NODE")
	assert.Contains(t, leasesOut, "SLOTS")
	assert.Contains(t, leasesOut, "held")
	assert.Contains(t, leasesOut, nid1)
	assert.NotContains(t, leasesOut, "released")
}

func extractNodeID(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "Node ID:") {
			continue
		}
		parts := strings.SplitN(line, "node-", 2)
		if len(parts) < 2 {
			continue
		}
		token := strings.TrimSpace(parts[1])
		return "node-" + strings.Fields(token)[0]
	}
	return ""
}

func acquireSlots(t *testing.T, store, poolID, serviceID string, slots int) (string, string) {
	t.Helper()
	out := runPoolCmd(t, "acquire", poolID,
		"--service", serviceID, "--slots", strconv.Itoa(slots),
		"--store", store, "--no-attest",
	)
	return extractLeaseID(out), extractAcquireNodeID(out)
}

// extractAcquireNodeID pulls the "node-<hex12>" out of an acquire receipt's
// "  Node:             node-xxxx" line.
func extractAcquireNodeID(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "Node:") {
			continue
		}
		parts := strings.SplitN(line, "node-", 2)
		if len(parts) < 2 {
			continue
		}
		token := strings.TrimSpace(parts[1])
		fields := strings.Fields(token)
		if len(fields) == 0 {
			continue
		}
		return "node-" + fields[0]
	}
	return ""
}

func extractLeaseID(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "Lease ID:") {
			continue
		}
		parts := strings.SplitN(line, "lease-", 2)
		if len(parts) < 2 {
			continue
		}
		token := strings.TrimSpace(parts[1])
		return "lease-" + strings.Fields(token)[0]
	}
	return ""
}

// TestPoolAcquireCmd_CapacityExceeded: full node acquisition fails with clear error message.
func TestPoolAcquireCmd_CapacityExceeded(t *testing.T) {
	store := t.TempDir()
	poolID := createPool(t, store, "cap-pool", "T4-16G")
	require.NotEmpty(t, poolID)

	_ = runPoolCmd(t, "node-add", poolID, "--store", store, "--no-attest")

	// Fill up the only node
	acquireSlots(t, store, poolID, "inf-cap000000000001", 4)

	// Try 1 more → capacity exceeded error
	out, err := runPoolCmdErr(t, "acquire", poolID,
		"--service", "inf-cap000000000002", "--slots", "1",
		"--store", store,
	)
	require.Error(t, err)
	assert.Contains(t, out, "no ready node")
	assert.Contains(t, out, "add nodes")
	assert.Contains(t, out, "evaluate")
}

// TestPoolReleaseCmd_IdempotentReject: release succeeds once, second release fails with already released.
func TestPoolReleaseCmd_IdempotentReject(t *testing.T) {
	store := t.TempDir()
	poolID := createPool(t, store, "idem-pool", "V100-32G")
	require.NotEmpty(t, poolID)

	_ = runPoolCmd(t, "node-add", poolID, "--store", store)
	leaseID, _ := acquireSlots(t, store, poolID, "inf-idem000000000001", 2)
	require.NotEmpty(t, leaseID)

	// Release once
	releaseOut := runPoolCmd(t, "release", leaseID, "--store", store)
	assert.Contains(t, releaseOut, "lease freed")
	assert.Contains(t, releaseOut, "Released At:")

	// Second release should fail
	secondOut, err := runPoolCmdErr(t, "release", leaseID, "--store", store)
	require.Error(t, err)
	assert.Contains(t, secondOut, "already released")
}

// TestPoolEvaluateCmd_BudgetRejected: currentCost + costImpact > budgetLimit triggers BUDGET REJECTED.
func TestPoolEvaluateCmd_BudgetRejected(t *testing.T) {
	store := t.TempDir()
	// slots-per-node 4, cost 2.0/node-hour via direct create call
	createOut := runPoolCmd(t, "create",
		"--name", "budget-pool", "--gpu-type", "H100-80G",
		"--slots-per-node", "4", "--min-nodes", "1",
		"--max-nodes", "10", "--cost-per-node-hour", "2.0",
		"--store", store, "--no-attest",
	)
	poolID := extractPoolID(createOut)
	require.NotEmpty(t, poolID)

	_ = runPoolCmd(t, "node-add", poolID, "--store", store)
	acquireSlots(t, store, poolID, "inf-bu000000000001", 4) // fully occupied → free=0

	// pending=4 needs 1 node ($2/hr impact), but $99+$2 > $100 → BUDGET REJECTED
	evalOut := runPoolCmd(t, "evaluate", poolID,
		"--pending-slots", "4", "--budget-limit", "100",
		"--current-cost", "99", "--store", store, "--no-attest",
	)
	assert.Contains(t, evalOut, "BUDGET REJECTED")
	assert.Contains(t, evalOut, "NO_CHANGE")
	assert.Contains(t, evalOut, "REJECTED")
	assert.NotContains(t, evalOut, "SCALE_UP")
}

// TestPoolEvaluateCmd_ScaleUp: pending > free triggers scale_up when budget OK.
func TestPoolEvaluateCmd_ScaleUp(t *testing.T) {
	store := t.TempDir()
	poolID := createPool(t, store, "su-pool", "L40-48G")
	require.NotEmpty(t, poolID)

	_ = runPoolCmd(t, "node-add", poolID, "--store", store)
	acquireSlots(t, store, poolID, "inf-su000000000001", 2) // partial fill → free>0

	// pending=10 fits in free → no_change; increase pending > free
	evalOut := runPoolCmd(t, "evaluate", poolID,
		"--pending-slots", "8", "--budget-limit", "1000",
		"--current-cost", "0", "--store", store, "--no-attest",
	)
	// free slots exist but pending demand exceeds; scale_up triggered
	assert.Contains(t, evalOut, "SCALE_UP")
	assert.Contains(t, evalOut, "within budget")
}
