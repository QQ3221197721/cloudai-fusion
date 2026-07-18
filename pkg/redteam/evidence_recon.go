package redteam

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// evidence_recon.go extends the redteam.* taxonomy with M1 receipts: tool
// executions (recon) and range-farm lifecycle. Kept in a separate file so the
// M0 evidence.go is untouched. Every tool run records WHAT was executed and a
// hash of its output, so the report provably reflects reality.

const (
	ActionRecon          = "redteam.recon"
	ActionRangeProvision = "redteam.range.provision"
	ActionRangeTeardown  = "redteam.range.teardown"
)

// recordRecon records a signed receipt for a single tool execution: the tool,
// technique, target, input/output fingerprints, honest mode, and a bounded
// summary. The raw output is fingerprinted (SHA-256), not stored, keeping the
// ledger compact and free of bulky/sensitive data while remaining verifiable.
func recordRecon(ctx context.Context, rec evidence.Recorder, logger *logrus.Logger, engagementID string, a Action, out ToolOutput, execErr error) {
	payload := map[string]any{
		"engagement_id": engagementID,
		"action_id":     a.ID,
		"tool":          a.Tool,
		"technique":     a.Technique,
		"target":        a.Target,
		"input_hash":    hashToolInput(ToolInput{Target: a.Target, Args: a.Params}),
		"output_hash":   sha256Hex(out.Raw),
		"output_bytes":  len(out.Raw),
		"summary":       bounded(out.Summary, 512),
		"mode":          modeStr(out.Mode),
	}
	if execErr != nil {
		payload["error"] = execErr.Error()
	}
	emit(ctx, rec, logger, evidence.RecordInput{
		Actor:   "redteam",
		Action:  ActionRecon,
		Subject: a.Target,
		Input:   map[string]any{"engagement_id": engagementID, "tool": a.Tool, "technique": a.Technique},
		Output:  map[string]any{"summary": bounded(out.Summary, 256), "mode": modeStr(out.Mode)},
		Payload: payload,
		Backends: []evidence.BackendFact{
			{Component: "redteam.tool." + a.Tool, Mode: modeStr(out.Mode), Driver: out.Driver},
		},
	})
}

// recordRangeProvision records the creation of an ephemeral practice/eval range.
func recordRangeProvision(ctx context.Context, rec evidence.Recorder, logger *logrus.Logger, r *Range, driver, mode string) {
	emit(ctx, rec, logger, evidence.RecordInput{
		Actor:   "redteam",
		Action:  ActionRangeProvision,
		Subject: r.ID,
		Input:   map[string]any{"name": r.Name, "apps": r.Apps},
		Output:  map[string]any{"kube_context": r.KubeContext, "endpoints": r.Endpoints},
		Payload: map[string]any{
			"range_id":     r.ID,
			"name":         r.Name,
			"apps":         r.Apps,
			"kube_context": r.KubeContext,
			"endpoints":    r.Endpoints,
		},
		Backends: []evidence.BackendFact{{Component: "redteam.range", Mode: mode, Driver: driver}},
	})
}

// recordRangeTeardown records the destruction of a range.
func recordRangeTeardown(ctx context.Context, rec evidence.Recorder, logger *logrus.Logger, rangeID, driver, mode string) {
	emit(ctx, rec, logger, evidence.RecordInput{
		Actor:    "redteam",
		Action:   ActionRangeTeardown,
		Subject:  rangeID,
		Input:    map[string]any{"range_id": rangeID},
		Output:   map[string]any{"torn_down": true},
		Payload:  map[string]any{"range_id": rangeID},
		Backends: []evidence.BackendFact{{Component: "redteam.range", Mode: mode, Driver: driver}},
	})
}
