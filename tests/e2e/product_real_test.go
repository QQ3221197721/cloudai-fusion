package e2e

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/cache"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/eventbus"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/multicluster"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/scheduler"
)

// TestProduct_MultiTenantSchedulingLifecycle drives the REAL business managers
// (not mocks) through a cross-module product workflow and asserts the outputs
// of independent subsystems agree: the number of quota grants equals the
// recorded GPU usage equals the number of "workload.scheduled" events actually
// delivered, over-quota requests are rejected with a real reason, and only the
// scheduled workloads have cached state.
func TestProduct_MultiTenantSchedulingLifecycle(t *testing.T) {
	ctx := context.Background()
	lg := logrus.New()
	lg.SetLevel(logrus.PanicLevel)

	// Real event bus — one delivery per scheduled workload.
	bus := eventbus.NewMemoryBus(eventbus.Config{}, lg)
	defer func() { _ = bus.Close() }()
	var eventsSeen int64
	if _, err := bus.Subscribe("workload.scheduled", func(_ context.Context, _ *eventbus.Event) error {
		atomic.AddInt64(&eventsSeen, 1)
		return nil
	}); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Real capacity manager — tenant hard-capped at 4 GPUs (borrowing off).
	capMgr := scheduler.NewCapacityManager(64)
	capMgr.SetQuota(&scheduler.TenantQuota{TenantID: "team-alpha", GPUQuota: 4})

	// Real cache — workload state store.
	c := cache.NewMemoryCache(cache.Config{}, lg)
	defer func() { _ = c.Close() }()

	const attempts = 6
	granted := 0
	for i := 0; i < attempts; i++ {
		wl := fmt.Sprintf("wl-%d", i)
		ok, reason := capMgr.TryAllocate("team-alpha", 1, 0, 0)
		if !ok {
			if reason == "" {
				t.Errorf("%s rejected with empty reason (must be explained)", wl)
			}
			continue
		}
		granted++
		if err := c.Set(ctx, wl, []byte("running"), time.Minute); err != nil {
			t.Fatalf("cache set %s: %v", wl, err)
		}
		evt, err := eventbus.NewEvent("workload.scheduled", "Scheduled", wl, map[string]string{"tenant": "team-alpha"})
		if err != nil {
			t.Fatalf("new event: %v", err)
		}
		if err := bus.Publish(ctx, evt); err != nil {
			t.Fatalf("publish %s: %v", wl, err)
		}
	}

	// Consistency 1: exactly quota-many grants succeed.
	if granted != 4 {
		t.Fatalf("granted=%d, want 4 (quota cap of 4 GPUs)", granted)
	}
	// Consistency 2: recorded usage equals grants (no over/under counting).
	usage := capMgr.GetUsage()["team-alpha"]
	if usage == nil || usage.GPUsAllocated != granted {
		t.Errorf("usage GPUsAllocated=%v, want %d (must equal grants)", usage, granted)
	}
	// Consistency 3: exactly one event delivered per successful schedule.
	if got := int(atomic.LoadInt64(&eventsSeen)); got != granted {
		t.Errorf("events delivered=%d, want %d (must equal grants)", got, granted)
	}
	if got := int(bus.Stats().TotalDelivered); got != granted {
		t.Errorf("bus TotalDelivered=%d, want %d", got, granted)
	}
	// Consistency 4: cached state round-trips exactly for scheduled workloads.
	for i := 0; i < granted; i++ {
		wl := fmt.Sprintf("wl-%d", i)
		v, err := c.Get(ctx, wl)
		if err != nil || string(v) != "running" {
			t.Errorf("%s cache=%q err=%v, want \"running\"", wl, string(v), err)
		}
	}
	// Consistency 5: a quota-rejected workload must have NO cached state.
	if v, _ := c.Get(ctx, fmt.Sprintf("wl-%d", attempts-1)); len(v) != 0 {
		t.Errorf("rejected workload has cache state %q; expected none", string(v))
	}
}

// TestProduct_FederationRoutingReflectsRealLoad verifies the multi-cluster
// resource-based router dynamically routes to the genuinely least-loaded member
// cluster, and that the decision follows real capacity as load changes.
func TestProduct_FederationRoutingReflectsRealLoad(t *testing.T) {
	ctx := context.Background()
	mgr := multicluster.NewManager(multicluster.DefaultManagerConfig())

	east, err := mgr.JoinCluster(ctx, &multicluster.MemberCluster{Name: "east", Region: "us-east-1"})
	if err != nil {
		t.Fatalf("join east: %v", err)
	}
	west, err := mgr.JoinCluster(ctx, &multicluster.MemberCluster{Name: "west", Region: "us-west-2"})
	if err != nil {
		t.Fatalf("join west: %v", err)
	}

	// east heavily loaded, west idle.
	_ = mgr.UpdateHeartbeat(ctx, east.ID, multicluster.ClusterCapacity{GPUCount: 8, UsedGPUCount: 7, CPUMillicores: 1000, UsedCPUMillicores: 900})
	_ = mgr.UpdateHeartbeat(ctx, west.ID, multicluster.ClusterCapacity{GPUCount: 8, UsedGPUCount: 1, CPUMillicores: 1000, UsedCPUMillicores: 100})

	svc, err := mgr.CreateGlobalService(ctx, &multicluster.GlobalService{
		Name:   "inference",
		Policy: multicluster.LBPolicyResourceBased,
		Endpoints: []multicluster.ServiceEndpoint{
			{ClusterID: east.ID, Address: "10.0.0.1", Port: 80, Weight: 1, Healthy: true},
			{ClusterID: west.ID, Address: "10.0.0.2", Port: 80, Weight: 1, Healthy: true},
		},
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}

	// Resource routing must pick the least-loaded cluster (west).
	ep, err := mgr.ResolveEndpoint(ctx, svc.ID)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if ep.ClusterID != west.ID {
		t.Errorf("resource routing picked %s, want west (least loaded)", ep.ClusterID)
	}

	// Flip the load: west saturated, east freed. Routing must follow real load.
	_ = mgr.UpdateHeartbeat(ctx, west.ID, multicluster.ClusterCapacity{GPUCount: 8, UsedGPUCount: 8, CPUMillicores: 1000, UsedCPUMillicores: 1000})
	_ = mgr.UpdateHeartbeat(ctx, east.ID, multicluster.ClusterCapacity{GPUCount: 8, UsedGPUCount: 0, CPUMillicores: 1000, UsedCPUMillicores: 0})
	ep2, err := mgr.ResolveEndpoint(ctx, svc.ID)
	if err != nil {
		t.Fatalf("resolve after flip: %v", err)
	}
	if ep2.ClusterID != east.ID {
		t.Errorf("after load flip routing picked %s, want east (now least loaded)", ep2.ClusterID)
	}
}
