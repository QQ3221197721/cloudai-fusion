package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ---------------------------------------------------------------------------
// handlePluginManifests
// ---------------------------------------------------------------------------

// TestHandlePluginManifests verifies GET /manifests returns the catalog.
func TestHandlePluginManifests(t *testing.T) {
	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	r.GET("/manifests", handlePluginManifests())

	c.Request = httptest.NewRequest("GET", "/manifests", nil)
	r.ServeHTTP(w, c.Request)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	manifests, ok := body["manifests"].([]interface{})
	if !ok {
		t.Fatalf("manifests missing or not array: %v", body)
	}
	if len(manifests) == 0 {
		t.Fatal("expected at least one manifest")
	}
	total, ok := body["total"].(float64)
	if !ok || int(total) != len(manifests) {
		t.Fatalf("total = %v, want %d", body["total"], len(manifests))
	}
}

// ---------------------------------------------------------------------------
// handlePluginList — with a real (empty) Manager
// ---------------------------------------------------------------------------

// TestHandlePluginList_EmptyManager verifies the list endpoint with no plugins.
func TestHandlePluginList_EmptyManager(t *testing.T) {
	reg := plugin.NewRegistry()
	mgr := plugin.NewManager(reg, plugin.ManagerConfig{})

	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	r.GET("/plugins", handlePluginList(mgr))

	c.Request = httptest.NewRequest("GET", "/plugins", nil)
	r.ServeHTTP(w, c.Request)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if body["total"].(float64) != 0 {
		t.Fatalf("total = %v, want 0", body["total"])
	}
	plugins, ok := body["plugins"].([]interface{})
	if !ok {
		t.Fatal("plugins field missing or not array")
	}
	if len(plugins) != 0 {
		t.Fatalf("plugins = %v, want empty", plugins)
	}
}

// ---------------------------------------------------------------------------
// handlePluginHealth — not found
// ---------------------------------------------------------------------------

// TestHandlePluginHealth_NotFound verifies 404 for a non-existent plugin.
func TestHandlePluginHealth_NotFound(t *testing.T) {
	reg := plugin.NewRegistry()
	mgr := plugin.NewManager(reg, plugin.ManagerConfig{})

	w := httptest.NewRecorder()
	c, r := gin.CreateTestContext(w)
	r.GET("/plugins/:name/health", handlePluginHealth(mgr))

	c.Request = httptest.NewRequest("GET", "/plugins/nonexistent/health", nil)
	// gin needs the router to parse params
	r.ServeHTTP(w, c.Request)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	if body["error"] != "plugin not found" {
		t.Fatalf("error = %v, want 'plugin not found'", body["error"])
	}
}
