package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestCaptureHardwareEndpointExamples is a throwaway helper used to print the
// exact JSON each hardware-transparency endpoint returns on this host so the
// examples in the task report are literal, not paraphrased.
func TestCaptureHardwareEndpointExamples(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		path    string
		handler gin.HandlerFunc
	}{
		{"/api/v1/gpu/mig", handleGPUMig},
		{"/api/v1/gpu/migrate", handleGPUMigrate},
		{"/api/v1/sgx/status", handleSGXStatus},
	} {
		r := gin.New()
		r.GET(tc.path, tc.handler)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
		t.Logf("GET %s -> %d\n%s", tc.path, w.Code, w.Body.String())
	}
}
