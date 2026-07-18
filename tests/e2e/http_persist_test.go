package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/api"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/auth"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/cloud"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/cluster"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/feature"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/monitor"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/security"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/store"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/workload"
)

var sqliteSeq int64

// newPersistentRouter wires the production router backed by a pure-Go in-memory
// SQLite store (no CGO), so persistence-dependent flows — auth register/login
// and the workload state machine with transactional events — run through the
// real HTTP stack with real logic.
func newPersistentRouter(t *testing.T) (http.Handler, *auth.Service, *store.Store) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	lg := logrus.New()
	lg.SetLevel(logrus.PanicLevel)

	// Unique shared-cache in-memory DB per test; a single connection keeps it alive.
	dsn := fmt.Sprintf("file:cloudai_%d?mode=memory&cache=shared", atomic.AddInt64(&sqliteSeq, 1))
	st, err := store.New(store.Config{Driver: "sqlite", DSN: dsn, MaxOpenConns: 1, LogLevel: "silent"})
	if err != nil {
		t.Fatalf("sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	authSvc, err := auth.NewService(auth.Config{
		JWTSecret: "test-secret-0123456789-abcdefghij-xyz",
		JWTExpiry: time.Hour,
	})
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	authSvc.SetStore(st)

	cloudMgr, _ := cloud.NewManager(cloud.ManagerConfig{})
	clusterMgr, err := cluster.NewManager(cluster.ManagerConfig{CloudManager: cloudMgr, Store: st})
	if err != nil {
		t.Fatalf("cluster: %v", err)
	}
	workloadMgr, err := workload.NewManager(workload.ManagerConfig{Store: st, Logger: lg})
	if err != nil {
		t.Fatalf("workload: %v", err)
	}

	// Security + monitoring are store-backed here so policy CRUD and alert
	// rules/events run their DB-first paths through the real HTTP stack.
	securityMgr, err := security.NewManager(security.ManagerConfig{})
	if err != nil {
		t.Fatalf("security: %v", err)
	}
	securityMgr.SetStore(st)
	monitorSvc, err := monitor.NewService(monitor.ServiceConfig{})
	if err != nil {
		t.Fatalf("monitor: %v", err)
	}
	monitorSvc.SetStore(st)

	ff := feature.NewManager(feature.Config{Logger: lg})

	router := api.NewRouter(api.RouterConfig{
		AuthService:     authSvc,
		CloudManager:    cloudMgr,
		ClusterManager:  clusterMgr,
		WorkloadManager: workloadMgr,
		SecurityManager: securityMgr,
		MonitorService:  monitorSvc,
		FeatureFlags:    ff,
		Store:           st,
		Logger:          lg,
	})
	return router, authSvc, st
}

// mintAdminToken issues an admin JWT for driving permission-gated endpoints.
func mintAdminToken(t *testing.T, authSvc *auth.Service) string {
	t.Helper()
	tok, err := authSvc.GenerateToken(&auth.User{ID: "u-admin", Username: "admin", Role: auth.RoleAdmin})
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	return tok.AccessToken
}

// TestHTTP_AuthPersistence_SQLite exercises the store-backed auth chain over
// real HTTP: register persists a user, login verifies the bcrypt hash, wrong
// passwords and duplicate registrations are rejected, and the issued token is
// accepted by the real auth middleware.
func TestHTTP_AuthPersistence_SQLite(t *testing.T) {
	router, _, _ := newPersistentRouter(t)

	reg := `{"username":"alice","email":"alice@test.local","password":"password123","display_name":"Alice"}`

	// Register -> 201 with a token (user persisted to SQLite).
	w := doReq(router, "POST", "/api/v1/auth/register", reg, "")
	if w.Code != http.StatusCreated {
		t.Fatalf("register: got %d: %s", w.Code, w.Body.String())
	}
	var regTok struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &regTok)
	if regTok.AccessToken == "" {
		t.Fatalf("register missing access_token: %s", w.Body.String())
	}

	// Duplicate username must be rejected (real DB uniqueness).
	if w := doReq(router, "POST", "/api/v1/auth/register", reg, ""); w.Code == http.StatusCreated {
		t.Errorf("duplicate register should fail, got %d", w.Code)
	}

	// Login with the correct password -> 200 + token (bcrypt verified).
	w = doReq(router, "POST", "/api/v1/auth/login", `{"username":"alice","password":"password123"}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("login: got %d: %s", w.Code, w.Body.String())
	}
	var loginTok struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &loginTok)
	if loginTok.AccessToken == "" {
		t.Fatalf("login missing access_token: %s", w.Body.String())
	}

	// Wrong password -> 401.
	if w := doReq(router, "POST", "/api/v1/auth/login", `{"username":"alice","password":"wrong-password"}`, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("login with wrong password: got %d, want 401", w.Code)
	}

	// The login token must be accepted by the auth middleware (not 401).
	if w := doReq(router, "GET", "/api/v1/clusters", "", loginTok.AccessToken); w.Code == http.StatusUnauthorized {
		t.Errorf("valid login token rejected by auth middleware (401)")
	}
}

// TestHTTP_WorkloadStateMachineAndEvents_SQLite drives the workload scheduling
// chain over real HTTP (POST -> PUT status -> GET events) and asserts the state
// machine rejects invalid transitions and that every valid transition records
// exactly one event — proving the status-update + event-insert transaction
// boundary lands through the API.
func TestHTTP_WorkloadStateMachineAndEvents_SQLite(t *testing.T) {
	router, authSvc, _ := newPersistentRouter(t)
	tok, err := authSvc.GenerateToken(&auth.User{ID: "u-admin", Username: "admin", Role: auth.RoleAdmin})
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	token := tok.AccessToken

	// Create -> persisted, starts "pending".
	create := `{"name":"train-job","cluster_id":"c-1","type":"training","image":"pytorch:latest",` +
		`"resource_request":{"gpu_count":1,"cpu_millicores":2000,"memory_bytes":1073741824}}`
	w := doReq(router, "POST", "/api/v1/workloads", create, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("create workload: got %d: %s", w.Code, w.Body.String())
	}
	var wl struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &wl)
	if wl.ID == "" || wl.Status != "pending" {
		t.Fatalf("created workload id=%q status=%q, want id + pending", wl.ID, wl.Status)
	}

	// Invalid transition pending->running must be rejected by the state machine.
	if w := doReq(router, "PUT", "/api/v1/workloads/"+wl.ID+"/status", `{"status":"running"}`, token); w.Code < 400 {
		t.Errorf("invalid transition pending->running should be rejected, got %d: %s", w.Code, w.Body.String())
	}

	// Valid chain pending->queued->scheduled->running; each records one event.
	for _, to := range []string{"queued", "scheduled", "running"} {
		w := doReq(router, "PUT", "/api/v1/workloads/"+wl.ID+"/status",
			fmt.Sprintf(`{"status":%q,"reason":"e2e"}`, to), token)
		if w.Code != http.StatusOK {
			t.Fatalf("transition ->%s: got %d: %s", to, w.Code, w.Body.String())
		}
		var updated struct {
			Status string `json:"status"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &updated)
		if updated.Status != to {
			t.Errorf("after ->%s workload status=%q", to, updated.Status)
		}
	}

	// Exactly one event per valid transition (status + event committed together).
	w = doReq(router, "GET", "/api/v1/workloads/"+wl.ID+"/events", "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("get events: got %d: %s", w.Code, w.Body.String())
	}
	var ev struct {
		Events []map[string]interface{} `json:"events"`
		Total  int                      `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &ev)
	if ev.Total != 3 || len(ev.Events) != 3 {
		t.Errorf("expected 3 transition events, got total=%d len=%d: %s", ev.Total, len(ev.Events), w.Body.String())
	}

	// Final-state consistency: GET workload reflects the last transition.
	w = doReq(router, "GET", "/api/v1/workloads/"+wl.ID, "", token)
	var got struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Status != "running" {
		t.Errorf("final workload status=%q, want running", got.Status)
	}
}

// TestHTTP_SecurityPolicyCRUD_SQLite drives the security-policy endpoints over
// real HTTP against the store-backed security manager: create persists a policy
// (rules serialized to JSON) to SQLite, list/get read it back through the
// DB-first path, a second policy is stored independently, and an unknown ID is a
// real 404. Auth is enforced by the production middleware throughout.
func TestHTTP_SecurityPolicyCRUD_SQLite(t *testing.T) {
	router, authSvc, st := newPersistentRouter(t)
	token := mintAdminToken(t, authSvc)

	// Auth enforced: no token -> 401.
	if w := doReq(router, "GET", "/api/v1/security/policies", "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated list: got %d, want 401", w.Code)
	}

	// Initially empty: DB-first read (built-in defaults live only in the cache).
	w := doReq(router, "GET", "/api/v1/security/policies", "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("list: got %d: %s", w.Code, w.Body.String())
	}
	var list0 struct {
		Policies []map[string]interface{} `json:"policies"`
		Total    int                      `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &list0)
	if list0.Total != 0 || len(list0.Policies) != 0 {
		t.Fatalf("expected 0 persisted policies initially, got total=%d len=%d", list0.Total, len(list0.Policies))
	}

	// Create -> 201, server assigns id + status=active and keeps the rules.
	create := `{"name":"deny-privileged","description":"block privileged pods",` +
		`"type":"pod-security","scope":"global","enforcement":"enforce",` +
		`"rules":[{"id":"r1","name":"no-privileged","condition":"pod.privileged==true","action":"deny"}]}`
	w = doReq(router, "POST", "/api/v1/security/policies", create, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("create policy: got %d: %s", w.Code, w.Body.String())
	}
	var created struct {
		ID     string                   `json:"id"`
		Name   string                   `json:"name"`
		Status string                   `json:"status"`
		Rules  []map[string]interface{} `json:"rules"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if created.ID == "" || created.Status != "active" {
		t.Fatalf("created policy id=%q status=%q, want id + active", created.ID, created.Status)
	}
	if created.Name != "deny-privileged" || len(created.Rules) != 1 {
		t.Fatalf("created policy name=%q rules=%d, want deny-privileged + 1 rule", created.Name, len(created.Rules))
	}

	// Persisted to SQLite: read the row directly and confirm the JSON rules landed.
	model, err := st.GetSecurityPolicyByID(created.ID)
	if err != nil {
		t.Fatalf("policy %s not persisted to sqlite: %v", created.ID, err)
	}
	if model.Name != "deny-privileged" || model.Type != "pod-security" {
		t.Errorf("persisted policy name=%q type=%q", model.Name, model.Type)
	}
	if !strings.Contains(model.Rules, "no-privileged") {
		t.Errorf("policy rules not persisted as JSON: %q", model.Rules)
	}

	// List reflects the create (read-through the DB).
	w = doReq(router, "GET", "/api/v1/security/policies", "", token)
	var list1 struct {
		Policies []map[string]interface{} `json:"policies"`
		Total    int                      `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &list1)
	if list1.Total != 1 || len(list1.Policies) != 1 {
		t.Fatalf("after create expected 1 policy, got total=%d len=%d", list1.Total, len(list1.Policies))
	}
	if list1.Policies[0]["id"] != created.ID {
		t.Errorf("listed id=%v, want %s (create->list must be consistent)", list1.Policies[0]["id"], created.ID)
	}

	// GET by id round-trips the rules through SQLite JSON (real (de)serialization).
	w = doReq(router, "GET", "/api/v1/security/policies/"+created.ID, "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("get by id: got %d: %s", w.Code, w.Body.String())
	}
	var got struct {
		ID    string                   `json:"id"`
		Rules []map[string]interface{} `json:"rules"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.ID != created.ID || len(got.Rules) != 1 {
		t.Fatalf("get by id id=%q rules=%d, want %s + 1 rule", got.ID, len(got.Rules), created.ID)
	}
	if got.Rules[0]["action"] != "deny" {
		t.Errorf("round-tripped rule action=%v, want deny", got.Rules[0]["action"])
	}

	// A second policy is stored as an independent row.
	create2 := `{"name":"trusted-registry","type":"image","scope":"global","enforcement":"warn","rules":[]}`
	if w := doReq(router, "POST", "/api/v1/security/policies", create2, token); w.Code != http.StatusCreated {
		t.Fatalf("create second policy: got %d: %s", w.Code, w.Body.String())
	}
	w = doReq(router, "GET", "/api/v1/security/policies", "", token)
	var list2 struct {
		Policies []map[string]interface{} `json:"policies"`
		Total    int                      `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &list2)
	if list2.Total != 2 || len(list2.Policies) != 2 {
		t.Errorf("after 2 creates expected 2 policies, got total=%d len=%d", list2.Total, len(list2.Policies))
	}

	// Unknown id -> real 404 from the manager (not found in DB or cache).
	if w := doReq(router, "GET", "/api/v1/security/policies/does-not-exist", "", token); w.Code != http.StatusNotFound {
		t.Errorf("get unknown policy: got %d, want 404", w.Code)
	}
}

// TestHTTP_MonitoringAlertRules_SQLite verifies the monitoring endpoints serve
// store-backed alert rules and events over real HTTP: rules/events seeded into
// SQLite are returned through the DB-first read path, with the model->domain
// conversion (JSON channels/labels, seconds->Duration) round-tripping intact.
func TestHTTP_MonitoringAlertRules_SQLite(t *testing.T) {
	router, authSvc, st := newPersistentRouter(t)
	token := mintAdminToken(t, authSvc)

	// Auth enforced.
	if w := doReq(router, "GET", "/api/v1/monitoring/alerts/rules", "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated rules: got %d, want 401", w.Code)
	}

	// DB-first read: no persisted rules yet (defaults live only in the cache).
	w := doReq(router, "GET", "/api/v1/monitoring/alerts/rules", "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("list rules: got %d: %s", w.Code, w.Body.String())
	}
	var rules0 struct {
		Rules []map[string]interface{} `json:"rules"`
		Total int                      `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &rules0)
	if rules0.Total != 0 {
		t.Fatalf("expected 0 persisted rules initially, got %d", rules0.Total)
	}

	// Seed two rules straight into SQLite (as another writer would).
	now := time.Now().UTC()
	if err := st.CreateAlertRule(&store.AlertRuleModel{
		ID: "rule-gpu-hot", Name: "GPU Overheat", Description: "temp too high",
		Severity: "critical", Condition: "gpu_temp > 90", Threshold: 90,
		DurationSec: 300, Channels: `["slack","pagerduty"]`, Labels: `{"team":"ml"}`,
		Status: "active", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed rule 1: %v", err)
	}
	if err := st.CreateAlertRule(&store.AlertRuleModel{
		ID: "rule-queue", Name: "Queue Buildup", Severity: "warning",
		Condition: "queue_len > 50", Threshold: 50, DurationSec: 600,
		Channels: `[]`, Labels: `{}`, Status: "active", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("seed rule 2: %v", err)
	}

	// GET now serves the persisted rules through the real monitor service + SQLite.
	w = doReq(router, "GET", "/api/v1/monitoring/alerts/rules", "", token)
	var rules1 struct {
		Rules []map[string]interface{} `json:"rules"`
		Total int                      `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &rules1)
	if rules1.Total != 2 || len(rules1.Rules) != 2 {
		t.Fatalf("after seeding expected 2 rules, got total=%d len=%d", rules1.Total, len(rules1.Rules))
	}

	// The critical rule must round-trip its converted fields.
	var hot map[string]interface{}
	for _, r := range rules1.Rules {
		if r["id"] == "rule-gpu-hot" {
			hot = r
			break
		}
	}
	if hot == nil {
		t.Fatalf("seeded rule 'rule-gpu-hot' not returned via HTTP: %s", w.Body.String())
	}
	if hot["severity"] != "critical" || hot["condition"] != "gpu_temp > 90" {
		t.Errorf("rule fields not round-tripped: severity=%v condition=%v", hot["severity"], hot["condition"])
	}
	if chans, ok := hot["notification_channels"].([]interface{}); !ok || len(chans) != 2 {
		t.Errorf("channels JSON not round-tripped: %v", hot["notification_channels"])
	}
	// seconds->time.Duration conversion: 300s marshals as its nanosecond count.
	if d, ok := hot["duration"].(float64); !ok || int64(d) != int64(300*time.Second) {
		t.Errorf("duration conversion wrong: got %v, want %d", hot["duration"], int64(300*time.Second))
	}

	// Alert events: seed + serve through HTTP.
	if err := st.CreateAlertEvent(&store.AlertEventModel{
		ID: "evt-1", RuleID: "rule-gpu-hot", RuleName: "GPU Overheat",
		Severity: "critical", Message: "gpu 0 at 95C", Status: "firing",
		Labels: `{"node":"gpu-01"}`, FiredAt: now, CreatedAt: now,
	}); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	w = doReq(router, "GET", "/api/v1/monitoring/alerts/events", "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("list events: got %d: %s", w.Code, w.Body.String())
	}
	var events struct {
		Events []map[string]interface{} `json:"events"`
		Total  int                      `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &events)
	if events.Total != 1 || len(events.Events) != 1 {
		t.Fatalf("expected 1 persisted event, got total=%d len=%d", events.Total, len(events.Events))
	}
	if events.Events[0]["rule_id"] != "rule-gpu-hot" || events.Events[0]["status"] != "firing" {
		t.Errorf("event not round-tripped: rule_id=%v status=%v", events.Events[0]["rule_id"], events.Events[0]["status"])
	}
}

// TestHTTP_WorkloadConcurrentStatusRace_SQLite fires many concurrent PUT status
// requests for the SAME running->succeeded transition and asserts the store's
// optimistic lock (UPDATE ... WHERE id=? AND status=<from>) admits exactly ONE
// writer: only one request returns 200, the final persisted state is the winner's
// target, and exactly one transition event is committed (the event insert shares
// the same transaction as the status update).
func TestHTTP_WorkloadConcurrentStatusRace_SQLite(t *testing.T) {
	router, authSvc, st := newPersistentRouter(t)
	token := mintAdminToken(t, authSvc)

	// Create and drive the workload to "running" via the normal chain.
	create := `{"name":"race-job","cluster_id":"c-1","type":"training","image":"pytorch:latest",` +
		`"resource_request":{"gpu_count":1,"cpu_millicores":2000,"memory_bytes":1073741824}}`
	w := doReq(router, "POST", "/api/v1/workloads", create, token)
	if w.Code != http.StatusCreated {
		t.Fatalf("create workload: got %d: %s", w.Code, w.Body.String())
	}
	var wl struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &wl)
	if wl.ID == "" {
		t.Fatalf("created workload missing id: %s", w.Body.String())
	}
	for _, to := range []string{"queued", "scheduled", "running"} {
		if w := doReq(router, "PUT", "/api/v1/workloads/"+wl.ID+"/status",
			fmt.Sprintf(`{"status":%q}`, to), token); w.Code != http.StatusOK {
			t.Fatalf("setup transition ->%s: got %d: %s", to, w.Code, w.Body.String())
		}
	}

	// Race the SAME transition running->succeeded from many goroutines at once.
	const racers = 12
	var wg sync.WaitGroup
	var okCount, failCount int64
	release := make(chan struct{})
	wg.Add(racers)
	for i := 0; i < racers; i++ {
		go func() {
			defer wg.Done()
			<-release // start all racers as simultaneously as possible
			w := doReq(router, "PUT", "/api/v1/workloads/"+wl.ID+"/status",
				`{"status":"succeeded","reason":"race"}`, token)
			if w.Code == http.StatusOK {
				atomic.AddInt64(&okCount, 1)
			} else {
				atomic.AddInt64(&failCount, 1)
			}
		}()
	}
	close(release)
	wg.Wait()

	// Optimistic lock: exactly one writer wins, the rest are rejected.
	if got := atomic.LoadInt64(&okCount); got != 1 {
		t.Fatalf("optimistic lock failed: %d concurrent transitions succeeded, want exactly 1", got)
	}
	if got := atomic.LoadInt64(&failCount); got != racers-1 {
		t.Errorf("expected %d rejected racers, got %d", racers-1, got)
	}

	// Final state is the winner's target, persisted in SQLite.
	if model, err := st.GetWorkloadByID(wl.ID); err != nil {
		t.Fatalf("read workload: %v", err)
	} else if model.Status != "succeeded" {
		t.Errorf("final persisted status=%q, want succeeded", model.Status)
	}

	// Final state is consistent over HTTP too.
	w = doReq(router, "GET", "/api/v1/workloads/"+wl.ID, "", token)
	var final struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &final)
	if final.Status != "succeeded" {
		t.Errorf("HTTP final status=%q, want succeeded", final.Status)
	}

	// Exactly one succeeded-event committed (event insert bound to the winning txn).
	w = doReq(router, "GET", "/api/v1/workloads/"+wl.ID+"/events", "", token)
	var ev struct {
		Events []map[string]interface{} `json:"events"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &ev)
	succeeded := 0
	for _, e := range ev.Events {
		if e["to_status"] == "succeeded" {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Errorf("expected exactly 1 committed succeeded event, got %d (events=%d)", succeeded, len(ev.Events))
	}
}
