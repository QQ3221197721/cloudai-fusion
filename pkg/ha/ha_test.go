package ha

import (
	"context"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func newTestManager() *Manager {
	return NewManager(ManagerConfig{
		PostgreSQL: DefaultPostgreSQLHAConfig(),
		Redis:      DefaultRedisSentinelConfig(),
		CrossAZ:    DefaultCrossAZConfig(),
		Logger:     logrus.StandardLogger(),
	})
}

// ============================================================================
// Default Config Tests
// ============================================================================

func TestDefaultPostgreSQLHAConfig(t *testing.T) {
	cfg := DefaultPostgreSQLHAConfig()
	if cfg.Mode != PGModeReplication {
		t.Errorf("expected mode replication, got %s", cfg.Mode)
	}
	if len(cfg.ReadReplicaHosts) != 2 {
		t.Errorf("expected 2 read replicas, got %d", len(cfg.ReadReplicaHosts))
	}
	if cfg.MaxOpenConns != 100 {
		t.Errorf("expected max_open_conns=100, got %d", cfg.MaxOpenConns)
	}
	if !cfg.AutoFailover {
		t.Error("expected auto_failover=true")
	}
	if cfg.MaxConnections != 500 {
		t.Errorf("expected max_connections=500, got %d", cfg.MaxConnections)
	}
}

func TestDefaultRedisSentinelConfig(t *testing.T) {
	cfg := DefaultRedisSentinelConfig()
	if !cfg.Enabled {
		t.Error("expected sentinel enabled")
	}
	if cfg.MasterName != "cloudai-master" {
		t.Errorf("expected master cloudai-master, got %s", cfg.MasterName)
	}
	if len(cfg.SentinelAddrs) != 3 {
		t.Errorf("expected 3 sentinel addrs, got %d", len(cfg.SentinelAddrs))
	}
	if cfg.Quorum != 2 {
		t.Errorf("expected quorum=2, got %d", cfg.Quorum)
	}
	if cfg.PoolSize != 50 {
		t.Errorf("expected pool_size=50, got %d", cfg.PoolSize)
	}
}

func TestDefaultCrossAZConfig(t *testing.T) {
	cfg := DefaultCrossAZConfig()
	if !cfg.Enabled {
		t.Error("expected cross-AZ enabled")
	}
	if cfg.Strategy != AZStrategySpread {
		t.Errorf("expected spread strategy, got %s", cfg.Strategy)
	}
	if len(cfg.AvailabilityZones) != 3 {
		t.Errorf("expected 3 AZs, got %d", len(cfg.AvailabilityZones))
	}
	if cfg.TopologyKey != "topology.kubernetes.io/zone" {
		t.Errorf("unexpected topology key: %s", cfg.TopologyKey)
	}
}

// ============================================================================
// Manager Tests
// ============================================================================

func TestNewManager(t *testing.T) {
	m := newTestManager()
	if m == nil {
		t.Fatal("expected non-nil manager")
	}

	if _, ok := m.components["postgresql"]; !ok {
		t.Error("expected postgresql component")
	}
	if _, ok := m.components["redis"]; !ok {
		t.Error("expected redis component")
	}
}

func TestGetComponentHealth(t *testing.T) {
	m := newTestManager()

	h, err := m.GetComponentHealth("postgresql")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.Name != "postgresql" {
		t.Errorf("expected name postgresql, got %s", h.Name)
	}
	if h.Status != HealthStatusUnknown {
		t.Errorf("expected unknown status initially, got %s", h.Status)
	}
}

func TestGetComponentHealthNotFound(t *testing.T) {
	m := newTestManager()

	_, err := m.GetComponentHealth("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent component")
	}
}

func TestGetAllHealth(t *testing.T) {
	m := newTestManager()
	all := m.GetAllHealth()

	if len(all) != 2 {
		t.Errorf("expected 2 components, got %d", len(all))
	}
}

func TestGetOverallStatus(t *testing.T) {
	m := newTestManager()

	// Initially unknown
	status := m.GetOverallStatus()
	if status != HealthStatusUnknown {
		t.Errorf("expected unknown, got %s", status)
	}

	// Set all healthy
	m.mu.Lock()
	m.components["postgresql"].Status = HealthStatusHealthy
	m.components["redis"].Status = HealthStatusHealthy
	m.mu.Unlock()

	status = m.GetOverallStatus()
	if status != HealthStatusHealthy {
		t.Errorf("expected healthy, got %s", status)
	}

	// Set one degraded
	m.mu.Lock()
	m.components["redis"].Status = HealthStatusDegraded
	m.mu.Unlock()

	status = m.GetOverallStatus()
	if status != HealthStatusDegraded {
		t.Errorf("expected degraded, got %s", status)
	}

	// Set one down
	m.mu.Lock()
	m.components["postgresql"].Status = HealthStatusDown
	m.mu.Unlock()

	status = m.GetOverallStatus()
	if status != HealthStatusDown {
		t.Errorf("expected down, got %s", status)
	}
}

// ============================================================================
// Health Check Tests
// ============================================================================

func TestCheckPostgreSQLHealth(t *testing.T) {
	m := newTestManager()
	ctx := context.Background()

	h := m.CheckPostgreSQLHealth(ctx)
	if h.Status != HealthStatusHealthy {
		t.Errorf("expected healthy, got %s", h.Status)
	}
	if h.Primary == nil {
		t.Fatal("expected primary endpoint")
	}
	if !h.Primary.Reachable {
		t.Error("expected primary reachable")
	}
	if h.Primary.Role != "primary" {
		t.Errorf("expected role primary, got %s", h.Primary.Role)
	}
	if len(h.Replicas) != 2 {
		t.Errorf("expected 2 replicas, got %d", len(h.Replicas))
	}
	for _, r := range h.Replicas {
		if r.Role != "replica" {
			t.Errorf("expected role replica, got %s", r.Role)
		}
	}
}

func TestCheckRedisHealth(t *testing.T) {
	m := newTestManager()
	ctx := context.Background()

	h := m.CheckRedisHealth(ctx)
	if h.Status != HealthStatusHealthy {
		t.Errorf("expected healthy, got %s", h.Status)
	}
	if h.Primary == nil {
		t.Fatal("expected master endpoint")
	}
	if h.Primary.Role != "master" {
		t.Errorf("expected role master, got %s", h.Primary.Role)
	}
	if len(h.Replicas) != 3 {
		t.Errorf("expected 3 sentinels, got %d", len(h.Replicas))
	}
}

func TestCheckHealthCancelledContext(t *testing.T) {
	m := newTestManager()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	h := m.CheckPostgreSQLHealth(ctx)
	if h.Status != HealthStatusUnknown {
		t.Errorf("expected unknown on cancelled ctx, got %s", h.Status)
	}

	h2 := m.CheckRedisHealth(ctx)
	if h2.Status != HealthStatusUnknown {
		t.Errorf("expected unknown on cancelled ctx, got %s", h2.Status)
	}
}

// ============================================================================
// Failover Tests
// ============================================================================

func TestTriggerFailover(t *testing.T) {
	m := newTestManager()
	ctx := context.Background()

	ev, err := m.TriggerFailover(ctx, "postgresql", "manual test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Component != "postgresql" {
		t.Errorf("expected postgresql, got %s", ev.Component)
	}
	if !ev.Success {
		t.Error("expected success=true")
	}
	if ev.FromEndpoint == "" {
		t.Error("expected non-empty from_endpoint")
	}
	if ev.Reason != "manual test" {
		t.Errorf("expected reason 'manual test', got %s", ev.Reason)
	}

	// Check component status updated
	h, _ := m.GetComponentHealth("postgresql")
	if h.Status != HealthStatusDegraded {
		t.Errorf("expected degraded after failover, got %s", h.Status)
	}
}

func TestTriggerFailoverRedis(t *testing.T) {
	m := newTestManager()
	ctx := context.Background()

	ev, err := m.TriggerFailover(ctx, "redis", "sentinel failover")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Component != "redis" {
		t.Errorf("expected redis, got %s", ev.Component)
	}
	if !ev.Success {
		t.Error("expected success")
	}
}

func TestTriggerFailoverUnknownComponent(t *testing.T) {
	m := newTestManager()
	ctx := context.Background()

	_, err := m.TriggerFailover(ctx, "kafka", "test")
	if err == nil {
		t.Error("expected error for unsupported component")
	}
}

func TestTriggerFailoverCancelledContext(t *testing.T) {
	m := newTestManager()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := m.TriggerFailover(ctx, "postgresql", "test")
	if err == nil {
		t.Error("expected error on cancelled context")
	}
}

func TestGetFailoverHistory(t *testing.T) {
	m := newTestManager()
	ctx := context.Background()

	m.TriggerFailover(ctx, "postgresql", "test 1")
	m.TriggerFailover(ctx, "redis", "test 2")

	history := m.GetFailoverHistory()
	if len(history) != 2 {
		t.Errorf("expected 2 events, got %d", len(history))
	}
}

// ============================================================================
// Connection Config Tests
// ============================================================================

func TestGetConnectionConfig(t *testing.T) {
	m := newTestManager()
	cc := m.GetConnectionConfig()

	if cc.PostgreSQL.PrimaryDSN == "" {
		t.Error("expected non-empty primary DSN")
	}
	if len(cc.PostgreSQL.ReplicaDSNs) != 2 {
		t.Errorf("expected 2 replica DSNs, got %d", len(cc.PostgreSQL.ReplicaDSNs))
	}
	if cc.PostgreSQL.MaxOpenConns != 100 {
		t.Errorf("expected max_open_conns=100, got %d", cc.PostgreSQL.MaxOpenConns)
	}
	if cc.Redis.MasterName != "cloudai-master" {
		t.Errorf("expected master name cloudai-master, got %s", cc.Redis.MasterName)
	}
	if len(cc.Redis.SentinelAddrs) != 3 {
		t.Errorf("expected 3 sentinel addrs, got %d", len(cc.Redis.SentinelAddrs))
	}
}

// ============================================================================
// Validation Tests
// ============================================================================

func TestValidateConfigValid(t *testing.T) {
	cfg := ManagerConfig{
		PostgreSQL: DefaultPostgreSQLHAConfig(),
		Redis:      DefaultRedisSentinelConfig(),
		CrossAZ:    DefaultCrossAZConfig(),
	}
	issues := ValidateConfig(cfg)
	if len(issues) != 0 {
		t.Errorf("expected no issues for default config, got: %v", issues)
	}
}

func TestValidateConfigNoReplicas(t *testing.T) {
	cfg := ManagerConfig{
		PostgreSQL: PostgreSQLHAConfig{
			Mode:           PGModeReplication,
			MaxOpenConns:   100,
		},
		Redis: DefaultRedisSentinelConfig(),
		CrossAZ: DefaultCrossAZConfig(),
	}
	issues := ValidateConfig(cfg)
	found := false
	for _, issue := range issues {
		if issue == "PostgreSQL replication mode requires at least one read replica" {
			found = true
		}
	}
	if !found {
		t.Error("expected replication replica issue")
	}
}

func TestValidateConfigLowConns(t *testing.T) {
	cfg := ManagerConfig{
		PostgreSQL: PostgreSQLHAConfig{
			Mode:         PGModeStandalone,
			MaxOpenConns: 5,
		},
		Redis:   RedisSentinelConfig{Enabled: false},
		CrossAZ: CrossAZConfig{Enabled: false},
	}
	issues := ValidateConfig(cfg)
	found := false
	for _, issue := range issues {
		if issue == "PostgreSQL max_open_conns should be at least 10 for production" {
			found = true
		}
	}
	if !found {
		t.Error("expected low max_open_conns issue")
	}
}

func TestValidateConfigSentinelQuorum(t *testing.T) {
	cfg := ManagerConfig{
		PostgreSQL: PostgreSQLHAConfig{Mode: PGModeStandalone, MaxOpenConns: 50},
		Redis: RedisSentinelConfig{
			Enabled:       true,
			SentinelAddrs: []string{"s1", "s2"},
			Quorum:        1,
		},
		CrossAZ: CrossAZConfig{Enabled: false},
	}
	issues := ValidateConfig(cfg)
	if len(issues) < 2 {
		t.Errorf("expected at least 2 issues (sentinel count + quorum), got %d: %v", len(issues), issues)
	}
}

func TestValidateConfigCrossAZ(t *testing.T) {
	cfg := ManagerConfig{
		PostgreSQL: PostgreSQLHAConfig{Mode: PGModeStandalone, MaxOpenConns: 50},
		Redis:      RedisSentinelConfig{Enabled: false},
		CrossAZ: CrossAZConfig{
			Enabled:           true,
			AvailabilityZones: []string{"az-1"},
			MinAZsForHA:       2,
		},
	}
	issues := ValidateConfig(cfg)
	found := false
	for _, issue := range issues {
		if len(issue) > 0 {
			found = true
		}
	}
	if !found {
		t.Error("expected cross-AZ issue")
	}
}

// ============================================================================
// Start/Stop Tests
// ============================================================================

func TestStartStop(t *testing.T) {
	m := newTestManager()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	m.Start(ctx)
	time.Sleep(100 * time.Millisecond)
	m.Stop()
}
