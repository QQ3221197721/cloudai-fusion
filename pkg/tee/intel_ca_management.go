// Package tee - Production-grade TEE Provider Framework for CloudAI Fusion
package tee

import (
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ============================================================================
// INTEL ROOT CA MANAGEMENT (Real Download + Cache)
// ============================================================================

// FetchAndCacheIntelRootCA downloads Intel Root CA from official source and caches locally
// Returns PEM-encoded certificate or error if download fails
func FetchAndCacheIntelRootCA() ([]byte, error) {
	cacheDir := getIntelCARootDir()
	cachePath := filepath.Join(cacheDir, "intel_root_ca.pem")
	
	// Check cache validity (24 hours TTL)
	if isValidCache(cachePath, 24*time.Hour) {
		return os.ReadFile(cachePath)
	}
	
	// Download from Intel official source
	resp, err := http.Get("https://download.intel.com/security/rootcerts/intel_root_ca.crt")
	if err != nil {
		return nil, fmt.Errorf("network-error-downloading-intel-ca: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("intel-http-%d", resp.StatusCode)
	}
	
	certPEM, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read-intel-ca: %w", err)
	}
	
	// Save to cache
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir-cache-dir: %w", err)
	}
	
	if err := os.WriteFile(cachePath, certPEM, 0644); err != nil {
		return nil, fmt.Errorf("write-intel-ca-cache: %w", err)
	}
	
	return certPEM, nil
}

// getIntelCARootDir returns the local cache directory for Intel certificates
func getIntelCARootDir() string {
	base := os.Getenv("CLOUDAI_TEE_CACHE_DIR")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cloudai-fusion", "tee")
	}
	return base
}

// isValidCache checks if cached file is within TTL
func isValidCache(path string, ttl time.Duration) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	modTime := info.ModTime()
	return time.Since(modTime) < ttl
}

// CertPoolFromPEMs creates x509.CertPool from list of PEM certs
func CertPoolFromPEMs(pems [][]byte) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	for _, pem := range pems {
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("failed-to-append-cert-from-pem")
		}
	}
	return pool, nil
}

// ============================================================================
// SECURE API KEY MANAGEMENT
// ============================================================================

// LoadIASAPIKey loads API key from environment variable or returns error
// Supports: CLOUDAI_INTEL_API_KEY or fallback to env var IAS_API_KEY
func LoadIASAPIKey() (string, error) {
	key := os.Getenv("CLOUDAI_INTEL_API_KEY")
	if key == "" {
		key = os.Getenv("IAS_API_KEY")
	}
	if key == "" {
		return "", fmt.Errorf("missing-api-key: set CLOUDAI_INTEL_API_KEY or IAS_API_KEY")
	}
	return key, nil
}

// ============================================================================
// MOCK IAS SERVER SUPPORT (For Development Without Real Credentials)
// ============================================================================

// MockIASResponse generates a valid-looking mock response for testing
// Use this in development when you don't have real Intel IAS credentials
type MockIASResponse struct {
	QuoteStatus         string   `json:"quoteStatus"`
	PSEID               string   `json:"pseID"`
	TCBEvaluationStatus string   `json:"tcbEvaluationStatus"`
	AdditionalInfo      []string `json:"additionalInfo,omitempty"`
}

var mockIASResponses = map[bool]*MockIASResponse{
	true: {
		QuoteStatus:         "VALID",
		PSEID:               "0x12345678",
		TCBEvaluationStatus: "FULLY_UPDATED",
	},
	false: {
		QuoteStatus:         "REVOKED",
		PSEID:               "0xDEADBEEF",
		TCBEvaluationStatus: "NOT_EVALUATED",
	},
}

func GenerateMockIASResponse(valid bool) *MockIASResponse {
	return mockIASResponses[valid]
}

// MockIASClients provides test clients that return pre-defined responses
type MockIASClients struct {
	mu       sync.RWMutex
	responses []*MockIASResponse
}

// NewMockIASClients creates a mock client generator with fixed responses
func NewMockIASClients(validResponses int) *MockIASClients {
	resp := &MockIASClients{
		responses: make([]*MockIASResponse, validResponses),
	}
	for i := 0; i < validResponses; i++ {
		resp.responses[i] = mockIASResponses[i%2 == 0] // alternating VALID/REVOKED
	}
	return resp
}

// GetNextResponse returns next response in round-robin order (for load testing)
func (m *MockIASClients) GetNextResponse() *MockIASResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	idx := len(m.responses) % len(m.responses)
	resp := m.responses[idx]
	return resp
}
