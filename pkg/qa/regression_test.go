package qa

import (
	"strings"
	"testing"
)

// benchdb_test.go exercises the benchmark database with deterministic ordering.

func TestBenchDBRoundTrip(t *testing.T) {
	db, err := NewBenchDB(t.TempDir() + "/bench")
	if err != nil { t.Fatalf("NewBenchDB: %v", err) }
	run1 := BenchRun{Seq: 100, Label: "commit-abc", Samples: []BenchSample{{Name: "Parse", NsPerOp: 1234.0, BytesPerOp: 128, AllocsPerOp: 2}}}
	run2 := BenchRun{Seq: 101, Label: "commit-def", Samples: []BenchSample{{Name: "Parse", NsPerOp: 1300.0}}}
	db.Save(run1); db.Save(run2)
	base, ok := db.Baseline(); if !ok || base.Seq != 100 { t.Fatalf("baseline: got %d want 100", base.Seq) }
	cur, ok := db.Latest(); if !ok || cur.Seq != 101 { t.Fatalf("latest: got %d want 101", cur.Seq) }
}

func TestBenchDBRecentOrdering(t *testing.T) {
	db, err := NewBenchDB(t.TempDir() + "/recent")
	if err != nil { t.Fatal(err) }
	for i := int64(0); i < 5; i++ { db.Save(BenchRun{Seq: i, Label: "seq"}) }
	recent := db.Recent(3)
	if len(recent) != 3 { t.Fatalf("recent len: got %d want 3", len(recent)) } else if recent[0].Seq != 4 || recent[2].Seq != 2 { t.Fatalf("order wrong: %v", recent) }
}

// regression_test.go exercises Regressor with happy/sad/error paths.

func TestRegressPassNoDelta(t *testing.T) {
	base := &BenchRun{Samples: []BenchSample{{Name: "Foo", NsPerOp: 1000.0, BytesPerOp: 100, AllocsPerOp: 10}}}
	cur := &BenchRun{Samples: []BenchSample{{Name: "Foo", NsPerOp: 1000.0, BytesPerOp: 100, AllocsPerOp: 10}}}
	r := Regress(base, cur, RegressConfig{MaxTimePct: 10.0})
	if !r.Pass || len(r.Violations) != 0 { t.Errorf("pass expected: %v", r) }
}

func TestRegressFailBaselineWorse(t *testing.T) {
	base := &BenchRun{Samples: []BenchSample{{Name: "Foo", NsPerOp: 1000.0, BytesPerOp: 100, AllocsPerOp: 10}}}
	cur := &BenchRun{Samples: []BenchSample{{Name: "Foo", NsPerOp: 1200.0}}} // 20% slower
	r := Regress(base, cur, RegressConfig{MaxTimePct: 10.0})
	if r.Pass || len(r.Violations) == 0 { t.Fatalf("expected violation: %v", r) } else if !strings.Contains(r.Violations[0].String(), "time") { t.Logf("violation string: %s", r.Violations[0].String()) }
}
