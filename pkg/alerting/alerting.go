// Package alerting provides multi-channel alert notification with rule-based
// routing and time-based escalation. It supports Email (SMTP), Slack
// (incoming webhooks) and PagerDuty (Events API v2) delivery channels.
package alerting

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/smtp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Severity classifies the urgency of an alert. Ordering is meaningful:
// higher values are more urgent.
type Severity int

const (
	// SeverityLow is informational; routed to low-friction channels (email).
	SeverityLow Severity = iota
	// SeverityMedium is a warning that warrants attention (Slack).
	SeverityMedium
	// SeverityHigh is a serious problem requiring paging (PagerDuty).
	SeverityHigh
	// SeverityCritical is a critical outage; paged with escalation.
	SeverityCritical
)

// String renders the severity for logging and payloads.
func (s Severity) String() string {
	switch s {
	case SeverityLow:
		return "low"
	case SeverityMedium:
		return "medium"
	case SeverityHigh:
		return "high"
	case SeverityCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// Alert is a single notifiable event.
type Alert struct {
	ID        string
	Severity  Severity
	Source    string
	Message   string
	Timestamp time.Time
	Labels    map[string]string
}

// NotificationChannel delivers alerts to a specific backend.
type NotificationChannel interface {
	// Name returns a stable identifier for the channel.
	Name() string
	// ValidateConfig verifies the channel is configured correctly. It must be
	// called before Send is expected to succeed.
	ValidateConfig() error
	// Send delivers the alert. Implementations must honour ctx cancellation.
	Send(ctx context.Context, alert Alert) error
}

// ---------------------------------------------------------------------------
// Email channel
// ---------------------------------------------------------------------------

// mailSender abstracts smtp.SendMail so the SMTP dial path can be exercised in
// tests without a live server. The default implementation calls net/smtp.
type mailSender func(addr string, a smtp.Auth, from string, to []string, msg []byte) error

// EmailChannel delivers alerts over SMTP.
type EmailChannel struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	To       []string
	// UseTLS enables implicit TLS auth negotiation via LOGIN/PLAIN over the
	// server-advertised STARTTLS. net/smtp handles STARTTLS automatically when
	// the server advertises it.
	UseTLS bool

	// send is injectable for testing; nil means use smtp.SendMail.
	send mailSender
}

// Name implements NotificationChannel.
func (c *EmailChannel) Name() string { return "email" }

// ValidateConfig implements NotificationChannel.
func (c *EmailChannel) ValidateConfig() error {
	if strings.TrimSpace(c.Host) == "" {
		return errors.New("email: host is required")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("email: invalid port %d", c.Port)
	}
	if strings.TrimSpace(c.From) == "" {
		return errors.New("email: from address is required")
	}
	if len(c.To) == 0 {
		return errors.New("email: at least one recipient is required")
	}
	return nil
}

// Send implements NotificationChannel using the standard net/smtp dial +
// auth + send pattern.
func (c *EmailChannel) Send(ctx context.Context, alert Alert) error {
	if err := c.ValidateConfig(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	addr := fmt.Sprintf("%s:%d", c.Host, c.Port)
	var auth smtp.Auth
	if c.Username != "" {
		auth = smtp.PlainAuth("", c.Username, c.Password, c.Host)
	}

	subject := fmt.Sprintf("[%s] %s (%s)", strings.ToUpper(alert.Severity.String()), alert.Source, alert.ID)
	var body bytes.Buffer
	fmt.Fprintf(&body, "From: %s\r\n", c.From)
	fmt.Fprintf(&body, "To: %s\r\n", strings.Join(c.To, ", "))
	fmt.Fprintf(&body, "Subject: %s\r\n", subject)
	body.WriteString("MIME-Version: 1.0\r\n")
	body.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	fmt.Fprintf(&body, "Severity: %s\r\n", alert.Severity)
	fmt.Fprintf(&body, "Source:   %s\r\n", alert.Source)
	fmt.Fprintf(&body, "Time:     %s\r\n\r\n", alert.Timestamp.Format(time.RFC3339))
	body.WriteString(alert.Message)
	body.WriteString("\r\n")
	for k, v := range sortedLabels(alert.Labels) {
		fmt.Fprintf(&body, "%s: %s\r\n", v.k, v.v)
		_ = k
	}

	send := c.send
	if send == nil {
		send = smtp.SendMail
	}
	if err := send(addr, auth, c.From, c.To, body.Bytes()); err != nil {
		return fmt.Errorf("email: send failed: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// HTTP-based channels (Slack, PagerDuty)
// ---------------------------------------------------------------------------

// httpDoer abstracts *http.Client so HTTP channels can be tested with a fake
// transport.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

func defaultHTTPClient() httpDoer {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}
}

// SlackChannel posts alerts to a Slack incoming webhook.
type SlackChannel struct {
	WebhookURL string
	Channel    string // optional override
	Username   string // optional override

	client httpDoer
}

// Name implements NotificationChannel.
func (c *SlackChannel) Name() string { return "slack" }

// ValidateConfig implements NotificationChannel.
func (c *SlackChannel) ValidateConfig() error {
	if !strings.HasPrefix(c.WebhookURL, "https://") {
		return errors.New("slack: webhook URL must be an https URL")
	}
	return nil
}

func severityColor(s Severity) string {
	switch s {
	case SeverityCritical, SeverityHigh:
		return "danger"
	case SeverityMedium:
		return "warning"
	default:
		return "good"
	}
}

// Send implements NotificationChannel by POSTing a Slack message payload.
func (c *SlackChannel) Send(ctx context.Context, alert Alert) error {
	if err := c.ValidateConfig(); err != nil {
		return err
	}

	fields := make([]map[string]interface{}, 0, len(alert.Labels)+2)
	fields = append(fields,
		map[string]interface{}{"title": "Severity", "value": alert.Severity.String(), "short": true},
		map[string]interface{}{"title": "Source", "value": alert.Source, "short": true},
	)
	for _, kv := range sortedLabels(alert.Labels) {
		fields = append(fields, map[string]interface{}{"title": kv.k, "value": kv.v, "short": true})
	}

	payload := map[string]interface{}{
		"attachments": []map[string]interface{}{{
			"color":    severityColor(alert.Severity),
			"title":    fmt.Sprintf("%s [%s]", alert.Source, alert.ID),
			"text":     alert.Message,
			"fields":   fields,
			"ts":       alert.Timestamp.Unix(),
			"fallback": alert.Message,
		}},
	}
	if c.Channel != "" {
		payload["channel"] = c.Channel
	}
	if c.Username != "" {
		payload["username"] = c.Username
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("slack: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("slack: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := c.client
	if client == nil {
		client = defaultHTTPClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("slack: post failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

// pdEventsURL is the PagerDuty Events API v2 enqueue endpoint. It is a var so
// tests can point it at a local server.
var pdEventsURL = "https://events.pagerduty.com/v2/enqueue"

// PagerDutyChannel triggers incidents through the PagerDuty Events API v2.
type PagerDutyChannel struct {
	IntegrationKey string

	client httpDoer
}

// Name implements NotificationChannel.
func (c *PagerDutyChannel) Name() string { return "pagerduty" }

// ValidateConfig implements NotificationChannel.
func (c *PagerDutyChannel) ValidateConfig() error {
	if strings.TrimSpace(c.IntegrationKey) == "" {
		return errors.New("pagerduty: integration key is required")
	}
	return nil
}

func pdSeverity(s Severity) string {
	switch s {
	case SeverityCritical:
		return "critical"
	case SeverityHigh:
		return "error"
	case SeverityMedium:
		return "warning"
	default:
		return "info"
	}
}

// Send implements NotificationChannel by enqueuing a "trigger" event.
func (c *PagerDutyChannel) Send(ctx context.Context, alert Alert) error {
	if err := c.ValidateConfig(); err != nil {
		return err
	}

	customDetails := make(map[string]string, len(alert.Labels)+1)
	for k, v := range alert.Labels {
		customDetails[k] = v
	}
	customDetails["alert_id"] = alert.ID

	payload := map[string]interface{}{
		"routing_key":  c.IntegrationKey,
		"event_action": "trigger",
		"dedup_key":    alert.ID,
		"payload": map[string]interface{}{
			"summary":        fmt.Sprintf("%s: %s", alert.Source, alert.Message),
			"source":         alert.Source,
			"severity":       pdSeverity(alert.Severity),
			"timestamp":      alert.Timestamp.Format(time.RFC3339),
			"custom_details": customDetails,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("pagerduty: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pdEventsURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("pagerduty: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := c.client
	if client == nil {
		client = defaultHTTPClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("pagerduty: post failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	// Events API v2 returns 202 Accepted on success.
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("pagerduty: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Routing & escalation
// ---------------------------------------------------------------------------

// AlertRouter maps alert severities to the channels that should receive them.
type AlertRouter struct {
	mu        sync.RWMutex
	rules     map[Severity][]NotificationChannel
	escalator *EscalationPolicy
}

// NewAlertRouter returns an empty router.
func NewAlertRouter() *AlertRouter {
	return &AlertRouter{rules: make(map[Severity][]NotificationChannel)}
}

// AddRule registers channels for a severity level. Multiple channels may be
// attached to the same severity.
func (r *AlertRouter) AddRule(sev Severity, channels ...NotificationChannel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rules[sev] = append(r.rules[sev], channels...)
}

// SetEscalationPolicy attaches an escalation policy consulted on delivery
// failure.
func (r *AlertRouter) SetEscalationPolicy(p *EscalationPolicy) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.escalator = p
}

// ChannelsFor returns the channels configured for a severity (read-only copy).
func (r *AlertRouter) ChannelsFor(sev Severity) []NotificationChannel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]NotificationChannel, len(r.rules[sev]))
	copy(out, r.rules[sev])
	return out
}

// Route delivers the alert to every channel registered for its severity. If a
// channel fails and an escalation policy is set, escalation is attempted. The
// returned error aggregates all delivery failures; nil means at least the
// primary route (or an escalation) succeeded for every target.
func (r *AlertRouter) Route(ctx context.Context, alert Alert) error {
	channels := r.ChannelsFor(alert.Severity)
	if len(channels) == 0 {
		return fmt.Errorf("routing: no channel configured for severity %s", alert.Severity)
	}

	var errs []error
	delivered := false
	for _, ch := range channels {
		if err := ch.Send(ctx, alert); err != nil {
			errs = append(errs, fmt.Errorf("channel %s: %w", ch.Name(), err))
			continue
		}
		delivered = true
	}

	if delivered {
		return errorsJoin(errs) // surface partial failures but do not escalate
	}

	// All primary channels failed: escalate if we can.
	r.mu.RLock()
	esc := r.escalator
	r.mu.RUnlock()
	if esc != nil {
		if err := esc.Escalate(ctx, alert); err != nil {
			errs = append(errs, fmt.Errorf("escalation: %w", err))
			return errorsJoin(errs)
		}
		return nil
	}
	return errorsJoin(errs)
}

// EscalationLevel is a single tier in an escalation policy.
type EscalationLevel struct {
	// Timeout is how long to wait for acknowledgement before advancing to the
	// next level. It is used by callers driving acknowledgement loops.
	Timeout  time.Duration
	Channels []NotificationChannel
}

// EscalationPolicy advances an unacknowledged alert through progressively more
// urgent notification levels.
type EscalationPolicy struct {
	Levels []EscalationLevel
}

// Escalate walks the levels in order, sending to each level's channels until
// one level delivers successfully. It returns an error only if every level
// fails.
func (p *EscalationPolicy) Escalate(ctx context.Context, alert Alert) error {
	if len(p.Levels) == 0 {
		return errors.New("escalation: no levels defined")
	}
	var errs []error
	for i, lvl := range p.Levels {
		levelDelivered := false
		for _, ch := range lvl.Channels {
			if err := ch.Send(ctx, alert); err != nil {
				errs = append(errs, fmt.Errorf("level %d channel %s: %w", i, ch.Name(), err))
				continue
			}
			levelDelivered = true
		}
		if levelDelivered {
			return nil
		}
	}
	return errorsJoin(errs)
}

// NextLevel returns the index of the level that should receive an alert after
// elapsed time has passed, based on cumulative timeouts. It returns -1 when
// elapsed exceeds all configured levels (fully escalated).
func (p *EscalationPolicy) NextLevel(elapsed time.Duration) int {
	var acc time.Duration
	for i, lvl := range p.Levels {
		acc += lvl.Timeout
		if elapsed < acc {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

type kv struct{ k, v string }

// sortedLabels returns labels in deterministic key order for stable payloads.
func sortedLabels(labels map[string]string) []kv {
	out := make([]kv, 0, len(labels))
	for k, v := range labels {
		out = append(out, kv{k, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].k < out[j].k })
	return out
}

// errorsJoin joins errors, returning nil for an empty slice.
func errorsJoin(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}
