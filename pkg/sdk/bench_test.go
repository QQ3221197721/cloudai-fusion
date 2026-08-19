package sdk

// bench_test.go measures the real cost of Module 38 (the official Go SDK) on the
// machine running `go test -bench`.
//
// Measurement honesty notes:
//
//   - Every call benchmark runs against a local httptest server over loopback.
//     The reported ns/op therefore includes a real TCP/HTTP round trip to
//     127.0.0.1 — it is NOT a wide-area latency figure and must never be
//     presented as one. Loopback is used to remove network variance so the
//     SDK's own CPU/alloc cost is visible.
//
//   - BenchmarkRawHTTPGetDecode / BenchmarkRawHTTPPostDecode are the controls:
//     hand-rolled net/http + encoding/json doing exactly the same work against
//     an identical server, with an http.Client configured like the SDK's
//     (30s timeout, default transport). SDK - control = our layer's overhead.
//     This is the same-machine, same-condition comparison; it is the only
//     comparison here that carries no hardware caveat.
//
//   - Sub-client handlers return canned payloads. Server-side business logic is
//     deliberately excluded: this measures the client library, not the platform.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// benchReceiptHash is a canned SHA-256 hex digest used in response fixtures.
const benchReceiptHash = "a591a6d40bf420404a011733cfb7b190d62c65bf0bcda32b57b277d9ad9f146e"

// jsonServer starts an httptest server that replies with the given payload
// encoded as JSON, optionally asserting the request first.
func jsonServer(b *testing.B, assert func(*http.Request), payload any) *httptest.Server {
	b.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if assert != nil {
			assert(r)
		}
		// Drain the body so connection reuse behaves like a real server.
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(payload); err != nil {
			b.Errorf("encode response: %v", err)
		}
	}))
	b.Cleanup(srv.Close)
	return srv
}

// benchHTTPClient mirrors the http.Client the SDK builds in New(), so the
// hand-rolled controls are not accidentally given a different transport.
func benchHTTPClient() *http.Client {
	return &http.Client{Timeout: DefaultTimeout}
}

// ---------------------------------------------------------------------------
// Client construction
// ---------------------------------------------------------------------------

// BenchmarkNew measures bare client construction: URL trim, http.Client
// allocation, and wiring of the four sub-clients.
func BenchmarkNew(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		c := New("https://api.cloudai.io/")
		if c.Evidence == nil || c.GPU == nil || c.Security == nil || c.Billing == nil {
			b.Fatal("sub-clients not wired")
		}
	}
}

// BenchmarkNewWithAPIKey measures construction with the authentication option.
func BenchmarkNewWithAPIKey(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		c := New("https://api.cloudai.io", WithAPIKey("caf_live_xxxxxx"))
		if c.apiKey == "" {
			b.Fatal("api key not applied")
		}
	}
}

// BenchmarkNewWithAllOptions measures construction with the full option chain,
// including replacing the transport — the realistic production wiring.
func BenchmarkNewWithAllOptions(b *testing.B) {
	transport := &http.Transport{MaxIdleConnsPerHost: 100}
	hc := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	b.ReportAllocs()
	for b.Loop() {
		c := New("https://api.cloudai.io",
			WithAPIKey("caf_live_xxxxxx"),
			WithTimeout(10*time.Second),
			WithHTTPClient(hc),
		)
		if c.httpClient != hc {
			b.Fatal("custom client not applied")
		}
	}
}

// ---------------------------------------------------------------------------
// Per-call CPU work, isolated from any I/O
// ---------------------------------------------------------------------------

// BenchmarkMarshalGPUJob measures the request-body serialization SubmitJob
// performs for every submission.
func BenchmarkMarshalGPUJob(b *testing.B) {
	job := &GPUJob{
		Name:      "train-bert-large",
		GPUCount:  8,
		Image:     "nvcr.io/nvidia/pytorch:24.01-py3",
		Command:   []string{"torchrun", "--nproc_per_node=8", "train.py"},
		Namespace: "tenant-research",
		Priority:  5,
	}
	b.ReportAllocs()
	for b.Loop() {
		data, err := json.Marshal(job)
		if err != nil {
			b.Fatalf("marshal: %v", err)
		}
		if len(data) == 0 {
			b.Fatal("empty body")
		}
	}
}

// BenchmarkMarshalUsageRecord measures billing-record serialization.
func BenchmarkMarshalUsageRecord(b *testing.B) {
	rec := &UsageRecord{
		ResourceID: "gpu-abc123", Namespace: "tenant-xyz",
		Category: "gpu", Amount: 1.75, Unit: "hour", Timestamp: time.Now().UTC(),
	}
	b.ReportAllocs()
	for b.Loop() {
		data, err := json.Marshal(rec)
		if err != nil {
			b.Fatalf("marshal: %v", err)
		}
		if len(data) == 0 {
			b.Fatal("empty body")
		}
	}
}

// BenchmarkBuildRequest measures the request-construction half of Client.do:
// context binding, URL join, header set, bearer auth.
func BenchmarkBuildRequest(b *testing.B) {
	ctx := context.Background()
	body := []byte(`{"name":"job-1","gpuCount":2,"image":"nvcr.io/pytorch:24.01"}`)
	b.ReportAllocs()
	for b.Loop() {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			"https://api.cloudai.io/api/v1/gpu/jobs", bytes.NewReader(body))
		if err != nil {
			b.Fatalf("build request: %v", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer caf_live_xxxxxx")
	}
}

// BenchmarkListOptionsQuery measures pagination/filter parameter rendering.
func BenchmarkListOptionsQuery(b *testing.B) {
	opts := &ListOptions{Limit: 100, Offset: 2000, Namespace: "prod/us-east"}
	b.ReportAllocs()
	for b.Loop() {
		if q := opts.query(); len(q) != 3 {
			b.Fatalf("expected 3 params, got %d", len(q))
		}
	}
}

// BenchmarkNamespaceEscape measures the path escaping Evidence.Verify applies to
// namespaces containing separators.
func BenchmarkNamespaceEscape(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if url.QueryEscape("prod/us-east") != "prod%2Fus-east" {
			b.Fatal("unexpected escaping")
		}
	}
}

// BenchmarkParseAPIErrorJSON measures decoding a structured server error.
func BenchmarkParseAPIErrorJSON(b *testing.B) {
	body := []byte(`{"code":"CHAIN_BROKEN","message":"evidence chain broken at entry ev-8821"}`)
	b.ReportAllocs()
	for b.Loop() {
		err := parseAPIError(http.StatusConflict, body)
		if err.Code != "CHAIN_BROKEN" {
			b.Fatalf("unexpected error: %+v", err)
		}
	}
}

// BenchmarkParseAPIErrorPlaintext measures the non-JSON fallback path.
func BenchmarkParseAPIErrorPlaintext(b *testing.B) {
	body := []byte("502 Bad Gateway: upstream evidence service unavailable")
	b.ReportAllocs()
	for b.Loop() {
		err := parseAPIError(http.StatusBadGateway, body)
		if err.Message == "" {
			b.Fatal("no message extracted")
		}
	}
}

// ---------------------------------------------------------------------------
// End-to-end sub-client calls over loopback (4 sub-clients, 8 operations)
// ---------------------------------------------------------------------------

func BenchmarkEvidenceVerify(b *testing.B) {
	srv := jsonServer(b, nil, VerifyResult{
		Valid: true, EntryCount: 1024, Namespace: "prod/us-east", RootHash: benchReceiptHash,
	})
	c := New(srv.URL, WithAPIKey("caf_live_bench"))
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		res, err := c.Evidence.Verify(ctx, "prod/us-east")
		if err != nil {
			b.Fatalf("verify: %v", err)
		}
		if !res.Valid || res.EntryCount != 1024 {
			b.Fatalf("unexpected result: %+v", res)
		}
	}
}

func BenchmarkEvidenceAttest(b *testing.B) {
	srv := jsonServer(b, nil, AttestResult{
		ID: "att-9931", Hash: benchReceiptHash, Signature: "MEUCIQ", Timestamp: time.Now().UTC(),
	})
	c := New(srv.URL, WithAPIKey("caf_live_bench"))
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		res, err := c.Evidence.Attest(ctx, "config.set db_max_open_conns=50")
		if err != nil {
			b.Fatalf("attest: %v", err)
		}
		if res.ID == "" {
			b.Fatal("missing attestation id")
		}
	}
}

func BenchmarkEvidenceList(b *testing.B) {
	entries := make([]*EvidenceEntry, 20)
	for i := range entries {
		entries[i] = &EvidenceEntry{
			ID: "ev-" + benchReceiptHash[:8], Namespace: "prod", Statement: "gpu.allocate",
			Hash: benchReceiptHash, PrevHash: benchReceiptHash, Timestamp: time.Now().UTC(),
		}
	}
	srv := jsonServer(b, nil, entries)
	c := New(srv.URL, WithAPIKey("caf_live_bench"))
	ctx := context.Background()
	opts := &ListOptions{Limit: 20, Offset: 40, Namespace: "prod"}
	b.ReportAllocs()
	for b.Loop() {
		out, err := c.Evidence.List(ctx, opts)
		if err != nil {
			b.Fatalf("list: %v", err)
		}
		if len(out) != 20 {
			b.Fatalf("expected 20 entries, got %d", len(out))
		}
	}
}

func BenchmarkGPUSubmitJob(b *testing.B) {
	srv := jsonServer(b, nil, JobResult{
		ID: "job-4417", Status: "pending",
		AssignedGPUs: []string{"gpu-0", "gpu-1"}, SubmittedAt: time.Now().UTC(),
	})
	c := New(srv.URL, WithAPIKey("caf_live_bench"))
	ctx := context.Background()
	job := &GPUJob{Name: "train-bert", GPUCount: 2, Image: "nvcr.io/pytorch:24.01"}
	b.ReportAllocs()
	for b.Loop() {
		res, err := c.GPU.SubmitJob(ctx, job)
		if err != nil {
			b.Fatalf("submit: %v", err)
		}
		if res.ID != "job-4417" {
			b.Fatalf("unexpected result: %+v", res)
		}
	}
}

func BenchmarkGPUListGPUs(b *testing.B) {
	gpus := make([]*GPUInfo, 8)
	for i := range gpus {
		gpus[i] = &GPUInfo{UUID: "gpu-uuid", Model: "NVIDIA H100 80GB", MemoryMB: 81920, Node: "node-1"}
	}
	srv := jsonServer(b, nil, gpus)
	c := New(srv.URL, WithAPIKey("caf_live_bench"))
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		out, err := c.GPU.ListGPUs(ctx)
		if err != nil {
			b.Fatalf("list gpus: %v", err)
		}
		if len(out) != 8 {
			b.Fatalf("expected 8 gpus, got %d", len(out))
		}
	}
}

func BenchmarkGPUGetTopology(b *testing.B) {
	topo := Topology{
		GPUs: []GPUInfo{
			{UUID: "gpu-0", Model: "NVIDIA H100 80GB", MemoryMB: 81920, Node: "node-1"},
			{UUID: "gpu-1", Model: "NVIDIA H100 80GB", MemoryMB: 81920, Node: "node-1"},
		},
		Links: []TopologyLink{
			{Source: "gpu-0", Target: "gpu-1", Type: "nvlink", BandwidthGBps: 600},
		},
	}
	srv := jsonServer(b, nil, topo)
	c := New(srv.URL, WithAPIKey("caf_live_bench"))
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		out, err := c.GPU.GetTopology(ctx)
		if err != nil {
			b.Fatalf("topology: %v", err)
		}
		if len(out.GPUs) != 2 || len(out.Links) != 1 {
			b.Fatalf("unexpected topology: %+v", out)
		}
	}
}

func BenchmarkSecurityRunCampaign(b *testing.B) {
	srv := jsonServer(b, nil, Campaign{ID: "camp-77", Status: "running", FindingsCount: 3})
	c := New(srv.URL, WithAPIKey("caf_live_bench"))
	ctx := context.Background()
	cfg := &CampaignConfig{
		Name:       "quarterly-red-team",
		Frameworks: []string{"MITRE-ATT&CK"},
		Scope:      []string{"api", "edge", "mesh"},
	}
	b.ReportAllocs()
	for b.Loop() {
		out, err := c.Security.RunCampaign(ctx, cfg)
		if err != nil {
			b.Fatalf("campaign: %v", err)
		}
		if out.ID != "camp-77" {
			b.Fatalf("unexpected campaign: %+v", out)
		}
	}
}

func BenchmarkSecurityGetCoverage(b *testing.B) {
	srv := jsonServer(b, nil, Coverage{
		Namespace: "prod", Mappings: map[string]int{"MITRE-ATT&CK": 187},
		TotalFrameworks: 1, HealthScore: 82, LastUpdated: time.Now().UTC(),
	})
	c := New(srv.URL, WithAPIKey("caf_live_bench"))
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		out, err := c.Security.GetCoverage(ctx)
		if err != nil {
			b.Fatalf("coverage: %v", err)
		}
		if out.HealthScore != 82 {
			b.Fatalf("unexpected coverage: %+v", out)
		}
	}
}

func BenchmarkBillingRecordUsage(b *testing.B) {
	srv := jsonServer(b, nil, BillingReceipt{
		ID: "rcpt-2210", Amount: 1.75, Unit: "hour", ReceiptHash: benchReceiptHash,
		SignedAt: time.Now().UTC(), Signature: "MEUCIQ", ResourceID: "gpu-abc123",
	})
	c := New(srv.URL, WithAPIKey("caf_live_bench"))
	ctx := context.Background()
	usage := &UsageRecord{ResourceID: "gpu-abc123", Category: "gpu", Amount: 1.75, Unit: "hour"}
	b.ReportAllocs()
	for b.Loop() {
		receipt, err := c.Billing.RecordUsage(ctx, usage)
		if err != nil {
			b.Fatalf("record usage: %v", err)
		}
		if receipt.ReceiptHash != benchReceiptHash {
			b.Fatalf("unexpected receipt: %+v", receipt)
		}
	}
}

// BenchmarkEvidenceVerifyParallel measures whether one shared Client scales
// across goroutines (it documents itself as safe for concurrent use).
func BenchmarkEvidenceVerifyParallel(b *testing.B) {
	srv := jsonServer(b, nil, VerifyResult{Valid: true, EntryCount: 1024, Namespace: "prod"})
	c := New(srv.URL, WithAPIKey("caf_live_bench"))
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		for pb.Next() {
			if _, err := c.Evidence.Verify(ctx, "prod"); err != nil {
				b.Errorf("verify: %v", err)
				return
			}
		}
	})
}

// ---------------------------------------------------------------------------
// Hand-rolled net/http controls (the SDK-overhead measurement)
// ---------------------------------------------------------------------------

// BenchmarkRawHTTPGetDecode is the control for BenchmarkEvidenceVerify: the same
// GET, query escaping, read and JSON decode, written by hand against the same
// server. The delta is what the SDK layer costs a caller.
func BenchmarkRawHTTPGetDecode(b *testing.B) {
	srv := jsonServer(b, nil, VerifyResult{
		Valid: true, EntryCount: 1024, Namespace: "prod/us-east", RootHash: benchReceiptHash,
	})
	hc := benchHTTPClient()
	ctx := context.Background()
	target := srv.URL + "/api/v1/evidence/verify?namespace=" + url.QueryEscape("prod/us-east")

	b.ReportAllocs()
	for b.Loop() {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			b.Fatalf("build request: %v", err)
		}
		req.Header.Set("Accept", "application/json")
		resp, err := hc.Do(req)
		if err != nil {
			b.Fatalf("do: %v", err)
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			b.Fatalf("read: %v", err)
		}
		var out VerifyResult
		if err := json.Unmarshal(data, &out); err != nil {
			b.Fatalf("decode: %v", err)
		}
		if !out.Valid {
			b.Fatal("expected valid chain")
		}
	}
}

// BenchmarkRawHTTPPostDecode is the control for BenchmarkGPUSubmitJob: marshal
// the body, build the request with headers, POST, read and decode by hand.
func BenchmarkRawHTTPPostDecode(b *testing.B) {
	srv := jsonServer(b, nil, JobResult{
		ID: "job-4417", Status: "pending",
		AssignedGPUs: []string{"gpu-0", "gpu-1"}, SubmittedAt: time.Now().UTC(),
	})
	hc := benchHTTPClient()
	ctx := context.Background()
	job := &GPUJob{Name: "train-bert", GPUCount: 2, Image: "nvcr.io/pytorch:24.01"}
	target := srv.URL + "/api/v1/gpu/jobs"

	b.ReportAllocs()
	for b.Loop() {
		body, err := json.Marshal(job)
		if err != nil {
			b.Fatalf("marshal: %v", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
		if err != nil {
			b.Fatalf("build request: %v", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")
		resp, err := hc.Do(req)
		if err != nil {
			b.Fatalf("do: %v", err)
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			b.Fatalf("read: %v", err)
		}
		var out JobResult
		if err := json.Unmarshal(data, &out); err != nil {
			b.Fatalf("decode: %v", err)
		}
		if out.ID != "job-4417" {
			b.Fatalf("unexpected result: %+v", out)
		}
	}
}
