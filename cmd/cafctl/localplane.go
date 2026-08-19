// Package main - the embedded, zero-dependency local control plane behind
// `cafctl up --local`.
//
// Honesty contract (this is the whole point of the file):
//   - It is NOT the full apiserver. cmd/apiserver needs a real DB store,
//     messaging, cluster and evidence backends before it will boot; requiring
//     those would break the "no credentials, no daemons" promise. This plane
//     serves the subset that can run truthfully with nothing installed and says
//     so on every surface (/api/v1/capabilities, the boot banner, --json).
//   - Compute operations are backed by pkg/cloudprovider's LocalMockProvider —
//     the project's existing zero-credential backend with real CRUD — not a new
//     mock invented here.
//   - Every mutating call is recorded into a real evidence ledger (in-memory
//     store + Ed25519 signer), so `cafctl verify` semantics still hold locally.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/cloudprovider"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// localPlaneComponent is the component name reported by the health endpoints.
const localPlaneComponent = "cafctl-local-plane"

// localPlaneConfig configures an embedded plane. Zero values are usable: port
// defaults to defaultLocalPort and Dir to the current directory.
type localPlaneConfig struct {
	Port int    // TCP port to bind on 127.0.0.1
	Dir  string // project directory holding .caf/
}

// defaultLocalPort matches the port cafctl status probes, so a plane started
// by cafctl up --local is visible to the existing status command.
const defaultLocalPort = 8080

// localPlane is a running (or ready-to-run) embedded control plane.
type localPlane struct {
	cfg      localPlaneConfig
	provider *cloudprovider.LocalMockProvider
	ledger   *evidence.Ledger
	// signerSource records where the signing identity came from so the boot
	// banner can be truthful: "project key" vs "ephemeral".
	signerSource string
	started      time.Time

	mu  sync.Mutex
	srv *http.Server
	ln  net.Listener
}

// newLocalPlane builds a plane with a LocalMockProvider and a real evidence
// ledger. It performs no network I/O, so tests can drive Handler() directly
// through httptest without binding a port.
func newLocalPlane(cfg localPlaneConfig) (*localPlane, error) {
	if cfg.Port == 0 {
		cfg.Port = defaultLocalPort
	}
	if cfg.Dir == "" {
		cfg.Dir = "."
	}

	signer, source, err := localPlaneSigner(cfg.Dir)
	if err != nil {
		return nil, err
	}
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{
		Store:    evidence.NewMemoryStore(),
		Signer:   signer,
		Anchorer: evidence.NewSimulatedAnchorer(),
	})
	if err != nil {
		return nil, fmt.Errorf("build evidence ledger: %w", err)
	}

	return &localPlane{
		cfg: cfg,
		// No simulated network latency: this backend stands in for a local
		// control plane, so instant responses are the honest behavior. The
		// latency profiles exist for benchmarking cloud round-trips.
		provider:     cloudprovider.NewLocalMockProvider(cloudprovider.WithoutLatency()),
		ledger:       ledger,
		signerSource: source,
		started:      time.Now(),
	}, nil
}

// localPlaneSigner reuses the project signing key written by cafctl init when
// present, so locally recorded evidence chains verify against .caf/public.pem.
// Without a project key it falls back to an ephemeral signer and reports that.
func localPlaneSigner(dir string) (evidence.Signer, string, error) {
	keyPath := filepath.Clean(filepath.Join(dir, ".caf", "keys", "private.pem"))
	if pemBytes, err := os.ReadFile(keyPath); err == nil {
		if signer, serr := evidence.NewSignerFromPEM(pemBytes); serr == nil {
			return signer, "project key " + filepath.ToSlash(keyPath), nil
		}
	}
	signer, err := evidence.GenerateEphemeralSigner()
	if err != nil {
		return nil, "", fmt.Errorf("generate signer: %w", err)
	}
	return signer, "ephemeral (run 'cafctl init' to persist a project key)", nil
}

// BaseURL is the address clients should use to reach this plane.
func (p *localPlane) BaseURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", p.cfg.Port)
}

// ----------------------------------------------------------------------------
// HTTP surface
// ----------------------------------------------------------------------------

// localPlaneEndpoint documents one route for the boot banner and quickstart doc.
type localPlaneEndpoint struct {
	Path string
	Desc string
}

// localPlaneEndpoints is the advertised surface. It doubles as the boot banner
// content so the printed endpoints can never drift from the served routes.
func localPlaneEndpoints() []localPlaneEndpoint {
	return []localPlaneEndpoint{
		{"/healthz", "liveness probe (also served at /health)"},
		{"/readyz", "readiness probe with per-check detail"},
		{"/version", "build/runtime version"},
		{"/api/v1/capabilities", "honest real-vs-simulated backend report"},
		{"/api/v1/cloud/providers", "registered providers (localmock)"},
		{"/api/v1/cloud/instances", "GET list / POST create (attested)"},
		{"/api/v1/evidence/chain", "evidence chain head + record count"},
	}
}

// Handler builds the route table. Method checks are explicit so behavior does
// not depend on ServeMux pattern features.
func (p *localPlane) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", p.handleHealth)
	mux.HandleFunc("/health", p.handleHealth)
	mux.HandleFunc("/readyz", p.handleReady)
	mux.HandleFunc("/version", p.handleVersion)
	mux.HandleFunc("/api/v1/capabilities", p.handleCapabilities)
	mux.HandleFunc("/api/v1/cloud/providers", p.handleProviders)
	mux.HandleFunc("/api/v1/cloud/instances", p.handleInstances)
	mux.HandleFunc("/api/v1/evidence/chain", p.handleEvidenceChain)
	mux.HandleFunc("/", p.handleIndex)
	return mux
}

func writeHTTPJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func (p *localPlane) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeHTTPJSON(w, http.StatusNotFound, map[string]string{"error": "not found", "path": r.URL.Path})
		return
	}
	routes := make([]map[string]string, 0, len(localPlaneEndpoints()))
	for _, e := range localPlaneEndpoints() {
		routes = append(routes, map[string]string{"path": e.Path, "description": e.Desc})
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"component": localPlaneComponent,
		"mode":      "local-embedded",
		"note":      "zero-dependency subset of the CloudAI Fusion control plane; not the full apiserver",
		"routes":    routes,
	})
}

func (p *localPlane) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"status":         "healthy",
		"component":      localPlaneComponent,
		"mode":           "local-embedded",
		"uptime_seconds": int(time.Since(p.started).Seconds()),
	})
}

// readyCheck is one readiness sub-check with an actionable detail string.
type readyCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// readiness evaluates every sub-check. It is separated from the HTTP layer so
// tests can assert on the checks without a server.
func (p *localPlane) readiness(ctx context.Context) (bool, []readyCheck) {
	checks := make([]readyCheck, 0, 3)

	caps := p.provider.Capabilities()
	providerOK := caps.Online && caps.CredentialStatus == cloudprovider.CredentialsSatisfied
	checks = append(checks, readyCheck{
		Name:   "provider.localmock",
		OK:     providerOK,
		Detail: fmt.Sprintf("%s: %s (%s)", caps.Provider, caps.CredentialStatus, caps.Notes),
	})

	// A real list call: proves the provider serves traffic, not just reports.
	instances, err := p.provider.ListInstances(ctx)
	listOK := err == nil
	detail := fmt.Sprintf("%d instance(s) in memory", len(instances))
	if err != nil {
		detail = "list failed: " + err.Error()
	}
	checks = append(checks, readyCheck{Name: "provider.list", OK: listOK, Detail: detail})

	// The evidence ledger must be able to read back its own chain.
	bundle, eerr := p.ledger.Export(ctx)
	ledgerOK := eerr == nil
	ledgerDetail := "export failed: " + FormatError(eerr)
	if eerr == nil {
		ledgerDetail = fmt.Sprintf("%d record(s), signer: %s", len(bundle.Records), p.signerSource)
	} else {
		ledgerDetail = "export failed: " + FormatError(eerr)
	}
	checks = append(checks, readyCheck{Name: "evidence.ledger", OK: ledgerOK, Detail: ledgerDetail})

	ready := true
	for _, c := range checks {
		if !c.OK {
			ready = false
		}
	}
	return ready, checks
}

func (p *localPlane) handleReady(w http.ResponseWriter, r *http.Request) {
	ready, checks := p.readiness(r.Context())
	code := http.StatusOK
	status := "ready"
	if !ready {
		code = http.StatusServiceUnavailable
		status = "not-ready"
	}
	writeHTTPJSON(w, code, map[string]any{
		"status": status,
		"checks": checks,
	})
}

func (p *localPlane) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"component":  localPlaneComponent,
		"version":    rootCmd.Version,
		"go_version": runtime.Version(),
		"platform":   runtime.GOOS + "/" + runtime.GOARCH,
	})
}

// localPlaneCapabilities builds the honest capability report. It mirrors the
// /api/v1/capabilities contract that pkg/api serves (and that cafctl status
// already parses), so the local plane plugs into existing tooling.
func (p *localPlane) localPlaneCapabilities() CapabilitiesResponse {
	caps := p.provider.Capabilities()
	env := scanEnvironment()

	backends := []Backend{
		{
			Component: "cloudprovider",
			Mode:      "real",
			Driver:    string(caps.Provider),
			Detail:    "pkg/cloudprovider LocalMockProvider — real in-memory CRUD, zero credentials",
		},
		{
			Component: "evidence.store",
			Mode:      "simulated",
			Driver:    "memory",
			Detail:    "in-memory chain; export to .caf/evidence.chain for durable proof",
		},
		{
			Component: "evidence.anchor",
			Mode:      "simulated",
			Driver:    "simulated-anchorer",
			Detail:    "no Rekor transparency log configured (needs network + Rekor URL)",
		},
	}

	clusterBackend := Backend{Component: "cluster", Mode: "simulated", Driver: "none", Detail: env.Kubeconfig.Hint}
	if env.Kubeconfig.Available {
		clusterBackend = Backend{Component: "cluster", Mode: "simulated", Driver: "kubeconfig-present",
			Detail: "kubeconfig found (" + env.Kubeconfig.Detail + ") but the local plane does not talk to it; use the apiserver for real cluster ops"}
	}
	backends = append(backends, clusterBackend)

	gpuBackend := Backend{Component: "gpu", Mode: "simulated", Driver: "none", Detail: env.GPU.Hint}
	if env.GPU.Available {
		gpuBackend = Backend{Component: "gpu", Mode: "real", Driver: "nvidia-smi", Detail: env.GPU.Detail}
	}
	backends = append(backends, gpuBackend)

	simulated := make([]Backend, 0, len(backends))
	for _, b := range backends {
		if b.Mode == "simulated" {
			simulated = append(simulated, b)
		}
	}

	return CapabilitiesResponse{
		RunMode:        "simulation",
		AllReal:        len(simulated) == 0,
		SimulatedCount: len(simulated),
		Backends:       backends,
		Simulated:      simulated,
	}
}

func (p *localPlane) handleCapabilities(w http.ResponseWriter, _ *http.Request) {
	writeHTTPJSON(w, http.StatusOK, p.localPlaneCapabilities())
}

func (p *localPlane) handleProviders(w http.ResponseWriter, _ *http.Request) {
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"providers": []cloudprovider.Capabilities{p.provider.Capabilities()},
		"note":      "credentialed clouds (aws/azure/gcp) are unavailable without credentials; see 'cafctl cloud provider-list'",
	})
}

// handleInstances serves GET (list) and POST (create). Creates are attested into
// the evidence ledger, mirroring the rest of the platform's write paths.
func (p *localPlane) handleInstances(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		instances, err := p.provider.ListInstances(ctx)
		if err != nil {
			writeHTTPJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeHTTPJSON(w, http.StatusOK, map[string]any{"count": len(instances), "instances": instances})
	case http.MethodPost:
		var req cloudprovider.CreateInstanceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeHTTPJSON(w, http.StatusBadRequest, map[string]string{
				"error": "invalid JSON body: " + err.Error(),
				"hint":  `send {"name":"demo","type":"t3.micro"}`,
			})
			return
		}
		id, err := p.provider.CreateInstance(ctx, req)
		if err != nil {
			writeHTTPJSON(w, http.StatusBadRequest, map[string]string{
				"error": err.Error(),
				"hint":  "field 'type' is required, e.g. t3.micro",
			})
			return
		}
		rec, rerr := p.ledger.Record(ctx, evidence.RecordInput{
			Actor:   "cafctl up --local",
			Action:  "localplane.instance.create",
			Subject: id,
			Input:   map[string]string{"type": req.Type, "region": req.Region, "name": req.Name},
			Output:  map[string]string{"instance_id": id},
		})
		resp := map[string]any{"instance_id": id, "attested": rerr == nil}
		if rerr == nil {
			resp["evidence_hash"] = rec.Hash
		} else {
			resp["attestation_error"] = rerr.Error()
		}
		writeHTTPJSON(w, http.StatusCreated, resp)
	default:
		writeHTTPJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "use GET or POST"})
	}
}

func (p *localPlane) handleEvidenceChain(w http.ResponseWriter, r *http.Request) {
	bundle, err := p.ledger.Export(r.Context())
	if err != nil {
		writeHTTPJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	head := ""
	if len(bundle.Records) > 0 {
		head = bundle.Records[len(bundle.Records)-1].Hash
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{
		"records":       len(bundle.Records),
		"head_hash":     head,
		"key_id":        bundle.KeyID,
		"signer_source": p.signerSource,
	})
}

// ----------------------------------------------------------------------------
// Lifecycle
// ----------------------------------------------------------------------------

// Start binds the port and begins serving. It records a boot attestation first
// so /readyz has a non-empty chain to report, and returns an actionable error
// when the port is already taken.
func (p *localPlane) Start(ctx context.Context) error {
	if _, err := p.ledger.Record(ctx, evidence.RecordInput{
		Actor:   "cafctl up --local",
		Action:  "localplane.boot",
		Subject: p.BaseURL(),
		Input:   map[string]string{"dir": p.cfg.Dir, "port": fmt.Sprintf("%d", p.cfg.Port)},
		Output:  map[string]string{"mode": "local-embedded"},
	}); err != nil {
		return fmt.Errorf("record boot evidence: %w", err)
	}

	addr := fmt.Sprintf("127.0.0.1:%d", p.cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("bind %s: %w (port busy? retry with --port <free-port>, or run 'cafctl doctor' to see what is listening)", addr, err)
	}

	srv := &http.Server{
		Handler:           p.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	p.mu.Lock()
	p.srv, p.ln = srv, ln
	p.mu.Unlock()

	go func() {
		if serveErr := srv.Serve(ln); serveErr != nil && serveErr != http.ErrServerClosed {
			PrintError("local plane stopped: %v", serveErr)
		}
	}()
	return nil
}

// Stop gracefully shuts the server down. Safe to call when never started.
func (p *localPlane) Stop(ctx context.Context) error {
	p.mu.Lock()
	srv := p.srv
	p.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Shutdown(ctx)
}

// ----------------------------------------------------------------------------
// Self-check
// ----------------------------------------------------------------------------

// probeResult is one endpoint reachability probe from the CLI's own process.
type probeResult struct {
	URL        string `json:"url"`
	StatusCode int    `json:"status_code"`
	LatencyMS  int64  `json:"latency_ms"`
	Body       string `json:"body,omitempty"`
	Err        string `json:"error,omitempty"`
}

// OK reports whether the probe got a 2xx response.
func (r probeResult) OK() bool { return r.Err == "" && r.StatusCode >= 200 && r.StatusCode < 300 }

// probeEndpoint issues a single GET with a short timeout, retrying until the
// deadline so a just-bound listener is not reported as unreachable.
func probeEndpoint(ctx context.Context, url string, deadline time.Duration) probeResult {
	client := &http.Client{Timeout: 2 * time.Second}
	start := time.Now()
	end := start.Add(deadline)
	var last probeResult
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return probeResult{URL: url, Err: err.Error()}
		}
		attempt := time.Now()
		resp, err := client.Do(req)
		if err == nil {
			buf := make([]byte, 4096)
			n, _ := resp.Body.Read(buf)
			_ = resp.Body.Close()
			return probeResult{
				URL:        url,
				StatusCode: resp.StatusCode,
				LatencyMS:  time.Since(attempt).Milliseconds(),
				Body:       strings.TrimSpace(string(buf[:n])),
			}
		}
		last = probeResult{URL: url, Err: err.Error(), LatencyMS: time.Since(attempt).Milliseconds()}
		if time.Now().After(end) {
			return last
		}
		select {
		case <-ctx.Done():
			return last
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// SelfCheck probes the liveness and readiness endpoints over real TCP.
func (p *localPlane) SelfCheck(ctx context.Context, deadline time.Duration) []probeResult {
	base := p.BaseURL()
	return []probeResult{
		probeEndpoint(ctx, base+"/healthz", deadline),
		probeEndpoint(ctx, base+"/readyz", deadline),
	}
}
