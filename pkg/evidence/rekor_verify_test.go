package evidence

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockRekorWithProof returns a Rekor stub whose create-entry response contains a
// genuine 2-leaf RFC 6962 inclusion proof for a known entry body, so the client's
// offline verification can be exercised end-to-end.
func mockRekorWithProof(t *testing.T) *httptest.Server {
	t.Helper()
	bodyRaw := []byte("rekor-entry-body")
	leaf0 := mLeafHash(bodyRaw)
	leaf1 := mLeafHash([]byte("sibling-leaf"))
	root := mNodeHash(leaf0, leaf1)
	resp := map[string]any{
		"uuid-abc": map[string]any{
			"logIndex": 123,
			"body":     base64.StdEncoding.EncodeToString(bodyRaw),
			"verification": map[string]any{
				"inclusionProof": map[string]any{
					"logIndex":   0,
					"treeSize":   2,
					"rootHash":   hex.EncodeToString(root),
					"hashes":     []string{hex.EncodeToString(leaf1)},
					"checkpoint": "rekor-signed-note",
				},
			},
		},
	}
	respBytes, _ := json.Marshal(resp)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/log", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"treeSize":2}`))
	})
	mux.HandleFunc("/api/v1/log/entries", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(respBytes)
	})
	return httptest.NewServer(mux)
}

func TestVerifyRekorInclusion_OfflinePathRecompute(t *testing.T) {
	srv := mockRekorWithProof(t)
	defer srv.Close()
	anchorer, err := NewRekorAnchorer(srv.URL, nil)
	if err != nil {
		t.Fatalf("anchorer: %v", err)
	}

	signer, _ := NewSignerFromSeed(make([]byte, 32))
	sig, _ := signer.Sign([]byte("leafhex"))
	ref, err := anchorer.Anchor(context.Background(), AnchorRequest{
		LeafHex: "leafhex", SignatureB64: sig, PublicKey: signer.PublicKey(),
	})
	if err != nil {
		t.Fatalf("anchor: %v", err)
	}
	if ref.Proof == nil {
		t.Fatal("expected a structured rekor inclusion proof")
	}

	if err := VerifyRekorInclusion(ref); err != nil {
		t.Fatalf("rekor inclusion must verify offline: %v", err)
	}

	// Tampering the stated root must fail the Merkle recomputation.
	ref.Proof.RootHash = flipHex(t, ref.Proof.RootHash)
	if VerifyRekorInclusion(ref) == nil {
		t.Fatal("tampered rekor root must fail verification")
	}
}

func TestChainCountsRekorVerified(t *testing.T) {
	srv := mockRekorWithProof(t)
	defer srv.Close()
	anchorer, err := NewRekorAnchorer(srv.URL, nil)
	if err != nil {
		t.Fatalf("anchorer: %v", err)
	}
	signer, _ := NewSignerFromSeed(bytes.Repeat([]byte{9}, 32))
	l, err := NewLedger(LedgerConfig{Store: NewMemoryStore(), Signer: signer, Anchorer: anchorer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	if _, err := l.Record(context.Background(), RecordInput{Actor: "a", Action: "x", Subject: "s"}); err != nil {
		t.Fatalf("record: %v", err)
	}
	all := mustAll(t, l)
	rep, err := VerifyChain(all, signer.PublicKey())
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if rep.RekorVerified != 1 {
		t.Fatalf("expected 1 rekor-verified record, got %d (anchored=%d)", rep.RekorVerified, rep.AnchoredReal)
	}
}
