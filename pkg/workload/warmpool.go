package workload

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// WarmPoolStats holds warm pool runtime statistics
type WarmPoolStats struct {
	Capacity     int    `json:"capacity"`
	MinReady     int    `json:"min_ready"`
	AvailablePods int   `json:"available_pods"`
	PrewarmedPod int   `json:"prewarmed_pods_total"`
	HitRate      float64 `json:"hit_rate"`
	CreatedAt    time.Time `json:"created_at"`
}

// PodTemplate represents a pre-warmable pod configuration
type PodTemplate struct {
	Name      string            `json:"name"`
	Namespace string            `json:"namespace"`
	Image     string            `json:"image"`
	Resources map[string]string `json:"resources,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// WarmPool manages a pool of pre-warmed pods for rapid cold-start mitigation
type WarmPool struct {
	ready    chan *PodTemplate
	capacity int
	minReady int
	mu       sync.RWMutex
	hits     int64
	misses   int64
	started  time.Time
	stopCh   chan struct{}
}

// NewWarmPool creates a new warm pool with specified capacity and minimum ready threshold
func NewWarmPool(capacity, minReady int) *WarmPool {
	if capacity < 1 {
		capacity = 10
	}
	if minReady < 1 || minReady > capacity {
		minReady = capacity / 2
	}

	return &WarmPool{
		ready:    make(chan *PodTemplate, capacity),
		capacity: capacity,
		minReady: minReady,
		started:  time.Now().UTC(),
		stopCh:   make(chan struct{}),
	}
}

// PreHeat creates pre-warmed pods up to capacity
func (wp *WarmPool) PreHeat(ctx context.Context, template PodTemplate) error {
	wp.mu.Lock()
	if len(wp.ready) >= wp.capacity {
		wp.mu.Unlock()
		return nil // Already at capacity
	}
	wp.mu.Unlock()

	// Create multiple pre-warmed pods
	var created []*PodTemplate
	for i := 0; i < 3; i++ { // Create in batches for efficiency
		pod := &PodTemplate{
			Name:      fmt.Sprintf("%s-prewarm-%d", template.Name, i),
			Namespace: template.Namespace,
			Image:     template.Image,
			Resources: template.Resources,
			Metadata:  template.Metadata,
		}
		created = append(created, pod)
	}

	// Add to channel with timeout
	for _, pod := range created {
		select {
		case wp.ready <- pod:
			// Successfully added to pool
		default:
			// Pool full, stop adding
			break
		}
	}

	return nil
}

// Acquire retrieves a pre-warmed pod from the pool, or creates one if empty
func (wp *WarmPool) Acquire(ctx context.Context) (*PodTemplate, error) {
	select {
	case pod := <-wp.ready:
		atomic.StoreInt64(&wp.hits, atomic.LoadInt64(&wp.hits)+1)
		return pod, nil
	case <-ctx.Done():
		atomic.AddInt64(&wp.misses, 1)
		return nil, ctx.Err()
	default:
		// No pre-warmed pods available - signal that a cold start is needed
		atomic.AddInt64(&wp.misses, 1)
		return &PodTemplate{Name: "cold-start-needed"}, nil
	}
}

// Return adds a pod back to the warm pool
func (wp *WarmPool) Return(pod *PodTemplate) {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	select {
	case wp.ready <- pod:
		// Successfully returned to pool
	default:
		// Pool at capacity, pod will be dropped
	}
}

// Stats returns current warm pool statistics
func (wp *WarmPool) Stats() WarmPoolStats {
	wp.mu.RLock()
	defer wp.mu.RUnlock()

	totalAttempts := atomic.LoadInt64(&wp.hits) + atomic.LoadInt64(&wp.misses)
	hitRate := float64(0)
	if totalAttempts > 0 {
		hitRate = float64(atomic.LoadInt64(&wp.hits)) / float64(totalAttempts) * 100
	}

	return WarmPoolStats{
		Capacity:       wp.capacity,
		MinReady:       wp.minReady,
		AvailablePods:  len(wp.ready),
		PrewarmedPod:   int(totalAttempts - atomic.LoadInt64(&wp.misses)),
		HitRate:        hitRate,
		CreatedAt:      wp.started,
	}
}

// IsHealthy checks if pool has enough pre-warmed pods
func (wp *WarmPool) IsHealthy() bool {
	return len(wp.ready) >= wp.minReady
}

// Drain gracefully stops accepting new pods and drains the pool
func (wp *WarmPool) Drain(ctx context.Context) {
	close(wp.stopCh)
	
	// Wait for existing pods to return (with timeout)
	timeout := time.After(30 * time.Second)
	for len(wp.ready) > 0 {
		select {
		case <-timeout:
			return
		case <-time.After(500 * time.Millisecond):
			continue
		}
	}
}

// GetStopChannel returns the stop channel
func (wp *WarmPool) GetStopChannel() <-chan struct{} {
	return wp.stopCh
}
