// Package provenance implements Moat B (docs/verifiable-moat-spec.md §4):
// provenance-verifiable learning. It is the second spine primitive alongside
// completeness, and like it, it is DOMAIN-AGNOSTIC: the same signed DatasetManifest
// + ModelProvenance proves the training provenance of the red-team planner, the RL
// scheduler, or any future model.
//
// The guarantee: a DatasetManifest is a signed commitment to the EXACT set of
// evidence records a corpus was mined from (their RFC 6962 Merkle root + the STH
// they were cut from). A ModelProvenance binds produced weights to that manifest,
// the base model, and the training config. Together they extend SLSA-style
// provenance from build artifacts to MODEL WEIGHTS + TRAINING DATA - "these weights
// were trained only on this signed, in-scope corpus," offline-verifiable via
// `cafctl verify-model-provenance`.
//
// provenance depends only on pkg/evidence (its Ed25519 signer, canonical hashing,
// Merkle root, and Checkpoint), so it never creates an import cycle.
package provenance

import (
	"crypto/ed25519"
	"fmt"
	"sort"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

const (
	domainManifest   = "cloudai-fusion/dataset-manifest/v1"
	domainProvenance = "cloudai-fusion/model-provenance/v1"
)

// DatasetManifest is a signed commitment to a training corpus derived from the
// evidence ledger: the exact source records (by Merkle root), the STH they were
// cut from, and a human-readable filter. Signing makes the corpus's provenance
// verifiable - you can prove which signed, in-scope records produced it.
type DatasetManifest struct {
	ID          string               `json:"id"`
	Filter      string               `json:"filter"`       // selection description, e.g. "redteam-dpo"
	SampleCount int                  `json:"sample_count"` // number of source records
	MerkleRoot  string               `json:"merkle_root"`  // hex RFC 6962 root over source-record leaf hashes
	Checkpoint  *evidence.Checkpoint `json:"checkpoint,omitempty"`
	CreatedAt   string               `json:"created_at"`
	KeyID       string               `json:"key_id"`
	Signature   string               `json:"signature"` // base64 Ed25519 over the domain-separated content hash
}

// ModelProvenance binds a produced model artifact to the EXACT inputs that made
// it: base model, dataset manifest, and training config. It is SLSA-for-models.
type ModelProvenance struct {
	ID              string `json:"id"`
	BaseModelHash   string `json:"base_model_hash"`
	DatasetManifest string `json:"dataset_manifest_hash"` // ManifestHash of the bound DatasetManifest
	TrainConfigHash string `json:"train_config_hash"`
	WeightsHash     string `json:"weights_hash"`
	Method          string `json:"method"` // "dpo" | "sft" | ...
	Trainer         string `json:"trainer"`
	CreatedAt       string `json:"created_at"`
	KeyID           string `json:"key_id"`
	Signature       string `json:"signature"`
}

// contentHash returns a domain-separated canonical sha256 over v, so a manifest
// signature can never be replayed as a provenance signature (or an evidence leaf).
func contentHash(domain string, v any) (string, error) {
	return evidence.HashAny(struct {
		Domain string `json:"domain"`
		Body   any    `json:"body"`
	}{Domain: domain, Body: v})
}

// BuildDatasetManifest builds and signs a manifest committing to records (the
// corpus source) and an optional checkpoint. Records are committed in ascending
// Seq order so the Merkle root is deterministic and independently reproducible.
func BuildDatasetManifest(s evidence.Signer, filter string, records []*evidence.Evidence, cp *evidence.Checkpoint) (*DatasetManifest, error) {
	if s == nil {
		return nil, fmt.Errorf("provenance: a signer is required")
	}
	ordered := append([]*evidence.Evidence(nil), records...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Seq < ordered[j].Seq })
	root, err := evidence.MerkleRootHexOfRecords(ordered)
	if err != nil {
		return nil, fmt.Errorf("provenance: corpus merkle root: %w", err)
	}
	m := &DatasetManifest{
		ID:          common.NewUUID(),
		Filter:      filter,
		SampleCount: len(ordered),
		MerkleRoot:  root,
		Checkpoint:  cp,
		CreatedAt:   common.NowUTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if err := SignManifest(s, m); err != nil {
		return nil, err
	}
	return m, nil
}

// SignManifest sets KeyID and Signature over the manifest's domain-separated hash.
func SignManifest(s evidence.Signer, m *DatasetManifest) error {
	m.KeyID = s.KeyID()
	m.Signature = ""
	h, err := contentHash(domainManifest, *m)
	if err != nil {
		return err
	}
	sig, err := s.Sign([]byte(h))
	if err != nil {
		return err
	}
	m.Signature = sig
	return nil
}

// VerifyManifest checks a manifest's Ed25519 signature against a pinned key.
func VerifyManifest(m *DatasetManifest, pub ed25519.PublicKey) error {
	if m == nil {
		return fmt.Errorf("provenance: nil manifest")
	}
	unsigned := *m
	unsigned.Signature = ""
	h, err := contentHash(domainManifest, unsigned)
	if err != nil {
		return err
	}
	if err := evidence.VerifyLeaf(pub, []byte(h), m.Signature); err != nil {
		return fmt.Errorf("provenance: manifest signature: %w", err)
	}
	return nil
}

// ManifestHash is the canonical hash of a (signed) manifest, used to bind a
// ModelProvenance to an exact manifest.
func ManifestHash(m *DatasetManifest) (string, error) {
	return evidence.HashAny(m)
}

// SignProvenance sets KeyID and Signature over the provenance's content hash.
func SignProvenance(s evidence.Signer, p *ModelProvenance) error {
	if p.ID == "" {
		p.ID = common.NewUUID()
	}
	if p.CreatedAt == "" {
		p.CreatedAt = common.NowUTC().Format("2006-01-02T15:04:05Z07:00")
	}
	p.KeyID = s.KeyID()
	p.Signature = ""
	h, err := contentHash(domainProvenance, *p)
	if err != nil {
		return err
	}
	sig, err := s.Sign([]byte(h))
	if err != nil {
		return err
	}
	p.Signature = sig
	return nil
}

// VerifyProvenance checks a provenance record's signature against a pinned key.
func VerifyProvenance(p *ModelProvenance, pub ed25519.PublicKey) error {
	if p == nil {
		return fmt.Errorf("provenance: nil provenance")
	}
	unsigned := *p
	unsigned.Signature = ""
	h, err := contentHash(domainProvenance, unsigned)
	if err != nil {
		return err
	}
	if err := evidence.VerifyLeaf(pub, []byte(h), p.Signature); err != nil {
		return fmt.Errorf("provenance: provenance signature: %w", err)
	}
	return nil
}

// VerifyModelProvenance is the end-to-end check an auditor runs: both signatures
// verify AND the provenance is bound to exactly this manifest. On success it
// proves "these weights were produced from this signed, in-scope corpus."
func VerifyModelProvenance(m *DatasetManifest, p *ModelProvenance, pub ed25519.PublicKey) error {
	if err := VerifyManifest(m, pub); err != nil {
		return err
	}
	if err := VerifyProvenance(p, pub); err != nil {
		return err
	}
	mh, err := ManifestHash(m)
	if err != nil {
		return err
	}
	if mh != p.DatasetManifest {
		return fmt.Errorf("provenance: model is not bound to this manifest (hash mismatch)")
	}
	return nil
}
