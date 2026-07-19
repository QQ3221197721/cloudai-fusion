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

// edge_test.go exercises `cafctl verify-edge`: an all-in-policy reconciliation
// passes, while one with an out-of-policy offline decision (validly signed) fails
// the default compliance gate.

func TestVerifyEdgeCmd(t *testing.T) {
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x1a}, 32))
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
	chain := func(node string, policyOK ...bool) *delivery.EdgeChain {
		c := delivery.NewEdgeChain(node)
		for _, ok := range policyOK {
			if _, err := c.Append("edge.action", ok); err != nil {
				t.Fatalf("append: %v", err)
			}
		}
		return c
	}

	// All in-policy -> passes the compliance gate.
	ok, err := delivery.BuildReconciliation(signer, chain("edge-1", true, true, true))
	if err != nil {
		t.Fatalf("build ok: %v", err)
	}
	okPath := write("ok.json", ok)
	cmd := newVerifyEdgeCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--attestation", okPath, "--pubkey", keyPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("all-in-policy edge reconciliation must pass, got: %v", err)
	}

	// One out-of-policy decision -> signed, but the default gate fails.
	nc, err := delivery.BuildReconciliation(signer, chain("edge-2", true, false))
	if err != nil {
		t.Fatalf("build non-compliant: %v", err)
	}
	ncPath := write("nc.json", nc)
	cmd2 := newVerifyEdgeCmd()
	cmd2.SetOut(&bytes.Buffer{})
	cmd2.SetArgs([]string{"--attestation", ncPath, "--pubkey", keyPath})
	if err := cmd2.Execute(); err == nil {
		t.Fatal("an out-of-policy offline decision MUST fail the edge gate")
	}
}
