package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// Capability-based Authorization
//
// Go plugins are linked into the host process, so they inherit the host's OS
// privileges. Capability policies therefore govern what a plugin may request
// *through the platform's own APIs* — they are an authorization layer, not a
// sandbox. Anything a plugin does by calling the standard library directly
// (opening a socket, reading a file) is outside this layer's reach; that class
// of containment requires the out-of-process WASM runtime instead.
//
// The model is deny-by-default: an action is refused unless some granted
// capability matches it, and an explicit DenyList entry overrides any grant.
// ============================================================================

// Capability strings follow "verb:resource". Wildcards are allowed in either
// position ("read:*", "*:pods", "*"). These constants cover the actions the
// platform itself checks; plugins may define additional strings.
const (
	CapReadCluster  = "read:cluster"
	CapWriteCluster = "write:cluster"
	CapReadPods     = "read:pods"
	CapWritePods    = "write:pods"
	CapReadMetrics  = "read:metrics"
	CapWriteMetrics = "write:metrics"
	CapAccessGPU    = "access:gpu"
	CapNetworkRead  = "network:read"
	CapNetworkWrite = "network:write"
	CapReadSecrets  = "read:secrets"
	CapAdmin        = "*"
)

// DefaultAuditLogPath is the audit sink used when a SecurityManager is built
// without an explicit path. Callers that must not touch the working tree
// (tests, read-only deployments) should set SecurityConfig.AuditLogPath to a
// temporary path or leave audit-to-file disabled via DisableFileAudit.
const DefaultAuditLogPath = "pkg/plugin/audit.log"

// CapabilityPolicy is the set of capabilities granted to one plugin.
type CapabilityPolicy struct {
	PluginName string `json:"plugin_name"`
	// Permissions lists granted capabilities, e.g. "read:cluster".
	Permissions []string `json:"permissions,omitempty"`
	// DenyList overrides Permissions: a match here always refuses the action,
	// even when Permissions contains "*". Used to carve holes in broad grants.
	DenyList []string `json:"deny_list,omitempty"`
	// Namespace scopes the plugin's logical resource prefix (see hotload.go).
	Namespace string `json:"namespace,omitempty"`
	// GrantedBy records who approved the policy, for audit purposes.
	GrantedBy string    `json:"granted_by,omitempty"`
	GrantedAt time.Time `json:"granted_at,omitempty"`
}

// Validate rejects malformed capability strings so typos fail at grant time
// rather than silently denying every request later.
func (p *CapabilityPolicy) Validate() error {
	if p.PluginName == "" {
		return fmt.Errorf("capability policy: plugin_name is required")
	}
	for _, c := range p.Permissions {
		if err := validateCapability(c); err != nil {
			return fmt.Errorf("capability policy %q: permission %w", p.PluginName, err)
		}
	}
	for _, c := range p.DenyList {
		if err := validateCapability(c); err != nil {
			return fmt.Errorf("capability policy %q: deny_list %w", p.PluginName, err)
		}
	}
	return nil
}

func validateCapability(c string) error {
	if c == "" {
		return fmt.Errorf("entry must not be empty")
	}
	if c == CapAdmin {
		return nil
	}
	parts := strings.Split(c, ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("entry %q must be \"verb:resource\" or \"*\"", c)
	}
	return nil
}

// ============================================================================
// Audit records
// ============================================================================

// AuthzOutcome is the result of a capability check.
type AuthzOutcome string

const (
	// OutcomeAllowed means a granted capability matched the action.
	OutcomeAllowed AuthzOutcome = "allowed"
	// OutcomeDeniedNoPolicy means the plugin has no registered policy at all.
	OutcomeDeniedNoPolicy AuthzOutcome = "denied_no_policy"
	// OutcomeDeniedNoGrant means a policy exists but nothing matched (default deny).
	OutcomeDeniedNoGrant AuthzOutcome = "denied_no_grant"
	// OutcomeDeniedExplicit means the action hit the policy's DenyList.
	OutcomeDeniedExplicit AuthzOutcome = "denied_explicit"
)

// Allowed reports whether the outcome permitted the action.
func (o AuthzOutcome) Allowed() bool { return o == OutcomeAllowed }

// AuthzRecord is one line in the plugin audit log.
type AuthzRecord struct {
	Timestamp time.Time    `json:"timestamp"`
	Plugin    string       `json:"plugin"`
	Action    string       `json:"action"`
	Outcome   AuthzOutcome `json:"outcome"`
	// MatchedRule is the policy entry that decided the outcome, when there was one.
	MatchedRule string `json:"matched_rule,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
}

// String renders a record as a single human-readable audit line.
func (r AuthzRecord) String() string {
	rule := r.MatchedRule
	if rule == "" {
		rule = "-"
	}
	return fmt.Sprintf("%s plugin=%s action=%s outcome=%s rule=%s",
		r.Timestamp.UTC().Format(time.RFC3339), r.Plugin, r.Action, r.Outcome, rule)
}

// ErrCapabilityDenied is returned by Enforce when a capability check fails.
type ErrCapabilityDenied struct {
	Plugin  string
	Action  string
	Outcome AuthzOutcome
}

func (e *ErrCapabilityDenied) Error() string {
	return fmt.Sprintf("plugin %q is not authorized for action %q (%s)", e.Plugin, e.Action, e.Outcome)
}

// ============================================================================
// SecurityManager
// ============================================================================

// SecurityConfig configures a SecurityManager.
type SecurityConfig struct {
	// AuditLogPath is the JSON-lines audit sink. Empty means DefaultAuditLogPath.
	AuditLogPath string
	// DisableFileAudit keeps audit records in memory only. The in-memory buffer
	// is always maintained regardless of this setting.
	DisableFileAudit bool
	// MaxAuditBuffer caps the in-memory record buffer (default 1024). Oldest
	// records are dropped first; the file sink, when enabled, is complete.
	MaxAuditBuffer int
}

// SecurityManager evaluates capability policies and records every decision.
// All methods are safe for concurrent use.
type SecurityManager struct {
	mu       sync.RWMutex
	policies map[string]*CapabilityPolicy

	auditMu   sync.Mutex
	auditFile *os.File
	auditPath string
	records   []AuthzRecord
	maxBuffer int
}

// NewSecurityManager creates a manager with no policies — meaning every action
// is denied until a policy is granted.
func NewSecurityManager(cfg SecurityConfig) (*SecurityManager, error) {
	maxBuf := cfg.MaxAuditBuffer
	if maxBuf <= 0 {
		maxBuf = 1024
	}
	s := &SecurityManager{
		policies:  make(map[string]*CapabilityPolicy),
		maxBuffer: maxBuf,
	}

	if !cfg.DisableFileAudit {
		path := cfg.AuditLogPath
		if path == "" {
			path = DefaultAuditLogPath
		}
		// The path is operator configuration, not request data, but normalize it
		// so a stray ".." segment cannot silently redirect the audit trail.
		path = filepath.Clean(path)
		if strings.Contains(path, ".."+string(filepath.Separator)) || path == ".." {
			return nil, fmt.Errorf("audit log path %q must not traverse parent directories", cfg.AuditLogPath)
		}
		if dir := filepath.Dir(path); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return nil, fmt.Errorf("create audit log directory: %w", err)
			}
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, fmt.Errorf("open audit log %s: %w", path, err)
		}
		s.auditFile = f
		s.auditPath = path
	}
	return s, nil
}

// Close flushes and closes the audit sink.
func (s *SecurityManager) Close() error {
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	if s.auditFile == nil {
		return nil
	}
	err := s.auditFile.Close()
	s.auditFile = nil
	return err
}

// AuditLogPath returns the active audit file path ("" when file audit is off).
func (s *SecurityManager) AuditLogPath() string { return s.auditPath }

// ----------------------------------------------------------------------------
// Policy management
// ----------------------------------------------------------------------------

// Grant registers (or replaces) the policy for a plugin.
func (s *SecurityManager) Grant(policy CapabilityPolicy) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	if policy.GrantedAt.IsZero() {
		policy.GrantedAt = time.Now().UTC()
	}
	s.mu.Lock()
	s.policies[policy.PluginName] = &policy
	s.mu.Unlock()

	s.record(AuthzRecord{
		Timestamp:   time.Now().UTC(),
		Plugin:      policy.PluginName,
		Action:      "policy:grant",
		Outcome:     OutcomeAllowed,
		MatchedRule: strings.Join(policy.Permissions, ","),
		Namespace:   policy.Namespace,
	})
	return nil
}

// Revoke removes a plugin's policy, returning it to deny-everything.
func (s *SecurityManager) Revoke(pluginName string) {
	s.mu.Lock()
	delete(s.policies, pluginName)
	s.mu.Unlock()

	s.record(AuthzRecord{
		Timestamp: time.Now().UTC(),
		Plugin:    pluginName,
		Action:    "policy:revoke",
		Outcome:   OutcomeAllowed,
	})
}

// Policy returns a copy of a plugin's policy.
func (s *SecurityManager) Policy(pluginName string) (*CapabilityPolicy, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.policies[pluginName]
	if !ok {
		return nil, false
	}
	cp := *p
	cp.Permissions = append([]string(nil), p.Permissions...)
	cp.DenyList = append([]string(nil), p.DenyList...)
	return &cp, true
}

// Policies returns all policies sorted by plugin name.
func (s *SecurityManager) Policies() []CapabilityPolicy {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]CapabilityPolicy, 0, len(s.policies))
	for _, p := range s.policies {
		cp := *p
		cp.Permissions = append([]string(nil), p.Permissions...)
		cp.DenyList = append([]string(nil), p.DenyList...)
		out = append(out, cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PluginName < out[j].PluginName })
	return out
}

// ----------------------------------------------------------------------------
// Authorization
// ----------------------------------------------------------------------------

// Allow reports whether a plugin may perform an action, recording the decision.
// An unknown plugin, an unknown action, and an empty policy are all denied.
func (s *SecurityManager) Allow(pluginName string, action string) bool {
	outcome, rule, ns := s.evaluate(pluginName, action)
	s.record(AuthzRecord{
		Timestamp:   time.Now().UTC(),
		Plugin:      pluginName,
		Action:      action,
		Outcome:     outcome,
		MatchedRule: rule,
		Namespace:   ns,
	})
	return outcome.Allowed()
}

// Enforce is Allow with an error return, for call sites that propagate failures.
func (s *SecurityManager) Enforce(pluginName string, action string) error {
	outcome, rule, ns := s.evaluate(pluginName, action)
	s.record(AuthzRecord{
		Timestamp:   time.Now().UTC(),
		Plugin:      pluginName,
		Action:      action,
		Outcome:     outcome,
		MatchedRule: rule,
		Namespace:   ns,
	})
	if !outcome.Allowed() {
		return &ErrCapabilityDenied{Plugin: pluginName, Action: action, Outcome: outcome}
	}
	return nil
}

// Check evaluates an action without writing an audit record. Useful for
// pre-flight UI checks that would otherwise flood the log.
func (s *SecurityManager) Check(pluginName string, action string) AuthzOutcome {
	outcome, _, _ := s.evaluate(pluginName, action)
	return outcome
}

// evaluate resolves an action against a policy: DenyList first, then grants,
// then default-deny. It never writes to the audit log.
func (s *SecurityManager) evaluate(pluginName, action string) (AuthzOutcome, string, string) {
	s.mu.RLock()
	policy, ok := s.policies[pluginName]
	s.mu.RUnlock()

	if !ok {
		return OutcomeDeniedNoPolicy, "", ""
	}
	if action == "" {
		return OutcomeDeniedNoGrant, "", policy.Namespace
	}

	// Explicit deny wins over any grant, including "*".
	for _, rule := range policy.DenyList {
		if capabilityMatches(rule, action) {
			return OutcomeDeniedExplicit, rule, policy.Namespace
		}
	}
	for _, rule := range policy.Permissions {
		if capabilityMatches(rule, action) {
			return OutcomeAllowed, rule, policy.Namespace
		}
	}
	return OutcomeDeniedNoGrant, "", policy.Namespace
}

// capabilityMatches reports whether a policy rule covers a requested action.
// "*" matches everything; either half of "verb:resource" may be "*".
func capabilityMatches(rule, action string) bool {
	if rule == CapAdmin {
		return true
	}
	if rule == action {
		return true
	}
	ruleParts := strings.Split(rule, ":")
	actionParts := strings.Split(action, ":")
	if len(ruleParts) != 2 || len(actionParts) != 2 {
		return false
	}
	verbOK := ruleParts[0] == "*" || ruleParts[0] == actionParts[0]
	resOK := ruleParts[1] == "*" || ruleParts[1] == actionParts[1]
	return verbOK && resOK
}

// ----------------------------------------------------------------------------
// Manifest permission review (used by the marketplace submission gateway)
// ----------------------------------------------------------------------------

// ReviewRequestedPermissions checks a manifest's requested permissions against
// an allowlist of capabilities the marketplace is willing to hand out. It
// returns the entries that must be reviewed by a human before publication.
func ReviewRequestedPermissions(requested []string, allowed []string) (escalations []string) {
	for _, req := range requested {
		if err := validateCapability(req); err != nil {
			escalations = append(escalations, req)
			continue
		}
		// A request for blanket admin is always an escalation.
		if req == CapAdmin {
			escalations = append(escalations, req)
			continue
		}
		covered := false
		for _, a := range allowed {
			if capabilityMatches(a, req) {
				covered = true
				break
			}
		}
		if !covered {
			escalations = append(escalations, req)
		}
	}
	return escalations
}

// ----------------------------------------------------------------------------
// Audit log
// ----------------------------------------------------------------------------

// record appends to the in-memory buffer and, when enabled, the file sink.
// Audit failures never block the authorization path, so a full disk degrades
// to in-memory-only auditing rather than denying legitimate plugin work.
func (s *SecurityManager) record(r AuthzRecord) {
	s.auditMu.Lock()
	defer s.auditMu.Unlock()

	s.records = append(s.records, r)
	if len(s.records) > s.maxBuffer {
		drop := len(s.records) - s.maxBuffer
		s.records = append(s.records[:0], s.records[drop:]...)
	}

	if s.auditFile != nil {
		if line, err := json.Marshal(r); err == nil {
			_, _ = s.auditFile.Write(append(line, '\n'))
		}
	}
}

// AuditRecords returns a copy of the buffered audit records.
func (s *SecurityManager) AuditRecords() []AuthzRecord {
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	out := make([]AuthzRecord, len(s.records))
	copy(out, s.records)
	return out
}

// DeniedCount reports how many buffered records were refusals — a cheap signal
// for "a plugin is probing beyond its grant".
func (s *SecurityManager) DeniedCount() int {
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	n := 0
	for _, r := range s.records {
		if !r.Outcome.Allowed() {
			n++
		}
	}
	return n
}
