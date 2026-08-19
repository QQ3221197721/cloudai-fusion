// Package wasm — Tests for Module 51: Capability-based security model.
package wasm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPathRule_IsAllowed(t *testing.T) {
	rule := &PathRule{
		AllowedRoots: []string{"/safe/dir", "/tmp"},
		DeniedPaths:  []string{"/safe/dir/tmp-secrets"},
	}

	tests := []struct {
		name   string
		path   string
		allowed bool
	}{
		{"exact allowed root", "/safe/dir", true},
		{"child path under root", "/safe/dir/file.txt", true},
		{"under tmp", "/tmp/cache.dat", true},
		{"denied path", "/safe/dir/tmp-secrets/data", false},
		{"traversal attack 1", "/safe/../etc/passwd", false},
		{"traversal attack 2", "safe/../../etc/shadow", false},
		{"absolute traversal", "/../../../root/.ssh/id_rsa", false},
		{"outside any root", "/home/user/doc.pdf", false}, // not in AllowedRoots
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rule.IsPathAllowed(tt.path)
			if result != tt.allowed {
				t.Errorf("IsPathAllowed(%q) = %v, want %v", tt.path, result, tt.allowed)
			}
		})
	}
}

// TestPathRule_TraversalVariantsBlocked is a comprehensive test for URL-encoded, NUL,
// and raw-path traversal attack vectors — every variant must be denied.
func TestPathRule_TraversalVariantsBlocked(t *testing.T) {
	rule := &PathRule{AllowedRoots: []string{"/safe"}}

	attacks := []string{
		"/safe/../outside",           // raw ..
		"/safe/%2e%2e/outside",       // single URL encoding
		"/safe/%252e%252e/outside",   // double URL encoding
		"/safe/a/..\\b\\out",          // mixed backslash
		"/safe/a\x00/b",              // NUL byte injection
		"/safe/ok\u0000",                // NUL as suffix
	}

	for _, a := range attacks {
		t.Run(a, func(t *testing.T) {
			if rule.IsPathAllowed(a) {
				t.Errorf("Traversal attack %q should be blocked", a)
			}
		})
	}
}

func TestNetRule_CanAccessTarget(t *testing.T) {
	rule := &NetRule{
		AllowedHosts:    []string{"example.com", "*.cloudai-fusion.io"},
		AllowedPorts:    []int{443, 8080},
		BlockedPorts:    []int{443},
		AllowLoopback:   true,
		AllowPrivateIPv4: true,
	}

	tests := []struct {
		name    string
		host    string
		port    int
		allowed bool
	}{
		{"loopback allowed", "localhost", 8080, true},
		{"private IP allowed", "192.168.1.1", 8080, true},
		{"whitelisted host + port", "example.com", 443, false}, // 443 blocked
		{"whitelisted host + good port", "example.com", 8080, true},
		{"subdomain allowed", "api.cloudai-fusion.io", 443, false}, // 443 blocked
		{"unmatched domain", "evil.com", 8080, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rule.CanAccessTarget(tt.host, tt.port)
			if result != tt.allowed {
				t.Errorf("CanAccessTarget(%q, %d) = %v, want %v", tt.host, tt.port, result, tt.allowed)
			}
		})
	}
}

func TestGPURule_IsDeviceAllowed(t *testing.T) {
	rule := &GPURule{
		AllowedDevices:  []int{0, 2, 5},
		AllowedNodeNames: []string{"node-a", "node-b"},
		Topology:        "nvlink",
		MaxMemoryGB:     80,
	}

	tests := []struct {
		idx       int
		allowed   bool
	}{
		{0, true},
		{1, false}, // not in whitelist
		{2, true},
		{5, true},
		{10, false},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := rule.IsDeviceAllowed(tt.idx)
			if result != tt.allowed {
				t.Errorf("IsDeviceAllowed(%d) = %v, want %v", tt.idx, result, tt.allowed)
			}
		})
	}
}

func TestGrant_DefaultDeny(t *testing.T) {
	grant := NewDefaultGrant()

	if grant.HasFilesystemAccess() {
		t.Error("Default grant should deny filesystem access")
	}
	if grant.HasNetworkAccess() {
		t.Error("Default grant should deny network access")
	}
	if grant.HasGPUAccess() {
		t.Error("Default grant should deny GPU access")
	}

	// Grant one capability and verify others still denied
	grant.Filesystem = &PathRule{AllowedRoots: []string{"/data"}}
	if !grant.HasFilesystemAccess() {
		t.Error("Granting filesystem should enable it")
	}
	if grant.HasNetworkAccess() {
		t.Error("Network should remain disabled after granting filesystem")
	}
}

// TestPathRule_EmptyRootDoesNotGrantFilesystem — a blank AllowedRoots entry must
// never be interpreted as "/" (regression: blank root previously reached the
// prefix test with rootWithSep == "/").
func TestPathRule_EmptyRootDoesNotGrantFilesystem(t *testing.T) {
	blank := &PathRule{AllowedRoots: []string{"", "   "}}
	for _, p := range []string{"/etc/passwd", "/", "/anything"} {
		if blank.IsPathAllowed(p) {
			t.Errorf("blank root must deny %q", p)
		}
	}

	mixed := &PathRule{AllowedRoots: []string{"", "/data"}}
	if !mixed.IsPathAllowed("/data/file.txt") {
		t.Error("a real root must still grant access when listed next to a blank one")
	}
	if mixed.IsPathAllowed("/etc/passwd") {
		t.Error("blank root must not widen /data grant to the whole filesystem")
	}
}

// TestPathRule_DenyListBoundaryAndCase pins deny-list semantics: component
// boundaries (not substrings) and case-insensitive comparison, because Windows and
// macOS filesystems are case-insensitive.
func TestPathRule_DenyListBoundaryAndCase(t *testing.T) {
	rule := &PathRule{
		AllowedRoots: []string{"/safe"},
		DeniedPaths:  []string{"/safe/secrets", "/safe/keys/"},
	}

	tests := []struct {
		name    string
		path    string
		allowed bool
	}{
		{"denied dir itself", "/safe/secrets", false},
		{"denied child", "/safe/secrets/db.pem", false},
		{"denied upper case", "/safe/SECRETS/db.pem", false},
		{"denied mixed case", "/safe/Secrets/db.pem", false},
		{"denied trailing slash rule", "/safe/keys/id_rsa", false},
		{"sibling sharing a prefix is allowed", "/safe/secrets-public/readme.md", true},
		{"unrelated sibling is allowed", "/safe/secondary.txt", true},
		{"plain file under root", "/safe/app.log", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rule.IsPathAllowed(tt.path); got != tt.allowed {
				t.Errorf("IsPathAllowed(%q) = %v, want %v", tt.path, got, tt.allowed)
			}
		})
	}
}

// TestPathRule_EmptyDeniedPathsStillGrants is a regression test for the
// strings.Contains(x, "") bug that made every path deny when DeniedPaths was empty.
func TestPathRule_EmptyDeniedPathsStillGrants(t *testing.T) {
	rule := &PathRule{AllowedRoots: []string{"/data"}}
	for _, p := range []string{"/data", "/data/file.txt", "/data/nested/deep/file.bin"} {
		if !rule.IsPathAllowed(p) {
			t.Errorf("IsPathAllowed(%q) = false, want true (empty DeniedPaths must not deny everything)", p)
		}
	}
}

// TestPathRule_UnicodeConfusablesDocumentedGap documents an honestly-declared gap:
// fullwidth U+FF0E dots are NOT treated as traversal. No OS path resolver follows
// them, so this is not an escape today, but a downstream NFKC normalization would
// turn them into ".." — hence "not_covered" rather than "blocked".
func TestPathRule_UnicodeConfusablesDocumentedGap(t *testing.T) {
	rule := &PathRule{AllowedRoots: []string{"/safe"}}
	const confusable = "/safe/\uFF0E\uFF0E/etc/passwd"
	if !rule.IsPathAllowed(confusable) {
		t.Skip("implementation now rejects fullwidth confusables; update the escape-vector table to blocked")
	}
	t.Logf("documented gap: %q is treated as a normal component name, not traversal", confusable)
}

// TestNetRule_BlockedHostWinsOverAllowRules is a regression test for the deny-list
// priority inversion: BlockedHosts used to be evaluated last, so an allow-all or
// loopback/private rule silently overrode an explicit block.
func TestNetRule_BlockedHostWinsOverAllowRules(t *testing.T) {
	cases := []struct {
		name string
		rule *NetRule
		host string
	}{
		{
			name: "wildcard allow-all must not override block",
			rule: &NetRule{AllowedHosts: []string{"*"}, BlockedHosts: []string{"metadata.internal"}, AllowedPorts: []int{80}},
			host: "metadata.internal",
		},
		{
			name: "exact allow must not override block",
			rule: &NetRule{AllowedHosts: []string{"example.com"}, BlockedHosts: []string{"example.com"}, AllowedPorts: []int{80}},
			host: "example.com",
		},
		{
			name: "block list is case-insensitive",
			rule: &NetRule{AllowedHosts: []string{"example.com"}, BlockedHosts: []string{"EXAMPLE.CoM"}, AllowedPorts: []int{80}},
			host: "example.com",
		},
		{
			name: "loopback allowance must not override block",
			rule: &NetRule{AllowLoopback: true, BlockedHosts: []string{"127.0.0.1"}, AllowedPorts: []int{80}},
			host: "127.0.0.1",
		},
		{
			name: "private range allowance must not override block",
			rule: &NetRule{AllowPrivateIPv4: true, BlockedHosts: []string{"10.0.0.5"}, AllowedPorts: []int{80}},
			host: "10.0.0.5",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.rule.IsHostAllowed(tc.host) {
				t.Errorf("IsHostAllowed(%q) = true, want false (explicit block must win)", tc.host)
			}
			if tc.rule.CanAccessTarget(tc.host, 80) {
				t.Errorf("CanAccessTarget(%q, 80) = true, want false", tc.host)
			}
		})
	}
}

// TestNetRule_LoopbackSpoofingBlocked is a regression test for the "127." string
// prefix check that let an attacker-controlled hostname pose as localhost.
func TestNetRule_LoopbackSpoofingBlocked(t *testing.T) {
	rule := &NetRule{AllowLoopback: true, AllowedPorts: []int{80}}

	denied := []string{"127.evil.com", "127.0.0.1.evil.com", "localhost.evil.com", "0.0.0.0", ""}
	for _, h := range denied {
		if rule.IsHostAllowed(h) {
			t.Errorf("IsHostAllowed(%q) = true, want false (not a loopback address)", h)
		}
	}

	allowed := []string{"127.0.0.1", "127.0.0.53", "::1", "localhost", "::ffff:127.0.0.1"}
	for _, h := range allowed {
		if !rule.IsHostAllowed(h) {
			t.Errorf("IsHostAllowed(%q) = false, want true (real loopback with AllowLoopback)", h)
		}
	}
}

// TestNetRule_WildcardLabelMatching pins wildcard semantics: one or more non-empty
// labels before the suffix match; a sibling domain sharing the suffix does not.
func TestNetRule_WildcardLabelMatching(t *testing.T) {
	rule := &NetRule{AllowedHosts: []string{"*.example.com"}, AllowedPorts: []int{443}}

	cases := []struct {
		host    string
		allowed bool
	}{
		{"api.example.com", true},
		{"a.b.example.com", true},
		{"API.Example.com", true},   // DNS is case-insensitive
		{"evilexample.com", false},  // sibling suffix, no dot boundary
		{"example.com", false},      // apex not covered by *.
		{".example.com", false},     // empty label
		{"example.com.evil.io", false},
	}

	for _, c := range cases {
		t.Run(c.host, func(t *testing.T) {
			if got := rule.IsHostAllowed(c.host); got != c.allowed {
				t.Errorf("IsHostAllowed(%q) = %v, want %v", c.host, got, c.allowed)
			}
		})
	}
}

// TestNetRule_MetadataAndLinkLocalBlocked verifies that AllowPrivateIPv4 does not
// hand out the cloud metadata endpoint or any link-local/multicast address.
func TestNetRule_MetadataAndLinkLocalBlocked(t *testing.T) {
	rule := &NetRule{AllowPrivateIPv4: true, AllowedPorts: []int{80}}

	denied := []string{
		"169.254.169.254", // AWS/Azure/GCP IMDS
		"169.254.170.2",   // ECS task metadata
		"224.0.0.1",       // multicast
		"100.64.0.1",      // CGNAT, not an RFC1918 range
		"8.8.8.8",         // public
	}
	for _, h := range denied {
		if rule.IsHostAllowed(h) {
			t.Errorf("IsHostAllowed(%q) = true, want false", h)
		}
	}

	allowed := []string{"10.1.2.3", "172.16.0.1", "192.168.1.1"}
	for _, h := range allowed {
		if !rule.IsHostAllowed(h) {
			t.Errorf("IsHostAllowed(%q) = false, want true (RFC1918 with AllowPrivateIPv4)", h)
		}
	}
}

// TestNetRule_PortDefaultIsAllowAllUnlessStrict documents the port-dimension gap
// honestly: with no AllowedPorts the legacy default reaches every port on an
// allowed host, and RequireExplicitPorts is the opt-in that closes it.
func TestNetRule_PortDefaultIsAllowAllUnlessStrict(t *testing.T) {
	legacy := &NetRule{AllowedHosts: []string{"example.com"}}
	if !legacy.CanAccessTarget("example.com", 22) {
		t.Skip("default port semantics have been tightened; update the escape-vector table")
	}
	t.Log("documented gap: empty AllowedPorts reaches every port on an allowed host")

	strict := &NetRule{AllowedHosts: []string{"example.com"}, RequireExplicitPorts: true}
	if strict.CanAccessTarget("example.com", 22) {
		t.Error("RequireExplicitPorts=true must deny ports that are not explicitly listed")
	}
	if strict.CanAccessTarget("example.com", 443) {
		t.Error("RequireExplicitPorts=true with no AllowedPorts must deny every port")
	}

	scoped := &NetRule{AllowedHosts: []string{"example.com"}, AllowedPorts: []int{443}, RequireExplicitPorts: true}
	if !scoped.CanAccessTarget("example.com", 443) {
		t.Error("explicitly listed port must be allowed")
	}
	if scoped.CanAccessTarget("example.com", 22) {
		t.Error("unlisted port must be denied")
	}

	for _, p := range []int{0, -1, 65536, 99999} {
		if scoped.IsPortAllowed(p) {
			t.Errorf("IsPortAllowed(%d) = true, want false (out of range)", p)
		}
	}
}

// TestNetRule_ValidateURL covers the URL entry point, including schemes that carry
// no usable port and userinfo tricks that try to disguise the real host.
func TestNetRule_ValidateURL(t *testing.T) {
	rule := &NetRule{AllowedHosts: []string{"example.com"}, AllowedPorts: []int{443}}

	cases := []struct {
		url     string
		allowed bool
	}{
		{"https://example.com/x", true},
		{"https://example.com:443/x", true},
		{"http://example.com/x", false},              // port 80 not granted
		{"https://evil.com/x", false},                // host not granted
		{"https://example.com@evil.com/x", false},    // real host is evil.com
		{"file:///etc/passwd", false},                // no host, no port
		{"ftp://example.com/x", false},               // unsupported scheme, no port
		{"https://example.com:8443/x", false},        // port not granted
	}

	for _, c := range cases {
		t.Run(c.url, func(t *testing.T) {
			if got := rule.ValidateURL(c.url); got != c.allowed {
				t.Errorf("ValidateURL(%q) = %v, want %v", c.url, got, c.allowed)
			}
		})
	}
}

// TestGPURule_CanUseGPUAndMemoryBudget covers the combined GPU gate and the VRAM
// budget that MaxMemoryGB previously declared but never enforced.
func TestGPURule_CanUseGPUAndMemoryBudget(t *testing.T) {
	rule := &GPURule{
		AllowedDevices:   []int{0, 2},
		AllowedNodeNames: []string{"node-a"},
		Topology:         "nvlink",
		MaxMemoryGB:      80,
	}

	if !rule.CanUseGPU(0, "node-a", "nvlink") {
		t.Error("fully matching request must be allowed")
	}
	if rule.CanUseGPU(1, "node-a", "nvlink") {
		t.Error("device outside AllowedDevices must be denied")
	}
	if rule.CanUseGPU(0, "node-z", "nvlink") {
		t.Error("node outside AllowedNodeNames must be denied")
	}
	if rule.CanUseGPU(0, "node-a", "pcie") {
		t.Error("topology mismatch must be denied")
	}
	if rule.CanUseGPU(-1, "node-a", "nvlink") {
		t.Error("negative device index must be denied")
	}

	// Empty rule denies on every dimension.
	empty := &GPURule{}
	if empty.IsDeviceAllowed(0) || empty.IsNodeAllowed("node-a") || empty.IsMemoryAllowed(1) {
		t.Error("empty GPURule must deny device, node and memory")
	}

	// VRAM budget.
	if !rule.IsMemoryAllowed(80) {
		t.Error("request equal to MaxMemoryGB must be allowed")
	}
	if rule.IsMemoryAllowed(81) {
		t.Error("request above MaxMemoryGB must be denied")
	}
	if rule.IsMemoryAllowed(0) || rule.IsMemoryAllowed(-8) {
		t.Error("non-positive VRAM request must be denied")
	}
	if (&GPURule{AllowedDevices: []int{0}}).IsMemoryAllowed(1) {
		t.Error("a rule without MaxMemoryGB must grant no VRAM")
	}
}

// TestGrant_EnvDenyByDefault verifies host env vars are only visible when the grant
// lists them explicitly.
func TestGrant_EnvDenyByDefault(t *testing.T) {
	grant := NewDefaultGrant()
	if _, ok := grant.EnvValue("PATH"); ok {
		t.Error("default grant must expose no environment variables")
	}

	grant.Environment = map[string]string{"PLUGIN_MODE": "strict"}
	if v, ok := grant.EnvValue("PLUGIN_MODE"); !ok || v != "strict" {
		t.Errorf("EnvValue(PLUGIN_MODE) = (%q,%v), want (strict,true)", v, ok)
	}
	for _, k := range []string{"PATH", "AWS_SECRET_ACCESS_KEY", "plugin_mode"} {
		if _, ok := grant.EnvValue(k); ok {
			t.Errorf("EnvValue(%q) leaked a value that was never granted", k)
		}
	}

	var nilGrant *Grant
	if _, ok := nilGrant.EnvValue("PATH"); ok {
		t.Error("nil grant must expose nothing")
	}
}

// TestGrant_NilGrantDeniesEverything covers the nil-receiver path of the grant gates.
func TestGrant_NilGrantDeniesEverything(t *testing.T) {
	var g *Grant
	if g.HasFilesystemAccess() || g.HasNetworkAccess() || g.HasGPUAccess() {
		t.Error("nil grant must deny every capability")
	}
}

func TestEscapeVectorsCount(t *testing.T) {
	total, blocked, mitigated, notCovered := TotalEscapeVectors()
	t.Logf("Escape vectors: total=%d, blocked=%d, mitigated=%d, not_covered=%d",
		total, blocked, mitigated, notCovered)
	if total == 0 {
		t.Fatal("TotalEscapeVectors returned zero")
	}
	if blocked+mitigated+notCovered != total {
		t.Errorf("status counts %d+%d+%d do not sum to total %d (unknown status present)",
			blocked, mitigated, notCovered, total)
	}
	if notCovered == 0 {
		t.Error("the table must keep listing the vectors we do NOT cover")
	}

	for _, v := range KnownEscapeVectors() {
		switch v.Status {
		case "blocked", "mitigated":
			if v.TestRef == "" {
				t.Errorf("vector %q claims %q without a TestRef", v.Name, v.Status)
			}
			if strings.Contains(v.TestRef, "TBD") {
				t.Errorf("vector %q has placeholder TestRef %q", v.Name, v.TestRef)
			}
		case "not_covered":
			if v.BlockedBy == "" {
				t.Errorf("vector %q must explain why it is not covered", v.Name)
			}
		default:
			t.Errorf("vector %q has unknown status %q", v.Name, v.Status)
		}
	}
}

// ============================================================================
// Benchmarks — capability check latency, grant parsing, path normalization
// ============================================================================

var benchPathRule = &PathRule{
	AllowedRoots: []string{"/plugins/data", "/tmp/plugin-cache"},
	DeniedPaths:  []string{"/plugins/data/secrets", "/plugins/data/keys"},
}

func BenchmarkCapabilityFSCheck(b *testing.B) {
	cases := []struct {
		name string
		path string
	}{
		{"AllowShallow", "/plugins/data/input.bin"},
		{"AllowDeep", "/plugins/data/a/b/c/d/e/f/g/input.bin"},
		{"DenyOutsideRoot", "/etc/passwd"},
		{"DenyByDenyList", "/plugins/data/secrets/db.pem"},
		{"DenyTraversal", "/plugins/data/../../etc/passwd"},
		{"DenyEncodedTraversal", "/plugins/data/%2e%2e/%2e%2e/etc/passwd"},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				sinkBool = benchPathRule.IsPathAllowed(c.path)
			}
		})
	}
}

var benchNetRule = &NetRule{
	AllowedHosts:     []string{"api.internal", "*.cloudai-fusion.io", "telemetry.example.com"},
	AllowedPorts:     []int{443, 8443},
	BlockedHosts:     []string{"metadata.internal"},
	BlockedPorts:     []int{22},
	AllowLoopback:    true,
	AllowPrivateIPv4: true,
}

func BenchmarkCapabilityNetCheck(b *testing.B) {
	cases := []struct {
		name string
		host string
		port int
	}{
		{"AllowExactHost", "api.internal", 443},
		{"AllowWildcardHost", "gpu.cloudai-fusion.io", 443},
		{"AllowLoopbackIP", "127.0.0.1", 443},
		{"AllowPrivateIP", "10.4.5.6", 443},
		{"DenyUnknownHost", "evil.example.org", 443},
		{"DenyBlockedHost", "metadata.internal", 443},
		{"DenyBlockedPort", "api.internal", 22},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				sinkBool = benchNetRule.CanAccessTarget(c.host, c.port)
			}
		})
	}
}

func BenchmarkCapabilityNetValidateURL(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkBool = benchNetRule.ValidateURL("https://gpu.cloudai-fusion.io/v1/infer")
	}
}

var benchGPURule = &GPURule{
	AllowedDevices:   []int{0, 2, 4, 6},
	AllowedNodeNames: []string{"node-a", "node-b"},
	Topology:         "nvlink",
	MaxMemoryGB:      80,
}

func BenchmarkCapabilityGPUCheck(b *testing.B) {
	b.Run("AllowDevice", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			sinkBool = benchGPURule.IsDeviceAllowed(4)
		}
	})
	b.Run("DenyDevice", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			sinkBool = benchGPURule.IsDeviceAllowed(7)
		}
	})
	b.Run("CombinedAllow", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			sinkBool = benchGPURule.CanUseGPU(2, "node-b", "nvlink")
		}
	})
	b.Run("MemoryBudget", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			sinkBool = benchGPURule.IsMemoryAllowed(40)
		}
	})
}

// grantJSON mirrors how a plugin manifest ships its capability grant.
const grantJSON = `{
  "filesystem": {
    "allowed_roots": ["/plugins/data", "/tmp/plugin-cache"],
    "denied_paths": ["/plugins/data/secrets", "/plugins/data/keys"]
  },
  "network": {
    "allowed_hosts": ["api.internal", "*.cloudai-fusion.io"],
    "allowed_ports": [443, 8443],
    "blocked_hosts": ["metadata.internal"],
    "blocked_ports": [22],
    "allow_loopback": true,
    "allow_private_ipv4": false,
    "require_explicit_ports": true
  },
  "gpu": {
    "allowed_devices": [0, 2],
    "allowed_node_names": ["node-a"],
    "topology": "nvlink",
    "max_memory_gb": 80
  },
  "environment": {"PLUGIN_MODE": "strict"}
}`

func BenchmarkCapabilityGrantParse(b *testing.B) {
	raw := []byte(grantJSON)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var g Grant
		if err := json.Unmarshal(raw, &g); err != nil {
			b.Fatalf("unmarshal grant: %v", err)
		}
		sinkBool = g.HasFilesystemAccess() && g.HasNetworkAccess() && g.HasGPUAccess()
	}
}

// BenchmarkCapabilityGrantParseAndCheck measures the full cold path: parse a
// manifest grant, then run one check per dimension.
func BenchmarkCapabilityGrantParseAndCheck(b *testing.B) {
	raw := []byte(grantJSON)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var g Grant
		if err := json.Unmarshal(raw, &g); err != nil {
			b.Fatalf("unmarshal grant: %v", err)
		}
		sinkBool = g.Filesystem.IsPathAllowed("/plugins/data/input.bin") &&
			g.Network.CanAccessTarget("api.internal", 443) &&
			g.GPU.CanUseGPU(0, "node-a", "nvlink")
	}
}

// BenchmarkCapabilityPathNormalize isolates the normalization work that guards
// against traversal: component scanning plus the percent-decode rounds.
func BenchmarkCapabilityPathNormalize(b *testing.B) {
	cases := []struct {
		name string
		path string
	}{
		{"Shallow", "/plugins/data/input.bin"},
		{"Deep16", "/plugins/data/1/2/3/4/5/6/7/8/9/10/11/12/13/14/15/16/input.bin"},
		{"Encoded", "/plugins/data/%41%42%43/input.bin"},
		{"DoubleEncoded", "/plugins/data/%252e%252e/input.bin"},
		{"Backslashes", `\plugins\data\a\b\c\input.bin`},
	}
	for _, c := range cases {
		b.Run(c.name+"/ScanOnly", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				sinkBool = hasTraversalComponent(c.path)
			}
		})
		b.Run(c.name+"/FullCheck", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				sinkBool = benchPathRule.IsPathAllowed(c.path)
			}
		})
	}
}

// sinkBool keeps benchmark results observable so the compiler cannot elide the calls.
var sinkBool bool

