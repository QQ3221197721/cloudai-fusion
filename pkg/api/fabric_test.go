package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/fabric"
)

// fabric_test.go covers the VKG HTTP surface: GET /api/v1/evidence/lineage?key=<k>
// returns every signed receipt correlated to the key, across pillars.

func TestHandleEvidenceLineage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := context.Background()
	l := newCompletenessAPILedger(t) // helper from completeness_test.go (same package)

	emit := func(intent, subject string, corr ...string) {
		in, err := fabric.PCA{
			Intent: intent, Pillar: "test", Subject: subject, Correlations: corr,
			Actor: "t", Payload: map[string]any{"note": intent},
		}.RecordInput()
		if err != nil {
			t.Fatalf("pca %s: %v", intent, err)
		}
		if _, err := l.Record(ctx, in); err != nil {
			t.Fatalf("record %s: %v", intent, err)
		}
	}
	// Two pillars correlated to one incident; one unrelated.
	emit("redteam.finding", "asset-x", "incident-1")
	emit("schedule.bind", "wl-1", "incident-1")
	emit("delivery.deploy", "api", "other")

	r := gin.New()
	r.GET("/lineage", handleEvidenceLineage(l))
	do := func(q string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/lineage"+q, nil))
		return w
	}

	// Cross-pillar lineage for incident-1 -> 2 receipts.
	w := do("?key=" + url.QueryEscape("incident-1"))
	if w.Code != http.StatusOK {
		t.Fatalf("lineage: got %d, want 200 (body=%s)", w.Code, w.Body.String())
	}
	var resp struct {
		Key   string `json:"key"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Key != "incident-1" || resp.Count != 2 {
		t.Fatalf("lineage incident-1 = %+v, want key=incident-1 count=2", resp)
	}

	// Missing key -> 400.
	if w := do(""); w.Code != http.StatusBadRequest {
		t.Fatalf("missing key: got %d, want 400", w.Code)
	}
}
