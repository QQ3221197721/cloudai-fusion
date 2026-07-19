package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam"
)

// remediation_test.go exercises `cafctl verify-remediation`: a genuine exploit->fix
// differential verifies, while a still-reproducing "fix" fails - the auditor surface
// of RT-1.

func TestVerifyRemediationCmd(t *testing.T) {
	ctx := context.Background()
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x5c}, 32))
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

	w := redteam.ExploitWitness{FindingID: "f-1", Technique: "T1190", Steps: []string{"sqli"}, RangeSpec: "kind:juice"}
	before, err := redteam.ProveExploit(ctx, signer, redteam.SimulatedReplayer{Verdict: true}, w, redteam.PhasePreFix)
	if err != nil {
		t.Fatalf("before: %v", err)
	}
	after, err := redteam.ProveExploit(ctx, signer, redteam.SimulatedReplayer{Verdict: false}, w, redteam.PhasePostFix)
	if err != nil {
		t.Fatalf("after: %v", err)
	}
	stillVuln, err := redteam.ProveExploit(ctx, signer, redteam.SimulatedReplayer{Verdict: true}, w, redteam.PhasePostFix)
	if err != nil {
		t.Fatalf("stillVuln: %v", err)
	}

	bPath := write("before.json", before)
	aPath := write("after.json", after)
	svPath := write("stillvuln.json", stillVuln)

	// Genuine fix -> the CLI proves remediation.
	cmd := newVerifyRemediationCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"--before", bPath, "--after", aPath, "--pubkey", keyPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("a genuine remediation must verify, got: %v", err)
	}

	// Still vulnerable -> the CLI must fail.
	cmd2 := newVerifyRemediationCmd()
	cmd2.SetOut(&bytes.Buffer{})
	cmd2.SetArgs([]string{"--before", bPath, "--after", svPath, "--pubkey", keyPath})
	if err := cmd2.Execute(); err == nil {
		t.Fatal("a still-reproducing exploit MUST fail remediation")
	}
}
