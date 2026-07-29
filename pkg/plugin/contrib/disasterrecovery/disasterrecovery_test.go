package disasterrecovery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin"
)

// These tests prove the DR plugins deliver REAL safety value: the validating
// webhook is a genuine failover safety gate (prevents split-brain and data
// loss), the alerter emits correctly-formatted Slack/DingTalk payloads, and the
// collector reports honest degraded metrics when the DR endpoint is down.

// newWebhookWithState builds a DR webhook wired to a collector whose cached DR
// status is set deterministically (white-box: the test is in-package).
func newWebhookWithState(t *testing.T, lagSeconds, threshold int, primaryUp, standbyUp bool) *DRWebhookPlugin {
	t.Helper()
	cp, err := NewDRCollectorPlugin(DRConfig{LagThresholdSeconds: threshold})
	if err != nil {
		t.Fatalf("NewDRCollectorPlugin: %v", err)
	}
	collector := cp.(*DRCollectorPlugin)
	collector.mu.Lock()
	collector.lastLagSeconds = lagSeconds
	collector.lastPrimaryUp = primaryUp
	collector.lastStandbyUp = standbyUp
	collector.lastCheckedAt = time.Now()
	collector.mu.Unlock()

	wp, err := NewDRWebhookPlugin(DRConfig{LagThresholdSeconds: threshold}, collector)
	if err != nil {
		t.Fatalf("NewDRWebhookPlugin: %v", err)
	}
	return wp.(*DRWebhookPlugin)
}

func validateOp(t *testing.T, wh *DRWebhookPlugin, action string) *plugin.WebhookResponse {
	t.Helper()
	raw, _ := json.Marshal(FailoverOperation{Action: action, SourceHost: "primary", TargetHost: "standby"})
	resp, err := wh.Validate(context.Background(), &plugin.WebhookRequest{UID: "uid-1", Object: raw})
	if err != nil {
		t.Fatalf("Validate(%s): %v", action, err)
	}
	return resp
}

// TestDRWebhook_FailoverBlockedWhenPrimaryHealthy: the primary is still up, so
// failover is unnecessary and dangerous -> must be denied.
func TestDRWebhook_FailoverBlockedWhenPrimaryHealthy(t *testing.T) {
	wh := newWebhookWithState(t, 5, 30, true /*primaryUp*/, true /*standbyUp*/)
	resp := validateOp(t, wh, "failover")
	if resp.Allowed {
		t.Fatal("failover must be denied while primary is healthy")
	}
}

// TestDRWebhook_FailoverBlockedWhenStandbyDown: with primary down AND standby
// down there is nowhere safe to fail over to -> must be denied.
func TestDRWebhook_FailoverBlockedWhenStandbyDown(t *testing.T) {
	wh := newWebhookWithState(t, 5, 30, false /*primaryUp*/, false /*standbyUp*/)
	resp := validateOp(t, wh, "failover")
	if resp.Allowed {
		t.Fatal("failover must be denied when standby is also down")
	}
}

// TestDRWebhook_FailoverBlockedWhenLagTooHigh: replication lag beyond 10x the
// threshold means unacceptable data loss -> must be denied.
func TestDRWebhook_FailoverBlockedWhenLagTooHigh(t *testing.T) {
	// threshold 30 -> hard cap 300s; lag 1000s is far beyond.
	wh := newWebhookWithState(t, 1000, 30, false /*primaryUp*/, true /*standbyUp*/)
	resp := validateOp(t, wh, "failover")
	if resp.Allowed {
		t.Fatal("failover must be denied when replication lag risks data loss")
	}
}

// TestDRWebhook_FailoverAllowedWhenSafe: primary down, standby up, lag small ->
// failover is the correct action and must be allowed.
func TestDRWebhook_FailoverAllowedWhenSafe(t *testing.T) {
	wh := newWebhookWithState(t, 5, 30, false /*primaryUp*/, true /*standbyUp*/)
	resp := validateOp(t, wh, "failover")
	if !resp.Allowed {
		t.Fatalf("failover must be allowed when safe, got %+v", resp.Result)
	}
}

// TestDRWebhook_RollbackGates: rollback is allowed only after the original
// primary has recovered.
func TestDRWebhook_RollbackGates(t *testing.T) {
	// Primary recovered -> rollback allowed.
	if resp := validateOp(t, newWebhookWithState(t, 0, 30, true, true), "rollback"); !resp.Allowed {
		t.Fatal("rollback must be allowed once primary has recovered")
	}
	// Primary still down -> rollback must wait.
	if resp := validateOp(t, newWebhookWithState(t, 0, 30, false, true), "rollback"); resp.Allowed {
		t.Fatal("rollback must be denied while primary is not recovered")
	}
}

// TestDRWebhook_UnknownActionAllowed: non-failover/rollback ops pass through.
func TestDRWebhook_UnknownActionAllowed(t *testing.T) {
	if resp := validateOp(t, newWebhookWithState(t, 0, 30, true, true), "status-check"); !resp.Allowed {
		t.Fatal("unknown action must be allowed (no gate)")
	}
}

// TestDRWebhook_InvalidPayloadRejected: malformed operation JSON is rejected.
func TestDRWebhook_InvalidPayloadRejected(t *testing.T) {
	wh := newWebhookWithState(t, 0, 30, true, true)
	resp, err := wh.Validate(context.Background(), &plugin.WebhookRequest{UID: "x", Object: []byte("{not json")})
	if err != nil {
		t.Fatalf("Validate must not hard-error on bad payload: %v", err)
	}
	if resp.Allowed {
		t.Fatal("malformed operation must not be allowed")
	}
}

// TestDRAlerter_SlackPayloadFormat proves a real Slack attachment is emitted
// with the severity-mapped color.
func TestDRAlerter_SlackPayloadFormat(t *testing.T) {
	var captured map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	ap, _ := NewDRAlerterPlugin(DRConfig{SlackWebhook: srv.URL})
	alerter := ap.(*DRAlerterPlugin)

	err := alerter.SendAlert(context.Background(), &plugin.Alert{
		Name: "replication-lag", Severity: "critical", Message: "lag 500s", FiredAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("SendAlert: %v", err)
	}
	atts, ok := captured["attachments"].([]interface{})
	if !ok || len(atts) == 0 {
		t.Fatalf("expected slack attachments, got %v", captured)
	}
	att := atts[0].(map[string]interface{})
	if att["color"] != "#FF0000" {
		t.Fatalf("critical severity must map to red, got %v", att["color"])
	}
}

// TestDRAlerter_DingTalkPayloadFormat proves a markdown DingTalk payload.
func TestDRAlerter_DingTalkPayloadFormat(t *testing.T) {
	var captured map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	ap, _ := NewDRAlerterPlugin(DRConfig{DingtalkWebhook: srv.URL})
	alerter := ap.(*DRAlerterPlugin)

	if err := alerter.SendAlert(context.Background(), &plugin.Alert{Name: "failover", Severity: "warning", Message: "switched", FiredAt: time.Now()}); err != nil {
		t.Fatalf("SendAlert: %v", err)
	}
	if captured["msgtype"] != "markdown" {
		t.Fatalf("dingtalk msgtype = %v, want markdown", captured["msgtype"])
	}
	md, ok := captured["markdown"].(map[string]interface{})
	if !ok || md["title"] == "" {
		t.Fatalf("dingtalk markdown payload malformed: %v", captured)
	}
}

// TestDRAlerter_SupportedChannels reflects configured channels.
func TestDRAlerter_SupportedChannels(t *testing.T) {
	ap, _ := NewDRAlerterPlugin(DRConfig{SlackWebhook: "https://hooks.slack.com/x", DingtalkWebhook: "https://oapi.dingtalk.com/y"})
	alerter := ap.(*DRAlerterPlugin)
	ch := alerter.SupportedChannels()
	if len(ch) != 2 {
		t.Fatalf("expected 2 channels, got %v", ch)
	}

	noneP, _ := NewDRAlerterPlugin(DRConfig{})
	if len(noneP.(*DRAlerterPlugin).SupportedChannels()) != 0 {
		t.Fatal("no webhooks configured -> no channels")
	}
}

// TestDRCollector_MetricNames pins the metric contract.
func TestDRCollector_MetricNames(t *testing.T) {
	cp, _ := NewDRCollectorPlugin(DRConfig{})
	names := cp.(*DRCollectorPlugin).MetricNames()
	want := map[string]bool{
		"dr_replication_lag_seconds": true, "dr_primary_healthy": true,
		"dr_standby_healthy": true, "dr_rpo_seconds": true,
		"dr_rto_seconds": true, "dr_consistency_check_passed": true,
	}
	if len(names) != len(want) {
		t.Fatalf("metric count = %d, want %d", len(names), len(want))
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected metric %q", n)
		}
	}
}

// TestDRCollector_DegradedOnUnreachable proves honest degraded metrics when the
// DR monitor is unreachable (does not fabricate healthy status).
func TestDRCollector_DegradedOnUnreachable(t *testing.T) {
	// An unroutable TEST-NET-1 host makes fetchDRStatus fail fast.
	cp, _ := NewDRCollectorPlugin(DRConfig{PrimaryHost: "192.0.2.1"})
	collector := cp.(*DRCollectorPlugin)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	samples, err := collector.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect must degrade gracefully, got err: %v", err)
	}
	for _, s := range samples {
		if (s.Name == "dr_primary_healthy" || s.Name == "dr_standby_healthy") && s.Value != 0 {
			t.Fatalf("unreachable DR must report unhealthy (0), got %s=%v", s.Name, s.Value)
		}
	}
}
