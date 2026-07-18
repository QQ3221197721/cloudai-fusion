package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

func writePubkey(t *testing.T, pub ed25519.PublicKey) string {
	t.Helper()
	pem, err := evidence.MarshalPublicKeyPEM(pub)
	if err != nil {
		t.Fatalf("pem: %v", err)
	}
	p := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(p, pem, 0o600); err != nil {
		t.Fatalf("write pubkey: %v", err)
	}
	return p
}

func ledgerWithRecords(t *testing.T, n int) *evidence.Ledger {
	t.Helper()
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x21}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	l, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	for i := 0; i < n; i++ {
		if _, err := l.Record(context.Background(), evidence.RecordInput{
			Actor: "t", Action: "a", Subject: "s", Payload: map[string]any{"i": i},
		}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	return l
}

func TestVerifyInclusionCmd(t *testing.T) {
	l := ledgerWithRecords(t, 6)
	keyPath := writePubkey(t, l.Signer().PublicKey())
	all, _ := l.Store().All(context.Background())
	ip, _ := l.InclusionProofByID(context.Background(), all[2].ID)
	data, _ := json.Marshal(ip)

	cmd := newVerifyInclusionCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetIn(bytes.NewReader(data))
	cmd.SetArgs([]string{"--pubkey", keyPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("verify-inclusion should pass: %v\n%s", err, out.String())
	}

	// Tampering the leaf hash must make the command fail.
	repl := "ff"
	if ip.LeafHash[:2] == "ff" {
		repl = "00"
	}
	ip.LeafHash = repl + ip.LeafHash[2:]
	bad, _ := json.Marshal(ip)
	cmd2 := newVerifyInclusionCmd()
	cmd2.SetOut(&bytes.Buffer{})
	cmd2.SetIn(bytes.NewReader(bad))
	cmd2.SetArgs([]string{"--pubkey", keyPath})
	if err := cmd2.Execute(); err == nil {
		t.Fatal("tampered inclusion proof must fail verification")
	}
}

func TestVerifyConsistencyCmd(t *testing.T) {
	l := ledgerWithRecords(t, 8)
	keyPath := writePubkey(t, l.Signer().PublicKey())
	cpr, _ := l.ConsistencyProof(context.Background(), 3, 8)
	data, _ := json.Marshal(cpr)

	cmd := newVerifyConsistencyCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetIn(bytes.NewReader(data))
	cmd.SetArgs([]string{"--pubkey", keyPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("verify-consistency should pass: %v\n%s", err, out.String())
	}
}
