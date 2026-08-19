package tsdb

import "testing"

// TestRecordWriteQuery_ProducesVerifiableReceipts proves each TS write and query
// is sealed into a signed, offline-verifiable receipt.
func TestRecordWriteQuery_ProducesVerifiableReceipts(t *testing.T) {
	engine := NewEvidenceTSDBEngine()

	w, err := engine.RecordWrite("cpu.usage", 1000, 42.0)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if w.Receipt == nil || !w.Receipt.Verify() {
		t.Fatal("write must carry a verifiable receipt")
	}

	q, err := engine.RecordQuery("cpu.usage", 1000, 2000)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if q.Receipt == nil || !q.Receipt.Verify() {
		t.Fatal("query must carry a verifiable receipt")
	}
}

// TestRecordQuery_RejectsBadRange verifies range validation.
func TestRecordQuery_RejectsBadRange(t *testing.T) {
	engine := NewEvidenceTSDBEngine()
	if _, err := engine.RecordQuery("s", 2000, 1000); err == nil {
		t.Fatal("expected error for end-before-start range")
	}
	if _, err := engine.RecordQuery("", 0, 1); err == nil {
		t.Fatal("expected error for empty series")
	}
}

// TestCompactionScoring aligns the hot tier with real query traffic: keeping the
// most-queried series hot scores high; keeping cold series hot scores low; and
// RecommendHotSeries returns the traffic-optimal set.
func TestCompactionScoring(t *testing.T) {
	engine := NewEvidenceTSDBEngine()

	// Workload: cpu.usage is hammered, disk.io occasionally, temp.sensor rarely.
	for i := 0; i < 70; i++ {
		mustQuery(t, engine, "cpu.usage")
	}
	for i := 0; i < 25; i++ {
		mustQuery(t, engine, "disk.io")
	}
	for i := 0; i < 5; i++ {
		mustQuery(t, engine, "temp.sensor")
	}

	good := engine.ScoreCompaction([]string{"cpu.usage", "disk.io"})
	if good.AlignmentScore < 0.9 {
		t.Fatalf("hot={cpu,disk} should capture ~95%% of traffic, got %.2f", good.AlignmentScore)
	}
	bad := engine.ScoreCompaction([]string{"temp.sensor"})
	if bad.AlignmentScore > 0.1 {
		t.Fatalf("hot={temp} should capture ~5%% of traffic, got %.2f", bad.AlignmentScore)
	}
	if good.AlignmentScore <= bad.AlignmentScore {
		t.Fatal("aligned strategy must outscore misaligned strategy")
	}

	rec := engine.RecommendHotSeries(1)
	if len(rec) != 1 || rec[0] != "cpu.usage" {
		t.Fatalf("expected cpu.usage as top hot series, got %v", rec)
	}
}

func mustQuery(t *testing.T, e *EvidenceTSDBEngine, series string) {
	t.Helper()
	if _, err := e.RecordQuery(series, 0, 100); err != nil {
		t.Fatalf("query %s: %v", series, err)
	}
}
