package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence/zk"
)

// zk_test.go exercises `cafctl verify-zk`: a real Groth16 zkEvidence attestation
// verifies offline against its pinned verifying key, and a tampered public input
// fails — the confidential (Moat A / Layer A1) auditor surface.
func TestVerifyZKCmd(t *testing.T) {
	ws := make([]zk.LeafWitness, 3)
	nsFE := zk.FieldFromBytes([]byte("redteam/engagement/CLI"))
	for i := range ws {
		ws[i] = zk.LeafWitness{Namespace: nsFE, Eidx: uint64(i), InScope: true, PayloadHash: zk.FieldFromBytes([]byte{byte(i)})}
	}
	att, vk, err := zk.Groth16Prover{}.Prove(context.Background(), zk.StmtScopeCompliance, "in scope", ws)
	if err != nil {
		t.Fatalf("prove: %v", err)
	}

	dir := t.TempDir()
	vkPath := filepath.Join(dir, "vk.bin")
	if err := os.WriteFile(vkPath, vk, 0o600); err != nil {
		t.Fatalf("write vk: %v", err)
	}
	write := func(name string, v any) string {
		b, _ := json.Marshal(v)
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return p
	}

	// Valid attestation -> the CLI verifies.
	aPath := write("att.json", att)
	cmd := newVerifyZKCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--attestation", aPath, "--vk", vkPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("a valid zk attestation must verify, got: %v", err)
	}

	// Tamper the public root -> the CLI must fail.
	att.PublicRoot = att.ScopeCommit
	tPath := write("tampered.json", att)
	cmd2 := newVerifyZKCmd()
	cmd2.SetOut(&bytes.Buffer{})
	cmd2.SetArgs([]string{"--attestation", tPath, "--vk", vkPath})
	if err := cmd2.Execute(); err == nil {
		t.Fatal("a tampered zk attestation MUST fail verification")
	}
}
