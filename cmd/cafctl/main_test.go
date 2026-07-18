package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// buildBundle produces a signed 3-record bundle plus the signer, for CLI tests.
func buildBundle(t *testing.T) ([]byte, *evidence.Ed25519Signer) {
	t.Helper()
	seed := bytes.Repeat([]byte{0x7}, 32)
	signer, err := evidence.NewSignerFromSeed(seed)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	l, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := l.Record(context.Background(), evidence.RecordInput{
			Actor: "cli-test", Action: "schedule.bind", Subject: "wl", Payload: map[string]any{"i": i},
		}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	bundle, err := l.Export(context.Background())
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	b, _ := json.Marshal(bundle)
	return b, signer
}

func TestRunVerify_ValidEmbeddedKey(t *testing.T) {
	b, _ := buildBundle(t)
	var out bytes.Buffer
	ok, err := runVerify(b, nil, false, &out)
	if err != nil {
		t.Fatalf("runVerify: %v", err)
	}
	if !ok {
		t.Fatalf("expected valid, got report:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "VALID") {
		t.Fatalf("expected VALID in output, got:\n%s", out.String())
	}
}

func TestRunVerify_ValidPinnedKey(t *testing.T) {
	b, signer := buildBundle(t)
	pem, _ := signer.PublicKeyPEM()
	var out bytes.Buffer
	ok, err := runVerify(b, pem, false, &out)
	if err != nil {
		t.Fatalf("runVerify: %v", err)
	}
	if !ok {
		t.Fatalf("expected valid with pinned key, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "pinned") {
		t.Fatalf("expected 'pinned' note, got:\n%s", out.String())
	}
}

func TestRunVerify_TamperedBundleFails(t *testing.T) {
	b, _ := buildBundle(t)
	// Flip a payload value inside the serialized bundle without re-signing.
	tampered := bytes.Replace(b, []byte(`{"i":1}`), []byte(`{"i":9}`), 1)
	if bytes.Equal(tampered, b) {
		t.Fatal("test setup: expected to mutate the bundle bytes")
	}
	var out bytes.Buffer
	ok, err := runVerify(tampered, nil, false, &out)
	if err != nil {
		t.Fatalf("runVerify: %v", err)
	}
	if ok {
		t.Fatalf("expected INVALID after tamper, got:\n%s", out.String())
	}
}

func TestRunVerify_WrongPinnedKeyFails(t *testing.T) {
	b, _ := buildBundle(t)
	other, _ := evidence.GenerateEphemeralSigner()
	pem, _ := other.PublicKeyPEM()
	var out bytes.Buffer
	ok, _ := runVerify(b, pem, false, &out)
	if ok {
		t.Fatalf("expected INVALID against wrong pinned key, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "WARNING") {
		t.Fatalf("expected key-mismatch WARNING, got:\n%s", out.String())
	}
}
