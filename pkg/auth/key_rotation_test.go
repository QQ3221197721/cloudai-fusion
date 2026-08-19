package auth

import (
	"testing"
	"time"
)

func TestKeyRotator_New(t *testing.T) {
	rotator, err := NewKeyRotator()
	if err != nil {
		t.Fatalf("NewKeyRotator() failed: %v", err)
	}

	key := rotator.CurrentKey()
	if key == nil {
		t.Fatal("CurrentKey() returned nil")
	}

	if key.Kid == "" || len(key.Secret) != 32 {
		t.Error("Invalid initial key structure")
	}

	if key.Status != "active" {
		t.Errorf("Expected initial status 'active', got '%s'", key.Status)
	}
}

func TestKeyRotator_Rotate(t *testing.T) {
	rotator, _ := NewKeyRotator()

	oldKID := rotator.CurrentKey().Kid
	newKID, err := rotator.Rotate()
	if err != nil {
		t.Fatalf("Rotate() failed: %v", err)
	}

	if newKID == oldKID {
		t.Error("New key ID should be different from old key ID")
	}

	currentKey := rotator.CurrentKey()
	if currentKey == nil {
		t.Fatal("CurrentKey() returned nil after rotation")
	}

	if currentKey.Kid != newKID {
		t.Errorf("Current key should be the newly rotated key: expected %s, got %s", newKID, currentKey.Kid)
	}

	prevKey, ok := rotator.keys[oldKID]
	if !ok {
		t.Error("Previous key was removed from keys map")
	} else if prevKey.Status != "deprecated" {
		t.Errorf("Previous key should be deprecated, got %s", prevKey.Status)
	}
}

func TestKeyRotator_FindKeyByKid(t *testing.T) {
	rotator, _ := NewKeyRotator()

	validKey, err := rotator.FindKeyByKid(rotator.CurrentKey().Kid)
	if err != nil {
		t.Errorf("Finding valid key failed: %v", err)
	}
	if validKey == nil {
		t.Fatal("Found key is nil")
	}

	_, err = rotator.FindKeyByKid("nonexistent-key-id")
	if err == nil {
		t.Error("Looking up non-existent key should fail")
	}

	// Test revoked key
	rotator.mu.Lock()
	rotator.currentKID = "" // Remove current reference so we can manually test
	rotator.mu.Unlock()

	invalidKey := &SigningKey{
		Kid:       "revoked-test",
		Secret:    make([]byte, 32),
		CreatedAt: time.Now().UTC(),
		Revoked:   true,
		Status:    "revoked",
	}
	rotator.mu.Lock()
	rotator.keys["revoked-test"] = invalidKey
	rotator.mu.Unlock()

	_, err = rotator.FindKeyByKid("revoked-test")
	if err != ErrKeyRevoked {
		t.Errorf("Expected ErrKeyRevoked, got %v", err)
	}
}

func TestKeyRotator_ExpiredKey(t *testing.T) {
	rotator, _ := NewKeyRotator()

	expiredKey := &SigningKey{
		Kid:       "expired-test",
		Secret:    make([]byte, 32),
		CreatedAt: time.Now().UTC().Add(-48 * time.Hour),
		ExpiresAt: time.Now().UTC().Add(-24 * time.Hour),
		Status:    "active",
	}

	rotator.mu.Lock()
	rotator.keys["expired-test"] = expiredKey
	rotator.mu.Unlock()

	_, err := rotator.FindKeyByKid("expired-test")
	if err != ErrKeyExpired {
		t.Errorf("Expected ErrKeyExpired for expired key, got %v", err)
	}
}

func TestKeyRotator_GetValidKeys(t *testing.T) {
	rotator, _ := NewKeyRotator()

	validKeys := rotator.GetValidKeys()
	if len(validKeys) == 0 {
		t.Error("GetValidKeys() returned empty list")
	}

	// Add a revoked key
	rotator.mu.Lock()
	rotator.keys["revoked"] = &SigningKey{
		Kid:       "revoked",
		Secret:    make([]byte, 32),
		Revoked:   true,
		Status:    "revoked",
	}
	rotator.mu.Unlock()

	validKeys = rotator.GetValidKeys()
	for _, key := range validKeys {
		if key.Kid == "revoked" {
			t.Error("Revoked key should not appear in valid keys")
		}
	}
}

func TestKeyRotator_PurgeExpired(t *testing.T) {
	rotator, _ := NewKeyRotator()

	_ = len(rotator.keys) // initial count for testing

	oldKey := &SigningKey{
		Kid:       "very-old",
		Secret:    make([]byte, 32),
		CreatedAt: time.Now().UTC().Add(-30 * time.Hour),
		ExpiresAt: time.Now().UTC().Add(-25 * time.Hour),
		Status:    "expired",
	}
	rotator.mu.Lock()
	rotator.keys["very-old"] = oldKey
	rotator.mu.Unlock()

	removed := rotator.PurgeExpired(24 * time.Hour)
	if removed == 0 {
		t.Error("PurgeExpired() should have removed at least one key")
	}

	if _, ok := rotator.keys["very-old"]; ok {
		t.Error("Purged key should be removed from map")
	}
}

func TestHexEncode(t *testing.T) {
	input := []byte{0x00, 0x01, 0x0f, 0xff}
	expected := "00010fff"

	result := hexEncode(input)
	if result != expected {
		t.Errorf("hexEncode(%v) = %s, want %s", input, result, expected)
	}
}
