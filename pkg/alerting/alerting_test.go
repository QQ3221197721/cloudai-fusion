package alerting

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/smtp"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockHTTPTransport allows testing HTTP clients with a callback type.
type mockHTTPTransport func(req *http.Request) *http.Response

func (m mockHTTPTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return m(r), nil
}


func TestAlertRouting(t *testing.T) {
	var routes []string
	mu := &sync.Mutex{}

	t.Run("low_severity_to_email", func(t *testing.T) {
		router := NewAlertRouter()
		email := &EmailChannel{
			Host:     "smtp.example.com",
			Port:     587,
			From:     "alerts@example.com",
			To:       []string{"ops@example.com"},
			UseTLS:   true,
			send: func(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
				mu.Lock()
				defer mu.Unlock()
				routes = append(routes, "email")
				return nil
			},
		}
		if err := email.ValidateConfig(); err != nil {
			t.Fatalf("validate: %v", err)
		}
		router.AddRule(SeverityLow, email)

		alert := Alert{
			ID:        "a1",
			Severity:  SeverityLow,
			Source:    "test",
			Message:   "test message",
			Timestamp: time.Now(),
		}
		if err := router.Route(context.Background(), alert); err != nil {
			t.Fatalf("route: %v", err)
		}
	})

	t.Run("medium_severity_to_slack", func(t *testing.T) {
		router := NewAlertRouter()
		slack := &SlackChannel{
			WebhookURL: "https://hooks.slack.com/services/X/Y/Z",
			client: &http.Client{
				Transport: mockHTTPTransport(func(req *http.Request) *http.Response {
					body, _ := io.ReadAll(req.Body)
					var m map[string]interface{}
					json.Unmarshal(body, &m)
					if att, ok := m["attachments"].([]interface{}); ok && len(att) > 0 {
						attMap := att[0].(map[string]interface{})
						if attMap["color"] == "warning" {
							mu.Lock()
							defer mu.Unlock()
							routes = append(routes, "slack")
						}
					}
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}
				}),
			},
		}
		router.AddRule(SeverityMedium, slack)

		alert := Alert{ID: "a2", Severity: SeverityMedium, Source: "svc", Message: "warn", Timestamp: time.Now()}
		if err := router.Route(context.Background(), alert); err != nil {
			t.Fatalf("route: %v", err)
		}
	})

	t.Run("high_severity_to_pagerduty", func(t *testing.T) {
		router := NewAlertRouter()
		pd := &PagerDutyChannel{
			IntegrationKey: "my-integration-key",
			client: &http.Client{
				Transport: mockHTTPTransport(func(req *http.Request) *http.Response {
					body, _ := io.ReadAll(req.Body)
					var payload map[string]interface{}
					json.Unmarshal(body, &payload)
					if p, ok := payload["payload"].(map[string]interface{}); ok {
						if p["severity"] == "error" {
							mu.Lock()
							defer mu.Unlock()
							routes = append(routes, "pagerduty")
						}
					}
					return &http.Response{StatusCode: http.StatusAccepted, Body: io.NopCloser(strings.NewReader("{\"detail\":\"ok\"}"))}
				}),
			},
		}
		router.AddRule(SeverityHigh, pd)

		alert := Alert{ID: "a3", Severity: SeverityHigh, Source: "db", Message: "fail", Timestamp: time.Now()}
		if err := router.Route(context.Background(), alert); err != nil {
			t.Fatalf("route: %v", err)
		}
	})
}

func TestEscalationPolicy(t *testing.T) {
	policy := &EscalationPolicy{
		Levels: []EscalationLevel{
			{Timeout: 2 * time.Minute, Channels: []NotificationChannel{&EmailChannel{Host: "x", Port: 25, From: "x@x.com", To: []string{"x"}}}},
			{Timeout: 1 * time.Minute, Channels: []NotificationChannel{&SlackChannel{WebhookURL: "https://webhook.example.com/"}},
			},
		},
	}

	next := policy.NextLevel(0)
	if next != 0 {
		t.Errorf("NextLevel(0)=%d; want 0", next)
	}

	next = policy.NextLevel(time.Duration(1)*time.Minute)
	if next != 0 {
		t.Errorf("NextLevel(1m)=%d; want 0", next)
	}
	
	// With levels [2m, 3m], elapsed < 2m: level 0; 2m-3m: level 1; >=3m: -1
	next = policy.NextLevel(2*time.Minute - 1*time.Second)
	if next != 0 {
		t.Errorf("NextLevel(1m59s)=%d; want 0", next)
	}
	next = policy.NextLevel(2*time.Minute + 1*time.Second)
	if next != 1 {
		t.Errorf("NextLevel(2m+1s)=%d; want 1", next)
	}
	next = policy.NextLevel(4 * time.Minute)
	if next != -1 {
		t.Errorf("NextLevel(4m)=%d; want -1", next)
	}
}

func BenchmarkAlertRouting(b *testing.B) {
	ch := &SlackChannel{
		WebhookURL: "https://hooks.slack.com/test",
		client:     &http.Client{Transport: mockHTTPTransport(func(r *http.Request) *http.Response { return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}})},
	}
	router := NewAlertRouter()
	router.AddRule(SeverityMedium, ch)
	alert := Alert{ID: "b1", Severity: SeverityMedium, Source: "benchmark", Message: "hello", Timestamp: time.Now()}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := context.Background()
		_ = router.Route(ctx, alert)
	}
}
