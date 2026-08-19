package audit

import (
	"strings"
	"testing"
	"time"
)

// ============================================================================
// Evidence Chain — tamper evidence
// ============================================================================

func TestEvidenceChain_AppendIntact(t *testing.T) {
	ch := NewEvidenceChain(0) // unbounded

	for i := 0; i < 10; i++ {
		entry, err := ch.Append(&AuditEvent{
			Action:   "login",
			UserID:   "user1",
			Result:   "success",
			Severity: SeverityInfo,
		})
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
		if entry.Receipt == nil {
			t.Fatalf("entry %d has no receipt", i)
		}
	}

	idx, err := ch.Verify()
	if err != nil || idx != -1 {
		t.Fatalf("intact chain should verify, got tamper at %d: %v", idx, err)
	}

	// Offline verification with only the public key.
	if idx, err := VerifyAuditChain(ch.Entries(), ch.PublicKey()); err != nil {
		t.Fatalf("offline verify must pass, got tamper at %d: %v", idx, err)
	}
}

func TestEvidenceChain_DetectsContentTamper(t *testing.T) {
	ch := NewEvidenceChain(0)
	ch.Append(&AuditEvent{Action: "a1", UserID: "u1"})
	ch.Append(&AuditEvent{Action: "a2", UserID: "u2"})
	ch.Append(&AuditEvent{Action: "a3", UserID: "u3"})

	entries := ch.Entries()
	entries[1].Event.Result = "denied_fake_edit" // rewrite a stored event body

	idx, err := VerifyAuditChain(entries, ch.PublicKey())
	if err == nil {
		t.Fatal("expected tamper detection after content edit, got none")
	}
	if idx != 1 {
		t.Fatalf("expected tamper at index 1, got %d (%v)", idx, err)
	}
	t.Logf("content-tamper correctly detected at entry %d: %v", idx, err)
}

func TestEvidenceChain_DetectsReceiptForgery(t *testing.T) {
	ch := NewEvidenceChain(0)
	ch.Append(&AuditEvent{Action: "x1", UserID: "u1"})
	ch.Append(&AuditEvent{Action: "x2", UserID: "u2"})

	entries := ch.Entries()
	entries[0].Receipt.OutputHash[0] ^= 0xFF // forge committed output hash

	idx, err := VerifyAuditChain(entries, ch.PublicKey())
	if err == nil {
		t.Fatal("expected signature failure after receipt forgery")
	}
	if idx != 0 {
		t.Fatalf("expected forgery at index 0, got %d (%v)", idx, err)
	}
	t.Logf("receipt-forgery correctly detected at entry %d: %v", idx, err)
}

func TestEvidenceChain_DetectsDeletion(t *testing.T) {
	ch := NewEvidenceChain(0)
	ch.Append(&AuditEvent{Action: "a", UserID: "u1"})
	ch.Append(&AuditEvent{Action: "b", UserID: "u2"})
	ch.Append(&AuditEvent{Action: "c", UserID: "u3"})

	entries := ch.Entries()
	tampered := []*ChainedAuditEntry{entries[0], entries[2]} // delete the middle

	if _, err := VerifyAuditChain(tampered, ch.PublicKey()); err == nil {
		t.Fatal("expected chain-linkage failure after deletion")
	} else {
		t.Logf("deletion correctly detected via broken chain: %v", err)
	}
}

func TestEvidenceChain_BoundedBufferEvictsOldest(t *testing.T) {
	const maxEntries = 5
	ch := NewEvidenceChain(maxEntries)
	for i := 0; i < 10; i++ {
		if _, err := ch.Append(&AuditEvent{UserID: "u", Action: "op"}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	if ch.Size() != maxEntries {
		t.Fatalf("expected bounded size %d, got %d", maxEntries, ch.Size())
	}
}

func TestEvidenceChain_NilEventRejected(t *testing.T) {
	ch := NewEvidenceChain(0)
	if _, err := ch.Append(nil); err == nil {
		t.Fatal("nil event should return error")
	}
}

func TestEvidenceChain_NegativeMaxIsUnbounded(t *testing.T) {
	ch := NewEvidenceChain(-1)
	for i := 0; i < 100; i++ {
		_, _ = ch.Append(&AuditEvent{UserID: "u1"})
	}
	if ch.Size() != 100 {
		t.Fatalf("expected 100 events for negative max, got %d", ch.Size())
	}
}

// ============================================================================
// Rule Engine
// ============================================================================

func TestRuleEngine_SimpleCondition(t *testing.T) {
	re := NewRuleEngine()
	err := re.AddRule(&Rule{
		ID:        "r1",
		Name:      "success-login",
		Priority:  10,
		Enabled:   true,
		Condition: &FieldCondition{Field: "result", Op: OpEq, Value: "success"},
		Action:    RuleAction{Type: ActionAlert},
	})
	if err != nil {
		t.Fatalf("add rule: %v", err)
	}
	if got := re.Evaluate(&AuditEvent{Result: "success"}); len(got) != 1 {
		t.Fatalf("expected 1 match, got %d", len(got))
	}
	if got := re.Evaluate(&AuditEvent{Result: "failure"}); len(got) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(got))
	}
}

func TestRuleEngine_ComplexConditions(t *testing.T) {
	re := NewRuleEngine()
	cond := &AndCondition{Conditions: []Condition{
		&OrCondition{Conditions: []Condition{
			&FieldCondition{Field: "severity", Op: OpEq, Value: "critical"},
			&FieldCondition{Field: "category", Op: OpEq, Value: "authentication"},
		}},
		&FieldCondition{Field: "result", Op: OpEq, Value: "failure"},
	}}
	if err := re.AddRule(&Rule{ID: "r-complex", Name: "critical-auth-fail", Priority: 100, Enabled: true, Condition: cond, Action: RuleAction{Type: ActionDeny}}); err != nil {
		t.Fatalf("add rule: %v", err)
	}

	cases := []struct {
		ev     *AuditEvent
		expect bool
	}{
		{&AuditEvent{Severity: SeverityCritical, Category: CategoryAuth, Result: "failure"}, true},
		{&AuditEvent{Severity: SeverityWarning, Category: CategoryData, Result: "failure"}, false},
		{&AuditEvent{Severity: SeverityCritical, Category: CategoryAuth, Result: "success"}, false},
	}
	for _, tc := range cases {
		got := len(re.Evaluate(tc.ev)) > 0
		if got != tc.expect {
			t.Errorf("event %+v: expected match=%v got=%v", tc.ev, tc.expect, got)
		}
	}
}

func TestRuleEngine_RegexCondition(t *testing.T) {
	re := NewRuleEngine()
	if err := re.AddRule(&Rule{
		ID: "r-ip", Name: "internal-only", Priority: 50, Enabled: true,
		Condition: &FieldCondition{Field: "ip_address", Op: OpRegex, Value: `^10\..*`},
		Action:    RuleAction{Type: ActionTag},
	}); err != nil {
		t.Fatalf("add rule: %v", err)
	}
	cases := map[string]bool{"10.0.0.1": true, "192.168.1.1": false, "": false}
	for ip, expect := range cases {
		got := len(re.Evaluate(&AuditEvent{IPAddress: ip})) > 0
		if got != expect {
			t.Errorf("ip %q: expected match=%v got=%v", ip, expect, got)
		}
	}
}

func TestRuleEngine_InvalidRegexRejected(t *testing.T) {
	re := NewRuleEngine()
	err := re.AddRule(&Rule{
		ID: "bad", Name: "bad", Enabled: true,
		Condition: &FieldCondition{Field: "action", Op: OpRegex, Value: "([unclosed"},
	})
	if err == nil {
		t.Fatal("invalid regex should be rejected at AddRule")
	}
}

func TestRuleEngine_NumericAndMetadata(t *testing.T) {
	re := NewRuleEngine()
	_ = re.AddRule(&Rule{ID: "gt", Name: "server-error", Enabled: true, Priority: 5,
		Condition: &FieldCondition{Field: "status_code", Op: OpGt, Value: "499"},
		Action:    RuleAction{Type: ActionEscalate}})
	_ = re.AddRule(&Rule{ID: "meta", Name: "flagged", Enabled: true, Priority: 9,
		Condition: &FieldCondition{Field: "metadata.flag", Op: OpEq, Value: "true"},
		Action:    RuleAction{Type: ActionNotify}})

	ev := &AuditEvent{StatusCode: 500, Metadata: map[string]string{"flag": "true"}}
	matches := re.Evaluate(ev)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}
	// Highest priority first.
	if matches[0].RuleID != "meta" {
		t.Fatalf("expected highest-priority rule 'meta' first, got %q", matches[0].RuleID)
	}
}

func TestRuleEngine_NilRuleRejected(t *testing.T) {
	if err := NewRuleEngine().AddRule(nil); err == nil {
		t.Fatal("nil rule should fail validation")
	}
}

// ============================================================================
// Signed Reports
// ============================================================================

func TestEvidenceChain_GenerateReport(t *testing.T) {
	ch := NewEvidenceChain(0)
	// Attach a rule so findings appear in the report.
	_ = ch.Engine().AddRule(&Rule{ID: "crit", Name: "critical-events", Enabled: true, Priority: 1,
		Condition: &FieldCondition{Field: "severity", Op: OpEq, Value: "critical"},
		Action:    RuleAction{Type: ActionAlert, Message: "critical event observed"}})

	now := time.Now().UTC()
	start := now.Add(-24 * time.Hour)
	for i := 0; i < 10; i++ {
		ch.Append(&AuditEvent{
			Timestamp: start.Add(time.Duration(i) * time.Hour),
			Action:    "test_action", UserID: "user1",
			Severity: SeverityCritical, Result: "success", Category: CategorySecurity,
		})
	}

	report, err := ch.GenerateReport(start, now)
	if err != nil {
		t.Fatalf("generate report: %v", err)
	}
	if report.TotalEvents != 10 {
		t.Fatalf("expected 10 events, got %d", report.TotalEvents)
	}
	if !report.ChainVerified {
		t.Error("report should show verified chain")
	}
	if report.Signature == nil {
		t.Fatal("report should be signed")
	}
	if len(report.RuleFindings) != 10 {
		t.Errorf("expected 10 rule findings, got %d", len(report.RuleFindings))
	}
}

func TestVerifyReport(t *testing.T) {
	ch := NewEvidenceChain(0)
	ch.Append(&AuditEvent{Action: "test", UserID: "u1"})

	report, err := ch.GenerateReport(time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("generate report: %v", err)
	}
	if err := VerifyReport(report, ch.PublicKey()); err != nil {
		t.Fatalf("intact report should verify: %v", err)
	}
	report.TotalEvents += 100 // tamper
	if err := VerifyReport(report, ch.PublicKey()); err == nil {
		t.Fatal("tampered report should fail verification")
	}
}

func TestAuditReport_Markdown(t *testing.T) {
	ch := NewEvidenceChain(0)
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		ch.Append(&AuditEvent{
			Timestamp: now.Add(time.Duration(i) * time.Minute),
			Action:    "create_cluster", UserID: "admin", Result: "success",
			Category: CategoryAdmin, Severity: SeverityCritical,
		})
	}
	report, err := ch.GenerateReport(time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("generate report: %v", err)
	}
	md := report.ToMarkdown()
	for _, want := range []string{"# Audit Evidence Report", "Total Events:", "Chain Integrity:", "Severity Breakdown"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing section: %q", want)
		}
	}

	jsonBytes, err := report.ToJSON()
	if err != nil {
		t.Fatalf("to json: %v", err)
	}
	if !strings.Contains(string(jsonBytes), "\"total_events\"") {
		t.Error("json report missing total_events field")
	}
}
