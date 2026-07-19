package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// completeness_test.go covers the HTTP surface of Moat A / M0: GET
// /api/v1/evidence/completeness?namespace=<ns> returns a namespace completeness
// proof an auditor can verify offline. It closes the loop - a live server hands
// out the exact "no more, no less" proof that `cafctl verify-completeness` checks.

func newCompletenessAPILedger(t *testing.T) *evidence.Ledger {
	t.Helper()
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x71}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	l, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	return l
}

func TestHandleEvidenceCompleteness(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	l := newCompletenessAPILedger(t)

	ns := "redteam/engagement/http-1"
	var members []*evidence.Evidence
	for i := 0; i < 3; i++ {
		ev, err := l.Record(ctx, evidence.RecordInput{
			Actor: "t", Action: "redteam.recon", Subject: ns, Payload: map[string]any{"i": i},
		})
		if err != nil {
			t.Fatalf("record: %v", err)
		}
		members = append(members, ev)
	}
	if _, err := l.SealNamespace(ctx, ns, "redteam", members); err != nil {
		t.Fatalf("seal: %v", err)
	}

	r := gin.New()
	r.GET("/completeness", handleEvidenceCompleteness(l))

	do := func(query string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/completeness"+query, nil))
		return w
	}

	// Valid namespace -> 200 + an offline-verifiable proof.
	w := do("?namespace=" + url.QueryEscape(ns))
	if w.Code != http.StatusOK {
		t.Fatalf("valid namespace: got %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var proof evidence.CompletenessProof
	if err := json.Unmarshal(w.Body.Bytes(), &proof); err != nil {
		t.Fatalf("decode proof: %v", err)
	}
	if err := evidence.VerifyCompleteness(&proof, l.Signer().PublicKey()); err != nil {
		t.Fatalf("served proof must verify offline, got: %v", err)
	}
	if len(proof.Members) != 3 {
		t.Fatalf("expected 3 members, got %d", len(proof.Members))
	}

	// Missing namespace -> 400.
	if w := do(""); w.Code != http.StatusBadRequest {
		t.Fatalf("missing namespace: got %d, want 400", w.Code)
	}

	// Unknown namespace -> 404.
	if w := do("?namespace=" + url.QueryEscape("finops/month/1970-01")); w.Code != http.StatusNotFound {
		t.Fatalf("unknown namespace: got %d, want 404 (body=%s)", w.Code, w.Body.String())
	}
}
