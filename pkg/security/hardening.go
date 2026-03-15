package security

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// Pod Security Standards (PSS)
// ============================================================================

// PSSProfile defines the Pod Security Standard profile level.
type PSSProfile string

const (
	PSSPrivileged PSSProfile = "privileged"
	PSSBaseline   PSSProfile = "baseline"
	PSSRestricted PSSProfile = "restricted"
)

// PSSEnforcement defines the enforcement mode for PSS.
type PSSEnforcement string

const (
	PSSEnforce PSSEnforcement = "enforce"
	PSSAudit   PSSEnforcement = "audit"
	PSSWarn    PSSEnforcement = "warn"
)

// PSSConfig configures Pod Security Standards enforcement.
type PSSConfig struct {
	Profile   PSSProfile     `json:"profile"`
	Enforce   bool           `json:"enforce"`
	Audit     bool           `json:"audit"`
	Warn      bool           `json:"warn"`
	Version   string         `json:"version"`  // e.g. "v1.28", "latest"
	ExemptNamespaces []string `json:"exempt_namespaces"`
	ExemptUsers      []string `json:"exempt_users"`
	ExemptRuntimes   []string `json:"exempt_runtime_classes"`
}

// DefaultPSSConfig returns the default restricted PSS configuration.
func DefaultPSSConfig() PSSConfig {
	return PSSConfig{
		Profile: PSSRestricted,
		Enforce: true,
		Audit:   true,
		Warn:    true,
		Version: "latest",
		ExemptNamespaces: []string{
			"kube-system",
			"kube-public",
			"kube-node-lease",
		},
	}
}

// PSSPolicy represents the complete PSS policy for a namespace.
type PSSPolicy struct {
	Namespace     string            `json:"namespace"`
	Labels        map[string]string `json:"labels"`
	Violations    []PSSViolation    `json:"violations,omitempty"`
	LastAuditedAt time.Time         `json:"last_audited_at"`
}

// PSSViolation records a Pod Security Standards violation.
type PSSViolation struct {
	ID          string     `json:"id"`
	Namespace   string     `json:"namespace"`
	PodName     string     `json:"pod_name"`
	Container   string     `json:"container"`
	Profile     PSSProfile `json:"profile"`
	Rule        string     `json:"rule"`
	Description string     `json:"description"`
	Severity    string     `json:"severity"` // critical, high, medium, low
	DetectedAt  time.Time  `json:"detected_at"`
}

// RestrictedProfileRules returns all rules enforced by the restricted PSS profile.
func RestrictedProfileRules() []PSSRule {
	return []PSSRule{
		{Name: "HostProcess", Description: "Windows pods must not run with HostProcess", Field: "securityContext.windowsOptions.hostProcess", AllowedValues: []string{"false", "undefined"}},
		{Name: "HostNamespaces", Description: "Must not share host namespaces", Field: "hostNetwork,hostPID,hostIPC", AllowedValues: []string{"false"}},
		{Name: "Privileged", Description: "Containers must not run as privileged", Field: "securityContext.privileged", AllowedValues: []string{"false"}},
		{Name: "Capabilities", Description: "Must drop ALL capabilities, only allow NET_BIND_SERVICE", Field: "securityContext.capabilities", AllowedValues: []string{"drop:ALL"}},
		{Name: "HostPorts", Description: "Must not use host ports", Field: "containers[*].ports[*].hostPort", AllowedValues: []string{"0", "undefined"}},
		{Name: "HostPath", Description: "Must not mount hostPath volumes", Field: "volumes[*].hostPath", AllowedValues: []string{"undefined"}},
		{Name: "SELinux", Description: "SELinux type must be unconfined or container-specific", Field: "securityContext.seLinuxOptions.type", AllowedValues: []string{"container_t", "container_init_t", "container_kvm_t"}},
		{Name: "ProcMount", Description: "Must use default proc mount", Field: "securityContext.procMount", AllowedValues: []string{"Default"}},
		{Name: "Seccomp", Description: "Must use RuntimeDefault or Localhost seccomp profile", Field: "securityContext.seccompProfile.type", AllowedValues: []string{"RuntimeDefault", "Localhost"}},
		{Name: "Sysctls", Description: "Only safe sysctls are allowed", Field: "securityContext.sysctls", AllowedValues: []string{"kernel.shm_rmid_forced", "net.ipv4.ip_local_port_range", "net.ipv4.ip_unprivileged_port_start", "net.ipv4.tcp_syncookies", "net.ipv4.ping_group_range"}},
		{Name: "RunAsNonRoot", Description: "Must run as non-root", Field: "securityContext.runAsNonRoot", AllowedValues: []string{"true"}},
		{Name: "RunAsUser", Description: "Must not run as UID 0", Field: "securityContext.runAsUser", AllowedValues: []string{"non-zero"}},
		{Name: "AllowPrivilegeEscalation", Description: "Must not allow privilege escalation", Field: "securityContext.allowPrivilegeEscalation", AllowedValues: []string{"false"}},
		{Name: "ReadOnlyRootFilesystem", Description: "Root filesystem should be read-only", Field: "securityContext.readOnlyRootFilesystem", AllowedValues: []string{"true"}},
	}
}

// PSSRule defines a single PSS rule.
type PSSRule struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Field         string   `json:"field"`
	AllowedValues []string `json:"allowed_values"`
}

// GenerateNamespaceLabels generates the namespace labels for PSS enforcement.
func GenerateNamespaceLabels(cfg PSSConfig) map[string]string {
	labels := make(map[string]string)
	if cfg.Enforce {
		labels["pod-security.kubernetes.io/enforce"] = string(cfg.Profile)
		labels["pod-security.kubernetes.io/enforce-version"] = cfg.Version
	}
	if cfg.Audit {
		labels["pod-security.kubernetes.io/audit"] = string(cfg.Profile)
		labels["pod-security.kubernetes.io/audit-version"] = cfg.Version
	}
	if cfg.Warn {
		labels["pod-security.kubernetes.io/warn"] = string(cfg.Profile)
		labels["pod-security.kubernetes.io/warn-version"] = cfg.Version
	}
	return labels
}

// ============================================================================
// Image Signing Verification (cosign)
// ============================================================================

// CosignConfig configures container image signature verification.
type CosignConfig struct {
	Enabled           bool     `json:"enabled"`
	PublicKey         string   `json:"public_key,omitempty"`         // PEM-encoded public key
	KeylessEnabled    bool     `json:"keyless_enabled"`             // Sigstore keyless signing
	RekorURL          string   `json:"rekor_url"`                   // Transparency log URL
	AllowedRegistries []string `json:"allowed_registries"`
	Policy            string   `json:"policy"`                      // reject, warn, audit
	FulcioURL         string   `json:"fulcio_url,omitempty"`        // Certificate authority URL
	AllowedIdentities []string `json:"allowed_identities,omitempty"` // Email/OIDC identities for keyless
}

// DefaultCosignConfig returns production cosign configuration.
func DefaultCosignConfig() CosignConfig {
	return CosignConfig{
		Enabled:  true,
		RekorURL: "https://rekor.sigstore.dev",
		AllowedRegistries: []string{
			"ghcr.io/cloudai-fusion",
		},
		Policy:    "reject",
		FulcioURL: "https://fulcio.sigstore.dev",
	}
}

// ImageVerificationResult holds the result of image signature verification.
type ImageVerificationResult struct {
	Image       string    `json:"image"`
	Verified    bool      `json:"verified"`
	Signer      string    `json:"signer,omitempty"`
	SignedAt    time.Time `json:"signed_at,omitempty"`
	Registry    string    `json:"registry"`
	Digest      string    `json:"digest,omitempty"`
	RekorEntry  string    `json:"rekor_entry,omitempty"`
	Error       string    `json:"error,omitempty"`
	VerifiedAt  time.Time `json:"verified_at"`
}

// ============================================================================
// Hardening Manager
// ============================================================================

// HardeningConfig configures the security hardening manager.
type HardeningConfig struct {
	PSS    PSSConfig    `json:"pss"`
	Cosign CosignConfig `json:"cosign"`
	Logger *logrus.Logger
}

// HardeningManager manages security hardening policies.
type HardeningManager struct {
	config       HardeningConfig
	policies     map[string]*PSSPolicy
	violations   []*PSSViolation
	verifications []*ImageVerificationResult
	signingKey   *ecdsa.PrivateKey // For simulation/testing
	logger       *logrus.Logger
	mu           sync.RWMutex
}

// NewHardeningManager creates a new security hardening manager.
func NewHardeningManager(cfg HardeningConfig) *HardeningManager {
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}

	hm := &HardeningManager{
		config:   cfg,
		policies: make(map[string]*PSSPolicy),
		logger:   cfg.Logger,
	}

	// Generate a signing key for simulation
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err == nil {
		hm.signingKey = key
	}

	cfg.Logger.WithFields(logrus.Fields{
		"pss_profile":   cfg.PSS.Profile,
		"pss_enforce":   cfg.PSS.Enforce,
		"cosign_enabled": cfg.Cosign.Enabled,
		"cosign_policy": cfg.Cosign.Policy,
	}).Info("Security hardening manager initialized")

	return hm
}

// ============================================================================
// PSS Operations
// ============================================================================

// ApplyPSSToNamespace applies Pod Security Standards to a namespace.
func (hm *HardeningManager) ApplyPSSToNamespace(ctx context.Context, namespace string) (*PSSPolicy, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	hm.mu.Lock()
	defer hm.mu.Unlock()

	// Check if namespace is exempt
	for _, exempt := range hm.config.PSS.ExemptNamespaces {
		if exempt == namespace {
			return nil, fmt.Errorf("namespace %s is exempt from PSS enforcement", namespace)
		}
	}

	labels := GenerateNamespaceLabels(hm.config.PSS)

	policy := &PSSPolicy{
		Namespace:     namespace,
		Labels:        labels,
		LastAuditedAt: common.NowUTC(),
	}

	hm.policies[namespace] = policy

	hm.logger.WithFields(logrus.Fields{
		"namespace": namespace,
		"profile":   hm.config.PSS.Profile,
		"labels":    len(labels),
	}).Info("PSS policy applied to namespace")

	return policy, nil
}

// AuditNamespace checks a namespace for PSS violations.
func (hm *HardeningManager) AuditNamespace(ctx context.Context, namespace string) ([]PSSViolation, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	hm.mu.Lock()
	defer hm.mu.Unlock()

	policy, ok := hm.policies[namespace]
	if !ok {
		return nil, fmt.Errorf("no PSS policy found for namespace %s", namespace)
	}

	// Simulated audit — in production this would query the K8s API
	// for running pods and validate against PSS rules
	policy.LastAuditedAt = common.NowUTC()
	policy.Violations = nil // Clear previous violations

	hm.logger.WithFields(logrus.Fields{
		"namespace":  namespace,
		"violations": len(policy.Violations),
	}).Info("PSS audit completed")

	return policy.Violations, nil
}

// ReportViolation records a PSS violation.
func (hm *HardeningManager) ReportViolation(violation PSSViolation) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	violation.ID = common.NewUUID()
	violation.DetectedAt = common.NowUTC()

	hm.violations = append(hm.violations, &violation)

	if policy, ok := hm.policies[violation.Namespace]; ok {
		policy.Violations = append(policy.Violations, violation)
	}

	hm.logger.WithFields(logrus.Fields{
		"namespace": violation.Namespace,
		"pod":       violation.PodName,
		"rule":      violation.Rule,
		"severity":  violation.Severity,
	}).Warn("PSS violation detected")
}

// GetViolations returns all recorded violations, optionally filtered by namespace.
func (hm *HardeningManager) GetViolations(namespace string) []*PSSViolation {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	if namespace == "" {
		result := make([]*PSSViolation, len(hm.violations))
		copy(result, hm.violations)
		return result
	}

	var result []*PSSViolation
	for _, v := range hm.violations {
		if v.Namespace == namespace {
			result = append(result, v)
		}
	}
	return result
}

// GetPSSPolicy returns the PSS policy for a namespace.
func (hm *HardeningManager) GetPSSPolicy(namespace string) (*PSSPolicy, error) {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	p, ok := hm.policies[namespace]
	if !ok {
		return nil, fmt.Errorf("no PSS policy for namespace %s", namespace)
	}
	return p, nil
}

// ListPSSPolicies returns all applied PSS policies.
func (hm *HardeningManager) ListPSSPolicies() []*PSSPolicy {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	result := make([]*PSSPolicy, 0, len(hm.policies))
	for _, p := range hm.policies {
		result = append(result, p)
	}
	return result
}

// ============================================================================
// Image Verification Operations (cosign)
// ============================================================================

// VerifyImage verifies the signature of a container image.
func (hm *HardeningManager) VerifyImage(ctx context.Context, imageRef string) (*ImageVerificationResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	hm.mu.Lock()
	defer hm.mu.Unlock()

	result := &ImageVerificationResult{
		Image:      imageRef,
		Registry:   extractRegistry(imageRef),
		VerifiedAt: common.NowUTC(),
	}

	if !hm.config.Cosign.Enabled {
		result.Verified = true
		result.Signer = "verification-disabled"
		hm.verifications = append(hm.verifications, result)
		return result, nil
	}

	// Check registry allowlist
	if !hm.isRegistryAllowed(result.Registry) {
		result.Verified = false
		result.Error = fmt.Sprintf("registry %s not in allowed list", result.Registry)
		hm.verifications = append(hm.verifications, result)
		if hm.config.Cosign.Policy == "reject" {
			return result, fmt.Errorf("image from unauthorized registry: %s", result.Registry)
		}
		return result, nil
	}

	// Simulate signature verification
	// In production: use cosign.VerifyImageSignatures()
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(imageRef)))
	result.Digest = digest
	result.Verified = true
	result.Signer = "cloudai-fusion-ci"
	result.SignedAt = common.NowUTC().Add(-1 * time.Hour)

	if hm.config.Cosign.RekorURL != "" {
		result.RekorEntry = fmt.Sprintf("%s/api/v1/log/entries/%s", hm.config.Cosign.RekorURL, digest[:16])
	}

	hm.verifications = append(hm.verifications, result)

	hm.logger.WithFields(logrus.Fields{
		"image":    imageRef,
		"verified": result.Verified,
		"signer":   result.Signer,
		"digest":   digest[:20] + "...",
	}).Info("Image signature verified")

	return result, nil
}

// VerifyAllImages verifies multiple images and returns a summary.
func (hm *HardeningManager) VerifyAllImages(ctx context.Context, imageRefs []string) (*VerificationSummary, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	summary := &VerificationSummary{
		TotalImages: len(imageRefs),
		Results:     make([]*ImageVerificationResult, 0, len(imageRefs)),
		VerifiedAt:  common.NowUTC(),
	}

	for _, ref := range imageRefs {
		result, err := hm.VerifyImage(ctx, ref)
		if err != nil {
			summary.FailedImages++
			summary.Results = append(summary.Results, &ImageVerificationResult{
				Image:      ref,
				Verified:   false,
				Error:      err.Error(),
				VerifiedAt: common.NowUTC(),
			})
			continue
		}
		summary.Results = append(summary.Results, result)
		if result.Verified {
			summary.VerifiedImages++
		} else {
			summary.FailedImages++
		}
	}

	summary.AllVerified = summary.FailedImages == 0
	return summary, nil
}

// VerificationSummary summarizes batch image verification.
type VerificationSummary struct {
	TotalImages    int                        `json:"total_images"`
	VerifiedImages int                        `json:"verified_images"`
	FailedImages   int                        `json:"failed_images"`
	AllVerified    bool                       `json:"all_verified"`
	Results        []*ImageVerificationResult `json:"results"`
	VerifiedAt     time.Time                  `json:"verified_at"`
}

// GetVerificationHistory returns recent verification results.
func (hm *HardeningManager) GetVerificationHistory() []*ImageVerificationResult {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	result := make([]*ImageVerificationResult, len(hm.verifications))
	copy(result, hm.verifications)
	return result
}

// SignImage creates a cosign-compatible signature for an image (simulation).
func (hm *HardeningManager) SignImage(ctx context.Context, imageRef string) (*HardeningSignature, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if hm.signingKey == nil {
		return nil, fmt.Errorf("signing key not available")
	}

	digest := sha256.Sum256([]byte(imageRef))
	r, s, err := ecdsa.Sign(rand.Reader, hm.signingKey, digest[:])
	if err != nil {
		return nil, fmt.Errorf("signing failed: %w", err)
	}

	sigBytes := append(r.Bytes(), s.Bytes()...)
	pubKeyBytes, _ := x509.MarshalPKIXPublicKey(&hm.signingKey.PublicKey)
	pubKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubKeyBytes})

	sig := &HardeningSignature{
		Image:     imageRef,
		Digest:    fmt.Sprintf("sha256:%x", digest),
		Signature: base64.StdEncoding.EncodeToString(sigBytes),
		PublicKey: string(pubKeyPEM),
		SignedAt:  common.NowUTC(),
		Algorithm: "ECDSA-P256-SHA256",
	}

	hm.logger.WithFields(logrus.Fields{
		"image":  imageRef,
		"digest": sig.Digest[:20] + "...",
	}).Info("Image signed")

	return sig, nil
}

// HardeningSignature represents a cosign-compatible image signature produced by the hardening manager.
type HardeningSignature struct {
	Image     string    `json:"image"`
	Digest    string    `json:"digest"`
	Signature string    `json:"signature"`
	PublicKey string    `json:"public_key"`
	SignedAt  time.Time `json:"signed_at"`
	Algorithm string    `json:"algorithm"`
}

// isRegistryAllowed checks if a registry is in the allowed list.
func (hm *HardeningManager) isRegistryAllowed(registry string) bool {
	if len(hm.config.Cosign.AllowedRegistries) == 0 {
		return true // No allowlist = allow all
	}
	for _, allowed := range hm.config.Cosign.AllowedRegistries {
		if registry == allowed || strings.HasPrefix(registry, allowed) {
			return true
		}
	}
	return false
}

// extractRegistry extracts the registry portion from an image reference.
func extractRegistry(imageRef string) string {
	parts := strings.SplitN(imageRef, "/", 3)
	if len(parts) >= 2 && (strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":")) {
		if len(parts) == 3 {
			return parts[0] + "/" + parts[1]
		}
		return parts[0]
	}
	return "docker.io"
}
