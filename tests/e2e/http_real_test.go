package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/api"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/auth"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/cloud"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/cluster"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/feature"
)

// newRealRouter wires the REAL managers (no mocks, no DB — in-memory graceful
// degradation) into the production api.NewRouter, and returns the router plus an
// admin JWT minted by the real auth service so protected routes can be driven.
func newRealRouter(t *testing.T) (http.Handler, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	lg := logrus.New()
	lg.SetLevel(logrus.PanicLevel)

	authSvc, err := auth.NewService(auth.Config{
		JWTSecret: "test-secret-0123456789-abcdefghij-xyz",
		JWTExpiry: time.Hour,
	})
	if err != nil {
		t.Fatalf("auth service: %v", err)
	}
	cloudMgr, err := cloud.NewManager(cloud.ManagerConfig{})
	if err != nil {
		t.Fatalf("cloud manager: %v", err)
	}
	clusterMgr, err := cluster.NewManager(cluster.ManagerConfig{CloudManager: cloudMgr})
	if err != nil {
		t.Fatalf("cluster manager: %v", err)
	}
	ff := feature.NewManager(feature.Config{Logger: lg})

	router := api.NewRouter(api.RouterConfig{
		AuthService:    authSvc,
		CloudManager:   cloudMgr,
		ClusterManager: clusterMgr,
		FeatureFlags:   ff,
		Logger:         lg,
	})

	tok, err := authSvc.GenerateToken(&auth.User{
		ID: "u-admin", Username: "admin", Email: "admin@test.local", Role: auth.RoleAdmin,
	})
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return router, tok.AccessToken
}

func doReq(router http.Handler, method, path, body, bearer string) *httptest.ResponseRecorder {
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

type clusterList struct {
	Clusters []map[string]interface{} `json:"clusters"`
	Total    int                      `json:"total"`
}

// TestHTTP_ClusterLifecycle_RealManagers drives the real cluster CRUD endpoints
// through the production router + real cluster manager and asserts the HTTP
// responses are logically consistent end to end: auth is enforced, a created
// cluster appears in the list and by-id lookups, and a delete removes it.
func TestHTTP_ClusterLifecycle_RealManagers(t *testing.T) {
	router, token := newRealRouter(t)

	// 1. Auth enforced: protected route without a token must be 401.
	if w := doReq(router, "GET", "/api/v1/clusters", "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list: got %d, want 401", w.Code)
	}

	// 2. Initially empty (real in-memory manager).
	w := doReq(router, "GET", "/api/v1/clusters", "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("list: got %d: %s", w.Code, w.Body.String())
	}
	var list clusterList
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v (%s)", err, w.Body.String())
	}
	if list.Total != 0 {
		t.Fatalf("expected 0 clusters initially, got %d", list.Total)
	}

	// 3. Create a cluster via the real manager.
	w = doReq(router, "POST", "/api/v1/clusters",
		`{"name":"prod-eks","provider":"aws","region":"us-east-1","kubeconfig":"dummy-kubeconfig"}`, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: got %d: %s", w.Code, w.Body.String())
	}
	var created map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created: %v (%s)", err, w.Body.String())
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("created cluster missing id: %s", w.Body.String())
	}
	if created["name"] != "prod-eks" {
		t.Errorf("created name=%v, want prod-eks", created["name"])
	}

	// 4. Consistency: the list now contains exactly the created cluster.
	w = doReq(router, "GET", "/api/v1/clusters", "", token)
	list = clusterList{}
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if list.Total != 1 || len(list.Clusters) != 1 {
		t.Fatalf("after create expected 1 cluster, got total=%d len=%d", list.Total, len(list.Clusters))
	}
	if list.Clusters[0]["id"] != id {
		t.Errorf("listed id=%v, want %s (create->list must be consistent)", list.Clusters[0]["id"], id)
	}

	// 5. GET by id returns the same cluster.
	w = doReq(router, "GET", "/api/v1/clusters/"+id, "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("get by id: got %d: %s", w.Code, w.Body.String())
	}
	var got map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got["id"] != id || got["name"] != "prod-eks" {
		t.Errorf("get by id returned id=%v name=%v, want id=%s name=prod-eks", got["id"], got["name"], id)
	}

	// 6. Delete, then the list is empty again (real state mutation, not a mock).
	if w := doReq(router, "DELETE", "/api/v1/clusters/"+id, "", token); w.Code != http.StatusOK {
		t.Fatalf("delete: got %d: %s", w.Code, w.Body.String())
	}
	w = doReq(router, "GET", "/api/v1/clusters", "", token)
	list = clusterList{}
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if list.Total != 0 {
		t.Errorf("after delete expected 0 clusters, got %d", list.Total)
	}
}

// TestHTTP_FeatureFlags_RealManager verifies the public feature-flag endpoint
// serves the real feature manager's registered defaults through HTTP.
func TestHTTP_FeatureFlags_RealManager(t *testing.T) {
	router, _ := newRealRouter(t)

	w := doReq(router, "GET", "/api/v1/features", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list features: got %d: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "topology_aware_scheduling") {
		t.Errorf("features response missing a known default flag: %s", body)
	}

	// Health endpoint is always available.
	if w := doReq(router, "GET", "/healthz", "", ""); w.Code != http.StatusOK {
		t.Errorf("healthz: got %d", w.Code)
	}
}
