package main

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam"
)

// TestMoatDemo_EndToEndVerifiableEngagement is the CI-verified proof of the L13
// moat: a REAL red-team engagement's full signed chain verifies OFFLINE through
// cafctl's actual verification core, and any tamper is caught.
func TestMoatDemo_EndToEndVerifiableEngagement(t *testing.T) {
	t.Cleanup(capability.Reset)
	art, err := redteam.RunVerifiableEngagementDemo(context.Background())
	if err != nil {
		t.Fatalf("run demo: %v", err)
	}
	if art.ReceiptCount == 0 {
		t.Fatalf("engagement produced no signed receipts")
	}
	if art.FindingCount == 0 {
		t.Fatalf("engagement produced no findings")
	}

	// The genuine chain must verify against the pinned key via cafctl's core.
	ok, err := runVerify(art.BundleJSON, art.PublicKeyPEM, false, io.Discard)
	if err != nil {
		t.Fatalf("runVerify: %v", err)
	}
	if !ok {
		t.Fatalf("a genuine engagement chain must verify VALID")
	}

	// Tampering with a receipt must break verification.
	tampered, err := tamperBundle(art.BundleJSON)
	if err != nil {
		t.Fatalf("tamper: %v", err)
	}
	bad, err := runVerify(tampered, art.PublicKeyPEM, false, io.Discard)
	if err != nil {
		t.Fatalf("runVerify(tampered): %v", err)
	}
	if bad {
		t.Fatalf("a tampered chain MUST fail verification")
	}
}

// TestMoatDemoCmd_Runs exercises the cafctl moat-demo subcommand end to end,
// asserting it reports a valid, tamper-proof chain.
func TestMoatDemoCmd_Runs(t *testing.T) {
	t.Cleanup(capability.Reset)
	cmd := newMoatDemoCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--out", t.TempDir()})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("moat-demo: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"signed receipts", "Tamper check", "Artifacts written"} {
		if !bytes.Contains([]byte(out), []byte(want)) {
			t.Fatalf("moat-demo output missing %q; got:\n%s", want, out)
		}
	}
}
