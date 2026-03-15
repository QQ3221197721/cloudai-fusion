// Package security - sigstore.go provides supply chain security via Sigstore/Cosign.
// Implements container image signature verification, SBOM (Software Bill of Materials)
// generation, provenance attestation, and admission policy enforcement
// for trusted image deployment.
package security

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// Image Signature Model
// ============================================================================

// ImageSignature represents a Cosign-compatible signature for a container image.
type ImageSignature struct {
	ID          string            `json:"id"`
	ImageRef    string            `json:"image_ref"`    // registry/repo:tag
	Digest      string            `json:"digest"`       // sha256:...
	Signature   string            `json:"signature"`    // base64 encoded
	PublicKey   string            `json:"public_key"`   // PEM encoded
	SignedBy    string            `json:"signed_by"`    // identity (email or OIDC subject)
	Issuer      string            `json:"issuer"`       // OIDC issuer URL
	Verified    bool              `json:"verified"`
	Annotations map[string]string `json:"annotations,omitempty"`
	SignedAt    time.Time         `json:"signed_at"`
	VerifiedAt  *time.Time        `json:"verified_at,omitempty"`
}

// ImagePolicy defines trust policy for container images.
type ImagePolicy struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Description     string   `json:"description,omitempty"`
	TrustedRegistries []string `json:"trusted_registries"` // allowed registry prefixes
	RequireSignature bool     `json:"require_signature"`
	RequireSBOM      bool     `json:"require_sbom"`
	TrustedSigners   []string `json:"trusted_signers"` // trusted signer identities
	TrustedIssuers   []string `json:"trusted_issuers"` // trusted OIDC issuers
	Namespaces       []string `json:"namespaces,omitempty"` // scope (empty = all)
	Enforcement      string   `json:"enforcement"` // enforce, warn, audit
	Enabled          bool     `json:"enabled"`
}

// ============================================================================
// SBOM Model
// ============================================================================

// SBOMFormat defines supported SBOM formats.
type SBOMFormat string

const (
	SBOMFormatSPDX     SBOMFormat = "spdx"
	SBOMFormatCycloneDX SBOMFormat = "cyclonedx"
)

// SBOM represents a Software Bill of Materials for a container image.
type SBOM struct {
	ID          string       `json:"id"`
	ImageRef    string       `json:"image_ref"`
	Digest      string       `json:"digest"`
	Format      SBOMFormat   `json:"format"`
	Components  []SBOMComponent `json:"components"`
	TotalPkgs   int          `json:"total_packages"`
	Licenses    []string     `json:"licenses"`
	GeneratedAt time.Time    `json:"generated_at"`
	GeneratedBy string       `json:"generated_by"` // tool name
}

// SBOMComponent represents a package or dependency in an SBOM.
type SBOMComponent struct {
	Name      string   `json:"name"`
	Version   string   `json:"version"`
	Type      string   `json:"type"` // library, framework, application, os
	Ecosystem string   `json:"ecosystem"` // go, npm, pip, rpm, deb
	License   string   `json:"license,omitempty"`
	Hashes    []string `json:"hashes,omitempty"` // sha256:...
	PURL      string   `json:"purl,omitempty"` // package URL
}

// ============================================================================
// Provenance Attestation
// ============================================================================

// ProvenanceAttestation represents a SLSA provenance attestation.
type ProvenanceAttestation struct {
	ID             string            `json:"id"`
	ImageRef       string            `json:"image_ref"`
	Digest         string            `json:"digest"`
	BuildType      string            `json:"build_type"`      // e.g., "https://github.com/slsa-framework/slsa/blob/main/docs/provenance/v0.2"
	Builder        string            `json:"builder"`         // CI system
	SourceRepo     string            `json:"source_repo"`
	SourceCommit   string            `json:"source_commit"`
	SourceBranch   string            `json:"source_branch"`
	BuildInvocation string           `json:"build_invocation"` // build ID/URL
	Materials      []BuildMaterial   `json:"materials"`
	SLSALevel      int              `json:"slsa_level"` // 0-4
	Verified       bool             `json:"verified"`
	CreatedAt      time.Time        `json:"created_at"`
}

// BuildMaterial represents a build input.
type BuildMaterial struct {
	URI    string `json:"uri"`
	Digest string `json:"digest"`
}

// ============================================================================
// Supply Chain Security Manager
// ============================================================================

// SupplyChainManager provides supply chain security features.
type SupplyChainManager struct {
	signatures  []*ImageSignature
	policies    []*ImagePolicy
	sboms       map[string]*SBOM   // digest → SBOM
	attestations map[string]*ProvenanceAttestation // digest → attestation
	logger      *logrus.Logger
	mu          sync.RWMutex
}

// SupplyChainConfig configures the supply chain manager.
type SupplyChainConfig struct {
	Policies []*ImagePolicy
	Logger   *logrus.Logger
}

// NewSupplyChainManager creates a new supply chain security manager.
func NewSupplyChainManager(cfg SupplyChainConfig) *SupplyChainManager {
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}
	mgr := &SupplyChainManager{
		signatures:   make([]*ImageSignature, 0),
		policies:     cfg.Policies,
		sboms:        make(map[string]*SBOM),
		attestations: make(map[string]*ProvenanceAttestation),
		logger:       cfg.Logger,
	}
	if mgr.policies == nil {
		mgr.policies = DefaultImagePolicies()
	}
	return mgr
}

// ============================================================================
// Signature Operations
// ============================================================================

// RecordSignature stores a signature for an image.
func (m *SupplyChainManager) RecordSignature(sig *ImageSignature) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sig.ID == "" {
		sig.ID = common.NewUUID()
	}
	m.signatures = append(m.signatures, sig)
	m.logger.WithFields(logrus.Fields{
		"image": sig.ImageRef, "signer": sig.SignedBy,
	}).Debug("Image signature recorded")
}

// VerifyImage checks if an image meets the trust policy requirements.
func (m *SupplyChainManager) VerifyImage(ctx context.Context, imageRef, digest, namespace string) (*ImageVerifyResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := &ImageVerifyResult{
		ImageRef:  imageRef,
		Digest:    digest,
		Namespace: namespace,
		Checks:    make([]VerifyCheck, 0),
	}

	// Find applicable policies
	for _, policy := range m.policies {
		if !policy.Enabled {
			continue
		}
		if len(policy.Namespaces) > 0 && !containsString(policy.Namespaces, namespace) {
			continue
		}

		// Check trusted registries
		registryOK := len(policy.TrustedRegistries) == 0
		for _, reg := range policy.TrustedRegistries {
			if strings.HasPrefix(imageRef, reg) {
				registryOK = true
				break
			}
		}

		if !registryOK {
			result.Checks = append(result.Checks, VerifyCheck{
				Name:   "trusted-registry",
				Status: "fail",
				Detail: fmt.Sprintf("image not from trusted registry: %s", imageRef),
			})
			if policy.Enforcement == "enforce" {
				result.Allowed = false
				result.Reason = "image not from trusted registry"
				return result, nil
			}
		} else {
			result.Checks = append(result.Checks, VerifyCheck{
				Name: "trusted-registry", Status: "pass",
			})
		}

		// Check signature
		if policy.RequireSignature {
			signed := false
			for _, sig := range m.signatures {
				if sig.Digest == digest && sig.Verified {
					// Check trusted signer
					signerOK := len(policy.TrustedSigners) == 0
					for _, ts := range policy.TrustedSigners {
						if sig.SignedBy == ts {
							signerOK = true
							break
						}
					}
					if signerOK {
						signed = true
						break
					}
				}
			}

			if !signed {
				result.Checks = append(result.Checks, VerifyCheck{
					Name:   "signature",
					Status: "fail",
					Detail: "no valid signature found from trusted signer",
				})
				if policy.Enforcement == "enforce" {
					result.Allowed = false
					result.Reason = "image signature required but not found"
					return result, nil
				}
			} else {
				result.Checks = append(result.Checks, VerifyCheck{
					Name: "signature", Status: "pass",
				})
			}
		}

		// Check SBOM
		if policy.RequireSBOM {
			_, hasSBOM := m.sboms[digest]
			if !hasSBOM {
				result.Checks = append(result.Checks, VerifyCheck{
					Name:   "sbom",
					Status: "fail",
					Detail: "SBOM required but not found",
				})
				if policy.Enforcement == "enforce" {
					result.Allowed = false
					result.Reason = "SBOM required but not found"
					return result, nil
				}
			} else {
				result.Checks = append(result.Checks, VerifyCheck{
					Name: "sbom", Status: "pass",
				})
			}
		}
	}

	result.Allowed = true
	result.Reason = "all checks passed"
	return result, nil
}

// ImageVerifyResult holds the verification outcome.
type ImageVerifyResult struct {
	ImageRef  string        `json:"image_ref"`
	Digest    string        `json:"digest"`
	Namespace string        `json:"namespace"`
	Allowed   bool          `json:"allowed"`
	Reason    string        `json:"reason"`
	Checks    []VerifyCheck `json:"checks"`
}

// VerifyCheck is a single verification check result.
type VerifyCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // pass, fail, warn
	Detail string `json:"detail,omitempty"`
}

// ============================================================================
// SBOM Operations
// ============================================================================

// RecordSBOM stores an SBOM for an image.
func (m *SupplyChainManager) RecordSBOM(sbom *SBOM) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sbom.ID == "" {
		sbom.ID = common.NewUUID()
	}
	sbom.TotalPkgs = len(sbom.Components)
	m.sboms[sbom.Digest] = sbom
}

// GetSBOM retrieves the SBOM for an image digest.
func (m *SupplyChainManager) GetSBOM(digest string) (*SBOM, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sbom, ok := m.sboms[digest]
	return sbom, ok
}

// GenerateSBOM creates a simulated SBOM for an image.
func (m *SupplyChainManager) GenerateSBOM(imageRef, digest string) *SBOM {
	hash := sha256.Sum256([]byte(imageRef + digest))
	sbom := &SBOM{
		ID:          common.NewUUID(),
		ImageRef:    imageRef,
		Digest:      digest,
		Format:      SBOMFormatCycloneDX,
		GeneratedAt: time.Now().UTC(),
		GeneratedBy: "cloudai-fusion-sbom-generator",
		Components: []SBOMComponent{
			{Name: "alpine", Version: "3.19", Type: "os", Ecosystem: "apk", License: "MIT"},
			{Name: "go", Version: "1.25.0", Type: "framework", Ecosystem: "go", License: "BSD-3-Clause"},
			{Name: "gin", Version: "1.9.1", Type: "library", Ecosystem: "go", License: "MIT",
				PURL: "pkg:golang/github.com/gin-gonic/gin@v1.9.1"},
			{Name: "logrus", Version: "1.9.3", Type: "library", Ecosystem: "go", License: "MIT",
				PURL: "pkg:golang/github.com/sirupsen/logrus@v1.9.3"},
		},
		Licenses: []string{"MIT", "BSD-3-Clause", "Apache-2.0"},
	}
	sbom.TotalPkgs = len(sbom.Components)

	// Add hash
	for i := range sbom.Components {
		sbom.Components[i].Hashes = []string{"sha256:" + hex.EncodeToString(hash[:])}
	}

	m.RecordSBOM(sbom)
	return sbom
}

// ============================================================================
// Attestation Operations
// ============================================================================

// RecordAttestation stores a provenance attestation.
func (m *SupplyChainManager) RecordAttestation(att *ProvenanceAttestation) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if att.ID == "" {
		att.ID = common.NewUUID()
	}
	m.attestations[att.Digest] = att
}

// GetAttestation retrieves provenance attestation for an image.
func (m *SupplyChainManager) GetAttestation(digest string) (*ProvenanceAttestation, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	att, ok := m.attestations[digest]
	return att, ok
}

// ============================================================================
// Policy Management
// ============================================================================

// AddPolicy adds an image trust policy.
func (m *SupplyChainManager) AddPolicy(policy *ImagePolicy) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if policy.ID == "" {
		policy.ID = common.NewUUID()
	}
	m.policies = append(m.policies, policy)
}

// ListPolicies returns all image policies.
func (m *SupplyChainManager) ListPolicies() []*ImagePolicy {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]*ImagePolicy, len(m.policies))
	copy(result, m.policies)
	return result
}

// ============================================================================
// Status
// ============================================================================

// SupplyChainStatus reports supply chain security status.
type SupplyChainStatus struct {
	TotalSignatures  int `json:"total_signatures"`
	VerifiedImages   int `json:"verified_images"`
	TotalSBOMs       int `json:"total_sboms"`
	TotalAttestations int `json:"total_attestations"`
	ActivePolicies   int `json:"active_policies"`
}

// Status returns the current supply chain security status.
func (m *SupplyChainManager) Status() SupplyChainStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	verified := 0
	for _, s := range m.signatures {
		if s.Verified {
			verified++
		}
	}
	active := 0
	for _, p := range m.policies {
		if p.Enabled {
			active++
		}
	}

	return SupplyChainStatus{
		TotalSignatures:  len(m.signatures),
		VerifiedImages:   verified,
		TotalSBOMs:       len(m.sboms),
		TotalAttestations: len(m.attestations),
		ActivePolicies:   active,
	}
}

// ============================================================================
// Defaults
// ============================================================================

// DefaultImagePolicies returns sensible default image trust policies.
func DefaultImagePolicies() []*ImagePolicy {
	return []*ImagePolicy{
		{
			ID: "policy-production", Name: "Production Image Policy",
			Description:       "Strict policy for production namespaces",
			TrustedRegistries: []string{"ghcr.io/cloudai-fusion/", "registry.cloudai.io/"},
			RequireSignature:  true,
			RequireSBOM:       true,
			Namespaces:        []string{"production", "cloudai-fusion"},
			Enforcement:       "enforce",
			Enabled:           true,
		},
		{
			ID: "policy-staging", Name: "Staging Image Policy",
			Description:       "Warning-only policy for staging",
			TrustedRegistries: []string{"ghcr.io/", "docker.io/"},
			RequireSignature:  true,
			RequireSBOM:       false,
			Namespaces:        []string{"staging"},
			Enforcement:       "warn",
			Enabled:           true,
		},
		{
			ID: "policy-default", Name: "Default Image Policy",
			Description:       "Audit-only for all other namespaces",
			RequireSignature:  false,
			RequireSBOM:       false,
			Enforcement:       "audit",
			Enabled:           true,
		},
	}
}

// ============================================================================
// Helpers
// ============================================================================

func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
