package evidence

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// completeness.go implements Moat A, Layer A0 (see docs/verifiable-moat-spec.md):
// a NAMESPACE-PARAMETRIC completeness ("no more, no less") proof over a subset of
// the ledger, using only the existing RFC 6962 machinery.
//
// The problem it solves: an inclusion proof shows a listed receipt IS in the log
// ("no more"), but nothing today proves a report did not OMIT an in-scope receipt
// ("no less") without handing the verifier the entire plaintext log. The seal
// closes that gap RELATIVE TO A SEALED SET:
//
//   - At a terminal transition, the owner records a SEAL receipt whose payload
//     commits to (count k, subtree_root R_E = MerkleTreeHash over the k member
//     leaves, the member IDs).
//   - A CompletenessProof then shows: the seal is in the tree, every member is in
//     the tree, and the members hash to EXACTLY R_E with count k. Dropping, adding,
//     reordering, or editing any member changes the recomputed root and fails
//     verification.
//
// Because Namespace is just a string ("redteam/engagement/<id>",
// "scheduler/tenant/<id>", "finops/tenant/<month>"), the SAME code proves a
// red-team report, a tenant's scheduling fairness, or a month's FinOps savings —
// one implementation, every pillar (spec principle 6).
//
// A0's guarantee is completeness RELATIVE TO THE SEAL; closing the absolute gap
// against a confidential verifier is Layer A1 (zkEvidence), implemented in
// pkg/evidence/zk (a real gnark Groth16 proof over a Poseidon2 mirror commitment).
// A0 therefore includes the full member receipts in the proof (the auditor is
// permitted to see them); hiding them from an adversarial verifier is A1's job.

// ActionSubtreeSeal is the action name of a terminal "seal" receipt. It appears
// in the ledger like any other receipt and is itself hash-chained and signed.
const ActionSubtreeSeal = "evidence.seal"

// ErrNoSeal is returned by BuildCompletenessProof when a namespace has no seal
// yet (nothing sealed for it). Callers (e.g. the HTTP API) map it to 404.
var ErrNoSeal = errors.New("evidence: no seal found for namespace")

// SubtreeSeal is the payload of a seal receipt: a signed commitment to the exact
// set of receipts belonging to a namespace at seal time.
type SubtreeSeal struct {
	Namespace   string   `json:"namespace"`
	Count       int      `json:"count"`        // k = number of member receipts
	SubtreeRoot string   `json:"subtree_root"` // hex RFC 6962 MTH over member leaves (Seq asc)
	MemberIDs   []string `json:"member_ids"`   // member record IDs, Seq ascending
}

// CompletenessProof is the offline-verifiable artifact proving that Members is
// EXACTLY the set of receipts committed by the namespace's seal — no omission, no
// cherry-picking. It is self-contained: a verifier needs only this struct and the
// pinned public key.
type CompletenessProof struct {
	Namespace     string                    `json:"namespace"`
	Seal          *Evidence                 `json:"seal"`           // the terminal seal receipt
	SealInclusion *InclusionProofResponse   `json:"seal_inclusion"` // seal ∈ tree
	Members       []*Evidence               `json:"members"`        // ordered by Seq asc
	MemberProofs  []*InclusionProofResponse `json:"member_proofs"`  // each member ∈ tree
}

// SealNamespace commits to members (count + RFC 6962 subtree root + member IDs)
// and records a terminal seal receipt for the namespace, returning the seal. The
// members are ordered by Seq ascending so the root is deterministic; the caller
// is responsible for passing the complete set (e.g. redteam.EngagementReceipts).
func (l *Ledger) SealNamespace(ctx context.Context, namespace, actor string, members []*Evidence) (*Evidence, error) {
	if namespace == "" {
		return nil, errors.New("evidence: seal requires a namespace")
	}
	ordered := append([]*Evidence(nil), members...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Seq < ordered[j].Seq })

	root, err := MerkleRootHexOfRecords(ordered)
	if err != nil {
		return nil, fmt.Errorf("evidence: seal subtree root: %w", err)
	}
	ids := make([]string, len(ordered))
	for i, m := range ordered {
		ids[i] = m.ID
	}
	seal := &SubtreeSeal{
		Namespace:   namespace,
		Count:       len(ordered),
		SubtreeRoot: root,
		MemberIDs:   ids,
	}
	if actor == "" {
		actor = "evidence"
	}
	return l.Record(ctx, RecordInput{
		Actor:   actor,
		Action:  ActionSubtreeSeal,
		Subject: namespace,
		Input:   map[string]any{"namespace": namespace, "count": len(ordered)},
		Output:  map[string]any{"subtree_root": root},
		Payload: seal,
		Backends: []BackendFact{
			{Component: "evidence.seal", Mode: "real", Driver: "rfc6962-merkle"},
		},
	})
}

// BuildCompletenessProof assembles a proof for the latest seal of namespace.
// All inclusion proofs are built against a SINGLE ledger snapshot so the seal and
// every member share one signed checkpoint (STH). Returns an error if no seal
// exists for the namespace or a committed member is missing from the ledger.
func (l *Ledger) BuildCompletenessProof(ctx context.Context, namespace string) (*CompletenessProof, error) {
	all, err := l.store.All(ctx)
	if err != nil {
		return nil, err
	}

	// Find the latest (highest-Seq) seal for this namespace.
	var seal *Evidence
	for _, e := range all {
		if e.Action == ActionSubtreeSeal && e.Subject == namespace {
			if seal == nil || e.Seq > seal.Seq {
				seal = e
			}
		}
	}
	if seal == nil {
		return nil, fmt.Errorf("%w %q", ErrNoSeal, namespace)
	}
	var sp SubtreeSeal
	if err := json.Unmarshal(seal.Payload, &sp); err != nil {
		return nil, fmt.Errorf("evidence: parse seal payload: %w", err)
	}

	// One snapshot → one checkpoint shared by every inclusion proof.
	leaves, err := leafHashesFromRecords(all)
	if err != nil {
		return nil, err
	}
	cp, err := l.signCheckpoint(len(all), merkleRoot(leaves))
	if err != nil {
		return nil, err
	}
	idxByID := make(map[string]int, len(all))
	for i, e := range all {
		idxByID[e.ID] = i
	}
	mkProof := func(id string) (*InclusionProofResponse, *Evidence, error) {
		idx, ok := idxByID[id]
		if !ok {
			return nil, nil, fmt.Errorf("evidence: record %q not in ledger", id)
		}
		return &InclusionProofResponse{
			RecordID:   id,
			LeafIndex:  idx,
			TreeSize:   len(all),
			LeafHash:   hex.EncodeToString(leaves[idx]),
			AuditPath:  hexNodes(inclusionProof(idx, leaves)),
			Checkpoint: cp,
		}, all[idx], nil
	}

	sealProof, _, err := mkProof(seal.ID)
	if err != nil {
		return nil, err
	}
	members := make([]*Evidence, 0, len(sp.MemberIDs))
	memberProofs := make([]*InclusionProofResponse, 0, len(sp.MemberIDs))
	for _, id := range sp.MemberIDs {
		pr, ev, perr := mkProof(id)
		if perr != nil {
			return nil, perr
		}
		members = append(members, ev)
		memberProofs = append(memberProofs, pr)
	}

	return &CompletenessProof{
		Namespace:     namespace,
		Seal:          seal,
		SealInclusion: sealProof,
		Members:       members,
		MemberProofs:  memberProofs,
	}, nil
}

// VerifyCompleteness verifies a CompletenessProof against a pinned public key
// with ZERO access to the running platform. It proves the Members are EXACTLY the
// set the namespace was sealed with. Any omission, addition, reorder, or edit
// fails. It returns nil on success; a descriptive error otherwise.
func VerifyCompleteness(p *CompletenessProof, pub ed25519.PublicKey) error {
	if p == nil || p.Seal == nil || p.SealInclusion == nil {
		return errors.New("evidence: incomplete completeness proof")
	}

	// 1) The seal receipt is authentic (hash + signature) and committed by the tree.
	if r := VerifyRecord(p.Seal, pub); !r.OK() {
		return fmt.Errorf("evidence: seal record failed verification: %s", r.Error)
	}
	sealLeaf, err := merkleLeafHash(p.Seal)
	if err != nil {
		return fmt.Errorf("evidence: seal leaf hash: %w", err)
	}
	if hex.EncodeToString(sealLeaf) != p.SealInclusion.LeafHash {
		return errors.New("evidence: seal leaf hash mismatch (seal record altered)")
	}
	if err := VerifyInclusionResponse(p.SealInclusion, pub); err != nil {
		return fmt.Errorf("evidence: seal inclusion: %w", err)
	}

	// 2) Parse the sealed commitment and bind it to the requested namespace.
	var sp SubtreeSeal
	if err := json.Unmarshal(p.Seal.Payload, &sp); err != nil {
		return fmt.Errorf("evidence: parse seal payload: %w", err)
	}
	if p.Namespace != "" && sp.Namespace != p.Namespace {
		return fmt.Errorf("evidence: namespace mismatch: proof %q vs seal %q", p.Namespace, sp.Namespace)
	}

	// 3) Count must match — the first line of "no more, no less".
	if len(p.Members) != sp.Count {
		return fmt.Errorf("evidence: member count %d != sealed count %d", len(p.Members), sp.Count)
	}
	if len(p.MemberProofs) != len(p.Members) {
		return errors.New("evidence: member/proof count mismatch")
	}

	// 4) Every member is authentic and committed by the SAME checkpoint as the seal.
	sealRoot := p.SealInclusion.Checkpoint.RootHash
	for i, m := range p.Members {
		if m == nil {
			return fmt.Errorf("evidence: member %d is nil", i)
		}
		if r := VerifyRecord(m, pub); !r.OK() {
			return fmt.Errorf("evidence: member %d record failed verification: %s", i, r.Error)
		}
		mp := p.MemberProofs[i]
		if mp == nil || mp.Checkpoint == nil {
			return fmt.Errorf("evidence: member %d missing inclusion proof", i)
		}
		if mp.Checkpoint.RootHash != sealRoot {
			return fmt.Errorf("evidence: member %d proof uses a different checkpoint than the seal", i)
		}
		leaf, lerr := merkleLeafHash(m)
		if lerr != nil {
			return fmt.Errorf("evidence: member %d leaf hash: %w", i, lerr)
		}
		if hex.EncodeToString(leaf) != mp.LeafHash {
			return fmt.Errorf("evidence: member %d leaf hash mismatch (record altered)", i)
		}
		if err := VerifyInclusionResponse(mp, pub); err != nil {
			return fmt.Errorf("evidence: member %d inclusion: %w", i, err)
		}
	}

	// 5) The members must hash to EXACTLY the sealed subtree root. This is the
	//    "no omission, no cherry-picking" guarantee: any drop/add/reorder/edit
	//    changes the recomputed root and fails here.
	root, err := MerkleRootHexOfRecords(p.Members)
	if err != nil {
		return fmt.Errorf("evidence: recompute subtree root: %w", err)
	}
	if root != sp.SubtreeRoot {
		return errors.New("evidence: subtree root mismatch (members are not exactly the sealed set)")
	}
	return nil
}
