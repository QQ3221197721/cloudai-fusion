// Package security - sigstore.go provides supply chain security via Sigstore/Cosign.
// Implements container image signature verification, SBOM (Software Bill of Materials)
// generation, provenance attestation, and admission policy enforcement
// for trusted image deployment.
package security

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/capability"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// Image Signature Model
// ============================================================================

// ImageSignature represents a Cosign-compatible signature for a container image.
type ImageSignature struct {
	ID          string            `json:"id"`
	ImageRef    string            `json:"image_ref"`  // registry/repo:tag
	Digest      string            `json:"digest"`     // sha256:...
	Signature   string            `json:"signature"`  // base64 encoded
	PublicKey   string            `json:"public_key"` // PEM encoded
	SignedBy    string            `json:"signed_by"`  // identity (email or OIDC subject)
	Issuer      string            `json:"issuer"`     // OIDC issuer URL
	Verified    bool              `json:"verified"`
	Annotations map[string]string `json:"annotations,omitempty"`
	SignedAt    time.Time         `json:"signed_at"`
	VerifiedAt  *time.Time        `json:"verified_at,omitempty"`
}

// ImagePolicy defines trust policy for container images.
type ImagePolicy struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Description       string   `json:"description,omitempty"`
	TrustedRegistries []string `json:"trusted_registries"` // allowed registry prefixes
	RequireSignature  bool     `json:"require_signature"`
	RequireSBOM       bool     `json:"require_sbom"`
	TrustedSigners    []string `json:"trusted_signers"`      // trusted signer identities
	TrustedIssuers    []string `json:"trusted_issuers"`      // trusted OIDC issuers
	Namespaces        []string `json:"namespaces,omitempty"` // scope (empty = all)
	Enforcement       string   `json:"enforcement"`          // enforce, warn, audit
	Enabled           bool     `json:"enabled"`
}

// ============================================================================
// SBOM Model
// ============================================================================

// SBOMFormat defines supported SBOM formats.
type SBOMFormat string

const (
	SBOMFormatSPDX      SBOMFormat = "spdx"
	SBOMFormatCycloneDX SBOMFormat = "cyclonedx"
)

// SBOM represents a Software Bill of Materials for a container image.
type SBOM struct {
	ID          string          `json:"id"`
	ImageRef    string          `json:"image_ref"`
	Digest      string          `json:"digest"`
	Format      SBOMFormat      `json:"format"`
	Components  []SBOMComponent `json:"components"`
	TotalPkgs   int             `json:"total_packages"`
	Licenses    []string        `json:"licenses"`
	GeneratedAt time.Time       `json:"generated_at"`
	GeneratedBy string          `json:"generated_by"` // tool name
}

// SBOMComponent represents a package or dependency in an SBOM.
type SBOMComponent struct {
	Name      string   `json:"name"`
	Version   string   `json:"version"`
	Type      string   `json:"type"`      // library, framework, application, os
	Ecosystem string   `json:"ecosystem"` // go, npm, pip, rpm, deb
	License   string   `json:"license,omitempty"`
	Hashes    []string `json:"hashes,omitempty"` // sha256:...
	PURL      string   `json:"purl,omitempty"`   // package URL
}

// ============================================================================
// Provenance Attestation
// ============================================================================

// ProvenanceAttestation represents a SLSA provenance attestation.
type ProvenanceAttestation struct {
	ID              string          `json:"id"`
	ImageRef        string          `json:"image_ref"`
	Digest          string          `json:"digest"`
	BuildType       string          `json:"build_type"` // e.g., "https://github.com/slsa-framework/slsa/blob/main/docs/provenance/v0.2"
	Builder         string          `json:"builder"`    // CI system
	SourceRepo      string          `json:"source_repo"`
	SourceCommit    string          `json:"source_commit"`
	SourceBranch    string          `json:"source_branch"`
	BuildInvocation string          `json:"build_invocation"` // build ID/URL
	Materials       []BuildMaterial `json:"materials"`
	SLSALevel       int             `json:"slsa_level"` // 0-4
	Verified        bool            `json:"verified"`
	CreatedAt       time.Time       `json:"created_at"`
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
	signatures   []*ImageSignature
	policies     []*ImagePolicy
	sboms        map[string]*SBOM                  // digest → SBOM
	attestations map[string]*ProvenanceAttestation // digest → attestation
	logger       *logrus.Logger
	mu           sync.RWMutex
	capOnce      sync.Once // reports the signature-verifier capability exactly once
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

// ============================================================================
// Signature Verification
// ============================================================================

// SignatureVerifyStatus represents the outcome of an ECDSA signature verification.
type SignatureVerifyStatus string

const (
	SignatureVerified   SignatureVerifyStatus = "verified"     // Cryptographically valid
	SignatureFailed     SignatureVerifyStatus = "failed"       // Valid key + sig, but crypto check failed (tampering)
	SignatureUnverified SignatureVerifyStatus = "unverified"   // Missing public key or signature material
)

// VerifySignature performs real ECDSA-P256 cryptographic verification over the image digest.
// Returns SignatureVerified when the signature cryptographically matches the digest.
// Returns SignatureFailed when tampering is detected.
// Returns SignatureUnverified when ECDSA material (public key / signature) is missing.
func VerifySignature(sig *ImageSignature) (_ SignatureVerifyStatus, _ error) {
	if sig.PublicKey == "" || sig.Signature == "" {
		return SignatureUnverified, nil
	}

	// Decode base64-encoded signature.
	sigBytes, err := base64.StdEncoding.DecodeString(sig.Signature)
	if err != nil {
		return SignatureFailed, fmt.Errorf("decode signature: %w", err)
	}

	// Parse PEM-encoded public key.
	block, _ := pem.Decode([]byte(sig.PublicKey))
	if block == nil {
		return SignatureFailed, fmt.Errorf("missing or invalid PEM in public key")
	}
	pubInterface, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return SignatureFailed, fmt.Errorf("parse public key: %w", err)
	}
	pub, ok := pubInterface.(*ecdsa.PublicKey)
	if !ok {
		return SignatureFailed, fmt.Errorf("expected ECDSA public key, got type %T", pubInterface)
	}

	// Hash the image digest as message.
	h := sha256.Sum256([]byte(sig.Digest))

	// Parse DER-encoded signature.
	type sigStruct struct {
		R, S *big.Int
	}
	var derSig sigStruct
	_, err = asn1.Unmarshal(sigBytes, &derSig)
	if err != nil || derSig.R == nil || derSig.S == nil {
		return SignatureFailed, fmt.Errorf("parse DER signature: %w", err)
	}

	// Verify ECDSA signature using crypto/ecdsa.Verify with r,s components.
	if ecdsa.Verify(pub, h[:], derSig.R, derSig.S) {
		return SignatureVerified, nil
	}
	return SignatureFailed, nil
}

// verifyStatus is the error-free projection of VerifySignature used by the batch
// path, which reports per-item status in its result slice rather than returning
// an error for a single item.
func verifyStatus(sig *ImageSignature) SignatureVerifyStatus {
	status, _ := VerifySignature(sig)
	return status
}

// BatchVerifySignatures cryptographically verifies a batch of image signatures
// and returns a status per input (index-aligned).
//
// ECDSA-P256 verification is CPU-bound (one scalar multiplication per check and
// no shared state between checks), so the batch is split into GOMAXPROCS chunks
// verified in parallel. This amortizes a fleet of admission-time signature
// checks across all cores: for B independent signatures the wall-clock cost
// drops from B·t (sequential) toward (B/P)·t with P workers, while each
// individual check remains the exact same real crypto/ecdsa.Verify call. Small
// batches (< 2·workers) fall back to a sequential pass to avoid goroutine
// scheduling overhead dominating the work.
func BatchVerifySignatures(sigs []*ImageSignature) []SignatureVerifyStatus {
	out := make([]SignatureVerifyStatus, len(sigs))
	if len(sigs) == 0 {
		return out
	}

	workers := runtime.GOMAXPROCS(0)
	if workers < 1 {
		workers = 1
	}
	// Sequential fast path for small batches: the parallel dispatch overhead is
	// not worth it below ~2 items per worker.
	if workers == 1 || len(sigs) < 2*workers {
		for i, s := range sigs {
			out[i] = verifyStatus(s)
		}
		return out
	}

	var wg sync.WaitGroup
	chunk := (len(sigs) + workers - 1) / workers
	for start := 0; start < len(sigs); start += chunk {
		end := start + chunk
		if end > len(sigs) {
			end = len(sigs)
		}
		wg.Add(1)
		go func(lo, hi int) {
			defer wg.Done()
			for i := lo; i < hi; i++ {
				out[i] = verifyStatus(sigs[i])
			}
		}(start, end)
	}
	wg.Wait()
	return out
}

// reportSignatureCapability reports the true nature of our verifier to capability registry exactly once.
// This implements honest downgrade semantics: real crypto vs flag-trust fallback.
func (m *SupplyChainManager) reportSignatureCapability(hadMaterial bool) {
	m.capOnce.Do(func() {
		mode := capability.ModeReal
		driver := "crypto/ecdsa+P-256"
		detail := "real cryptographic verification with ECDSA P-256 curves over SHA-256 digests"
		if !hadMaterial {
			mode = capability.ModeSimulated
			driver = "no-signature-materials-found"
			detail = "verification attempted but no signatures contained ECDSA material"
		}
		_ = capability.Report("security.supply_chain.signature", driver, mode, detail)
	})
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

		// Check signature.
		//
		// This path performs REAL cryptographic verification (crypto/ecdsa
		// P-256 over the image digest, with the signer's crypto/x509 public
		// key) rather than trusting a recorded boolean flag. Honest downgrade
		// rule: when a candidate signature carries no ECDSA material (public
		// key + signature), it is reported as "unverified" — never silently
		// promoted to "pass".
		if policy.RequireSignature {
			signed := false
			sawTampered := false
			sawUnverified := false
			hadMaterial := false
			for _, sig := range m.signatures {
				if sig.Digest != digest {
					continue
				}
				// Only consider signatures from trusted signers.
				signerOK := len(policy.TrustedSigners) == 0
				for _, ts := range policy.TrustedSigners {
					if sig.SignedBy == ts {
						signerOK = true
						break
					}
				}
				if !signerOK {
					continue
				}
				if sig.PublicKey != "" && sig.Signature != "" {
					hadMaterial = true
				}
				status, verr := VerifySignature(sig)
				if verr != nil {
					m.logger.WithError(verr).WithField("image", imageRef).
						Warn("image signature verification error")
				}
				switch status {
				case SignatureVerified:
					signed = true
				case SignatureFailed:
					sawTampered = true
				case SignatureUnverified:
					sawUnverified = true
				}
				if signed {
					break
				}
			}

			// Surface the true nature of the verifier (real crypto vs flag-trust
			// fallback) to the capability registry, exactly once per manager.
			m.reportSignatureCapability(hadMaterial)

			switch {
			case signed:
				result.Checks = append(result.Checks, VerifyCheck{
					Name: "signature", Status: "pass",
					Detail: "ECDSA-P256 signature cryptographically verified",
				})
			case sawTampered:
				result.Checks = append(result.Checks, VerifyCheck{
					Name:   "signature",
					Status: "fail",
					Detail: "signature present but cryptographic verification failed (possible tampering)",
				})
				if policy.Enforcement == "enforce" {
					result.Allowed = false
					result.Reason = "image signature failed cryptographic verification"
					return result, nil
				}
			case sawUnverified:
				result.Checks = append(result.Checks, VerifyCheck{
					Name:   "signature",
					Status: "unverified",
					Detail: "signature recorded without ECDSA material; cannot cryptographically verify",
				})
				if policy.Enforcement == "enforce" {
					result.Allowed = false
					result.Reason = "image signature could not be cryptographically verified"
					return result, nil
				}
			default:
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
	TotalSignatures   int `json:"total_signatures"`
	VerifiedImages    int `json:"verified_images"`
	TotalSBOMs        int `json:"total_sboms"`
	TotalAttestations int `json:"total_attestations"`
	ActivePolicies    int `json:"active_policies"`
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
		TotalSignatures:   len(m.signatures),
		VerifiedImages:    verified,
		TotalSBOMs:        len(m.sboms),
		TotalAttestations: len(m.attestations),
		ActivePolicies:    active,
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
			Description:      "Audit-only for all other namespaces",
			RequireSignature: false,
			RequireSBOM:      false,
			Enforcement:      "audit",
			Enabled:          true,
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

// signDigestECDSA creates an ECDSA-P256 signature over a digest for testing.
// This is provided as a utility for tests that need to generate valid signatures.
func signDigestECDSA(digest string) (signature string, publicKey string, privateKey *ecdsa.PrivateKey, err error) {
	// Generate a new ECDSA P-256 key pair.
	privateKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", nil, fmt.Errorf("generate key: %w", err)
	}

	// Hash the digest.
	h := sha256.Sum256([]byte(digest))

	// Sign the hash.
	r, s, err := ecdsa.Sign(rand.Reader, privateKey, h[:])
	if err != nil {
		return "", "", nil, fmt.Errorf("sign digest: %w", err)
	}

	// Encode signature as base64 (DER format).
	type sigStruct struct {
		R, S *big.Int
	}
	sigData, err := asn1.Marshal(sigStruct{R: r, S: s})
	if err != nil {
		return "", "", nil, fmt.Errorf("marshal signature: %w", err)
	}
	signature = base64.StdEncoding.EncodeToString(sigData)

	// Export public key in PEM format (PKIX).
	pubKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", nil, fmt.Errorf("marshal public key: %w", err)
	}
	pemBlock := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubKeyBytes,
	}
	publicKey = string(pem.EncodeToMemory(pemBlock))

	return signature, publicKey, privateKey, nil
}
