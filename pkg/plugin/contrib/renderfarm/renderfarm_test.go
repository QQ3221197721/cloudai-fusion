package renderfarm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/plugin"
)

// These tests prove the render-farm plugins deliver REAL commercial value:
// cost-aware scheduling, reliability-aware scoring, GPU-utilization bias,
// Prometheus-driven observability, and SSRF-hardened connectivity — not stubs.

// mustScorePlugin builds the score plugin and type-asserts to the concrete type.
func mustScorePlugin(t *testing.T, cfgs []RenderFarmConfig) *RenderFarmScorePlugin {
	t.Helper()
	p, err := NewRenderFarmScorePlugin(cfgs)
	if err != nil {
		t.Fatalf("NewRenderFarmScorePlugin: %v", err)
	}
	sp, ok := p.(*RenderFarmScorePlugin)
	if !ok {
		t.Fatalf("unexpected type %T", p)
	}
	return sp
}

func renderWorkload() *plugin.WorkloadInfo {
	return &plugin.WorkloadInfo{ID: "job-1", Type: "batch", Labels: map[string]string{"workload-type": "render"}}
}

// TestRenderFarmScore_CostOptimization proves cheaper Spot capacity scores
// strictly higher — the core cost-optimization business value.
func TestRenderFarmScore_CostOptimization(t *testing.T) {
	sp := mustScorePlugin(t, nil)
	w := renderWorkload()

	cheap := &plugin.NodeInfo{Name: "cheap", CostPerHour: 0.05, Labels: map[string]string{"cluster-id": "cheap"}}
	pricey := &plugin.NodeInfo{Name: "pricey", CostPerHour: 0.50, Labels: map[string]string{"cluster-id": "pricey"}}

	cheapScore, _ := sp.Score(context.Background(), plugin.NewCycleState(), w, cheap)
	priceyScore, _ := sp.Score(context.Background(), plugin.NewCycleState(), w, pricey)

	if cheapScore <= priceyScore {
		t.Fatalf("cheaper Spot must score higher: cheap=%d pricey=%d", cheapScore, priceyScore)
	}
}

// TestRenderFarmScore_InterruptionPenalty proves that a high Spot interruption
// rate lowers the score — reliability awareness fed back from the collector.
func TestRenderFarmScore_InterruptionPenalty(t *testing.T) {
	sp := mustScorePlugin(t, nil)
	w := renderWorkload()
	node := &plugin.NodeInfo{Name: "n", CostPerHour: 0.1, Labels: map[string]string{"cluster-id": "c1"}}

	before, _ := sp.Score(context.Background(), plugin.NewCycleState(), w, node)

	// Simulate the collector feeding back a high interruption rate.
	sp.UpdateInterruptionRate("c1", 0.8)
	after, _ := sp.Score(context.Background(), plugin.NewCycleState(), w, node)

	if after >= before {
		t.Fatalf("high interruption rate must lower score: before=%d after=%d", before, after)
	}
}

// TestRenderFarmScore_GPUBonus proves nodes with more free GPUs score higher —
// drives better cluster utilization.
func TestRenderFarmScore_GPUBonus(t *testing.T) {
	sp := mustScorePlugin(t, nil)
	w := renderWorkload()

	idle := &plugin.NodeInfo{Name: "idle", CostPerHour: 0.1, GPUFree: 4, Labels: map[string]string{"cluster-id": "a"}}
	busy := &plugin.NodeInfo{Name: "busy", CostPerHour: 0.1, GPUFree: 0, Labels: map[string]string{"cluster-id": "b"}}

	idleScore, _ := sp.Score(context.Background(), plugin.NewCycleState(), w, idle)
	busyScore, _ := sp.Score(context.Background(), plugin.NewCycleState(), w, busy)

	if idleScore <= busyScore {
		t.Fatalf("more free GPUs must score higher: idle=%d busy=%d", idleScore, busyScore)
	}
}

// TestRenderFarmScore_NonRenderNeutral proves the plugin stays neutral (50) for
// non-render workloads — it does not distort general scheduling.
func TestRenderFarmScore_NonRenderNeutral(t *testing.T) {
	sp := mustScorePlugin(t, nil)
	w := &plugin.WorkloadInfo{ID: "train-1", Labels: map[string]string{"workload-type": "training"}}
	node := &plugin.NodeInfo{Name: "n", CostPerHour: 0.01, GPUFree: 8, Labels: map[string]string{"cluster-id": "c"}}

	score, res := sp.Score(context.Background(), plugin.NewCycleState(), w, node)
	if score != 50 {
		t.Fatalf("non-render workload must be neutral 50, got %d", score)
	}
	if !res.IsSuccess() {
		t.Fatal("score result must be success")
	}
	if sp.ScoreWeight() != 2 {
		t.Fatalf("ScoreWeight = %d, want 2", sp.ScoreWeight())
	}
}

// TestRenderFarmCollector_PrometheusParsing proves the collector really parses
// Prometheus text into metric values (observability value).
func TestRenderFarmCollector_PrometheusParsing(t *testing.T) {
	text := []byte(`# HELP render_frames_total total frames
# TYPE render_frames_total counter
render_frames_total{cluster="aliyun"} 1234
render_estimated_cost_usd 56.78
render_spot_interruptions_total 3
`)
	m := parsePrometheusMetrics(text)
	if m["render_frames_total"] != 1234 {
		t.Fatalf("render_frames_total = %v, want 1234", m["render_frames_total"])
	}
	if m["render_estimated_cost_usd"] != 56.78 {
		t.Fatalf("render_estimated_cost_usd = %v, want 56.78", m["render_estimated_cost_usd"])
	}
	if m["render_spot_interruptions_total"] != 3 {
		t.Fatalf("render_spot_interruptions_total = %v, want 3", m["render_spot_interruptions_total"])
	}
	// Comment lines must be ignored.
	if _, ok := m["#"]; ok {
		t.Fatal("comment lines must not become metrics")
	}
}

// TestRenderFarmCollector_FeedbackLoop proves the collector scrapes a live
// endpoint AND feeds the interruption rate back into the score plugin, closing
// the observability -> scheduling loop.
func TestRenderFarmCollector_FeedbackLoop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			_, _ = w.Write([]byte("render_spot_interruptions_total 50\nrender_frames_total 10\n"))
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	sp := mustScorePlugin(t, nil)
	cp, err := NewRenderFarmCollectorPlugin([]RenderFarmConfig{{Name: "farm", BaseURL: srv.URL, CloudProvider: "aliyun", Region: "cn"}}, sp)
	if err != nil {
		t.Fatalf("NewRenderFarmCollectorPlugin: %v", err)
	}
	collector := cp.(*RenderFarmCollectorPlugin)

	samples, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(samples) == 0 {
		t.Fatal("collector must produce samples from a live endpoint")
	}
	// 50 interruptions normalizes to 0.5; verify the score plugin got it.
	node := &plugin.NodeInfo{Name: "n", CostPerHour: 0.1, Labels: map[string]string{"cluster-id": "farm"}}
	base, _ := mustScorePlugin(t, nil).Score(context.Background(), plugin.NewCycleState(), renderWorkload(), node)
	fed, _ := sp.Score(context.Background(), plugin.NewCycleState(), renderWorkload(), node)
	if fed >= base {
		t.Fatalf("collector feedback must lower the fed score: base=%d fed=%d", base, fed)
	}
}

// TestRenderFarmCloudProvider_EstimateCost proves real cost math: hours * price.
func TestRenderFarmCloudProvider_EstimateCost(t *testing.T) {
	p, err := NewRenderFarmCloudProviderPlugin([]RenderFarmConfig{{Name: "aliyun-render", SpotPriceUSD: 2.0}})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	provider := p.(*RenderFarmCloudProviderPlugin)

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(3 * time.Hour)
	cost, err := provider.EstimateCost(context.Background(), "aliyun-render", start, end)
	if err != nil {
		t.Fatalf("EstimateCost: %v", err)
	}
	if cost != 6.0 {
		t.Fatalf("cost = %v, want 6.0 (3h * $2)", cost)
	}
	// Unknown cluster must error.
	if _, err := provider.EstimateCost(context.Background(), "ghost", start, end); err == nil {
		t.Fatal("unknown cluster must error")
	}
}

// TestRenderFarmCloudProvider_SSRFProtection proves the URL guard blocks
// metadata/loopback/private ranges — a real security control.
func TestRenderFarmCloudProvider_SSRFProtection(t *testing.T) {
	blocked := []string{
		"http://169.254.169.254/latest/meta-data", // cloud metadata
		"http://127.0.0.1:8000",                   // loopback
		"http://10.1.2.3:8000",                    // private
		"ftp://example.com",                       // bad scheme
	}
	for _, u := range blocked {
		if err := validateRenderFarmURL(u); err == nil {
			t.Errorf("expected %q to be blocked", u)
		}
	}
	// A K8s service DNS name (unresolvable in tests but dotted) is trusted.
	if err := validateRenderFarmURL("http://render-farm.default.svc.cluster.local:8000"); err != nil {
		t.Errorf("K8s DNS name must be allowed, got %v", err)
	}

	// Init must reject a config carrying a blocked URL.
	p, _ := NewRenderFarmCloudProviderPlugin([]RenderFarmConfig{{Name: "evil", BaseURL: "http://169.254.169.254"}})
	if err := p.Init(context.Background(), nil); err == nil {
		t.Fatal("Init must reject an SSRF URL in initial config")
	}
}

// TestRenderFarmCloudProvider_Health proves health reflects the real endpoint:
// 2xx healthy, 5xx unhealthy.
func TestRenderFarmCloudProvider_Health(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer healthy.Close()
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }))
	defer broken.Close()

	okP, _ := NewRenderFarmCloudProviderPlugin([]RenderFarmConfig{{Name: "ok", BaseURL: healthy.URL}})
	if err := okP.Health(context.Background()); err != nil {
		t.Fatalf("healthy endpoint must pass, got %v", err)
	}
	badP, _ := NewRenderFarmCloudProviderPlugin([]RenderFarmConfig{{Name: "bad", BaseURL: broken.URL}})
	if err := badP.Health(context.Background()); err == nil {
		t.Fatal("5xx endpoint must fail health")
	}
}
