package zk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"sync"
)

// CapabilityComponent is the pkg/capability component name for the zk prover, so a
// simulated prover backing a consequential attestation fails a production boot.
const CapabilityComponent = "evidence.zk"

// Statement enumerates the properties the zk layer can attest to. The shipped
// circuit proves scope-compliance AND completeness-under-predicate jointly (both
// are properties of the same sealed set), so either statement maps to it.
type Statement string

const (
	StmtScopeCompliance   Statement = "scope-compliance"
	StmtCompletePredicate Statement = "completeness-under-predicate"
)

// ZKAttestation is the offline-verifiable artifact (spec §3.2). It carries the
// public inputs, the succinct proof, and the verifying-key id it must be checked
// against. It reveals NOTHING about the individual receipts.
type ZKAttestation struct {
	Statement   Statement `json:"statement"`
	PublicRoot  string    `json:"public_root"`  // hex Poseidon commitment over the members
	ScopeCommit string    `json:"scope_commit"` // hex Poseidon(namespace) — the attested scope
	Count       int       `json:"count"`        // number of members (fixes the circuit size)
	Predicate   string    `json:"predicate,omitempty"`
	Proof       []byte    `json:"proof"` // gnark Groth16 proof (BN254)
	VKID        string    `json:"vk_id"` // sha256 of the verifying key bytes
	Mode        string    `json:"mode"`  // "real" | "simulated" (honest label)
}

// Prover produces a ZKAttestation for a statement over confidential witnesses.
type Prover interface {
	// Prove returns the attestation and the serialized verifying key (published
	// out-of-band, pinned by VKID). ws are the confidential members.
	Prove(ctx context.Context, stmt Statement, predicate string, ws []LeafWitness) (*ZKAttestation, []byte, error)
	Mode() capability.Mode
}

// Groth16Prover is the REAL prover: it compiles the completeness circuit for the
// given member count, runs a Groth16 setup, and produces a succinct proof. It
// reports ModeReal to pkg/capability.
type Groth16Prover struct {
	mu      sync.RWMutex
	pkCache map[int]*groth16.ProvingKey // cached PK by circuit size n
	vkCache map[int][]byte              // cached VK bytes by circuit size n
}

func (p *Groth16Prover) Mode() capability.Mode { return capability.ModeReal }

// getCachedPK returns or computes the PK/VK pair for a given circuit size, with caching
func (p *Groth16Prover) getCachedPK(n int) (*groth16.ProvingKey, []byte, error) {
	p.mu.RLock()
	if pk, ok := p.pkCache[n]; ok {
		vkBytes, ok := p.vkCache[n]
		if ok {
			p.mu.RUnlock()
			return pk, vkBytes, nil
		}
		p.mu.RUnlock()
		// Have PK but need to compute VK
		// Have PK but need to compute VK
	} else {
	}
	// Need to compute new params
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, newCircuit(n))
	if err != nil {
		return nil, fmt.Errorf("zk: compile: %w", err)
	}
	
	pk, vk, err := groth16.Setup(ccs)
	if err != nil {
		return nil, fmt.Errorf("zk: setup: %w", err)
	}
	
	// Store in cache
	p.mu.Lock()
	if p.pkCache == nil {
		p.pkCache = make(map[int]*groth16.ProvingKey)
	}
	if p.vkCache == nil {
		p.vkCache = make(map[int][]byte)
	}
	p.pkCache[n] = pk
	vkBytes := vk.Serialize()
	p.vkCache[n] = vkBytes
	p.mu.Unlock()
	return pk, vkBytes, nil
}

func (p *Groth16Prover) Prove(ctx context.Context, stmt Statement, predicate string, ws []LeafWitness) (*ZKAttestation, []byte, error) {
	if err := capability.Report(CapabilityComponent, "groth16-bn254-poseidon2", capability.ModeReal, "real snark prover"); err != nil {
		return nil, nil, err
	}
	if len(ws) == 0 {
		return nil, nil, errors.New("zk: no members to prove")
	}
	n := len(ws)

	// 1) Get or compute PK/VK with caching
	pk, vkBytes, err := p.getCachedPK(n)
	if err != nil {
		return nil, nil, err
	}
	ccs := pk.CS // Use cached CS if available

	// 3) Public commitments computed off-circuit with the matching native Poseidon.
	root := Commitment(ws)
	scope := scopeField(ws[0].Namespace)

	// 4) Full witness (public + private) and proof.
	assignment := newCircuit(n)
	assignment.Root = root
	assignment.ScopeCommit = scope
	for i, w := range ws {
		var eidx, inScope fr.Element
		eidx.SetUint64(w.Eidx)
		if w.InScope {
			inScope.SetOne()
		}
		assignment.Namespace[i] = w.Namespace
		assignment.Eidx[i] = eidx
		assignment.InScope[i] = inScope
		assignment.PayloadHash[i] = w.PayloadHash
	}
	fullWitness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
	if err != nil {
		return nil, nil, fmt.Errorf("zk: witness: %w", err)
	}
	proof, err := groth16.Prove(ccs, pk, fullWitness)
	if err != nil {
		// A scope violation (InScope != 1) or a wrong commitment makes the circuit
		// unsatisfiable — the prover CANNOT forge a passing proof. That is the point.
		return nil, nil, fmt.Errorf("zk: prove (witness does not satisfy the statement): %w", err)
	}

	// 5) Serialize proof + vk; VKID pins the key.
	var pbuf, vbuf bytes.Buffer
	if _, err := proof.WriteTo(&pbuf); err != nil {
		return nil, nil, fmt.Errorf("zk: serialize proof: %w", err)
	}
	if _, err := vk.WriteTo(&vbuf); err != nil {
		return nil, nil, fmt.Errorf("zk: serialize vk: %w", err)
	}
	vkBytes := vbuf.Bytes()
	att := &ZKAttestation{
		Statement:   stmt,
		PublicRoot:  feHex(root),
		ScopeCommit: feHex(scope),
		Count:       n,
		Predicate:   predicate,
		Proof:       pbuf.Bytes(),
		VKID:        vkID(vkBytes),
		Mode:        string(capability.ModeReal),
	}
	return att, vkBytes, nil
}

// DryRunProver is the SIMULATED prover: it computes the public commitments (so the
// binding to A0 still holds for dev/tests) but produces NO cryptographic proof and
// reports ModeSimulated — so pkg/capability blocks it from backing a consequential
// attestation in production, per the platform's real-vs-simulated discipline.
type DryRunProver struct{}

func (DryRunProver) Mode() capability.Mode { return capability.ModeSimulated }

func (DryRunProver) Prove(ctx context.Context, stmt Statement, predicate string, ws []LeafWitness) (*ZKAttestation, []byte, error) {
	if err := capability.Report(CapabilityComponent, "dry-run", capability.ModeSimulated, "no snark prover (labeled)"); err != nil {
		return nil, nil, err // errors in production — exactly the intended gate
	}
	if len(ws) == 0 {
		return nil, nil, errors.New("zk: no members to prove")
	}
	root := Commitment(ws)
	scope := scopeField(ws[0].Namespace)
	return &ZKAttestation{
		Statement:   stmt,
		PublicRoot:  feHex(root),
		ScopeCommit: feHex(scope),
		Count:       len(ws),
		Predicate:   predicate,
		Proof:       nil,
		VKID:        "",
		Mode:        string(capability.ModeSimulated),
	}, nil, nil
}

// VerifyZK verifies an attestation against a pinned verifying key, fully offline.
// It returns nil iff the proof is valid for the attestation's public inputs and the
// vk matches VKID. A simulated (proof-less) attestation is rejected.
func VerifyZK(att *ZKAttestation, vkBytes []byte) error {
	if att == nil {
		return errors.New("zk: nil attestation")
	}
	if len(att.Proof) == 0 {
		return errors.New("zk: attestation has no proof (simulated/dry-run cannot be verified)")
	}
	if vkID(vkBytes) != att.VKID {
		return errors.New("zk: verifying key does not match attestation VKID")
	}
	root, err := feFromHex(att.PublicRoot)
	if err != nil {
		return fmt.Errorf("zk: bad public_root: %w", err)
	}
	scope, err := feFromHex(att.ScopeCommit)
	if err != nil {
		return fmt.Errorf("zk: bad scope_commit: %w", err)
	}

	vk := groth16.NewVerifyingKey(ecc.BN254)
	if _, err := vk.ReadFrom(bytes.NewReader(vkBytes)); err != nil {
		return fmt.Errorf("zk: read vk: %w", err)
	}
	proof := groth16.NewProof(ecc.BN254)
	if _, err := proof.ReadFrom(bytes.NewReader(att.Proof)); err != nil {
		return fmt.Errorf("zk: read proof: %w", err)
	}

	// Public witness: only Root + ScopeCommit are public; sizes follow Count.
	pub := newCircuit(att.Count)
	pub.Root = root
	pub.ScopeCommit = scope
	for i := 0; i < att.Count; i++ {
		var zero fr.Element
		pub.Namespace[i] = zero
		pub.Eidx[i] = zero
		pub.InScope[i] = zero
		pub.PayloadHash[i] = zero
	}
	pubWitness, err := frontend.NewWitness(pub, ecc.BN254.ScalarField(), frontend.PublicOnly())
	if err != nil {
		return fmt.Errorf("zk: public witness: %w", err)
	}
	if err := groth16.Verify(proof, vk, pubWitness); err != nil {
		return fmt.Errorf("zk: proof verification failed: %w", err)
	}
	return nil
}

func vkID(vkBytes []byte) string {
	sum := sha256.Sum256(vkBytes)
	return hex.EncodeToString(sum[:])
}
