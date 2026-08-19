// Package auth provides JWT signing-key rotation support
package auth

import (
	"crypto/rand"
	"errors"
	"sync"
	"time"
)

var (
	ErrNoValidKeys    = errors.New("no valid keys available for token verification")
	ErrKeyExpired     = errors.New("key has expired")
	ErrKeyRevoked     = errors.New("key has been revoked")
	ErrNoCurrentKey   = errors.New("no current key configured")
)

// KeyRotator handles automatic JWT signing key rotation
type KeyRotator struct {
	keys       map[string]*SigningKey
	currentKID string
	nextIdx    int64
	mu         sync.RWMutex
}

// SigningKey represents a single JWT signing key
type SigningKey struct {
	Kid       string    `json:"kid"`
	Secret    []byte    `json:"-"` // Never expose raw secret
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Revoked   bool      `json:"revoked,omitempty"`
	Status    string    `json:"status"` // active | deprecated | expired | revoked
}

// NewKeyRotator creates a new key rotator with an initial key
func NewKeyRotator() (*KeyRotator, error) {
	kid, secret, err := generateNewKey()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	initialKey := &SigningKey{
		Kid:       kid,
		Secret:    secret,
		CreatedAt: now,
		ExpiresAt: now.Add(24 * time.Hour), // Default 24-hour expiry
		Status:    "active",
	}

	return &KeyRotator{
		keys:       map[string]*SigningKey{kid: initialKey},
		currentKID: kid,
		nextIdx:    1,
	}, nil
}

// generateNewKey creates a unique key ID and random secret
func generateNewKey() (kid string, secret []byte, err error) {
	kidBytes := make([]byte, 16)
	if _, err = rand.Read(kidBytes); err != nil {
		return "", nil, err
	}
	kid = "key-" + hexEncode(kidBytes)

	secret = make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return "", nil, err
	}

	return kid, secret, nil
}

// Rotate generates a new signing key and marks the previous one as deprecated
func (kr *KeyRotator) Rotate() (newKID string, err error) {
	kr.mu.Lock()
	defer kr.mu.Unlock()

	// Mark current key as deprecated if it exists
	if kr.currentKID != "" {
		if existing, ok := kr.keys[kr.currentKID]; ok && !existing.Revoked {
			existing.Status = "deprecated"
			existing.ExpiresAt = time.Now().UTC().Add(1 * time.Hour) // Deprecation window
		}
	}

	// Generate new key
	newKID, secret, err := generateNewKey()
	if err != nil {
		return "", err
	}

	now := time.Now().UTC()
	newKey := &SigningKey{
		Kid:       newKID,
		Secret:    secret,
		CreatedAt: now,
		ExpiresAt: now.Add(24 * time.Hour),
		Status:    "active",
	}

	kr.keys[newKID] = newKey
	kr.currentKID = newKID
	kr.nextIdx++

	return newKID, nil
}

// CurrentKey returns the current active signing key
func (kr *KeyRotator) CurrentKey() *SigningKey {
	kr.mu.RLock()
	defer kr.mu.RUnlock()

	if kr.currentKID == "" {
		return nil
	}
	return kr.keys[kr.currentKID]
}

// FindKeyByKid looks up a key by its ID
func (kr *KeyRotator) FindKeyByKid(kid string) (*SigningKey, error) {
	kr.mu.RLock()
	defer kr.mu.RUnlock()

	key, ok := kr.keys[kid]
	if !ok {
		return nil, ErrNoValidKeys
	}

	// Check status
	switch key.Status {
	case "revoked":
		return nil, ErrKeyRevoked
	case "expired":
		return nil, ErrKeyExpired
	case "active", "deprecated":
		if isExpired(key) {
			return nil, ErrKeyExpired
		}
		return key, nil
	default:
		return nil, ErrNoValidKeys
	}
}

// VerifyWithAnyValidKey tries to verify a token using current or any previous valid key
func (kr *KeyRotator) VerifyWithAnyValidKey(tokenString string, claimsFunc func(key *SigningKey) (*Claims, error)) (*Claims, error) {
	kr.mu.RLock()
	defer kr.mu.RUnlock()

	// Try current key first
	if kr.currentKID != "" {
		if key, ok := kr.keys[kr.currentKID]; ok && !key.Revoked && !isExpired(key) {
			if claims, err := claimsFunc(key); err == nil {
				return claims, nil
			}
		}
	}

	// Try other active/deprecated keys
	for _, key := range kr.keys {
		if key.Kid == kr.currentKID {
			continue // Already tried
		}
		if !key.Revoked && !isExpired(key) && (key.Status == "active" || key.Status == "deprecated") {
			if claims, err := claimsFunc(key); err == nil {
				return claims, nil
			}
		}
	}

	return nil, ErrTokenInvalid
}

// isExpired checks if a key has passed its expiration time
func isExpired(key *SigningKey) bool {
	return time.Now().UTC().After(key.ExpiresAt)
}

// GetValidKeys returns all currently valid keys (not revoked/expired)
func (kr *KeyRotator) GetValidKeys() []*SigningKey {
	kr.mu.RLock()
	defer kr.mu.RUnlock()

	var validKeys []*SigningKey
	now := time.Now().UTC()

	for _, key := range kr.keys {
		if !key.Revoked && !now.After(key.ExpiresAt) {
			validKeys = append(validKeys, key)
		}
	}

	return validKeys
}

// PurgeExpired removes old expired keys (for cleanup)
func (kr *KeyRotator) PurgeExpired(cutoff time.Duration) int {
	kr.mu.Lock()
	defer kr.mu.Unlock()

	now := time.Now().UTC()
	cutoffTime := now.Add(-cutoff)

	var removed []string
	for kid, key := range kr.keys {
		if key.CreatedAt.Before(cutoffTime) && (key.Revoked || now.After(key.ExpiresAt)) {
			removed = append(removed, kid)
		}
	}

	for _, kid := range removed {
		delete(kr.keys, kid)
		if kid == kr.currentKID {
			kr.currentKID = ""
		}
	}

	return len(removed)
}

// hexEncode converts bytes to lowercase hex string
func hexEncode(b []byte) string {
	result := make([]byte, len(b)*2)
	for i, c := range b {
		result[i*2] = hexTable[c>>4]
		result[i*2+1] = hexTable[c&0xf]
	}
	return string(result)
}

var hexTable = "0123456789abcdef"
