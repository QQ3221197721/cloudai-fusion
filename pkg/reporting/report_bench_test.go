package reporting

import (
	"bytes"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Report generation latency at graduated scale (100 / 1000 / 10000 rows)
// ---------------------------------------------------------------------------

func generateRecords(count int) []Record {
	recs := make([]Record, 0, count)
	ns := []string{"prod", "dev", "staging", "sandbox", "ml-training", "inference"}
	tens := []string{"tenant-alpha", "tenant-beta", "tenant-gamma"}
	res := []string{"gpu", "storage", "compute", "bandwidth"}
	reg := []string{"us-west-2", "eu-central-1", "ap-northeast-1"}
	for i := 0; i < count; i++ {
		recs = append(recs, Record{
			Timestamp: time.Now(),
			Namespace: ns[i%len(ns)],
			Tenant:    tens[i%len(tens)],
			Resource:  res[i%len(res)],
			Region:    reg[i%len(reg)],
			Quantity:  int64(i%100 + 1),
			Cost:      float64(i%500 + 1),
		})
	}
	return recs
}

func BenchmarkGenerate_100Rows(b *testing.B) {
	recs := generateRecords(100)
	engine := NewEngine()

	spec := ReportSpec{
		Title: "dashboard",
		GroupBy: []string{"tenant", "resource"},
		SortByCostDesc: true,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rep, err := engine.Generate(recs, spec)
		if err != nil {
			b.Fatal(err)
		}
		if rep == nil || rep.RowCount == 0 {
			b.Fatal("expected non-empty report")
		}
	}
}

func BenchmarkGenerate_1kRows(b *testing.B) {
	recs := generateRecords(1000)
	engine := NewEngine()

	spec := ReportSpec{
		Title:       "billing-detail",
		GroupBy:     []string{"namespace", "tenant", "resource"},
		SortByCostDesc: true,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rep, err := engine.Generate(recs, spec)
		if err != nil {
			b.Fatal(err)
		}
		if rep == nil {
			b.Fatal("report is nil")
		}
	}
}

func BenchmarkGenerate_10kRows(b *testing.B) {
	recs := generateRecords(10000)
	engine := NewEngine()

	spec := ReportSpec{
		Title:       "rollup-monthly",
		GroupBy:     []string{"region", "tenant", "resource"},
		SortByCostDesc: false,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rep, err := engine.Generate(recs, spec)
		if err != nil {
			b.Fatal(err)
		}
		if rep == nil {
			b.Fatal("report is nil")
		}
	}
}

// ---------------------------------------------------------------------------
// Group-by aggregation throughput (events/sec via Snapshot from StreamAggregator)
// ---------------------------------------------------------------------------

func BenchmarkAggregate_Snapshot_1kEvents(b *testing.B) {
	agg := NewStreamAggregator([]string{"tenant", "resource"})
	recs := generateRecords(1000)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var totalQty int64
		for pb.Next() {
			for i := range recs {
				agg.Add(&recs[i])
			}
			snap := agg.Snapshot("throughput-test")
			totalQty += snap.TotalQty
		}
		_ = totalQty // prevent dead store elimination
	})
}

func BenchmarkAggregate_Snapshot_10kEvents(b *testing.B) {
	agg := NewStreamAggregator([]string{"region", "tenant", "resource"})
	recs := generateRecords(10000)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var totalCnt int64
		for pb.Next() {
			for i := range recs {
				agg.Add(&recs[i])
			}
			snap := agg.Snapshot("throughput-test")
			totalCnt += int64(snap.RowCount)
		}
		_ = totalCnt
	})
}

// ---------------------------------------------------------------------------
// Serialization/export throughput (JSON / CSV bytes/sec)
// ---------------------------------------------------------------------------

func benchmarkSerialization(b *testing.B, recsCount int, compact bool) {
	recs := generateRecords(recsCount)
	engine := NewEngine()
	spec := ReportSpec{
		Title:   "export-test",
		GroupBy: []string{"tenant", "resource"},
	}
	report, err := engine.Generate(recs, spec)
	if err != nil {
		b.Fatalf("failed to generate baseline report: %v", err)
	}

	w := &bytes.Buffer{}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Reset()
		var err error
		if compact {
			err = WriteJSONCompact(w, report)
		} else {
			err = WriteJSON(w, report)
		}
		if err != nil {
			b.Fatal(err)
		}
		n := w.Len()
		if n <= 0 {
			b.Fatalf("expected positive bytes written")
		}
	}
}

func BenchmarkExport_JSON_100Rows(b *testing.B) { benchmarkSerialization(b, 100, false) }
func BenchmarkExport_JSON_Compact_100Rows(b *testing.B) { benchmarkSerialization(b, 100, true) }
func BenchmarkExport_CSV_100Rows(b *testing.B) { exportCSV(b, 100) }

func BenchmarkExport_JSON_1kRows(b *testing.B)  { benchmarkSerialization(b, 1000, false) }
func BenchmarkExport_JSON_Compact_1kRows(b *testing.B) { benchmarkSerialization(b, 1000, true) }
func BenchmarkExport_CSV_1kRows(b *testing.B)   { exportCSV(b, 1000) }

func exportCSV(b *testing.B, recsCount int) {
	recs := generateRecords(recsCount)
	engine := NewEngine()
	spec := ReportSpec{Title: "csv-export", GroupBy: []string{"tenant"}}
	report, err := engine.Generate(recs, spec)
	if err != nil {
		b.Fatal(err)
	}
	w := &bytes.Buffer{}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Reset()
		if err := WriteCSV(w, report); err != nil {
			b.Fatal(err)
		}
		n := w.Len()
		if n <= 0 {
			b.Fatal("expected positive bytes written")
		}
	}
}

// ---------------------------------------------------------------------------
// Concurrent report generation
// ---------------------------------------------------------------------------

func BenchmarkConcurrent_ReportGeneration_1kRows(b *testing.B) {
	recs := generateRecords(1000)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var cnt int
		engine := NewEngine()
		spec := ReportSpec{
			Title:   "concurrent",
			GroupBy: []string{"tenant", "resource"},
		}
		for pb.Next() {
			rep, err := engine.Generate(recs, spec)
			if err != nil {
				b.Fatal(err)
			}
			if rep == nil {
				b.Fatal("nil report")
			}
			cnt++
		}
		_ = cnt
	})
}
