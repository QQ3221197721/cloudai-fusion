// Package edr_telemetry implements real-time telemetry ingestion and training pipeline
package edr_telemetry

// KafkaClient is an interface for Kafka client operations
type KafkaClient interface {
	Connect() error
	ConsumeTopics(topics []string) error
	Close() error
}

// NewKafkaClient returns a new Kafka client instance
func NewKafkaClient() KafkaClient {
	return nil // Stub implementation
}

// Evidence represents captured proof of behavior
type Evidence struct {
	Type    string `json:"type"`
	Data    string `json:"data"`
	Success bool   `json:"success"`
}
