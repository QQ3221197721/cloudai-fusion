package delivery

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// edge.go is DL-3 (docs/verifiable-moat-spec.md §5.3): an offline-capable edge node
// accrues a LOCAL hash-chain of its autonomous decisions; on reconnect it emits a
// signed EdgeReconciliation embedding that chain. A verifier RECOMPUTES the chain
// and the compliance flag from the embedded decisions, so a node cannot lie: it
// cannot claim "all in-policy" over a chain that shows an out-of-policy decision,
// even by re-signing. Versus KubeEdge's best-effort sync, this proves offline
// autonomy stayed in policy. Reuses the spine (evidence.SignStatement) - no new crypto.

const edgeDomain = "cloudai-fusion/edge-reconciliation/v1"

// ActionEdge tags a recorded EdgeReconciliation receipt.
const ActionEdge = "delivery.edge"

// edgeGenesis is the PrevHash of the first offline decision.
const edgeGenesis = "edge-genesis"

// EdgeDecision is one autonomous decision in an edge node's offline hash-chain.
type EdgeDecision struct {
	Seq      int    `json:"seq"`       // 0-based position in the chain
	PrevHash string `json:"prev_hash"` // hash of Seq-1 (edgeGenesis for Seq 0)
	Action   string `json:"action"`
	PolicyOK bool   `json:"policy_ok"` // did the node's local policy permit it?
	Hash     string `json:"hash"`      // sha256 over the decision content
}

// EdgeChain accumulates a node's offline decisions as a linked hash-chain.
type EdgeChain struct {
	NodeID    string         `json:"node_id"`
	Decisions []EdgeDecision `json:"decisions"`
}

// NewEdgeChain starts an empty offline chain for a node.
func NewEdgeChain(nodeID string) *EdgeChain { return &EdgeChain{NodeID: nodeID} }

// edgeDecisionHash returns the canonical sha256 over a decision's content (Hash zeroed).
func edgeDecisionHash(d EdgeDecision) (string, error) {
	d.Hash = ""
	return evidence.HashAny(d)
}

// Append links a new offline decision onto the chain and returns it.
func (c *EdgeChain) Append(action string, policyOK bool) (EdgeDecision, error) {
	seq := len(c.Decisions)
	prev := edgeGenesis
	if seq > 0 {
		prev = c.Decisions[seq-1].Hash
	}
	d := EdgeDecision{Seq: seq, PrevHash: prev, Action: action, PolicyOK: policyOK}
	h, err := edgeDecisionHash(d)
	if err != nil {
		return EdgeDecision{}, err
	}
	d.Hash = h
	c.Decisions = append(c.Decisions, d)
	return d, nil
}

// VerifyEdgeChain checks the offline chain is intact: dense sequence, unbroken
// PrevHash linkage, and each decision's content hashes to its stored Hash.
func VerifyEdgeChain(decisions []EdgeDecision) error {
	prev := edgeGenesis
	for i, d := range decisions {
		if d.Seq != i {
			return fmt.Errorf("delivery: edge decision %d has seq %d", i, d.Seq)
		}
		if d.PrevHash != prev {
			return fmt.Errorf("delivery: broken edge chain at seq %d", i)
		}
		h, err := edgeDecisionHash(d)
		if err != nil {
			return err
		}
		if h != d.Hash {
			return fmt.Errorf("delivery: altered edge decision at seq %d", i)
		}
		prev = d.Hash
	}
	return nil
}

// EdgeReconciliation is the signed proof emitted on reconnect. It embeds the
// offline chain so a verifier can recompute head and compliance independently.
type EdgeReconciliation struct {
	ID                 string         `json:"id"`
	NodeID             string         `json:"node_id"`
	Decisions          []EdgeDecision `json:"decisions"`
	DecisionCount      int            `json:"decision_count"`
	ChainHead          string         `json:"chain_head"`
	AllPolicyCompliant bool           `json:"all_policy_compliant"`
	ReconciledAt       string         `json:"reconciled_at"`
	CreatedAt          string         `json:"created_at"`
	KeyID              string         `json:"key_id"`
	Signature          string         `json:"signature"`
}

// BuildReconciliation verifies the offline chain, derives head + compliance, and
// signs the reconciliation. It refuses to reconcile a broken chain.
func BuildReconciliation(s evidence.Signer, c *EdgeChain) (*EdgeReconciliation, error) {
	if s == nil {
		return nil, fmt.Errorf("delivery: edge reconciliation requires a signer")
	}
	if c == nil {
		return nil, fmt.Errorf("delivery: nil edge chain")
	}
	if err := VerifyEdgeChain(c.Decisions); err != nil {
		return nil, fmt.Errorf("delivery: cannot reconcile a broken chain: %w", err)
	}
	head := edgeGenesis
	if n := len(c.Decisions); n > 0 {
		head = c.Decisions[n-1].Hash
	}
	allOK := true
	for _, d := range c.Decisions {
		if !d.PolicyOK {
			allOK = false
			break
		}
	}
	att := &EdgeReconciliation{
		ID:                 common.NewUUID(),
		NodeID:             c.NodeID,
		Decisions:          c.Decisions,
		DecisionCount:      len(c.Decisions),
		ChainHead:          head,
		AllPolicyCompliant: allOK,
		ReconciledAt:       common.NowUTC().Format("2006-01-02T15:04:05Z07:00"),
		CreatedAt:          common.NowUTC().Format("2006-01-02T15:04:05Z07:00"),
		KeyID:              s.KeyID(),
	}
	att.Signature = ""
	sig, err := evidence.SignStatement(s, edgeDomain, *att)
	if err != nil {
		return nil, err
	}
	att.Signature = sig
	return att, nil
}

// VerifyEdgeReconciliation checks the signature AND recomputes the chain, head, and
// compliance flag from the embedded decisions - so a node cannot sign a false
// compliance claim over a chain that contradicts it.
func VerifyEdgeReconciliation(att *EdgeReconciliation, pub ed25519.PublicKey) error {
	if att == nil {
		return fmt.Errorf("delivery: nil edge reconciliation")
	}
	unsigned := *att
	unsigned.Signature = ""
	if err := evidence.VerifyStatement(pub, edgeDomain, unsigned, att.Signature); err != nil {
		return fmt.Errorf("delivery: edge reconciliation signature: %w", err)
	}
	if len(att.Decisions) != att.DecisionCount {
		return fmt.Errorf("delivery: decision count %d != declared %d", len(att.Decisions), att.DecisionCount)
	}
	if err := VerifyEdgeChain(att.Decisions); err != nil {
		return err
	}
	head := edgeGenesis
	if n := len(att.Decisions); n > 0 {
		head = att.Decisions[n-1].Hash
	}
	if head != att.ChainHead {
		return fmt.Errorf("delivery: chain head mismatch")
	}
	allOK := true
	for _, d := range att.Decisions {
		if !d.PolicyOK {
			allOK = false
			break
		}
	}
	if allOK != att.AllPolicyCompliant {
		return fmt.Errorf("delivery: compliance flag contradicts the chain (claimed %v, actual %v)", att.AllPolicyCompliant, allOK)
	}
	return nil
}

// ProvesCompliance returns nil only when every offline decision was in-policy.
func (att *EdgeReconciliation) ProvesCompliance() error {
	if !att.AllPolicyCompliant {
		return fmt.Errorf("delivery: edge node made out-of-policy offline decisions")
	}
	return nil
}

// RecordReconciliation appends a signed reconciliation as a delivery.edge receipt.
func RecordReconciliation(ctx context.Context, rec evidence.Recorder, att *EdgeReconciliation) (*evidence.Evidence, error) {
	if rec == nil || att == nil {
		return nil, nil
	}
	return rec.Record(ctx, evidence.RecordInput{
		Actor:   "delivery",
		Action:  ActionEdge,
		Subject: att.NodeID,
		Input:   map[string]any{"node_id": att.NodeID, "decisions": att.DecisionCount},
		Output:  map[string]any{"chain_head": att.ChainHead, "all_policy_compliant": att.AllPolicyCompliant},
		Payload: att,
		Backends: []evidence.BackendFact{
			{Component: "delivery.edge", Mode: "real", Driver: "offline-hashchain"},
		},
	})
}

// EdgeNodeKeyOf is the fabric KeyFunc for the edge well: the node a reconciliation
// receipt covered, or "" for non-edge receipts.
func EdgeNodeKeyOf(e *evidence.Evidence) string {
	if e == nil || e.Action != ActionEdge {
		return ""
	}
	var p struct {
		NodeID string `json:"node_id"`
	}
	if json.Unmarshal(e.Payload, &p) == nil {
		return p.NodeID
	}
	return ""
}
