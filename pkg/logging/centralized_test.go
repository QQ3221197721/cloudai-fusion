package logging

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func newTestCentralizedManager() *CentralizedManager {
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)
	return NewCentralizedManager(CentralizedConfig{
		DefaultBackend:   BackendLoki,
		DefaultRetention: 90,
		ArchivalEnabled:  true,
		Logger:           logger,
	})
}

func TestNewCentralizedManager(t *testing.T) {
	m := newTestCentralizedManager()
	if m == nil {
		t.Fatal("NewCentralizedManager returned nil")
	}
	if m.config.DefaultBackend != BackendLoki {
		t.Errorf("backend = %s, want loki", m.config.DefaultBackend)
	}
	if m.config.DefaultRetention != 90 {
		t.Errorf("retention = %d, want 90", m.config.DefaultRetention)
	}
}

func TestNewCentralizedManagerDefaults(t *testing.T) {
	m := NewCentralizedManager(CentralizedConfig{})
	if m.config.DefaultBackend != BackendLoki {
		t.Errorf("default backend = %s, want loki", m.config.DefaultBackend)
	}
	if m.config.DefaultRetention != 90 {
		t.Errorf("default retention = %d, want 90", m.config.DefaultRetention)
	}
}

// ============================================================================
// Source Tests
// ============================================================================

func TestSourceManagement(t *testing.T) {
	m := newTestCentralizedManager()

	source := &LogSource{
		Name: "api-server", Type: "container", Namespace: "default",
		Enabled: true, ParseFormat: "json",
	}
	if err := m.AddSource(source); err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	if source.ID == "" {
		t.Error("source ID should be auto-generated")
	}
	if source.Backend != BackendLoki {
		t.Errorf("backend = %s, want loki (default)", source.Backend)
	}

	s, err := m.GetSource(source.ID)
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	if s.Name != "api-server" {
		t.Errorf("name = %s, want api-server", s.Name)
	}

	sources := m.ListSources()
	if len(sources) != 1 {
		t.Errorf("sources = %d, want 1", len(sources))
	}
}

func TestSourceWithCustomBackend(t *testing.T) {
	m := newTestCentralizedManager()

	m.AddSource(&LogSource{Name: "s1", Backend: BackendElasticsearch, Enabled: true})
	m.AddSource(&LogSource{Name: "s2", Backend: BackendLoki, Enabled: true})
	m.AddSource(&LogSource{Name: "s3", Backend: BackendElasticsearch, Enabled: true})

	esSources := m.ListSourcesByBackend(BackendElasticsearch)
	if len(esSources) != 2 {
		t.Errorf("ES sources = %d, want 2", len(esSources))
	}

	lokiSources := m.ListSourcesByBackend(BackendLoki)
	if len(lokiSources) != 1 {
		t.Errorf("Loki sources = %d, want 1", len(lokiSources))
	}
}

func TestRemoveSource(t *testing.T) {
	m := newTestCentralizedManager()

	source := &LogSource{Name: "temp", Enabled: true}
	m.AddSource(source)

	if err := m.RemoveSource(source.ID); err != nil {
		t.Fatalf("RemoveSource: %v", err)
	}
	if len(m.ListSources()) != 0 {
		t.Error("source should be removed")
	}

	// Remove nonexistent
	if err := m.RemoveSource("nonexistent"); err == nil {
		t.Error("expected error for nonexistent source")
	}
}

// ============================================================================
// Archival Policy Tests
// ============================================================================

func TestArchivalPolicyLifecycle(t *testing.T) {
	m := newTestCentralizedManager()

	policy := &ArchivalPolicy{
		Name:          "standard",
		Enabled:       true,
		SourceBackend: BackendLoki,
		MinLevel:      LogLevelInfo,
		RetentionDays: 365,
		Tiers: []TierConfig{
			{Tier: TierWarm, AfterDays: 7, StorageClass: "STANDARD_IA", CompressionOn: true},
			{Tier: TierCold, AfterDays: 30, StorageClass: "GLACIER", CompressionOn: true},
			{Tier: TierFrozen, AfterDays: 90, StorageClass: "DEEP_ARCHIVE", CompressionOn: true},
		},
	}

	if err := m.CreateArchivalPolicy(policy); err != nil {
		t.Fatalf("CreateArchivalPolicy: %v", err)
	}
	if policy.ID == "" {
		t.Error("policy ID should be generated")
	}

	p, err := m.GetArchivalPolicy(policy.ID)
	if err != nil {
		t.Fatalf("GetArchivalPolicy: %v", err)
	}
	if p.Name != "standard" {
		t.Errorf("name = %s, want standard", p.Name)
	}

	policies := m.ListArchivalPolicies()
	if len(policies) != 1 {
		t.Errorf("policies = %d, want 1", len(policies))
	}
}

func TestArchivalPolicyInvalidTiers(t *testing.T) {
	m := newTestCentralizedManager()

	// Non-monotonic tiers should fail
	policy := &ArchivalPolicy{
		Name: "bad", Enabled: true, RetentionDays: 30,
		Tiers: []TierConfig{
			{Tier: TierWarm, AfterDays: 30},
			{Tier: TierCold, AfterDays: 7}, // Less than previous -> invalid
		},
	}
	if err := m.CreateArchivalPolicy(policy); err == nil {
		t.Error("expected error for non-monotonic tier transitions")
	}
}

func TestEnableArchivalPolicy(t *testing.T) {
	m := newTestCentralizedManager()
	policy := &ArchivalPolicy{Name: "test", Enabled: true, RetentionDays: 30}
	m.CreateArchivalPolicy(policy)

	m.EnableArchivalPolicy(policy.ID, false)
	p, _ := m.GetArchivalPolicy(policy.ID)
	if p.Enabled {
		t.Error("policy should be disabled")
	}

	// Nonexistent
	if err := m.EnableArchivalPolicy("nonexistent", true); err == nil {
		t.Error("expected error for nonexistent policy")
	}
}

func TestEvaluateArchivalPolicy(t *testing.T) {
	m := newTestCentralizedManager()

	policy := &ArchivalPolicy{
		Name: "test", Enabled: true, RetentionDays: 365,
		Tiers: []TierConfig{
			{Tier: TierWarm, AfterDays: 7},
			{Tier: TierCold, AfterDays: 30},
			{Tier: TierFrozen, AfterDays: 90},
		},
	}
	m.CreateArchivalPolicy(policy)

	tests := []struct {
		ageDays  int
		expected ArchivalTier
	}{
		{0, TierHot},
		{3, TierHot},
		{7, TierWarm},
		{15, TierWarm},
		{30, TierCold},
		{60, TierCold},
		{90, TierFrozen},
		{200, TierFrozen},
		{365, TierDeleted},
		{500, TierDeleted},
	}

	for _, tt := range tests {
		tier, err := m.EvaluateArchivalPolicy(policy.ID, tt.ageDays)
		if err != nil {
			t.Fatalf("EvaluateArchivalPolicy(%d): %v", tt.ageDays, err)
		}
		if tier != tt.expected {
			t.Errorf("age=%d days: tier=%s, want %s", tt.ageDays, tier, tt.expected)
		}
	}

	// Nonexistent policy
	_, err := m.EvaluateArchivalPolicy("nonexistent", 10)
	if err == nil {
		t.Error("expected error for nonexistent policy")
	}
}

// ============================================================================
// ILM Tests
// ============================================================================

func TestILMPolicy(t *testing.T) {
	m := newTestCentralizedManager()

	if m.GetILMPolicy() != nil {
		t.Error("ILM should be nil initially")
	}

	policy := &IndexLifecyclePolicy{
		Name: "test-ilm", HotMaxAgeDays: 7, WarmMaxAgeDays: 30,
		ColdMaxAgeDays: 90, DeleteAfterDays: 365, HotMaxSizeGB: 50,
	}
	m.SetILMPolicy(policy)

	p := m.GetILMPolicy()
	if p.Name != "test-ilm" {
		t.Errorf("ILM name = %s, want test-ilm", p.Name)
	}
}

func TestEvaluateILM(t *testing.T) {
	m := newTestCentralizedManager()
	m.SetILMPolicy(&IndexLifecyclePolicy{
		Name: "test", HotMaxAgeDays: 7, WarmMaxAgeDays: 30,
		ColdMaxAgeDays: 90, DeleteAfterDays: 365,
	})

	now := time.Now().UTC()

	// Fresh index -> should stay hot
	m.RegisterIndex(&ElasticsearchIndex{
		Name: "idx-fresh", Tier: TierHot, CreatedAt: now.Add(-1 * 24 * time.Hour),
	})
	// 15 day old hot index -> should move to warm
	m.RegisterIndex(&ElasticsearchIndex{
		Name: "idx-old-hot", Tier: TierHot, CreatedAt: now.Add(-15 * 24 * time.Hour),
	})
	// 60 day old warm index -> should move to cold
	m.RegisterIndex(&ElasticsearchIndex{
		Name: "idx-old-warm", Tier: TierWarm, CreatedAt: now.Add(-60 * 24 * time.Hour),
	})

	actions := m.EvaluateILM()
	if len(actions) != 2 {
		t.Errorf("ILM actions = %d, want 2 (fresh stays, 2 need transitions)", len(actions))
	}

	// Check action details
	for _, a := range actions {
		name := a["index"].(string)
		target := a["target_tier"].(ArchivalTier)
		if name == "idx-old-hot" && target != TierWarm {
			t.Errorf("idx-old-hot target = %s, want warm", target)
		}
		if name == "idx-old-warm" && target != TierCold {
			t.Errorf("idx-old-warm target = %s, want cold", target)
		}
	}
}

func TestEvaluateILMNoPolicy(t *testing.T) {
	m := newTestCentralizedManager()
	actions := m.EvaluateILM()
	if actions != nil {
		t.Error("expected nil with no ILM policy")
	}
}

// ============================================================================
// Ingestion Stats Tests
// ============================================================================

func TestIngestionStats(t *testing.T) {
	m := newTestCentralizedManager()

	source := &LogSource{Name: "test-src", Enabled: true}
	m.AddSource(source)

	m.RecordIngestion(source.ID, 1024, 10)
	m.RecordIngestion(source.ID, 2048, 20)

	stats, err := m.GetStats(source.ID)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.BytesIngested != 3072 {
		t.Errorf("bytes = %d, want 3072", stats.BytesIngested)
	}
	if stats.LinesIngested != 30 {
		t.Errorf("lines = %d, want 30", stats.LinesIngested)
	}

	m.RecordError(source.ID)
	m.RecordError(source.ID)
	stats, _ = m.GetStats(source.ID)
	if stats.ErrorCount != 2 {
		t.Errorf("errors = %d, want 2", stats.ErrorCount)
	}
}

func TestIngestionStatsNotFound(t *testing.T) {
	m := newTestCentralizedManager()

	if err := m.RecordIngestion("nonexistent", 100, 1); err == nil {
		t.Error("expected error for nonexistent source")
	}
	if err := m.RecordError("nonexistent"); err == nil {
		t.Error("expected error for nonexistent source")
	}
}

func TestGetAllStats(t *testing.T) {
	m := newTestCentralizedManager()

	s1 := &LogSource{Name: "big", Enabled: true}
	s2 := &LogSource{Name: "small", Enabled: true}
	m.AddSource(s1)
	m.AddSource(s2)

	m.RecordIngestion(s1.ID, 10000, 100)
	m.RecordIngestion(s2.ID, 500, 5)

	allStats := m.GetAllStats()
	if len(allStats) != 2 {
		t.Fatalf("stats = %d, want 2", len(allStats))
	}
	// Should be sorted by bytes descending
	if allStats[0].BytesIngested < allStats[1].BytesIngested {
		t.Error("stats should be sorted by bytes descending")
	}
}

// ============================================================================
// Summary Tests
// ============================================================================

func TestGetSummary(t *testing.T) {
	m := newTestCentralizedManager()

	m.AddSource(&LogSource{Name: "s1", Backend: BackendLoki, Enabled: true})
	m.AddSource(&LogSource{Name: "s2", Backend: BackendElasticsearch, Enabled: false})

	m.CreateArchivalPolicy(&ArchivalPolicy{Name: "p1", Enabled: true, RetentionDays: 90})
	m.SetILMPolicy(&IndexLifecyclePolicy{Name: "ilm"})

	summary := m.GetSummary()
	if summary["total_sources"].(int) != 2 {
		t.Errorf("total_sources = %v, want 2", summary["total_sources"])
	}
	if summary["enabled_sources"].(int) != 1 {
		t.Errorf("enabled_sources = %v, want 1", summary["enabled_sources"])
	}
	if summary["total_policies"].(int) != 1 {
		t.Errorf("total_policies = %v, want 1", summary["total_policies"])
	}
	if summary["has_ilm"].(bool) != true {
		t.Error("has_ilm should be true")
	}
	if summary["default_backend"].(LogBackend) != BackendLoki {
		t.Errorf("default_backend = %v, want loki", summary["default_backend"])
	}
}

// ============================================================================
// Default Policy/ILM Generation
// ============================================================================

func TestGenerateDefaultPolicies(t *testing.T) {
	m := newTestCentralizedManager()

	policies := m.GenerateDefaultPolicies()
	if len(policies) != 3 {
		t.Errorf("default policies = %d, want 3", len(policies))
	}

	// Standard should have 3 tiers
	if len(policies[0].Tiers) != 3 {
		t.Errorf("standard tiers = %d, want 3", len(policies[0].Tiers))
	}
	// Security should have 3 year retention
	if policies[1].RetentionDays != 1095 {
		t.Errorf("security retention = %d, want 1095", policies[1].RetentionDays)
	}
	// Debug should be 7 days
	if policies[2].RetentionDays != 7 {
		t.Errorf("debug retention = %d, want 7", policies[2].RetentionDays)
	}

	// Should all be stored
	stored := m.ListArchivalPolicies()
	if len(stored) != 3 {
		t.Errorf("stored policies = %d, want 3", len(stored))
	}
}

func TestGenerateDefaultILM(t *testing.T) {
	m := newTestCentralizedManager()
	policy := m.GenerateDefaultILM()

	if policy.Name != "cloudai-fusion-ilm" {
		t.Errorf("name = %s, want cloudai-fusion-ilm", policy.Name)
	}
	if policy.HotMaxAgeDays != 7 {
		t.Errorf("hot_max_age = %d, want 7", policy.HotMaxAgeDays)
	}
	if policy.DeleteAfterDays != 365 {
		t.Errorf("delete_after = %d, want 365", policy.DeleteAfterDays)
	}

	// Should be stored
	if m.GetILMPolicy() == nil {
		t.Error("ILM policy should be stored")
	}
}
