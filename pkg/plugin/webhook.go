package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ============================================================================
// Webhook Plugin — out-of-process extension via HTTP callbacks
// ============================================================================

// WebhookConfig configures a webhook plugin.
type WebhookConfig struct {
	// Name of this webhook plugin (unique).
	Name string `json:"name" yaml:"name"`

	// URL is the HTTP(S) endpoint to call.
	URL string `json:"url" yaml:"url"`

	// ExtensionPoints lists which hooks this webhook handles.
	ExtensionPoints []ExtensionPoint `json:"extensionPoints" yaml:"extensionPoints"`

	// TimeoutSeconds for each webhook call. Default 10.
	TimeoutSeconds int `json:"timeoutSeconds,omitempty" yaml:"timeoutSeconds,omitempty"`

	// FailurePolicy: "Fail" (reject on error) or "Ignore" (skip on error).
	FailurePolicy string `json:"failurePolicy,omitempty" yaml:"failurePolicy,omitempty"`

	// CABundle is an optional PEM CA certificate for TLS verification.
	CABundle string `json:"caBundle,omitempty" yaml:"caBundle,omitempty"`

	// Headers are additional HTTP headers sent with each request.
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`

	// Priority for ordering within extension points.
	Priority int `json:"priority,omitempty" yaml:"priority,omitempty"`
}

// WebhookRequest is the JSON payload sent to the webhook endpoint.
type WebhookRequest struct {
	// UID is a unique identifier for this specific call.
	UID string `json:"uid"`
	// ExtensionPoint identifies which hook is being invoked.
	ExtensionPoint ExtensionPoint `json:"extensionPoint"`
	// Object is the arbitrary payload (e.g., Workload, SecurityPolicy).
	Object json.RawMessage `json:"object"`
	// OldObject is the previous state (for mutating webhooks).
	OldObject json.RawMessage `json:"oldObject,omitempty"`
}

// WebhookResponse is the JSON payload returned by the webhook.
type WebhookResponse struct {
	UID     string          `json:"uid"`
	Allowed bool            `json:"allowed"`
	Result  *Result         `json:"result,omitempty"`
	Patch   json.RawMessage `json:"patch,omitempty"`
	// Mutated object (if the webhook modifies the input).
	MutatedObject json.RawMessage `json:"mutatedObject,omitempty"`
}

// ============================================================================
// WebhookPlugin implements Plugin and calls an external HTTP endpoint
// ============================================================================

// WebhookPlugin wraps an HTTP-based external extension.
type WebhookPlugin struct {
	BasePlugin
	cfg    WebhookConfig
	client *http.Client
}

// NewWebhookPlugin creates a WebhookPlugin from config.
func NewWebhookPlugin(cfg WebhookConfig) *WebhookPlugin {
	timeout := 10 * time.Second
	if cfg.TimeoutSeconds > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds) * time.Second
	}
	if cfg.FailurePolicy == "" {
		cfg.FailurePolicy = "Fail"
	}
	if cfg.Priority == 0 {
		cfg.Priority = 500 // webhooks run after built-in plugins by default
	}

	return &WebhookPlugin{
		BasePlugin: NewBasePlugin(Metadata{
			Name:            cfg.Name,
			Version:         "external",
			Description:     fmt.Sprintf("Webhook plugin → %s", cfg.URL),
			ExtensionPoints: cfg.ExtensionPoints,
			Priority:        cfg.Priority,
			Tags:            map[string]string{"type": "webhook"},
		}),
		cfg: cfg,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// Call sends a WebhookRequest to the configured URL and returns the response.
func (w *WebhookPlugin) Call(ctx context.Context, req *WebhookRequest) (*WebhookResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("webhook %q: marshal request: %w", w.cfg.Name, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, w.cfg.URL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("webhook %q: create request: %w", w.cfg.Name, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range w.cfg.Headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := w.client.Do(httpReq)
	if err != nil {
		if w.cfg.FailurePolicy == "Ignore" {
			return &WebhookResponse{
				UID:     req.UID,
				Allowed: true,
				Result:  SuccessResult(w.cfg.Name),
			}, nil
		}
		return nil, fmt.Errorf("webhook %q: HTTP call failed: %w", w.cfg.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if w.cfg.FailurePolicy == "Ignore" {
			return &WebhookResponse{
				UID:     req.UID,
				Allowed: true,
				Result:  SuccessResult(w.cfg.Name),
			}, nil
		}
		return nil, fmt.Errorf("webhook %q: HTTP %d: %s", w.cfg.Name, resp.StatusCode, string(respBody))
	}

	var webhookResp WebhookResponse
	if err := json.NewDecoder(resp.Body).Decode(&webhookResp); err != nil {
		return nil, fmt.Errorf("webhook %q: decode response: %w", w.cfg.Name, err)
	}
	return &webhookResp, nil
}

// Health checks the webhook endpoint by sending a GET request.
func (w *WebhookPlugin) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.cfg.URL, nil)
	if err != nil {
		return err
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook %q health check failed: %w", w.cfg.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("webhook %q returned HTTP %d", w.cfg.Name, resp.StatusCode)
	}
	return nil
}

// WebhookPluginFactory returns a Factory for the given webhook config.
func WebhookPluginFactory(cfg WebhookConfig) Factory {
	return func() (Plugin, error) {
		return NewWebhookPlugin(cfg), nil
	}
}
