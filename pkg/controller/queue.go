package controller

import (
	"sync"
	"time"
)

// ============================================================================
// WorkQueue — rate-limited work queue with requeue and backoff support
// ============================================================================

// WorkQueue is a rate-limited, thread-safe work queue that supports
// immediate and delayed requeue. Modeled after client-go's workqueue.
type WorkQueue struct {
	// items holds the FIFO queue of pending requests
	items []Request

	// dirty tracks items that need processing (dedup set)
	dirty map[string]struct{}

	// processing tracks items currently being processed
	processing map[string]struct{}

	// delayed holds items scheduled for future processing
	delayed []delayedItem

	// backoff tracks per-item failure counts for exponential backoff
	backoff map[string]*backoffEntry

	// config
	baseDelay time.Duration // initial backoff delay (default 5ms)
	maxDelay  time.Duration // maximum backoff delay (default 1000s)

	mu        sync.Mutex
	cond      *sync.Cond
	shutdown  bool
	draining  bool
	metrics   QueueMetrics
}

// delayedItem wraps a request with its scheduled processing time.
type delayedItem struct {
	req   Request
	readyAt time.Time
}

// backoffEntry tracks failure count for exponential backoff.
type backoffEntry struct {
	failures int
	lastFail time.Time
}

// QueueMetrics holds runtime queue statistics.
type QueueMetrics struct {
	// TotalAdded is the total number of items added to the queue.
	TotalAdded int64 `json:"total_added"`

	// TotalProcessed is the total number of items completed.
	TotalProcessed int64 `json:"total_processed"`

	// TotalRequeued is the total number of items requeued.
	TotalRequeued int64 `json:"total_requeued"`

	// CurrentDepth is the current queue depth.
	CurrentDepth int `json:"current_depth"`

	// InFlight is the number of items currently being processed.
	InFlight int `json:"in_flight"`

	// LongestRunning tracks the longest item processing time.
	LongestRunning time.Duration `json:"longest_running"`
}

// NewWorkQueue creates a new rate-limited work queue.
func NewWorkQueue() *WorkQueue {
	q := &WorkQueue{
		dirty:      make(map[string]struct{}),
		processing: make(map[string]struct{}),
		backoff:    make(map[string]*backoffEntry),
		baseDelay:  5 * time.Millisecond,
		maxDelay:   1000 * time.Second,
	}
	q.cond = sync.NewCond(&q.mu)

	// Start the delayed item promoter
	go q.promoteDelayedLoop()

	return q
}

// NewWorkQueueWithConfig creates a work queue with custom backoff parameters.
func NewWorkQueueWithConfig(baseDelay, maxDelay time.Duration) *WorkQueue {
	q := NewWorkQueue()
	q.baseDelay = baseDelay
	q.maxDelay = maxDelay
	return q
}

// Add adds a request to the queue (deduplication: if already queued, skip).
func (q *WorkQueue) Add(req Request) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.shutdown {
		return
	}

	key := req.NamespacedName()

	// Dedup: skip if already in dirty set
	if _, exists := q.dirty[key]; exists {
		return
	}

	q.dirty[key] = struct{}{}
	q.metrics.TotalAdded++

	// If currently being processed, it will be re-added when Done() is called
	if _, inFlight := q.processing[key]; inFlight {
		return
	}

	q.items = append(q.items, req)
	q.metrics.CurrentDepth = len(q.items)
	q.cond.Signal()
}

// AddAfter adds a request to the queue after a specified delay.
func (q *WorkQueue) AddAfter(req Request, delay time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.shutdown {
		return
	}

	if delay <= 0 {
		// No delay — add immediately (but unlock/relock to avoid holding two locks)
		q.mu.Unlock()
		q.Add(req)
		q.mu.Lock()
		return
	}

	q.delayed = append(q.delayed, delayedItem{
		req:     req,
		readyAt: time.Now().Add(delay),
	})
}

// AddRateLimited adds a request using exponential backoff based on failure count.
func (q *WorkQueue) AddRateLimited(req Request) {
	key := req.NamespacedName()

	q.mu.Lock()
	entry, exists := q.backoff[key]
	if !exists {
		entry = &backoffEntry{}
		q.backoff[key] = entry
	}
	entry.failures++
	entry.lastFail = time.Now()
	delay := q.calculateBackoff(entry.failures)
	q.mu.Unlock()

	q.AddAfter(req, delay)
	q.mu.Lock()
	q.metrics.TotalRequeued++
	q.mu.Unlock()
}

// Get blocks until an item is available, then returns it.
// The caller MUST call Done() when processing is complete.
// Returns (Request{}, true) if the queue has been shut down.
func (q *WorkQueue) Get() (Request, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for len(q.items) == 0 && !q.shutdown {
		q.cond.Wait()
	}

	if q.shutdown && len(q.items) == 0 {
		return Request{}, true
	}

	// Dequeue from front
	req := q.items[0]
	q.items = q.items[1:]
	q.metrics.CurrentDepth = len(q.items)

	key := req.NamespacedName()
	q.processing[key] = struct{}{}
	delete(q.dirty, key)
	q.metrics.InFlight = len(q.processing)

	return req, false
}

// Done marks a request as done processing.
// If the request was re-added while being processed, it will be enqueued again.
func (q *WorkQueue) Done(req Request) {
	q.mu.Lock()
	defer q.mu.Unlock()

	key := req.NamespacedName()
	delete(q.processing, key)
	q.metrics.InFlight = len(q.processing)
	q.metrics.TotalProcessed++

	// If re-added while processing, enqueue it now
	if _, dirty := q.dirty[key]; dirty {
		q.items = append(q.items, req)
		q.metrics.CurrentDepth = len(q.items)
		q.cond.Signal()
	}
}

// Forget resets the backoff counter for a request (call on successful reconciliation).
func (q *WorkQueue) Forget(req Request) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.backoff, req.NamespacedName())
}

// NumRequeues returns the number of times a request has been requeued.
func (q *WorkQueue) NumRequeues(req Request) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	if entry, ok := q.backoff[req.NamespacedName()]; ok {
		return entry.failures
	}
	return 0
}

// Len returns the current number of items in the queue.
func (q *WorkQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// ShutDown signals the queue to stop accepting new items.
// Existing items will be drained.
func (q *WorkQueue) ShutDown() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.shutdown = true
	q.cond.Broadcast()
}

// ShuttingDown returns true if ShutDown has been called.
func (q *WorkQueue) ShuttingDown() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.shutdown
}

// Metrics returns current queue statistics.
func (q *WorkQueue) Metrics() QueueMetrics {
	q.mu.Lock()
	defer q.mu.Unlock()
	m := q.metrics
	m.CurrentDepth = len(q.items)
	m.InFlight = len(q.processing)
	return m
}

// calculateBackoff computes exponential backoff delay: baseDelay * 2^(failures-1).
func (q *WorkQueue) calculateBackoff(failures int) time.Duration {
	if failures <= 0 {
		return q.baseDelay
	}

	delay := q.baseDelay
	for i := 1; i < failures; i++ {
		delay *= 2
		if delay > q.maxDelay {
			return q.maxDelay
		}
	}
	return delay
}

// promoteDelayedLoop periodically checks delayed items and promotes them to the queue.
func (q *WorkQueue) promoteDelayedLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		q.mu.Lock()
		if q.shutdown {
			q.mu.Unlock()
			return
		}

		now := time.Now()
		remaining := make([]delayedItem, 0, len(q.delayed))
		var toPromote []Request

		for _, d := range q.delayed {
			if now.After(d.readyAt) || now.Equal(d.readyAt) {
				toPromote = append(toPromote, d.req)
			} else {
				remaining = append(remaining, d)
			}
		}
		q.delayed = remaining
		q.mu.Unlock()

		// Add promoted items (outside lock to avoid deadlock)
		for _, req := range toPromote {
			q.Add(req)
		}
	}
}
