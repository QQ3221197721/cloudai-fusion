package disaster

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ============================================================================
// L16 HTTP 处理层单测（httptest，真实可运行）
// ============================================================================

// buildTestHandlers 构造一个带两个区域（一主一备）的处理层。
func buildTestHandlers(t *testing.T) *HTTPHandlers {
	t.Helper()

	mgr := NewManager(ManagerConfig{})
	// 同包测试可直接写已初始化的 regions map
	mgr.regions["us-east-1"] = &DRRegion{
		ID: "us-east-1", Name: "US East", Status: RegionStatusActive, IsPrimary: true,
	}
	mgr.regions["eu-west-1"] = &DRRegion{
		ID: "eu-west-1", Name: "EU West", Status: RegionStatusStandby, IsPrimary: false,
	}

	env, err := NewIsolationEnforcer(EnvDev, DefaultEnvironmentConfigs(), &NullAuditLogger{}, nil)
	if err != nil {
		t.Fatalf("NewIsolationEnforcer: %v", err)
	}

	ev := MustNewFailoverEvidenceVerifier()
	return NewHTTPHandlers(mgr, env, ev)
}

func doRequest(h *HTTPHandlers, method, path string, body []byte) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	h.Register(mux)
	var reqBody *bytes.Reader
	if body == nil {
		reqBody = bytes.NewReader(nil)
	} else {
		reqBody = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reqBody)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestHTTP_Status(t *testing.T) {
	h := buildTestHandlers(t)
	rec := doRequest(h, http.MethodGet, "/api/v1/disaster/status", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	regions := resp["regions"].(map[string]interface{})
	if int(regions["total"].(float64)) != 2 {
		t.Errorf("expected 2 total regions, got %v", regions["total"])
	}
	if int(regions["active"].(float64)) != 1 {
		t.Errorf("expected 1 active region, got %v", regions["active"])
	}
}

func TestHTTP_Regions(t *testing.T) {
	h := buildTestHandlers(t)
	rec := doRequest(h, http.MethodGet, "/api/v1/disaster/regions", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if int(resp["count"].(float64)) != 2 {
		t.Errorf("expected 2 regions, got %v", resp["count"])
	}
}

func TestHTTP_Failover_Success(t *testing.T) {
	h := buildTestHandlers(t)
	body, _ := json.Marshal(failoverRequest{TargetRegionID: "eu-west-1", TriggerReason: "manual-test"})
	rec := doRequest(h, http.MethodPost, "/api/v1/disaster/failover", body)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 completed, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "completed" {
		t.Errorf("expected status=completed, got %v", resp["status"])
	}
	if resp["evidence_id"] == "" || resp["evidence_id"] == nil {
		t.Error("expected non-empty evidence_id")
	}

	// 切换后 eu-west-1 应成为主区
	target, _ := h.mgr.GetRegion("eu-west-1")
	if !target.IsPrimary || target.Status != RegionStatusActive {
		t.Errorf("expected eu-west-1 promoted to primary/active, got primary=%v status=%s", target.IsPrimary, target.Status)
	}
}

func TestHTTP_Failover_UnknownRegionRejected(t *testing.T) {
	h := buildTestHandlers(t)
	body, _ := json.Marshal(failoverRequest{TargetRegionID: "does-not-exist"})
	rec := doRequest(h, http.MethodPost, "/api/v1/disaster/failover", body)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown region, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHTTP_Failover_AlreadyPrimaryRejected(t *testing.T) {
	h := buildTestHandlers(t)
	body, _ := json.Marshal(failoverRequest{TargetRegionID: "us-east-1"})
	rec := doRequest(h, http.MethodPost, "/api/v1/disaster/failover", body)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for already-primary region, got %d", rec.Code)
	}
}

func TestHTTP_Failover_RejectsGET(t *testing.T) {
	h := buildTestHandlers(t)
	rec := doRequest(h, http.MethodGet, "/api/v1/disaster/failover", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for GET on failover, got %d", rec.Code)
	}
}
