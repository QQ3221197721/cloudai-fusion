// Package auth implements secure JWT token management with rotation and refresh policies
package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sirupsen/logrus"
)

const (
	// Token lifetime configuration
	accessTokenLifetime = time.Minute * 30      // Shorter than 24h for security
	refreshTokenLifetime = time.Hour * 72       // 72 hours
	
	// Rotation settings
	keyRotationInterval = time.Hour * 24         // Rotate signing keys daily
	keyBackupCount = 3                           // Keep 3 old keys for validation
	
	// Refresh token policy
	maxRefreshAttempts = 5                      // Prevent brute force
	refreshWindow = time.Hour * 23               // Can refresh within last 24h before expiry
)

// KeyManager manages JWT signing key rotation
type KeyManager struct {
	currentKey   *rsa.PrivateKey
	currentKeyId string
	historyKeys  []*KeyVersion
	logger       *logrus.Logger
	keyStore     *sql.DB
}

type KeyVersion struct {
	KeyId        string
	PublicKey    []byte
	CreatedAt    time.Time
	RevokedAt    *time.Time
	Status       string // active, revoked, expired
}

func NewKeyManager(ctx context.Context, db *sql.DB, logger *logrus.Logger) (*KeyManager, error) {
	if logger == nil {
		logger = logrus.StandardLogger()
	}
	
	km := &KeyManager{
		logger: logger.WithFields(logrus.Fields{"component": "key_manager"}),
		keyStore: db,
	}
	
	// Initialize or rotate keys
	if err := km.initializeKeys(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize keys: %w", err)
	}
	
	return km, nil
}

// initializeKeys creates new key pair and stores old keys in history
func (km *KeyManager) initializeKeys(ctx context.Context) error {
	// Check if we have existing keys
	existingKey, err := km.getCurrentActiveKey(ctx)
	if err != nil || existingKey == nil {
		// Create new key pair
		newKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return fmt.Errorf("failed to generate RSA key: %w", err)
		}
		
		keyId := generateUUID()[:8]
		
		// Store current key
		km.currentKey = newKey
		km.currentKeyId = keyId
		
		// Add to history
		km.historyKeys = append(km.historyKeys, &KeyVersion{
			KeyId:     keyId,
			PublicKey: encodePublicKey(newKey.PublicKey),
			CreatedAt: time.Now().UTC(),
			Status:    "active",
		})
		
		return nil
	}
	
	// Load existing key
	publicKey, _ := decodeBase64(existingKey.PublicKey)
	newKey := &rsa.PrivateKey{
		PublicKey: rsa.PublicKey{
			N: publicKey.N,
			E: publicKey.E,
		},
	}
	
	km.currentKey = newKey
	km.currentKeyId = existingKey.KeyId
	
	// Load history
	km.loadHistoryKeys(ctx)
	
	return nil
}

// GenerateAccessToken creates a short-lived access token
func (km *KeyManager) GenerateAccessToken(user User) (string, error) {
	now := time.Now().UTC()
	
	claims := jwt.MapClaims{
		"user_id":      user.ID,
		"username":     user.Username,
		"email":        user.Email,
		"role":         user.Role,
		"iss":          "cloudai-fusion",
		"sub":          fmt.Sprintf("token_%s", user.ID),
		"iat":          now.Unix(),
		"exp":          now.Add(accessTokenLifetime).Unix(),
		"jti":          generateUUID(), // Unique token ID for rotation tracking
	}
	
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	
	signedToken, err := token.SignedString(km.currentKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}
	
	km.logger.WithFields(logrus.Fields{
		"token_id": claims["jti"],
		"user_id":  user.ID,
		"expires_in": accessTokenLifetime.String(),
	}).Info("Access token generated")
	
	return signedToken, nil
}

// GenerateRefreshToken creates a longer-lived refresh token
func (km *KeyManager) GenerateRefreshToken(userId string) (string, error) {
	now := time.Now().UTC()
	
	claims := jwt.MapClaims{
		"user_id": userId,
		"type":    "refresh",
		"iss":     "cloudai-fusion",
		"sub":     fmt.Sprintf("refresh_%s", userId),
		"iat":     now.Unix(),
		"exp":     now.Add(refreshTokenLifetime).Unix(),
		"jti":     generateUUID(),
	}
	
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	
	signedToken, err := token.SignedString(km.currentKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign refresh token: %w", err)
	}
	
	km.logger.WithField("user_id", userId).Debug("Refresh token generated")
	
	return signedToken, nil
}

// ValidateAndRotate validates refresh token and issues new access token
func (km *KeyManager) ValidateAndRotateRefreshToken(refreshToken string) (string, string, error) {
	token, err := jwt.Parse(refreshToken, func(token *jwt.Token) (interface{}, error) {
		// Validate signing method
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		
		// Try current key first
		currentPublicKey := km.currentKey.Public()
		if err := verifySignature(refreshToken, currentPublicKey); err == nil {
			return currentPublicKey, nil
		}
		
		// Try historical keys
		for _, version := range km.historyKeys {
			if version.Status != "active" {
				continue
			}
			
			publicKey := decodeBase64(version.PublicKey)
			if err := verifySignature(refreshToken, publicKey); err == nil {
				return publicKey, nil
			}
		}
		
		return nil, fmt.Errorf("invalid signature")
	})
	
	if err != nil {
		return "", "", fmt.Errorf("invalid refresh token: %w", err)
	}
	
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", "", fmt.Errorf("invalid claims format")
	}
	
	// Check token type
	if claims["type"] != "refresh" {
		return "", "", fmt.Errorf("not a refresh token")
	}
	
	// Check expiration
	expTime := time.Unix(int64(claims["exp"].(float64)), 0)
	if expTime.Before(time.Now()) {
		return "", "", fmt.Errorf("refresh token expired")
	}
	
	// Check if can still be refreshed (within 24h window)
	if claims["exp"].(float64) < time.Now().Add(refreshWindow).Unix() {
		return "", "", fmt.Errorf("refresh token too close to expiry")
	}
	
	userID := claims["user_id"].(string)
	
	// Generate new tokens
	newAccessToken, err := km.GenerateAccessToken(User{ID: userID})
	if err != nil {
		return "", "", fmt.Errorf("failed to generate new access token: %w", err)
	}
	
	newRefreshToken, err := km.GenerateRefreshToken(userID)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate new refresh token: %w", err)
	}
	
	km.logger.WithField("user_id", userID).Info("Refresh token rotated successfully")
	
	return newAccessToken, newRefreshToken, nil
}
