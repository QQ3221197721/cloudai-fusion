package redteam

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// witness_library.go is the RT-1 COMPOUNDING ASSET (docs/verifiable-moat-spec.md
// §2.5, §5.4d): an append-only, deduplicated, ledger-backed corpus of minimized
// exploit witnesses that were PROVEN to reproduce (and, when remediated, proven
// fixed). Every engagement deposits its minimal, replayable witnesses; the library
// only grows. A competitor starting later cannot retroactively own this signed
// history of "attacks tried → defended → proven fixed" — that time-accumulated,
// verifiable corpus is the structural moat that a clever re-implementation is not.
//
// Each deposit is a signed, hash-chained receipt, so the library is itself
// tamper-evident and independently verifiable (cafctl verify / verify-remediation).

// ActionWitnessDeposit tags a witness-library deposit receipt.
const ActionWitnessDeposit = "redteam.witness.deposit"

// LibraryEntry is the queryable metadata of one deposited witness.
type LibraryEntry struct {
	WitnessHash string `json:"witness_hash"`
	Technique   string `json:"technique"` // MITRE ATT&CK id
	FindingID   string `json:"finding_id"`
	StepCount   int    `json:"step_count"` // minimized step count
	Fixed       bool   `json:"fixed"`      // has a valid differential remediation
	DepositedAt string `json:"deposited_at"`
}

// depositRecord is the stored payload: the queryable entry + the reusable witness.
type depositRecord struct {
	Entry   LibraryEntry   `json:"entry"`
	Witness ExploitWitness `json:"witness"`
}

// libraryLedger is the minimal ledger surface DepositWitness needs.
type libraryLedger interface {
	evidence.Recorder
	Store() evidence.Store
}

// DepositWitness verifies the proof(s) bind exactly this witness, deduplicates by
// witness hash, and (if new) records a signed redteam.witness.deposit receipt. It
// returns the entry and whether it was newly added. `after` is optional: with it,
// the deposit records a proven remediation (Fixed=true); without it, a
// proven-exploitable (not-yet-fixed) witness.
func DepositWitness(ctx context.Context, l libraryLedger, w ExploitWitness, before, after *ExploitProof, pub ed25519.PublicKey) (*LibraryEntry, bool, error) {
	if l == nil {
		return nil, false, errors.New("redteam: witness library needs a ledger")
	}
	wh, err := WitnessHash(w)
	if err != nil {
		return nil, false, err
	}
	if before == nil {
		return nil, false, errors.New("redteam: deposit needs a pre-fix proof")
	}
	if err := VerifyExploitProof(before, pub); err != nil {
		return nil, false, fmt.Errorf("redteam: deposit before proof: %w", err)
	}
	if before.WitnessHash != wh {
		return nil, false, errors.New("redteam: before proof binds a different witness")
	}
	if !before.Reproduced {
		return nil, false, errors.New("redteam: before proof did not reproduce (nothing to deposit)")
	}
	fixed := false
	if after != nil {
		if err := VerifyRemediation(before, after, pub); err != nil {
			return nil, false, fmt.Errorf("redteam: deposit remediation: %w", err)
		}
		fixed = true
	}

	// Deduplicate by witness hash: the corpus grows, but never with duplicates.
	existing, err := LoadWitnessLibrary(ctx, l.Store(), pub)
	if err != nil {
		return nil, false, err
	}
	for i := range existing {
		if existing[i].WitnessHash == wh {
			e := existing[i]
			return &e, false, nil
		}
	}

	entry := LibraryEntry{
		WitnessHash: wh,
		Technique:   w.Technique,
		FindingID:   w.FindingID,
		StepCount:   len(w.Steps),
		Fixed:       fixed,
		DepositedAt: common.NowUTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if _, err := l.Record(ctx, evidence.RecordInput{
		Actor:   "redteam",
		Action:  ActionWitnessDeposit,
		Subject: w.Technique,
		Input:   map[string]any{"technique": w.Technique, "finding_id": w.FindingID, "witness_hash": wh},
		Output:  map[string]any{"step_count": len(w.Steps), "fixed": fixed},
		Payload: depositRecord{Entry: entry, Witness: w},
		Backends: []evidence.BackendFact{
			{Component: "redteam.witness", Mode: "real", Driver: "minimized-witness"},
		},
	}); err != nil {
		return nil, false, err
	}
	return &entry, true, nil
}

// LoadWitnessLibrary projects the deposited witnesses from the ledger. The chain's
// own verification (evidence.VerifyChain) covers integrity; this is the read-model.
func LoadWitnessLibrary(ctx context.Context, store evidence.Store, _ ed25519.PublicKey) ([]LibraryEntry, error) {
	if store == nil {
		return nil, errors.New("redteam: witness library needs a store")
	}
	all, err := store.All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]LibraryEntry, 0)
	for _, e := range all {
		if e.Action != ActionWitnessDeposit {
			continue
		}
		var dr depositRecord
		if json.Unmarshal(e.Payload, &dr) != nil {
			continue
		}
		out = append(out, dr.Entry)
	}
	return out, nil
}

// WitnessByHash returns the reusable minimized witness for a hash, if deposited —
// so a future engagement can REPLAY a known technique without re-deriving it.
func WitnessByHash(ctx context.Context, store evidence.Store, witnessHash string) (*ExploitWitness, error) {
	all, err := store.All(ctx)
	if err != nil {
		return nil, err
	}
	for _, e := range all {
		if e.Action != ActionWitnessDeposit {
			continue
		}
		var dr depositRecord
		if json.Unmarshal(e.Payload, &dr) != nil {
			continue
		}
		if dr.Entry.WitnessHash == witnessHash {
			w := dr.Witness
			return &w, nil
		}
	}
	return nil, fmt.Errorf("redteam: no deposited witness %q", witnessHash)
}

// ByTechnique filters library entries by MITRE technique id.
func ByTechnique(entries []LibraryEntry, technique string) []LibraryEntry {
	out := make([]LibraryEntry, 0)
	for _, e := range entries {
		if e.Technique == technique {
			out = append(out, e)
		}
	}
	return out
}
