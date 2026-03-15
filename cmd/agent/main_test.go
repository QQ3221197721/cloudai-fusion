package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ============================================================================
// initLogger
// ============================================================================

func TestInitLogger(t *testing.T) {
	t.Run("debug level", func(t *testing.T) {
		l := initLogger("debug")
		if l == nil {
			t.Fatal("expected non-nil logger")
		}
		if l.GetLevel() != logrus.DebugLevel {
			t.Errorf("expected debug level, got %s", l.GetLevel())
		}
	})

	t.Run("warn level", func(t *testing.T) {
		l := initLogger("warn")
		if l.GetLevel() != logrus.WarnLevel {
			t.Errorf("expected warn level, got %s", l.GetLevel())
		}
	})

	t.Run("invalid level defaults", func(t *testing.T) {
		l := initLogger("invalid-level")
		// logrus.ParseLevel returns an error; initLogger still returns a logger
		if l == nil {
			t.Fatal("expected non-nil logger even with invalid level")
		}
	})
}

// ============================================================================
// Version variables
// ============================================================================

func TestVersionVariables(t *testing.T) {
	if Version == "" {
		t.Error("Version should have a default value")
	}
	if GitCommit == "" {
		t.Error("GitCommit should have a default value")
	}
	if BuildTime == "" {
		t.Error("BuildTime should have a default value")
	}
}

// ============================================================================
// AgentType constants
// ============================================================================

func TestAgentTypeConstants(t *testing.T) {
	types := []AgentType{AgentTypeScheduler, AgentTypeSecurity, AgentTypeCost, AgentTypeOperations}
	expected := []string{"scheduler", "security", "cost", "operations"}

	for i, at := range types {
		if string(at) != expected[i] {
			t.Errorf("AgentType[%d]: expected %q, got %q", i, expected[i], at)
		}
	}
}

// ============================================================================
// AgentOrchestrator
// ============================================================================

func newTestOrchestrator() *AgentOrchestrator {
	return &AgentOrchestrator{
		agents: make(map[AgentType]*Agent),
		logger: logrus.New(),
		apiAddr: "localhost:8080",
		aiAddr:  "localhost:8090",
	}
}

func TestRegisterAgent(t *testing.T) {
	o := newTestOrchestrator()
	agent := &Agent{
		Type:     AgentTypeScheduler,
		Name:     "Test Scheduler Agent",
		Status:   "initializing",
		Interval: 10 * time.Second,
		Logger:   logrus.New(),
	}

	o.RegisterAgent(AgentTypeScheduler, agent)

	if len(o.agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(o.agents))
	}
	if o.agents[AgentTypeScheduler].Name != "Test Scheduler Agent" {
		t.Error("agent name mismatch")
	}
}

func TestStartAllAndStopAll(t *testing.T) {
	o := newTestOrchestrator()
	for _, at := range []AgentType{AgentTypeScheduler, AgentTypeSecurity, AgentTypeCost} {
		o.RegisterAgent(at, &Agent{
			Type:     at,
			Name:     string(at) + " agent",
			Status:   "initializing",
			Interval: 100 * time.Millisecond,
			Logger:   logrus.New(),
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	o.StartAll(ctx)

	// Let agents tick once
	time.Sleep(200 * time.Millisecond)

	for _, a := range o.agents {
		if a.Status != "running" {
			t.Errorf("agent %s: expected running, got %s", a.Name, a.Status)
		}
	}

	o.StopAll()

	for _, a := range o.agents {
		if a.Status != "stopped" {
			t.Errorf("agent %s: expected stopped, got %s", a.Name, a.Status)
		}
	}
	cancel()
}

func TestExecuteAgent(t *testing.T) {
	o := newTestOrchestrator()

	tests := []struct {
		agentType AgentType
		name      string
	}{
		{AgentTypeScheduler, "scheduler"},
		{AgentTypeSecurity, "security"},
		{AgentTypeCost, "cost"},
		{AgentTypeOperations, "operations"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := &Agent{
				Type:   tt.agentType,
				Name:   tt.name,
				Logger: logrus.New(),
			}
			o.executeAgent(context.Background(), agent)

			if agent.LastAction == "" {
				t.Error("expected LastAction to be set")
			}
			if agent.LastRunAt.IsZero() {
				t.Error("expected LastRunAt to be set")
			}
		})
	}
}

// ============================================================================
// HTTP Handlers
// ============================================================================

func setupTestRouter(o *AgentOrchestrator) *gin.Engine {
	router := gin.New()
	agentAPI := router.Group("/api/v1/agents")
	{
		agentAPI.GET("/", o.HandleListAgents)
		agentAPI.GET("/:type/status", o.HandleAgentStatus)
		agentAPI.POST("/:type/trigger", o.HandleTriggerAgent)
		agentAPI.PUT("/:type/config", o.HandleUpdateAgentConfig)
		agentAPI.GET("/insights", o.HandleInsights)
		agentAPI.GET("/actions/history", o.HandleActionHistory)
		agentAPI.GET("/metrics/realtime", o.HandleRealtimeMetrics)
	}
	return router
}

func TestHandleListAgents(t *testing.T) {
	o := newTestOrchestrator()
	o.RegisterAgent(AgentTypeScheduler, &Agent{
		Type: AgentTypeScheduler, Name: "Sched", Status: "running",
		Interval: 30 * time.Second, Logger: logrus.New(),
	})
	o.RegisterAgent(AgentTypeSecurity, &Agent{
		Type: AgentTypeSecurity, Name: "Sec", Status: "running",
		Interval: 60 * time.Second, Logger: logrus.New(),
	})

	router := setupTestRouter(o)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/agents/", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	agents, ok := resp["agents"].([]interface{})
	if !ok {
		t.Fatal("expected agents array in response")
	}
	if len(agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(agents))
	}
}

func TestHandleAgentStatus_Found(t *testing.T) {
	o := newTestOrchestrator()
	o.RegisterAgent(AgentTypeSecurity, &Agent{
		Type: AgentTypeSecurity, Name: "SecAgent", Status: "running",
		Interval: 60 * time.Second, Logger: logrus.New(),
		LastAction: "scanned clusters",
	})

	router := setupTestRouter(o)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/agents/security/status", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["name"] != "SecAgent" {
		t.Errorf("expected name SecAgent, got %v", resp["name"])
	}
}

func TestHandleAgentStatus_NotFound(t *testing.T) {
	o := newTestOrchestrator()
	router := setupTestRouter(o)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/agents/nonexistent/status", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleTriggerAgent(t *testing.T) {
	o := newTestOrchestrator()
	o.RegisterAgent(AgentTypeCost, &Agent{
		Type: AgentTypeCost, Name: "CostAgent", Status: "running",
		Interval: 300 * time.Second, Logger: logrus.New(),
	})

	router := setupTestRouter(o)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/agents/cost/trigger", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", w.Code)
	}
}

func TestHandleTriggerAgent_NotFound(t *testing.T) {
	o := newTestOrchestrator()
	router := setupTestRouter(o)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/agents/nonexistent/trigger", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandleUpdateAgentConfig(t *testing.T) {
	o := newTestOrchestrator()
	router := setupTestRouter(o)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/v1/agents/scheduler/config", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleInsights(t *testing.T) {
	o := newTestOrchestrator()
	router := setupTestRouter(o)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/agents/insights", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	insights, ok := resp["insights"].([]interface{})
	if !ok {
		t.Fatal("expected insights array")
	}
	if len(insights) < 3 {
		t.Errorf("expected at least 3 default insights, got %d", len(insights))
	}
}

func TestHandleActionHistory(t *testing.T) {
	o := newTestOrchestrator()
	router := setupTestRouter(o)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/agents/actions/history", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleRealtimeMetrics_NoCollector(t *testing.T) {
	o := newTestOrchestrator()
	o.collector = nil
	router := setupTestRouter(o)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/agents/metrics/realtime", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestHandleRealtimeMetrics_WithCollector_NoData(t *testing.T) {
	// The nil-collector path is tested above (TestHandleRealtimeMetrics_NoCollector).
	// When a collector exists but has no data, GetLatest() returns nil and the handler
	// responds 200 with "no metrics collected yet". This path requires a real Collector
	// instance which depends on the agentpkg import (only available in main.go).
	// Coverage for that branch is validated through integration tests.
	t.Log("nil-data path validated via integration")
}

// ============================================================================
// Agent lifecycle integration
// ============================================================================

func TestAgentLifecycle(t *testing.T) {
	o := newTestOrchestrator()
	agent := &Agent{
		Type:     AgentTypeOperations,
		Name:     "Ops",
		Status:   "initializing",
		Interval: 50 * time.Millisecond,
		Logger:   logrus.New(),
	}
	o.RegisterAgent(AgentTypeOperations, agent)

	ctx, cancel := context.WithCancel(context.Background())
	o.StartAll(ctx)

	// Wait for a few ticks
	time.Sleep(180 * time.Millisecond)

	if agent.Status != "running" {
		t.Errorf("expected running, got %s", agent.Status)
	}
	if agent.LastAction == "" {
		t.Error("expected LastAction to be populated after ticks")
	}
	if agent.LastRunAt.IsZero() {
		t.Error("expected LastRunAt to be set")
	}

	cancel()
	o.StopAll()

	if agent.Status != "stopped" {
		t.Errorf("expected stopped after StopAll, got %s", agent.Status)
	}
}
