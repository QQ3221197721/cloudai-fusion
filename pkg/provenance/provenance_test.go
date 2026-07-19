package provenance

import (
	"bytes"
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// provenance_test.go proves Moat B: a signed DatasetManifest + ModelProvenance
// verify offline and bind weights to an exact, signed corpus - and any tamper
// (corpus root, weights, binding, or key) fails.

func newProvLedger(t *testing.T, seed byte) *evidence.Ledger {
	t.Helper()
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{seed}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	l, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	return l
}

func records(t *testing.T, l *evidence.Ledger, n int) []*evidence.Evidence {
	t.Helper()
	out := make([]*evidence.Evidence, 0, n)
	for i := 0; i < n; i++ {
		ev, err := l.Record(context.Background(), evidence.RecordInput{
			Actor: "trainer", Action: "trace.sample", Subject: "s", Payload: map[string]any{"i": i},
		})
		if err != nil {
			t.Fatalf("record: %v", err)
		}
		out = append(out, ev)
	}
	return out
}

func TestModelProvenance_VerifiesAndBinds(t *testing.T) {
	ctx := context.Background()
	l := newProvLedger(t, 0x21)
	pub := l.Signer().PublicKey()

	corpus := records(t, l, 4)
	cp, err := l.Checkpoint(ctx)
	if err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	manifest, err := BuildDatasetManifest(l.Signer(), "redteam-dpo", corpus, cp)
	if err != nil {
		t.Fatalf("build manifest: %v", err)
	}
	if manifest.SampleCount != 4 {
		t.Fatalf("expected 4 samples, got %d", manifest.SampleCount)
	}

	mh, err := ManifestHash(manifest)
	if err != nil {
		t.Fatalf("manifest hash: %v", err)
	}
	prov := &ModelProvenance{
		BaseModelHash:   "sha256:base",
		DatasetManifest: mh,
		TrainConfigHash: "sha256:cfg",
		WeightsHash:     "sha256:weights",
		Method:          "dpo",
		Trainer:         "advanced_trainer.py",
	}
	if err := SignProvenance(l.Signer(), prov); err != nil {
		t.Fatalf("sign provenance: %v", err)
	}

	if err := VerifyModelProvenance(manifest, prov, pub); err != nil {
		t.Fatalf("valid model provenance must verify, got: %v", err)
	}

	// Tamper the corpus commitment -> manifest signature fails.
	bad := *manifest
	bad.MerkleRoot = "deadbeef"
	if err := VerifyModelProvenance(&bad, prov, pub); err == nil {
		t.Fatal("tampered corpus root MUST fail")
	}

	// Tamper the weights -> provenance signature fails.
	badProv := *prov
	badProv.WeightsHash = "sha256:swapped"
	if err := VerifyModelProvenance(manifest, &badProv, pub); err == nil {
		t.Fatal("tampered weights hash MUST fail")
	}

	// Wrong key -> fails.
	other := newProvLedger(t, 0x22)
	if err := VerifyModelProvenance(manifest, prov, other.Signer().PublicKey()); err == nil {
		t.Fatal("verification under the wrong key MUST fail")
	}
}

func TestModelProvenance_DetectsWrongCorpusBinding(t *testing.T) {
	ctx := context.Background()
	l := newProvLedger(t, 0x31)
	pub := l.Signer().PublicKey()

	cp, _ := l.Checkpoint(ctx)
	manifestA, err := BuildDatasetManifest(l.Signer(), "corpus-A", records(t, l, 3), cp)
	if err != nil {
		t.Fatalf("manifest A: %v", err)
	}
	// The model is bound to corpus A.
	mhA, _ := ManifestHash(manifestA)
	prov := &ModelProvenance{BaseModelHash: "b", DatasetManifest: mhA, WeightsHash: "w", Method: "sft"}
	if err := SignProvenance(l.Signer(), prov); err != nil {
		t.Fatalf("sign: %v", err)
	}

	// A DIFFERENT, validly-signed corpus B must not satisfy the binding.
	cp2, _ := l.Checkpoint(ctx)
	manifestB, err := BuildDatasetManifest(l.Signer(), "corpus-B", records(t, l, 5), cp2)
	if err != nil {
		t.Fatalf("manifest B: %v", err)
	}
	if err := VerifyModelProvenance(manifestB, prov, pub); err == nil {
		t.Fatal("a model must NOT verify against a corpus it was not trained on")
	}
	// Sanity: it still verifies against its real corpus.
	if err := VerifyModelProvenance(manifestA, prov, pub); err != nil {
		t.Fatalf("model must verify against its real corpus, got: %v", err)
	}
}
