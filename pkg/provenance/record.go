package provenance

import (
	"context"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// record.go makes M1's signed artifacts first-class ledger citizens: recording a
// DatasetManifest or ModelProvenance appends it to the evidence chain, so it is
// tamper-evident, inclusion-provable (GET /evidence/records/:id/proof), exportable
// (GET /evidence/export), and completeness-groupable by any fabric.Well predicate -
// on top of its own detached Ed25519 signature. Evidence is additive: a nil
// recorder is a no-op.

const (
	// ActionDatasetManifest tags a recorded DatasetManifest receipt.
	ActionDatasetManifest = "dataset.manifest"
	// ActionModelProvenance tags a recorded ModelProvenance receipt.
	ActionModelProvenance = "model.provenance"
)

// RecordManifest appends a signed manifest as a dataset.manifest receipt.
func RecordManifest(ctx context.Context, rec evidence.Recorder, m *DatasetManifest) (*evidence.Evidence, error) {
	if rec == nil || m == nil {
		return nil, nil
	}
	return rec.Record(ctx, evidence.RecordInput{
		Actor:   "provenance",
		Action:  ActionDatasetManifest,
		Subject: m.ID,
		Input:   map[string]any{"filter": m.Filter, "samples": m.SampleCount},
		Output:  map[string]any{"merkle_root": m.MerkleRoot},
		Payload: m,
		Backends: []evidence.BackendFact{
			{Component: "provenance.manifest", Mode: "real", Driver: "ed25519"},
		},
	})
}

// RecordProvenance appends a signed model provenance as a model.provenance receipt.
func RecordProvenance(ctx context.Context, rec evidence.Recorder, p *ModelProvenance) (*evidence.Evidence, error) {
	if rec == nil || p == nil {
		return nil, nil
	}
	return rec.Record(ctx, evidence.RecordInput{
		Actor:   "provenance",
		Action:  ActionModelProvenance,
		Subject: p.ID,
		Input:   map[string]any{"method": p.Method, "dataset_manifest_hash": p.DatasetManifest},
		Output:  map[string]any{"weights_hash": p.WeightsHash},
		Payload: p,
		Backends: []evidence.BackendFact{
			{Component: "provenance.model", Mode: "real", Driver: "ed25519"},
		},
	})
}
