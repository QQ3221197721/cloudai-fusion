// cmd_autoscale_test.go walks the real `cafctl autoscale` developer journey:
// policy-add → evaluate-monitor (regression triggers scale_up) → apply →
// history shows the applied decision. Uses the same wireCmd harness as the
// other cafctl command tests.
package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runAutoscaleCmd executes one autoscale subcommand against a fresh store and
// returns its captured output. Directly call the subcommand constructor to avoid
// parent command's Help text being printed during Find().
func runAutoscaleCmd(t *testing.T, sub string, args ...string) string {
	t.Helper()
	var subCmd *cobra.Command
	switch sub {
	case "policy-add":
		subCmd = newPolicyAddCmd()
	case "policy-list":
		subCmd = newPolicyListCmd()
	case "evaluate-monitor":
		subCmd = newEvaluateMonitorCmd()
	case "evaluate-experiment":
		subCmd = newEvaluateExperimentCmd()
	case "apply":
		subCmd = newApplyCmd()
	case "history":
		subCmd = newHistoryCmd()
	default:
		t.Fatalf("unknown subcommand: %q", sub)
	}
	buf := wireCmd(subCmd)
	subCmd.SetArgs(args)
	require.NoError(t, subCmd.Execute(), "%s must succeed: %v", sub, args)
	return buf.String()
}

func TestAutoscalePolicyAddCmd_Persists(t *testing.T) {
	store := t.TempDir()
	out := runAutoscaleCmd(t, "policy-add",
		"--name", "p95-guard",
		"--metric", "latency_p95",
		"--threshold", "25",
		"--min", "1",
		"--max", "10",
		"--cooldown", "10",
		"--store", store,
		"--no-attest",
	)
	assert.Contains(t, out, "added successfully")
	assert.Contains(t, out, "latency_p95")
	assert.Contains(t, out, "25% regression triggers scale_up")
	assert.Contains(t, out, "1~10")

	// policy-list shows it back.
	listOut := runAutoscaleCmd(t, "policy-list", "--store", store)
	assert.Contains(t, listOut, "p95-guard")
	assert.Contains(t, listOut, "latency_p95")
}

func TestAutoscaleEvaluateMonitorCmd_ScaleUp(t *testing.T) {
	store := t.TempDir()
	runAutoscaleCmd(t, "policy-add",
		"--name", "p95-guard", "--metric", "latency_p95",
		"--threshold", "25", "--min", "1", "--max", "10", "--cooldown", "10",
		"--store", store, "--no-attest",
	)

	out := runAutoscaleCmd(t, "evaluate-monitor",
		"--metric", "latency_p95",
		"--regression", "30",
		"--budget", "100",
		"--current-cost", "80",
		"--store", store,
		"--no-attest",
	)
	assert.Contains(t, out, "SCALE_UP", "30%% regression > 25%% threshold must trigger SCALE_UP")
	assert.Contains(t, out, "within budget", "$80+$2 ≤ $100 budget check must pass")
	assert.Contains(t, out, "YES", "Budget OK must render YES")

	decisionID := extractDecisionID(out)
	require.NotEmpty(t, decisionID, "decision ID must be extractable from output")
	assert.True(t, strings.HasPrefix(decisionID, "sd-"), "ID prefix must be sd-: %s", decisionID)
}

func TestAutoscaleEvaluateMonitorCmd_BudgetRejected(t *testing.T) {
	store := t.TempDir()
	runAutoscaleCmd(t, "policy-add",
		"--name", "p95-guard", "--metric", "latency_p95",
		"--threshold", "25", "--min", "1", "--max", "10", "--cooldown", "10",
		"--store", store, "--no-attest",
	)

	out := runAutoscaleCmd(t, "evaluate-monitor",
		"--metric", "latency_p95",
		"--regression", "30",
		"--budget", "100",
		"--current-cost", "99", // $99+$2 > $100 → BUDGET REJECTED
		"--store", store,
		"--no-attest",
	)
	assert.Contains(t, out, "NO_CHANGE", "budget overrun must downgrade to NO_CHANGE")
	assert.Contains(t, out, "BUDGET REJECTED", "rejection must be explicit")
}

func TestAutoscaleEvaluateExperimentCmd_UpgradeRecommended(t *testing.T) {
	store := t.TempDir()
	out := runAutoscaleCmd(t, "evaluate-experiment",
		"--accuracy-gain", "3.5",
		"--budget", "100",
		"--current-cost", "60",
		"--store", store,
		"--no-attest",
	)
	assert.Contains(t, out, "SCALE_UP", "3.5pp gain ≥ 2.0pp must recommend upgrade")
	assert.Contains(t, out, "accuracy gain")
}

func TestAutoscaleFullJourney_AddEvaluateApplyHistory(t *testing.T) {
	store := t.TempDir()

	// 1. policy-add
	addOut := runAutoscaleCmd(t, "policy-add",
		"--name", "p95-guard", "--metric", "latency_p95",
		"--threshold", "25", "--min", "1", "--max", "10", "--cooldown", "10",
		"--store", store, "--no-attest",
	)
	assert.Contains(t, addOut, "added successfully")

	// 2. evaluate-monitor: 30% regression triggers scale_up
	evalOut := runAutoscaleCmd(t, "evaluate-monitor",
		"--metric", "latency_p95",
		"--regression", "30",
		"--budget", "100",
		"--current-cost", "80",
		"--store", store,
		"--no-attest",
	)
	assert.Contains(t, evalOut, "SCALE_UP")
	decisionID := extractDecisionID(evalOut)
	require.NotEmpty(t, decisionID, "decision ID must be extractable")

	// 3. history shows the pending decision
	histOut := runAutoscaleCmd(t, "history", "--store", store)
	assert.Contains(t, histOut, "monitor_alert")
	assert.Contains(t, histOut, "pending", "fresh decision must be pending")

	// 4. apply
	applyOut := runAutoscaleCmd(t, "apply", decisionID, "--store", store, "--no-attest")
	assert.Contains(t, applyOut, "applied successfully")

	// 5. history now shows it as applied
	histOut2 := runAutoscaleCmd(t, "history", "--store", store)
	assert.NotContains(t, histOut2, "pending", "applied decision must not show pending")
	assert.Contains(t, histOut2, "✓")
}

// extractDecisionID pulls the "sd-<hex16>" ID out of the rendered decision.
func extractDecisionID(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "ID:") {
			continue
		}
		parts := strings.SplitN(line, "sd-", 2)
		if len(parts) < 2 {
			continue
		}
		token := strings.TrimSpace(parts[1])
		fields := strings.Fields(token)
		if len(fields) == 0 {
			continue
		}
		return "sd-" + fields[0]
	}
	return ""
}

// ensure cobra stays referenced (compile guard for the helper signatures).
var _ = cobra.NoArgs
