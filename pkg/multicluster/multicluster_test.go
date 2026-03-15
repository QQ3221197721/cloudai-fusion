package multicluster

import (
	"context"
	"testing"
	"time"
)

func TestJoinCluster(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	member, err := mgr.JoinCluster(ctx, &MemberCluster{
		Name:     "cluster-east",
		Region:   "us-east-1",
		Provider: "aws",
		Endpoint: "https://k8s-east.example.com",
	})
	if err != nil {
		t.Fatalf("JoinCluster failed: %v", err)
	}
	if member.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if member.Status != MemberStatusReady {
		t.Errorf("expected ready status, got %s", member.Status)
	}
	if member.Role != RoleMember {
		t.Errorf("expected member role, got %s", member.Role)
	}
}

func TestJoinDuplicateCluster(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	mgr.JoinCluster(ctx, &MemberCluster{Name: "cluster-a", Region: "us-east-1"})
	_, err := mgr.JoinCluster(ctx, &MemberCluster{Name: "cluster-a", Region: "us-west-2"})
	if err == nil {
		t.Fatal("expected error for duplicate cluster name")
	}
}

func TestLeaveCluster(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	member, _ := mgr.JoinCluster(ctx, &MemberCluster{Name: "cluster-temp", Region: "eu-west-1"})

	err := mgr.LeaveCluster(ctx, member.ID)
	if err != nil {
		t.Fatalf("LeaveCluster failed: %v", err)
	}

	_, err = mgr.GetMember(ctx, member.ID)
	if err == nil {
		t.Fatal("expected error after leaving")
	}
}

func TestListMembers(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	mgr.JoinCluster(ctx, &MemberCluster{Name: "c1", Region: "us-east-1"})
	mgr.JoinCluster(ctx, &MemberCluster{Name: "c2", Region: "us-west-2"})
	mgr.JoinCluster(ctx, &MemberCluster{Name: "c3", Region: "eu-west-1"})

	members, err := mgr.ListMembers(ctx)
	if err != nil {
		t.Fatalf("ListMembers failed: %v", err)
	}
	if len(members) != 3 {
		t.Errorf("expected 3 members, got %d", len(members))
	}
}

func TestUpdateHeartbeat(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	member, _ := mgr.JoinCluster(ctx, &MemberCluster{Name: "hb-test", Region: "us-east-1"})

	err := mgr.UpdateHeartbeat(ctx, member.ID, ClusterCapacity{
		CPUMillicores: 32000,
		MemoryBytes:   64 * 1024 * 1024 * 1024,
		GPUCount:      8,
		NodeCount:     4,
	})
	if err != nil {
		t.Fatalf("UpdateHeartbeat failed: %v", err)
	}

	updated, _ := mgr.GetMember(ctx, member.ID)
	if updated.Capacity.GPUCount != 8 {
		t.Errorf("expected 8 GPUs, got %d", updated.Capacity.GPUCount)
	}
}

func TestCreateGlobalService(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	svc, err := mgr.CreateGlobalService(ctx, &GlobalService{
		Name:      "apiserver",
		Namespace: "cloudai-system",
		Endpoints: []ServiceEndpoint{
			{ClusterID: "c1", Address: "10.0.1.1", Port: 8080, Weight: 3, Healthy: true, Region: "us-east-1"},
			{ClusterID: "c2", Address: "10.0.2.1", Port: 8080, Weight: 1, Healthy: true, Region: "eu-west-1"},
		},
	})
	if err != nil {
		t.Fatalf("CreateGlobalService failed: %v", err)
	}
	if svc.Policy != LBPolicyWeightedRoundRobin {
		t.Errorf("expected weighted-round-robin policy, got %s", svc.Policy)
	}
}

func TestResolveEndpoint(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	svc, _ := mgr.CreateGlobalService(ctx, &GlobalService{
		Name:      "test-svc",
		Namespace: "default",
		Policy:    LBPolicyWeightedRoundRobin,
		Endpoints: []ServiceEndpoint{
			{ClusterID: "c1", Address: "10.0.1.1", Port: 8080, Weight: 10, Healthy: true},
			{ClusterID: "c2", Address: "10.0.2.1", Port: 8080, Weight: 1, Healthy: false},
		},
	})

	ep, err := mgr.ResolveEndpoint(ctx, svc.ID)
	if err != nil {
		t.Fatalf("ResolveEndpoint failed: %v", err)
	}
	// Only c1 is healthy, so it must be selected
	if ep.ClusterID != "c1" {
		t.Errorf("expected c1, got %s", ep.ClusterID)
	}
}

func TestResolveEndpointLatencyBased(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	svc, _ := mgr.CreateGlobalService(ctx, &GlobalService{
		Name:   "latency-svc",
		Policy: LBPolicyLatencyBased,
		Endpoints: []ServiceEndpoint{
			{ClusterID: "c1", Address: "10.0.1.1", Port: 80, Weight: 1, Healthy: true, LatencyMs: 50},
			{ClusterID: "c2", Address: "10.0.2.1", Port: 80, Weight: 1, Healthy: true, LatencyMs: 10},
		},
	})

	ep, _ := mgr.ResolveEndpoint(ctx, svc.ID)
	if ep.ClusterID != "c2" {
		t.Errorf("expected c2 (lowest latency), got %s", ep.ClusterID)
	}
}

func TestResolveEndpointNoHealthy(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	svc, _ := mgr.CreateGlobalService(ctx, &GlobalService{
		Name: "unhealthy-svc",
		Endpoints: []ServiceEndpoint{
			{ClusterID: "c1", Healthy: false},
		},
	})

	_, err := mgr.ResolveEndpoint(ctx, svc.ID)
	if err == nil {
		t.Fatal("expected error when no healthy endpoints")
	}
}

func TestCreateDRPlan(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	plan, err := mgr.CreateDRPlan(ctx, &DRPlan{
		Name:             "main-dr",
		Strategy:         DRHotStandby,
		PrimaryClusterID: "c1",
		StandbyClusters: []StandbyCluster{
			{ClusterID: "c2", Priority: 1, Role: "standby"},
			{ClusterID: "c3", Priority: 2, Role: "standby"},
		},
		RPO: 5 * time.Minute,
		RTO: 15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("CreateDRPlan failed: %v", err)
	}
	if plan.Status != DRStatusActive {
		t.Errorf("expected active status, got %s", plan.Status)
	}
	if plan.FailoverPolicy == nil {
		t.Fatal("expected default failover policy")
	}
}

func TestTriggerFailover(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	plan, _ := mgr.CreateDRPlan(ctx, &DRPlan{
		Name:             "failover-test",
		Strategy:         DRHotStandby,
		PrimaryClusterID: "primary-1",
		StandbyClusters: []StandbyCluster{
			{ClusterID: "standby-1", Priority: 1},
		},
	})

	event, err := mgr.TriggerFailover(ctx, plan.ID, "")
	if err != nil {
		t.Fatalf("TriggerFailover failed: %v", err)
	}
	if event.Status != "completed" {
		t.Errorf("expected completed, got %s", event.Status)
	}
	if event.ToClusterID != "standby-1" {
		t.Errorf("expected failover to standby-1, got %s", event.ToClusterID)
	}

	// Verify primary changed
	updated, _ := mgr.GetDRPlan(ctx, plan.ID)
	if updated.PrimaryClusterID != "standby-1" {
		t.Errorf("expected primary to be standby-1, got %s", updated.PrimaryClusterID)
	}
}

func TestFailback(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	plan, _ := mgr.CreateDRPlan(ctx, &DRPlan{
		Name:             "failback-test",
		Strategy:         DRMultiActive,
		PrimaryClusterID: "primary-1",
		StandbyClusters: []StandbyCluster{
			{ClusterID: "standby-1", Priority: 1},
		},
	})

	// Failover
	mgr.TriggerFailover(ctx, plan.ID, "standby-1")

	// Failback
	event, err := mgr.Failback(ctx, plan.ID, "primary-1")
	if err != nil {
		t.Fatalf("Failback failed: %v", err)
	}
	if event.ToClusterID != "primary-1" {
		t.Errorf("expected failback to primary-1, got %s", event.ToClusterID)
	}
}

func TestListFailoverEvents(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	plan, _ := mgr.CreateDRPlan(ctx, &DRPlan{
		Name:             "events-test",
		PrimaryClusterID: "p1",
		StandbyClusters:  []StandbyCluster{{ClusterID: "s1", Priority: 1}},
	})

	mgr.TriggerFailover(ctx, plan.ID, "s1")
	mgr.Failback(ctx, plan.ID, "p1")

	events, _ := mgr.ListFailoverEvents(ctx, plan.ID)
	if len(events) != 2 {
		t.Errorf("expected 2 events, got %d", len(events))
	}
}

func TestUpgradeCluster(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	member, _ := mgr.JoinCluster(ctx, &MemberCluster{Name: "upgrade-test", Region: "us-east-1"})

	err := mgr.UpgradeCluster(ctx, member.ID, "1.30.0", nil)
	if err != nil {
		t.Fatalf("UpgradeCluster failed: %v", err)
	}

	events, _ := mgr.GetLifecycleEvents(ctx, member.ID)
	found := false
	for _, e := range events {
		if e.Phase == PhaseUpgrading {
			found = true
		}
	}
	if !found {
		t.Error("expected upgrading lifecycle event")
	}
}

func TestScaleCluster(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	member, _ := mgr.JoinCluster(ctx, &MemberCluster{
		Name:     "scale-test",
		Region:   "us-east-1",
		Capacity: ClusterCapacity{NodeCount: 3},
	})

	err := mgr.ScaleCluster(ctx, member.ID, 10)
	if err != nil {
		t.Fatalf("ScaleCluster failed: %v", err)
	}

	updated, _ := mgr.GetMember(ctx, member.ID)
	if updated.Capacity.NodeCount != 10 {
		t.Errorf("expected 10 nodes, got %d", updated.Capacity.NodeCount)
	}
}

func TestDrainCluster(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx := context.Background()

	member, _ := mgr.JoinCluster(ctx, &MemberCluster{Name: "drain-test", Region: "eu-west-1"})

	err := mgr.DrainCluster(ctx, member.ID)
	if err != nil {
		t.Fatalf("DrainCluster failed: %v", err)
	}

	updated, _ := mgr.GetMember(ctx, member.ID)
	if updated.Status != MemberStatusNotReady {
		t.Errorf("expected not-ready status, got %s", updated.Status)
	}
}

func TestHealthMonitorLoop(t *testing.T) {
	cfg := DefaultManagerConfig()
	cfg.HealthCheckInterval = 50 * time.Millisecond
	mgr := NewManager(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	mgr.JoinCluster(ctx, &MemberCluster{Name: "monitor-test", Region: "us-east-1"})

	mgr.StartHealthMonitor(ctx)
	// Should exit cleanly after context timeout
}

func TestCancelledContext(t *testing.T) {
	mgr := NewManager(DefaultManagerConfig())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := mgr.JoinCluster(ctx, &MemberCluster{Name: "test"})
	if err == nil {
		t.Fatal("expected error with cancelled context")
	}

	_, err = mgr.CreateGlobalService(ctx, &GlobalService{Name: "test"})
	if err == nil {
		t.Fatal("expected error with cancelled context")
	}
}
