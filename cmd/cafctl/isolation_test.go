package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
)

// isolation_test.go exercises `cafctl verify-isolation`: an isolated placement
// passes, while a non-isolated co-placement (validly signed) fails the default
// isolation gate - the auditor/SLA surface of CN-3.

func TestVerifyIsolationCmd(t *testing.T) {
	ctx := context.Background()
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x0e}, 32))
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

	// Isolated -> passes the gate.
	ok, err := scheduler.BuildIsolationAttestation(ctx, signer, scheduler.SimulatedIsolationProbe{Verdict: true},
		"gpu-node-1", 0, scheduler.GPUShareMIG, []string{"team-a", "team-b"})
	if err != nil {
		t.Fatalf("build ok: %v", err)
	}
	okPath := write("ok.json", ok)
	cmd := newVerifyIsolationCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--attestation", okPath, "--pubkey", keyPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("isolated placement must pass the gate, got: %v", err)
	}

	// Not isolated -> signed, but the default gate fails.
	bad, err := scheduler.BuildIsolationAttestation(ctx, signer, scheduler.SimulatedIsolationProbe{Verdict: false},
		"gpu-node-1", 1, scheduler.GPUShareMPS, []string{"team-a", "team-b"})
	if err != nil {
		t.Fatalf("build bad: %v", err)
	}
	badPath := write("bad.json", bad)
	cmd2 := newVerifyIsolationCmd()
	cmd2.SetOut(&bytes.Buffer{})
	cmd2.SetArgs([]string{"--attestation", badPath, "--pubkey", keyPath})
	if err := cmd2.Execute(); err == nil {
		t.Fatal("a non-isolated co-placement MUST fail the isolation gate")
	}
}
