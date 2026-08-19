// Package sdk is the official Go client library for the CloudAI Fusion platform.
//
// External developers use this package to interact with the CloudAI Fusion
// control plane — verifying evidence chains, scheduling GPU workloads, running
// security campaigns, and recording billable usage — without hand-rolling HTTP
// calls or worrying about authentication, retries, and error decoding.
//
// # Getting started
//
// Create a client and reach into the module sub-clients:
//
//	client := sdk.New("https://api.cloudai.io", sdk.WithAPIKey("caf_live_xxx"))
//
//	result, err := client.Evidence.Verify(context.Background(), "production")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("chain valid: %v (%d entries)\n", result.Valid, result.EntryCount)
//
// The design intentionally mirrors well-established Go SDKs (such as the Docker
// SDK): a single Client holds shared transport and credentials, while each
// domain exposes a focused sub-client accessed as a field.
package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultTimeout is the request timeout applied when the caller does not
	// override it via WithTimeout or WithHTTPClient.
	DefaultTimeout = 30 * time.Second

	// userAgent identifies the SDK in outbound requests. Servers use this to
	// gate features and gather adoption telemetry.
	userAgent = "cloudai-fusion-go-sdk/1.0"
)

// Client is the primary entry point for the CloudAI Fusion SDK.
//
// A Client is safe for concurrent use by multiple goroutines and should be
// reused across the lifetime of an application rather than created per request.
// External developers use this to interact with the platform.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client

	// Sub-clients for each module. They share this Client's transport and
	// credentials, so configuring the parent configures them all.
	Evidence *EvidenceClient
	GPU      *GPUClient
	Security *SecurityClient
	Billing  *BillingClient
}

// Option configures a Client during construction. Options are applied in the
// order they are passed to New, so later options win on conflict.
type Option func(*Client)

// WithAPIKey sets the API key used to authenticate requests. Keys are sent as a
// Bearer token in the Authorization header.
//
//	client := sdk.New(baseURL, sdk.WithAPIKey("caf_live_xxx"))
func WithAPIKey(key string) Option {
	return func(c *Client) {
		c.apiKey = key
	}
}

// WithTimeout sets the per-request timeout on the client's default HTTP client.
// It has no effect when a custom HTTP client is supplied via WithHTTPClient.
//
//	client := sdk.New(baseURL, sdk.WithTimeout(10*time.Second))
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if c.httpClient != nil {
			c.httpClient.Timeout = d
		}
	}
}

// WithHTTPClient replaces the underlying *http.Client. Use this to plug in
// custom transports, proxies, TLS configuration, or instrumentation.
//
//	client := sdk.New(baseURL, sdk.WithHTTPClient(&http.Client{Timeout: time.Minute}))
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// New creates a new CloudAI Fusion client bound to baseURL.
//
// Usage:
//
//	client := sdk.New("https://api.cloudai.io", sdk.WithAPIKey("caf_xxx"))
//
// A trailing slash on baseURL is trimmed so callers may pass either form. The
// returned Client is ready to use; its sub-clients are wired automatically.
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: DefaultTimeout},
	}

	for _, opt := range opts {
		opt(c)
	}

	// Wire the module sub-clients. Each holds a back-reference so it can reuse
	// the shared transport and credentials.
	c.Evidence = &EvidenceClient{client: c}
	c.GPU = &GPUClient{client: c}
	c.Security = &SecurityClient{client: c}
	c.Billing = &BillingClient{client: c}

	return c
}

// APIError represents a non-2xx response returned by the CloudAI Fusion API.
// It is returned by every SDK method when the server rejects a request, so
// callers can branch on StatusCode or inspect the server-supplied Message.
type APIError struct {
	// StatusCode is the HTTP status code of the failing response.
	StatusCode int `json:"-"`
	// Code is the machine-readable error code from the API body, when present.
	Code string `json:"code"`
	// Message is the human-readable error description from the API body.
	Message string `json:"message"`
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("cloudai-fusion: %d %s: %s", e.StatusCode, e.Code, e.Message)
	}
	return fmt.Sprintf("cloudai-fusion: %d: %s", e.StatusCode, e.Message)
}

// ListOptions carries common pagination and filtering parameters shared by the
// list-style endpoints across modules.
type ListOptions struct {
	// Limit caps the number of entries returned. Zero means server default.
	Limit int
	// Offset skips the given number of entries for pagination.
	Offset int
	// Namespace optionally scopes the listing to a single namespace.
	Namespace string
}

// query renders the options as URL query parameters, omitting zero values.
func (o *ListOptions) query() url.Values {
	v := url.Values{}
	if o == nil {
		return v
	}
	if o.Limit > 0 {
		v.Set("limit", fmt.Sprintf("%d", o.Limit))
	}
	if o.Offset > 0 {
		v.Set("offset", fmt.Sprintf("%d", o.Offset))
	}
	if o.Namespace != "" {
		v.Set("namespace", o.Namespace)
	}
	return v
}

// do performs an HTTP request against the API, marshaling body as JSON (when
// non-nil) and decoding a successful JSON response into out (when non-nil).
//
// It centralizes authentication, headers, and error handling for every module
// sub-client, keeping the individual methods small and consistent.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("cloudai-fusion: encode request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return fmt.Errorf("cloudai-fusion: build request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("cloudai-fusion: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("cloudai-fusion: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseAPIError(resp.StatusCode, data)
	}

	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("cloudai-fusion: decode response: %w", err)
		}
	}
	return nil
}

// parseAPIError builds an *APIError from a failing response, falling back to the
// raw body when it is not valid JSON.
func parseAPIError(status int, body []byte) *APIError {
	apiErr := &APIError{StatusCode: status}
	if len(body) > 0 {
		// Best effort: the server may return a structured error, or plain text.
		if err := json.Unmarshal(body, apiErr); err != nil || apiErr.Message == "" {
			apiErr.Message = strings.TrimSpace(string(body))
		}
	}
	if apiErr.Message == "" {
		apiErr.Message = http.StatusText(status)
	}
	return apiErr
}
