package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/provenance"
)

// provenance_test.go exercises `cafctl verify-model-provenance`: a valid signed
// manifest+provenance verifies, and tampering the weights fails - the auditor
// surface of Moat B (SLSA-for-models).

func TestVerifyModelProvenanceCmd(t *testing.T) {
	ctx := context.Background()
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x4d}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	l, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}

	var corpus []*evidence.Evidence
	for i := 0; i < 3; i++ {
		ev, rerr := l.Record(ctx, evidence.RecordInput{
			Actor: "trainer", Action: "trace.sample", Subject: "s", Payload: map[string]any{"i": i},
		})
		if rerr != nil {
			t.Fatalf("record: %v", rerr)
		}
		corpus = append(corpus, ev)
	}
	cp, _ := l.Checkpoint(ctx)
	manifest, err := provenance.BuildDatasetManifest(l.Signer(), "redteam-dpo", corpus, cp)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	mh, _ := provenance.ManifestHash(manifest)
	prov := &provenance.ModelProvenance{
		BaseModelHash: "sha256:base", DatasetManifest: mh, WeightsHash: "sha256:w", Method: "dpo",
	}
	if err := provenance.SignProvenance(l.Signer(), prov); err != nil {
		t.Fatalf("sign provenance: %v", err)
	}

	dir := t.TempDir()
	pemBytes, err := evidence.MarshalPublicKeyPEM(l.Signer().PublicKey())
	if err != nil {
		t.Fatalf("pem: %v", err)
	}
	keyPath := filepath.Join(dir, "key.pem")
	if err := os.WriteFile(keyPath, pemBytes, 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	write := func(name string, v any) string {
		b, _ := json.Marshal(v)
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return p
	}
	mPath := write("manifest.json", manifest)
	pPath := write("prov.json", prov)

	// Valid -> the CLI verifies.
	cmd := newVerifyModelProvenanceCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--manifest", mPath, "--provenance", pPath, "--pubkey", keyPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("valid model provenance must verify via CLI, got: %v", err)
	}

	// Tamper the weights (signature no longer matches) -> the CLI must fail.
	prov.WeightsHash = "sha256:swapped"
	pPath2 := write("prov2.json", prov)
	cmd2 := newVerifyModelProvenanceCmd()
	cmd2.SetOut(&bytes.Buffer{})
	cmd2.SetArgs([]string{"--manifest", mPath, "--provenance", pPath2, "--pubkey", keyPath})
	if err := cmd2.Execute(); err == nil {
		t.Fatal("tampered weights MUST fail via CLI")
	}
}
