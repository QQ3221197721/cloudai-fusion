// Package auth provides JWT-based authentication for the Red Team Platform.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"gorm.io/gorm"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/models"
)

// =========================================
// Authentication Handler Interface
// =========================================

// AuthHandler defines the interface for authentication operations.
type AuthHandler interface {
	Login(ctx context.Context, username, password string) (*LoginResponse, error)
	Logout(ctx context.Context, token string) error
	RefreshToken(ctx context.Context, refreshToken string) (*LoginResponse, error)
	VerifyToken(authHeader string) (*jwt.Token, map[string]any, error)
	GetUserFromToken(claims map[string]any) (*models.User, error)
	Middleware() gin.HandlerFunc
}

// =========================================
// JWT Authentication Implementation
// =========================================

// JWTAuthHandler implements AuthHandler interface with JWT tokens and bcrypt passwords.
type JWTAuthHandler struct {
	secretKey           []byte
	db                  *gorm.DB
	tokenExpiry         time.Duration
	refreshTokenExpiry  time.Duration
	maxLoginAttempts    int
	loginWindowMinutes  int
}

// NewJWTAuthHandler creates a new JWT authentication handler.
func NewJWTAuthHandler(db *gorm.DB, envSecret string) (*JWTAuthHandler, error) {
	// Generate or use provided secret key
	var secretKey []byte
	if envSecret != "" {
		secretKey = []byte(envSecret)
		if len(secretKey) < 32 {
			return nil, fmt.Errorf("secret key must be at least 32 characters")
		}
	} else {
		secretKey = make([]byte, 32)
		if _, err := rand.Read(secretKey); err != nil {
			return nil, fmt.Errorf("failed to generate secret key: %w", err)
		}
	}

	return &JWTAuthHandler{
		secretKey:            secretKey,
		db:                   db,
		tokenExpiry:          15 * time.Minute,  // Short-lived access token
		refreshTokenExpiry:   7 * 24 * time.Hour, // Long-lived refresh token
		maxLoginAttempts:     5,                 // Rate limit threshold
		loginWindowMinutes:   5,                 // Window for rate limiting
	}, nil
}

// LoginRequest represents login form data.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse represents successful login result.
type LoginResponse struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int64     `json:"expires_in"`
	UserID       string    `json:"user_id"`
	Username     string    `json:"username"`
	Role         string    `json:"role"`
	Permissions  []string  `json:"permissions"`
	FullName     string    `json:"full_name,omitempty"`
}

// Login authenticates user with username/password and issues JWT tokens.
func (j *JWTAuthHandler) Login(ctx context.Context, username, password string) (*LoginResponse, error) {
	// Check rate limiting
	if err := j.checkRateLimiting(ctx, username); err != nil {
		return nil, err
	}

	// Fetch user from database
	var user models.User
	if err := j.db.WithContext(ctx).First(&user, "username = ? AND active = ?", username).Error; err != nil {
		j.recordFailedLogin(ctx, username, "")
		return nil, errors.New("invalid credentials")
	}

	// Verify password hash (BCrypt)
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		j.recordFailedLogin(ctx, username, "")
		return nil, errors.New("invalid credentials")
	}

	// Account lock check
	if user.IsLocked() {
		return nil, fmt.Errorf("account locked until %s", user.LockedUntil.Format(time.RFC3339))
	}

	// Clear failed login attempts on success
	j.clearFailedLogins(ctx, username)

	// Update last login timestamp
	j.db.WithContext(ctx).Model(&user).Update("last_login", time.Now())

	// Parse permissions from JSONB array
	permissions := []string{"read"}
	if user.Permissions.Valid && user.Permissions.Strings != nil {
		permissions = user.Permissions.Strings
	}

	// Generate access token (short-lived)
	accessToken, err := j.generateAccessToken(user)
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}

	// Generate refresh token (long-lived, stored in DB)
	refreshToken, err := j.generateRefreshToken(user)
	if err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	return &LoginResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int64(j.tokenExpiry.Seconds()),
		UserID:       user.ID.String(),
		Username:     user.Username,
		Role:         user.Role,
		Permissions:  permissions,
		FullName:     user.FullName,
		RefreshToken: refreshToken,
	}, nil
}

// generateAccessToken creates short-lived JWT access token.
func (j *JWTAuthHandler) generateAccessToken(user models.User) (string, error) {
	claims := jwt.MapClaims{
		"user_id":  user.ID.String(),
		"username": user.Username,
		"role":     user.Role,
		"exp":      time.Now().Add(j.tokenExpiry).Unix(),
		"iss":      "cloudai-fusion-red-team",
		"iat":      time.Now().Unix(),
		"jti":      uuid.New().String(), // Unique token ID
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(j.secretKey)
}

// generateRefreshToken creates long-lived refresh token stored in database.
func (j *JWTAuthHandler) generateRefreshToken(user models.User) (string, error) {
	// Generate cryptographically secure random token
	rawToken := make([]byte, 64)
	if _, err := rand.Read(rawToken); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}

	// Encode as base64 URL-safe string
	token := base64.URLEncoding.EncodeToString(rawToken)

	// Store in database for validation and revocation
	refreshTokenRecord := &models.RefreshToken{
		Token:     token,
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(j.refreshTokenExpiry),
		IsActive:  true,
	}

	if err := j.db.Create(refreshTokenRecord).Error; err != nil {
		return "", fmt.Errorf("failed to store refresh token: %w", err)
	}

	return token, nil
}

// RefreshToken reissues new access token using valid refresh token.
func (j *JWTAuthHandler) RefreshToken(ctx context.Context, refreshToken string) (*LoginResponse, error) {
	// Find refresh token in database
	var tokenRecord models.RefreshToken
	if err := j.db.WithContext(ctx).Where("token = ?", refreshToken).First(&tokenRecord).Error; err != nil {
		return nil, errors.New("invalid refresh token")
	}

	// Check if expired
	if time.Now().After(tokenRecord.ExpiresAt) {
		// Deactivate expired token
		j.db.Model(&tokenRecord).Update("is_active", false)
		return nil, errors.New("refresh token expired")
	}

	// Check if inactive (revoked)
	if !tokenRecord.IsActive {
		return nil, errors.New("refresh token has been revoked")
	}

	// Fetch user
	var user models.User
	if err := j.db.WithContext(ctx).First(&user, tokenRecord.UserID).Error; err != nil {
		return nil, errors.New("user not found")
	}

	// Deactivate old refresh token
	tokenRecord.IsActive = false
	j.db.Save(&tokenRecord)

	// Generate new tokens
	newAccessToken, err := j.generateAccessToken(user)
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}

	newRefreshToken, err := j.generateRefreshToken(user)
	if err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	// Parse permissions
	permissions := []string{"read"}
	if user.Permissions.Valid && user.Permissions.Strings != nil {
		permissions = user.Permissions.Strings
	}

	return &LoginResponse{
		AccessToken:  newAccessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int64(j.tokenExpiry.Seconds()),
		UserID:       user.ID.String(),
		Username:     user.Username,
		Role:         user.Role,
		Permissions:  permissions,
		FullName:     user.FullName,
		RefreshToken: newRefreshToken,
	}, nil
}

// Logout invalidates the current refresh token.
func (j *JWTAuthHandler) Logout(ctx context.Context, token string) error {
	var tokenRecord models.RefreshToken
	if err := j.db.WithContext(ctx).Where("token = ?", token).First(&tokenRecord).Error; err != nil {
		return errors.New("token not found")
	}

	// Deactivate the token
	return j.db.Model(&tokenRecord).Update("is_active", false).Error
}

// VerifyToken validates JWT access token and returns claims.
func (j *JWTAuthHandler) VerifyToken(authHeader string) (*jwt.Token, map[string]any, error) {
	// Extract Bearer token
	parts := splitAuthHeader(authHeader)
	if len(parts) != 2 {
		return nil, nil, errors.New("invalid authorization header format")
	}

	tokenString := parts[1]

	// Parse and verify JWT
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Validate signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return j.secretKey, nil
	})

	if err != nil {
		return nil, nil, fmt.Errorf("invalid token: %w", err)
	}

	// Extract claims
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, nil, errors.New("invalid token claims")
	}

	return token, claims, nil
}

// GetUserFromToken extracts user information from verified token claims.
func (j *JWTAuthHandler) GetUserFromToken(claims map[string]any) (*models.User, error) {
	userIDStr, ok := claims["user_id"].(string)
	if !ok {
		return nil, errors.New("invalid user_id claim")
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return nil, errors.New("invalid user_id format")
	}

	var user models.User
	if err := j.db.First(&user, userID).Error; err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	return &user, nil
}

// Middleware returns HTTP middleware for JWT authentication.
func (j *JWTAuthHandler) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")

		if authHeader == "" {
			c.JSON(401, gin.H{"error": "missing authorization header"})
			c.Abort()
			return
		}

		token, claims, err := j.VerifyToken(authHeader)
		if err != nil {
			c.JSON(401, gin.H{"error": "unauthorized", "details": err.Error()})
			c.Abort()
			return
		}

		// Attach claims to context for handlers to use
		c.Set("jwt_claims", claims)
		c.Set("jwt_token", token)

		c.Next()
	}
}

// checkRateLimiting throttens login attempts.
func (j *JWTAuthHandler) checkRateLimiting(ctx context.Context, username string) error {
	window := time.Duration(j.loginWindowMinutes) * time.Minute
	windowStart := time.Now().Add(-window)

	var count int64
	if err := j.db.WithContext(ctx).
		Table("login_attempts").
		Where("username = ? AND attempted_at > ? AND success = ?", username, windowStart, false).
		Count(&count).Error; err != nil {
		return fmt.Errorf("database error during rate limit check: %w", err)
	}

	if count >= int64(j.maxLoginAttempts) {
		// Set account lock
		lockDuration := 15 * time.Minute
		now := time.Now()
		lockedUntil := now.Add(lockDuration)

		j.db.WithContext(ctx).Model(&models.User{}).
			Where("username = ?", username).
			Update(map[string]interface{}{
				"locked_until":          lockedUntil,
				"failed_login_attempts": j.maxLoginAttempts,
			})

		return fmt.Errorf("too many failed attempts, please try again in %d minutes", j.loginWindowMinutes)
	}

	return nil
}

// recordFailedLogin tracks unsuccessful authentication attempts.
func (j *JWTAuthHandler) recordFailedLogin(ctx context.Context, username, ipAddress string) {
	attempt := &models.LoginAttempt{
		Username:  username,
		AttemptedAt: time.Now(),
		IPAddress: ipAddress,
		Success:   false,
	}
	j.db.Create(attempt)

	// Increment failed login counter
	j.db.WithContext(ctx).Model(&models.User{}).
		Where("username = ?", username).
		UpdateColumn("failed_login_attempts", gorm.Expr("failed_login_attempts + 1"))
}

// clearFailedLogins resets the failed login counter on successful authentication.
func (j *JWTAuthHandler) clearFailedLogins(ctx context.Context, username string) {
	j.db.WithContext(ctx).
		Table("login_attempts").
		Where("username = ? AND success = ?", username, false).
		Delete(nil)

	j.db.WithContext(ctx).Model(&models.User{}).
		Where("username = ?", username).
		Update(map[string]interface{}{
			"failed_login_attempts": 0,
			"locked_until":          nil,
		})
}

// Helper functions
func splitAuthHeader(header string) []string {
	return strings.SplitN(strings.TrimSpace(header), " ", 2)
}

// Export bcrypt hash generator for creating test users
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(bytes), err
}
