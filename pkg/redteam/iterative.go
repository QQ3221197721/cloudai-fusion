package redteam

import (
	"context"

	"github.com/sirupsen/logrus"
)

// iterative.go adds a ReAct-style loop WITHOUT modifying the M0 engine: it reuses
// the same authorization gate, executor, and evidence recording, but re-plans
// after each step using the accumulated AttackGraph. This is the seam M2's LLM
// planner plugs into (plan -> authorize -> execute -> observe -> re-plan).

// IterativePlanner proposes the next batch of actions given the current graph.
// Returning zero actions signals the engagement is complete.
type IterativePlanner interface {
	PlanNext(ctx context.Context, e *Engagement, g *AttackGraph) ([]Action, error)
}

// IterativeEngine drives an engagement as a bounded ReAct loop.
type IterativeEngine struct {
	mgr      *Manager
	executor Executor
	logger   *logrus.Logger
}

// NewIterativeEngine builds a ReAct engine over a manager and executor. A nil
// executor defaults to DryRunExecutor.
func NewIterativeEngine(mgr *Manager, executor Executor, logger *logrus.Logger) *IterativeEngine {
	if executor == nil {
		executor = DryRunExecutor{}
	}
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	return &IterativeEngine{mgr: mgr, executor: executor, logger: logger}
}

// Run executes the ReAct loop for up to maxSteps planning rounds. Each proposed
// action is authorized by the gate (which records the decision); allowed,
// approved actions are executed; findings feed back into the graph so the next
// round can react to them. A tripped kill-switch stops immediately.
func (en *IterativeEngine) Run(ctx context.Context, engagementID string, planner IterativePlanner, graph *AttackGraph, maxSteps int) (*RunResult, error) {
	e, err := en.mgr.Get(engagementID)
	if err != nil {
		return nil, err
	}
	if err := en.mgr.Start(engagementID); err != nil {
		return nil, err
	}
	if graph == nil {
		graph = NewAttackGraph()
	}
	gate := e.Gate()
	res := &RunResult{}

	for step := 0; step < maxSteps; step++ {
		if gate.Aborted() {
			res.Aborted = true
			break
		}
		actions, err := planner.PlanNext(ctx, e, graph)
		if err != nil {
			return res, err
		}
		if len(actions) == 0 {
			break // planner signals completion
		}
		for _, a := range actions {
			if gate.Aborted() {
				res.Aborted = true
				break
			}
			res.Planned++
			d := gate.Check(ctx, a)
			if !d.Allowed {
				res.Denied++
				continue
			}
			res.Authorized++
			if d.NeedsApproval {
				res.Skipped++
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
				graph.AddFinding(f)
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
