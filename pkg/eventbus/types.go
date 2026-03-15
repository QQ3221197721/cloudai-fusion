// Package eventbus implements an event-driven architecture for CloudAI Fusion.
// Provides a publish/subscribe event bus that decouples services from direct
// function calls. Supports pluggable backends: in-memory (default) and NATS.
//
// Events flow through topics. Publishers emit events without knowing who
// subscribes; subscribers react to events without knowing who published.
// This is the foundation for fully decoupled microservice communication.
package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// ============================================================================
// Event — the universal message envelope
// ============================================================================

// Event represents a domain event flowing through the event bus.
type Event struct {
	// ID is the unique event identifier (UUID).
	ID string `json:"id"`

	// Topic is the event category/channel (e.g., "cluster.created", "workload.scheduled").
	Topic string `json:"topic"`

	// Type is the event type within the topic (e.g., "Created", "Updated", "Deleted").
	Type string `json:"type"`

	// Source identifies the service that emitted this event.
	Source string `json:"source"`

	// Timestamp is when the event was created.
	Timestamp time.Time `json:"timestamp"`

	// Data is the event payload (serialized as JSON).
	Data json.RawMessage `json:"data"`

	// Metadata holds optional key-value pairs for routing, tracing, etc.
	Metadata map[string]string `json:"metadata,omitempty"`

	// CorrelationID links related events together (e.g., saga pattern).
	CorrelationID string `json:"correlation_id,omitempty"`

	// CausationID is the ID of the event that caused this event.
	CausationID string `json:"causation_id,omitempty"`
}

// NewEvent creates a new event with the given topic, type, source, and payload.
func NewEvent(topic, eventType, source string, data interface{}) (*Event, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event data: %w", err)
	}
	return &Event{
		ID:        generateEventID(),
		Topic:     topic,
		Type:      eventType,
		Source:    source,
		Timestamp: time.Now().UTC(),
		Data:      payload,
		Metadata:  make(map[string]string),
	}, nil
}

// UnmarshalData deserializes the event payload into the target.
func (e *Event) UnmarshalData(target interface{}) error {
	return json.Unmarshal(e.Data, target)
}

// WithCorrelation sets the correlation and causation IDs.
func (e *Event) WithCorrelation(correlationID, causationID string) *Event {
	e.CorrelationID = correlationID
	e.CausationID = causationID
	return e
}

// WithMetadata adds a key-value pair to the event metadata.
func (e *Event) WithMetadata(key, value string) *Event {
	if e.Metadata == nil {
		e.Metadata = make(map[string]string)
	}
	e.Metadata[key] = value
	return e
}

// ============================================================================
// Handler — event processing callback
// ============================================================================

// Handler is a function that processes an event.
// Returning an error signals the bus to retry (backend-dependent).
type Handler func(ctx context.Context, event *Event) error

// ============================================================================
// Subscription — topic subscription handle
// ============================================================================

// Subscription represents an active subscription to a topic.
type Subscription struct {
	// ID is the unique subscription identifier.
	ID string

	// Topic is the subscribed topic pattern (supports wildcards: "cluster.*").
	Topic string

	// Group is the optional consumer group (for load-balanced delivery).
	Group string

	// Handler is the event processing function.
	Handler Handler

	// active indicates whether the subscription is still active.
	active bool
}

// Unsubscribe deactivates this subscription.
func (s *Subscription) Unsubscribe() {
	s.active = false
}

// IsActive returns whether the subscription is still active.
func (s *Subscription) IsActive() bool {
	return s.active
}

// ============================================================================
// Well-known Topics
// ============================================================================

const (
	// Cluster lifecycle events
	TopicClusterCreated   = "cluster.created"
	TopicClusterUpdated   = "cluster.updated"
	TopicClusterDeleted   = "cluster.deleted"
	TopicClusterHealth    = "cluster.health"

	// Workload lifecycle events
	TopicWorkloadCreated   = "workload.created"
	TopicWorkloadScheduled = "workload.scheduled"
	TopicWorkloadRunning   = "workload.running"
	TopicWorkloadCompleted = "workload.completed"
	TopicWorkloadFailed    = "workload.failed"

	// Security events
	TopicSecurityPolicyApplied  = "security.policy.applied"
	TopicSecurityViolation      = "security.violation"
	TopicSecurityScanCompleted  = "security.scan.completed"

	// Scheduling events
	TopicScheduleRequest  = "scheduler.request"
	TopicScheduleDecision = "scheduler.decision"

	// Agent events
	TopicAgentInsight = "agent.insight"
	TopicAgentAction  = "agent.action"

	// System events
	TopicSystemAlert  = "system.alert"
	TopicSystemMetric = "system.metric"
)

// ============================================================================
// Config
// ============================================================================

// Config holds event bus configuration.
type Config struct {
	// Backend selects the event bus backend: "memory" (default) or "nats".
	Backend string `mapstructure:"backend"`

	// NATSURL is the NATS server URL (only used when Backend="nats").
	NATSURL string `mapstructure:"nats_url"`

	// NATSClusterID is the NATS cluster ID for streaming.
	NATSClusterID string `mapstructure:"nats_cluster_id"`

	// BufferSize is the channel buffer size for in-memory backend.
	BufferSize int `mapstructure:"buffer_size"`

	// MaxRetries is the maximum number of retry attempts for failed handlers.
	MaxRetries int `mapstructure:"max_retries"`

	// RetryDelay is the base delay between retries.
	RetryDelay time.Duration `mapstructure:"retry_delay"`
}

// DefaultConfig returns default event bus configuration.
func DefaultConfig() Config {
	return Config{
		Backend:    "memory",
		NATSURL:    "nats://localhost:4222",
		BufferSize: 4096,
		MaxRetries: 3,
		RetryDelay: 1 * time.Second,
	}
}

// ============================================================================
// Utility
// ============================================================================

func generateEventID() string {
	return fmt.Sprintf("evt-%x-%x",
		time.Now().UnixNano(),
		time.Now().UnixNano()>>32&0xFFFF)
}
