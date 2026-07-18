package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

func TestHandleEvidenceRotateKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	signer, _ := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x71}, 32))
	l, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	oldKeyID := l.Signer().KeyID()

	r := gin.New()
	r.POST("/rotate", handleEvidenceRotateKey(l))

	req := httptest.NewRequest(http.MethodPost, "/rotate", bytes.NewReader([]byte(`{"reason":"test rotation"}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("rotate: status %d body %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["rotated"] != true {
		t.Fatalf("expected rotated=true, got %v", resp["rotated"])
	}
	newKeyID, _ := resp["new_key_id"].(string)
	if newKeyID == "" || newKeyID == oldKeyID {
		t.Fatalf("new key id must differ from old (%q vs %q)", newKeyID, oldKeyID)
	}
	if resp["ephemeral"] != true {
		t.Fatal("empty-key request should produce an ephemeral key")
	}
	// The ledger's active signer must now be the rotated key.
	if l.Signer().KeyID() != newKeyID {
		t.Fatal("ledger signer must be the rotated key")
	}

	// A key.rotate receipt must exist and the post-rotation bundle must verify.
	all, _ := l.Store().All(context.Background())
	var sawRotate bool
	for _, e := range all {
		if e.Action == "key.rotate" {
			sawRotate = true
		}
	}
	if !sawRotate {
		t.Fatal("expected a key.rotate receipt in the ledger")
	}
	bundle, _ := l.Export(context.Background())
	rep, _ := evidence.VerifyBundle(bundle)
	if !rep.Valid {
		t.Fatalf("post-rotation bundle must verify (rotation-aware), got %+v", rep)
	}
}
