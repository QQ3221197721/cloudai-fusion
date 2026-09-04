package capability

import (
	"strings"
	"sync"
)

// FlagResolver is a production-grade, allocation-lean resolver for the small set
// of command-line flags the platform consults on every binary's startup path
// (apiserver, scheduler, agent all read --run-mode / --env / --log-level etc.).
//
// Why not just use the stdlib flag package everywhere? flag is a general-purpose
// parser: every FlagSet allocates a struct plus two maps, every StringVar boxes a
// *stringValue behind the flag.Value interface, and every Lookup returns a *Flag
// whose value is read through a dynamic-dispatch .Value.String(). For the handful
// of flags we resolve once at boot and then read repeatedly, that machinery is
// pure overhead.
//
// FlagResolver splits the work into two explicit phases:
//   - Cold path (Parse): a single linear scan over the argument slice, matching
//     against a tiny registered-name slice. No FlagSet, no flag.Value boxing, no
//     usage bookkeeping. Only one allocation (the values slice) amortized across
//     all flags.
//   - Hot path (LookupString): after Warmup builds a direct name->value cache,
//     each lookup is a single map read returning the string directly — no *Flag
//     indirection and no interface dispatch, so it is zero-allocation.
//
// It is concurrency-safe: Parse/Register take the write lock; LookupString takes
// the read lock and, once warmed, never mutates shared state.
type FlagResolver struct {
	mu       sync.RWMutex
	names    []string          // registered flag names, WITHOUT leading dashes
	values   []string          // resolved values, index-aligned with names
	cache    map[string]string // name -> value, built by Warmup for the hot path
	resolved bool
}

// NewFlagResolver creates an empty resolver. Register known flags before Parse to
// get the fastest hot path; unknown flags encountered during Parse are still
// captured (auto-registered) so callers never silently lose a value.
func NewFlagResolver() *FlagResolver {
	return &FlagResolver{}
}

// normalizeName strips any leading dashes so "--run-mode", "-run-mode" and
// "run-mode" all resolve to the same canonical key.
func normalizeName(name string) string {
	return strings.TrimLeft(name, "-")
}

// Register declares a known flag ahead of Parse and returns its stable index.
// Registering the same name twice returns the existing index.
func (r *FlagResolver) Register(name string) int {
	canon := normalizeName(name)
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, n := range r.names {
		if n == canon {
			return i
		}
	}
	idx := len(r.names)
	r.names = append(r.names, canon)
	r.values = append(r.values, "")
	return idx
}

// Parse resolves all flags from args in a single linear scan. It accepts both the
// "--name=value" and the "--name value" (space-separated) forms, single or double
// dash. This is the cold path; call it once at startup.
func (r *FlagResolver) Parse(args []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Ensure values is index-aligned with names before we start writing.
	if len(r.values) < len(r.names) {
		grown := make([]string, len(r.names))
		copy(grown, r.values)
		r.values = grown
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if len(arg) < 2 || arg[0] != '-' {
			continue
		}

		// Strip one or two leading dashes.
		body := arg[1:]
		if len(body) > 0 && body[0] == '-' {
			body = body[1:]
		}
		if body == "" {
			continue
		}

		// Split "name=value"; otherwise consume the next token as the value.
		var name, val string
		if eq := strings.IndexByte(body, '='); eq >= 0 {
			name = body[:eq]
			val = body[eq+1:]
		} else {
			name = body
			if i+1 < len(args) && (len(args[i+1]) == 0 || args[i+1][0] != '-') {
				val = args[i+1]
				i++
			}
		}

		r.setValueLocked(name, val)
	}

	r.resolved = true
	// Invalidate any stale hot-path cache; it will be rebuilt on Warmup/lookup.
	r.cache = nil
	return nil
}

// setValueLocked stores val for name, auto-registering unknown names so no value
// is silently dropped. Caller must hold the write lock.
func (r *FlagResolver) setValueLocked(name, val string) {
	for i, n := range r.names {
		if n == name {
			r.values[i] = val
			return
		}
	}
	r.names = append(r.names, name)
	r.values = append(r.values, val)
}

// Warmup builds the direct name->value cache that powers the zero-allocation hot
// path. It is idempotent and safe to call multiple times.
func (r *FlagResolver) Warmup() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buildCacheLocked()
}

// buildCacheLocked (re)builds the hot-path cache. Caller must hold the write lock.
func (r *FlagResolver) buildCacheLocked() {
	if len(r.names) == 0 {
		r.cache = map[string]string{}
		return
	}
	m := make(map[string]string, len(r.names))
	for i, n := range r.names {
		m[n] = r.values[i]
	}
	r.cache = m
}

// LookupString returns the resolved value for name (leading dashes optional).
// After Warmup this is a single map read with zero allocations. Before Warmup it
// falls back to a linear scan under the write lock (still correct, just slower),
// building the cache opportunistically.
func (r *FlagResolver) LookupString(name string) string {
	canon := normalizeName(name)

	r.mu.RLock()
	if r.cache != nil {
		val := r.cache[canon]
		r.mu.RUnlock()
		return val
	}
	r.mu.RUnlock()

	// Cache miss: build it once under the write lock, then serve from it.
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cache == nil {
		r.buildCacheLocked()
	}
	return r.cache[canon]
}

// IsResolved reports whether Parse has completed at least once.
func (r *FlagResolver) IsResolved() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.resolved
}

// Reset clears all registrations, values and cache, returning the resolver to its
// freshly-constructed state (used to force the cold path in benchmarks/tests).
func (r *FlagResolver) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.names = nil
	r.values = nil
	r.cache = nil
	r.resolved = false
}

// Stats returns registry counters for debugging/observability.
func (r *FlagResolver) Stats() (registered, cached, values int) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c := 0
	if r.cache != nil {
		c = len(r.cache)
	}
	return len(r.names), c, len(r.values)
}
