package redteam

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/sirupsen/logrus"
)

// range_manager.go tracks provisioned ranges on top of a RangeProvider so the
// /api/v1/redteam/ranges endpoints can create, list, inspect, and tear down the
// ephemeral practice/eval targets. The provider does the real work (in-memory
// for CI/dry-run, kind-backed for integration); this manager keeps the roster
// and serializes access.
type RangeManager struct {
	provider RangeProvider
	logger   *logrus.Logger
	mu       sync.RWMutex
	ranges   map[string]*Range
}

// NewRangeManager builds a manager over the given provider. A nil provider
// defaults to the in-memory (simulated) provider so the API always functions.
func NewRangeManager(provider RangeProvider, logger *logrus.Logger) *RangeManager {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	if provider == nil {
		provider = NewInMemoryRangeProvider(nil, logger)
	}
	return &RangeManager{provider: provider, logger: logger, ranges: make(map[string]*Range)}
}

// Provision creates a range via the provider and records it in the roster.
func (m *RangeManager) Provision(ctx context.Context, spec RangeSpec) (*Range, error) {
	r, err := m.provider.Provision(ctx, spec)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.ranges[r.ID] = r
	m.mu.Unlock()
	return r, nil
}

// List returns all tracked ranges, ordered by creation time (newest first).
func (m *RangeManager) List() []*Range {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Range, 0, len(m.ranges))
	for _, r := range m.ranges {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// Get returns a tracked range by ID.
func (m *RangeManager) Get(id string) (*Range, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.ranges[id]
	return r, ok
}

// Teardown tears down a range via the provider and removes it from the roster.
func (m *RangeManager) Teardown(ctx context.Context, id string) error {
	m.mu.RLock()
	_, ok := m.ranges[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("redteam: range %q not found", id)
	}
	if err := m.provider.Teardown(ctx, id); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.ranges, id)
	m.mu.Unlock()
	return nil
}
