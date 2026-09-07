package license

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// LicenseType represents the tier of license
type LicenseType string

const (
	Community   LicenseType = "community"
	Professional LicenseType = "professional"
	Enterprise  LicenseType = "enterprise"
)

// LicenseManager handles license validation for enterprise features
type LicenseManager struct {
	publicKey     *ecdsa.PublicKey
	privateKey    *ecdsa.PrivateKey // For development/demo purposes
	DebugMode     bool              // Allow unrestricted access in debug mode
	EmbeddedSeed  []byte            // Seed embedded in binary for signature verification
}

// LicenseInfo contains license validation data
type LicenseInfo struct {
	TenantID       string             // Unique tenant identifier
	IssuedAt       time.Time          // When license was issued
	ExpirationDate time.Time          // License expiration
	LicenseType    LicenseType        // community, professional, enterprise
	Features       map[string]bool    // Enabled features
	MaxTargets     int                // Maximum scan targets (-1 = unlimited)
	APIQuota       int                // Monthly API quota limit (-1 = unlimited)
	MaxUsers       int                // Maximum concurrent users (-1 = unlimited)
	Signature      []byte             // Digital signature for integrity
	Nonce          string             // Unique nonce for this license
}

// NewLicenseManager creates a new LicenseManager with embedded key pair
func NewLicenseManager(debugMode bool) (*LicenseManager, error) {
	lm := &LicenseManager{
		DebugMode: debugMode,
	}

	// Load or generate keys
	if err := lm.loadOrGenerateKeys(); err != nil {
		return nil, fmt.Errorf("failed to load/generate keys: %w", err)
	}

	return lm, nil
}

// loadOrGenerateKeys loads existing keys or generates new ones
func (lm *LicenseManager) loadOrGenerateKeys() error {
	privKeyFile := ".private_key.pem"
	pubKeyFile := ".public_key.pem"

	// Check if keys exist
	if _, err := os.Stat(privKeyFile); err == nil {
		// Load existing keys
		privData, err := os.ReadFile(privKeyFile)
		if err != nil {
			return fmt.Errorf("failed to read private key: %w", err)
		}

		pubData, err := os.ReadFile(pubKeyFile)
		if err != nil {
			return fmt.Errorf("failed to read public key: %w", err)
		}

		privateKey, err := parsePrivateKey(privData)
		if err != nil {
			return fmt.Errorf("failed to parse private key: %w", err)
		}

		publicKey, err := parsePublicKey(pubData)
		if err != nil {
			return fmt.Errorf("failed to parse public key: %w", err)
		}

		lm.privateKey = privateKey
		lm.publicKey = publicKey
		return nil
	}

	// Generate new key pair
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate key pair: %w", err)
	}

	// Save keys for future use using PEM encoding
	privDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %w", err)
	}
	if err := os.WriteFile(privKeyFile, privDER, 0600); err != nil {
		return fmt.Errorf("failed to save private key: %w", err)
	}

	pubDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return fmt.Errorf("failed to marshal public key: %w", err)
	}
	if err := os.WriteFile(pubKeyFile, pubDER, 0644); err != nil {
		return fmt.Errorf("failed to save public key: %w", err)
	}

	lm.privateKey = privateKey
	lm.publicKey = &privateKey.PublicKey
	return nil
}

// parsePrivateKey parses ECDSA private key from bytes
func parsePrivateKey(data []byte) (*ecdsa.PrivateKey, error) {
	privKey, err := x509.ParsePKCS8PrivateKey(data)
	if err != nil {
		return nil, err
	}
	if ecKey, ok := privKey.(*ecdsa.PrivateKey); ok {
		return ecKey, nil
	}
	return nil, errors.New("not an ECDSA private key")
}

// parsePublicKey parses ECDSA public key from bytes
func parsePublicKey(data []byte) (*ecdsa.PublicKey, error) {
	pub, err := x509.ParsePKIXPublicKey(data)
	if err != nil {
		return nil, err
	}
	if ecKey, ok := pub.(*ecdsa.PublicKey); ok {
		return ecKey, nil
	}
	return nil, errors.New("not an ECDSA public key")
}

// ValidateLicense checks license validity before enabling advanced features
func (lm *LicenseManager) ValidateLicense(licenseKey string) (*LicenseInfo, error) {
	// Check if running in debug mode (development/testing only)
	if lm.DebugMode {
		return &LicenseInfo{
			TenantID:       "debug-mode",
			LicenseType:    Enterprise,
			IssuedAt:       time.Now(),
			ExpirationDate: time.Now().Add(365 * 24 * time.Hour),
			Features:       map[string]bool{"all": true},
			MaxTargets:     -1, // Unlimited
			APIQuota:       -1, // Unlimited
			MaxUsers:       -1, // Unlimited
		}, nil
	}

	if strings.TrimSpace(licenseKey) == "" {
		return nil, errors.New("empty license key provided")
	}

	// Parse and validate license key structure
	decoded, err := decodeLicenseKey(licenseKey)
	if err != nil {
		return nil, fmt.Errorf("invalid license key format: %w", err)
	}

	// Verify digital signature using embedded public key
	if !lm.verifySignature(decoded, lm.publicKey) {
		return nil, errors.New("license signature validation failed")
	}

	// Parse license payload
	var license LicenseInfo
	if err := json.Unmarshal(decoded, &license); err != nil {
		return nil, fmt.Errorf("license parsing failed: %w", err)
	}

	// Check expiration
	if time.Now().After(license.ExpirationDate) {
		return nil, fmt.Errorf("license expired on %s", license.ExpirationDate.Format("2006-01-02"))
	}

	// Validate tenant ID is not empty
	if strings.TrimSpace(license.TenantID) == "" {
		return nil, errors.New("license has no tenant ID")
	}

	return &license, nil
}

// decodeLicenseKey decodes base64-encoded license key
func decodeLicenseKey(licenseKey string) ([]byte, error) {
	trimmed := strings.TrimSpace(licenseKey)
	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return nil, fmt.Errorf("base64 decoding failed: %w", err)
	}

	if len(decoded) < 32 {
		return nil, errors.New("license payload too short")
	}

	return decoded, nil
}

// verifySignature validates license digital signature
func (lm *LicenseManager) verifySignature(encodedData []byte, publicKey *ecdsa.PublicKey) bool {
	// Extract signature (last 64 bytes) and data (everything else)
	sigLen := 64
	if len(encodedData) < sigLen+32 {
		return false
	}

	signature := encodedData[len(encodedData)-sigLen:]
	data := encodedData[:len(encodedData)-sigLen]

	hash := sha256.Sum256(data)

	// Verify ECDSA signature
	return ecdsa.VerifyASN1(publicKey, hash[:], signature)
}

// CreateLicense creates a new license for a tenant
func (lm *LicenseManager) CreateLicense(tenantID string, licenseType LicenseType, days int, features []string, maxTargets int) (*LicenseInfo, string, error) {
	if lm.DebugMode {
		// In debug mode, just create a simple license without signing
		return &LicenseInfo{
			TenantID:       tenantID,
			LicenseType:    licenseType,
			IssuedAt:       time.Now(),
			ExpirationDate: time.Now().Add(time.Duration(days) * 24 * time.Hour),
			Features:       makeMap(features),
			MaxTargets:     maxTargets,
			APIQuota:       -1,
			MaxUsers:       -1,
		}, "demo-license-key", nil
	}

	license := LicenseInfo{
		TenantID:       tenantID,
		LicenseType:    licenseType,
		IssuedAt:       time.Now(),
		ExpirationDate: time.Now().Add(time.Duration(days) * 24 * time.Hour),
		Features:       makeMap(features),
		MaxTargets:     maxTargets,
		APIQuota:       getQuotaByTier(licenseType),
		MaxUsers:       getMaxUsersByTier(licenseType),
		Nonce:          generateNonce(),
	}

	// Marshal license to JSON
	payload, err := json.Marshal(license)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal license: %w", err)
	}

	// Sign the payload
	hash := sha256.Sum256(payload)
	signature, err := lm.privateKey.Sign(rand.Reader, hash[:], nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to sign license: %w", err)
	}

	// Combine payload + signature
	signedData := append(payload, signature...)

	// Encode to base64
	encoded := base64.StdEncoding.EncodeToString(signedData)

	return &license, encoded, nil
}

// FeatureAccess checks if tenant can access a specific feature
func (lm *LicenseManager) FeatureAccess(license *LicenseInfo, featureName string) bool {
	if license == nil {
		return false
	}

	// Community tier gets basic features only
	if license.LicenseType == Community {
		return isBasicFeature(featureName)
	}

	// Check if feature is explicitly enabled
	if enabled, ok := license.Features[featureName]; ok {
		return enabled
	}

	// Check wildcard entry
	if allEnabled, ok := license.Features["all"]; ok {
		return allEnabled
	}

	// Default: professional+ get all features unless specified otherwise
	return license.LicenseType != Community
}

// GetRemainingTargets calculates remaining targets based on quota
func (lm *LicenseManager) GetRemainingTargets(license *LicenseInfo, usedTargets int) int {
	if license.MaxTargets < 0 {
		return -1 // Unlimited
	}
	return license.MaxTargets - usedTargets
}

// GetRemainingAPIQuota calculates remaining API quota
func (lm *LicenseManager) GetRemainingAPIQuota(license *LicenseInfo, usedQuota int) int {
	if license.APIQuota < 0 {
		return -1 // Unlimited
	}
	return license.APIQuota - usedQuota
}

// isBasicFeature returns true if feature is available to community tier
func isBasicFeature(featureName string) bool {
	basicFeatures := map[string]bool{
		"basic_vulnerability_scan":  true,
		"target_discovery":          true,
		"reporting":                 true,
		"user_enumeration":          true,
		"asset_management":          true,
	}
	return basicFeatures[featureName]
}

// getQuotaByTier returns monthly API quota for license type
func getQuotaByTier(licenseType LicenseType) int {
	switch licenseType {
	case Community:
		return 1000
	case Professional:
		return 10000
	default: // Enterprise
		return -1 // Unlimited
	}
}

// getMaxUsersByTier returns maximum concurrent users for license type
func getMaxUsersByTier(licenseType LicenseType) int {
	switch licenseType {
	case Community:
		return 5
	case Professional:
		return 50
	default: // Enterprise
		return -1 // Unlimited
	}
}

// makeMap creates a map from string slice
func makeMap(keys []string) map[string]bool {
	m := make(map[string]bool)
	for _, k := range keys {
		m[k] = true
	}
	return m
}

// generateNonce creates a unique random string
func generateNonce() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return base64.StdEncoding.EncodeToString(bytes)
}

// GenerateDemoLicense creates demo license for testing (legacy function)
func GenerateDemoLicense(tenantID string, days int) string {
	return "eyJ0ZW5hbnRfaWQiOiJkZW1vIiwibGljZW5zZV90eXBlIjoicHJvZmVzc2lvbmFsIn0="
}

// GetLicenseTier returns numeric tier for sorting/comparison
func GetLicenseTier(licenseType LicenseType) int {
	switch licenseType {
	case Community:
		return 1
	case Professional:
		return 2
	case Enterprise:
		return 3
	default:
		return 0
	}
}
