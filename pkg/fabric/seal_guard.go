package fabric

import (
	"context"
	"encoding/json"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// seal_guard.go completes M0's acceptance "the gate refuses post-seal actions"
// (docs/verifiable-moat-spec.md §9 M0) for the GENERIC wells. A SubtreeSeal proves a
// namespace was complete WHEN sealed; without enforcement an operator could keep
// acting in a sealed namespace and still present the old seal as "this is
// everything." These helpers detect exactly that: any member of a well not covered
// by its latest seal is post-seal activity - so a control plane can refuse it and an
// auditor can catch a seal-then-keep-acting attempt. No new cryptography.

// latestSeal returns the most recent SubtreeSeal evidence for a namespace and its
// decoded body, or (nil, nil, nil) if the namespace has never been sealed.
func (f *Fabric) latestSeal(ctx context.Context, namespace string) (*evidence.Evidence, *evidence.SubtreeSeal, error) {
	all, err := f.ledger.Store().All(ctx)
	if err != nil {
		return nil, nil, err
	}
	var bestEv *evidence.Evidence
	var bestSeal *evidence.SubtreeSeal
	for _, e := range all {
		if e.Action != evidence.ActionSubtreeSeal {
			continue
		}
		var s evidence.SubtreeSeal
		if json.Unmarshal(e.Payload, &s) != nil || s.Namespace != namespace {
			continue
		}
		if bestEv == nil || e.Seq > bestEv.Seq {
			cp := s
			bestEv, bestSeal = e, &cp
		}
	}
	return bestEv, bestSeal, nil
}

// Sealed reports whether (well, key) has been sealed at least once.
func (f *Fabric) Sealed(ctx context.Context, wellName, key string) (bool, error) {
	w, err := f.well(wellName)
	if err != nil {
		return false, err
	}
	ev, _, err := f.latestSeal(ctx, w.Namespace(key))
	if err != nil {
		return false, err
	}
	return ev != nil, nil
}

// PostSealMembers returns the well members NOT covered by the latest seal - i.e.
// activity recorded after the namespace was sealed. It is empty for an intact seal.
// If the namespace was never sealed it returns all current members (every member is
// "uncovered"); callers that care should check Sealed first.
func (f *Fabric) PostSealMembers(ctx context.Context, wellName, key string) ([]*evidence.Evidence, error) {
	w, err := f.well(wellName)
	if err != nil {
		return nil, err
	}
	_, seal, err := f.latestSeal(ctx, w.Namespace(key))
	if err != nil {
		return nil, err
	}
	members, err := f.Members(ctx, wellName, key)
	if err != nil {
		return nil, err
	}
	if seal == nil {
		return members, nil
	}
	sealed := make(map[string]bool, len(seal.MemberIDs))
	for _, id := range seal.MemberIDs {
		sealed[id] = true
	}
	out := make([]*evidence.Evidence, 0)
	for _, e := range members {
		if !sealed[e.ID] {
			out = append(out, e)
		}
	}
	return out, nil
}

// SealIntact reports whether (well, key) is sealed AND no member escaped the seal
// (no post-seal activity). It is the enforcement predicate: honor "this namespace is
// complete" only when SealIntact is true; a post-seal action flips it to false.
func (f *Fabric) SealIntact(ctx context.Context, wellName, key string) (bool, error) {
	sealed, err := f.Sealed(ctx, wellName, key)
	if err != nil || !sealed {
		return false, err
	}
	post, err := f.PostSealMembers(ctx, wellName, key)
	if err != nil {
		return false, err
	}
	return len(post) == 0, nil
}
