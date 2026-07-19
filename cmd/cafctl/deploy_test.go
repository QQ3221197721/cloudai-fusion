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

// deploy_test.go exercises `cafctl verify-deploy`: a no-drift deploy passes, and a
// drift attestation (validly signed) fails the default integrity gate - the
// auditor/release surface of DL-1.

func TestVerifyDeployCmd(t *testing.T) {
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x3e}, 32))
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

	// No drift -> signature + integrity pass.
	ok, err := delivery.BuildDeployAttestation(signer, delivery.DeployInput{
		Workload: "api", Cluster: "prod", ImageDigest: "sha256:aaa", SignedDigest: "sha256:aaa",
	})
	if err != nil {
		t.Fatalf("build ok: %v", err)
	}
	okPath := write("ok.json", ok)
	cmd := newVerifyDeployCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--attestation", okPath, "--pubkey", keyPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("no-drift deploy must pass the gate, got: %v", err)
	}

	// Drift -> validly signed, but the default integrity gate fails the command.
	drift, err := delivery.BuildDeployAttestation(signer, delivery.DeployInput{
		Workload: "api", Cluster: "prod", ImageDigest: "sha256:bbb", SignedDigest: "sha256:aaa",
	})
	if err != nil {
		t.Fatalf("build drift: %v", err)
	}
	driftPath := write("drift.json", drift)
	cmd2 := newVerifyDeployCmd()
	cmd2.SetOut(&bytes.Buffer{})
	cmd2.SetArgs([]string{"--attestation", driftPath, "--pubkey", keyPath})
	if err := cmd2.Execute(); err == nil {
		t.Fatal("a drifted deployment MUST fail the deploy gate")
	}
}
