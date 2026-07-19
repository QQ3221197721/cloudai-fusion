package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// completeness_test.go exercises the `cafctl verify-completeness` CLI end to end:
// a valid namespace completeness proof verifies, and an omitted member fails -
// the auditor-facing surface of Moat A / M0.

func TestVerifyCompletenessCmd(t *testing.T) {
	ctx := context.Background()
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x7c}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	l, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	ns := "redteam/engagement/cli-1"
	var members []*evidence.Evidence
	for i := 0; i < 3; i++ {
		ev, rerr := l.Record(ctx, evidence.RecordInput{
			Actor: "t", Action: "redteam.recon", Subject: ns, Payload: map[string]any{"i": i},
		})
		if rerr != nil {
			t.Fatalf("record: %v", rerr)
		}
		members = append(members, ev)
	}
	if _, err := l.SealNamespace(ctx, ns, "redteam", members); err != nil {
		t.Fatalf("seal: %v", err)
	}
	proof, err := l.BuildCompletenessProof(ctx, ns)
	if err != nil {
		t.Fatalf("build proof: %v", err)
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
	writeJSON := func(name string, v any) string {
		b, _ := json.Marshal(v)
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return p
	}

	// Valid proof -> the CLI verifies successfully.
	validPath := writeJSON("proof.json", proof)
	var out bytes.Buffer
	cmd := newVerifyCompletenessCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--proof", validPath, "--pubkey", keyPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("valid proof must verify via CLI, got: %v (out=%s)", err, out.String())
	}

	// Tamper (omit a member) -> the CLI must fail.
	proof.Members = proof.Members[:len(proof.Members)-1]
	proof.MemberProofs = proof.MemberProofs[:len(proof.MemberProofs)-1]
	tamperedPath := writeJSON("tampered.json", proof)
	cmd2 := newVerifyCompletenessCmd()
	cmd2.SetOut(&bytes.Buffer{})
	cmd2.SetArgs([]string{"--proof", tamperedPath, "--pubkey", keyPath})
	if err := cmd2.Execute(); err == nil {
		t.Fatal("tampered (omitted member) proof MUST fail via CLI")
	}
}
