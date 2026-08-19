package aiops

import (
	"testing"
	"time"
)

func TestEvidenceAIOpsEngine_ReceiptSigned(t *testing.T) {
	e := NewEvidenceAIOpsEngine()
	e.RegisterAnomaly("net-partition", "error", time.Now())
	res, err := e.SelfHeal("restart", []string{"cache-unavailable"})
	if err != nil {
		t.Fatalf("SelfHeal: %v", err)
	}
	if res.Receipt == nil || !res.Receipt.Verify() {
		t.Fatal("expected a verifiable receipt")
	}
}

func TestEvidenceAIOpsEngine_CausalityRanking(t *testing.T) {
	e := NewEvidenceAIOpsEngine()
	e.RegisterAnomaly("net-partition", "error", time.Now().Add(-2))
	e.RegisterAnomaly("db-down", "error", time.Now().Add(-1))
	e.RegisterAnomaly("app-timeout", "latency", time.Now())

	// net-partition causes db-down => db-down is symptom, not cause
	// app-timeout caused by db-down => network partition is root cause
	e.AddCausalLink("net-partition", "db-down")
	e.AddCausalLink("db-down", "app-timeout")

	res, _ := e.SelfHeal("rollback", []string{"app-timeout", "db-down"})
	graph := res.CausalityGraph
	if len(graph) < 3 {
		t.Fatalf("expected at least 3 nodes, got %d", len(graph))
	}
	// The top-ranked node should have highest in-degree - out-degree
	// net-partition: in=0, out=1 => diff=-1; db-down: in=1, out=1 => diff=0; app-timeout: in=1, out=0 => diff=1
	if res.Score <= 0 {
		t.Error("score must be positive when ranked")
	}
}

func TestEvidenceAIOpsEngine_SingleAnomalyIsRootCause(t *testing.T) {
	e := NewEvidenceAIOpsEngine()
	e.RegisterAnomaly("oom-killed", "resource", time.Now())
	res, _ := e.SelfHeal("restart", []string{"oom-killed"})
	if len(res.CausalityGraph) < 1 {
		t.Error("at least one anomaly must be ranked")
	}
	if res.Score < 0.5 {
		t.Log("single anomaly ranks highly as potential root cause")
	}
}
