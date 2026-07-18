package redteam

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// executor.go provides the M1 ToolExecutor: the real-tool implementation of the
// engine's Executor seam. It is engagement-scoped (bound to one engagement ID at
// construction) so it needs no change to the M0 Executor interface. For each
// authorized action it looks up the named tool, invokes it, and records a
// verifiable redteam.recon receipt; a parsed finding (if any) is returned to the
// engine, which attaches and records it.
type ToolExecutor struct {
	engagementID string
	registry     *ToolRegistry
	recorder     evidence.Recorder
	logger       *logrus.Logger
}

// NewToolExecutor builds an engagement-scoped executor over a tool registry.
func NewToolExecutor(engagementID string, registry *ToolRegistry, recorder evidence.Recorder, logger *logrus.Logger) *ToolExecutor {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	if recorder == nil {
		recorder = evidence.NopRecorder{}
	}
	return &ToolExecutor{engagementID: engagementID, registry: registry, recorder: recorder, logger: logger}
}

// Execute runs the tool named by the action, records a redteam.recon receipt, and
// returns any parsed finding. The action has already passed the authorization
// gate (the engine calls Execute only for allowed, approved actions).
func (x *ToolExecutor) Execute(ctx context.Context, a Action) (*Finding, error) {
	tool, ok := x.registry.Get(a.Tool)
	if !ok {
		return nil, fmt.Errorf("redteam: no tool %q registered", a.Tool)
	}
	out, err := tool.Invoke(ctx, ToolInput{Target: a.Target, Args: a.Params})
	// Always record what was executed (even on error) so the ledger is complete.
	recordRecon(ctx, x.recorder, x.logger, x.engagementID, a, out, err)
	if err != nil {
		return nil, err
	}
	if out.Finding != nil && out.Finding.Technique == "" {
		out.Finding.Technique = a.Technique
	}
	return out.Finding, nil
}
