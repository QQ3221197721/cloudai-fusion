package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/fabric"
)

// saga_test.go exercises `cafctl verify-saga`: a completed cross-pillar saga's
// completeness proof verifies and reports its outcome, while dropping a step fails
// - the MF Choreographer auditor surface.

func TestVerifySagaCmd(t *testing.T) {
	ctx := context.Background()
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x6d}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	l, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}

	ch := fabric.NewChoreographer(l, nil)
	if _, err := ch.Run(ctx, "saga-x", []fabric.SagaStep{
		{Name: "quarantine", Do: func(context.Context) error { return nil }},
		{Name: "redeploy", Do: func(context.Context) error { return nil }},
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, err := ch.Seal(ctx, "saga-x"); err != nil {
		t.Fatalf("seal: %v", err)
	}
	proof, err := ch.Proof(ctx, "saga-x")
	if err != nil {
		t.Fatalf("proof: %v", err)
	}

	dir := t.TempDir()
	pemBytes, err := evidence.MarshalPublicKeyPEM(signer.PublicKey())
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

	// Valid saga proof -> the CLI verifies and reports the outcome.
	pPath := write("saga.json", proof)
	cmd := newVerifySagaCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--proof", pPath, "--pubkey", keyPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("a valid saga must verify, got: %v", err)
	}

	// Tamper (hide a step) -> the CLI must fail.
	proof.Members = proof.Members[:len(proof.Members)-1]
	proof.MemberProofs = proof.MemberProofs[:len(proof.MemberProofs)-1]
	tPath := write("tampered.json", proof)
	cmd2 := newVerifySagaCmd()
	cmd2.SetOut(&bytes.Buffer{})
	cmd2.SetArgs([]string{"--proof", tPath, "--pubkey", keyPath})
	if err := cmd2.Execute(); err == nil {
		t.Fatal("a tampered saga proof (hidden step) MUST fail")
	}
}
