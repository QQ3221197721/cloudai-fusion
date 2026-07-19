package redteam

import (
	"context"

	"github.com/sirupsen/logrus"
)

// engine.go orchestrates an engagement: plan -> authorize -> execute -> record.
// It is deliberately layered so later milestones swap implementations without
// touching the control flow:
//   - Planner: deterministic (M0) -> LLM ReAct planner (M2).
//   - Executor: dry-run/no-op (M0) -> MCP real-tool executor (M1).
// The gate and evidence recording are constant across milestones.

// Planner proposes candidate actions for an engagement.
type Planner interface {
	Plan(ctx context.Context, e *Engagement) ([]Action, error)
}

// StaticPlanner returns a fixed action list. Used for tests and deterministic
// playbooks before the LLM planner lands.
type StaticPlanner struct{ Actions []Action }

// Plan returns the configured actions.
func (p StaticPlanner) Plan(context.Context, *Engagement) ([]Action, error) { return p.Actions, nil }

// Executor performs an authorized action. In M0 there is no tooling, so the
// default executor is a no-op dry run; M1 plugs in the sandboxed MCP tool plane.
type Executor interface {
	Execute(ctx context.Context, a Action) (*Finding, error)
}

// DryRunExecutor performs no real work (M0). It never produces findings.
type DryRunExecutor struct{}

// Execute is a no-op.
func (DryRunExecutor) Execute(context.Context, Action) (*Finding, error) { return nil, nil }

// RunResult summarizes one engagement run.
type RunResult struct {
	Planned    int  `json:"planned"`
	Authorized int  `json:"authorized"`
	Denied     int  `json:"denied"`
	Skipped    int  `json:"skipped"` // authorized but awaiting approval
	Executed   int  `json:"executed"`
	Findings   int  `json:"findings"`
	Aborted    bool `json:"aborted"`
}

// Engine ties a Manager, Planner, and Executor into a run loop.
type Engine struct {
	mgr      *Manager
	planner  Planner
	executor Executor
	logger   *logrus.Logger
}

// NewEngine builds an engine. A nil executor defaults to DryRunExecutor.
func NewEngine(mgr *Manager, planner Planner, executor Executor, logger *logrus.Logger) *Engine {
	if executor == nil {
		executor = DryRunExecutor{}
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &Engine{mgr: mgr, planner: planner, executor: executor, logger: logger}
}

// Run executes an engagement: it plans actions, routes each through the
// authorization gate (which records the decision), executes the authorized ones,
// and records any findings. Approval-required actions are skipped in M0 (no
// auto-approval). A tripped kill-switch stops the loop immediately.
func (en *Engine) Run(ctx context.Context, engagementID string) (*RunResult, error) {
	e, err := en.mgr.Get(engagementID)
	if err != nil {
		return nil, err
	}
	if err := en.mgr.Start(engagementID); err != nil {
		return nil, err
	}
	gate := e.Gate()

	actions, err := en.planner.Plan(ctx, e)
	if err != nil {
		return nil, err
	}
	res := &RunResult{Planned: len(actions)}

	for _, a := range actions {
		if gate.Aborted() {
			res.Aborted = true
			break
		}
		d := gate.Check(ctx, a)
		if !d.Allowed {
			res.Denied++
			continue
		}
		res.Authorized++
		if d.NeedsApproval {
			res.Skipped++
			en.logger.WithFields(logrus.Fields{
				"engagement": engagementID, "action": a.ID, "risk": a.RiskTier.String(),
			}).Info("redteam: action authorized but awaiting human approval")
			continue
		}
		f, execErr := en.executor.Execute(ctx, a)
		if execErr != nil {
			en.logger.WithError(execErr).WithField("action", a.ID).Warn("redteam: action execution failed")
			continue
		}
		res.Executed++
		if f != nil {
			if err := en.mgr.AddFinding(ctx, engagementID, f); err == nil {
				res.Findings++
			}
		}
	}

	if gate.Aborted() {
		res.Aborted = true
	} else {
		_ = en.mgr.Complete(engagementID)
	}
	return res, nil
}
