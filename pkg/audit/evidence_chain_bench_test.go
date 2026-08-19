package audit

import (
	"testing"
	"time"
)

// ============================================================================
// Benchmarks - Evidence Chain
// ============================================================================

func BenchmarkEvidenceChain_Append(b *testing.B) {
	ch := NewEvidenceChain(0)
	event := &AuditEvent{Action: "op", UserID: "user1", Result: "success"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ch.Append(event)
	}
}

func BenchmarkEvidenceChain_Verify(b *testing.B) {
	ch := NewEvidenceChain(0)
	event := &AuditEvent{Action: "op", UserID: "user1", Result: "success"}

	// Pre-populate 1000 entries
	for i := 0; i < 1000; i++ {
		ch.Append(event)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if idx, err := ch.Verify(); err != nil || idx != -1 {
			b.Fatalf("verification failed at index %d: %v", idx, err)
		}
	}
}

func BenchmarkEvidenceChain_Signing(b *testing.B) {
	ch := NewEvidenceChain(0)
	events := make([]*AuditEvent, 1000)
	for i := range events {
		events[i] = &AuditEvent{
			Action:   "test_action_" + string(rune(i)),
			UserID:   "user_abc123",
			Resource: "workload",
			Result:   "success",
			Category: CategorySecurity,
			Metadata: map[string]string{"region": "us-east-1", "team": "platform"},
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, ev := range events {
			_, _ = ch.Append(ev)
		}
	}
}

func BenchmarkVerifyAuditChain_LargeTrail(b *testing.B) {
	const n = 5000
	ch := NewEvidenceChain(n)
	for i := 0; i < n; i++ {
		ch.Append(&AuditEvent{UserID: "u", Action: "op"})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx, err := ch.Verify()
		if err != nil && idx != -1 {
			b.Fatalf("tamper at %d: %v", idx, err)
		}
	}
}

// ============================================================================
// Benchmarks - Rule Engine
// ============================================================================

func BenchmarkRuleEngine_SimpleCondition(b *testing.B) {
	re := NewRuleEngine()
	_ = re.AddRule(&Rule{
		ID:        "simple", Name: "simple-rule", Enabled: true, Priority: 1,
		Condition: &FieldCondition{Field: "result", Op: OpEq, Value: "success"},
		Action:    RuleAction{Type: ActionAlert},
	})
	event := &AuditEvent{Result: "success"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		re.Evaluate(event)
	}
}

func BenchmarkRuleEngine_ComplexConditions(b *testing.B) {
	re := NewRuleEngine()
	cond := &OrCondition{Conditions: []Condition{
		&FieldCondition{Field: "severity", Op: OpEq, Value: "critical"},
		&FieldCondition{Field: "category", Op: OpEq, Value: "authentication"},
		&FieldCondition{Field: "ip_address", Op: OpRegex, Value: `^10\..*`},
	}}
	_ = re.AddRule(&Rule{
		ID: "complex", Name: "complex-rule", Enabled: true, Priority: 100,
		Condition: cond, Action: RuleAction{Type: ActionDeny},
	})
	event := &AuditEvent{Severity: SeverityCritical, IPAddress: "10.0.0.1"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		re.Evaluate(event)
	}
}

func BenchmarkRuleEngine_MultipleRules(b *testing.B) {
	re := NewRuleEngine()
	actions := []string{"login", "logout", "create_cluster", "delete_user", "backup_data", "restore_config"}
	for _, act := range actions {
		err := re.AddRule(&Rule{
			ID:        act, Name: act, Enabled: true, Priority: 1,
			Condition: &FieldCondition{Field: "action", Op: OpEq, Value: act},
			Action:    RuleAction{Type: ActionTag},
		})
		if err != nil {
			b.Fatalf("add rule: %v", err)
		}
	}
	event := &AuditEvent{Action: "create_cluster"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		re.Evaluate(event)
	}
}

func BenchmarkRuleEngine_NestedConditions(b *testing.B) {
	re := NewRuleEngine()
	_ = re.AddRule(&Rule{
		ID: "nested", Name: "nested", Enabled: true, Priority: 5,
		Condition: &AndCondition{Conditions: []Condition{
			&OrCondition{Conditions: []Condition{
				&NotCondition{Condition: &FieldCondition{Field: "result", Op: OpEq, Value: "failure"}},
				&FieldCondition{Field: "severity", Op: OpGt, Value: "medium"},
			}},
			&FieldCondition{Field: "category", Op: OpEq, Value: "security"},
		}},
		Action: RuleAction{Type: ActionNotify},
	})
	event := &AuditEvent{Category: CategorySecurity, Severity: SeverityWarning, Result: "success"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		re.Evaluate(event)
	}
}

// ============================================================================
// Benchmarks - Report Generation
// ============================================================================

func BenchmarkReportGeneration_LargeTrail(b *testing.B) {
	ch := NewEvidenceChain(10000)
	now := time.Now().UTC()
	start := now.Add(-24 * time.Hour)
	for i := 0; i < 10000; i++ {
		ch.Append(&AuditEvent{
			Timestamp: start.Add(time.Duration(i) * time.Minute),
			Action:    "op", UserID: "user1", Severity: SeverityCritical, Result: "success",
			Category: CategorySecurity, Metadata: map[string]string{"k": "v"},
		})
	}
	_ = ch.Engine().AddRule(&Rule{
		ID: "critical", Name: "critical", Enabled: true, Priority: 1,
		Condition: &FieldCondition{Field: "severity", Op: OpEq, Value: "critical"},
		Action:    RuleAction{Type: ActionAlert},
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		report, err := ch.GenerateReport(start, now)
		if err != nil {
			b.Fatalf("generate report: %v", err)
		}
		_ = report.ToMarkdown()
	}
}

func BenchmarkReportJSONSerialization(b *testing.B) {
	ch := NewEvidenceChain(0)
	ch.Append(&AuditEvent{Action: "test", UserID: "u1", Timestamp: time.Now().UTC()})

	report, err := ch.GenerateReport(time.Time{}, time.Time{})
	if err != nil {
		b.Fatalf("generate report: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = report.ToJSON()
	}
}

func BenchmarkReportMarkdownFormatting(b *testing.B) {
	ch := NewEvidenceChain(0)
	for i := 0; i < 10; i++ {
		ch.Append(&AuditEvent{
			Timestamp: time.Now().UTC(),
			Action:    "test_action", UserID: "admin",
			Severity: SeverityCritical, Result: "success", Category: CategoryAdmin,
		})
	}
	report, _ := ch.GenerateReport(time.Time{}, time.Time{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = report.ToMarkdown()
	}
}
