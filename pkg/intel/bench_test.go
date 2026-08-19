package intel

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
)

// bench_test.go measures the REAL L1 threat-intelligence ingestion pipeline:
//
//   - STIX 2.1 bundle ingestion throughput (indicators/sec), parse-only and
//     full parse+store paths;
//   - deduplication rate keyed by (ioc_type,value), against a no-dedup naive
//     baseline, to prove the value of keyed upserts;
//   - TTL expiry eviction correctness on the in-memory (simulated) backend.
//
// Honesty: the MemoryStore backing these benchmarks is the SIMULATED backend and
// is reported as such via pkg/capability (see TestBench_CapabilityHonesty). No
// external/commercial platform numbers are fabricated; the only comparison is
// against our own naive baseline built in this file.

// buildSTIXBundle synthesizes a STIX 2.1 bundle with `unique` distinct ipv4
// indicators, each repeated `dupFactor` times (distinct STIX object ids, same
// observable value). It returns the bundle bytes and the raw indicator count.
// This models real feeds where the same IOC arrives from several sources.
func buildSTIXBundle(unique, dupFactor int) ([]byte, int) {
	if dupFactor < 1 {
		dupFactor = 1
	}
	var b strings.Builder
	b.WriteString(`{"type":"bundle","id":"bundle--bench","objects":[`)
	first := true
	raw := 0
	oid := 0
	for u := 0; u < unique; u++ {
		// Spread values across a /8 so each is a distinct observable.
		ip := fmt.Sprintf("10.%d.%d.%d", (u>>16)&0xff, (u>>8)&0xff, u&0xff)
		for d := 0; d < dupFactor; d++ {
			if !first {
				b.WriteByte(',')
			}
			first = false
			oid++
			fmt.Fprintf(&b,
				`{"type":"indicator","spec_version":"2.1","id":"indicator--%d","pattern_type":"stix","pattern":"[ipv4-addr:value = '%s']","valid_from":"2026-01-01T00:00:00Z","confidence":75}`,
				oid, ip)
			raw++
		}
	}
	b.WriteString(`]}`)
	return []byte(b.String()), raw
}

// BenchmarkParseSTIXBundle measures pure STIX 2.1 parse throughput (no storage).
func BenchmarkParseSTIXBundle(b *testing.B) {
	const unique, dup = 2000, 3
	bundle, raw := buildSTIXBundle(unique, dup)
	b.ReportAllocs()
	b.SetBytes(int64(len(bundle)))
	b.ResetTimer()
	total := 0
	for i := 0; i < b.N; i++ {
		imp, err := ParseSTIXBundle(bundle)
		if err != nil {
			b.Fatalf("parse: %v", err)
		}
		total += len(imp.IOCs)
	}
	b.StopTimer()
	// Every raw indicator yields one IOC at parse time (dedup happens at store).
	b.ReportMetric(float64(raw)*float64(b.N)/b.Elapsed().Seconds(), "indicators/s")
	if total == 0 {
		b.Fatal("no IOCs parsed")
	}
}

// BenchmarkImportSTIXBundle_Dedup measures full parse+store ingestion throughput
// via the Hub, with (ioc_type,value) keyed dedup. Re-importing the same bundle
// is idempotent, so the store stays bounded at `unique` entries — this is the
// steady-state hot path when overlapping feeds re-deliver known IOCs.
func BenchmarkImportSTIXBundle_Dedup(b *testing.B) {
	const unique, dup = 2000, 3
	bundle, raw := buildSTIXBundle(unique, dup)
	store := NewMemoryStore()
	hub := NewHub(nil, store, nil)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := hub.ImportSTIXBundle(ctx, bundle); err != nil {
			b.Fatalf("import: %v", err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(raw)*float64(b.N)/b.Elapsed().Seconds(), "indicators/s")
	// Dedup invariant: regardless of b.N, the store holds exactly `unique` IOCs.
	if got := store.IOCCount(); got != unique {
		b.Fatalf("dedup failed: stored %d IOCs, want %d unique", got, unique)
	}
}

// naiveStore is the NO-DEDUP baseline: it appends every ingested IOC to a slice,
// exactly as a naive pipeline that skips keyed upserts would. Lookup must scan.
// It exists only to quantify the value of our keyed dedup — it is not a Store.
type naiveStore struct {
	iocs []IOCEntry
}

func (n *naiveStore) upsert(iocs []IOCEntry) { n.iocs = append(n.iocs, iocs...) }

func (n *naiveStore) lookup(iocType, value string) (IOCEntry, bool) {
	for i := range n.iocs {
		if n.iocs[i].IOCType == iocType && n.iocs[i].Value == value {
			return n.iocs[i], true
		}
	}
	return IOCEntry{}, false
}

// BenchmarkImportSTIXBundle_NaiveBaseline is the same ingestion WITHOUT dedup:
// parse the bundle and append all indicators to a growing slice. Compare its
// indicators/sec and unbounded growth against the dedup benchmark above.
func BenchmarkImportSTIXBundle_NaiveBaseline(b *testing.B) {
	const unique, dup = 2000, 3
	bundle, raw := buildSTIXBundle(unique, dup)
	ns := &naiveStore{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		imp, err := ParseSTIXBundle(bundle)
		if err != nil {
			b.Fatalf("parse: %v", err)
		}
		ns.upsert(imp.IOCs)
	}
	b.StopTimer()
	b.ReportMetric(float64(raw)*float64(b.N)/b.Elapsed().Seconds(), "indicators/s")
	// No dedup: the baseline store grows with every raw indicator ingested.
	if got := len(ns.iocs); got != raw*b.N {
		b.Fatalf("naive baseline should retain all %d raw IOCs, got %d", raw*b.N, got)
	}
}

// BenchmarkLookup_DedupMap_vs_NaiveScan contrasts query cost: our keyed store is
// O(1) map lookup; the no-dedup baseline is an O(n) linear scan. Both are seeded
// from the same bundle so the comparison is apples-to-apples.
func BenchmarkLookup_DedupMap_vs_NaiveScan(b *testing.B) {
	const unique, dup = 2000, 3
	bundle, _ := buildSTIXBundle(unique, dup)
	imp, err := ParseSTIXBundle(bundle)
	if err != nil {
		b.Fatalf("parse: %v", err)
	}
	// Target the LAST unique value — worst case for a linear scan.
	target := fmt.Sprintf("10.%d.%d.%d", ((unique-1)>>16)&0xff, ((unique-1)>>8)&0xff, (unique-1)&0xff)

	ms := NewMemoryStore()
	_ = ms.UpsertIOCs(imp.IOCs)
	ns := &naiveStore{}
	ns.upsert(imp.IOCs)

	b.Run("dedup_map_O1", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			hits, _ := ms.LookupIOCs("ip", []string{target})
			if len(hits) != 1 {
				b.Fatalf("map lookup miss: %d", len(hits))
			}
		}
	})
	b.Run("naive_scan_On", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, ok := ns.lookup("ip", target); !ok {
				b.Fatal("scan lookup miss")
			}
		}
	})
}

// TestBench_DedupRate records the REAL dedup rate: raw indicators parsed vs
// unique IOCs stored, for a realistic overlap factor. It asserts the invariant
// and logs the numbers used in the performance report.
func TestBench_DedupRate(t *testing.T) {
	cases := []struct{ unique, dup int }{
		{1000, 2},
		{1000, 3},
		{1000, 5},
	}
	for _, c := range cases {
		bundle, raw := buildSTIXBundle(c.unique, c.dup)
		store := NewMemoryStore()
		hub := NewHub(nil, store, nil)
		res, err := hub.ImportSTIXBundle(context.Background(), bundle)
		if err != nil {
			t.Fatalf("import: %v", err)
		}
		if res.IOCAdded != raw {
			t.Fatalf("IOCAdded=%d, want raw=%d (parser must surface every indicator)", res.IOCAdded, raw)
		}
		stored := store.IOCCount()
		if stored != c.unique {
			t.Fatalf("stored=%d, want unique=%d (keyed dedup broken)", stored, c.unique)
		}
		dedupRate := 1.0 - float64(stored)/float64(raw)
		t.Logf("dupFactor=%d raw=%d stored_unique=%d dedup_rate=%.1f%%",
			c.dup, raw, stored, dedupRate*100)
	}
}

// TestTTLEvict_Correctness proves EvictExpired removes only IOCs older than the
// TTL, keeps fresh ones, never evicts timestamp-less IOCs, and is a no-op for a
// non-positive TTL.
func TestTTLEvict_Correctness(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	s := NewMemoryStore()
	_ = s.UpsertIOCs([]IOCEntry{
		{IOCType: "ip", Value: "1.1.1.1", Severity: SeverityHigh, FirstSeenAt: now.Add(-48 * time.Hour)},  // stale
		{IOCType: "ip", Value: "2.2.2.2", Severity: SeverityHigh, FirstSeenAt: now.Add(-1 * time.Hour)},   // fresh
		{IOCType: "domain", Value: "old.example", Severity: SeverityLow, FirstSeenAt: now.Add(-72 * time.Hour)}, // stale
		{IOCType: "domain", Value: "notime.example", Severity: SeverityLow},                               // zero ts: keep
		// Re-observed indicator: first seen long ago but LastSeenAt is recent → fresh.
		{IOCType: "ip", Value: "3.3.3.3", Severity: SeverityMedium, FirstSeenAt: now.Add(-90 * time.Hour), LastSeenAt: now.Add(-30 * time.Minute)},
	})
	if s.IOCCount() != 5 {
		t.Fatalf("setup: IOCCount=%d, want 5", s.IOCCount())
	}

	// Non-positive TTL must be a no-op.
	if n := s.EvictExpired(now, 0); n != 0 || s.IOCCount() != 5 {
		t.Fatalf("ttl<=0 must not evict: evicted=%d count=%d", n, s.IOCCount())
	}

	// 24h TTL: evicts 1.1.1.1 (48h) and old.example (72h); keeps fresh, re-seen, and zero-ts.
	evicted := s.EvictExpired(now, 24*time.Hour)
	if evicted != 2 {
		t.Fatalf("evicted=%d, want 2", evicted)
	}
	if s.IOCCount() != 3 {
		t.Fatalf("post-evict IOCCount=%d, want 3", s.IOCCount())
	}
	if hits, _ := s.LookupIOCs("ip", []string{"1.1.1.1"}); len(hits) != 0 {
		t.Fatalf("stale IOC 1.1.1.1 should have been evicted")
	}
	if hits, _ := s.LookupIOCs("ip", []string{"2.2.2.2"}); len(hits) != 1 {
		t.Fatalf("fresh IOC 2.2.2.2 must survive")
	}
	if hits, _ := s.LookupIOCs("ip", []string{"3.3.3.3"}); len(hits) != 1 {
		t.Fatalf("re-observed IOC 3.3.3.3 (recent LastSeenAt) must survive")
	}
	if hits, _ := s.LookupIOCs("domain", []string{"notime.example"}); len(hits) != 1 {
		t.Fatalf("timestamp-less IOC must never be evicted (unknown age)")
	}
}

// BenchmarkEvictExpired measures TTL sweep throughput over a populated store.
// Half the IOCs are stale so the sweep does real deletion work each iteration;
// the store is refilled per iteration so every run evicts the same amount.
func BenchmarkEvictExpired(b *testing.B) {
	const n = 5000
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	seed := make([]IOCEntry, 0, n)
	for i := 0; i < n; i++ {
		age := time.Duration(1) * time.Hour
		if i%2 == 0 {
			age = 48 * time.Hour // half are stale under a 24h TTL
		}
		seed = append(seed, IOCEntry{
			IOCType: "ip", Value: fmt.Sprintf("10.0.%d.%d", (i>>8)&0xff, i&0xff),
			Severity: SeverityHigh, FirstSeenAt: now.Add(-age),
		})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		s := NewMemoryStore()
		_ = s.UpsertIOCs(seed)
		b.StartTimer()
		if ev := s.EvictExpired(now, 24*time.Hour); ev != n/2 {
			b.Fatalf("evicted=%d, want %d", ev, n/2)
		}
	}
}

// TestBench_CapabilityHonesty asserts the in-memory backend that powers these
// benchmarks is reported to pkg/capability as SIMULATED (driver=memory) — the
// honest run-mode signal, never masquerading as a real backend.
func TestBench_CapabilityHonesty(t *testing.T) {
	t.Cleanup(capability.Reset)
	capability.Reset()
	store := NewMemoryStore()
	_ = NewHub(nil, store, nil) // NewHub reports the store to the capability registry

	if store.IsReal() {
		t.Fatalf("MemoryStore.IsReal() must be false")
	}
	var found bool
	for _, bk := range capability.Snapshot() {
		if bk.Component == "intel.store" {
			found = true
			if bk.Mode != capability.ModeSimulated {
				t.Fatalf("intel.store mode=%q, want simulated", bk.Mode)
			}
			if bk.Driver != "memory" {
				t.Fatalf("intel.store driver=%q, want memory", bk.Driver)
			}
		}
	}
	if !found {
		t.Fatal("intel.store not registered in capability snapshot")
	}
}
