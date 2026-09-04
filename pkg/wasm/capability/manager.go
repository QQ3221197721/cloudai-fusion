// Package capability — Module 51: WASM/WASI Capability Security Manager.
//
// This subpackage houses the M51 capability-based access control decision engine
// for WASM plugins: a precompiled capability bitmap giving O(1) permission checks
// for WASI import/export authorization. It is intentionally a SEPARATE package from
// pkg/wasm root (which hosts M42/M50 artifacts) to avoid file/symbol conflicts.
//
// Decision surface: (subject, object, action) — the same shape a classic ACL /
// Casbin enforcer uses — so decisions can be compared 1:1 against a real RBAC engine.
package capability

// PermissionSpec is a single WASI capability grant:
//   - Subject: the plugin / workload identity requesting access
//   - Object:  the resource (filesystem path, host:port, GPU device, import name)
//   - Action:  the operation ("import", "export", "read", ...)
type PermissionSpec struct {
	Subject string
	Object  string
	Action  string
}

// fnv1a64 is the standard FNV-1a 64-bit hash: deterministic and allocation-free.
// Used to key the precompiled capability bitmap.
func fnv1a64(s string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}

// capKey folds a (subject, object, action) tuple into a single 64-bit bitmap key.
// The rotate-and-xor mixing keeps field boundaries distinct without allocating.
func capKey(sub, obj, act string) uint64 {
	ho := fnv1a64(obj)
	ha := fnv1a64(act)
	h := fnv1a64(sub)
	h ^= (ho << 1) | (ho >> 63)
	h ^= (ha << 2) | (ha >> 62)
	return h
}

// Manager is the M51 capability security manager. It precompiles the granted
// capability set into a hash-set bitmap so that CheckPermission is O(1) with zero
// allocations on the hot path — the WASI import/export authorization gate.
type Manager struct {
	granted map[uint64]struct{}
	count   int
}

// NewManager precompiles the capability bitmap from a grant set.
func NewManager(grants []PermissionSpec) *Manager {
	m := &Manager{
		granted: make(map[uint64]struct{}, len(grants)),
		count:   len(grants),
	}
	for _, g := range grants {
		m.granted[capKey(g.Subject, g.Object, g.Action)] = struct{}{}
	}
	return m
}

// Grant adds a single capability to the bitmap (default-deny elsewhere).
func (m *Manager) Grant(sub, obj, act string) {
	if _, ok := m.granted[capKey(sub, obj, act)]; !ok {
		m.count++
	}
	m.granted[capKey(sub, obj, act)] = struct{}{}
}

// CheckPermission is the hot path: O(1) hash + set probe. Returns allow/deny.
// Default-deny: any tuple not explicitly granted is refused.
func (m *Manager) CheckPermission(sub, obj, act string) bool {
	_, ok := m.granted[capKey(sub, obj, act)]
	return ok
}

// Count returns the number of distinct granted capabilities.
func (m *Manager) Count() int { return m.count }
