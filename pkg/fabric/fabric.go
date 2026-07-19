// Package fabric is the unifying layer of the Verifiable Fabric
// (docs/verifiable-moat-spec.md §5.0): every pillar's consequential receipts are
// grouped, sealed, and completeness-proven through ONE namespace-parametric
// primitive (pkg/evidence: SubtreeSeal / CompletenessProof / VerifyCompleteness).
//
// The architectural point (elevation, not patch): onboarding a new "well" — a
// red-team engagement, a scheduling tenant, a FinOps month, or something not yet
// imagined — costs exactly ONE Register call with a single predicate that maps a
// receipt to its key. No new cryptography, no changes to the spine. The system is
// thereby *closed under composition* (any well's receipts verify with the same
// tooling) and *open for extension* (the Nth well is uniform). That invariant is
// the moat, not any single well.
//
// fabric depends only on pkg/evidence, so pillars never import it and no cycle can
// form; a wiring site (e.g. cmd/apiserver) registers each pillar's exported
// KeyFunc into a single Fabric.
package fabric

import (
	"context"
	"fmt"
	"sync"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// Ledger is the minimal evidence surface the Fabric needs. *evidence.Ledger
// satisfies it. Keeping it an interface makes the Fabric unit-testable and keeps
// the dependency one-directional (fabric -> evidence only).
type Ledger interface {
	SealNamespace(ctx context.Context, namespace, actor string, members []*evidence.Evidence) (*evidence.Evidence, error)
	BuildCompletenessProof(ctx context.Context, namespace string) (*evidence.CompletenessProof, error)
	Store() evidence.Store
}

// KeyFunc maps a receipt to the well-specific key it belongs to (an engagement ID,
// a tenant, a month, ...), or "" if the receipt is not part of this well. It is
// the ONLY per-pillar code the Fabric needs.
type KeyFunc func(*evidence.Evidence) string

// Well is a verifiable-completeness participant. Name identifies it; Prefix +
// key form the evidence namespace; Actor labels the seal receipt; KeyOf assigns
// each receipt to a key.
type Well struct {
	Name   string
	Prefix string
	Actor  string
	KeyOf  KeyFunc
}

// Namespace returns the evidence namespace for one instance (key) of the well.
func (w Well) Namespace(key string) string { return w.Prefix + "/" + key }

// Fabric holds the registered wells and seals/proves completeness for all of them
// through the same evidence primitive. It adds no new cryptography.
type Fabric struct {
	ledger Ledger
	mu     sync.RWMutex
	wells  map[string]Well
}

// New builds a Fabric over an evidence ledger.
func New(ledger Ledger) *Fabric {
	return &Fabric{ledger: ledger, wells: make(map[string]Well)}
}

// Register adds a well. This single call is the entire cost of onboarding a pillar
// (spec's "open for extension"). It rejects incomplete or duplicate wells.
func (f *Fabric) Register(w Well) error {
	if w.Name == "" || w.Prefix == "" || w.KeyOf == nil {
		return fmt.Errorf("fabric: well needs Name, Prefix, and KeyOf")
	}
	if w.Actor == "" {
		w.Actor = w.Name
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.wells[w.Name]; exists {
		return fmt.Errorf("fabric: well %q already registered", w.Name)
	}
	f.wells[w.Name] = w
	return nil
}

// Wells returns the registered well names (for introspection / capabilities).
func (f *Fabric) Wells() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]string, 0, len(f.wells))
	for name := range f.wells {
		out = append(out, name)
	}
	return out
}

func (f *Fabric) well(name string) (Well, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	w, ok := f.wells[name]
	if !ok {
		return Well{}, fmt.Errorf("fabric: no well %q registered", name)
	}
	return w, nil
}

// Members returns the receipts belonging to (well, key) in ascending Seq order.
// Seal receipts are always excluded — a completeness set never seals prior seals.
func (f *Fabric) Members(ctx context.Context, wellName, key string) ([]*evidence.Evidence, error) {
	w, err := f.well(wellName)
	if err != nil {
		return nil, err
	}
	all, err := f.ledger.Store().All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*evidence.Evidence, 0)
	for _, e := range all {
		if e.Action == evidence.ActionSubtreeSeal {
			continue
		}
		if w.KeyOf(e) == key {
			out = append(out, e)
		}
	}
	return out, nil
}

// Seal seals (well, key)'s current members so their completeness is provable. It
// is the single uniform entry point that replaces the per-pillar SealX methods.
func (f *Fabric) Seal(ctx context.Context, wellName, key string) (*evidence.Evidence, error) {
	w, err := f.well(wellName)
	if err != nil {
		return nil, err
	}
	members, err := f.Members(ctx, wellName, key)
	if err != nil {
		return nil, err
	}
	return f.ledger.SealNamespace(ctx, w.Namespace(key), w.Actor, members)
}

// Completeness returns the offline-verifiable completeness proof for (well, key).
// Verify it with evidence.VerifyCompleteness against the pinned public key.
func (f *Fabric) Completeness(ctx context.Context, wellName, key string) (*evidence.CompletenessProof, error) {
	w, err := f.well(wellName)
	if err != nil {
		return nil, err
	}
	return f.ledger.BuildCompletenessProof(ctx, w.Namespace(key))
}
