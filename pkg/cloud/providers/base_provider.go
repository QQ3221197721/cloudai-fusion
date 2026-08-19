// Package providers implements the Multi-Cloud Unified Interface (Module 2).
package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// genericProvider is the shared client implementation reused by all six vendor
// files. It is a *pure client*: it never mutates state directly. Instead it
// issues real HTTP requests through a mock transport (mockTransport) that plays
// the role of the cloud REST API. The transport owns the resource state and
// guards it with a mutex, exactly mirroring how a live vendor endpoint would
// behave. This makes concurrency (-race) testing meaningful: many goroutines
// can hit ListInstances/CreateInstance/DeleteInstance simultaneously and the
// shared state stays consistent.
//
// To go to production, each vendor file swaps `newMockTransport` for the real
// vendor SDK transport at the `// TODO: 接入 ...` seam — the ComputeAPI /
// StorageAPI / NetworkAPI method bodies here stay unchanged.
type genericProvider struct {
	client  *http.Client
	baseURL string
	name    string
	region  string
}

var _ CloudProvider = (*genericProvider)(nil)

func (p *genericProvider) Name() string          { return p.name }
func (p *genericProvider) DefaultRegion() string { return p.region }

// ---------------------------------------------------------------------------
// ComputeAPI
// ---------------------------------------------------------------------------

func (p *genericProvider) ListInstances(ctx context.Context) ([]Instance, error) {
	var out struct {
		Instances []Instance `json:"instances"`
	}
	if err := p.doJSON(ctx, http.MethodGet, "/compute/instances", nil, &out); err != nil {
		return nil, fmt.Errorf("%s: list instances: %w", p.name, err)
	}
	return out.Instances, nil
}

func (p *genericProvider) CreateInstance(ctx context.Context, req InstanceRequest) (string, error) {
	if req.Type == "" {
		return "", fmt.Errorf("%s: create instance: type is required", p.name)
	}
	if req.Region == "" {
		req.Region = p.region
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := p.doJSON(ctx, http.MethodPost, "/compute/instances", req, &out); err != nil {
		return "", fmt.Errorf("%s: create instance: %w", p.name, err)
	}
	return out.ID, nil
}

func (p *genericProvider) DeleteInstance(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("%s: delete instance: id is required", p.name)
	}
	if err := p.doJSON(ctx, http.MethodDelete, "/compute/instances/"+id, nil, nil); err != nil {
		return fmt.Errorf("%s: delete instance %s: %w", p.name, id, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// StorageAPI
// ---------------------------------------------------------------------------

func (p *genericProvider) ListBuckets(ctx context.Context) ([]Bucket, error) {
	var out struct {
		Buckets []Bucket `json:"buckets"`
	}
	if err := p.doJSON(ctx, http.MethodGet, "/storage/buckets", nil, &out); err != nil {
		return nil, fmt.Errorf("%s: list buckets: %w", p.name, err)
	}
	return out.Buckets, nil
}

func (p *genericProvider) UploadObject(ctx context.Context, bucket, obj string, reader io.Reader) error {
	if bucket == "" || obj == "" {
		return fmt.Errorf("%s: upload object: bucket and object name are required", p.name)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("%s: upload object: read payload: %w", p.name, err)
	}
	// PUT the raw object bytes. The mock transport verifies the bucket exists.
	if err := p.doRaw(ctx, http.MethodPut, "/storage/buckets/"+bucket+"/objects/"+obj, data, nil); err != nil {
		return fmt.Errorf("%s: upload object %s/%s: %w", p.name, bucket, obj, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// NetworkAPI
// ---------------------------------------------------------------------------

func (p *genericProvider) ListVPCs(ctx context.Context) ([]VPC, error) {
	var out struct {
		VPCs []VPC `json:"vpcs"`
	}
	if err := p.doJSON(ctx, http.MethodGet, "/network/vpcs", nil, &out); err != nil {
		return nil, fmt.Errorf("%s: list vpcs: %w", p.name, err)
	}
	return out.VPCs, nil
}

func (p *genericProvider) CreateSecurityGroup(ctx context.Context, rules []SecurityRule) (string, error) {
	if len(rules) == 0 {
		return "", fmt.Errorf("%s: create security group: at least one rule is required", p.name)
	}
	for i, r := range rules {
		if r.Direction != "ingress" && r.Direction != "egress" {
			return "", fmt.Errorf("%s: create security group: rule[%d] invalid direction %q", p.name, i, r.Direction)
		}
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := p.doJSON(ctx, http.MethodPost, "/network/security-groups", rules, &out); err != nil {
		return "", fmt.Errorf("%s: create security group: %w", p.name, err)
	}
	return out.ID, nil
}

// ---------------------------------------------------------------------------
// HTTP plumbing (mock-backed, SDK-swappable)
// ---------------------------------------------------------------------------

// doJSON marshals body as JSON, performs the request, and decodes the JSON
// response into out (out may be nil to ignore the body).
func (p *genericProvider) doJSON(ctx context.Context, method, path string, body, out any) error {
	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		payload = b
	}
	return p.doRaw(ctx, method, path, payload, out)
}

// doRaw performs the request with a raw byte payload.
func (p *genericProvider) doRaw(ctx context.Context, method, path string, payload []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return err // preserves context.Canceled / DeadlineExceeded
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("api error: status %d: %s", resp.StatusCode, string(respBody))
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Mock transport — the synthetic "cloud" that owns resource state.
// ---------------------------------------------------------------------------

// mockTransport is an http.RoundTripper that simulates a vendor REST API. It
// holds the authoritative resource state and guards it with a mutex so
// concurrent goroutines observe consistent state under -race.
//
// TODO: 接入真实 SDK 时删除 mockTransport，改用厂商 SDK 的 http.Client / signer：
//   - AWS  v2: https://docs.aws.amazon.com/sdk-for-go/  (ec2/s3/ec2 SecurityGroup)
//   - Azure  : https://learn.microsoft.com/azure/developer/go/
//   - GCP    : https://pkg.go.dev/cloud.google.com/go/compute
type mockTransport struct {
	vendor string

	mu        sync.Mutex
	instances map[string]Instance
	buckets   map[string]Bucket
	vpcs      map[string]VPC
	seq       int
}

var _ http.RoundTripper = (*mockTransport)(nil)

func (t *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Respect context cancellation exactly like a network client would.
	if err := req.Context().Err(); err != nil {
		return nil, err
	}

	path := req.URL.Path
	var reqBody []byte
	if req.Body != nil {
		reqBody, _ = io.ReadAll(req.Body)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	switch {
	case req.Method == http.MethodGet && path == "/compute/instances":
		list := make([]Instance, 0, len(t.instances))
		for _, v := range t.instances {
			list = append(list, v)
		}
		return jsonResp(200, map[string]any{"instances": list}), nil

	case req.Method == http.MethodPost && path == "/compute/instances":
		var r InstanceRequest
		_ = json.Unmarshal(reqBody, &r)
		t.seq++
		id := fmt.Sprintf("%s-i-%04d", t.vendor, t.seq)
		t.instances[id] = Instance{
			ID:       id,
			Name:     r.Name,
			Type:     r.Type,
			Region:   r.Region,
			State:    "running",
			Metadata: map[string]string{"created_at": time.Now().UTC().Format(time.RFC3339)},
		}
		return jsonResp(201, map[string]any{"id": id}), nil

	case req.Method == http.MethodDelete && hasPrefix(path, "/compute/instances/"):
		id := path[len("/compute/instances/"):]
		if _, ok := t.instances[id]; !ok {
			return jsonResp(404, map[string]any{"error": "instance not found"}), nil
		}
		delete(t.instances, id)
		return jsonResp(204, nil), nil

	case req.Method == http.MethodGet && path == "/storage/buckets":
		list := make([]Bucket, 0, len(t.buckets))
		for _, v := range t.buckets {
			list = append(list, v)
		}
		return jsonResp(200, map[string]any{"buckets": list}), nil

	case req.Method == http.MethodPut && hasPrefix(path, "/storage/buckets/"):
		// path form: /storage/buckets/{bucket}/objects/{obj}
		rest := path[len("/storage/buckets/"):]
		bucket := rest
		if i := indexByte(rest, '/'); i >= 0 {
			bucket = rest[:i]
		}
		if _, ok := t.buckets[bucket]; !ok {
			return jsonResp(404, map[string]any{"error": "bucket not found"}), nil
		}
		return jsonResp(200, map[string]any{"bytes": len(reqBody)}), nil

	case req.Method == http.MethodGet && path == "/network/vpcs":
		list := make([]VPC, 0, len(t.vpcs))
		for _, v := range t.vpcs {
			list = append(list, v)
		}
		return jsonResp(200, map[string]any{"vpcs": list}), nil

	case req.Method == http.MethodPost && path == "/network/security-groups":
		t.seq++
		id := fmt.Sprintf("%s-sg-%04d", t.vendor, t.seq)
		return jsonResp(201, map[string]any{"id": id}), nil

	default:
		return jsonResp(404, map[string]any{"error": "unknown route"}), nil
	}
}

func jsonResp(code int, payload any) *http.Response {
	var body []byte
	if payload != nil {
		body, _ = json.Marshal(payload)
	}
	return &http.Response{
		StatusCode: code,
		Status:     fmt.Sprintf("%d", code),
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// newMockTransport builds a mock transport seeded with a small, realistic
// catalog for the given vendor so List* calls return non-empty data.
func newMockTransport(vendor, region string, seedInstances []Instance, seedBuckets []Bucket, seedVPCs []VPC) *mockTransport {
	t := &mockTransport{
		vendor:    vendor,
		instances: make(map[string]Instance),
		buckets:   make(map[string]Bucket),
		vpcs:      make(map[string]VPC),
	}
	for _, i := range seedInstances {
		t.instances[i.ID] = i
	}
	for _, b := range seedBuckets {
		t.buckets[b.Name] = b
	}
	for _, v := range seedVPCs {
		t.vpcs[v.ID] = v
	}
	return t
}

// newGenericProvider wires a provider to a vendor-seeded mock transport.
func newGenericProvider(cfg ProviderConfig, seedInstances []Instance, seedBuckets []Bucket, seedVPCs []VPC) *genericProvider {
	region := cfg.Region
	transport := newMockTransport(cfg.Name, region, seedInstances, seedBuckets, seedVPCs)
	baseURL := cfg.Endpoint
	if baseURL == "" {
		baseURL = "https://mock." + cfg.Name + ".cloudai-fusion.local"
	}
	return &genericProvider{
		client:  &http.Client{Transport: transport},
		baseURL: baseURL,
		name:    cfg.Name,
		region:  region,
	}
}
