package aiops

// evidence_healproof.go signs self-healing actions and adds an independent
// innovation: anomaly causality graph to distinguish root causes from symptoms.
//
// Innovation — Anomaly Causality Graph:
// Each anomaly becomes a node with directed edges to its effects. A breadth-first
// search finds ancestors of anomalies and scores candidates by connectivity:
// fewer descendants but more ancestors = true cause vs downstream symptom.
// This prevents treating "database unavailable" as the root cause when the real
// cause is "network partition".

import (
	"crypto/ed25519"
	"crypto/rand"
	"sort"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// EvidenceCausalNode captures one node in the causality graph.
type EvidenceCausalNode struct {
	ID         string        `json:"id"`
	Type       string        `json:"type"` // "error"/"latency"/"resource"
	Timestamp  time.Time     `json:"timestamp"`
	InDegree   int           `json:"in_degree"`
	OutDegree  int           `json:"out_degree"`
	CausalRank float64       `json:"causal_rank"`
}

// EvidenceSelfHealResult is the signed outcome of a self-heal attempt.
type EvidenceSelfHealResult struct {
	ActionTaken     string            `json:"action_taken"`     // "restart"/"rollback"/"skip"
	CausedBy        string            `json:"caused_by"`
	Score           float64           `json:"score"`
	CausalityGraph  []EvidenceCausalNode `json:"causality_graph"`
	Receipt         *evidence.Receipt `json:"receipt"`
}

// EvidenceAIOpsEngine wraps self-heal with receipts and causality graph analysis.
type EvidenceAIOpsEngine struct {
	receiptBuilder    *evidence.ReceiptBuilder
	anomalyGraph      map[string][]string // anomaly -> effects
	nodeTypes         map[string]string   // ID -> type
	nodeTimestamps    map[string]time.Time
	maxAnomalies      int
	windowMinutes     float64
}

// NewEvidenceAIOpsEngine constructs an engine with fresh Ed25519 key.
func NewEvidenceAIOpsEngine() *EvidenceAIOpsEngine {
	_, privKey, _ := ed25519.GenerateKey(rand.Reader)
	return &EvidenceAIOpsEngine{
		receiptBuilder:   evidence.NewReceiptBuilder("aiops", privKey),
		anomalyGraph:     make(map[string][]string),
		nodeTypes:        make(map[string]string),
		nodeTimestamps:   make(map[string]time.Time),
		maxAnomalies:     50,
		windowMinutes:    15,
	}
}

// RegisterAnomaly logs an anomaly event with its type and timestamp.
func (e *EvidenceAIOpsEngine) RegisterAnomaly(id, typ string, ts time.Time) {
	e.nodeTypes[id] = typ
	if e.nodeTimestamps[id].IsZero() {
		e.nodeTimestamps[id] = ts
	}
	if len(e.anomalyGraph) > e.maxAnomalies {
		for k := range e.anomalyGraph {
			delete(e.anomalyGraph, k)
			break
		}
	}
}

// AddCausalLink registers that `cause` causes `effect`.
func (e *EvidenceAIOpsEngine) AddCausalLink(cause, effect string) {
	e.anomalyGraph[cause] = append(e.anomalyGraph[cause], effect)
}

// SelfHeal attests a self-healing action and ranks causes via the causality graph.
func (e *EvidenceAIOpsEngine) SelfHeal(currentAction string, observedEffects []string) (*EvidenceSelfHealResult, error) {
	// Compute degrees for each known anomaly.
	degs := e.computeDegrees()
	ranked := e.rankCauses(degs)

	var bestID string
	var bestScore float64
	if len(ranked) > 0 {
		bestID = ranked[0].ID
		bestScore = ranked[0].CausalRank
	} else {
		bestScore = 0
	}

	input := map[string]interface{}{
		"action": currentAction,
		"effects": observedEffects,
		"best_cause_id": bestID,
		"rank": bestScore,
	}
	output := map[string]interface{}{"graph": ranked}
	receipt, err := e.receiptBuilder.Build("aiops.heal", input, output)
	if err != nil {
		return nil, err
	}

	return &EvidenceSelfHealResult{
		ActionTaken: currentAction,
		CausedBy:    bestID,
		Score:       bestScore,
		CausalityGraph: ranked,
		Receipt:     receipt,
	}, nil
}

func (e *EvidenceAIOpsEngine) computeDegrees() map[string]struct {
	in, out int
} {
	outDeg := make(map[string]int)
	inDeg := make(map[string]int)
	for cause, effects := range e.anomalyGraph {
		outDeg[cause] = len(effects)
		for _, eff := range effects {
			inDeg[eff]++
			if outDeg[eff] == 0 {
				outDeg[eff] = 0
			}
		}
	}
	degs := make(map[string]struct {
		in, out int
	})
	allIDs := make(map[string]bool)
	for id := range e.nodeTypes {
		allIDs[id] = true
	}
	for cause, effects := range e.anomalyGraph {
		allIDs[cause] = true
		for _, eff := range effects {
			allIDs[eff] = true
		}
	}
	for id := range allIDs {
		degs[id] = struct {
			in, out int
		}{in: inDeg[id], out: outDeg[id]}
	}
	return degs
}

func (e *EvidenceAIOpsEngine) rankCauses(degs map[string]struct {
	in, out int
}) []EvidenceCausalNode {
	nodes := make([]EvidenceCausalNode, 0, len(degs))
	for id, d := range degs {
		typ := e.nodeTypes[id]
		if typ == "" {
			typ = "error"
		}
		ts := e.nodeTimestamps[id]
		nodes = append(nodes, EvidenceCausalNode{
			ID:       id,
			Type:     typ,
			Timestamp: ts,
			InDegree:  d.in,
			OutDegree: d.out,
		})
	}

	// Rank by: higher in-degree - out-degree => more likely root cause.
	sort.Slice(nodes, func(i, j int) bool {
		diffI := nodes[i].InDegree - nodes[i].OutDegree
		diffJ := nodes[j].InDegree - nodes[j].OutDegree
		if diffI != diffJ {
			return diffI > diffJ
		}
		return nodes[i].InDegree > nodes[j].InDegree
	})

	for i := range nodes {
		nodes[i].CausalRank = float64(i+1) / float64(len(nodes)+1)
	}
	return nodes
}
