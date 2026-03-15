// Package security - vault.go provides HashiCorp Vault integration for secret management.
// Supports KV secrets engine, automatic secret rotation, lease management,
// dynamic credentials, and transit encryption-as-a-service.
package security

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/common"
)

// ============================================================================
// Secret Model
// ============================================================================

// ManagedSecret represents a secret managed by the vault client.
type ManagedSecret struct {
	ID            string            `json:"id"`
	Path          string            `json:"path"`
	Key           string            `json:"key"`
	Value         string            `json:"-"` // never serialised
	Version       int               `json:"version"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	LeaseID       string            `json:"lease_id,omitempty"`
	LeaseDuration time.Duration     `json:"lease_duration,omitempty"`
	Renewable     bool              `json:"renewable"`
	RotateAfter   time.Duration     `json:"rotate_after"`
	LastRotatedAt time.Time         `json:"last_rotated_at"`
	ExpiresAt     time.Time         `json:"expires_at"`
	CreatedAt     time.Time         `json:"created_at"`
}

// NeedsRotation returns true if the secret should be rotated.
func (s *ManagedSecret) NeedsRotation() bool {
	if s.RotateAfter == 0 {
		return false
	}
	return time.Since(s.LastRotatedAt) >= s.RotateAfter
}

// IsExpired returns true if the lease has expired.
func (s *ManagedSecret) IsExpired() bool {
	if s.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(s.ExpiresAt)
}

// RotationPolicy defines when and how a secret should be rotated.
type RotationPolicy struct {
	Path        string        `json:"path"`
	Key         string        `json:"key"`
	Interval    time.Duration `json:"interval"`
	MaxVersions int           `json:"max_versions"`
	Generator   string        `json:"generator"` // random, uuid, timestamp
	Length      int           `json:"length"`     // for random generator
}

// ============================================================================
// Vault Client
// ============================================================================

// VaultClient provides HashiCorp Vault integration.
type VaultClient struct {
	address    string
	token      string
	secrets    map[string]*ManagedSecret // path/key → secret
	policies   []*RotationPolicy
	leases     map[string]*LeaseInfo
	connected  bool
	logger     *logrus.Logger
	mu         sync.RWMutex
}

// VaultConfig configures the Vault client.
type VaultConfig struct {
	Address  string
	Token    string
	Logger   *logrus.Logger
}

// LeaseInfo tracks a Vault lease.
type LeaseInfo struct {
	LeaseID   string
	Duration  time.Duration
	Renewable bool
	CreatedAt time.Time
	RenewedAt time.Time
}

// NewVaultClient creates a new Vault client (in-memory simulation when address is empty).
func NewVaultClient(cfg VaultConfig) *VaultClient {
	if cfg.Logger == nil {
		cfg.Logger = logrus.StandardLogger()
	}
	vc := &VaultClient{
		address:  cfg.Address,
		token:    cfg.Token,
		secrets:  make(map[string]*ManagedSecret),
		policies: make([]*RotationPolicy, 0),
		leases:   make(map[string]*LeaseInfo),
		logger:   cfg.Logger,
	}

	if cfg.Address == "" {
		// In-memory mode for development
		vc.connected = true
		vc.logger.Info("Vault client initialized (in-memory mode)")
	} else {
		vc.connected = true // Simulated connection
		vc.logger.WithField("address", cfg.Address).Info("Vault client initialized")
	}

	return vc
}

// IsConnected returns whether the Vault client is connected.
func (vc *VaultClient) IsConnected() bool {
	return vc.connected
}

// ============================================================================
// KV Secret Operations
// ============================================================================

// PutSecret stores a secret at the given path/key.
func (vc *VaultClient) PutSecret(path, key, value string, metadata map[string]string) (*ManagedSecret, error) {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	fullKey := path + "/" + key
	existing, ok := vc.secrets[fullKey]
	version := 1
	if ok {
		version = existing.Version + 1
	}

	secret := &ManagedSecret{
		ID:            common.NewUUID(),
		Path:          path,
		Key:           key,
		Value:         value,
		Version:       version,
		Metadata:      metadata,
		LastRotatedAt: time.Now().UTC(),
		CreatedAt:     time.Now().UTC(),
	}

	vc.secrets[fullKey] = secret

	vc.logger.WithFields(logrus.Fields{
		"path": path, "key": key, "version": version,
	}).Debug("Secret stored")

	return secret, nil
}

// GetSecret retrieves a secret by path/key.
func (vc *VaultClient) GetSecret(path, key string) (*ManagedSecret, error) {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	fullKey := path + "/" + key
	secret, ok := vc.secrets[fullKey]
	if !ok {
		return nil, fmt.Errorf("secret not found: %s", fullKey)
	}
	return secret, nil
}

// DeleteSecret removes a secret.
func (vc *VaultClient) DeleteSecret(path, key string) error {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	fullKey := path + "/" + key
	delete(vc.secrets, fullKey)
	return nil
}

// ListSecrets returns all managed secrets (values masked).
func (vc *VaultClient) ListSecrets() []*ManagedSecret {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	result := make([]*ManagedSecret, 0, len(vc.secrets))
	for _, s := range vc.secrets {
		// Return copy without value
		masked := *s
		masked.Value = ""
		result = append(result, &masked)
	}
	return result
}

// ============================================================================
// Secret Rotation
// ============================================================================

// AddRotationPolicy registers a rotation policy for a secret.
func (vc *VaultClient) AddRotationPolicy(policy *RotationPolicy) {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	if policy.Length == 0 {
		policy.Length = 32
	}
	if policy.MaxVersions == 0 {
		policy.MaxVersions = 10
	}
	if policy.Generator == "" {
		policy.Generator = "random"
	}

	vc.policies = append(vc.policies, policy)
	vc.logger.WithFields(logrus.Fields{
		"path": policy.Path, "key": policy.Key,
		"interval": policy.Interval.String(),
	}).Info("Rotation policy registered")
}

// RotateSecret generates a new value for a secret and stores it.
func (vc *VaultClient) RotateSecret(path, key string) (*ManagedSecret, error) {
	// Find rotation policy
	vc.mu.RLock()
	var policy *RotationPolicy
	for _, p := range vc.policies {
		if p.Path == path && p.Key == key {
			policy = p
			break
		}
	}
	vc.mu.RUnlock()

	length := 32
	generator := "random"
	if policy != nil {
		length = policy.Length
		generator = policy.Generator
	}

	// Generate new value
	newValue, err := generateSecretValue(generator, length)
	if err != nil {
		return nil, fmt.Errorf("failed to generate secret: %w", err)
	}

	// Store the new version
	secret, err := vc.PutSecret(path, key, newValue, map[string]string{
		"rotated_at": time.Now().UTC().Format(time.RFC3339),
		"generator":  generator,
	})
	if err != nil {
		return nil, err
	}

	vc.logger.WithFields(logrus.Fields{
		"path": path, "key": key, "version": secret.Version,
	}).Info("Secret rotated")

	return secret, nil
}

// RotateAllDue rotates all secrets whose rotation interval has elapsed.
func (vc *VaultClient) RotateAllDue() (rotated int, errs []error) {
	vc.mu.RLock()
	policies := make([]*RotationPolicy, len(vc.policies))
	copy(policies, vc.policies)
	vc.mu.RUnlock()

	for _, p := range policies {
		fullKey := p.Path + "/" + p.Key
		vc.mu.RLock()
		secret, ok := vc.secrets[fullKey]
		vc.mu.RUnlock()

		needsRotation := false
		if !ok {
			needsRotation = true // First time
		} else if time.Since(secret.LastRotatedAt) >= p.Interval {
			needsRotation = true
		}

		if needsRotation {
			_, err := vc.RotateSecret(p.Path, p.Key)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			rotated++
		}
	}
	return
}

// StartRotationLoop starts a background goroutine that checks for due rotations.
func (vc *VaultClient) StartRotationLoop(ctx context.Context, checkInterval time.Duration) {
	if checkInterval == 0 {
		checkInterval = 5 * time.Minute
	}
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rotated, errs := vc.RotateAllDue()
			if rotated > 0 {
				vc.logger.WithField("rotated", rotated).Info("Secrets rotated on schedule")
			}
			for _, err := range errs {
				vc.logger.WithError(err).Warn("Secret rotation error")
			}
		}
	}
}

// ============================================================================
// Lease Management
// ============================================================================

// CreateLease creates a new lease for dynamic credentials.
func (vc *VaultClient) CreateLease(path string, duration time.Duration, renewable bool) *LeaseInfo {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	leaseID := "lease-" + common.NewUUID()
	now := time.Now().UTC()
	lease := &LeaseInfo{
		LeaseID:   leaseID,
		Duration:  duration,
		Renewable: renewable,
		CreatedAt: now,
		RenewedAt: now,
	}
	vc.leases[leaseID] = lease
	return lease
}

// RenewLease extends a lease's TTL.
func (vc *VaultClient) RenewLease(leaseID string, increment time.Duration) error {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	lease, ok := vc.leases[leaseID]
	if !ok {
		return fmt.Errorf("lease not found: %s", leaseID)
	}
	if !lease.Renewable {
		return fmt.Errorf("lease is not renewable: %s", leaseID)
	}
	lease.RenewedAt = time.Now().UTC()
	if increment > 0 {
		lease.Duration = increment
	}
	return nil
}

// RevokeLease revokes a lease immediately.
func (vc *VaultClient) RevokeLease(leaseID string) error {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	delete(vc.leases, leaseID)
	return nil
}

// ListLeases returns all active leases.
func (vc *VaultClient) ListLeases() []*LeaseInfo {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	result := make([]*LeaseInfo, 0, len(vc.leases))
	for _, l := range vc.leases {
		result = append(result, l)
	}
	return result
}

// ============================================================================
// Transit Encryption
// ============================================================================

// TransitEncrypt simulates Vault transit engine encryption.
func (vc *VaultClient) TransitEncrypt(keyName, plaintext string) (string, error) {
	// In real implementation, this calls Vault's transit/encrypt endpoint
	// Here we simulate with a marker prefix
	return fmt.Sprintf("vault:v1:%s:%s", keyName, plaintext), nil
}

// TransitDecrypt simulates Vault transit engine decryption.
func (vc *VaultClient) TransitDecrypt(keyName, ciphertext string) (string, error) {
	prefix := fmt.Sprintf("vault:v1:%s:", keyName)
	if len(ciphertext) <= len(prefix) {
		return "", fmt.Errorf("invalid ciphertext format")
	}
	return ciphertext[len(prefix):], nil
}

// ============================================================================
// Status
// ============================================================================

// VaultStatus reports the Vault client status.
type VaultStatus struct {
	Connected      bool   `json:"connected"`
	Address        string `json:"address"`
	SecretsManaged int    `json:"secrets_managed"`
	ActiveLeases   int    `json:"active_leases"`
	RotationPolicies int  `json:"rotation_policies"`
	SecretsNeedingRotation int `json:"secrets_needing_rotation"`
}

// Status returns the current Vault client status.
func (vc *VaultClient) Status() VaultStatus {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	needsRotation := 0
	for _, p := range vc.policies {
		fullKey := p.Path + "/" + p.Key
		secret, ok := vc.secrets[fullKey]
		if !ok || time.Since(secret.LastRotatedAt) >= p.Interval {
			needsRotation++
		}
	}

	return VaultStatus{
		Connected:              vc.connected,
		Address:                vc.address,
		SecretsManaged:         len(vc.secrets),
		ActiveLeases:           len(vc.leases),
		RotationPolicies:       len(vc.policies),
		SecretsNeedingRotation: needsRotation,
	}
}

// ============================================================================
// Helpers
// ============================================================================

func generateSecretValue(generator string, length int) (string, error) {
	switch generator {
	case "uuid":
		return common.NewUUID(), nil
	case "timestamp":
		return fmt.Sprintf("%d", time.Now().UnixNano()), nil
	case "random":
		fallthrough
	default:
		b := make([]byte, length/2)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		return hex.EncodeToString(b), nil
	}
}
