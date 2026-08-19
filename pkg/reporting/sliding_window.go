// Package reporting - DGIM-style exponentially-decaying logarithmic bucket
// sliding window for O(log W) space / O(1) per-event streaming aggregation.
//
// The SlidingWindow maintains a count of events within a time window [t-W, t]
// using the DGIM (Datar-Gionis-Indyk-Motwani) bucket merging strategy. This
// provides a provable O(log W) space bound with at most 50% relative error in
// approximate mode. An exact mode is also provided for FinOps billing paths
// where zero-error is mandatory — it stores every timestamp (O(n) space) but
// shares the same interface so callers can select precision per use-case.
package reporting

import (
	"sync"
	"time"
)

// WindowMode selects the precision/space trade-off.
type WindowMode int

const (
	// WindowModeApproximate uses DGIM logarithmic buckets: O(log W) space,
	// ≤50% relative error on count queries. Suitable for dashboards and
	// capacity trend lines where approximate counts are acceptable.
	WindowModeApproximate WindowMode = iota

	// WindowModeExact stores every event timestamp: O(n) space but zero
	// counting error. Required for FinOps billing reconciliation where a
	// single missed event creates an accounting discrepancy.
	WindowModeExact
)

// dgimBucket represents one power-of-2 sized bucket in the DGIM scheme.
// Each bucket records the timestamp of its most recent constituent event and
// the count of events it represents (always a power of 2).
type dgimBucket struct {
	timestamp int64 // unix nanoseconds of the newest event in this bucket
	size      int64 // number of events represented (power of 2)
}

// maxDGIMLevels bounds the power-of-2 levels. For W=3600s at nanosecond
// granularity: log2(3.6e12) < 42. We use 48 for headroom.
const maxDGIMLevels = 48

// dgimLevel holds up to 2 buckets at one power-of-2 size class.
// Buckets are stored oldest-first: slot[0] is older than slot[1].
type dgimLevel struct {
	slots [2]dgimBucket
	count int // 0, 1, or 2 active buckets at this level
}

// SlidingWindow is a time-based sliding window counter that supports both
// DGIM approximate counting and exact counting modes. It is concurrency-safe.
//
// Algorithm (approximate mode):
//   - Incoming events create size-1 buckets at the current timestamp.
//   - When three buckets of the same size exist, the two oldest merge into one
//     bucket of double size (retaining the newer timestamp of the pair).
//   - Expired buckets (timestamp < now - windowDuration) are discarded on query.
//   - Sum() returns the count by summing all bucket sizes, minus half the
//     oldest bucket (DGIM error-correction heuristic).
//
// Algorithm (exact mode):
//   - Every event timestamp is appended to a circular deque.
//   - Expired entries are lazily evicted on Add/Sum.
//   - Sum() returns the exact count of entries within the window.
type SlidingWindow struct {
	mu       sync.Mutex
	mode     WindowMode
	windowNs int64 // window duration in nanoseconds

	// --- approximate mode state (level-indexed, zero-alloc after init) ---
	levels [maxDGIMLevels]dgimLevel
	total  int64 // running total of all bucket sizes (fast-path)

	// --- exact mode state ---
	ring    []int64 // circular buffer of event timestamps (unix nanos)
	ringLen int     // number of valid entries
	ringCap int     // allocated capacity
	head    int     // next write position
}

// SlidingWindowConfig configures a new SlidingWindow.
type SlidingWindowConfig struct {
	Window time.Duration
	Mode   WindowMode
}

// NewSlidingWindow creates a sliding window counter.
func NewSlidingWindow(cfg SlidingWindowConfig) *SlidingWindow {
	sw := &SlidingWindow{
		mode:     cfg.Mode,
		windowNs: cfg.Window.Nanoseconds(),
	}
	if cfg.Mode == WindowModeExact {
		// Pre-allocate for exact mode; will grow if needed.
		sw.ringCap = 4096
		sw.ring = make([]int64, sw.ringCap)
	}
	// Approximate mode uses the fixed-size levels array (zero-alloc).
	return sw
}

// Add registers one event at the given timestamp. O(1) amortized for
// approximate mode (bucket merges are bounded by O(log W) per size class with
// at most 2 merges cascading). O(1) amortized for exact mode (ring append +
// lazy eviction).
func (sw *SlidingWindow) Add(ts time.Time) {
	sw.mu.Lock()
	tsNano := ts.UnixNano()

	if sw.mode == WindowModeApproximate {
		sw.addApproximate(tsNano)
	} else {
		sw.addExact(tsNano)
	}
	sw.mu.Unlock()
}

// AddN registers n events at the given timestamp (bulk insert).
func (sw *SlidingWindow) AddN(ts time.Time, n int) {
	sw.mu.Lock()
	tsNano := ts.UnixNano()
	if sw.mode == WindowModeApproximate {
		for i := 0; i < n; i++ {
			sw.addApproximate(tsNano)
		}
	} else {
		for i := 0; i < n; i++ {
			sw.addExact(tsNano)
		}
	}
	sw.mu.Unlock()
}

// Sum returns the count of events within the sliding window ending at `now`.
// Approximate mode: O(log W) scan of buckets, result may overcount by ≤50%.
// Exact mode: O(evicted) amortized, returns exact count.
func (sw *SlidingWindow) Sum(now time.Time) int64 {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	nowNano := now.UnixNano()
	cutoff := nowNano - sw.windowNs

	if sw.mode == WindowModeApproximate {
		return sw.sumApproximate(cutoff)
	}
	return sw.sumExact(cutoff)
}

// MemoryBytes returns an estimate of the current memory consumption in bytes.
func (sw *SlidingWindow) MemoryBytes() int64 {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if sw.mode == WindowModeApproximate {
		// Count active buckets across all levels. Each: 16 bytes.
		var count int64
		for i := 0; i < maxDGIMLevels; i++ {
			count += int64(sw.levels[i].count)
		}
		return count * 16
	}
	// Exact mode: 8 bytes per ring slot (int64 timestamp)
	return int64(sw.ringCap) * 8
}

// BucketCount returns the number of active buckets (approximate mode) or
// active entries (exact mode). Useful for diagnostics.
func (sw *SlidingWindow) BucketCount() int {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if sw.mode == WindowModeApproximate {
		var count int
		for i := 0; i < maxDGIMLevels; i++ {
			count += sw.levels[i].count
		}
		return count
	}
	return sw.ringLen
}

// ---------------------------------------------------------------------------
// Approximate mode internals (level-indexed, O(1) per-event)
// ---------------------------------------------------------------------------

func (sw *SlidingWindow) addApproximate(tsNano int64) {
	// Insert a size-1 bucket at level 0, then cascade merges upward.
	sw.insertAtLevel(0, dgimBucket{timestamp: tsNano, size: 1})
}

// insertAtLevel adds a bucket to the given level. If the level already has 2
// buckets, merges the two oldest (they become one bucket at level+1).
func (sw *SlidingWindow) insertAtLevel(level int, b dgimBucket) {
	for level < maxDGIMLevels {
		lvl := &sw.levels[level]
		if lvl.count < 2 {
			// Room available — just insert.
			lvl.slots[lvl.count] = b
			lvl.count++
			sw.total += b.size
			return
		}
		// Level full (2 buckets). Merge the oldest bucket (slot[0]) with the
		// second (slot[1]) to create a double-size bucket for the next level.
		// Keep the incoming bucket `b` at this level.
		mergedTS := lvl.slots[0].timestamp
		if lvl.slots[1].timestamp > mergedTS {
			mergedTS = lvl.slots[1].timestamp
		}
		merged := dgimBucket{timestamp: mergedTS, size: lvl.slots[0].size * 2}
		sw.total -= lvl.slots[0].size + lvl.slots[1].size // remove old pair

		// Replace level with just the incoming bucket.
		lvl.slots[0] = b
		lvl.count = 1
		sw.total += b.size

		// Cascade merged bucket to next level.
		b = merged
		level++
	}
}

func (sw *SlidingWindow) sumApproximate(cutoff int64) int64 {
	// Evict expired buckets and sum the remainder.
	var sum int64
	var oldestSize int64
	var oldestTS int64

	for i := maxDGIMLevels - 1; i >= 0; i-- {
		lvl := &sw.levels[i]
		// Process slots, evicting expired ones.
		newCount := 0
		for j := 0; j < lvl.count; j++ {
			if lvl.slots[j].timestamp >= cutoff {
				if newCount != j {
					lvl.slots[newCount] = lvl.slots[j]
				}
				newCount++
			} else {
				sw.total -= lvl.slots[j].size
			}
		}
		lvl.count = newCount

		for j := 0; j < lvl.count; j++ {
			sum += lvl.slots[j].size
			// Track the oldest (largest-level, oldest-timestamp) bucket.
			if oldestTS == 0 || lvl.slots[j].timestamp < oldestTS {
				oldestTS = lvl.slots[j].timestamp
				oldestSize = lvl.slots[j].size
			}
		}
	}

	// DGIM error-correction: subtract half of the oldest bucket.
	if oldestSize > 0 {
		sum -= oldestSize / 2
	}
	if sum < 0 {
		sum = 0
	}
	return sum
}

// ---------------------------------------------------------------------------
// Exact mode internals
// ---------------------------------------------------------------------------

func (sw *SlidingWindow) addExact(tsNano int64) {
	// Check and grow ring if full.
	if sw.ringLen == sw.ringCap {
		// Grow ring 2x only when truly needed, with minimum cap.
		oldCap := sw.ringCap
		if sw.ringCap < 32768 {
			sw.ringCap *= 2
		} else {
			sw.ringCap = oldCap + 32768 // Gradual growth for large buffers
		}
		newRing := make([]int64, sw.ringCap)
		if sw.ringLen > 0 {
			start := (sw.head - sw.ringLen + oldCap) % oldCap
			copy(newRing, sw.ring[start:start+sw.ringLen])
		}
		sw.ring = newRing
		sw.head = sw.ringLen
	}
	sw.ring[sw.head] = tsNano
	sw.head = (sw.head + 1) % sw.ringCap
	sw.ringLen++
}

func (sw *SlidingWindow) sumExact(cutoff int64) int64 {
	// Evict entries from tail that are expired.
	tail := (sw.head - sw.ringLen + sw.ringCap) % sw.ringCap
	for sw.ringLen > 0 {
		if sw.ring[tail] < cutoff {
			tail = (tail + 1) % sw.ringCap
			sw.ringLen--
		} else {
			break
		}
	}
	return int64(sw.ringLen)
}

// ---------------------------------------------------------------------------
// Multi-group sliding window aggregator
// ---------------------------------------------------------------------------

// SlidingWindowAggregator extends the single-counter SlidingWindow to operate
// over grouped dimensions (like StreamAggregator) but with time-decay
// semantics: only events within [now-W, now] contribute to counts.
type SlidingWindowAggregator struct {
	mu       sync.Mutex
	mode     WindowMode
	windowNs int64
	dims     []string
	windows  map[string]*SlidingWindow
}

// NewSlidingWindowAggregator creates a grouped sliding window aggregator.
func NewSlidingWindowAggregator(dims []string, window time.Duration, mode WindowMode) *SlidingWindowAggregator {
	return &SlidingWindowAggregator{
		mode:     mode,
		windowNs: window.Nanoseconds(),
		dims:     append([]string(nil), dims...),
		windows:  make(map[string]*SlidingWindow, 256),
	}
}

// Add folds one record into the appropriate group window.
func (swa *SlidingWindowAggregator) Add(rec *Record) {
	key := compositeKeyOf(rec, swa.dims)
	swa.mu.Lock()
	w := swa.windows[key]
	if w == nil {
		w = NewSlidingWindow(SlidingWindowConfig{
			Window: time.Duration(swa.windowNs),
			Mode:   swa.mode,
		})
		swa.windows[key] = w
	}
	swa.mu.Unlock()
	w.Add(rec.Timestamp)
}

// Sum returns the windowed count for a specific group key at the given time.
func (swa *SlidingWindowAggregator) Sum(key string, now time.Time) int64 {
	swa.mu.Lock()
	w := swa.windows[key]
	swa.mu.Unlock()
	if w == nil {
		return 0
	}
	return w.Sum(now)
}

// TotalSum returns the sum across all groups at the given time.
func (swa *SlidingWindowAggregator) TotalSum(now time.Time) int64 {
	swa.mu.Lock()
	windows := make([]*SlidingWindow, 0, len(swa.windows))
	for _, w := range swa.windows {
		windows = append(windows, w)
	}
	swa.mu.Unlock()

	var total int64
	for _, w := range windows {
		total += w.Sum(now)
	}
	return total
}

// GroupCount returns the number of distinct groups tracked.
func (swa *SlidingWindowAggregator) GroupCount() int {
	swa.mu.Lock()
	defer swa.mu.Unlock()
	return len(swa.windows)
}
