package intel

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestCHTime_ClampsAndFormats(t *testing.T) {
	// Zero time clamps to epoch (ClickHouse DateTime cannot predate 1970).
	if got := chTime(time.Time{}); got != "1970-01-01 00:00:00" {
		t.Fatalf("zero time should clamp to epoch, got %q", got)
	}
	ts := time.Date(2024, 3, 1, 12, 30, 45, 0, time.UTC)
	if got := chTime(ts); got != "2024-03-01 12:30:45" {
		t.Fatalf("chTime format mismatch: %q", got)
	}
	// Round-trip through parseCHTime.
	if rt := parseCHTime(chTime(ts)); !rt.Equal(ts) {
		t.Fatalf("round-trip mismatch: %v != %v", rt, ts)
	}
	if !parseCHTime("not-a-time").IsZero() {
		t.Fatalf("invalid time should parse to zero")
	}
}

func TestCHArrayParam_EscapesQuotes(t *testing.T) {
	if got := chArrayParam([]string{"a", "b"}); got != "['a','b']" {
		t.Fatalf("array param mismatch: %q", got)
	}
	// Single quotes and backslashes must be escaped so the literal is safe.
	if got := chArrayParam([]string{"o'malley", `back\slash`}); got != `['o\'malley','back\\slash']` {
		t.Fatalf("escaping failed: %q", got)
	}
	if got := chArrayParam(nil); got != "[]" {
		t.Fatalf("empty array should be [], got %q", got)
	}
}

func TestNonNilAndBounded(t *testing.T) {
	if nonNil(nil) == nil {
		t.Fatalf("nonNil(nil) must return a non-nil slice")
	}
	if len(nonNil([]string{"x"})) != 1 {
		t.Fatalf("nonNil must preserve contents")
	}
	if bounded("abcdef", 3) != "abc…" {
		t.Fatalf("bounded truncation failed: %q", bounded("abcdef", 3))
	}
	if bounded("ab", 5) != "ab" {
		t.Fatalf("bounded should not truncate short strings")
	}
}

func TestNewClickHouseStore_RequiresEndpoint(t *testing.T) {
	if _, err := NewClickHouseStore(ClickHouseConfig{}); err == nil {
		t.Fatalf("empty endpoint must error")
	}
}

func TestNewClickHouseStore_UnreachableFallsThrough(t *testing.T) {
	// An unreachable endpoint must return an error (so callers fall back to the
	// simulated MemoryStore) rather than hang or pretend success.
	_, err := NewClickHouseStore(ClickHouseConfig{
		Endpoint: "http://127.0.0.1:1", // nothing listens here
		Timeout:  1 * time.Second,
	})
	if err == nil {
		t.Fatalf("unreachable ClickHouse must return an error")
	}
}

// TestClickHouseStore_Live exercises the real store end-to-end. It is skipped
// unless CLOUDAI_TEST_CH_ENDPOINT is set (e.g. http://localhost:8123), so CI
// without a ClickHouse server stays green while integration runs can opt in.
func TestClickHouseStore_Live(t *testing.T) {
	endpoint := os.Getenv("CLOUDAI_TEST_CH_ENDPOINT")
	if endpoint == "" {
		t.Skip("set CLOUDAI_TEST_CH_ENDPOINT to run the live ClickHouse test")
	}
	store, err := NewClickHouseStore(ClickHouseConfig{
		Endpoint: endpoint,
		Database: "security_test",
		User:     os.Getenv("CLOUDAI_TEST_CH_USER"),
		Password: os.Getenv("CLOUDAI_TEST_CH_PASSWORD"),
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if !store.IsReal() || store.Driver() != "clickhouse" {
		t.Fatalf("live store must report real/clickhouse")
	}

	now := time.Now().UTC().Truncate(time.Second)
	if err := store.UpsertCVE(CVEEntry{CVEID: "CVE-LIVE-1", CVSSv3Score: 9.9, MitreTags: []string{"T1190"}, PublishedAt: now}); err != nil {
		t.Fatalf("upsert cve: %v", err)
	}
	if err := store.UpsertIOCs([]IOCEntry{{IOCType: "ip", Value: "198.51.100.7", Severity: SeverityHigh, FirstSeenAt: now}}); err != nil {
		t.Fatalf("upsert iocs: %v", err)
	}
	cves, err := store.RecentCVEs(now.Add(-time.Hour), 10)
	if err != nil {
		t.Fatalf("recent cves: %v", err)
	}
	if len(cves) == 0 {
		t.Fatalf("expected at least the inserted CVE")
	}
	hits, err := store.LookupIOCs("ip", []string{"198.51.100.7"})
	if err != nil {
		t.Fatalf("lookup iocs: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("expected the inserted IOC")
	}
	_ = context.Background()
}
