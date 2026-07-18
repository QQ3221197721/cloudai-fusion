package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam"
)

// redteamTestRouter wires the red-team handlers WITHOUT the auth stack, so the
// endpoints can be exercised directly. Auth/RBAC is covered by the router-level
// middleware in production; here we validate handler behavior + wiring.
func redteamTestRouter(mgr *redteam.Manager, l *evidence.Ledger) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/v1/redteam")
	g.POST("/engagements", handleRedTeamCreate(mgr))
	g.GET("/engagements", handleRedTeamList(mgr))
	g.GET("/engagements/:id", handleRedTeamGet(mgr))
	g.POST("/engagements/:id/abort", handleRedTeamAbort(mgr))
	g.POST("/engagements/:id/approve", handleRedTeamApprove(mgr))
	g.GET("/engagements/:id/report", handleRedTeamReport(mgr, l))
	g.GET("/engagements/:id/evidence", handleRedTeamEvidence(l))
	return r
}

func TestRedTeamAPI_LifecycleAndVerifiableReport(t *testing.T) {
	t.Cleanup(capability.Reset)
	signer, _ := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x9a}, 32))
	ledger, _ := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	mgr := redteam.NewManager(ledger, nil)
	r := redteamTestRouter(mgr, ledger)

	// Create an engagement.
	body := `{"scope":{"targets":[{"kind":"host","value":"lab.local"}],"allow_techniques":["T1595"],"max_risk_tier":0,"approval_required_at":1}}`
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/redteam/engagements", bytes.NewBufferString(body)))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: want 201, got %d (%s)", w.Code, w.Body.String())
	}
	var created struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || created.ID == "" {
		t.Fatalf("create response: %v (%s)", err, w.Body.String())
	}
	id := created.ID

	// Get it back.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/redteam/engagements/"+id, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("get: want 200, got %d", w.Code)
	}

	// Verifiable report must embed a signed checkpoint.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/redteam/engagements/"+id+"/report", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("report: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var rep struct {
		EngagementID string `json:"engagement_id"`
		Checkpoint   *struct {
			RootHash string `json:"root_hash"`
		} `json:"checkpoint"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatalf("report decode: %v", err)
	}
	if rep.EngagementID != id || rep.Checkpoint == nil || rep.Checkpoint.RootHash == "" {
		t.Fatalf("report must carry a signed checkpoint, got %s", w.Body.String())
	}

	// Evidence listing for the engagement must be non-empty (grant receipt at least).
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/redteam/engagements/"+id+"/evidence", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("evidence: want 200, got %d", w.Code)
	}
	var ev struct {
		Total int `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &ev)
	if ev.Total < 1 {
		t.Fatalf("engagement must have at least the scope-grant receipt, got %d", ev.Total)
	}

	// Kill-switch.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/redteam/engagements/"+id+"/abort", bytes.NewBufferString(`{"reason":"test"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("abort: want 200, got %d", w.Code)
	}
	if e, _ := mgr.Get(id); e == nil || e.Status != redteam.StatusAborted {
		t.Fatal("engagement must be aborted after the abort endpoint")
	}
}

func TestRedTeamAPI_CreateRejectsEmptyScope(t *testing.T) {
	t.Cleanup(capability.Reset)
	signer, _ := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x9b}, 32))
	ledger, _ := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	mgr := redteam.NewManager(ledger, nil)
	r := redteamTestRouter(mgr, ledger)

	// A scope with no targets must be rejected (deny-by-default).
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/redteam/engagements", bytes.NewBufferString(`{"scope":{}}`)))
	if w.Code == http.StatusCreated {
		t.Fatalf("empty-scope engagement must be rejected, got %d", w.Code)
	}
}
