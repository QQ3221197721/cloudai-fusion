package disaster

import "time"

// SplitBrainDetector monitors for split-brain conditions in distributed clusters.
type SplitBrainDetector struct {
	// onDetection callback when split-brain is detected
	onDetection func(*SplitBrainEvidence) error
}

// NodeStatus represents the status of a node during split-brain detection.
type NodeStatus struct {
	ID             string        `json:"id"`
	NetworkLatency time.Duration `json:"network_latency"`
	IsPrimary      bool          `json:"is_primary"`
	IsReachable    bool          `json:"is_reachable"`
	LastHeartbeat  time.Time     `json:"last_heartbeat"`
}

// EvidenceChain stores cryptographic evidence of split-brain events.
type EvidenceChain struct {
	entries map[string][]byte
}

// NewEvidenceChain creates a new evidence chain.
func NewEvidenceChain() *EvidenceChain {
	return &EvidenceChain{
		entries: make(map[string][]byte),
	}
}

// AddEvidence adds evidence to the chain.
func (ec *EvidenceChain) AddEvidence(evidenceID string, data []byte) error {
	ec.entries[evidenceID] = data
	return nil
}

// DisasterManagerAdapter adapts the disaster manager for split-brain handling.
type DisasterManagerAdapter struct {
	regions []string
}

// ListRegions returns the list of configured disaster recovery regions.
func (d *DisasterManagerAdapter) ListRegions() []string {
	if d.regions == nil {
		return []string{}
	}
	return d.regions
}

// EnvironmentID represents an environment identifier.
type EnvironmentID string

// String returns the string representation of the environment ID.
func (e EnvironmentID) String() string {
	return string(e)
}

// EnvironmentConfig contains environment isolation configuration.
type EnvironmentConfig struct {
	ID            EnvironmentID `json:"id"`
	ReadOnly      bool          `json:"read_only"`
	AllowCrossEnv bool          `json:"allow_cross_env"`
	DataRetention int           `json:"data_retention"`
}

// IsolationEnforcer enforces environment isolation policies.
type IsolationEnforcer struct {
	config EnvironmentConfig
}

// NewIsolationEnforcer creates a new isolation enforcer.
func NewIsolationEnforcer(config EnvironmentConfig) *IsolationEnforcer {
	return &IsolationEnforcer{config: config}
}

// GetCurrentConfig returns the current environment configuration.
func (ie *IsolationEnforcer) GetCurrentConfig() EnvironmentConfig {
	return ie.config
}

// SplitBrainEvidence contains evidence of a split-brain event.
type SplitBrainEvidence struct {
	EvidenceID       string        `json:"evidence_id"`
	ViolationType    string        `json:"violation_type"`
	Fingerprint      string        `json:"fingerprint"`
	MerkleProof      []byte        `json:"merkle_proof"`
	MitigationAction string        `json:"mitigation_action"`
	Nodes            []*NodeStatus `json:"nodes"`
	DetectedAt       time.Time     `json:"detected_at"`
}
