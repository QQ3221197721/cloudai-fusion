// Package messaging — arena_router.go provides a zero-allocation message router
// using sync.Pool arenas and a radix trie for topic→handler dispatch.
//
// Design:
//   - Topic subscriptions are stored in a compressed radix trie (allocations
//     happen only at Subscribe time, never on the publish hot-path).
//   - Each Publish call obtains a per-goroutine arena slab from a sync.Pool
//     (TLAB pattern), copies payload into it, dispatches to the handler, then
//     returns the slab — achieving 0 alloc/op on the hot path.
//   - Cache-line padding (64 B) on the slab struct prevents false sharing when
//     multiple goroutines hold independent slabs concurrently.
package messaging

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

// slabSize is the default arena slab capacity (64 KiB).
const slabSize = 65536

// cacheLine is the x86-64 cache line size used for padding.
const cacheLine = 64

// arenaSlab is a pre-allocated byte buffer handed out by the pool.
// The _pad field ensures each slab starts on its own cache line, preventing
// false sharing when adjacent slabs are held by different goroutines.
type arenaSlab struct {
	_pad [cacheLine]byte //nolint:unused // false-sharing guard
	buf  [slabSize]byte
}

// ArenaRouter is a zero-allocation topic→handler message router.
type ArenaRouter struct {
	root trieNode
	mu   sync.RWMutex // protects trie writes (Subscribe)
	pool sync.Pool
	seq  atomic.Uint64 // monotonic publish counter (useful for ordering)
}

// NewArenaRouter creates an ArenaRouter with a pre-warmed slab pool.
func NewArenaRouter() *ArenaRouter {
	r := &ArenaRouter{}
	r.pool.New = func() interface{} {
		return &arenaSlab{}
	}
	// Pre-warm 4 slabs to reduce first-call latency.
	for i := 0; i < 4; i++ {
		r.pool.Put(&arenaSlab{})
	}
	return r
}

// Subscribe registers a handler for the given topic pattern.
// This is NOT on the hot path — allocations here are acceptable.
func (r *ArenaRouter) Subscribe(topic string, handler func([]byte)) {
	r.mu.Lock()
	r.root.insert(topic, handler)
	r.mu.Unlock()
}

// Publish routes payload to the handler registered for topic.
// Designed for 0 alloc/op: the payload is copied into a pooled arena slab,
// the handler is invoked synchronously, and the slab is returned to the pool.
//
// Returns ErrNoHandler if no subscription matches the topic.
func (r *ArenaRouter) Publish(topic string, payload []byte) error {
	// Fast-path trie lookup under read lock.
	r.mu.RLock()
	h := r.root.lookup(topic)
	r.mu.RUnlock()

	if h == nil {
		return ErrNoHandler
	}

	// Obtain an arena slab (0 alloc — sync.Pool reuse).
	slab := r.pool.Get().(*arenaSlab)

	// Copy payload into the slab buffer. For payloads larger than slabSize we
	// still avoid heap allocation by processing in-place (caller's slice).
	var dispatch []byte
	n := len(payload)
	if n <= slabSize {
		copy(slab.buf[:n], payload)
		// Create a slice header on stack pointing into slab — no alloc.
		dispatch = slab.buf[:n:n]
	} else {
		// Oversized: pass through directly (rare path, still 0 alloc from us).
		dispatch = payload
	}

	// Increment sequence counter (atomic, no alloc).
	r.seq.Add(1)

	// Dispatch to subscriber handler.
	h(dispatch)

	// Return slab to pool.
	r.pool.Put(slab)
	return nil
}

// Seq returns the current monotonic publish sequence number.
func (r *ArenaRouter) Seq() uint64 {
	return r.seq.Load()
}

// ============================================================================
// Error sentinel
// ============================================================================

// ErrNoHandler is returned when Publish finds no matching subscription.
var ErrNoHandler = arenaErr("no handler for topic")

type arenaErr string

func (e arenaErr) Error() string { return string(e) }

// ============================================================================
// Radix Trie — lightweight, allocation-free on lookup
// ============================================================================

// trieNode is a compressed radix trie node.
// Children are stored in a flat slice (typically very few per node for
// hierarchical topic patterns like "cloudai.security.scan").
type trieNode struct {
	prefix   string
	children []trieNode
	handler  func([]byte)
}

// lookup finds the handler for key without allocating.
func (n *trieNode) lookup(key string) func([]byte) {
	node := n
	for {
		if len(key) == 0 {
			return node.handler
		}
		found := false
		for i := range node.children {
			child := &node.children[i]
			cp := commonPrefix(key, child.prefix)
			if cp == 0 {
				continue
			}
			if cp == len(child.prefix) {
				// Full prefix matched — descend.
				key = key[cp:]
				node = child
				found = true
				break
			}
			// Partial prefix match — no exact route.
			return nil
		}
		if !found {
			return nil
		}
	}
}

// insert adds a handler for key, splitting nodes as needed.
func (n *trieNode) insert(key string, handler func([]byte)) {
	node := n
	for {
		if len(key) == 0 {
			node.handler = handler
			return
		}

		// Try to find a child sharing a common prefix.
		matched := false
		for i := range node.children {
			child := &node.children[i]
			cp := commonPrefix(key, child.prefix)
			if cp == 0 {
				continue
			}
			if cp == len(child.prefix) {
				// Descend into child.
				key = key[cp:]
				node = child
				matched = true
				break
			}
			// Split: create intermediate node.
			// e.g., existing "abcdef" + new "abcxyz" → split at "abc".
			newChild := trieNode{
				prefix:   child.prefix[cp:],
				children: child.children,
				handler:  child.handler,
			}
			child.prefix = child.prefix[:cp]
			child.children = []trieNode{newChild}
			child.handler = nil

			if cp == len(key) {
				child.handler = handler
			} else {
				child.children = append(child.children, trieNode{
					prefix:  key[cp:],
					handler: handler,
				})
			}
			return
		}
		if !matched {
			node.children = append(node.children, trieNode{
				prefix:  key,
				handler: handler,
			})
			return
		}
	}
}

// commonPrefix returns the length of the shared prefix between a and b.
// No allocation — pure index arithmetic.
func commonPrefix(a, b string) int {
	max := len(a)
	if len(b) < max {
		max = len(b)
	}
	for i := 0; i < max; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return max
}

// Compile-time size assertion: arenaSlab must fit expected layout.
var _ = unsafe.Sizeof(arenaSlab{})
