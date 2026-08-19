package edge

import "time"

// Logger is a common logging interface for the edge package.
type Logger interface {
	Infof(format string, args ...interface{})
	Errorf(format string, args ...interface{})
	Debugf(format string, args ...interface{})
	Warnf(format string, args ...interface{})
}

// DeltaMetrics tracks delta sync metrics.
type DeltaMetrics struct {
	NodesRegistered   int
	SyncsStarted     int
	SyncsCompleted   int
	TotalBytesSync   int64
}

// NewDeltaMetrics creates a new DeltaMetrics instance.
func NewDeltaMetrics() *DeltaMetrics {
	return &DeltaMetrics{}
}

// RecordNodeRegistered records a node registration event.
func (dm *DeltaMetrics) RecordNodeRegistered(nodeID string) {
	dm.NodesRegistered++
}

// RecordSyncStarted records a sync start event.
func (dm *DeltaMetrics) RecordSyncStarted(sessionID string) {
	dm.SyncsStarted++
}

// RecordSyncCompleted records a sync completion event.
func (dm *DeltaMetrics) RecordSyncCompleted(sessionID string, duration time.Duration) {
	dm.SyncsCompleted++
}

// AIModel represents an AI model for edge hardware optimization.
type AIModel struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Framework   string            `json:"framework"`
	SizeBytes   int64             `json:"size_bytes"`
	Precision   string            `json:"precision"`
	Parameters  int64             `json:"parameters"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// QuantizationResult represents the result of model quantization.
type QuantizationResult struct {
	ModelID        string  `json:"model_id"`
	OriginalSize   int64   `json:"original_size"`
	QuantizedSize  int64   `json:"quantized_size"`
	Precision      string  `json:"precision"`
	SpeedupFactor  float64 `json:"speedup_factor"`
	AccuracyLoss   float64 `json:"accuracy_loss"`
}

// Duration returns the duration of a sync session.
func (s *SyncSession) Duration() time.Duration {
	if s.EndTime.IsZero() {
		return time.Since(s.StartTime)
	}
	return s.EndTime.Sub(s.StartTime)
}
