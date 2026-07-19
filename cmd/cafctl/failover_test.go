package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/delivery"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// failover_test.go exercises `cafctl verify-failover`: a healthy failover passes,
// while a split-brain attestation (validly signed) fails the default SLO/BCP gate.

func TestVerifyFailoverCmd(t *testing.T) {
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x2f}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
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

	// Healthy failover -> passes the SLO gate.
	ok, err := delivery.BuildFailoverAttestation(signer, delivery.FailoverInput{
		Service: "db", FromCluster: "eu-1", ToCluster: "eu-2", Promotions: 1,
		RTOSeconds: 15, RPOSeconds: 1, RTOTargetSec: 60, RPOTargetSec: 5,
	})
	if err != nil {
		t.Fatalf("build ok: %v", err)
	}
	okPath := write("ok.json", ok)
	cmd := newVerifyFailoverCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--attestation", okPath, "--pubkey", keyPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("healthy failover must pass the gate, got: %v", err)
	}

	// Split-brain -> signed, but the default SLO gate fails the command.
	sb, err := delivery.BuildFailoverAttestation(signer, delivery.FailoverInput{
		Service: "db", Promotions: 2, RTOSeconds: 10, RTOTargetSec: 60,
	})
	if err != nil {
		t.Fatalf("build split-brain: %v", err)
	}
	sbPath := write("sb.json", sb)
	cmd2 := newVerifyFailoverCmd()
	cmd2.SetOut(&bytes.Buffer{})
	cmd2.SetArgs([]string{"--attestation", sbPath, "--pubkey", keyPath})
	if err := cmd2.Execute(); err == nil {
		t.Fatal("a split-brain failover MUST fail the SLO gate")
	}
}
