package attack_graph

import (
	"math/rand"
	"time"
)

// ============================================================================
// Experience Replay Buffer - For Offline Learning
// ============================================================================
// Stores past experiences (state, action, reward, next_state) for mini-batch SGD updates.
// Implements circular buffer with fixed capacity to prevent memory exhaustion.
// Enables decoupling of experience collection and learning steps.
//
// Design Rationale:
// - Breaks temporal correlation in training data
// - Allows reuse of valuable experiences
// - Stabilizes RL training through statistical regularization

type Experience struct {
	State     State
	Action    Action
	Reward    float64
	NextState State
	Timestamp time.Time
}

type ExperienceReplayBuffer struct {
	capacity int
	buffer   []Experience
	head     int // Next write position
	size     int // Current number of elements

	rand *rand.Rand
}

// NewExperienceReplayBuffer creates a replay buffer with specified capacity
func NewExperienceReplayBuffer(capacity int) *ExperienceReplayBuffer {
	return &ExperienceReplayBuffer{
		capacity: capacity,
		buffer:   make([]Experience, capacity),
		head:     0,
		size:     0,
		rand:     rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Add stores a new experience in the buffer
func (rb *ExperienceReplayBuffer) Add(exp Experience) {
	rb.buffer[rb.head] = exp
	rb.head = (rb.head + 1) % rb.capacity
	if rb.size < rb.capacity {
		rb.size++
	}
}

// Sample returns a random mini-batch of experiences
func (rb *ExperienceReplayBuffer) Sample(batchSize int) []Experience {
	if rb.size == 0 {
		return nil
	}

	batch := make([]Experience, 0, batchSize)
	for i := 0; i < batchSize; i++ {
		idx := rb.rand.Intn(rb.size)
		batch = append(batch, rb.buffer[idx])
	}
	return batch
}

// Size returns the current number of experiences in the buffer
func (rb *ExperienceReplayBuffer) Size() int {
	return rb.size
}

// IsFull returns true if buffer is at capacity
func (rb *ExperienceReplayBuffer) IsFull() bool {
	return rb.size == rb.capacity
}

// GetAll returns all experiences (for debugging/testing)
func (rb *ExperienceReplayBuffer) GetAll() []Experience {
	if rb.size == 0 {
		return nil
	}

	all := make([]Experience, rb.size)
	for i := 0; i < rb.size; i++ {
		idx := (rb.head - rb.size + i + rb.capacity) % rb.capacity
		all[i] = rb.buffer[idx]
	}
	return all
}
