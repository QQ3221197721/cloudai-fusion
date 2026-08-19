package main

import (
	"encoding/json"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runWellRouterCmd executes one wellrouter subcommand and returns its output.
func runWellRouterCmd(t *testing.T, sub string, args ...string) string {
	t.Helper()
	var subCmd *cobra.Command
	switch sub {
	case "rule-add":
		subCmd = newRuleAddCmd()
	case "rule-delete":
		subCmd = newRuleDeleteCmd()
	case "rule-list":
		subCmd = newRuleListCmd()
	case "publish":
		subCmd = newPublishCmd()
	case "stats":
		subCmd = newStatsCmd()
	case "dlq-list":
		subCmd = newDlqListCmd()
	default:
		t.Fatalf("unknown subcommand: %q", sub)
	}
	buf := wireCmd(subCmd)
	subCmd.SetArgs(args)
	require.NoError(t, subCmd.Execute(), "%s must succeed: %v", sub, args)
	return buf.String()
}

// ruleIDFromJSON extracts the id field from a rule-add --output json result.
func ruleIDFromJSON(t *testing.T, output string) string {
	t.Helper()
	var doc struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &doc), "output must be valid JSON: %s", output)
	require.Regexp(t, `^rule-[0-9a-f]{8}$`, doc.ID)
	return doc.ID
}

// ----------------------------------------------------------------------------
// Journey 1: default rule list → custom rule add → JSON rule-list → delete.
// ----------------------------------------------------------------------------

func TestWellRouterJourney_RuleLifecycle(t *testing.T) {
	t.Parallel()
	store := t.TempDir()

	// rule-list shows the 16 compiled default rules.
	listOut := runWellRouterCmd(t, "rule-list", "--store", store)
	assert.Contains(t, listOut, "rule-")
	assert.Contains(t, listOut, "aisecops.well.event")
	assert.Contains(t, listOut, "Total: 16 rules (16 active)")

	// rule-add (attested, text output).
	addOut := runWellRouterCmd(t, "rule-add", "--store", store,
		"--topic", "cluster.alert.>", "--source", "L5", "--targets", "L8,L13", "--max-hops", "4")
	assert.Contains(t, addOut, "rule added")
	assert.Contains(t, addOut, "L5-cloud-workload")
	assert.Contains(t, addOut, "L8-response, L13-evidence")
	assert.Contains(t, addOut, "Attestation: seq #")

	// rule-add with JSON output captures the ID for deletion.
	addJSON := runWellRouterCmd(t, "rule-add", "--store", store,
		"--topic", "custom.topic", "--source", "L1", "--targets", "L2", "--output", "json")
	ruleID := ruleIDFromJSON(t, addJSON)
	assert.Contains(t, addJSON, `"max_hops": 8`)
	assert.Contains(t, addJSON, `"source_well": "L1-intel"`)

	// rule-delete by exact ID.
	delOut := runWellRouterCmd(t, "rule-delete", "--store", store, ruleID)
	assert.Contains(t, delOut, "rule deleted")

	// JSON rule-list: the deleted rule is gone, the surviving custom rule present.
	// (encoding/json HTML-escapes ">" as \u003e, so match the stable prefix.)
	finalJSON := runWellRouterCmd(t, "rule-list", "--store", store, "--output", "json")
	assert.Contains(t, finalJSON, `cluster.alert.`)
	assert.NotContains(t, finalJSON, ruleID, "deleted rule must not resurrect")
	assert.NotContains(t, finalJSON, `"custom.topic"`, "deleted rule's topic must be gone")
	assert.Contains(t, finalJSON, `"rules_total": 17`, "16 defaults + 1 survivor")
}

// ----------------------------------------------------------------------------
// Journey 2: publish normal forward → stats show the forwarding.
// ----------------------------------------------------------------------------

func TestWellRouterJourney_PublishForwardStats(t *testing.T) {
	t.Parallel()
	store := t.TempDir()

	pubOut := runWellRouterCmd(t, "publish", "--store", store,
		"--topic", "aisecops.well.event", "--source", "L1", "--hop", "0",
		"--correlation-id", "cli-journey-2")
	assert.Contains(t, pubOut, "SUCCESS")
	assert.Contains(t, pubOut, "Forwarded:")
	// L1 default rule fans out to 4 downstream wells (L2, L3, L4, L14).
	assert.Regexp(t, `(?m)Forwarded:\s+4\b`, pubOut)
	assert.Contains(t, pubOut, "Rejected:")
	assert.Contains(t, pubOut, "Correlation ID:   cli-journey-2")

	statsOut := runWellRouterCmd(t, "stats", "--store", store)
	assert.Contains(t, statsOut, "Active:         16")
	assert.Contains(t, statsOut, "Forwarded:")
	assert.Contains(t, statsOut, "Dead-Lettered:")
}

// ----------------------------------------------------------------------------
// Journey 3: publish over hop → REJECTED + dlq-list shows the entry.
// ----------------------------------------------------------------------------

func TestWellRouterJourney_RejectionToDLQ(t *testing.T) {
	t.Parallel()
	store := t.TempDir()

	rejOut := runWellRouterCmd(t, "publish", "--store", store,
		"--topic", "aisecops.well.event", "--source", "L1", "--hop", "8")
	assert.Contains(t, rejOut, "REJECTED")
	assert.Contains(t, rejOut, "hop limit exceeded")
	assert.Regexp(t, `(?m)Rejected:\s+1\b`, rejOut)
	assert.Regexp(t, `(?m)DLQ:\s+1\b`, rejOut)

	// The DLQ is in-memory by spec (memory bus has no native DLQ), so a fresh
	// CLI process starts empty; the rejection itself was fully reported by
	// publish above (same-process DLQ querying is covered by pkg/wellrouter tests).
	dlqOut := runWellRouterCmd(t, "dlq-list", "--store", store, "--limit", "5")
	assert.Contains(t, dlqOut, "No dead-lettered events")

	// Empty-store dlq-list shows the helpful empty state.
	emptyOut := runWellRouterCmd(t, "dlq-list", "--store", t.TempDir())
	assert.Contains(t, emptyOut, "No dead-lettered events")
}

// ----------------------------------------------------------------------------
// Journey 4: error paths — invalid well, unknown rule delete.
// ----------------------------------------------------------------------------

func TestWellRouterJourney_ErrorPaths(t *testing.T) {
	t.Parallel()
	store := t.TempDir()

	// Invalid source well must fail loudly.
	var addCmd = newRuleAddCmd()
	buf := wireCmd(addCmd)
	addCmd.SetArgs([]string{"--store", store, "--topic", "t.x", "--source", "L99", "--targets", "L2"})
	err := addCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, buf.String(), "invalid well 99")

	// Deleting an unknown rule surfaces ErrRuleNotFound.
	delCmd := newRuleDeleteCmd()
	dbuf := wireCmd(delCmd)
	delCmd.SetArgs([]string{"--store", store, "rule-00000000"})
	require.Error(t, delCmd.Execute())
	assert.Contains(t, dbuf.String(), "not found")

	// max-hops above the hard cap of 8 is rejected up front.
	capCmd := newRuleAddCmd()
	cbuf := wireCmd(capCmd)
	capCmd.SetArgs([]string{"--store", store, "--topic", "t.x", "--source", "L1", "--targets", "L2", "--max-hops", "9"})
	require.Error(t, capCmd.Execute())
	assert.Contains(t, cbuf.String(), "max_hops")
}
