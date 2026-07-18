package evidence

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestCanonicalJSON_NoHTMLEscaping proves the hashed canonical form uses standard
// JSON (no Go-specific \u003c HTML escaping), so a verifier in any language that
// emits standard JSON reproduces the exact bytes — and thus the exact hash.
func TestCanonicalJSON_NoHTMLEscaping(t *testing.T) {
	l := newTestLedger(t, NewMemoryStore())
	e, err := l.Record(context.Background(), RecordInput{
		Actor:   "a<b>&c",
		Action:  "op<x>&y",
		Subject: "s&<>",
		Payload: map[string]any{"k": "v<&>"},
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	content, err := e.content()
	if err != nil {
		t.Fatalf("content: %v", err)
	}
	if bytes.Contains(content, []byte(`\u003c`)) ||
		bytes.Contains(content, []byte(`\u003e`)) ||
		bytes.Contains(content, []byte(`\u0026`)) {
		t.Fatalf("canonical content must not HTML-escape <, >, &; got: %s", content)
	}
	if !strings.Contains(string(content), "op<x>&y") {
		t.Fatalf("canonical content must contain the literal action; got: %s", content)
	}

	// Trailing framing must be trimmed: the canonical form is exactly the value.
	if bytes.HasSuffix(content, []byte("\n")) {
		t.Fatal("canonical content must not carry a trailing newline")
	}

	// The record must still verify after the durable JSON round-trip.
	rep, err := VerifyChain([]*Evidence{e}, l.Signer().PublicKey())
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !rep.Valid {
		t.Fatalf("record with HTML-special chars must verify, got %+v", rep)
	}
}
