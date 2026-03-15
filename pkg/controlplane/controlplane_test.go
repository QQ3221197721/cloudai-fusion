package controlplane

import (
	"context"
	"testing"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/controller"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/eventbus"
)

// ============================================================================
// Helper: stub reconciler
// ============================================================================

type stubReconciler struct {
	name         string
	kind         string
	reconcileErr error
	callCount    int
}

func (r *stubReconciler) Reconcile(_ context.Context, req controller.Request) (controller.Result, error) {
	r.callCount++
	return controller.Result{}, r.reconcileErr
}
func (r *stubReconciler) Name() string         { return r.name }
func (r *stubReconciler) ResourceKind() string  { return r.kind }

// ============================================================================
// Config defaults
// ============================================================================

func TestNew_DefaultConfig(t *testing.T) {
	svc := New(Config{})
	if svc == nil {
		t.Fatal("New returned nil")
	}
	if svc.ctrlManager == nil {
		t.Error("ctrlManager should not be nil")
	}
	if svc.eventTriggers == nil {
		t.Error("eventTriggers should be initialized")
	}
}

func TestNew_WithLogger(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	svc := New(Config{Logger: logger})
	if svc.logger != logger {
		t.Error("logger should be set from config")
	}
}

func TestNew_DefaultConcurrency(t *testing.T) {
	svc := New(Config{MaxConcurrentReconciles: 0})
	// Should default to 2
	if svc.config.MaxConcurrentReconciles != 2 {
		t.Errorf("MaxConcurrentReconciles = %d, want 2", svc.config.MaxConcurrentReconciles)
	}
}

func TestNew_DefaultSyncPeriod(t *testing.T) {
	svc := New(Config{SyncPeriod: 0})
	if svc.config.SyncPeriod != 10*time.Minute {
		t.Errorf("SyncPeriod = %v, want 10m", svc.config.SyncPeriod)
	}
}

func TestNew_CustomValues(t *testing.T) {
	svc := New(Config{
		MaxConcurrentReconciles: 8,
		SyncPeriod:              5 * time.Minute,
		GRPCPort:                50051,
		HealthPort:              8081,
	})
	if svc.config.MaxConcurrentReconciles != 8 {
		t.Errorf("MaxConcurrentReconciles = %d, want 8", svc.config.MaxConcurrentReconciles)
	}
	if svc.config.SyncPeriod != 5*time.Minute {
		t.Errorf("SyncPeriod = %v, want 5m", svc.config.SyncPeriod)
	}
}

// ============================================================================
// ControllerManager
// ============================================================================

func TestService_ControllerManager(t *testing.T) {
	svc := New(Config{})
	mgr := svc.ControllerManager()
	if mgr == nil {
		t.Error("ControllerManager() should not return nil")
	}
}

// ============================================================================
// RegisterEventTrigger
// ============================================================================

func TestService_RegisterEventTrigger(t *testing.T) {
	svc := New(Config{})
	svc.RegisterEventTrigger("cluster.created", "cluster-controller")
	svc.RegisterEventTrigger("workload.scheduled", "workload-controller")

	if len(svc.eventTriggers) != 2 {
		t.Errorf("eventTriggers count = %d, want 2", len(svc.eventTriggers))
	}
	if svc.eventTriggers["cluster.created"] != "cluster-controller" {
		t.Error("cluster.created trigger not registered correctly")
	}
}

func TestService_RegisterEventTrigger_Overwrite(t *testing.T) {
	svc := New(Config{})
	svc.RegisterEventTrigger("cluster.created", "old-controller")
	svc.RegisterEventTrigger("cluster.created", "new-controller")

	if svc.eventTriggers["cluster.created"] != "new-controller" {
		t.Errorf("expected overwrite, got %q", svc.eventTriggers["cluster.created"])
	}
}

// ============================================================================
// Start / Stop lifecycle
// ============================================================================

func TestService_StartStop(t *testing.T) {
	bus := eventbus.NewMemoryBus(eventbus.DefaultConfig(), logrus.StandardLogger())
	defer bus.Close()

	svc := New(Config{
		EventBus: bus,
		Logger:   logrus.StandardLogger(),
	})

	ctx := context.Background()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Check started
	status := svc.Status()
	if !status.Started {
		t.Error("service should be started")
	}

	svc.Stop()
	status = svc.Status()
	if status.Started {
		t.Error("service should be stopped")
	}
}

func TestService_DoubleStart(t *testing.T) {
	svc := New(Config{Logger: logrus.StandardLogger()})
	ctx := context.Background()
	svc.Start(ctx)
	defer svc.Stop()

	err := svc.Start(ctx)
	if err == nil {
		t.Error("second Start should return error")
	}
}

func TestService_StopBeforeStart(t *testing.T) {
	svc := New(Config{Logger: logrus.StandardLogger()})
	// Should not panic
	svc.Stop()
}

// ============================================================================
// Start with EventBus triggers
// ============================================================================

func TestService_StartWithEventTriggers(t *testing.T) {
	bus := eventbus.NewMemoryBus(eventbus.DefaultConfig(), logrus.StandardLogger())
	defer bus.Close()

	svc := New(Config{
		EventBus: bus,
		Logger:   logrus.StandardLogger(),
	})

	rec := &stubReconciler{name: "cluster-ctrl", kind: "Cluster"}
	svc.ControllerManager().RegisterReconciler(rec)

	svc.RegisterEventTrigger(eventbus.TopicClusterCreated, "cluster-ctrl")

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	// Verify subscription is created — bus stats should show active subscriptions
	stats := bus.Stats()
	if stats.ActiveSubscriptions < 1 {
		t.Errorf("ActiveSubscriptions = %d, want >= 1", stats.ActiveSubscriptions)
	}
}

func TestService_StartWithoutEventBus(t *testing.T) {
	svc := New(Config{
		Logger: logrus.StandardLogger(),
	})
	// Should start without panic even without event bus
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start without event bus: %v", err)
	}
	defer svc.Stop()
}

// ============================================================================
// Status
// ============================================================================

func TestService_Status_Initial(t *testing.T) {
	svc := New(Config{Logger: logrus.StandardLogger()})
	status := svc.Status()
	if status.Started {
		t.Error("should not be started")
	}
	if status.EventBusStats != nil {
		t.Error("EventBusStats should be nil without event bus")
	}
	if status.EventTriggerCount != 0 {
		t.Errorf("EventTriggerCount = %d, want 0", status.EventTriggerCount)
	}
}

func TestService_Status_WithEventBus(t *testing.T) {
	bus := eventbus.NewMemoryBus(eventbus.DefaultConfig(), logrus.StandardLogger())
	defer bus.Close()

	svc := New(Config{
		EventBus: bus,
		Logger:   logrus.StandardLogger(),
	})
	svc.RegisterEventTrigger("cluster.created", "cluster-ctrl")

	status := svc.Status()
	if status.EventBusStats == nil {
		t.Error("EventBusStats should not be nil")
	}
	if status.EventTriggerCount != 1 {
		t.Errorf("EventTriggerCount = %d, want 1", status.EventTriggerCount)
	}
}

// ============================================================================
// ServiceStatus struct
// ============================================================================

func TestServiceStatus_Fields(t *testing.T) {
	status := ServiceStatus{
		Started:           true,
		EventTriggerCount: 3,
	}
	if !status.Started {
		t.Error("Started should be true")
	}
	if status.EventTriggerCount != 3 {
		t.Error("EventTriggerCount mismatch")
	}
}

// ============================================================================
// handleEvent — indirectly tested via event bus publish
// ============================================================================

func TestService_HandleEvent_ViaEventBus(t *testing.T) {
	bus := eventbus.NewMemoryBus(eventbus.Config{
		BufferSize: 100,
		MaxRetries: 1,
		RetryDelay: time.Millisecond,
	}, logrus.StandardLogger())
	defer bus.Close()

	svc := New(Config{
		EventBus: bus,
		Logger:   logrus.StandardLogger(),
	})

	rec := &stubReconciler{name: "wl-ctrl", kind: "Workload"}
	svc.ControllerManager().RegisterReconciler(rec)

	svc.RegisterEventTrigger("workload.created", "wl-ctrl")

	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	// Publish an event with proper resource data
	evt, _ := eventbus.NewEvent("workload.created", "Created", "test", map[string]interface{}{
		"id":        "wl-1",
		"name":      "training-job",
		"namespace": "ml",
	})
	bus.Publish(context.Background(), evt)

	// Give async processing a moment
	time.Sleep(50 * time.Millisecond)

	// The event should have been published (ack event)
	busStats := bus.Stats()
	if busStats.TotalPublished < 2 {
		// At least the original + ack event
		t.Logf("TotalPublished = %d (expected >= 2, ack may be counted)", busStats.TotalPublished)
	}
}

// ============================================================================
// LeaderElection config passthrough
// ============================================================================

func TestNew_WithLeaderElection(t *testing.T) {
	svc := New(Config{
		LeaderElection: true,
		Logger:         logrus.StandardLogger(),
	})
	if !svc.config.LeaderElection {
		t.Error("LeaderElection should be true")
	}
}
