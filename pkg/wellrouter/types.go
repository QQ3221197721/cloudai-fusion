// Package wellrouter implements a rule-based routing engine for the 16 AISecOps
// deep wells (Module 6). It composes pkg/eventbus without modifying it: the
// directed connectivity matrix compiled in eventbus/deepwell.go is materialized
// into editable RouteRules (wildcard topic patterns, per-rule hop caps, DLQ
// policy), and every routing decision — forward, reject, dead-letter — is
// recorded both in a local append-only audit log and (when wired) in the signed,
// hash-chained evidence ledger.
//
// Unlike the silent drop at the hop cap in eventbus.WellRouter.route (deepwell.go
// L236-238), this router REJECTS loudly: hop-limit violations return
// ErrHopLimitExceeded, land in a queryable in-memory DLQ (the memory backend has
// no native dead-letter queue), and are attested with the full trace chain.
//
// Storage layout (<root>, default ./.caf):
//
//	<root>/wellrouter/rules.json    rule table (atomic tmp+rename writes)
//	<root>/wellrouter/audit.jsonl   append-only routing audit (offset+truncate guard)
package wellrouter

import (
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/eventbus"
)

// ============================================================================
// Sentinel errors
// ============================================================================

var (
	// ErrHopLimitExceeded is returned when an event's current hop count has
	// already reached the matched rule's MaxHops; the event is dead-lettered.
	ErrHopLimitExceeded = errors.New("wellrouter: hop limit exceeded")

	// ErrRuleNotFound is returned when DeleteRule references an unknown ID.
	ErrRuleNotFound = errors.New("wellrouter: rule not found")

	// ErrNoMatchingRule is returned when no enabled rule matches the event's
	// source well + topic pattern.
	ErrNoMatchingRule = errors.New("wellrouter: no matching rule")
)

// ============================================================================
// Constants — metadata keys mirror eventbus deepwell values (they are private
// there; the string values are the compatibility contract).
// ============================================================================

const (
	// MetaWell mirrors eventbus "well": target/source well number as string.
	MetaWell = "well"
	// MetaWellName mirrors eventbus "well_name": e.g. "L1-intel".
	MetaWellName = "well_name"
	// MetaHop mirrors eventbus "aisecops_hop": hop counter as string.
	MetaHop = "aisecops_hop"
	// MetaForwardedFrom mirrors eventbus "forwarded_from".
	MetaForwardedFrom = "forwarded_from"
	// MetaTrace carries the JSON-encoded []HopRecord chain across hops.
	MetaTrace = "wellrouter_trace"

	// TopicDLQ is the topic dead-lettered events are republished to so
	// operators can subscribe; the canonical copy lives in the in-memory DLQ.
	TopicDLQ = "wellrouter.dlq"

	// DefaultMaxHops is the default and hard cap for any rule's MaxHops.
	DefaultMaxHops = 8
	// MaxHopsCap is the upper bound enforced on rule creation/update.
	MaxHopsCap = 8

	// DefaultDLQCapacity bounds the in-memory DLQ (newest entries kept).
	DefaultDLQCapacity = 1024
	// dedupCapacity bounds the per-rule dedup table.
	dedupCapacity = 65536
)

// ============================================================================
// RouteRule
// ============================================================================

// RouteRule is one declarative forwarding rule: events whose topic matches
// TopicPattern (eventbus wildcard semantics: "*" one segment, ">" all remaining)
// and whose source well equals SourceWell are re-published to every TargetWell
// with hop+1, unless the hop cap rejects them first.
type RouteRule struct {
	// ID is the unique rule identifier, format "rule-<hex8>".
	ID string `json:"id"`

	// TopicPattern matches event topics using eventbus wildcard semantics.
	TopicPattern string `json:"topic_pattern"`

	// SourceWell is the well a published event must originate from.
	SourceWell eventbus.DeepWell `json:"source_well"`

	// TargetWells are the downstream wells derived events are published to.
	TargetWells []eventbus.DeepWell `json:"target_wells"`

	// MaxHops bounds propagation depth; validated to [1, MaxHopsCap].
	MaxHops int `json:"max_hops"`

	// Enabled toggles the rule without deleting it.
	Enabled bool `json:"enabled"`

	// DLQ toggles dead-lettering of hop-rejected events for this rule.
	DLQ bool `json:"dlq"`

	// CreatedAt is rule creation time (UTC).
	CreatedAt time.Time `json:"created_at"`
}

// Validate normalizes and validates the rule: fills defaults (ID, MaxHops,
// Enabled, DLQ, CreatedAt when zero) and rejects malformed input such as an
// invalid well, empty targets, a bad topic, or MaxHoops above the hard cap.
func (r *RouteRule) Validate() error {
	if r.ID == "" {
		r.ID = generateRuleID()
	}
	if r.MaxHops == 0 {
		r.MaxHops = DefaultMaxHops
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	if !r.SourceWell.Valid() {
		return fmt.Errorf("wellrouter: rule %s: invalid source well %d", r.ID, int(r.SourceWell))
	}
	if r.TopicPattern == "" {
		return fmt.Errorf("wellrouter: rule %s: empty topic pattern", r.ID)
	}
	if stringsContainEmptySegment(r.TopicPattern) {
		return fmt.Errorf("wellrouter: rule %s: malformed topic pattern %q", r.ID, r.TopicPattern)
	}
	if len(r.TargetWells) == 0 {
		return fmt.Errorf("wellrouter: rule %s: no target wells", r.ID)
	}
	for _, t := range r.TargetWells {
		if !t.Valid() {
			return fmt.Errorf("wellrouter: rule %s: invalid target well %d", r.ID, int(t))
		}
	}
	if r.MaxHops < 1 || r.MaxHops > MaxHopsCap {
		return fmt.Errorf("wellrouter: rule %s: max_hops must be in [1, %d], got %d", r.ID, MaxHopsCap, r.MaxHops)
	}
	return nil
}

// stringsContainEmptySegment reports whether a dotted topic has empty parts,
// e.g. "a..b" or a trailing dot — guaranteed-unmatchable patterns worth
// rejecting up front instead of silently never firing.
func stringsContainEmptySegment(topic string) bool {
	if topic == "" {
		return true
	}
	start := 0
	for i := 0; i <= len(topic); i++ {
		if i == len(topic) || topic[i] == '.' {
			if i == start {
				return true
			}
			start = i + 1
		}
	}
	return false
}

// generateRuleID returns "rule-<8 hex chars>" from crypto randomness.
func generateRuleID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand never fails on supported platforms; fall back to
		// time-based entropy so ID generation is total.
		return fmt.Sprintf("rule-%08x", time.Now().UnixNano()&0xFFFFFFFF)
	}
	return fmt.Sprintf("rule-%x", b)
}

// ============================================================================
// Trace chain
// ============================================================================

// HopRecord marks one well an event has traversed.
type HopRecord struct {
	Well    string    `json:"well"`
	EventID string    `json:"event_id"`
	At      time.Time `json:"at"`
}

// ============================================================================
// RoutedEvent
// ============================================================================

// RoutedEvent is the routing outcome for one published event: the enriched
// envelope with hop count, accumulated trace, matched rule, and status.
type RoutedEvent struct {
	// EventID is the ID of the (original) event that was routed.
	EventID string `json:"event_id"`

	// Topic is the event topic.
	Topic string `json:"topic"`

	// HopCount is the hop counter at routing time.
	HopCount int `json:"hop_count"`

	// Trace is the full well path accumulated so far (included in
	// "wellrouter.hop.rejected" attestations so rejections are auditable).
	Trace []HopRecord `json:"trace,omitempty"`

	// RuleID is the rule that matched the event.
	RuleID string `json:"rule_id,omitempty"`

	// Status is the routing outcome: "forwarded" | "rejected" | "dlq".
	Status string `json:"status"`

	// Reason explains a rejection ("hop limit exceeded") or forward summary.
	Reason string `json:"reason,omitempty"`

	// CorrelationID threads through to derived events.
	CorrelationID string `json:"correlation_id,omitempty"`

	// RejectedAt is when the routing decision was made (UTC).
	RejectedAt time.Time `json:"rejected_at"`
}

// Routing statuses.
const (
	StatusForwarded = "forwarded"
	StatusRejected  = "rejected"
	StatusDLQ       = "dlq"
)

// ============================================================================
// RouterStats
// ============================================================================

// RouterStats is the router's live counters (snapshot via Stats()).
type RouterStats struct {
	// Forwarded counts derived events successfully published to target wells.
	Forwarded int64 `json:"forwarded"`

	// Rejected counts hop-limit rejections.
	Rejected int64 `json:"rejected"`

	// DLQ counts events placed in the dead-letter queue.
	DLQ int64 `json:"dlq"`

	// DedupSkipped counts target deliveries suppressed by the dedup table.
	DedupSkipped int64 `json:"dedup_skipped"`

	// RulesActive counts enabled rules.
	RulesActive int `json:"rules_active"`

	// RulesTotal counts all rules including disabled ones.
	RulesTotal int `json:"rules_total"`

	// StartTime is router creation time (UTC).
	StartTime time.Time `json:"start_time"`
}

// ============================================================================
// AuditRecord — one JSONL line in <root>/wellrouter/audit.jsonl
// ============================================================================

// AuditRecord is the durable (file-backed) audit trail entry written for every
// routing decision (rule.add / rule.delete / forward / hop.rejected). It
// complements the signed ledger attestation: the ledger proves authenticity,
// audit.jsonl survives without any ledger wired.
type AuditRecord struct {
	// At is the decision time (UTC).
	At time.Time `json:"at"`

	// Action is the decision kind, e.g. "wellrouter.forward".
	Action string `json:"action"`

	// Subject identifies what was acted on (rule ID or event ID).
	Subject string `json:"subject"`

	// Detail carries decision-specific context (wells, hop, reason...).
	Detail any `json:"detail,omitempty"`
}

// ============================================================================
// Default rule set — compiled from the eventbus connectivity matrix
// ============================================================================

// DefaultRules compiles the authoritative connectivity matrix exported by
// pkg/eventbus (deepwell.go L93-110) into one aggregated rule per source well:
// 16 rules total, 39 directed edges, TopicWellEvent topic, MaxHops=8. Behavior
// is compatible with eventbus.WellRouter while remaining editable at runtime.
func DefaultRules() []*RouteRule {
	wells := eventbus.AllWells()
	rules := make([]*RouteRule, 0, len(wells))
	for _, src := range wells {
		targets := eventbus.DownstreamWells(src)
		if len(targets) == 0 {
			continue
		}
		rules = append(rules, &RouteRule{
			ID:           generateRuleID(),
			TopicPattern: eventbus.TopicWellEvent,
			SourceWell:   src,
			TargetWells:  targets,
			MaxHops:      DefaultMaxHops,
			Enabled:      true,
			DLQ:          true,
			CreatedAt:    time.Now().UTC(),
		})
	}
	return rules
}

// DefaultRuleEdgeCount returns the number of directed edges in the compiled
// default rule set (sum of targets across the 16 per-source rules).
func DefaultRuleEdgeCount() int {
	total := 0
	for _, src := range eventbus.AllWells() {
		total += len(eventbus.DownstreamWells(src))
	}
	return total
}
