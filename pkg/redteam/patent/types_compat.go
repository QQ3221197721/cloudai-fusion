// Package patent provides type compatibility for M34 Red Team Platform integration.
package patent

// ============================================================================
// COMPATIBILITY TYPES FOR CROSS-PATENT COMMUNICATION
// ============================================================================

// TargetInfo represents target system info (used across patents)
type TargetInfo struct {
	IP        string   `json:"ip"`
	Hostname  string   `json:"hostname,omitempty"`
	Ports     []int    `json:"ports,omitempty"`
	Services  []string `json:"services,omitempty"`
	KnownCVEs []string `json:"known_cves,omitempty"`
}

// VulnerabilityProfile is used by quantum-resistant predictor
type VulnerabilityProfile struct {
	ID            string   `json:"id"`
	FeatureVector []float64 `json:"feature_vector"`
	CVE           string   `json:"cve,omitempty"`
}

// AdversarialInput is used by adversarial ML defense
type AdversarialInput struct {
	ActionSequence []Action  `json:"action_sequence"`
	ContextMetrics map[string]float64 `json:"context_metrics"`
}

// DetectorType defines defensive mechanism category
type DetectorType string

const (
	MLClassifierDetector      DetectorType = "ml_classifier"
	AnomalyDetector           DetectorType = "anomaly_detector"
	SignatureBasedDetector    DetectorType = "signature_based"
	HybridDefenseSystem       DetectorType = "hybrid_defense"
)

// Result represents adversarial evaluation output
type Result struct {
	EvasionProbability float64 `json:"evasion_probability"`
	ConfidenceScore    float64 `json:"confidence_score"`
	DetectionTimeMs    int64   `json:"detection_time_ms"`
}
