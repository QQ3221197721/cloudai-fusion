package intel

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMemoryStore_UpsertAndQueryCVE(t *testing.T) {
	s := NewMemoryStore()
	if s.IsReal() {
		t.Fatalf("MemoryStore must report IsReal()=false (simulated)")
	}
	if s.Driver() != "memory" {
		t.Fatalf("Driver()=%q, want memory", s.Driver())
	}

	now := time.Now().UTC()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	must(s.UpsertCVE(CVEEntry{CVEID: "CVE-2024-0001", CVSSv3Score: 9.8, PublishedAt: now}))
	must(s.UpsertCVE(CVEEntry{CVEID: "CVE-2024-0002", CVSSv3Score: 5.0, PublishedAt: now.Add(-48 * time.Hour)}))
	// Upsert same ID must not duplicate.
	must(s.UpsertCVE(CVEEntry{CVEID: "CVE-2024-0001", CVSSv3Score: 9.9, PublishedAt: now}))

	if s.CVECount() != 2 {
		t.Fatalf("CVECount()=%d, want 2", s.CVECount())
	}

	recent, err := s.RecentCVEs(now.Add(-24*time.Hour), 10)
	if err != nil {
		t.Fatalf("RecentCVEs: %v", err)
	}
	if len(recent) != 1 || recent[0].CVEID != "CVE-2024-0001" {
		t.Fatalf("RecentCVEs returned %+v, want only CVE-2024-0001", recent)
	}
	if recent[0].CVSSv3Score != 9.9 {
		t.Fatalf("upsert did not overwrite score: got %v", recent[0].CVSSv3Score)
	}
	if !recent[0].IsCritical() {
		t.Fatalf("CVE with score 9.9 must be critical")
	}
}

func TestMemoryStore_LimitOrdering(t *testing.T) {
	s := NewMemoryStore()
	base := time.Now().UTC()
	for i := 0; i < 5; i++ {
		_ = s.UpsertCVE(CVEEntry{
			CVEID:       "CVE-X-" + string(rune('A'+i)),
			PublishedAt: base.Add(time.Duration(i) * time.Hour),
		})
	}
	got, err := s.RecentCVEs(base.Add(-time.Hour), 3)
	if err != nil {
		t.Fatalf("RecentCVEs: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("limit not applied: got %d, want 3", len(got))
	}
	// Newest first: index 4 (latest) must lead.
	if !got[0].PublishedAt.After(got[1].PublishedAt) {
		t.Fatalf("results not sorted newest-first: %v", got)
	}
}

func TestMemoryStore_LookupIOCs(t *testing.T) {
	s := NewMemoryStore()
	_ = s.UpsertIOCs([]IOCEntry{
		{IOCType: "ip", Value: "10.0.0.1", Severity: SeverityHigh},
		{IOCType: "domain", Value: "evil.example", Severity: SeverityCritical},
	})
	if s.IOCCount() != 2 {
		t.Fatalf("IOCCount()=%d, want 2", s.IOCCount())
	}
	hits, err := s.LookupIOCs("ip", []string{"10.0.0.1", "10.0.0.2"})
	if err != nil {
		t.Fatalf("LookupIOCs: %v", err)
	}
	if len(hits) != 1 || hits[0].Value != "10.0.0.1" {
		t.Fatalf("LookupIOCs returned %+v, want single 10.0.0.1", hits)
	}
}

func TestMemoryStore_KnowledgeGraph(t *testing.T) {
	s := NewMemoryStore()
	_ = s.PutKnowledgeGraph(KnowledgeGraph{
		Tactics:    []Tactic{{TacticID: "TA0001", Name: "Initial Access"}},
		Techniques: []Technique{{TechniqueID: "T1190", Name: "Exploit Public-Facing Application", TacticIDs: []string{"TA0001"}}},
	})
	tech, ok := s.TechniqueByID("T1190")
	if !ok || tech.Name != "Exploit Public-Facing Application" {
		t.Fatalf("TechniqueByID(T1190) failed: %+v ok=%v", tech, ok)
	}
	if _, ok := s.TechniqueByID("T9999"); ok {
		t.Fatalf("TechniqueByID must return ok=false for unknown id")
	}
}

func TestParseCVEJSONL(t *testing.T) {
	data := []byte(`{"cve_id":"CVE-2024-1234","description":"Test","cvss_v3_score":9.8,"mitre_tags":["T1190"],"published_at":"2024-01-01T00:00:00Z"}
` + "\n" + // blank line should be skipped
		`not-json-should-skip` + "\n" +
		`{"cve_id":"CVE-2024-5678","cvss_v3_score":4.2,"published_at":"2024-02-01T00:00:00Z"}`)

	cves := ParseCVEJSONL(data)
	if len(cves) != 2 {
		t.Fatalf("ParseCVEJSONL parsed %d, want 2 (blank + malformed skipped)", len(cves))
	}
	if cves[0].CVEID != "CVE-2024-1234" || cves[0].CVSSv3Score != 9.8 {
		t.Fatalf("first CVE mismatch: %+v", cves[0])
	}
	if len(cves[0].MitreTags) != 1 || cves[0].MitreTags[0] != "T1190" {
		t.Fatalf("mitre tags mismatch: %+v", cves[0].MitreTags)
	}
}

func TestParseIOCFeed(t *testing.T) {
	data := []byte("# comment line\n" +
		"ip\t192.168.1.100\thigh\t2024-01-01T00:00:00Z\tfeed-a\n" +
		"domain\tbad.example\tcritical\n" + // no timestamp/source — still valid
		"malformed-single-field\n")

	iocs := ParseIOCFeed(data)
	if len(iocs) != 2 {
		t.Fatalf("ParseIOCFeed parsed %d, want 2", len(iocs))
	}
	if iocs[0].IOCType != "ip" || iocs[0].Value != "192.168.1.100" || iocs[0].Severity != SeverityHigh {
		t.Fatalf("first IOC mismatch: %+v", iocs[0])
	}
	if iocs[0].FirstSeenAt.IsZero() {
		t.Fatalf("first IOC should have parsed a timestamp")
	}
	if len(iocs[0].Sources) != 1 || iocs[0].Sources[0] != "feed-a" {
		t.Fatalf("first IOC sources mismatch: %+v", iocs[0].Sources)
	}
}

func TestSyncResult(t *testing.T) {
	r := &SyncResult{}
	if r.HasErrors() {
		t.Fatalf("new SyncResult must have no errors")
	}
	r.AddCVE()
	r.AddCVE()
	r.AddIOC()
	r.RecordError("boom")
	if r.CVEAdded != 2 || r.IOCAdded != 1 {
		t.Fatalf("counters wrong: %+v", r)
	}
	if !r.HasErrors() || len(r.Errors) != 1 {
		t.Fatalf("error recording failed: %+v", r)
	}
}

func TestHub_SyncAll_OfflineFeeds(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "nvd.jsonl"),
		`{"cve_id":"CVE-2024-9999","cvss_v3_score":9.9,"published_at":"2024-03-01T00:00:00Z"}`)
	writeFile(t, filepath.Join(dir, "ioc-feed.tsv"),
		"ip\t203.0.113.7\tcritical\t2024-03-01T00:00:00Z")

	hub := NewHub([]FeedSource{{Name: "local-nvd", LocalPath: dir}}, nil, nil)

	res, err := hub.SyncAll(context.Background())
	if err != nil {
		t.Fatalf("SyncAll: %v", err)
	}
	if res.CVEAdded != 1 || res.IOCAdded != 1 {
		t.Fatalf("sync counts wrong: %+v", res)
	}

	recent, err := hub.RecentCVEs(context.Background(), time.Time{}, 10)
	if err != nil {
		t.Fatalf("RecentCVEs: %v", err)
	}
	if len(recent) != 1 || recent[0].CVEID != "CVE-2024-9999" {
		t.Fatalf("expected the synced CVE, got %+v", recent)
	}

	hits, err := hub.MatchIOCs(context.Background(), "ip", []string{"203.0.113.7"})
	if err != nil || len(hits) != 1 {
		t.Fatalf("MatchIOCs failed: hits=%+v err=%v", hits, err)
	}
}

func TestHub_SyncAll_MissingLocalPath(t *testing.T) {
	hub := NewHub([]FeedSource{{Name: "broken"}}, nil, nil)
	res, err := hub.SyncAll(context.Background())
	if err == nil {
		t.Fatalf("expected an error when local_path is empty")
	}
	if !res.HasErrors() {
		t.Fatalf("result should record the source error")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
