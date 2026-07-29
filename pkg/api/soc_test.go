package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/soc"
)

// socTestRouter wires the SOC handlers WITHOUT the auth stack so behavior and
// wiring can be exercised directly (RBAC is covered by router middleware).
func socTestRouter(eng *soc.Engine) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/v1/soc")
	g.GET("/findings", handleSOCFindings(eng))
	g.GET("/playbooks", handleSOCPlaybooks(eng))
	g.POST("/analyze/endpoint", handleSOCAnalyzeEndpoint(eng))
	g.POST("/analyze/network", handleSOCAnalyzeNetwork(eng))
	g.POST("/analyze/workload", handleSOCAnalyzeWorkload(eng))
	g.POST("/analyze/identity", handleSOCAnalyzeIdentity(eng))
	g.POST("/analyze/image", handleSOCAnalyzeImage(eng))
	g.POST("/findings/:id/respond", handleSOCRespond(eng))
	return r
}

func socEngineWithIntel(t *testing.T) *soc.Engine {
	t.Helper()
	store := intel.NewMemoryStore()
	if err := store.UpsertIOCs([]intel.IOCEntry{
		{IOCType: "ip", Value: "203.0.113.9", Severity: intel.SeverityHigh},
	}); err != nil {
		t.Fatalf("seed iocs: %v", err)
	}
	return soc.NewEngine(store, nil)
}

func doJSON(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, bytes.NewBufferString(body)))
	return w
}

func TestSOCAPI_NetworkAnalysisThenRespond(t *testing.T) {
	eng := socEngineWithIntel(t)
	r := socTestRouter(eng)

	// L4: submit a connection to a known-malicious IP → one finding.
	w := doJSON(t, r, http.MethodPost, "/api/v1/soc/analyze/network",
		`{"host":"node-1","ips":["203.0.113.9"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("analyze/network: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var res struct {
		Findings []soc.Finding `json:"findings"`
		Total    int           `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if res.Total != 1 || res.Findings[0].Technique != "T1071" {
		t.Fatalf("expected one T1071 finding, got %+v", res.Findings)
	}
	findingID := res.Findings[0].ID

	// L8: orchestrate a response for the finding.
	w = doJSON(t, r, http.MethodPost, "/api/v1/soc/findings/"+findingID+"/respond", ``)
	if w.Code != http.StatusOK {
		t.Fatalf("respond: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var resp soc.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Playbook != "c2-egress" {
		t.Fatalf("expected c2-egress playbook, got %q", resp.Playbook)
	}

	// Respond to a missing finding → 404.
	w = doJSON(t, r, http.MethodPost, "/api/v1/soc/findings/nope/respond", ``)
	if w.Code != http.StatusNotFound {
		t.Fatalf("respond unknown: want 404, got %d", w.Code)
	}
}

func TestSOCAPI_FindingsAndPlaybooks(t *testing.T) {
	eng := socEngineWithIntel(t)
	r := socTestRouter(eng)

	// Playbooks are always available.
	w := doJSON(t, r, http.MethodGet, "/api/v1/soc/playbooks", "")
	if w.Code != http.StatusOK {
		t.Fatalf("playbooks: want 200, got %d", w.Code)
	}
	var pb struct {
		Total int `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &pb)
	if pb.Total == 0 {
		t.Fatalf("expected built-in playbooks, got 0")
	}

	// Workload posture check produces findings that then appear in /findings.
	w = doJSON(t, r, http.MethodPost, "/api/v1/soc/analyze/workload",
		`{"name":"api","namespace":"prod","privileged":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("analyze/workload: want 200, got %d (%s)", w.Code, w.Body.String())
	}

	w = doJSON(t, r, http.MethodGet, "/api/v1/soc/findings?limit=10", "")
	if w.Code != http.StatusOK {
		t.Fatalf("findings: want 200, got %d", w.Code)
	}
	var fr struct {
		Total int `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &fr)
	if fr.Total == 0 {
		t.Fatalf("expected at least one stored finding")
	}
}

func TestSOCAPI_BadRequest(t *testing.T) {
	eng := socEngineWithIntel(t)
	r := socTestRouter(eng)
	w := doJSON(t, r, http.MethodPost, "/api/v1/soc/analyze/endpoint", `{bad json`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("malformed body: want 400, got %d (%s)", w.Code, w.Body.String())
	}
}
