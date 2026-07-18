package redteam

import (
	"context"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
)

// adTestGraph models a classic path: user1 -> devs -> WS01 -> admin -> Domain Admins.
func adTestGraph() *ADGraph {
	g := NewADGraph()
	g.AddNode("user1", "user", false)
	g.AddNode("devs", "group", false)
	g.AddNode("WS01", "computer", false)
	g.AddNode("admin", "user", false)
	g.AddNode("DomainAdmins", "group", true) // high value
	g.AddEdge("user1", "devs", "MemberOf", "T1069")
	g.AddEdge("devs", "WS01", "AdminTo", "T1021")
	g.AddEdge("WS01", "admin", "HasSession", "T1550")
	g.AddEdge("admin", "DomainAdmins", "MemberOf", "T1069")
	return g
}

func lateralScope(domain string) Scope {
	return Scope{
		Targets:         []Target{{Kind: TargetHost, Value: domain}},
		AllowTechniques: []string{"*"},
		MaxRiskTier:     RiskLateral,
		ApprovalReq:     RiskLateral, // lateral movement requires approval
	}
}

func TestADGraph_ShortestPathAndHighValue(t *testing.T) {
	g := adTestGraph()
	path, ok := g.ShortestPath("user1", "DomainAdmins")
	if !ok || len(path) != 4 {
		t.Fatalf("expected a 4-hop path to DomainAdmins, got ok=%v len=%d", ok, len(path))
	}
	if path[0].From != "user1" || path[len(path)-1].To != "DomainAdmins" {
		t.Fatalf("path endpoints wrong: %+v", path)
	}
	hv, target, ok := g.PathToHighValue("user1")
	if !ok || target != "DomainAdmins" || len(hv) != 4 {
		t.Fatalf("PathToHighValue should reach DomainAdmins in 4 hops, got target=%s len=%d", target, len(hv))
	}
	if _, ok := g.ShortestPath("user1", "nonexistent"); ok {
		t.Fatal("path to an unreachable node must not be found")
	}
}

func TestADPathEngine_ExecutesAuthorizedPath(t *testing.T) {
	t.Cleanup(capability.Reset)
	ledger := newLedger(t)
	mgr := NewManager(ledger, nil)
	ctx := context.Background()
	e, err := mgr.Create(ctx, lateralScope("corp.local"), "alice")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	eng := NewADPathEngine(mgr, ledger, nil, "kind-adrange") // isolated range context
	res, err := eng.ExecutePath(ctx, e.ID, adTestGraph(), "user1", "DomainAdmins", "corp.local", "operator")
	if err != nil {
		t.Fatalf("execute path: %v", err)
	}
	if !res.Reached || len(res.Steps) != 4 {
		t.Fatalf("path must be reached in 4 authorized hops, got %+v", res)
	}
	for i, s := range res.Steps {
		if s.Status != "authorized" {
			t.Fatalf("hop %d must be authorized, got %q", i, s.Status)
		}
	}

	all, _ := ledger.Store().All(ctx)
	if countAction(all, ActionADPath) != 1 || countAction(all, ActionApproval) != 1 || countAction(all, ActionFinding) != 1 {
		t.Fatalf("expected ad.path+approval+finding, got path=%d approval=%d finding=%d",
			countAction(all, ActionADPath), countAction(all, ActionApproval), countAction(all, ActionFinding))
	}
	// Isolation must be honestly reported real (a range context was supplied).
	var isoReal bool
	for _, b := range capability.Snapshot() {
		if b.Component == "redteam.ad.isolation" && b.Mode == capability.ModeReal {
			isoReal = true
		}
	}
	if !isoReal {
		t.Fatal("ad.isolation must report real when a range context is set")
	}
	assertChainVerifies(t, ledger)
}

func TestADPathEngine_RequiresApproval(t *testing.T) {
	t.Cleanup(capability.Reset)
	ledger := newLedger(t)
	mgr := NewManager(ledger, nil)
	ctx := context.Background()
	e, _ := mgr.Create(ctx, lateralScope("corp.local"), "alice")

	eng := NewADPathEngine(mgr, ledger, nil, "kind-adrange")
	if _, err := eng.ExecutePath(ctx, e.ID, adTestGraph(), "user1", "DomainAdmins", "corp.local", ""); err == nil {
		t.Fatal("lateral path must require human approval")
	}
	all, _ := ledger.Store().All(ctx)
	if countAction(all, ActionFinding) != 0 {
		t.Fatal("no finding may be recorded without approval")
	}
	assertChainVerifies(t, ledger)
}

func TestADPathEngine_KillSwitchHaltsPath(t *testing.T) {
	t.Cleanup(capability.Reset)
	ledger := newLedger(t)
	mgr := NewManager(ledger, nil)
	ctx := context.Background()
	e, _ := mgr.Create(ctx, lateralScope("corp.local"), "alice")
	// Trip the kill-switch before executing.
	if err := mgr.Abort(ctx, e.ID, "operator kill-switch"); err != nil {
		t.Fatalf("abort: %v", err)
	}

	eng := NewADPathEngine(mgr, ledger, nil, "kind-adrange")
	res, err := eng.ExecutePath(ctx, e.ID, adTestGraph(), "user1", "DomainAdmins", "corp.local", "operator")
	if err != nil {
		t.Fatalf("execute path: %v", err)
	}
	if res.Reached {
		t.Fatal("an aborted engagement must not reach the target")
	}
	all, _ := ledger.Store().All(ctx)
	if countAction(all, ActionADPath) != 1 || countAction(all, ActionFinding) != 0 {
		t.Fatalf("expected a recorded (halted) ad.path and no finding, got path=%d finding=%d",
			countAction(all, ActionADPath), countAction(all, ActionFinding))
	}
	assertChainVerifies(t, ledger)
}

func TestADPathEngine_OutOfScopeDomainDenied(t *testing.T) {
	t.Cleanup(capability.Reset)
	ledger := newLedger(t)
	mgr := NewManager(ledger, nil)
	ctx := context.Background()
	// Scope authorizes a different domain than the path targets.
	e, _ := mgr.Create(ctx, lateralScope("other.local"), "alice")

	eng := NewADPathEngine(mgr, ledger, nil, "kind-adrange")
	res, err := eng.ExecutePath(ctx, e.ID, adTestGraph(), "user1", "DomainAdmins", "corp.local", "operator")
	if err != nil {
		t.Fatalf("execute path: %v", err)
	}
	if res.Reached {
		t.Fatal("out-of-scope domain must not be reached")
	}
	all, _ := ledger.Store().All(ctx)
	if countAction(all, ActionScopeDeny) < 1 || countAction(all, ActionFinding) != 0 {
		t.Fatalf("expected a scope.deny and no finding, got deny=%d finding=%d",
			countAction(all, ActionScopeDeny), countAction(all, ActionFinding))
	}
	assertChainVerifies(t, ledger)
}
