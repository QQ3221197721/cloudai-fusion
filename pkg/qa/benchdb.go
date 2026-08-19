package qa

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
)

// benchdb.go is the Benchmark Database: a pure-Go, file-backed store of benchmark
// runs used as the historical corpus the Performance Regressor compares against.
// It persists to a single JSON file and is safe for concurrent use.
//
// Ordering is by an explicit monotonic Seq counter, NOT by wall-clock time. This
// is deliberate: on Windows the system clock has ~15ms granularity, so two runs
// saved in quick succession can share a timestamp and sort non-deterministically.
// A monotonic sequence assigned at Save time gives a total, stable order.

// BenchSample is one benchmark's measured result, mirroring `go test -bench`
// output columns (ns/op, B/op, allocs/op).
type BenchSample struct {
	Name        string  `json:"name"`
	NsPerOp     float64 `json:"ns_per_op"`
	BytesPerOp  int64   `json:"bytes_per_op"`
	AllocsPerOp int64   `json:"allocs_per_op"`
	Iterations  int64   `json:"iterations"`
}

// BenchRun is a single measurement session: a set of samples captured together
// (e.g. one `go test -bench=. -count=N` invocation on a commit).
type BenchRun struct {
	ID      string        `json:"id"`
	Seq     int64         `json:"seq"` // monotonic ordering key, clock-independent
	Label   string        `json:"label,omitempty"`
	Samples []BenchSample `json:"samples"`
}

// BenchmarkDB is a concurrency-safe, file-backed collection of BenchRuns kept
// sorted ascending by Seq.
type BenchmarkDB struct {
	path string
	mu   sync.RWMutex
	runs []BenchRun
	seq  int64
}

// NewBenchDB opens (or creates) a benchmark DB backed by the JSON file at path.
// A missing file is treated as an empty DB; a present file is loaded and its
// highest Seq becomes the starting point for future Save calls.
func NewBenchDB(path string) (*BenchmarkDB, error) {
	db := &BenchmarkDB{path: path}
	if err := db.load(); err != nil {
		return nil, err
	}
	return db, nil
}

// load reads the backing file into memory. A non-existent file is not an error.
func (db *BenchmarkDB) load() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	data, err := os.ReadFile(db.path)
	if err != nil {
		if os.IsNotExist(err) {
			db.runs = nil
			db.seq = 0
			return nil
		}
		return fmt.Errorf("qa: reading bench db: %w", err)
	}
	var runs []BenchRun
	if len(data) > 0 {
		if err := json.Unmarshal(data, &runs); err != nil {
			return fmt.Errorf("qa: parsing bench db: %w", err)
		}
	}
	sortRunsBySeq(runs)
	db.runs = runs
	for _, r := range runs {
		if r.Seq > db.seq {
			db.seq = r.Seq
		}
	}
	return nil
}

// Save appends run and persists the DB. If run.Seq is 0 a fresh monotonic Seq is
// assigned; a caller-provided Seq is honored (and advances the counter if higher)
// so tests can pin a deterministic order. The persisted run is returned.
func (db *BenchmarkDB) Save(run BenchRun) (BenchRun, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	if run.Seq == 0 {
		db.seq++
		run.Seq = db.seq
	} else if run.Seq > db.seq {
		db.seq = run.Seq
	}
	if run.ID == "" {
		run.ID = fmt.Sprintf("run-%d", run.Seq)
	}
	db.runs = append(db.runs, run)
	sortRunsBySeq(db.runs)

	if err := db.persistLocked(); err != nil {
		return BenchRun{}, err
	}
	return run, nil
}

// persistLocked writes the in-memory runs to disk. Caller must hold db.mu.
func (db *BenchmarkDB) persistLocked() error {
	data, err := json.MarshalIndent(db.runs, "", "  ")
	if err != nil {
		return fmt.Errorf("qa: encoding bench db: %w", err)
	}
	if err := os.WriteFile(db.path, data, 0o644); err != nil {
		return fmt.Errorf("qa: writing bench db: %w", err)
	}
	return nil
}

// Len reports how many runs are stored.
func (db *BenchmarkDB) Len() int {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return len(db.runs)
}

// Recent returns up to n runs, most recent (highest Seq) first. n<=0 returns nil.
func (db *BenchmarkDB) Recent(n int) []BenchRun {
	db.mu.RLock()
	defer db.mu.RUnlock()
	if n <= 0 || len(db.runs) == 0 {
		return nil
	}
	if n > len(db.runs) {
		n = len(db.runs)
	}
	out := make([]BenchRun, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, db.runs[len(db.runs)-1-i])
	}
	return out
}

// Baseline returns the oldest stored run (lowest Seq) - the reference point for
// regression comparisons - and false when the DB is empty.
func (db *BenchmarkDB) Baseline() (BenchRun, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	if len(db.runs) == 0 {
		return BenchRun{}, false
	}
	return db.runs[0], true
}

// Latest returns the most recent stored run (highest Seq) and false when empty.
func (db *BenchmarkDB) Latest() (BenchRun, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	if len(db.runs) == 0 {
		return BenchRun{}, false
	}
	return db.runs[len(db.runs)-1], true
}

// sortRunsBySeq sorts ascending by Seq using a stable sort so runs that somehow
// share a Seq keep insertion order.
func sortRunsBySeq(runs []BenchRun) {
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].Seq < runs[j].Seq })
}
