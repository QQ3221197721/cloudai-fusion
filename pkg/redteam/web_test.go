package redteam

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
)

// vulnServer simulates a broken-access-control app: /login hands out a token to
// anyone, and /admin is reachable only with that token. An auth-bypass chain
// (login -> extract token -> use token on /admin) exploits it.
func vulnServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"token":"secret-abc"}`))
	})
	mux.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer secret-abc" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ADMIN PANEL"))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("unauthorized"))
	})
	return httptest.NewServer(mux)
}

func exploitScope(baseURL string) Scope {
	return Scope{
		Targets:         []Target{{Kind: TargetURL, Value: baseURL}},
		AllowTechniques: []string{"T1190"},
		MaxRiskTier:     RiskExploit,
		ApprovalReq:     RiskExploit, // exploitation requires human approval
	}
}

func authBypassChain(baseURL string) WebChain {
	return WebChain{
		Name:      "auth-bypass",
		Technique: "T1190",
		BaseURL:   baseURL,
		Steps: []WebStep{
			{
				Name:          "login",
				Method:        "POST",
				Path:          "/login",
				Headers:       map[string]string{"Content-Type": "application/json"},
				Body:          `{"user":"x","pass":"y"}`,
				Extract:       []Extractor{{Var: "token", Source: "body-regex", Regex: `"token":"([^"]+)"`}},
				SuccessStatus: 200,
			},
			{
				Name:            "admin",
				Method:          "GET",
				Path:            "/admin",
				Headers:         map[string]string{"Authorization": "Bearer {{token}}"},
				SuccessStatus:   200,
				SuccessContains: "ADMIN PANEL",
			},
		},
	}
}

func TestWebExploit_AuthBypassChainIsVerifiable(t *testing.T) {
	t.Cleanup(capability.Reset)
	srv := vulnServer()
	defer srv.Close()

	ledger := newLedger(t)
	mgr := NewManager(ledger, nil)
	ctx := context.Background()
	e, err := mgr.Create(ctx, exploitScope(srv.URL), "alice")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	eng := NewWebExploitEngine(mgr, ledger, nil)
	result, err := eng.RunChain(ctx, e.ID, authBypassChain(srv.URL), "operator")
	if err != nil {
		t.Fatalf("run chain: %v", err)
	}
	if !result.Success || len(result.Steps) != 2 || !result.Steps[0].OK || !result.Steps[1].OK {
		t.Fatalf("auth-bypass chain must succeed end-to-end: %+v", result)
	}
	// Step 1 must have extracted the token and both steps carry hashes.
	if len(result.Steps[0].Extracted) != 1 || result.Steps[0].Extracted[0] != "token" {
		t.Fatalf("login step must extract token, got %+v", result.Steps[0].Extracted)
	}
	for i, s := range result.Steps {
		if s.RequestHash == "" || s.ResponseHash == "" {
			t.Fatalf("step %d must carry request+response hashes", i)
		}
	}

	// Ledger: exploit receipt + approval + finding, all verifiable.
	all, _ := ledger.Store().All(ctx)
	if countAction(all, ActionWebExploit) != 1 || countAction(all, ActionApproval) != 1 || countAction(all, ActionFinding) != 1 {
		t.Fatalf("expected web.exploit+approval+finding, got exploit=%d approval=%d finding=%d",
			countAction(all, ActionWebExploit), countAction(all, ActionApproval), countAction(all, ActionFinding))
	}
	// The web.exploit receipt must carry per-step request/response hashes.
	var payload map[string]any
	for _, ev := range all {
		if ev.Action == ActionWebExploit {
			_ = json.Unmarshal(ev.Payload, &payload)
		}
	}
	steps, _ := payload["steps"].([]any)
	if len(steps) != 2 {
		t.Fatalf("web.exploit receipt must record 2 steps, got %v", payload["steps"])
	}
	first, _ := steps[0].(map[string]any)
	if first["request_hash"] == nil || first["response_hash"] == nil {
		t.Fatalf("receipt steps must carry request/response hashes, got %v", first)
	}
	assertChainVerifies(t, ledger)
}

func TestWebExploit_RequiresApproval(t *testing.T) {
	t.Cleanup(capability.Reset)
	srv := vulnServer()
	defer srv.Close()
	ledger := newLedger(t)
	mgr := NewManager(ledger, nil)
	ctx := context.Background()
	e, _ := mgr.Create(ctx, exploitScope(srv.URL), "alice")

	eng := NewWebExploitEngine(mgr, ledger, nil)
	// No approver -> must refuse and NOT run the chain.
	if _, err := eng.RunChain(ctx, e.ID, authBypassChain(srv.URL), ""); err == nil {
		t.Fatal("exploit-tier chain must require approval")
	}
	all, _ := ledger.Store().All(ctx)
	if countAction(all, ActionWebExploit) != 0 || countAction(all, ActionFinding) != 0 {
		t.Fatal("no exploit/finding receipts may be recorded without approval")
	}
	assertChainVerifies(t, ledger)
}

func TestWebExploit_OutOfScopeDenied(t *testing.T) {
	t.Cleanup(capability.Reset)
	srv := vulnServer()
	defer srv.Close()
	ledger := newLedger(t)
	mgr := NewManager(ledger, nil)
	ctx := context.Background()
	// Scope authorizes a DIFFERENT origin than the chain's base URL.
	scope := exploitScope("http://example.com/")
	e, _ := mgr.Create(ctx, scope, "alice")

	eng := NewWebExploitEngine(mgr, ledger, nil)
	if _, err := eng.RunChain(ctx, e.ID, authBypassChain(srv.URL), "operator"); err == nil {
		t.Fatal("out-of-scope chain must be denied")
	}
	all, _ := ledger.Store().All(ctx)
	if countAction(all, ActionScopeDeny) != 1 || countAction(all, ActionWebExploit) != 0 {
		t.Fatalf("expected a scope.deny and no exploit, got deny=%d exploit=%d",
			countAction(all, ActionScopeDeny), countAction(all, ActionWebExploit))
	}
	assertChainVerifies(t, ledger)
}

func TestRunWebChain_ReproducibleRequestHashes(t *testing.T) {
	srv := vulnServer()
	defer srv.Close()
	ctx := context.Background()
	chain := authBypassChain(srv.URL)

	r1, err := RunWebChain(ctx, srv.Client(), chain)
	if err != nil {
		t.Fatalf("run1: %v", err)
	}
	r2, err := RunWebChain(ctx, srv.Client(), chain)
	if err != nil {
		t.Fatalf("run2: %v", err)
	}
	if !r1.Success || !r2.Success || len(r1.Steps) != len(r2.Steps) {
		t.Fatalf("both runs must succeed with equal step counts")
	}
	for i := range r1.Steps {
		if r1.Steps[i].RequestHash != r2.Steps[i].RequestHash {
			t.Fatalf("step %d request hash must be reproducible: %s != %s", i, r1.Steps[i].RequestHash, r2.Steps[i].RequestHash)
		}
	}
}
