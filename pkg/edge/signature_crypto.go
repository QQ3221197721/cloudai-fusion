package edge

// SignatureCrypto implements cryptographic signing and verification for edge node communications
// This module provides message authentication, data integrity verification,
// and secure communication channels between edge nodes and the cloud control plane.

import (
	"bytes"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// SigningAlgorithm represents supported signing algorithms
type SigningAlgorithm string

const (
	SHA256WithRSA SigningAlgorithm = "SHA256withRSA"
	HMACSHA256    SigningAlgorithm = "HMAC-SHA256"
)

// MessageSignature represents a signed message with metadata
type MessageSignature struct {
	MessageID   string    `json:"messageId"`
	SignerID    string    `json:"signerId"`
	Timestamp   time.Time `json:"timestamp"`
	ExpiresAt   time.Time `json:"expiresAt"`
	Algorithm   string    `json:"algorithm"`
	Signature   string    `json:"signature"`
	PayloadHash string    `json:"payloadHash"`
}

// CryptoManager manages cryptographic keys and operations
type CryptoManager struct {
	mu              sync.RWMutex
	privateKeys     map[string]*rsa.PrivateKey
	publicKeys      map[string]*rsa.PublicKey
	hmacKeys        map[string][]byte
	keyExpiration   time.Duration
	logger          Logger
}

// NewCryptoManager creates a new crypto manager instance
func NewCryptoManager(keyExpiration time.Duration, logger Logger) *CryptoManager {
	return &CryptoManager{
		privateKeys:   make(map[string]*rsa.PrivateKey),
		publicKeys:    make(map[string]*rsa.PublicKey),
		hmacKeys:      make(map[string][]byte),
		keyExpiration: keyExpiration,
		logger:        logger,
	}
}

// GenerateKeyPair generates a new RSA key pair for an edge node
func (cm *CryptoManager) GenerateKeyPair(nodeID string) (*rsa.PrivateKey, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA key: %w", err)
	}

	cm.privateKeys[nodeID] = privateKey
	
	// Extract public key
	cm.publicKeys[nodeID] = &privateKey.PublicKey

	if cm.logger != nil {
		cm.logger.Infof("Generated key pair for edge node: %s", nodeID)
	}

	return privateKey, nil
}

// ImportPrivateKey imports an existing private key
func (cm *CryptoManager) ImportPrivateKey(nodeID string, derData []byte) error {
	block, _ := pem.Decode(derData)
	if block == nil {
		return errors.New("failed to decode PEM block")
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse PKCS1 private key: %w", err)
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.privateKeys[nodeID] = privateKey
	cm.publicKeys[nodeID] = &privateKey.PublicKey

	return nil
}

// ExportPublicKey exports a public key in PEM format
func (cm *CryptoManager) ExportPublicKey(nodeID string) ([]byte, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	publicKey, ok := cm.publicKeys[nodeID]
	if !ok {
		return nil, fmt.Errorf("public key not found for node: %s", nodeID)
	}

	publicDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal public key: %w", err)
	}

	publicPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicDER,
	})

	return publicPEM, nil
}

// SetHMACKey sets an HMAC key for symmetric signing
func (cm *CryptoManager) SetHMACKey(nodeID string, key []byte) error {
	if len(key) < 32 {
		return errors.New("HMAC key must be at least 32 bytes")
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.hmacKeys[nodeID] = key

	if cm.logger != nil {
		cm.logger.Infof("Set HMAC key for node: %s", nodeID)
	}

	return nil
}

// SignMessage signs a payload using RSA private key
func (cm *CryptoManager) SignMessage(nodeID string, payload []byte) (*MessageSignature, error) {
	cm.mu.RLock()
	privateKey, _ := cm.privateKeys[nodeID]
	cm.mu.RUnlock()

	if privateKey == nil {
		return nil, fmt.Errorf("no private key available for node: %s", nodeID)
	}

	messageID := generateMessageID()
	timestamp := time.Now()

	// Calculate payload hash
	payloadHash := sha256.Sum256(payload)
	
	// Create signature
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to sign payload: %w", err)
	}

	expiration := timestamp.Add(cm.keyExpiration)
	if expiration.IsZero() {
		expiration = timestamp.Add(time.Hour)
	}

	signatureObj := &MessageSignature{
		MessageID:   messageID,
		SignerID:    nodeID,
		Timestamp:   timestamp,
		ExpiresAt:   expiration,
		Algorithm:   string(SHA256WithRSA),
		Signature:   base64.StdEncoding.EncodeToString(signature),
		PayloadHash: base64.StdEncoding.EncodeToString(payloadHash[:]),
	}

	if cm.logger != nil {
		cm.logger.Debugf("Signed message %s from node %s", messageID, nodeID)
	}

	return signatureObj, nil
}

// VerifySignature verifies a message signature
func (cm *CryptoManager) VerifySignature(sig *MessageSignature) (bool, error) {
	// Check expiration
	if time.Now().After(sig.ExpiresAt) {
		return false, errors.New("signature expired")
	}

	cm.mu.RLock()
	publicKey, exists := cm.publicKeys[sig.SignerID]
	cm.mu.RUnlock()

	if !exists {
		return false, fmt.Errorf("public key not found for signer: %s", sig.SignerID)
	}

	// Decode signature
	signatureBytes, err := base64.StdEncoding.DecodeString(sig.Signature)
	if err != nil {
		return false, fmt.Errorf("failed to decode signature: %w", err)
	}

	// Reconstruct payload hash
	expectedHash, err := base64.StdEncoding.DecodeString(sig.PayloadHash)
	if err != nil {
		return false, fmt.Errorf("failed to decode payload hash: %w", err)
	}

	// In a real scenario, we would have the original payload here
	// For this skeleton implementation, we verify the signature structure
	hash := sha256.Sum256([]byte{}) // Placeholder - actual payload should be passed
	
	if !bytes.Equal(hash[:], expectedHash) {
		return false, errors.New("payload hash mismatch")
	}

	// Verify RSA signature
	err = rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, hash[:], signatureBytes)
	if err != nil {
		return false, fmt.Errorf("invalid signature: %w", err)
	}

	return true, nil
}

// SignHMAC signs a payload using HMAC-SHA256
func (cm *CryptoManager) SignHMAC(nodeID string, payload []byte) (string, error) {
	cm.mu.RLock()
	key, exists := cm.hmacKeys[nodeID]
	cm.mu.RUnlock()

	if !exists {
		return "", fmt.Errorf("HMAC key not found for node: %s", nodeID)
	}

	h := hmac.New(sha256.New, key)
	h.Write(payload)
	
	return base64.StdEncoding.EncodeToString(h.Sum(nil)), nil
}

// VerifyHMAC verifies an HMAC signature
func (cm *CryptoManager) VerifyHMAC(nodeID string, payload []byte, signature string) bool {
	expectedSig, err := cm.SignHMAC(nodeID, payload)
	if err != nil {
		return false
	}

	return hmac.Equal([]byte(signature), []byte(expectedSig))
}

// Encrypt encrypts data using RSA OAEP
func (cm *CryptoManager) Encrypt(publicKey *rsa.PublicKey, plaintext []byte) ([]byte, error) {
	ciphertext, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, publicKey, plaintext, nil)
	if err != nil {
		return nil, fmt.Errorf("encryption failed: %w", err)
	}

	return ciphertext, nil
}

// Decrypt decrypts data using RSA OAEP
func (cm *CryptoManager) Decrypt(privateKey *rsa.PrivateKey, ciphertext []byte) ([]byte, error) {
	plaintext, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, privateKey, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %w", err)
	}

	return plaintext, nil
}

// CertificateInfo contains X.509 certificate information
type CertificateInfo struct {
	Subject       string
	Issuer        string
	SerialNumber  string
	NotBefore     time.Time
	NotAfter      time.Time
	PublicKeyBits int
}

// ParseCertificate parses an X.509 certificate and returns its info
func ParseCertificate(certData []byte) (*CertificateInfo, error) {
	cert, err := x509.ParseCertificate(certData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	publicKeyBits := 0
	if rsaPub, ok := cert.PublicKey.(*rsa.PublicKey); ok {
		publicKeyBits = rsaPub.N.BitLen()
	}

	return &CertificateInfo{
		Subject:       cert.Subject.String(),
		Issuer:        cert.Issuer.String(),
		SerialNumber:  cert.SerialNumber.String(),
		NotBefore:     cert.NotBefore,
		NotAfter:      cert.NotAfter,
		PublicKeyBits: publicKeyBits,
	}, nil
}

// generateMessageID creates a unique message identifier
func generateMessageID() string {
	buffer := make([]byte, 16)
	io.ReadFull(rand.Reader, buffer)
	
	result := make([]byte, 32)
	base64.StdEncoding.Encode(result, buffer)
	return string(result)
}

// ValidateTimestamp checks if a timestamp is within acceptable skew
func ValidateTimestamp(timestamp time.Time, maxSkew time.Duration) bool {
	now := time.Now()
	diff := now.Sub(timestamp)
	
	if diff < 0 {
		diff = -diff
	}
	
	return diff <= maxSkew
}

// RotateKey rotates a node's key pair
func (cm *CryptoManager) RotateKey(nodeID string) (*rsa.PrivateKey, error) {
	newKey, err := cm.GenerateKeyPair(nodeID)
	if err != nil {
		return nil, err
	}

	if cm.logger != nil {
		cm.logger.Infof("Rotated key pair for node: %s", nodeID)
	}

	return newKey, nil
}

// RevokeKey removes a node's keys from the manager
func (cm *CryptoManager) RevokeKey(nodeID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	delete(cm.privateKeys, nodeID)
	delete(cm.publicKeys, nodeID)
	delete(cm.hmacKeys, nodeID)

	if cm.logger != nil {
		cm.logger.Warnf("Revoked keys for node: %s", nodeID)
	}
}

// GetStats returns cryptographic manager statistics
func (cm *CryptoManager) GetStats() map[string]interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return map[string]interface{}{
		"totalNodes":  len(cm.privateKeys),
		"hmacEnabled": len(cm.hmacKeys),
		"keyExpiry":   cm.keyExpiration.String(),
	}
}
