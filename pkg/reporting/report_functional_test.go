package reporting

import (
	"bytes"
	"strings"
	"testing"
)

func TestReport_Engine_Generate(t *testing.T) {
	recs := []Record{
		{Namespace: "prod", Tenant: "alpha", Resource: "gpu", Cost: 100},
		{Namespace: "prod", Tenant: "beta", Resource: "gpu", Cost: 200},
		{Namespace: "dev", Tenant: "alpha", Resource: "cpu", Cost: 50},
	}

	engine := NewEngine()
	spec := ReportSpec{Title: "test", GroupBy: []string{"tenant", "resource"}}
	report, err := engine.Generate(recs, spec)
	if err != nil {
		t.Fatal(err)
	}
	if report == nil || report.RowCount <= 0 {
		t.Error("expected non-empty report")
	}
	if report.TotalCost != 350 {
		t.Errorf("expected total cost 350, got %v", report.TotalCost)
	}
}

func TestReport_RollUp(t *testing.T) {
	recs := []Record{
		{Namespace: "prod", Tenant: "alpha", Cost: 100},
		{Namespace: "dev", Tenant: "beta", Cost: 200},
	}
	engine := NewEngine()
	dims := []string{"namespace"}
	reports := engine.RollUp(recs, dims)
	// RollUp returns len(dims)+1 reports (grand total + per-dimension)
	expectedLen := len(dims) + 1
	if len(reports) != expectedLen {
		t.Errorf("expected %d reports for depth=0..len(dims), got %d", expectedLen, len(reports))
	}
}

func TestReport_Serialization(t *testing.T) {
	report := &Report{
		Title:      "test",
		Dimensions: []string{"tenant", "resource"},
		Rows:       []AggRow{{Keys: map[string]string{"tenant": "a", "resource": "gpu"}, Count: 1, Quantity: 10, Cost: 100}},
	}
	var buf bytes.Buffer
	if err := WriteJSON(&buf, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "test") {
		t.Error("JSON should contain title")
	}

	buf.Reset()
	if err := WriteCSV(&buf, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "tenant") && !strings.Contains(buf.String(), "resource") {
		t.Error("CSV should contain dimension headers (tenant/resource)")
	}
	if !strings.Contains(buf.String(), "count") {
		t.Error("CSV should contain metric columns")
	}
}
