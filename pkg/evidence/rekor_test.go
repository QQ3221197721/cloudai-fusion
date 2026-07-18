package evidence

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockRekor stands in for a real Rekor server so we can validate the client's
// request shape and response parsing without external infrastructure. It serves
// GET /api/v1/log (liveness) and POST /api/v1/log/entries (create).
func mockRekor(t *testing.T, capture *hashedrekord) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/log", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"treeSize":1,"rootHash":"deadbeef"}`))
	})
	mux.HandleFunc("/api/v1/log/entries", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if capture != nil {
			_ = json.Unmarshal(body, capture)
		}
		w.WriteHeader(http.StatusCreated)
		// Rekor returns a map keyed by entry UUID.
		_, _ = w.Write([]byte(`{"abc123uuid":{"logIndex":42,"verification":{"inclusionProof":{"logIndex":42,"treeSize":100,"rootHash":"deadbeef"}}}}`))
	})
	return httptest.NewServer(mux)
}

func TestRekorAnchorer_SubmitsHashedRekordAndParsesResponse(t *testing.T) {
	var captured hashedrekord
	srv := mockRekor(t, &captured)
	defer srv.Close()

	anchorer, err := NewRekorAnchorer(srv.URL, nil)
	if err != nil {
		t.Fatalf("new rekor anchorer: %v", err)
	}
	if !anchorer.Real() || anchorer.Backend() != "rekor" {
		t.Fatal("rekor anchorer must report real=true backend=rekor")
	}

	signer, _ := NewSignerFromSeed(make([]byte, 32))
	leaf := "abcdef0123456789"
	sig, _ := signer.Sign([]byte(leaf))

	ref, err := anchorer.Anchor(context.Background(), AnchorRequest{
		LeafHex:      leaf,
		SignatureB64: sig,
		PublicKey:    signer.PublicKey(),
	})
	if err != nil {
		t.Fatalf("anchor: %v", err)
	}

	// The reference must carry the real inclusion data from the server.
	if ref.Backend != "rekor" || ref.LogIndex != 42 || ref.EntryUUID != "abc123uuid" {
		t.Fatalf("unexpected transparency ref: %+v", ref)
	}
	if ref.InclusionProof == "" {
		t.Fatal("inclusion proof should be captured")
	}

	// The submitted entry must be a well-formed hashedrekord carrying our sig+key.
	if captured.Kind != "hashedrekord" || captured.APIVersion != "0.0.1" {
		t.Fatalf("unexpected entry kind/version: %+v", captured)
	}
	if captured.Spec.Signature.Content != sig {
		t.Fatal("submitted signature content must match the receipt signature")
	}
	if captured.Spec.Data.Hash.Algorithm != "sha256" || captured.Spec.Data.Hash.Value == "" {
		t.Fatal("submitted data hash must be a populated sha256")
	}
	pubBytes, derr := base64.StdEncoding.DecodeString(captured.Spec.Signature.PublicKey.Content)
	if derr != nil {
		t.Fatalf("decode pubkey content: %v", derr)
	}
	if !strings.HasPrefix(string(pubBytes), "-----BEGIN PUBLIC KEY-----") {
		t.Fatal("submitted public key must be PEM-encoded")
	}
}

func TestRekorAnchorer_UnreachableErrors(t *testing.T) {
	// A closed server address must fail construction (so callers can fall back).
	srv := mockRekor(t, nil)
	addr := srv.URL
	srv.Close()
	if _, err := NewRekorAnchorer(addr, nil); err == nil {
		t.Fatal("expected an error constructing a Rekor client against a closed server")
	}
}
