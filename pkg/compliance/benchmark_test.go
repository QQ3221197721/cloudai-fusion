//go:build !no_compliance_bench
// +build !no_compliance_bench

package compliance_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	regolib "github.com/open-policy-agent/opa/rego"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/compliance"
)

// ============================================================================
// M36 COMPLIANCE REPORTER T2 BENCHMARK
// Head-to-head vs OPA Rego for control evaluation - Honest WIN/LOSS
// ============================================================================
// Anti-fiasco rules (MANDATORY):
// 1. Import real competitor: github.com/open-policy-agent/opa/rego ✓
// 2. Count=6 median sampling for statistical significance
// 3. Same work unit both sides: evaluate the SAME N=5 controls against identical resource state
// 4. Compare eval latency ns/op per control, throughput controls/sec, correctness (pass/fail match)
// 5. Benchmark uses -benchtime=2s -count=6 -json to prevent text output eating
// 6. Honest verdict: if OPA wins raw eval speed, admit it; define edge (framework mapping + audit reports)

// TestResourceState provides the identical resource state fed to BOTH engines.
var TestResourceState = compliance.ResourceState{
	TenantID: "tenant-acme-corp",
	Resources: []compliance.CloudResource{
		{Type: "s3_bucket", ID: "prod-db-001", Name: "Production Database", Encrypted: true, PubliclyAccessible: false},
		{Type: "k8s_pod", ID: "web-app-001", Name: "Web Application", Encrypted: false, PubliclyAccessible: true},
		{Type: "api_endpoint", ID: "rest-api-001", Name: "REST API Gateway", Config: map[string]interface{}{"https_enabled": false}},
		{Type: "vm_instance", ID: "app-server-001", Name: "Application Server", Encrypted: true, PubliclyAccessible: false},
		{Type: "database", ID: "postgres-prod", Name: "PostgreSQL Primary", Encrypted: true, PubliclyAccessible: false},
	},
	Logs: []compliance.LogEntry{
		{Timestamp: time.Now(), Source: "auth", Message: "Successful login admin@corp.com", EventType: "login_success", Severity: "info"},
		{Timestamp: time.Now(), Source: "network", Message: "Suspicious outbound traffic detected", EventType: "anomaly", Severity: "warning"},
		{Timestamp: time.Now(), Source: "access_control", Message: "Failed authentication attempt", EventType: "auth_failure", Severity: "error"},
	},
	Users: []compliance.User{
		{ID: "u-001", Username: "admin", Roles: []string{"admin"}, HasMFA: true, IsInactive: false},
		{ID: "u-002", Username: "developer", Roles: []string{"developer"}, HasMFA: false, IsInactive: false},
		{ID: "u-003", Username: "viewer", Roles: []string{"viewer"}, HasMFA: true, IsInactive: false},
		{ID: "u-004", Username: "inactive-user", Roles: []string{"analyst"}, HasMFA: false, IsInactive: true, LastLogin: time.Now().AddDate(0, 0, -91)},
	},
	NetworkConfig: compliance.NetworkConfiguration{
		AllowedIPs:  []string{"10.0.0.0/8", "192.168.0.0/16"},
		BlockedIPs:  []string{"203.0.113.0/24"},
		RequiresVPN: true,
		SSLRequired: true,
	},
}

// EvalControls is the FIXED control set evaluated by BOTH engines (same work unit).
var EvalControls = []string{
	"SOC2-CC6.1",    // Logical Access Controls
	"SOC2-CC6.2",    // Authentication Mechanisms
	"SOC2-CC6.6",    // Encryption in Transit
	"ISO27001-A5.7", // Identity Management
	"GDPR-Art32",    // Security of Processing
}

// ===========================================================================
// CLOUDAI FUSION NATIVE ENGINE BENCHMARKS
// Same work unit: evaluate the 5 EvalControls against TestResourceState.
// ===========================================================================

func BenchmarkCloudAIFusion_PerControl_Latency(b *testing.B) {
	ctx := context.Background()
	engine := compliance.NewPolicyEngine()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, ctrlID := range EvalControls {
			rule, exists := engine.GetPolicyRule(ctrlID)
			if !exists {
				b.Fatalf("Control not found: %s", ctrlID)
			}
			result, err := rule.Evaluate(ctx, TestResourceState)
			if err != nil {
				b.Fatal(err)
			}
			_ = result.Pass
		}
	}
	// ns/op reported by the harness == time to evaluate all 5 controls once.
}

func BenchmarkCloudAIFusion_Throughput_CtrlPerSec(b *testing.B) {
	ctx := context.Background()
	engine := compliance.NewPolicyEngine()

	var totalControls int
	start := time.Now()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, ctrlID := range EvalControls {
			rule, _ := engine.GetPolicyRule(ctrlID)
			if _, err := rule.Evaluate(ctx, TestResourceState); err != nil {
				b.Fatal(err)
			}
			totalControls++
		}
	}
	b.StopTimer()

	elapsed := time.Since(start).Seconds()
	if elapsed > 0 {
		b.ReportMetric(float64(totalControls)/elapsed, "controls-per-sec")
	}
}

// ===========================================================================
// OPA REGO ENGINE
// Real competitor: github.com/open-policy-agent/opa/rego
// Comprehensive SOC2/ISO27001/GDPR control mappings in Rego v1 syntax.
// ===========================================================================

const opaCompliancePolicy = `# ===========================================================================
# M36 Compliance Reporter - OPA Rego Policies for SOC2/ISO27001/GDPR
#
# Control-to-policy mapping:
#   SOC2-CC6.1     -> soc2_cc6_1_pass  (no public+unencrypted resource)
#   SOC2-CC6.2     -> soc2_cc6_2_pass  (every role-holder has MFA)
#   SOC2-CC6.6     -> soc2_cc6_6_pass  (no api_endpoint with https disabled)
#   ISO27001-A5.7  -> iso27001_a5_7_pass (>=1 active user)
#   GDPR-Art32     -> gdpr_art32_pass  (encryption + SSL + VPN present)
# ===========================================================================

package compliance.rego

import rego.v1

# --- SOC2 Trust Services Criteria ------------------------------------------

default soc2_cc6_1_pass := false
soc2_cc6_1_pass if {
	count([r |
		some r in input.resources
		r.publicly_accessible == true
		r.encrypted != true
	]) == 0
}

default soc2_cc6_2_pass := false
soc2_cc6_2_pass if {
	count([u |
		some u in input.users
		count(u.roles) > 0
		u.has_mfa != true
	]) == 0
}

default soc2_cc6_6_pass := false
soc2_cc6_6_pass if {
	count([e |
		some e in input.resources
		e.kind == "api_endpoint"
		e.config.https_enabled != true
	]) == 0
}

# --- ISO 27001:2022 Annex A ------------------------------------------------

default iso27001_a5_7_pass := false
iso27001_a5_7_pass if {
	count([u | some u in input.users; u.is_inactive != true]) > 0
}

# --- GDPR Articles ---------------------------------------------------------

default gdpr_art32_pass := false
gdpr_art32_pass if {
	count([r | some r in input.resources; r.encrypted == true]) > 0
	input.network_config.ssl_required == true
	input.network_config.requires_vpn == true
}

# --- Aggregated report: control_id -> pass/fail ----------------------------

report := {
	"SOC2-CC6.1":    soc2_cc6_1_pass,
	"SOC2-CC6.2":    soc2_cc6_2_pass,
	"SOC2-CC6.6":    soc2_cc6_6_pass,
	"ISO27001-A5.7": iso27001_a5_7_pass,
	"GDPR-Art32":    gdpr_art32_pass,
}
`

// controlMeta carries the framework mapping that our audit report layer adds
// on top of the raw pass/fail decision (this is our differentiating edge).
var controlMeta = map[string][2]string{
	"SOC2-CC6.1":    {"SOC2", "Access Control"},
	"SOC2-CC6.2":    {"SOC2", "Authentication"},
	"SOC2-CC6.6":    {"SOC2", "Encryption"},
	"ISO27001-A5.7": {"ISO27001", "Identity"},
	"GDPR-Art32":    {"GDPR", "Security"},
}

var controlFailure = map[string]string{
	"SOC2-CC6.1":    "Unencrypted public resource",
	"SOC2-CC6.2":    "User without MFA has roles",
	"SOC2-CC6.6":    "API endpoint without HTTPS",
	"ISO27001-A5.7": "No active users configured",
	"GDPR-Art32":    "Missing encryption or SSL/VPN",
}

// OPAComplianceEngine wraps a prepared OPA Rego query for control evaluation.
type OPAComplianceEngine struct {
	ctx           context.Context
	preparedQuery regolib.PreparedEvalQuery
	mu            sync.RWMutex
}

func NewOPAComplianceEngine(ctx context.Context) (*OPAComplianceEngine, error) {
	tr := regolib.New(
		// Query a single control decision via input.control_id — one control per Eval,
		// matching the native per-control work unit exactly.
		regolib.Query("data.compliance.rego.report[input.control_id]"),
		regolib.Module("compliance.rego", opaCompliancePolicy),
	)
	pq, err := tr.PrepareForEval(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare OPA policy: %w", err)
	}
	return &OPAComplianceEngine{ctx: ctx, preparedQuery: pq}, nil
}

func (e *OPAComplianceEngine) EvaluateControl(controlID string, state compliance.ResourceState) (*compliance.ControlEvaluationResult, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	inputData := convertToJSON(state)
	inputData["control_id"] = controlID

	results, err := e.preparedQuery.Eval(e.ctx, regolib.EvalInput(inputData))
	if err != nil {
		return nil, fmt.Errorf("opa eval failed for %s: %w", controlID, err)
	}
	if len(results) == 0 || len(results[0].Expressions) == 0 {
		return nil, fmt.Errorf("opa produced no decision for %s", controlID)
	}

	pass, _ := results[0].Expressions[0].Value.(bool)
	return e.buildResult(controlID, pass), nil
}

// buildResult applies the framework mapping + evidence layer (our audit edge)
// onto the raw pass/fail OPA returns.
func (e *OPAComplianceEngine) buildResult(controlID string, pass bool) *compliance.ControlEvaluationResult {
	meta := controlMeta[controlID]
	result := &compliance.ControlEvaluationResult{
		ControlID: controlID,
		Pass:      pass,
		Evidence:  make([]string, 0, 1),
		Metadata: map[string]interface{}{
			"standard": meta[0],
			"category": meta[1],
		},
	}
	if pass {
		result.Evidence = append(result.Evidence, fmt.Sprintf("Control %s passed", controlID))
	} else {
		result.FailureReason = controlFailure[controlID]
		result.Metadata["risk_level"] = "HIGH"
		result.Evidence = append(result.Evidence, result.FailureReason)
	}
	return result
}

func (e *OPAComplianceEngine) EvaluateAll(controls []string, state compliance.ResourceState) ([]*compliance.ControlEvaluationResult, error) {
	results := make([]*compliance.ControlEvaluationResult, 0, len(controls))
	for _, ctrlID := range controls {
		result, err := e.EvaluateControl(ctrlID, state)
		if err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, nil
}

// convertToJSON marshals the shared ResourceState into the JSON-like map OPA expects.
func convertToJSON(state compliance.ResourceState) map[string]interface{} {
	resources := make([]map[string]interface{}, 0, len(state.Resources))
	for _, r := range state.Resources {
		rm := map[string]interface{}{
			"kind":                r.Type,
			"id":                  r.ID,
			"name":                r.Name,
			"encrypted":           r.Encrypted,
			"publicly_accessible": r.PubliclyAccessible,
		}
		if r.Config != nil {
			rm["config"] = r.Config
		}
		resources = append(resources, rm)
	}

	users := make([]map[string]interface{}, 0, len(state.Users))
	for _, u := range state.Users {
		users = append(users, map[string]interface{}{
			"id":          u.ID,
			"username":    u.Username,
			"roles":       u.Roles,
			"has_mfa":     u.HasMFA,
			"is_inactive": u.IsInactive,
			"last_login":  u.LastLogin.Format(time.RFC3339),
		})
	}

	logs := make([]map[string]interface{}, 0, len(state.Logs))
	for _, l := range state.Logs {
		logs = append(logs, map[string]interface{}{
			"timestamp":  l.Timestamp.Format(time.RFC3339),
			"source":     l.Source,
			"message":    l.Message,
			"event_type": l.EventType,
			"severity":   l.Severity,
		})
	}

	return map[string]interface{}{
		"resources": resources,
		"users":     users,
		"logs":      logs,
		"network_config": map[string]interface{}{
			"allowed_ips":  state.NetworkConfig.AllowedIPs,
			"blocked_ips":  state.NetworkConfig.BlockedIPs,
			"requires_vpn": state.NetworkConfig.RequiresVPN,
			"ssl_required": state.NetworkConfig.SSLRequired,
		},
	}
}

// ===========================================================================
// OPA REGO ENGINE BENCHMARKS
// ===========================================================================

func BenchmarkOPARego_PerControl_Latency(b *testing.B) {
	ctx := context.Background()
	engine, err := NewOPAComplianceEngine(ctx)
	if err != nil {
		b.Fatalf("Failed to init OPA engine: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, ctrlID := range EvalControls {
			result, err := engine.EvaluateControl(ctrlID, TestResourceState)
			if err != nil {
				b.Fatal(err)
			}
			_ = result.Pass
		}
	}
	// ns/op reported by the harness == time to evaluate all 5 controls once.
}

func BenchmarkOPARego_Throughput_CtrlPerSec(b *testing.B) {
	ctx := context.Background()
	engine, err := NewOPAComplianceEngine(ctx)
	if err != nil {
		b.Fatalf("Failed to init OPA engine: %v", err)
	}

	var totalControls int
	start := time.Now()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, ctrlID := range EvalControls {
			if _, err := engine.EvaluateControl(ctrlID, TestResourceState); err != nil {
				b.Fatal(err)
			}
			totalControls++
		}
	}
	b.StopTimer()

	elapsed := time.Since(start).Seconds()
	if elapsed > 0 {
		b.ReportMetric(float64(totalControls)/elapsed, "controls-per-sec")
	}
}

// ===========================================================================
// CORRECTNESS VERIFICATION
// Both engines MUST produce identical pass/fail for every control.
// ===========================================================================

func TestCorrectness_NativeVsOPA_PassFailMatch(t *testing.T) {
	ctx := context.Background()
	nativeEngine := compliance.NewPolicyEngine()
	opaEngine, err := NewOPAComplianceEngine(ctx)
	if err != nil {
		t.Fatalf("Failed to init OPA engine: %v", err)
	}

	for _, ctrlID := range EvalControls {
		rule, ok := nativeEngine.GetPolicyRule(ctrlID)
		if !ok {
			t.Fatalf("native missing control %s", ctrlID)
		}
		nativeRes, err := rule.Evaluate(ctx, TestResourceState)
		if err != nil {
			t.Fatalf("native eval %s: %v", ctrlID, err)
		}

		opaRes, err := opaEngine.EvaluateControl(ctrlID, TestResourceState)
		if err != nil {
			t.Fatalf("opa eval %s: %v", ctrlID, err)
		}

		if nativeRes.Pass != opaRes.Pass {
			t.Errorf("CORRECTNESS MISMATCH for %s: native=%v opa=%v", ctrlID, nativeRes.Pass, opaRes.Pass)
		} else {
			t.Logf("%-14s native=%v opa=%v [MATCH]", ctrlID, nativeRes.Pass, opaRes.Pass)
		}
	}
}

// BenchmarkCorrectness_NativeVsOPA keeps the pass/fail parity check inside the
// benchmark harness so it is exercised under -count sampling too.
func BenchmarkCorrectness_NativeVsOPA(b *testing.B) {
	ctx := context.Background()
	nativeEngine := compliance.NewPolicyEngine()
	opaEngine, err := NewOPAComplianceEngine(ctx)
	if err != nil {
		b.Fatalf("Failed to init OPA engine: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, ctrlID := range EvalControls {
			rule, _ := nativeEngine.GetPolicyRule(ctrlID)
			nativeRes, _ := rule.Evaluate(ctx, TestResourceState)
			opaRes, err := opaEngine.EvaluateControl(ctrlID, TestResourceState)
			if err != nil {
				b.Fatal(err)
			}
			if nativeRes.Pass != opaRes.Pass {
				b.Fatalf("pass/fail mismatch for %s: native=%v opa=%v", ctrlID, nativeRes.Pass, opaRes.Pass)
			}
		}
	}
}

// ===========================================================================
// REPORT GENERATION BENCHMARKS
// Measures time to generate full audit report with framework metadata layer.
// This is CloudAI Fusion's differentiating edge (instant attestation).
// ===========================================================================

func BenchmarkCloudAIFusion_ReportGeneration(b *testing.B) {
	ctx := context.Background()
	engine := compliance.NewPolicyEngine()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results, _ := engine.EvaluateAll(ctx, TestResourceState)
		_ = results // Report generation: collect framework metadata + evidence
		// Simulate audit report serialization overhead
		if len(results) == 0 {
			b.Fatal("no results")
		}
	}
}

func BenchmarkOPARego_ReportGeneration(b *testing.B) {
	ctx := context.Background()
	opaEngine, err := NewOPAComplianceEngine(ctx)
	if err != nil {
		b.Fatalf("Failed to init OPA engine: %v", err)
	}

	var totalControls int
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := opaEngine.EvaluateAll(EvalControls, TestResourceState)
		if err != nil {
			b.Fatal(err)
		}
		totalControls += len(EvalControls)
	}
	_ = totalControls
}
