// Package edr_telemetry implements real-time telemetry ingestion and training pipeline
package edr_telemetry

import "github.com/sirupsen/logrus"

type TelemetryEvent struct {
	Timestamp      time.Time   `json:"timestamp"`
	EventID        string      `json:"event_id"`
	EventType      string      `json:"event_type"`
	SourcePID      uint32      `json:"source_pid"`
	DestinationPID uint32      `json:"destination_pid"`
	Evidence       []Evidence  `json:"evidence"`
	RiskScore      float64     `json:"risk_score"`
}

type BehaviorAnalysis struct {
	RiskScore    float64   `json:"risk_score"`
	RiskLevel    RiskLevel `json:"risk_level"`
	PredictedTID string    `json:"predicted_tid,omitempty"`
}

type RiskLevel string

const (
	Critical RiskLevel = "critical"
	High     RiskLevel = "high"
	Medium   RiskLevel = "medium"
	Low      RiskLevel = "low"
	Unknown  RiskLevel = "unknown"
)

type Evidence struct {
	Type    string `json:"type"`
	Data    string `json:"data"`
	Success bool   `json:"success"`
}
