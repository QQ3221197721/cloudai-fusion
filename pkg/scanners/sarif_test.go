package scanners_test

import (
	"encoding/json"
	"testing"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/scanners"
)

func TestParseSARIF(t *testing.T) {
	sarifJSON := `{
		"$schema": "http://json.schemastore.org/sarif-2.1.0",
		"version": "2.1.0",
		"runs": [{
			"tool": {
				"driver": {
					"name": "test-tool",
					"version": "1.0.0",
					"rules": []
				}
			},
			"results": [{
				"ruleId": "SEC-001",
				"level": "error",
				"message": {
					"text": "Test security finding"
				},
				"locations": [{
					"physicalLocation": {
						"artifactLocation": {"uri": "main.go"}
					}
				}]
			}]
		}]
	}`

	report, err := scanners.ParseSARIF([]byte(sarifJSON))
	if err != nil {
		t.Fatalf("Failed to parse SARIF: %v", err)
	}

	if report == nil {
		t.Fatal("Expected non-nil SARIF report")
	}
	if len(report.Runs) != 1 {
		t.Errorf("Expected 1 run, got %d", len(report.Runs))
	}
	if len(report.Runs[0].Results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(report.Runs[0].Results))
	}
}

func TestAggregateResults(t *testing.T) {
	j1 := `{"$schema":"http://json.schemastore.org/sarif-2.1.0","version":"2.1.0","runs":[{"tool":{"driver":{"name":"tool1","version":"v1"}},"results":[{"ruleId":"R1","level":"error","message":{"text":"msg"}}]}]}`
	j2 := `{"$schema":"http://json.schemastore.org/sarif-2.1.0","version":"2.1.0","runs":[{"tool":{"driver":{"name":"tool2","version":"v2","rules":[{"id":"R2","name":"RuleTwo"}]}},"results":[{"ruleId":"R2","level":"warning","message":{"text":"msg2"}}]}]}`

	r1, err1 := scanners.ParseSARIF([]byte(j1))
	r2, err2 := scanners.ParseSARIF([]byte(j2))
	if err1 != nil || err2 != nil {
		t.Fatalf("Failed to parse test reports: %v, %v", err1, err2)
	}

	agg := scanners.AggregateResults([]*scanners.SARIFReport{r1, r2})

	if agg.TotalFindings != 2 {
		t.Errorf("Expected total findings=2, got %d", agg.TotalFindings)
	}
	if len(agg.BySeverity) == 0 {
		t.Error("Expected BySeverity map not empty")
	}
	if len(agg.ByRule) == 0 {
		t.Error("Expected ByRule map not empty")
	}
}

func TestSARIFRuleStruct(t *testing.T) {
	rule := scanners.SARIFRule{ID: "CVE-2024-1234", Name: "CVE Check", Severity: "error"}
	
	b, err := json.Marshal(rule)
	if err != nil {
		t.Fatalf("Failed to marshal rule: %v", err)
	}

	var unmarshaled scanners.SARIFRule
	err = json.Unmarshal(b, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal rule: %v", err)
	}

	if unmarshaled.ID != rule.ID {
		t.Errorf("Expected ID %s, got %s", rule.ID, unmarshaled.ID)
	}
}

func TestArtifactContent(t *testing.T) {
	content := scanners.ArtifactContent{
		Text: "some text content",
		Bytes: "bW9yZSBjb250ZW50",
	}
	
	b, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("Failed to marshal content: %v", err)
	}

	var decoded scanners.ArtifactContent
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal content: %v", err)
	}

	if decoded.Text != content.Text {
		t.Errorf("Expected Text %q, got %q", content.Text, decoded.Text)
	}
}
