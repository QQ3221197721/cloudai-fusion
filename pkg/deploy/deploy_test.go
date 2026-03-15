package deploy

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Mock Implementations
// ============================================================================

type mockMetrics struct {
	errorRate  float64
	p99Latency float64
	replicas   int
	errOnCall  error
}

func (m *mockMetrics) GetErrorRate(_ context.Context, _, _ string) (float64, error) {
	return m.errorRate, m.errOnCall
}
func (m *mockMetrics) GetP99Latency(_ context.Context, _, _ string) (float64, error) {
	return m.p99Latency, m.errOnCall
}
func (m *mockMetrics) GetReadyReplicas(_ context.Context, _, _ string) (int, error) {
	return m.replicas, m.errOnCall
}

type mockExecutor struct {
	currentImage  string
	canaryImage   string
	trafficWeight int
	promoted      bool
	rolledBack    bool
	errOnCall     error
}

func (m *mockExecutor) DeployCanary(_ context.Context, _, image string, _ int) error {
	m.canaryImage = image
	return m.errOnCall
}
func (m *mockExecutor) SetTrafficWeight(_ context.Context, _ string, weight int) error {
	m.trafficWeight = weight
	return m.errOnCall
}
func (m *mockExecutor) PromoteCanary(_ context.Context, _ string) error {
	m.promoted = true
	return m.errOnCall
}
func (m *mockExecutor) RollbackCanary(_ context.Context, _ string) error {
	m.rolledBack = true
	return m.errOnCall
}
func (m *mockExecutor) GetCurrentImage(_ context.Context, _ string) (string, error) {
	return m.currentImage, m.errOnCall
}

// ============================================================================
// Default Config Tests
// ============================================================================

func TestDefaultCanaryConfig(t *testing.T) {
	cfg := DefaultCanaryConfig()
	if cfg.InitialWeight != 5 {
		t.Errorf("expected InitialWeight=5, got %d", cfg.InitialWeight)
	}
	if len(cfg.ProgressionSteps) != 5 {
		t.Errorf("expected 5 progression steps, got %d", len(cfg.ProgressionSteps))
	}
	if !cfg.AutoPromote {
		t.Error("expected AutoPromote=true")
	}
	if !cfg.AutoRollback {
		t.Error("expected AutoRollback=true")
	}
}

func TestDefaultRollbackConfig(t *testing.T) {
	cfg := DefaultRollbackConfig()
	if cfg.ConsecutiveFailures != 3 {
		t.Errorf("expected ConsecutiveFailures=3, got %d", cfg.ConsecutiveFailures)
	}
	if cfg.MaxRevisionHistory != 10 {
		t.Errorf("expected MaxRevisionHistory=10, got %d", cfg.MaxRevisionHistory)
	}
}

// ============================================================================
// Canary Controller Tests
// ============================================================================

func TestNewCanaryController(t *testing.T) {
	cc := NewCanaryController(DefaultCanaryConfig(), &mockMetrics{}, &mockExecutor{currentImage: "v1"}, nil)
	if cc == nil {
		t.Fatal("controller should not be nil")
	}
}

func TestCanaryController_StartCanary(t *testing.T) {
	exec := &mockExecutor{currentImage: "app:v1"}
	cc := NewCanaryController(DefaultCanaryConfig(), &mockMetrics{}, exec, nil)

	status, err := cc.StartCanary(context.Background(), "apiserver", "app:v2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Phase != PhaseCanary {
		t.Errorf("expected phase canary, got %s", status.Phase)
	}
	if status.StableVersion != "app:v1" {
		t.Errorf("expected stable v1, got %s", status.StableVersion)
	}
	if status.CanaryVersion != "app:v2" {
		t.Errorf("expected canary v2, got %s", status.CanaryVersion)
	}
	if status.CanaryWeight != 5 {
		t.Errorf("expected initial weight 5, got %d", status.CanaryWeight)
	}
	if exec.canaryImage != "app:v2" {
		t.Error("executor should have deployed canary image")
	}
}

func TestCanaryController_StartCanary_AlreadyInProgress(t *testing.T) {
	exec := &mockExecutor{currentImage: "app:v1"}
	cc := NewCanaryController(DefaultCanaryConfig(), &mockMetrics{}, exec, nil)

	cc.StartCanary(context.Background(), "apiserver", "app:v2")
	_, err := cc.StartCanary(context.Background(), "apiserver", "app:v3")
	if err == nil {
		t.Error("expected error for duplicate canary")
	}
}

func TestCanaryController_StartCanary_ExecutorError(t *testing.T) {
	exec := &mockExecutor{currentImage: "app:v1", errOnCall: fmt.Errorf("deploy failed")}
	cc := NewCanaryController(DefaultCanaryConfig(), &mockMetrics{}, exec, nil)
	_, err := cc.StartCanary(context.Background(), "apiserver", "app:v2")
	if err == nil {
		t.Error("expected error when executor fails")
	}
}

func TestCanaryController_EvaluateCanary_NoActive(t *testing.T) {
	cc := NewCanaryController(DefaultCanaryConfig(), &mockMetrics{}, &mockExecutor{}, nil)
	_, err := cc.EvaluateCanary(context.Background(), "apiserver")
	if err == nil {
		t.Error("expected error when no active canary")
	}
}

func TestCanaryController_EvaluateCanary_Healthy(t *testing.T) {
	metrics := &mockMetrics{errorRate: 0.1, p99Latency: 50}
	exec := &mockExecutor{currentImage: "app:v1"}
	cc := NewCanaryController(DefaultCanaryConfig(), metrics, exec, nil)

	cc.StartCanary(context.Background(), "apiserver", "app:v2")
	status, err := cc.EvaluateCanary(context.Background(), "apiserver")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Health != HealthHealthy {
		t.Errorf("expected healthy, got %s", status.Health)
	}
	// AutoPromote should have increased weight
	if status.CanaryWeight <= 5 {
		t.Errorf("expected weight increase from 5, got %d", status.CanaryWeight)
	}
}

func TestCanaryController_EvaluateCanary_HighErrorRate_Rollback(t *testing.T) {
	metrics := &mockMetrics{errorRate: 5.0, p99Latency: 50}
	exec := &mockExecutor{currentImage: "app:v1"}
	cc := NewCanaryController(DefaultCanaryConfig(), metrics, exec, nil)

	cc.StartCanary(context.Background(), "apiserver", "app:v2")
	status, err := cc.EvaluateCanary(context.Background(), "apiserver")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Phase != PhaseRolledBack {
		t.Errorf("expected rolled_back phase, got %s", status.Phase)
	}
	if !exec.rolledBack {
		t.Error("executor should have performed rollback")
	}
}

func TestCanaryController_EvaluateCanary_HighLatency_Rollback(t *testing.T) {
	metrics := &mockMetrics{errorRate: 0.1, p99Latency: 1000}
	exec := &mockExecutor{currentImage: "app:v1"}
	cc := NewCanaryController(DefaultCanaryConfig(), metrics, exec, nil)

	cc.StartCanary(context.Background(), "apiserver", "app:v2")
	status, err := cc.EvaluateCanary(context.Background(), "apiserver")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.Phase != PhaseRolledBack {
		t.Errorf("expected rolled_back, got %s", status.Phase)
	}
}

func TestCanaryController_FullPromotion(t *testing.T) {
	metrics := &mockMetrics{errorRate: 0.1, p99Latency: 20}
	exec := &mockExecutor{currentImage: "app:v1"}
	cc := NewCanaryController(DefaultCanaryConfig(), metrics, exec, nil)

	cc.StartCanary(context.Background(), "apiserver", "app:v2")

	// Evaluate multiple times to progress through all steps
	var status *DeploymentStatus
	for i := 0; i < 10; i++ {
		var err error
		status, err = cc.EvaluateCanary(context.Background(), "apiserver")
		if err != nil {
			t.Fatalf("evaluation %d error: %v", i, err)
		}
		if status.Phase == PhaseComplete {
			break
		}
	}

	if status.Phase != PhaseComplete {
		t.Errorf("expected complete after full progression, got %s", status.Phase)
	}
	if exec.promoted != true {
		t.Error("executor should have promoted canary")
	}
	if status.StableVersion != "app:v2" {
		t.Errorf("stable version should be v2 after promotion, got %s", status.StableVersion)
	}
}

func TestCanaryController_GetStatus(t *testing.T) {
	exec := &mockExecutor{currentImage: "app:v1"}
	cc := NewCanaryController(DefaultCanaryConfig(), &mockMetrics{}, exec, nil)

	_, ok := cc.GetStatus("apiserver")
	if ok {
		t.Error("should not find status before deployment")
	}

	cc.StartCanary(context.Background(), "apiserver", "app:v2")
	status, ok := cc.GetStatus("apiserver")
	if !ok || status == nil {
		t.Error("should find status after deployment")
	}
}

func TestCanaryController_ListActive(t *testing.T) {
	exec := &mockExecutor{currentImage: "app:v1"}
	cc := NewCanaryController(DefaultCanaryConfig(), &mockMetrics{}, exec, nil)

	cc.StartCanary(context.Background(), "apiserver", "app:v2")
	cc.StartCanary(context.Background(), "scheduler", "sched:v2")

	active := cc.ListActive()
	if len(active) != 2 {
		t.Errorf("expected 2 active deployments, got %d", len(active))
	}
}

// ============================================================================
// Rollback Controller Tests
// ============================================================================

func TestNewRollbackController(t *testing.T) {
	rc := NewRollbackController(DefaultRollbackConfig(), &mockMetrics{}, &mockExecutor{}, nil)
	if rc == nil {
		t.Fatal("controller should not be nil")
	}
}

func TestRollbackController_CheckHealth_Healthy(t *testing.T) {
	metrics := &mockMetrics{errorRate: 0.5, p99Latency: 100}
	rc := NewRollbackController(DefaultRollbackConfig(), metrics, &mockExecutor{}, nil)

	health, err := rc.CheckHealth(context.Background(), "apiserver")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *health != HealthHealthy {
		t.Errorf("expected healthy, got %s", *health)
	}
}

func TestRollbackController_CheckHealth_Degraded(t *testing.T) {
	metrics := &mockMetrics{errorRate: 10.0, p99Latency: 100}
	rc := NewRollbackController(DefaultRollbackConfig(), metrics, &mockExecutor{}, nil)

	health, _ := rc.CheckHealth(context.Background(), "apiserver")
	if *health != HealthDegraded {
		t.Errorf("expected degraded, got %s", *health)
	}
}

func TestRollbackController_CheckHealth_TriggersRollback(t *testing.T) {
	metrics := &mockMetrics{errorRate: 10.0, p99Latency: 2000}
	exec := &mockExecutor{}
	cfg := DefaultRollbackConfig()
	cfg.ConsecutiveFailures = 2
	rc := NewRollbackController(cfg, metrics, exec, nil)

	rc.CheckHealth(context.Background(), "apiserver")
	health, _ := rc.CheckHealth(context.Background(), "apiserver")
	if *health != HealthUnhealthy {
		t.Errorf("expected unhealthy after consecutive failures, got %s", *health)
	}
	if !exec.rolledBack {
		t.Error("should have triggered rollback")
	}
}

func TestRollbackController_CheckHealth_Cooldown(t *testing.T) {
	metrics := &mockMetrics{errorRate: 10.0}
	exec := &mockExecutor{}
	cfg := DefaultRollbackConfig()
	cfg.ConsecutiveFailures = 1
	cfg.CooldownPeriod = 1 * time.Hour
	rc := NewRollbackController(cfg, metrics, exec, nil)

	// First check triggers rollback
	rc.CheckHealth(context.Background(), "apiserver")
	// Second check should be in cooldown
	health, _ := rc.CheckHealth(context.Background(), "apiserver")
	if *health != HealthUnknown {
		t.Errorf("expected unknown during cooldown, got %s", *health)
	}
}

func TestRollbackController_CheckHealth_MetricsError(t *testing.T) {
	metrics := &mockMetrics{errOnCall: fmt.Errorf("metrics unavailable")}
	rc := NewRollbackController(DefaultRollbackConfig(), metrics, &mockExecutor{}, nil)

	health, err := rc.CheckHealth(context.Background(), "apiserver")
	if err != nil {
		t.Fatalf("should not return error on metrics failure, got: %v", err)
	}
	if *health != HealthUnknown {
		t.Errorf("expected unknown when metrics fail, got %s", *health)
	}
}

func TestRollbackController_GetEvents(t *testing.T) {
	metrics := &mockMetrics{errorRate: 10.0}
	cfg := DefaultRollbackConfig()
	cfg.ConsecutiveFailures = 1
	rc := NewRollbackController(cfg, metrics, &mockExecutor{}, nil)

	rc.CheckHealth(context.Background(), "apiserver")
	events := rc.GetEvents()
	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}
	if !events[0].Automatic {
		t.Error("event should be automatic")
	}
}

// ============================================================================
// Deployment Manager Tests
// ============================================================================

func TestNewDeploymentManager(t *testing.T) {
	cc := NewCanaryController(DefaultCanaryConfig(), &mockMetrics{}, &mockExecutor{currentImage: "v1"}, nil)
	rc := NewRollbackController(DefaultRollbackConfig(), &mockMetrics{}, &mockExecutor{}, nil)
	dm := NewDeploymentManager(cc, rc, logrus.StandardLogger())
	if dm == nil {
		t.Fatal("manager should not be nil")
	}
}

func TestDeploymentManager_ExecutePlan_Canary(t *testing.T) {
	exec := &mockExecutor{currentImage: "app:v1"}
	cc := NewCanaryController(DefaultCanaryConfig(), &mockMetrics{}, exec, nil)
	dm := NewDeploymentManager(cc, nil, logrus.StandardLogger())

	plan := &DeploymentPlan{
		ID:         "plan-1",
		Components: map[string]string{"apiserver": "app:v2"},
		Strategy:   "canary",
		CreatedAt:  time.Now(),
	}

	if err := dm.ExecutePlan(context.Background(), plan); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exec.canaryImage != "app:v2" {
		t.Error("canary image should be deployed")
	}
}

func TestDeploymentManager_ExecutePlan_UnknownStrategy(t *testing.T) {
	exec := &mockExecutor{currentImage: "app:v1"}
	cc := NewCanaryController(DefaultCanaryConfig(), &mockMetrics{}, exec, nil)
	dm := NewDeploymentManager(cc, nil, logrus.StandardLogger())

	plan := &DeploymentPlan{
		ID:         "plan-2",
		Components: map[string]string{"apiserver": "app:v3"},
		Strategy:   "blue-green",
	}
	// Falls back to canary
	if err := dm.ExecutePlan(context.Background(), plan); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeploymentManager_ExecutePlan_MultiComponent(t *testing.T) {
	exec := &mockExecutor{currentImage: "v1"}
	cc := NewCanaryController(DefaultCanaryConfig(), &mockMetrics{}, exec, nil)
	dm := NewDeploymentManager(cc, nil, logrus.StandardLogger())

	plan := &DeploymentPlan{
		ID: "plan-multi",
		Components: map[string]string{
			"apiserver": "api:v2",
			"scheduler": "sched:v2",
		},
		Strategy: "canary",
	}

	if err := dm.ExecutePlan(context.Background(), plan); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	active := cc.ListActive()
	if len(active) != 2 {
		t.Errorf("expected 2 active deployments, got %d", len(active))
	}
}

func TestComponentOrder(t *testing.T) {
	if len(ComponentOrder) != 4 {
		t.Errorf("expected 4 components in order, got %d", len(ComponentOrder))
	}
	if ComponentOrder[0] != "apiserver" {
		t.Errorf("first component should be apiserver, got %s", ComponentOrder[0])
	}
}
