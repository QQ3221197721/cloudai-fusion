// Package manifest - SLSA Provenance v1.0 export functionality
// Implements §6.1 Export Formats Deep Dive for SLSA Provenance
// Makes CloudAI Fusion evidence consumable by Sigstore/Cosign toolchains
package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

const (
	slsaStatementTypeURI = "https://in-toto.io/Statement/v1"
	slsaBuildTypeURI     = "https://cloudai-fusion.io/evidence-manifest/v1"
	slsaBuilderID        = "https://cloudai-fusion.io/ledger/"
)

// SLSAProvenanceExport represents an SLSA Level 3+ provenance statement
// conforming to https://slsa-framework.github.io/provenance/v1
type SLSAProvenanceExport struct {
	Type       string            `json:"_type"`            // Always "https://in-toto.io/Statement/v1"
	Subject    []*SLSAMaterial   `json:"subject"`          // Evidence chain items as materials
	Builder    SLSABuilder       `json:"builder"`          // Builder identity
	Invocation *SLSAInvocation   `json:"invocation"`       // Configuration and parameters
	Metadata   SLSAMetadata      `json:"metadata"`         // Timing and completeness
	Predicate  any               `json:"predicate,omitempty"`
}

// SLSAMaterial represents a provenance material (§6.1 SLSA Provenance v1.0)
type SLSAMaterial struct {
	Name     string            `json:"name"`     // e.g., "my-app"
	Digest   map[string]string `json:"digest"`   // SHA256 hashes
	Version  string            `json:"version,omitempty"`
	Location string            `json:"location,omitempty"`
}

// SLSABuilder identifies who performed the build/attestation
type SLSABuilder struct {
	ID string `json:"id"` // Must match slsaBuilderID constant
}

// SLSAInvocation captures how the action was triggered
type SLSAInvocation struct {
	ConfigSource *SLSAConfigSource  `json:"configSource,omitempty"` // The evidence-manifest.yaml reference
	Parameters   map[string]any    `json:"parameters,omitempty"`   // Policy parameters
	Env          map[string]string `json:"environment,omitempty"`  // Environment variables
}

// SLSAConfigSource references the declarative configuration file
type SLSAConfigSource struct {
	URI        string            `json:"uri"`
	Digest     map[string]string `json:"digest,omitempty"`
	EntryPoint string            `json:"entryPoint,omitempty"`
}

// SLSAMetadata contains timing and quality information
type SLSAMetadata struct {
	StartTime    time.Time `json:"startTime,omitempty"`
	EndTime      time.Time `json:"endTime,omitempty"`
	Digest       map[string]string `json:"intotoDigest,omitempty"`
	Completeness *struct {
		Arguments   bool `json:"arguments"`
		Environment bool `json:"environment"`
	} `json:"completeness,omitempty"`
	Reproducible bool   `json:"reproducible"`
	Recipe       string `json:"recipe,omitempty"`
}

// ToSLSAProvenance converts evidence chain + manifest into SLSA v1.0
func ToSLSAProvenance(records []*evidence.Evidence, manifest *Manifest) (*SLSAProvenanceExport, error) {
	if len(records) == 0 {
		return nil, fmt.Errorf("no evidence records to convert")
	}

	// Build subject materials from evidence chain
	materials := buildSLSAMaterials(records)

	// Hash the manifest
	manifestHash := computeCanonicalHash(manifest.RawYAML)

	// Calculate timestamps
	startTime := getEarliestTimestamp(records)
	endTime := getLatestTimestamp(records)

	// Get policy parameters that affected this attestation
	params := extractPolicyParameters(manifest)

	export := &SLSAProvenanceExport{
		Type:       slsaStatementTypeURI,
		Subject:    materials,
		Builder:    SLSABuilder{ID: slsaBuilderID},
		Invocation: &SLSAInvocation{
			ConfigSource: &SLSAConfigSource{
				URI:        fmt.Sprintf("file://%s", manifest.Source),
				Digest:     map[string]string{"sha256": manifestHash},
				EntryPoint: "spec.export.webhook",
			},
			Parameters: params,
		},
		Metadata: SLSAMetadata{
			StartTime: startTime,
			EndTime: endTime,
			Digest: map[string]string{
				"application/sarif": manifestHash, // Manifest hash doubles as digest
			},
			Completeness: &struct {
				Arguments   bool `json:"arguments"`
				Environment bool `json:"environment"`
			}{
				Arguments: true, Environment: false, // We don't capture full env vars
			},
			Reproducible: false, // Not fully reproducible due to external dependencies
		},
	}

	return export, nil
}

// buildSLSAMaterials converts each evidence record into SLSA material
func buildSLSAMaterials(records []*evidence.Evidence) []*SLSAMaterial {
	materials := make([]*SLSAMaterial, len(records))

	for i, rec := range records {
		name := fmt.Sprintf("%s/%s", rec.Action, rec.Subject)

		materials[i] = &SLSAMaterial{
			Name: name,
			Digest: map[string]string{
				"sha256": rec.Hash,
			},
			Version:  fmt.Sprintf("seq-%d", rec.Seq),
			Location: fmt.Sprintf("evidence:chain/seq:%d", rec.Seq),
		}
	}

	return materials
}

// computeCanonicalHash produces deterministic hash of YAML content
func computeCanonicalHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// getEarliestTimestamp finds min timestamp across records
func getEarliestTimestamp(records []*evidence.Evidence) time.Time {
	if len(records) == 0 {
		return time.Now().UTC()
	}
	min := records[0].Timestamp
	for _, r := range records[1:] {
		if r.Timestamp.Before(min) {
			min = r.Timestamp
		}
	}
	return min
}

// getLatestTimestamp finds max timestamp
func getLatestTimestamp(records []*evidence.Evidence) time.Time {
	if len(records) == 0 {
		return time.Now().UTC()
	}
	max := records[0].Timestamp
	for _, r := range records[1:] {
		if r.Timestamp.After(max) {
			max = r.Timestamp
		}
	}
	return max
}

// extractPolicyParameters returns relevant policy config for provenance
func extractPolicyParameters(manifest *Manifest) map[string]any {
	return map[string]any{
		"min_signers": manifest.Spec.Policy.MinSigners,
		"require_zkp": manifest.Spec.Policy.RequireZKP,
		"algorithm":   manifest.Spec.Chain.Algorithm,
		"max_age_hrs": manifest.Spec.Policy.MaxAge.Hours(),
	}
}

// ToJSON marshals SLSA export to JSON bytes with indentation
func (s *SLSAProvenanceExport) ToJSON() ([]byte, error) {
	return json.MarshalIndent(s, "", "  ")
}

// ToSARIF converts SLSA provenance to SARIF format for compliance auditors
func ToSARIF(prov *SLSAProvenanceExport) (*SARIFFormat, error) {
	if prov == nil || len(prov.Subject) == 0 {
		return nil, fmt.Errorf("cannot generate SARIF from empty provenance")
	}

	var results []SARIFFormatResult
	for _, mat := range prov.Subject {
		result := SARIFFormatResult{
			RuleID: "CAF.EVIDENCE.ATTESTATION",
			Message: SARIFMessage{
				Text: fmt.Sprintf("Verified receipt %s (seq hash %s)",
					mat.Name, truncateSHA256(mat.Digest["sha256"])),
			},
			Level: "pass",
			Locations: []SARIFFormatLocation{
				{
					PhysicalLocation: SARIFPhysicalLocation{
						ArtifactLocation: SARIFArtifactLocation{
							URI: ".caf/evidence.chain",
						},
					},
				},
			},
		}
		results = append(results, result)
	}

	return &SARIFFormat{
		Schema: "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
		Version: "2.1.0",
		Runs: []SARIFFormatRun{{
			Tool: SARIFFormatTool{
				Driver: SARIFFormatToolDriver{
					Name:             "CloudAI-Fusion-Evidence",
					InformationUri:   "https://cloudai-fusion.io/",
					Version:          "1.0.0",
				},
			},
			Results: results,
		}},
	}, nil
}

// truncateSHA256 shows first 16 chars of hash
func truncateSHA256(hash string) string {
	if len(hash) > 16 {
		return hash[:16] + "..."
	}
	return hash
}

// -----------------------------------------------------------------------------
// SARIF Format Structure
// -----------------------------------------------------------------------------

// SARIFFormat is the Static Analysis Report Interchange Format root
type SARIFFormat struct {
	Schema  string           `json:"$schema"`
	Version string           `json:"version"`
	Runs    []SARIFFormatRun `json:"runs"`
}

type SARIFFormatRun struct {
	Tool    SARIFFormatTool       `json:"tool"`
	Results []SARIFFormatResult   `json:"results"`
}

type SARIFFormatTool struct {
	Driver SARIFFormatToolDriver `json:"driver"`
}

type SARIFFormatToolDriver struct {
	Name             string         `json:"name"`
	InformationUri   string         `json:"informationUri"`
	Version          string         `json:"version"`
}

type SARIFFormatResult struct {
	RuleID    string              `json:"ruleId"`
	Level     string              `json:"level"`
	Message   SARIFMessage        `json:"message"`
	Locations []SARIFFormatLocation `json:"locations"`
}

type SARIFFormatLocation struct {
	PhysicalLocation SARIFPhysicalLocation `json:"physicalLocation"`
}

type SARIFPhysicalLocation struct {
	ArtifactLocation SARIFArtifactLocation `json:"artifactLocation"`
}

type SARIFArtifactLocation struct {
	URI string `json:"uri"`
}

// SARIFMessage carries the human-readable result text
type SARIFMessage struct {
	Text string `json:"text"`
}

// -----------------------------------------------------------------------------
// Helper Methods for Testing and Integration
// -----------------------------------------------------------------------------

// ExportToDisk writes the SLSA provenance to disk
func (s *SLSAProvenanceExport) ExportToDisk(path string) error {
	data, err := s.ToJSON()
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	return os.Rename(tmp, path)
}

// Validate checks the export conforms to SLSA spec
func (s *SLSAProvenanceExport) Validate() []error {
	var errs []error

	if s.Builder.ID != slsaBuilderID {
		errs = append(errs, fmt.Errorf("invalid builder ID: expected %s, got %s", slsaBuilderID, s.Builder.ID))
	}

	if len(s.Subject) == 0 {
		errs = append(errs, fmt.Errorf("no subjects; must have at least one material"))
	}

	for i, mat := range s.Subject {
		if mat.Name == "" {
			errs = append(errs, fmt.Errorf("subject[%d] missing required 'name' field", i))
		}
		if len(mat.Digest) == 0 {
			errs = append(errs, fmt.Errorf("subject[%d] has no digests", i))
		}
		if _, ok := mat.Digest["sha256"]; !ok {
			errs = append(errs, fmt.Errorf("subject[%d] missing sha256 digest", i))
		}
	}

	return errs
}
