package audit

// evidence_chain.go implements Module 36: an immutable, tamper-evident audit
// evidence chain plus a compliance rule engine and signed report generation.
//
// Three capabilities, built on real Ed25519 crypto (reusing pkg/evidence's
// ReceiptBuilder / Receipt / VerifyChainOfReceipts — no new crypto invented):
//
//  1. EvidenceChain: every AuditEvent is Ed25519-signed and hash-chained to its
//     predecessor, so three tampering vectors are all detectable OFFLINE with
//     only the public key:
//       (a) editing a stored event body   -> event/hash mismatch
//       (b) forging a receipt field        -> signature verification fails
//       (c) deleting / reordering entries  -> receipt chain linkage breaks
//
//  2. RuleEngine: policy -> condition -> action. Conditions compose field
//     predicates (eq/ne/contains/regex/gt/lt/in) with AND/OR/NOT, so an operator
//     can express "critical auth failure from outside CN" and attach an action
//     (alert/deny/escalate/tag/notify) that fires as events are recorded.
//
//  3. Signed reports: GenerateReport summarizes a period, is rendered to JSON or
//     Markdown, and is itself sealed with a Receipt so the report is offline-
//     verifiable and non-repudiable.
//
// Competitors emit logs; this emits proofs.

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// ============================================================================
// Evidence Chain
// ============================================================================

// ChainedAuditEntry pairs an audit event with the signed receipt that commits
// to it, plus any rule findings triggered when the event was recorded.
type ChainedAuditEntry struct {
	Event    *AuditEvent       `json:"event"`
	Receipt  *evidence.Receipt `json:"receipt"`
	Findings []RuleMatch       `json:"findings,omitempty"`
}

// EvidenceChain is an append-only, cryptographically chained audit log. Each
// entry is Ed25519-signed and hash-linked to its predecessor, making the trail
// unforgeable and offline-verifiable — the Module 36 moat over plaintext logs.
type EvidenceChain struct {
	mu         sync.Mutex
	builder    *evidence.ReceiptBuilder
	pub        ed25519.PublicKey
	priv       ed25519.PrivateKey
	entries    []*ChainedAuditEntry
	engine     *RuleEngine
	maxEntries int
}

// NewEvidenceChain creates a chain backed by a fresh Ed25519 key. maxEntries<=0
// keeps an unbounded trail (the full, verifiable history).
func NewEvidenceChain(maxEntries int) *EvidenceChain {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	return &EvidenceChain{
		builder:    evidence.NewReceiptBuilder("audit.evidence-chain", priv),
		pub:        pub,
		priv:       priv,
		engine:     NewRuleEngine(),
		maxEntries: maxEntries,
	}
}

// Engine returns the rule engine so callers can register compliance rules.
func (c *EvidenceChain) Engine() *RuleEngine { return c.engine }

// PublicKey returns the verifier key required to check the chain offline. A
// third party needs nothing else — no server, no database — to audit it.
func (c *EvidenceChain) PublicKey() ed25519.PublicKey { return c.pub }

// Append records a signed, chained audit event and evaluates the rule engine
// against it. The receipt's OutputHash is SHA-256(event JSON); a later edit of
// the stored event is detectable by re-hashing during Verify.
func (c *EvidenceChain) Append(event *AuditEvent) (*ChainedAuditEntry, error) {
	if event == nil {
		return nil, fmt.Errorf("audit: nil event")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if event.ID == "" {
		event.ID = fmt.Sprintf("evt_%d", time.Now().UnixNano())
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	receipt, err := c.builder.Build("audit.event", event, event)
	if err != nil {
		return nil, fmt.Errorf("audit: sign event: %w", err)
	}

	findings := c.engine.Evaluate(event)
	entry := &ChainedAuditEntry{Event: event, Receipt: receipt, Findings: findings}
	c.entries = append(c.entries, entry)

	// Bounded trails evict the oldest entries; this breaks offline chain
	// linkage by design (the retained window still verifies on its own only if
	// the caller re-anchors), so unbounded is the auditable default.
	if c.maxEntries > 0 && len(c.entries) > c.maxEntries {
		c.entries = c.entries[len(c.entries)-c.maxEntries:]
	}
	return entry, nil
}

// Entries returns a snapshot copy of the chain in insertion order.
func (c *EvidenceChain) Entries() []*ChainedAuditEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*ChainedAuditEntry, len(c.entries))
	copy(out, c.entries)
	return out
}

// Size returns the number of entries currently held.
func (c *EvidenceChain) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// auditEventHash recomputes the SHA-256 a receipt committed to for an event. It
// must mirror ReceiptBuilder.Build's output hashing (json.Marshal + Sum256).
func auditEventHash(ev *AuditEvent) ([32]byte, error) {
	b, err := json.Marshal(ev)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(b), nil
}

// VerifyAuditChain checks a chain offline and returns the index of the first
// tampered entry (or -1 when intact) plus an explanatory error. It enforces,
// per entry: (1) the signer identity matches the expected key, (2) a valid
// Ed25519 signature, (3) that the stored event still hashes to the committed
// value; then (4) that receipt chain linkage is unbroken across the whole trail.
func VerifyAuditChain(entries []*ChainedAuditEntry, pub ed25519.PublicKey) (int, error) {
	receipts := make([]*evidence.Receipt, 0, len(entries))
	for i, e := range entries {
		if e == nil || e.Receipt == nil {
			return i, fmt.Errorf("audit: entry %d has no receipt", i)
		}
		if !e.Receipt.SignerPublicKey.Equal(pub) {
			return i, fmt.Errorf("audit: entry %d signed by an unexpected key", i)
		}
		if !e.Receipt.Verify() {
			return i, fmt.Errorf("audit: receipt signature invalid at entry %d (tampered)", i)
		}
		h, err := auditEventHash(e.Event)
		if err != nil {
			return i, err
		}
		if h != e.Receipt.OutputHash {
			return i, fmt.Errorf("audit: event/hash mismatch at entry %d (record tampered)", i)
		}
		receipts = append(receipts, e.Receipt)
	}
	if err := evidence.VerifyChainOfReceipts(receipts); err != nil {
		return -1, err
	}
	return -1, nil
}

// Verify is the method form of VerifyAuditChain over this chain's own entries.
func (c *EvidenceChain) Verify() (int, error) {
	return VerifyAuditChain(c.Entries(), c.pub)
}

// ============================================================================
// Rule Engine — policy -> condition -> action
// ============================================================================

// Operator is a field-level comparison operator.
type Operator string

const (
	OpEq       Operator = "eq"       // exact string equality
	OpNe       Operator = "ne"       // inequality
	OpContains Operator = "contains" // substring match
	OpRegex    Operator = "regex"    // RE2 regular expression match
	OpGt       Operator = "gt"       // numeric greater-than
	OpLt       Operator = "lt"       // numeric less-than
	OpIn       Operator = "in"       // membership in a comma-separated set
)

// Condition evaluates to true/false against an audit event.
type Condition interface {
	Eval(e *AuditEvent) bool
	Validate() error
}

// FieldCondition compares a single event field against a value with an operator.
type FieldCondition struct {
	Field string   `json:"field"`
	Op    Operator `json:"op"`
	Value string   `json:"value"`

	re *regexp.Regexp // compiled lazily by Validate for OpRegex
}

// Validate pre-compiles regex conditions and rejects unknown operators.
func (f *FieldCondition) Validate() error {
	switch f.Op {
	case OpEq, OpNe, OpContains, OpGt, OpLt, OpIn:
		return nil
	case OpRegex:
		re, err := regexp.Compile(f.Value)
		if err != nil {
			return fmt.Errorf("audit: invalid regex %q for field %q: %w", f.Value, f.Field, err)
		}
		f.re = re
		return nil
	default:
		return fmt.Errorf("audit: unknown operator %q", f.Op)
	}
}

// Eval resolves the event field and applies the operator.
func (f *FieldCondition) Eval(e *AuditEvent) bool {
	got := eventField(e, f.Field)
	switch f.Op {
	case OpEq:
		return got == f.Value
	case OpNe:
		return got != f.Value
	case OpContains:
		return strings.Contains(got, f.Value)
	case OpRegex:
		if f.re == nil {
			// Not validated; compile defensively (Validate is the fast path).
			re, err := regexp.Compile(f.Value)
			if err != nil {
				return false
			}
			f.re = re
		}
		return f.re.MatchString(got)
	case OpGt, OpLt:
		gv, err1 := strconv.ParseFloat(got, 64)
		wv, err2 := strconv.ParseFloat(f.Value, 64)
		if err1 != nil || err2 != nil {
			return false
		}
		if f.Op == OpGt {
			return gv > wv
		}
		return gv < wv
	case OpIn:
		for _, part := range strings.Split(f.Value, ",") {
			if strings.TrimSpace(part) == got {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// AndCondition is true when all sub-conditions are true.
type AndCondition struct {
	Conditions []Condition `json:"and"`
}

func (a *AndCondition) Eval(e *AuditEvent) bool {
	for _, c := range a.Conditions {
		if !c.Eval(e) {
			return false
		}
	}
	return true
}

func (a *AndCondition) Validate() error {
	if len(a.Conditions) == 0 {
		return fmt.Errorf("audit: AND condition has no operands")
	}
	for _, c := range a.Conditions {
		if err := c.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// OrCondition is true when any sub-condition is true.
type OrCondition struct {
	Conditions []Condition `json:"or"`
}

func (o *OrCondition) Eval(e *AuditEvent) bool {
	for _, c := range o.Conditions {
		if c.Eval(e) {
			return true
		}
	}
	return false
}

func (o *OrCondition) Validate() error {
	if len(o.Conditions) == 0 {
		return fmt.Errorf("audit: OR condition has no operands")
	}
	for _, c := range o.Conditions {
		if err := c.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// NotCondition negates its inner condition.
type NotCondition struct {
	Condition Condition `json:"not"`
}

func (n *NotCondition) Eval(e *AuditEvent) bool { return !n.Condition.Eval(e) }
func (n *NotCondition) Validate() error {
	if n.Condition == nil {
		return fmt.Errorf("audit: NOT condition has no operand")
	}
	return n.Condition.Validate()
}

// eventField resolves a dotted field name to its string value on the event.
// "metadata.<key>" reaches into the metadata map; unknown fields yield "".
func eventField(e *AuditEvent, field string) string {
	switch field {
	case "action":
		return e.Action
	case "resource":
		return e.Resource
	case "resource_id":
		return e.ResourceID
	case "result":
		return e.Result
	case "severity":
		return string(e.Severity)
	case "category":
		return string(e.Category)
	case "username":
		return e.Username
	case "user_id":
		return e.UserID
	case "ip_address":
		return e.IPAddress
	case "tenant_id":
		return e.TenantID
	case "session_id":
		return e.SessionID
	case "status_code":
		return strconv.Itoa(e.StatusCode)
	default:
		if strings.HasPrefix(field, "metadata.") {
			if e.Metadata == nil {
				return ""
			}
			return e.Metadata[strings.TrimPrefix(field, "metadata.")]
		}
		return ""
	}
}

// ActionType classifies the reaction a matched rule triggers.
type ActionType string

const (
	ActionAlert    ActionType = "alert"
	ActionDeny     ActionType = "deny"
	ActionEscalate ActionType = "escalate"
	ActionTag      ActionType = "tag"
	ActionNotify   ActionType = "notify"
)

// RuleAction is what happens when a rule's condition matches an event.
type RuleAction struct {
	Type    ActionType        `json:"type"`
	Message string            `json:"message,omitempty"`
	Params  map[string]string `json:"params,omitempty"`
}

// Rule binds a condition to an action, with a priority for ordering matches.
type Rule struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Priority    int        `json:"priority"`
	Enabled     bool       `json:"enabled"`
	Condition   Condition  `json:"condition"`
	Action      RuleAction `json:"action"`
}

// RuleMatch is a single rule firing against a single event.
type RuleMatch struct {
	RuleID    string     `json:"rule_id"`
	RuleName  string     `json:"rule_name"`
	Action    RuleAction `json:"action"`
	MatchedAt time.Time  `json:"matched_at"`
}

// RuleEngine holds an ordered set of compliance rules.
type RuleEngine struct {
	mu    sync.RWMutex
	rules []*Rule
}

// NewRuleEngine returns an empty rule engine.
func NewRuleEngine() *RuleEngine { return &RuleEngine{} }

// AddRule validates and registers a rule. Rules with nil conditions or bad
// regex are rejected up front so evaluation never fails silently.
func (re *RuleEngine) AddRule(r *Rule) error {
	if r == nil {
		return fmt.Errorf("audit: nil rule")
	}
	if r.ID == "" {
		return fmt.Errorf("audit: rule has empty ID")
	}
	if r.Condition == nil {
		return fmt.Errorf("audit: rule %q has nil condition", r.ID)
	}
	if err := r.Condition.Validate(); err != nil {
		return fmt.Errorf("audit: rule %q: %w", r.ID, err)
	}
	re.mu.Lock()
	defer re.mu.Unlock()
	re.rules = append(re.rules, r)
	return nil
}

// Rules returns a snapshot of the registered rules.
func (re *RuleEngine) Rules() []*Rule {
	re.mu.RLock()
	defer re.mu.RUnlock()
	out := make([]*Rule, len(re.rules))
	copy(out, re.rules)
	return out
}

// Evaluate returns every matching rule for an event, highest priority first.
func (re *RuleEngine) Evaluate(e *AuditEvent) []RuleMatch {
	re.mu.RLock()
	rules := make([]*Rule, len(re.rules))
	copy(rules, re.rules)
	re.mu.RUnlock()

	var matches []RuleMatch
	now := time.Now().UTC()
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		if r.Condition.Eval(e) {
			matches = append(matches, RuleMatch{
				RuleID:    r.ID,
				RuleName:  r.Name,
				Action:    r.Action,
				MatchedAt: now,
			})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return priorityOf(rules, matches[i].RuleID) > priorityOf(rules, matches[j].RuleID)
	})
	return matches
}

func priorityOf(rules []*Rule, id string) int {
	for _, r := range rules {
		if r.ID == id {
			return r.Priority
		}
	}
	return 0
}

// ============================================================================
// Signed Report Generation (JSON / Markdown)
// ============================================================================

// AuditReport is an auto-generated summary of a chain over a time window. It is
// sealed with a Receipt so the report itself is offline-verifiable.
type AuditReport struct {
	ID                string         `json:"id"`
	GeneratedAt       time.Time      `json:"generated_at"`
	PeriodStart       time.Time      `json:"period_start"`
	PeriodEnd         time.Time      `json:"period_end"`
	TotalEvents       int            `json:"total_events"`
	ChainVerified     bool           `json:"chain_verified"`
	TamperIndex       int            `json:"tamper_index"` // -1 when intact
	SeverityBreakdown map[string]int `json:"severity_breakdown"`
	CategoryBreakdown map[string]int `json:"category_breakdown"`
	ResultBreakdown   map[string]int `json:"result_breakdown"`
	RuleFindings      []RuleMatch    `json:"rule_findings"`

	// Signature seals the report body (everything above). Nil until signed.
	Signature *evidence.Receipt `json:"signature,omitempty"`
}

// GenerateReport builds a signed report over events within [start,end]. A zero
// start/end is treated as unbounded on that side.
func (c *EvidenceChain) GenerateReport(start, end time.Time) (*AuditReport, error) {
	entries := c.Entries()
	tamperIdx, verr := VerifyAuditChain(entries, c.pub)

	report := &AuditReport{
		ID:                fmt.Sprintf("rpt_%d", time.Now().UnixNano()),
		GeneratedAt:       time.Now().UTC(),
		PeriodStart:       start,
		PeriodEnd:         end,
		ChainVerified:     verr == nil,
		TamperIndex:       tamperIdx,
		SeverityBreakdown: map[string]int{},
		CategoryBreakdown: map[string]int{},
		ResultBreakdown:   map[string]int{},
	}

	for _, e := range entries {
		ts := e.Event.Timestamp
		if !start.IsZero() && ts.Before(start) {
			continue
		}
		if !end.IsZero() && ts.After(end) {
			continue
		}
		report.TotalEvents++
		report.SeverityBreakdown[string(e.Event.Severity)]++
		report.CategoryBreakdown[string(e.Event.Category)]++
		report.ResultBreakdown[e.Event.Result]++
		report.RuleFindings = append(report.RuleFindings, e.Findings...)
	}

	// Seal the report body: sign over the report with its Signature field nil.
	receipt, err := c.builder.Build("audit.report", report, report)
	if err != nil {
		return nil, fmt.Errorf("audit: sign report: %w", err)
	}
	report.Signature = receipt
	return report, nil
}

// VerifyReport re-hashes the report body (signature omitted) and checks the
// sealing receipt's signature against pub. Returns nil when the report is intact.
func VerifyReport(r *AuditReport, pub ed25519.PublicKey) error {
	if r == nil || r.Signature == nil {
		return fmt.Errorf("audit: report is not signed")
	}
	if !r.Signature.SignerPublicKey.Equal(pub) {
		return fmt.Errorf("audit: report signed by an unexpected key")
	}
	if !r.Signature.Verify() {
		return fmt.Errorf("audit: report signature invalid (tampered)")
	}
	// Re-hash the body with Signature stripped, mirroring GenerateReport's Build.
	body := *r
	body.Signature = nil
	b, err := json.Marshal(&body)
	if err != nil {
		return err
	}
	if sha256.Sum256(b) != r.Signature.OutputHash {
		return fmt.Errorf("audit: report body/hash mismatch (tampered)")
	}
	return nil
}

// ToJSON renders the report as indented JSON (includes the signature).
func (r *AuditReport) ToJSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// ToMarkdown renders a human-readable, signed compliance report.
func (r *AuditReport) ToMarkdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Audit Evidence Report\n\n")
	fmt.Fprintf(&b, "- **Report ID:** `%s`\n", r.ID)
	fmt.Fprintf(&b, "- **Generated:** %s\n", r.GeneratedAt.Format(time.RFC3339))
	if !r.PeriodStart.IsZero() || !r.PeriodEnd.IsZero() {
		fmt.Fprintf(&b, "- **Period:** %s → %s\n",
			fmtTime(r.PeriodStart), fmtTime(r.PeriodEnd))
	}
	fmt.Fprintf(&b, "- **Total Events:** %d\n", r.TotalEvents)
	if r.ChainVerified {
		fmt.Fprintf(&b, "- **Chain Integrity:** ✅ verified (offline, public-key only)\n")
	} else {
		fmt.Fprintf(&b, "- **Chain Integrity:** ❌ TAMPERED at entry %d\n", r.TamperIndex)
	}
	if r.Signature != nil {
		fmt.Fprintf(&b, "- **Report Signature:** `%s` (receipt `%s`)\n",
			shortHash(r.Signature.OutputHash), r.Signature.ID)
	}

	writeBreakdown(&b, "Severity", r.SeverityBreakdown)
	writeBreakdown(&b, "Category", r.CategoryBreakdown)
	writeBreakdown(&b, "Result", r.ResultBreakdown)

	fmt.Fprintf(&b, "\n## Rule Findings (%d)\n\n", len(r.RuleFindings))
	if len(r.RuleFindings) == 0 {
		b.WriteString("_No rules matched in this period._\n")
	} else {
		b.WriteString("| Rule | Action | Message | Matched At |\n")
		b.WriteString("|---|---|---|---|\n")
		for _, f := range r.RuleFindings {
			fmt.Fprintf(&b, "| %s | %s | %s | %s |\n",
				f.RuleName, f.Action.Type, f.Action.Message,
				f.MatchedAt.Format(time.RFC3339))
		}
	}
	return b.String()
}

func writeBreakdown(b *strings.Builder, title string, m map[string]int) {
	if len(m) == 0 {
		return
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintf(b, "\n## %s Breakdown\n\n", title)
	b.WriteString("| " + title + " | Count |\n|---|---|\n")
	for _, k := range keys {
		label := k
		if label == "" {
			label = "(unspecified)"
		}
		fmt.Fprintf(b, "| %s | %d |\n", label, m[k])
	}
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "(open)"
	}
	return t.Format(time.RFC3339)
}

func shortHash(h [32]byte) string {
	return fmt.Sprintf("%x", h[:6])
}
