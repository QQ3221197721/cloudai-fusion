package evidence

import (
	"context"
	"sort"
	"sync"
)

// Store persists the append-only evidence chain. The Ledger serializes writes,
// so implementations do not need to assign Seq or compute PrevHash — they only
// durably store records and answer queries. Real = Postgres (GORMStore);
// fallback = MemoryStore (honestly reported as simulated by the wiring).
type Store interface {
	// Append durably stores e. Records arrive in Seq order.
	Append(ctx context.Context, e *Evidence) error
	// Last returns the highest-Seq record, or (nil, nil) if the chain is empty.
	Last(ctx context.Context) (*Evidence, error)
	// Get returns the record with the given ID.
	Get(ctx context.Context, id string) (*Evidence, error)
	// List returns records matching filter, newest first (bounded by Limit).
	List(ctx context.Context, f Filter) ([]*Evidence, error)
	// All returns the full chain in ascending Seq order (for export/verification).
	All(ctx context.Context) ([]*Evidence, error)
	// Count returns the number of records.
	Count(ctx context.Context) (int64, error)
}

// ChainedBuilder builds a record from the current chain head (nil when empty).
type ChainedBuilder func(last *Evidence) (*Evidence, error)

// AtomicStore provides atomic read-head-then-append so Seq/PrevHash assignment is
// safe even with concurrent writers (e.g. the apiserver and scheduler sharing one
// DB-backed chain). The Ledger uses it when available and falls back to an
// in-process lock otherwise.
type AtomicStore interface {
	Store
	AppendChained(ctx context.Context, build ChainedBuilder) (*Evidence, error)
}

// MemoryStore is an in-process, non-durable Store. It is real code (unit-tested)
// but provides no persistence across restarts, so the Ledger reports it to
// pkg/capability as simulated — a production boot then refuses it.
type MemoryStore struct {
	mu      sync.RWMutex
	records []*Evidence
	byID    map[string]*Evidence
}

// NewMemoryStore returns an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: make(map[string]*Evidence)}
}

// Append stores a copy-by-reference record in Seq order.
func (m *MemoryStore) Append(_ context.Context, e *Evidence) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, e)
	m.byID[e.ID] = e
	return nil
}

// Last returns the highest-Seq record or (nil, nil) when empty.
func (m *MemoryStore) Last(_ context.Context) (*Evidence, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.records) == 0 {
		return nil, nil
	}
	return m.records[len(m.records)-1], nil
}

// Get returns the record with id, or nil when not found.
func (m *MemoryStore) Get(_ context.Context, id string) (*Evidence, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.byID[id], nil
}

// List returns matching records newest-first, bounded by f.Limit (default 100).
func (m *MemoryStore) List(_ context.Context, f Filter) ([]*Evidence, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]*Evidence, 0)
	for _, e := range m.records {
		if f.Action != "" && e.Action != f.Action {
			continue
		}
		if f.Subject != "" && e.Subject != f.Subject {
			continue
		}
		if f.Actor != "" && e.Actor != f.Actor {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq > out[j].Seq })
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// All returns the full chain in ascending Seq order.
func (m *MemoryStore) All(_ context.Context) ([]*Evidence, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Evidence, len(m.records))
	copy(out, m.records)
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out, nil
}

// Count returns the number of stored records.
func (m *MemoryStore) Count(_ context.Context) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return int64(len(m.records)), nil
}

// AppendChained atomically reads the head and appends the built record under the
// store lock, satisfying AtomicStore.
func (m *MemoryStore) AppendChained(_ context.Context, build ChainedBuilder) (*Evidence, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var last *Evidence
	if len(m.records) > 0 {
		last = m.records[len(m.records)-1]
	}
	e, err := build(last)
	if err != nil {
		return nil, err
	}
	m.records = append(m.records, e)
	m.byID[e.ID] = e
	return e, nil
}
