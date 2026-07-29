package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/hunt"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/runmode"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/wellreadiness"
)

func TestWellsAPI_ReportsHonestSnapshot(t *testing.T) {
	t.Cleanup(wellreadiness.Reset)
	wellreadiness.Reset()
	wellreadiness.SetPolicy(runmode.Simulation)

	// One fabric-connected well, one not — the endpoint must reflect the truth.
	_ = wellreadiness.Report(wellreadiness.Status{
		Well: 1, Name: "L1-intel", Claimed: wellreadiness.M3FabricConnected,
		Wired: true, BackendMode: wellreadiness.BackendReal, FabricConnected: true, EvidenceBacked: true,
	})
	_ = wellreadiness.Report(wellreadiness.Status{
		Well: 13, Name: "L13-evidence", Claimed: wellreadiness.M2RealBackend,
		Wired: true, BackendMode: wellreadiness.BackendReal, FabricConnected: false, EvidenceBacked: true,
	})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/wells", handleWells)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/wells", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("wells: want 200, got %d", w.Code)
	}
	var resp struct {
		Total              int  `json:"total"`
		AllFabricConnected bool `json:"all_fabric_connected"`
		Wells              []struct {
			Well            int  `json:"well"`
			FabricConnected bool `json:"fabric_connected"`
		} `json:"wells"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 2 {
		t.Fatalf("expected 2 wells, got %d", resp.Total)
	}
	// Because L13 is not fabric-connected, the aggregate MUST be false (honest).
	if resp.AllFabricConnected {
		t.Fatalf("all_fabric_connected must be false when a well is not connected")
	}
}

func TestHuntAPI_Run(t *testing.T) {
	store := intel.NewMemoryStore()
	if err := store.UpsertCVE(intel.CVEEntry{CVEID: "CVE-2024-1", CVSSv3Score: 9.8, MitreTags: []string{"T1190"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	eng := hunt.NewEngine(store, nil, nil)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/v1/hunt", handleHuntRun(eng))

	w := httptest.NewRecorder()
	body := `{"name":"t","min_cvss":7.0}`
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/hunt", bytes.NewBufferString(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("hunt: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Total int `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Total != 1 {
		t.Fatalf("expected 1 hunt finding, got %d", resp.Total)
	}
}

func TestIntelSyncAPI_NoSources(t *testing.T) {
	hub := intel.NewHub(nil, intel.NewMemoryStore(), nil)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/v1/intel/sync", handleIntelSync(hub))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/intel/sync", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("intel sync: want 200, got %d (%s)", w.Code, w.Body.String())
	}
}
