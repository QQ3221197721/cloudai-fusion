// Package wasm — Module 51: Capability-based security model for WASM plugins.
// This module implements a fine-grained permission system with path sanitization,
// network whitelisting, GPU access control, and escape vector documentation.
package wasm

import (
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// ============================================================================
// Path Rules with Directory Traversal Prevention (Module 51)
// ============================================================================

// PathRule defines allow/deny rules for filesystem access with traversal protection.
type PathRule struct {
	AllowedRoots []string `json:"allowed_roots"` // canonical paths (must end without *)
	DeniedPaths  []string `json:"denied_paths"`  // patterns to block even if under allowed root
}

// IsPathAllowed checks if a path is permitted under this rule set.
// Returns true ONLY if:
//   - Normalized path starts with one of AllowedRoots (with / separator or exact match)
//   - Does not match any DeniedPaths pattern (case-insensitive on Unix)
//   - Does not contain ".." components before normalization attempt
func (r *PathRule) IsPathAllowed(path string) bool {
	if path == "" {
		return false
	}

	// CRITICAL FIX: Reject NUL bytes and control characters (prevents C-string truncation attacks)
	for _, ch := range path {
		if ch == 0 || unicode.IsControl(ch) {
			return false
		}
	}

	// Normalize path separators to forward slashes first (cross-platform)
	path = strings.ReplaceAll(path, "\\", "/")

	// Reject raw ".." sequences BEFORE cleaning (anti-sandbox-escape)
	if hasTraversalComponent(path) {
		return false
	}

	// CRITICAL FIX: percent-encoded traversal (%2e%2e%2f) and double encodings
	// (%252e%252e) are decoded iteratively; any decoding round that reveals a
	// traversal component is rejected. PathUnescape is used instead of
	// QueryUnescape so that '+' in legitimate file names is not mangled.
	decoded := path
	for round := 0; round < 3; round++ {
		next, err := url.PathUnescape(decoded)
		if err != nil {
			// Malformed escape sequence: fail closed rather than guess.
			return false
		}
		if next == decoded {
			break
		}
		next = strings.ReplaceAll(next, "\\", "/")
		if hasTraversalComponent(next) {
			return false
		}
		decoded = next
	}
	path = decoded

	// Clean the path (removes . and redundant slashes)
	normalized := filepath.Clean(path)

	// Convert back to forward slashes for comparison (filepath on Windows may use \)
	normalized = strings.ReplaceAll(normalized, "\\", "/")

	if normalized == "." {
		normalized = "/"
	}

	// Final check: must not start with .. after clean
	if strings.HasPrefix(normalized, "..") {
		return false
	}

	if len(r.AllowedRoots) == 0 {
		// Default deny if no roots granted
		return false
	}

	// Find a matching allowed root. Root comparison stays case-sensitive:
	// that is the fail-closed direction on case-insensitive filesystems.
	matched := false
	for _, root := range r.AllowedRoots {
		rootNorm := strings.ReplaceAll(strings.TrimSpace(root), "\\", "/")
		if rootNorm == "" {
			// An empty root must never be interpreted as "/" (whole filesystem).
			continue
		}
		rootNorm = strings.TrimSuffix(rootNorm, "/")
		if rootNorm == "" {
			// Root was exactly "/": everything under / is in scope.
			matched = true
			break
		}
		if normalized == rootNorm || strings.HasPrefix(normalized, rootNorm+"/") {
			matched = true
			break
		}
	}

	if !matched {
		return false
	}

	// Deny list wins over the allowed root. Matching is done per component
	// boundary (not substring) and case-insensitively, because Windows/macOS
	// filesystems are case-insensitive and a case variant must not bypass a deny.
	normLower := strings.ToLower(normalized)
	for _, denied := range r.DeniedPaths {
		d := strings.ToLower(strings.TrimSuffix(strings.ReplaceAll(strings.TrimSpace(denied), "\\", "/"), "/"))
		if d == "" {
			continue
		}
		if normLower == d || strings.HasPrefix(normLower, d+"/") {
			return false
		}
	}

	return true
}

// hasTraversalComponent reports whether any '/'-separated component of path is
// "." or "..". Callers must pass a path already normalized to '/' separators.
func hasTraversalComponent(path string) bool {
	for _, comp := range strings.Split(path, "/") {
		if comp == ".." || comp == "." {
			return true
		}
	}
	return false
}

// ============================================================================
// Network Rules - Whitelist by Host+Port (Module 51)
// ============================================================================

// NetRule defines outbound/inbound network access rules via host/port whitelist.
type NetRule struct {
	AllowedHosts    []string `json:"allowed_hosts"`   // host names or IP addresses
	AllowedPorts    []int    `json:"allowed_ports"`   // port numbers
	BlockedHosts    []string `json:"blocked_hosts"`   // explicit blocks
	BlockedPorts    []int    `json:"blocked_ports"`   // blocked ports
	AllowLoopback   bool     `json:"allow_loopback"`  // allow 127.0.0.1/localhost
	AllowPrivateIPv4 bool    `json:"allow_private_ipv4"`

	// RequireExplicitPorts closes the port-dimension default-allow gap: when true,
	// an empty AllowedPorts list denies every port instead of allowing all of them.
	// Left false by default to preserve the documented legacy semantics.
	RequireExplicitPorts bool `json:"require_explicit_ports"`
}

// IsHostAllowed checks if target host matches allowed/blocked lists.
// BLOCKED HOSTS CHECKED FIRST (fail-open prevention)
func (r *NetRule) IsHostAllowed(host string) bool {
	hostLower := strings.ToLower(strings.TrimSpace(host))
	if hostLower == "" {
		return false
	}

	// CRITICAL FIX: Check blocked hosts BEFORE allowed (prevents fail-open priority inversion)
	for _, bh := range r.BlockedHosts {
		if strings.ToLower(strings.TrimSpace(bh)) == hostLower {
			return false
		}
	}

	// Loopback check (case-insensitive, uses IP parsing)
	if r.AllowLoopback {
		parsedIP := net.ParseIP(hostLower)
		if parsedIP != nil && parsedIP.IsLoopback() {
			return true
		}
		if hostLower == "localhost" {
			return true
		}
	}

	// Private IPv4 ranges check
	if r.AllowPrivateIPv4 {
		parsedIP := net.ParseIP(hostLower)
		if parsedIP != nil && !parsedIP.IsLoopback() {
			// Never auto-allow link-local or multicast
			if parsedIP.IsLinkLocalUnicast() || parsedIP.IsMulticast() {
				return false
			}
			// Explicitly exclude metadata endpoint (AWS/Azure/GCP)
			if hostLower == "169.254.169.254" || strings.HasPrefix(hostLower, "169.254.169.253.") {
				return false
			}
			prefixes := []string{
				"10.",
				"172.16.", "172.17.", "172.18.", "172.19.", "172.20.",
				"172.21.", "172.22.", "172.23.", "172.24.", "172.25.",
				"172.26.", "172.27.", "172.28.", "172.29.", "172.30.", "172.31.",
				"192.168.",
			}
			for _, prefix := range prefixes {
				if strings.HasPrefix(hostLower, prefix) {
					return true
				}
			}
		}
	}

	// Check allowed hosts (support wildcards like *.cloudai-fusion.io)
	for _, ah := range r.AllowedHosts {
		if ah == "*" {
			return true
		}
		if strings.HasPrefix(ah, "*.") {
			suffix := ah[1:] // include the dot: *.example.com -> .example.com
			if strings.HasSuffix(hostLower, suffix) {
				// Ensure it's not an exact match (e.g., evilexample.com)
				beforeDot := strings.TrimSuffix(hostLower, suffix)
				if len(beforeDot) > 0 { // Single non-empty label before dot OK
					return true
				}
			}
		} else if hostLower == ah {
			return true
		}
	}

	return false
}

// IsPortAllowed checks if a port is in the allowed or blocked lists.
// Blocked ports always win. With an empty AllowedPorts list the result depends on
// RequireExplicitPorts: true => deny everything (fail-closed), false => legacy
// allow-all-except-blocked behaviour. See docs/performance-validation-module-51.md.
func (r *NetRule) IsPortAllowed(port int) bool {
	if port <= 0 || port > 65535 {
		return false
	}

	// Explicit block always takes precedence
	for _, bp := range r.BlockedPorts {
		if port == bp {
			return false
		}
	}

	// If there are allowed ports, port must be in that list
	if len(r.AllowedPorts) > 0 {
		for _, ap := range r.AllowedPorts {
			if ap == port {
				return true
			}
		}
		// Port not explicitly allowed → DENY
		return false
	}

	// No AllowedPorts configured.
	return !r.RequireExplicitPorts
}

// CanAccessTarget returns whether the target host:port pair can be accessed.
func (r *NetRule) CanAccessTarget(host string, port int) bool {
	if !r.IsHostAllowed(host) {
		return false
	}
	if !r.IsPortAllowed(port) {
		return false
	}
	return true
}

// ValidateURL parses and validates a URL against this NetRule.
func (r *NetRule) ValidateURL(urlStr string) bool {
	u, err := url.Parse(urlStr)
	if err != nil {
		return false
	}
	host := u.Hostname()
	port := u.Port()
	
	if port == "" {
		// Default ports
		if u.Scheme == "https" {
			port = "443"
		} else if u.Scheme == "http" {
			port = "80"
		} else {
			return false
		}
	}
	
	parsedPort, err := strconv.Atoi(port)
	if err != nil {
		return false
	}

	return r.CanAccessTarget(host, parsedPort)
}

// ============================================================================
// GPU Rules - Device/Topology Based Access Control (Module 51)
// ============================================================================

// GPURule defines access control for GPU devices based on device index,
// node name, topology requirements, and memory limits.
type GPURule struct {
	AllowedDevices   []int           `json:"allowed_devices"`    // specific device indices (e.g., [0, 2])
	AllowedNodeNames []string        `json:"allowed_node_names"` // compute node identifiers
	Topology         string          `json:"topology"`           // e.g., "nvlink", "pcie", "" = any
	MaxMemoryGB      int             `json:"max_memory_gb"`      // VRAM limit per device
}

// IsDeviceAllowed checks whether a given GPU device index is accessible.
func (r *GPURule) IsDeviceAllowed(deviceIdx int) bool {
	if len(r.AllowedDevices) == 0 {
		return false // default deny if no devices listed
	}
	for _, idx := range r.AllowedDevices {
		if idx == deviceIdx {
			return true
		}
	}
	return false
}

// IsNodeAllowed checks whether the named node is authorized.
func (r *GPURule) IsNodeAllowed(nodeName string) bool {
	if len(r.AllowedNodeNames) == 0 {
		return false
	}
	for _, n := range r.AllowedNodeNames {
		if n == nodeName {
			return true
		}
	}
	return false
}

// MatchesTopology checks if the current topology satisfies the requirement.
// Empty topology means "any". Current implementation does exact string match.
func (r *GPURule) MatchesTopology(currentTopology string) bool {
	if r.Topology == "" || r.Topology == "any" {
		return true
	}
	return currentTopology == r.Topology
}

// CanUseGPU checks combined GPU access: device + topology + node must all pass.
// Provides a single-point check for capability gate integration.
// NOTE: memory budget is checked separately via IsMemoryAllowed.
func (r *GPURule) CanUseGPU(deviceIdx int, nodeName, topology string) bool {
	if !r.IsDeviceAllowed(deviceIdx) {
		return false
	}
	if !r.MatchesTopology(topology) {
		return false
	}
	if !r.IsNodeAllowed(nodeName) {
		return false
	}
	return true
}

// IsMemoryAllowed enforces the previously declared-but-unenforced MaxMemoryGB budget.
// Deny-by-default: a rule without MaxMemoryGB (<=0) grants no VRAM at all.
func (r *GPURule) IsMemoryAllowed(requestGB int) bool {
	if requestGB <= 0 {
		return false
	}
	if r.MaxMemoryGB <= 0 {
		return false
	}
	return requestGB <= r.MaxMemoryGB
}

// ============================================================================
// Grant Aggregation & Default-Deny Semantics (Module 51)
// ============================================================================

// Grant combines multiple capability rules into a single grant object.
// Default is full deny; caller MUST explicitly assign non-nil grants.
type Grant struct {
	Filesystem *PathRule  `json:"filesystem,omitempty"`
	Network    *NetRule   `json:"network,omitempty"`
	GPU        *GPURule   `json:"gpu,omitempty"`
	Environment map[string]string `json:"environment,omitempty"` // env vars to expose
}

// NewDefaultGrant returns a fully denied grant (all fields nil).
func NewDefaultGrant() *Grant {
	return &Grant{}
}

// HasFilesystemAccess returns true iff Filesystem grant is non-nil.
func (g *Grant) HasFilesystemAccess() bool {
	return g != nil && g.Filesystem != nil
}

// HasNetworkAccess returns true iff Network grant is non-nil.
func (g *Grant) HasNetworkAccess() bool {
	return g != nil && g.Network != nil
}

// HasGPUAccess returns true iff GPU grant is non-nil.
func (g *Grant) HasGPUAccess() bool {
	return g != nil && g.GPU != nil
}

// Environment access control

// EnvValue returns the value for key if explicitly granted (case-sensitive).
// Implements deny-by-default for env vars not listed in the grant.
func (g *Grant) EnvValue(key string) (string, bool) {
	if g == nil || g.Environment == nil {
		return "", false
	}
	v, ok := g.Environment[key]
	return v, ok
}

// ============================================================================
// Sandbox Escape Vectors (Module 51)
// ============================================================================

// EscapeVector describes a known sandbox escape attack vector and its status.
type EscapeVector struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	BlockedBy   string `json:"blocked_by"`
	Status      string `json:"status"` // blocked | mitigated | not_covered
	TestRef     string `json:"test_ref"`           // Go test name proving coverage
	Category    string `json:"category,omitempty"` // fs | net | gpu | runtime | other
}

// KnownEscapeVectors lists documented escape vectors and their coverage.
// Honesty principle: admit which ones are NOT covered by tests or design.
func KnownEscapeVectors() []EscapeVector {
	return []EscapeVector{
		{
			Name:        "Unauthorized filesystem access",
			Description: "Reading or writing a path outside the granted roots",
			BlockedBy:   "PathRule.IsPathAllowed + nil Filesystem grant denying everything",
			Status:      "blocked",
			TestRef:     "TestPathRule_IsAllowed",
			Category:    "fs",
		},
		{
			Name:        "Directory traversal via ../",
			Description: "Path canonicalization attacks escaping the granted root",
			BlockedBy:   "hasTraversalComponent() rejects . and .. components before filepath.Clean",
			Status:      "blocked",
			TestRef:     "TestPathRule_TraversalVariantsBlocked",
			Category:    "fs",
		},
		{
			Name:        "URL-encoded traversal bypass",
			Description: "%2e%2e%2f and double-encoded %252e variants circumventing the .. check",
			BlockedBy:   "url.PathUnescape applied iteratively (3 rounds), traversal re-checked each round",
			Status:      "blocked",
			TestRef:     "TestPathRule_TraversalVariantsBlocked",
			Category:    "fs",
		},
		{
			Name:        "NUL byte / control character truncation",
			Description: "Embedded \\x00 exploiting a C-string boundary in a downstream syscall",
			BlockedBy:   "unicode.IsControl rejection over every rune of the input path",
			Status:      "blocked",
			TestRef:     "TestPathRule_TraversalVariantsBlocked",
			Category:    "fs",
		},
		{
			Name:        "Case-variant deny-list bypass",
			Description: "SECRETS vs secrets on case-insensitive Windows/macOS filesystems",
			BlockedBy:   "deny-list compared case-insensitively at path-component boundaries",
			Status:      "blocked",
			TestRef:     "TestPathRule_DenyListBoundaryAndCase",
			Category:    "fs",
		},
		{
			Name:        "Empty AllowedRoots entry granting whole filesystem",
			Description: "A blank root string in a manifest being interpreted as \"/\"",
			BlockedBy:   "blank roots are skipped; no matched root means deny",
			Status:      "blocked",
			TestRef:     "TestPathRule_EmptyRootDoesNotGrantFilesystem",
			Category:    "fs",
		},
		{
			Name:        "Unauthorized network egress",
			Description: "Connecting to a host/port outside the grant",
			BlockedBy:   "NetRule.CanAccessTarget + nil Network grant denying everything",
			Status:      "blocked",
			TestRef:     "TestNetRule_CanAccessTarget",
			Category:    "net",
		},
		{
			Name:        "Cloud metadata SSRF",
			Description: "Reaching 169.254.169.254 (AWS/Azure/GCP IMDS) through a private-range grant",
			BlockedBy:   "link-local and multicast never auto-allowed by AllowPrivateIPv4; IMDS IP excluded explicitly",
			Status:      "blocked",
			TestRef:     "TestNetRule_MetadataAndLinkLocalBlocked",
			Category:    "net",
		},
		{
			Name:        "Loopback prefix spoofing",
			Description: "Attacker-controlled hostname 127.evil.com treated as localhost",
			BlockedBy:   "net.ParseIP().IsLoopback() instead of a \"127.\" string prefix test",
			Status:      "blocked",
			TestRef:     "TestNetRule_LoopbackSpoofingBlocked",
			Category:    "net",
		},
		{
			Name:        "Deny-list priority inversion",
			Description: "A BlockedHosts entry ignored because an allow rule matched first",
			BlockedBy:   "BlockedHosts evaluated before loopback/private/wildcard allow rules",
			Status:      "blocked",
			TestRef:     "TestNetRule_BlockedHostWinsOverAllowRules",
			Category:    "net",
		},
		{
			Name:        "Wildcard host sibling-suffix match",
			Description: "evilexample.com matching a *.example.com grant",
			BlockedBy:   "wildcard requires a dot-delimited non-empty label before the suffix",
			Status:      "blocked",
			TestRef:     "TestNetRule_WildcardLabelMatching",
			Category:    "net",
		},
		{
			Name:        "Port allow-all via empty AllowedPorts",
			Description: "A NetRule that lists hosts but no ports reaches every port on those hosts",
			BlockedBy:   "Only closed when the operator sets RequireExplicitPorts=true; default semantics stay allow-all",
			Status:      "mitigated",
			TestRef:     "TestNetRule_PortDefaultIsAllowAllUnlessStrict",
			Category:    "net",
		},
		{
			Name:        "Unauthorized GPU device access",
			Description: "Touching a device index / node outside the grant",
			BlockedBy:   "GPURule.IsDeviceAllowed + IsNodeAllowed + CanUseGPU, all default-deny",
			Status:      "blocked",
			TestRef:     "TestGPURule_IsDeviceAllowed",
			Category:    "gpu",
		},
		{
			Name:        "VRAM budget overrun",
			Description: "Requesting more VRAM than the grant's MaxMemoryGB",
			BlockedBy:   "GPURule.IsMemoryAllowed (deny when MaxMemoryGB<=0 or request exceeds it)",
			Status:      "blocked",
			TestRef:     "TestGPURule_CanUseGPUAndMemoryBudget",
			Category:    "gpu",
		},
		{
			Name:        "Stack exhaustion via deep recursion",
			Description: "Infinite nested calls OOM host scheduler",
			BlockedBy:   "wazero linear memory bounds + WithCloseOnContextDone termination",
			Status:      "mitigated",
			TestRef:     "wazero_runtime_test",
			Category:    "runtime",
		},
		{
			Name:        "Heap spray host RAM exhaustion",
			Description: "Continuous allocation exhausting available RAM",
			BlockedBy:   "MaxMemoryPages=100 pages enforced at wazero.Runtime level",
			Status:      "mitigated",
			TestRef:     "wazero_runtime_test",
			Category:    "runtime",
		},
		{
			Name:        "WebAssembly linear memory corruption",
			Description: "Out-of-bounds read/write within WASM module",
			BlockedBy:   "wazero core spec enforcement of linear memory boundaries",
			Status:      "blocked",
			TestRef:     "wazero_runtime_test",
			Category:    "runtime",
		},
		{
			Name:        "Runtime compiler exploit",
			Description: "wazero JIT/interpreter bug enabling escape",
			BlockedBy:   "No AOT JIT exposed; interpreter only; upgrade wazero for CVEs",
			Status:      "not_covered",
			TestRef:     "CVE monitoring only",
			Category:    "runtime",
		},
		{
			Name:        "CPU side-channel Spectre/Meltdown",
			Description: "Timing/branch prediction leaking host memory",
			BlockedBy:   "Hardware hypervisor hardening only; not addressable by app-level cap checks",
			Status:      "not_covered",
			TestRef:     "N/A",
			Category:    "other",
		},
		{
			Name:        "Host environment variable leakage",
			Description: "Reading host env vars that were never granted",
			BlockedBy:   "Grant.EnvValue only returns keys present in the grant map; no os.Getenv fallback",
			Status:      "blocked",
			TestRef:     "TestGrant_EnvDenyByDefault",
			Category:    "other",
		},
		{
			Name:        "Empty grant privilege escalation",
			Description: "A plugin shipped without any grant obtaining implicit access",
			BlockedBy:   "NewDefaultGrant leaves every rule nil; Has*Access all report false",
			Status:      "blocked",
			TestRef:     "TestGrant_DefaultDeny",
			Category:    "other",
		},
		{
			Name:        "Unicode-confusable path components",
			Description: "Fullwidth U+FF0E dots surviving the traversal check",
			BlockedBy:   "Not decoded: no OS resolver treats U+FF0E as a parent link, but a downstream NFKC normalization would",
			Status:      "not_covered",
			TestRef:     "TestPathRule_UnicodeConfusablesDocumentedGap",
			Category:    "fs",
		},
		{
			Name:        "Symlink / TOCTOU root escape",
			Description: "A symlink inside a granted root pointing outside it, or a path swapped between check and use",
			BlockedBy:   "Nothing at this layer: IsPathAllowed is a pure string decision and never touches the filesystem",
			Status:      "not_covered",
			TestRef:     "N/A - requires openat2/O_NOFOLLOW at the syscall layer",
			Category:    "fs",
		},
		{
			Name:        "DNS rebinding",
			Description: "An allowed hostname resolving to an internal IP after the check",
			BlockedBy:   "Nothing at this layer: host names are matched as strings, never resolved",
			Status:      "not_covered",
			TestRef:     "N/A - requires resolve-then-pin at dial time",
			Category:    "net",
		},
		{
			Name:        "Timing side channel",
			Description: "Guest measuring host behaviour through wall-clock or cache timing",
			BlockedBy:   "Not addressed; WithSysNanotime still exposes a real monotonic clock",
			Status:      "not_covered",
			TestRef:     "N/A",
			Category:    "runtime",
		},
	}
}

// TotalEscapeVectors returns counts of escape vectors by status.
func TotalEscapeVectors() (total, blocked, mitigated, notCovered int) {
	vectors := KnownEscapeVectors()
	total = len(vectors)
	for _, v := range vectors {
		switch v.Status {
		case "blocked":
			blocked++
		case "mitigated":
			mitigated++
		case "not_covered":
			fallthrough
		case "exposed",
			"partial":
			// Treat old exposed/partial as not covered for consistency
			notCovered++
		}
	}
	return total, blocked, mitigated, notCovered
}