package provenance

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// record_test.go proves the recorded artifacts are first-class ledger citizens:
// the chain still verifies, each receipt is inclusion-provable, and the embedded
// artifact still verifies against the pinned key.

func TestRecordProvenanceArtifacts(t *testing.T) {
	ctx := context.Background()
	l := newProvLedger(t, 0x41)
	pub := l.Signer().PublicKey()

	corpus := records(t, l, 3)
	cp, _ := l.Checkpoint(ctx)
	manifest, err := BuildDatasetManifest(l.Signer(), "redteam-dpo", corpus, cp)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	mev, err := RecordManifest(ctx, l, manifest)
	if err != nil || mev == nil {
		t.Fatalf("record manifest: ev=%v err=%v", mev, err)
	}

	mh, _ := ManifestHash(manifest)
	prov := &ModelProvenance{BaseModelHash: "b", DatasetManifest: mh, WeightsHash: "w", Method: "dpo"}
	if err := SignProvenance(l.Signer(), prov); err != nil {
		t.Fatalf("sign provenance: %v", err)
	}
	pev, err := RecordProvenance(ctx, l, prov)
	if err != nil || pev == nil {
		t.Fatalf("record provenance: ev=%v err=%v", pev, err)
	}

	// The whole chain (corpus + manifest + provenance receipts) still verifies.
	all, _ := l.Store().All(ctx)
	rep, _ := evidence.VerifyChain(all, pub)
	if !rep.Valid {
		t.Fatalf("chain with recorded artifacts must verify, got %+v", rep)
	}

	// The manifest receipt is inclusion-provable against a signed checkpoint.
	incl, err := l.InclusionProofByID(ctx, mev.ID)
	if err != nil || incl == nil {
		t.Fatalf("inclusion proof: proof=%v err=%v", incl, err)
	}
	if err := evidence.VerifyInclusionResponse(incl, pub); err != nil {
		t.Fatalf("manifest receipt must be inclusion-provable: %v", err)
	}

	// The embedded artifacts round-trip and still verify against the pinned key.
	var gotManifest DatasetManifest
	if err := json.Unmarshal(mev.Payload, &gotManifest); err != nil {
		t.Fatalf("decode manifest payload: %v", err)
	}
	if err := VerifyManifest(&gotManifest, pub); err != nil {
		t.Fatalf("embedded manifest must verify: %v", err)
	}
	var gotProv ModelProvenance
	if err := json.Unmarshal(pev.Payload, &gotProv); err != nil {
		t.Fatalf("decode provenance payload: %v", err)
	}
	if err := VerifyModelProvenance(&gotManifest, &gotProv, pub); err != nil {
		t.Fatalf("embedded model provenance must verify end-to-end: %v", err)
	}
}
