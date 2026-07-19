package redteam

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// ad_path.go executes an authorized AD attack path (M4, OSEP-class). It finds a
// path via the graph (adgraph.go), then walks each hop as a lateral-tier action
// through the SAME authorization gate (so the whole path needs human approval),
// recording a redteam.ad.path receipt with per-hop authorization status. Hops run
// only inside a declared isolation context (an isolated kind range in production)
// so there is no blast radius on the real network. The kill-switch halts mid-path.

// ActionADPath is the receipt emitted for an AD attack-path run.
const ActionADPath = "redteam.ad.path"

// ADPathStep is the recorded, authorized outcome of one hop.
type ADPathStep struct {
	From      string `json:"from"`
	To        string `json:"to"`
	EdgeKind  string `json:"edge_kind"`
	Technique string `json:"technique"`
	Status    string `json:"status"` // authorized | denied:<reason> | aborted
}

// ADPathResult is the outcome of an attack-path run.
type ADPathResult struct {
	Start   string       `json:"start"`
	Target  string       `json:"target"`
	Reached bool         `json:"reached"`
	Steps   []ADPathStep `json:"steps"`
}

// ADPathEngine authorizes and records AD attack paths with per-hop evidence.
type ADPathEngine struct {
	mgr       *Manager
	recorder  evidence.Recorder
	logger    *logrus.Logger
	isolation string // isolation context (e.g. an isolated kind cluster); "" = none
}

// NewADPathEngine builds the engine. A non-empty isolation context is reported
// real (hops are confined to an isolated range); empty is honestly simulated.
func NewADPathEngine(mgr *Manager, recorder evidence.Recorder, logger *logrus.Logger, isolation string) *ADPathEngine {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	if recorder == nil {
		recorder = evidence.NopRecorder{}
	}
	_ = capability.Report("redteam.ad.pathing", "graph-bfs", capability.ModeReal,
		"BloodHound-style attack-path search over a real AD graph")
	isoMode := capability.ModeSimulated
	if isolation != "" {
		isoMode = capability.ModeReal
	}
	_ = capability.Report("redteam.ad.isolation", "range-cluster", isoMode,
		"attack-path hops confined to an isolated range (blast-radius control)")
	return &ADPathEngine{mgr: mgr, recorder: recorder, logger: logger, isolation: isolation}
}

// ExecutePath finds a path (to target, or to the nearest high-value principal if
// target is empty) and authorizes each hop through the gate. The path is a
// lateral-tier operation: if the scope requires approval, a non-empty approver
// must be supplied (recorded once for the path). A denied hop or a tripped
// kill-switch stops the walk. On reaching the target a finding is recorded.
func (en *ADPathEngine) ExecutePath(ctx context.Context, engagementID string, graph *ADGraph, start, target, domain, approver string) (*ADPathResult, error) {
	e, err := en.mgr.Get(engagementID)
	if err != nil {
		return nil, err
	}
	gate := e.Gate()

	var path []ADEdge
	var ok bool
	if target == "" {
		path, target, ok = graph.PathToHighValue(start)
	} else {
		path, ok = graph.ShortestPath(start, target)
	}
	res := &ADPathResult{Start: start, Target: target}
	if !ok || len(path) == 0 {
		en.record(ctx, engagementID, res)
		return res, nil // no reachable path; nothing authorized
	}

	// Path-level human approval (lateral tier).
	if RiskLateral >= e.Scope.ApprovalReq {
		if approver == "" {
			return nil, fmt.Errorf("redteam: AD attack path requires human approval")
		}
		gate.Approve(ctx, common.NewUUID(), approver, true, RiskLateral)
	}

	reached := true
	for _, edge := range path {
		if gate.Aborted() {
			res.Steps = append(res.Steps, ADPathStep{From: edge.From, To: edge.To, EdgeKind: edge.Kind, Technique: edge.Technique, Status: "aborted"})
			reached = false
			break
		}
		action := Action{
			ID:        common.NewUUID(),
			Technique: edge.Technique,
			Tool:      "ad-path",
			Target:    domain,
			RiskTier:  RiskLateral,
			Params:    map[string]any{"from": edge.From, "to": edge.To, "edge_kind": edge.Kind, "isolation": en.isolation},
		}
		d := gate.Check(ctx, action) // records authorized / scope.deny
		if !d.Allowed {
			res.Steps = append(res.Steps, ADPathStep{From: edge.From, To: edge.To, EdgeKind: edge.Kind, Technique: edge.Technique, Status: "denied:" + d.Reason})
			reached = false
			break
		}
		res.Steps = append(res.Steps, ADPathStep{From: edge.From, To: edge.To, EdgeKind: edge.Kind, Technique: edge.Technique, Status: "authorized"})
	}

	res.Reached = reached
	en.record(ctx, engagementID, res)
	if res.Reached {
		lastTech := "T1021"
		if len(path) > 0 {
			lastTech = path[len(path)-1].Technique
		}
		_ = en.mgr.AddFinding(ctx, engagementID, &Finding{
			Asset:     target,
			Severity:  "critical",
			Title:     fmt.Sprintf("AD attack path reached %s in %d hops", target, len(path)),
			Technique: lastTech,
		})
	}
	return res, nil
}

// record emits a signed redteam.ad.path receipt with the full authorized path.
func (en *ADPathEngine) record(ctx context.Context, engagementID string, res *ADPathResult) {
	steps := make([]map[string]any, 0, len(res.Steps))
	for _, s := range res.Steps {
		steps = append(steps, map[string]any{
			"from": s.From, "to": s.To, "edge_kind": s.EdgeKind, "technique": s.Technique, "status": s.Status,
		})
	}
	isoMode := "simulated"
	if en.isolation != "" {
		isoMode = "real"
	}
	emit(ctx, en.recorder, en.logger, evidence.RecordInput{
		Actor:   "redteam",
		Action:  ActionADPath,
		Subject: res.Target,
		Input:   map[string]any{"engagement_id": engagementID, "start": res.Start, "target": res.Target},
		Output:  map[string]any{"reached": res.Reached, "hops": len(res.Steps)},
		Payload: map[string]any{
			"engagement_id": engagementID,
			"start":         res.Start,
			"target":        res.Target,
			"reached":       res.Reached,
			"isolation":     en.isolation,
			"steps":         steps,
		},
		Backends: []evidence.BackendFact{
			{Component: "redteam.ad.pathing", Mode: "real", Driver: "graph-bfs"},
			{Component: "redteam.ad.isolation", Mode: isoMode, Driver: "range-cluster"},
		},
	})
}
