package intel

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// concurrency_test.go stress-tests MemoryStore under concurrent readers and
// writers.
//
// NOTE ON -race: the Go race detector requires cgo, which is unavailable on this
// build host (no gcc in PATH), so `go test -race` cannot run here. This test is
// the honest substitute: it does NOT prove the absence of data races, but the Go
// runtime's built-in map-access checker still panics with "concurrent map writes"
// / "concurrent map read and map write" if the mutex discipline in store.go is
// broken, so unsynchronized access fails loudly rather than silently.

// TestMemoryStore_ConcurrentReadWrite hammers every MemoryStore entry point from
// many goroutines at once.
func TestMemoryStore_ConcurrentReadWrite(t *testing.T) {
	s := NewMemoryStore()
	const (
		writers   = 8
		readers   = 8
		perWriter = 200
	)

	// Seed a knowledge graph so TechniqueByID has something to find.
	if err := s.PutKnowledgeGraph(KnowledgeGraph{
		Techniques: []Technique{{TechniqueID: "T1059", Name: "Command and Scripting Interpreter"}},
	}); err != nil {
		t.Fatalf("seed knowledge graph: %v", err)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			for i := 0; i < perWriter; i++ {
				cve := CVEEntry{
					CVEID:       fmt.Sprintf("CVE-2026-%d%04d", id, i),
					CVSSv3Score: 7.5,
					PublishedAt: time.Now().UTC(),
				}
				if err := s.UpsertCVE(cve); err != nil {
					t.Errorf("UpsertCVE: %v", err)
					return
				}
				iocs := []IOCEntry{
					{IOCType: "ip", Value: fmt.Sprintf("203.0.113.%d", id), Severity: SeverityHigh},
					{IOCType: "domain", Value: fmt.Sprintf("evil-%d-%d.example", id, i), Severity: SeverityMedium},
				}
				if err := s.UpsertIOCs(iocs); err != nil {
					t.Errorf("UpsertIOCs: %v", err)
					return
				}
				if err := s.PutKnowledgeGraph(KnowledgeGraph{
					Techniques: []Technique{{TechniqueID: fmt.Sprintf("T%d", 2000+id), Name: "synthetic"}},
				}); err != nil {
					t.Errorf("PutKnowledgeGraph: %v", err)
					return
				}
			}
		}(w)
	}

	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			<-start
			zero := time.Time{}
			for i := 0; i < perWriter; i++ {
				if _, err := s.RecentCVEs(zero, 25); err != nil {
					t.Errorf("RecentCVEs: %v", err)
					return
				}
				if _, err := s.LookupIOCs("ip", []string{"203.0.113.1", "203.0.113.2"}); err != nil {
					t.Errorf("LookupIOCs: %v", err)
					return
				}
				_, _ = s.TechniqueByID("T1059")
				_ = s.CVECount()
				_ = s.IOCCount()
			}
		}(r)
	}

	close(start) // release all goroutines together to maximize interleaving
	wg.Wait()

	if got := s.CVECount(); got != writers*perWriter {
		t.Errorf("expected %d CVEs after concurrent writes, got %d", writers*perWriter, got)
	}
	// Each writer contributes 1 distinct ip + perWriter distinct domains.
	wantIOCs := writers + writers*perWriter
	if got := s.IOCCount(); got != wantIOCs {
		t.Errorf("expected %d IOCs after concurrent writes, got %d", wantIOCs, got)
	}
}

// TestHub_ConcurrentSTIXImport imports STIX bundles from several goroutines to
// confirm the Hub's store path is safe under parallel ingestion and that STIX id
// based upserts converge (repeated import of the same bundle is idempotent).
func TestHub_ConcurrentSTIXImport(t *testing.T) {
	store := NewMemoryStore()
	h := NewHub(nil, store, nil)

	bundle := []byte(`{
	  "type": "bundle",
	  "id": "bundle--0a1b2c3d-4e5f-6071-8293-a4b5c6d7e8f9",
	  "objects": [
	    {
	      "type": "indicator",
	      "spec_version": "2.1",
	      "id": "indicator--11111111-2222-3333-4444-555555555555",
	      "created": "2026-01-01T00:00:00.000Z",
	      "modified": "2026-01-01T00:00:00.000Z",
	      "pattern_type": "stix",
	      "pattern": "[ipv4-addr:value = '198.51.100.7']",
	      "valid_from": "2026-01-01T00:00:00Z",
	      "x_severity": "critical"
	    },
	    {
	      "type": "indicator",
	      "spec_version": "2.1",
	      "id": "indicator--66666666-7777-8888-9999-aaaaaaaaaaaa",
	      "created": "2026-01-01T00:00:00.000Z",
	      "modified": "2026-01-01T00:00:00.000Z",
	      "pattern_type": "stix",
	      "pattern": "[domain-name:value = 'c2.example']",
	      "valid_from": "2026-01-01T00:00:00Z",
	      "confidence": 95
	    }
	  ]
	}`)

	const goroutines = 16
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := h.ImportSTIXBundle(context.Background(), bundle); err != nil {
				t.Errorf("ImportSTIXBundle: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	// Two distinct observables, imported 16 times concurrently: keyed upserts
	// must collapse to exactly 2 IOCs, proving id/(type,value) deduplication
	// holds under parallelism rather than double-counting.
	if got := store.IOCCount(); got != 2 {
		t.Errorf("expected 2 deduplicated IOCs after %d concurrent imports, got %d", goroutines, got)
	}
}
