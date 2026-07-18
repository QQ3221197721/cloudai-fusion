package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

func middlewareTestLedger(t *testing.T) *evidence.Ledger {
	t.Helper()
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x55}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	l, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	return l
}

func newEvidenceTestRouter(l *evidence.Ledger) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	grp := r.Group("/api/v1")
	grp.Use(func(c *gin.Context) { c.Set("username", "alice"); c.Next() }) // fake auth
	grp.Use(EvidenceMiddleware(EvidenceMiddlewareConfig{Ledger: l}))
	grp.POST("/workloads", func(c *gin.Context) { c.JSON(http.StatusCreated, gin.H{"ok": true}) })
	grp.GET("/workloads", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	grp.POST("/auth/login", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"token": "x"}) })
	return r
}

func do(r *gin.Engine, method, path string) {
	req := httptest.NewRequest(method, path, nil)
	r.ServeHTTP(httptest.NewRecorder(), req)
}

func TestEvidenceMiddleware_ReceiptsMutatingRequest(t *testing.T) {
	l := middlewareTestLedger(t)
	r := newEvidenceTestRouter(l)

	do(r, http.MethodPost, "/api/v1/workloads")

	all, _ := l.Store().All(context.Background())
	if len(all) != 1 {
		t.Fatalf("expected exactly 1 receipt for the POST, got %d", len(all))
	}
	rec := all[0]
	if rec.Actor != "alice" {
		t.Fatalf("receipt actor should be the authenticated user, got %q", rec.Actor)
	}
	if rec.Action != "api.POST /api/v1/workloads" {
		t.Fatalf("unexpected action %q", rec.Action)
	}
	// Receipt must verify.
	rep, _ := evidence.VerifyChain(all, l.Signer().PublicKey())
	if !rep.Valid {
		t.Fatalf("action receipt must verify, got %+v", rep)
	}
}

func TestEvidenceMiddleware_SkipsReadsAndAuth(t *testing.T) {
	l := middlewareTestLedger(t)
	r := newEvidenceTestRouter(l)

	do(r, http.MethodGet, "/api/v1/workloads")     // read: no receipt
	do(r, http.MethodPost, "/api/v1/auth/login")   // credentials: never receipted

	all, _ := l.Store().All(context.Background())
	if len(all) != 0 {
		t.Fatalf("GET and auth requests must not be receipted, got %d records", len(all))
	}
}
