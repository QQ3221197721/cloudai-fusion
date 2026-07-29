package security

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// serveThrough runs one request through the gateway middleware and returns the
// response recorder — the REAL enforcement path (same code production runs).
func serveThrough(gw *Gateway, remoteAddr, path string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gw.GatewayMiddleware())
	r.GET("/*any", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestGateway_BlockIP_RealEnforcement is the L8 SOAR block-network proof: a
// runtime BlockIP makes the very next request from that source a 403 at the
// middleware. This is the "real" the actuator claims.
func TestGateway_BlockIP_RealEnforcement(t *testing.T) {
	gw := NewGateway(GatewayConfig{
		IPAccessList: NewIPAccessList(nil, nil),
		EnableIPACL:  true,
	})

	// Before the block: request passes.
	if w := serveThrough(gw, "203.0.113.5:41000", "/api/v1/anything"); w.Code != http.StatusOK {
		t.Fatalf("pre-block request should pass, got %d", w.Code)
	}

	if enforced := gw.BlockIP("203.0.113.5"); !enforced {
		t.Fatal("BlockIP must report enforcement active when IP ACL is enabled")
	}

	// After the block: same source is rejected; another source still passes.
	if w := serveThrough(gw, "203.0.113.5:41001", "/api/v1/anything"); w.Code != http.StatusForbidden {
		t.Fatalf("blocked IP should get 403, got %d", w.Code)
	}
	if w := serveThrough(gw, "198.51.100.9:41002", "/api/v1/anything"); w.Code != http.StatusOK {
		t.Fatalf("unrelated IP should pass, got %d", w.Code)
	}
}

// TestGateway_BlockIP_DisabledIsHonest: with enforcement off, BlockIP records
// intent but reports it is NOT enforced — and requests really do pass.
func TestGateway_BlockIP_DisabledIsHonest(t *testing.T) {
	gw := NewGateway(GatewayConfig{EnableIPACL: false})
	if enforced := gw.BlockIP("203.0.113.5"); enforced {
		t.Fatal("BlockIP must not claim enforcement when IP ACL is disabled")
	}
	if w := serveThrough(gw, "203.0.113.5:41000", "/x"); w.Code != http.StatusOK {
		t.Fatalf("with ACL disabled the request must pass, got %d", w.Code)
	}
}

// TestIPAccessList_CIDRSemantics covers CIDR blocks, exact IPs and allowlists.
func TestIPAccessList_CIDRSemantics(t *testing.T) {
	acl := NewIPAccessList(nil, []string{"10.0.0.0/8"})
	if acl.IsAllowed("10.1.2.3") {
		t.Fatal("10.1.2.3 must be blocked by 10.0.0.0/8")
	}
	if !acl.IsAllowed("192.168.1.1") {
		t.Fatal("192.168.1.1 must pass (no allowlist configured)")
	}

	// Runtime AddBlock with a single IP.
	acl.AddBlock("192.168.1.1")
	if acl.IsAllowed("192.168.1.1") {
		t.Fatal("192.168.1.1 must be blocked after AddBlock")
	}

	// Allowlist mode: only listed sources pass.
	allowOnly := NewIPAccessList([]string{"172.16.0.0/12"}, nil)
	if !allowOnly.IsAllowed("172.16.5.5") {
		t.Fatal("172.16.5.5 must be allowed by the allowlist")
	}
	if allowOnly.IsAllowed("8.8.8.8") {
		t.Fatal("8.8.8.8 must be rejected in allowlist mode")
	}
}

// TestWAF_CustomRuleBlocks proves the WAF really matches and blocks at the
// middleware, using a deterministic custom rule (independent of defaults).
func TestWAF_CustomRuleBlocks(t *testing.T) {
	waf := NewWAFEngine(nil)
	if err := waf.AddRule(&WAFRule{
		Name: "block-admin-probe", Target: "path", Pattern: `(?i)/wp-admin`,
		Action: "block", Severity: "high", Enabled: true,
	}); err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	gw := NewGateway(GatewayConfig{WAFEngine: waf, EnableWAF: true})

	if w := serveThrough(gw, "198.51.100.1:1000", "/wp-admin/setup.php"); w.Code != http.StatusForbidden {
		t.Fatalf("WAF should block /wp-admin probe, got %d", w.Code)
	}
	if w := serveThrough(gw, "198.51.100.1:1001", "/api/v1/clusters"); w.Code != http.StatusOK {
		t.Fatalf("clean path should pass, got %d", w.Code)
	}

	// Invalid regex must be rejected, not silently ignored.
	if err := waf.AddRule(&WAFRule{Name: "bad", Target: "path", Pattern: "(", Action: "block", Enabled: true}); err == nil {
		t.Fatal("AddRule with invalid regex must error")
	}
}

// TestGateway_APIKeyLifecycle covers registration, validation, revocation and
// expiry — the full key lifecycle at the middleware.
func TestGateway_APIKeyLifecycle(t *testing.T) {
	gw := NewGateway(GatewayConfig{EnableAPIKeys: true})
	gw.RegisterAPIKey(&APIKey{Key: "test-key-12345678", Plan: "pro", Enabled: true})

	key, err := gw.ValidateAPIKey("test-key-12345678")
	if err != nil || key.Plan != "pro" {
		t.Fatalf("ValidateAPIKey: key=%v err=%v", key, err)
	}
	if _, err := gw.ValidateAPIKey("unknown"); err == nil {
		t.Fatal("unknown key must fail validation")
	}

	if !gw.RevokeAPIKey("test-key-12345678") {
		t.Fatal("RevokeAPIKey must succeed for a registered key")
	}
	if _, err := gw.ValidateAPIKey("test-key-12345678"); err == nil {
		t.Fatal("revoked key must fail validation")
	}

	// Expired key.
	past := time.Now().Add(-time.Hour)
	gw.RegisterAPIKey(&APIKey{Key: "expired-key-0000", Enabled: true, ExpiresAt: &past})
	if _, err := gw.ValidateAPIKey("expired-key-0000"); err == nil {
		t.Fatal("expired key must fail validation")
	}
}

// TestGateway_Status verifies the honest status snapshot.
func TestGateway_Status(t *testing.T) {
	gw := NewGateway(GatewayConfig{EnableIPACL: true})
	gw.BlockIP("203.0.113.7")
	st := gw.Status()
	if !st.IPACLEnabled {
		t.Fatal("status must report IP ACL enabled")
	}
	if st.BlockRules == 0 {
		t.Fatal("status must count the runtime block rule")
	}
}
