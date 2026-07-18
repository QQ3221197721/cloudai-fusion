package redteam

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

func newLedger(t *testing.T) *evidence.Ledger {
	t.Helper()
	signer, err := evidence.NewSignerFromSeed(bytes.Repeat([]byte{0x7a}, 32))
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	l, err := evidence.NewLedger(evidence.LedgerConfig{Store: evidence.NewMemoryStore(), Signer: signer})
	if err != nil {
		t.Fatalf("ledger: %v", err)
	}
	return l
}

func countAction(all []*evidence.Evidence, action string) int {
	n := 0
	for _, e := range all {
		if e.Action == action {
			n++
		}
	}
	return n
}

func assertChainVerifies(t *testing.T, l *evidence.Ledger) {
	t.Helper()
	all, _ := l.Store().All(context.Background())
	if rep, _ := evidence.VerifyChain(all, l.Signer().PublicKey()); !rep.Valid {
		t.Fatalf("evidence chain must verify (cafctl-equivalent), got %+v", rep)
	}
}

func TestScope_Matching(t *testing.T) {
	s := Scope{
		Targets: []Target{
			{Kind: TargetCIDR, Value: "10.0.0.0/24"},
			{Kind: TargetHost, Value: "example.com"},
			{Kind: TargetURL, Value: "https://app.test/"},
		},
		AllowTechniques: []string{"T1190", "T1595"},
		DenyTechniques:  []string{"T1595"}, // deny overrides allow
	}
	// CIDR
	if !s.InScope("10.0.0.5") || !s.InScope("10.0.0.9:8080") {
		t.Fatal("CIDR host should be in scope")
	}
	if s.InScope("10.0.1.5") {
		t.Fatal("host outside CIDR must be out of scope")
	}
	// Host (exact + subdomain + URL host extraction)
	if !s.InScope("example.com") || !s.InScope("api.example.com") || !s.InScope("https://example.com/x") {
		t.Fatal("host and subdomain should be in scope")
	}
	if s.InScope("evil.com") {
		t.Fatal("unrelated host must be out of scope")
	}
	// URL prefix
	if !s.InScope("https://app.test/login") || s.InScope("https://other.test/") {
		t.Fatal("URL prefix scoping incorrect")
	}
	// Techniques: allowed unless denied; deny wins; "*" not present so others denied.
	if !s.TechniqueAllowed("T1190") {
		t.Fatal("T1190 must be allowed")
	}
	if s.TechniqueAllowed("T1595") {
		t.Fatal("denied technique must win over allow")
	}
	if s.TechniqueAllowed("T1059") {
		t.Fatal("unlisted technique must be denied (deny-by-default)")
	}
}

func TestScope_WindowAndWildcard(t *testing.T) {
	now := time.Now()
	s := Scope{
		Targets:         []Target{{Kind: TargetHost, Value: "lab.local"}},
		AllowTechniques: []string{"*"},
		Window:          TimeWindow{Start: now.Add(-time.Hour), End: now.Add(time.Hour)},
	}
	if !s.WithinWindow(now) {
		t.Fatal("now must be within window")
	}
	if s.WithinWindow(now.Add(2 * time.Hour)) {
		t.Fatal("after end must be out of window")
	}
	if !s.TechniqueAllowed("T9999") {
		t.Fatal("wildcard must allow any technique")
	}
}

func readOnlyScope() Scope {
	return Scope{
		Targets:         []Target{{Kind: TargetHost, Value: "lab.local"}},
		AllowTechniques: []string{"T1595", "T1190"},
		MaxRiskTier:     RiskReadOnly,
		ApprovalReq:     RiskExploit, // read-only auto-runs; exploit+ needs approval
	}
}

func TestGate_EnforcesScopeAndRecordsDecisions(t *testing.T) {
	t.Cleanup(capability.Reset)
	ledger := newLedger(t)
	gate := NewGate("eng-1", readOnlyScope(), ledger, nil)
	ctx := context.Background()

	// In-scope, allowed technique, read-only -> allowed, no approval needed.
	if d := gate.Check(ctx, Action{ID: "a1", Technique: "T1595", Target: "lab.local", RiskTier: RiskReadOnly}); !d.Allowed || d.NeedsApproval {
		t.Fatalf("in-scope read-only action must be allowed without approval: %+v", d)
	}
	// Out-of-scope target.
	if d := gate.Check(ctx, Action{ID: "a2", Technique: "T1595", Target: "evil.com", RiskTier: RiskReadOnly}); d.Allowed || d.Reason != "target out of scope" {
		t.Fatalf("out-of-scope target must be denied: %+v", d)
	}
	// Unauthorized technique.
	if d := gate.Check(ctx, Action{ID: "a3", Technique: "T1059", Target: "lab.local", RiskTier: RiskReadOnly}); d.Allowed || d.Reason != "technique not authorized" {
		t.Fatalf("unauthorized technique must be denied: %+v", d)
	}
	// Risk tier exceeds authorization.
	if d := gate.Check(ctx, Action{ID: "a4", Technique: "T1190", Target: "lab.local", RiskTier: RiskExploit}); d.Allowed || d.Reason != "risk tier exceeds authorization" {
		t.Fatalf("over-tier action must be denied: %+v", d)
	}

	all, _ := ledger.Store().All(ctx)
	if got := countAction(all, ActionScopeDeny); got != 3 {
		t.Fatalf("expected 3 scope.deny receipts, got %d", got)
	}
	if got := countAction(all, ActionActionAuthorized); got != 1 {
		t.Fatalf("expected 1 action.authorized receipt, got %d", got)
	}
	assertChainVerifies(t, ledger)
}

func TestGate_KillSwitchAndApproval(t *testing.T) {
	t.Cleanup(capability.Reset)
	ledger := newLedger(t)
	sc := readOnlyScope()
	sc.MaxRiskTier = RiskExploit
	sc.ApprovalReq = RiskExploit // exploit needs approval
	gate := NewGate("eng-2", sc, ledger, nil)
	ctx := context.Background()

	// Exploit-tier in scope -> allowed but needs approval.
	if d := gate.Check(ctx, Action{ID: "e1", Technique: "T1190", Target: "lab.local", RiskTier: RiskExploit}); !d.Allowed || !d.NeedsApproval {
		t.Fatalf("exploit-tier action must be allowed-with-approval: %+v", d)
	}

	// Kill-switch: idempotent, and blocks further actions.
	gate.Abort(ctx, "operator kill-switch")
	gate.Abort(ctx, "again") // idempotent, no second receipt
	if !gate.Aborted() {
		t.Fatal("gate must report aborted")
	}
	if d := gate.Check(ctx, Action{ID: "e2", Technique: "T1595", Target: "lab.local", RiskTier: RiskReadOnly}); d.Allowed || d.Reason != "engagement aborted" {
		t.Fatalf("post-abort action must be denied: %+v", d)
	}

	all, _ := ledger.Store().All(ctx)
	if got := countAction(all, ActionAbort); got != 1 {
		t.Fatalf("abort must be idempotent (exactly 1 receipt), got %d", got)
	}
	assertChainVerifies(t, ledger)
}

func TestGate_RateLimit(t *testing.T) {
	t.Cleanup(capability.Reset)
	ledger := newLedger(t)
	sc := readOnlyScope()
	sc.RateLimit = RateLimit{MaxActions: 2, Per: time.Hour}
	gate := NewGate("eng-3", sc, ledger, nil)
	ctx := context.Background()
	a := Action{Technique: "T1595", Target: "lab.local", RiskTier: RiskReadOnly}

	if d := gate.Check(ctx, a); !d.Allowed {
		t.Fatal("action 1 should be allowed")
	}
	if d := gate.Check(ctx, a); !d.Allowed {
		t.Fatal("action 2 should be allowed")
	}
	if d := gate.Check(ctx, a); d.Allowed || d.Reason != "rate limit exceeded" {
		t.Fatalf("action 3 must hit the rate limit: %+v", d)
	}
	assertChainVerifies(t, ledger)
}

func TestManager_CreateRunAndVerifiableChain(t *testing.T) {
	t.Cleanup(capability.Reset)
	ledger := newLedger(t)
	mgr := NewManager(ledger, nil)
	ctx := context.Background()

	// A scope with no targets is rejected (deny-by-default authorization).
	if _, err := mgr.Create(ctx, Scope{}, "alice"); err == nil {
		t.Fatal("creating an engagement with no targets must fail")
	}

	e, err := mgr.Create(ctx, readOnlyScope(), "alice")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if e.Status != StatusPending {
		t.Fatalf("new engagement must be pending, got %s", e.Status)
	}

	planner := StaticPlanner{Actions: []Action{
		{ID: "p1", Technique: "T1595", Target: "lab.local", RiskTier: RiskReadOnly}, // allowed
		{ID: "p2", Technique: "T1059", Target: "lab.local", RiskTier: RiskReadOnly}, // denied technique
		{ID: "p3", Technique: "T1190", Target: "evil.com", RiskTier: RiskReadOnly},  // denied target
	}}
	engine := NewEngine(mgr, planner, nil, nil) // DryRunExecutor
	res, err := engine.Run(ctx, e.ID)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Planned != 3 || res.Authorized != 1 || res.Denied != 2 || res.Executed != 1 {
		t.Fatalf("unexpected run result: %+v", res)
	}
	if e.Status != StatusCompleted {
		t.Fatalf("engagement must complete, got %s", e.Status)
	}

	// Ledger: 1 grant + 1 authorized + 2 deny.
	all, _ := ledger.Store().All(ctx)
	if countAction(all, ActionScopeGrant) != 1 || countAction(all, ActionActionAuthorized) != 1 || countAction(all, ActionScopeDeny) != 2 {
		t.Fatalf("unexpected receipt mix: grant=%d auth=%d deny=%d",
			countAction(all, ActionScopeGrant), countAction(all, ActionActionAuthorized), countAction(all, ActionScopeDeny))
	}
	assertChainVerifies(t, ledger)

	// capability honestly reports the authorization gate as real.
	var real bool
	for _, b := range capability.Snapshot() {
		if b.Component == "redteam.authz" && b.Mode == capability.ModeReal {
			real = true
		}
	}
	if !real {
		t.Fatal("redteam.authz must be reported real")
	}
}

func TestManager_AbortBlocksRunAndRecordsFinding(t *testing.T) {
	t.Cleanup(capability.Reset)
	ledger := newLedger(t)
	mgr := NewManager(ledger, nil)
	ctx := context.Background()

	e, err := mgr.Create(ctx, readOnlyScope(), "bob")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Record a finding (verifiable).
	if err := mgr.AddFinding(ctx, e.ID, &Finding{Asset: "lab.local", Severity: "high", Title: "test", Technique: "T1190"}); err != nil {
		t.Fatalf("add finding: %v", err)
	}
	if len(e.Findings) != 1 {
		t.Fatalf("finding must be attached, got %d", len(e.Findings))
	}
	// Abort -> aborted; a subsequent run must be refused (cannot start terminal).
	if err := mgr.Abort(ctx, e.ID, "manual"); err != nil {
		t.Fatalf("abort: %v", err)
	}
	if e.Status != StatusAborted {
		t.Fatalf("engagement must be aborted, got %s", e.Status)
	}
	engine := NewEngine(mgr, StaticPlanner{}, nil, nil)
	if _, err := engine.Run(ctx, e.ID); err == nil {
		t.Fatal("running an aborted engagement must fail")
	}

	all, _ := ledger.Store().All(ctx)
	if countAction(all, ActionFinding) != 1 || countAction(all, ActionAbort) != 1 {
		t.Fatalf("expected 1 finding + 1 abort receipt")
	}
	assertChainVerifies(t, ledger)
}
