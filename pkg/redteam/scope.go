// Package redteam is the Verifiable AI Red Team subsystem (M0 foundation).
//
// It provides authorized, evidence-grade, scope-enforced security-validation
// primitives. This foundational layer contains NO offensive tooling: it is the
// authorization gate, engagement state machine, and evidence taxonomy onto which
// later milestones attach real tools (behind a signed scope + human-in-the-loop).
//
// Design principles (mirroring docs/architecture.md "honesty over illusion"):
//   - Evidence-first: no decision or lifecycle transition happens without a signed
//     receipt in pkg/evidence ("if it isn't in the ledger, it didn't happen").
//   - Authorization-first: every action is checked against a signed Scope; an
//     out-of-scope action is refused AND recorded (redteam.scope.deny).
//   - Additive & side-effect-free: this package imports platform primitives but
//     mutates no existing subsystem.
package redteam

import (
	"net"
	"net/url"
	"strings"
	"time"
)

// RiskTier ranks how intrusive an action is. Higher tiers require broader
// authorization and (optionally) human approval.
type RiskTier int

const (
	// RiskReadOnly covers passive/active reconnaissance that does not alter or
	// exploit the target (scanning, fingerprinting, enumeration).
	RiskReadOnly RiskTier = iota
	// RiskExploit covers active exploitation of a vulnerability.
	RiskExploit
	// RiskLateral covers post-exploitation, lateral movement, and persistence.
	RiskLateral
)

// String returns a stable, human-readable label used in evidence payloads.
func (r RiskTier) String() string {
	switch r {
	case RiskReadOnly:
		return "read-only"
	case RiskExploit:
		return "exploit"
	case RiskLateral:
		return "lateral"
	default:
		return "unknown"
	}
}

// TargetKind classifies how a Target value is matched.
type TargetKind string

const (
	TargetCIDR    TargetKind = "cidr"    // e.g. 10.0.0.0/24
	TargetHost    TargetKind = "host"    // exact host or subdomain suffix
	TargetURL     TargetKind = "url"     // URL prefix
	TargetCluster TargetKind = "cluster" // named cluster reference
)

// Target is one authorized endpoint within a Scope.
type Target struct {
	Kind  TargetKind `json:"kind"`
	Value string     `json:"value"`
}

// TimeWindow bounds when an engagement may act. Zero Start/End means unbounded
// on that side.
type TimeWindow struct {
	Start time.Time `json:"start,omitempty"`
	End   time.Time `json:"end,omitempty"`
}

// RateLimit caps the number of authorized actions per rolling window.
type RateLimit struct {
	MaxActions int           `json:"max_actions"` // 0 = unlimited
	Per        time.Duration `json:"per"`
}

// Scope is the SIGNED authorization contract for an engagement. Nothing runs
// outside it. All matching is deny-by-default: an empty allow-set authorizes
// nothing.
type Scope struct {
	Targets         []Target   `json:"targets"`
	AllowTechniques []string   `json:"allow_techniques"` // MITRE ATT&CK IDs; "*" = any
	DenyTechniques  []string   `json:"deny_techniques"`
	Window          TimeWindow `json:"window"`
	MaxRiskTier     RiskTier   `json:"max_risk_tier"`
	ApprovalReq     RiskTier   `json:"approval_required_at"` // actions >= this tier need approval
	RateLimit       RateLimit  `json:"rate_limit"`
}

// InScope reports whether target (an IP, host, URL, or cluster ref) is covered
// by any Target in the scope.
func (s Scope) InScope(target string) bool {
	for _, t := range s.Targets {
		if t.matches(target) {
			return true
		}
	}
	return false
}

func (t Target) matches(target string) bool {
	switch t.Kind {
	case TargetCIDR:
		_, ipnet, err := net.ParseCIDR(t.Value)
		if err != nil {
			return false
		}
		ip := net.ParseIP(hostOnly(target))
		return ip != nil && ipnet.Contains(ip)
	case TargetHost:
		h := hostOnly(target)
		return h == t.Value || strings.HasSuffix(h, "."+t.Value)
	case TargetURL:
		return strings.HasPrefix(target, t.Value)
	case TargetCluster:
		return target == t.Value
	default:
		return false
	}
}

// TechniqueAllowed reports whether a MITRE ATT&CK technique ID is authorized.
// Deny-by-default: a technique must be explicitly allowed (or "*") and not denied.
func (s Scope) TechniqueAllowed(id string) bool {
	for _, d := range s.DenyTechniques {
		if d == id {
			return false
		}
	}
	for _, a := range s.AllowTechniques {
		if a == id || a == "*" {
			return true
		}
	}
	return false
}

// WithinWindow reports whether t is inside the authorized time window.
func (s Scope) WithinWindow(t time.Time) bool {
	if !s.Window.Start.IsZero() && t.Before(s.Window.Start) {
		return false
	}
	if !s.Window.End.IsZero() && t.After(s.Window.End) {
		return false
	}
	return true
}

// hostOnly extracts a bare host from an IP, host:port, or URL.
func hostOnly(target string) string {
	s := target
	if strings.Contains(s, "://") {
		if u, err := url.Parse(s); err == nil && u.Host != "" {
			s = u.Host
		}
	} else if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		return h
	}
	return s
}
