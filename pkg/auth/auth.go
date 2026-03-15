// Package auth provides authentication and authorization services
// for CloudAI Fusion, including JWT token management, OAuth 2.0 integration,
// and role-based access control (RBAC).
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/store"
)

// ============================================================================
// Errors
// ============================================================================

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrTokenExpired       = errors.New("token expired")
	ErrTokenInvalid       = errors.New("invalid token")
	ErrInsufficientPerms  = errors.New("insufficient permissions")
	ErrUserNotFound       = errors.New("user not found")
	ErrUserDisabled       = errors.New("user account disabled")
)

// ============================================================================
// Configuration
// ============================================================================

// Config holds authentication service configuration
type Config struct {
	JWTSecret string //nolint:gosec // G101: config field, not a hardcoded credential
	JWTExpiry time.Duration
}

// ============================================================================
// Models
// ============================================================================

// Role defines RBAC roles
type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleDeveloper Role = "developer"
	RoleViewer   Role = "viewer"
)

// Permission defines granular permissions
type Permission string

const (
	PermClusterCreate  Permission = "cluster:create"
	PermClusterRead    Permission = "cluster:read"
	PermClusterUpdate  Permission = "cluster:update"
	PermClusterDelete  Permission = "cluster:delete"
	PermWorkloadCreate Permission = "workload:create"
	PermWorkloadRead   Permission = "workload:read"
	PermWorkloadUpdate Permission = "workload:update"
	PermWorkloadDelete Permission = "workload:delete"
	PermSecurityManage Permission = "security:manage"
	PermSecurityRead   Permission = "security:read"
	PermUserManage     Permission = "user:manage"
	PermUserRead       Permission = "user:read"
	PermProviderManage Permission = "provider:manage"
	PermProviderRead   Permission = "provider:read"
	PermMonitorRead    Permission = "monitor:read"
	PermMonitorManage  Permission = "monitor:manage"
	PermCostRead       Permission = "cost:read"
	PermCostManage     Permission = "cost:manage"
	PermAgentManage    Permission = "agent:manage"
	PermAgentRead      Permission = "agent:read"
)

// rolePermissions maps roles to their allowed permissions
var rolePermissions = map[Role][]Permission{
	RoleAdmin: {
		PermClusterCreate, PermClusterRead, PermClusterUpdate, PermClusterDelete,
		PermWorkloadCreate, PermWorkloadRead, PermWorkloadUpdate, PermWorkloadDelete,
		PermSecurityManage, PermSecurityRead,
		PermUserManage, PermUserRead,
		PermProviderManage, PermProviderRead,
		PermMonitorRead, PermMonitorManage,
		PermCostRead, PermCostManage,
		PermAgentManage, PermAgentRead,
	},
	RoleOperator: {
		PermClusterRead, PermClusterUpdate,
		PermWorkloadCreate, PermWorkloadRead, PermWorkloadUpdate,
		PermSecurityRead,
		PermUserRead,
		PermProviderRead,
		PermMonitorRead, PermMonitorManage,
		PermCostRead,
		PermAgentRead,
	},
	RoleDeveloper: {
		PermClusterRead,
		PermWorkloadCreate, PermWorkloadRead, PermWorkloadUpdate,
		PermSecurityRead,
		PermProviderRead,
		PermMonitorRead,
		PermCostRead,
		PermAgentRead,
	},
	RoleViewer: {
		PermClusterRead,
		PermWorkloadRead,
		PermSecurityRead,
		PermProviderRead,
		PermMonitorRead,
		PermCostRead,
		PermAgentRead,
	},
}

// User represents an authenticated user
type User struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Role        Role      `json:"role"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

// Claims represents JWT token claims
type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Role     Role   `json:"role"`
	jwt.RegisteredClaims
}

// LoginRequest represents a login API request
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// TokenResponse represents the authentication token response
type TokenResponse struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int64     `json:"expires_in"`
	ExpiresAt    time.Time `json:"expires_at"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	User         User      `json:"user"`
}

// ============================================================================
// Service
// ============================================================================

// Service provides authentication and authorization functionality
type Service struct {
	jwtSecret []byte //nolint:gosec // G101: struct field, not a hardcoded credential
	jwtExpiry time.Duration
	store     *store.Store // nil when DB is unavailable (graceful degradation)
	logger    *logrus.Logger
}

// NewService creates a new authentication service
func NewService(cfg Config) (*Service, error) {
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT secret is required")
	}

	expiry := cfg.JWTExpiry
	if expiry == 0 {
		expiry = 24 * time.Hour
	}

	return &Service{
		jwtSecret: []byte(cfg.JWTSecret),
		jwtExpiry: expiry,
		logger:    logrus.StandardLogger(),
	}, nil
}

// SetStore injects the database store after initialization
func (s *Service) SetStore(st *store.Store) {
	s.store = st
}

// HashPassword hashes a plaintext password using bcrypt
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}
	return string(hash), nil
}

// CheckPassword compares a plaintext password with a bcrypt hash
func CheckPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// RegisterRequest represents a user registration request
type RegisterRequest struct {
	Username    string `json:"username" binding:"required,min=3,max=64"`
	Email       string `json:"email" binding:"required,email"`
	Password    string `json:"password" binding:"required,min=8"`
	DisplayName string `json:"display_name"`
}

// RegisterUser creates a new user in the database
func (s *Service) RegisterUser(req *RegisterRequest) (*User, error) {
	if s.store == nil {
		return nil, errors.New("database not available")
	}

	hashed, err := HashPassword(req.Password)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	dbUser := &store.User{
		ID:           uuid.New().String(),
		Username:     req.Username,
		Email:        req.Email,
		PasswordHash: hashed,
		DisplayName:  req.DisplayName,
		Role:         string(RoleViewer),
		Status:       "active",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := s.store.CreateUser(dbUser); err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}

	return &User{
		ID:          dbUser.ID,
		Username:    dbUser.Username,
		Email:       dbUser.Email,
		DisplayName: dbUser.DisplayName,
		Role:        Role(dbUser.Role),
		Status:      dbUser.Status,
		CreatedAt:   dbUser.CreatedAt,
	}, nil
}

// AuthenticateUser validates username/password against the database
func (s *Service) AuthenticateUser(username, password string) (*User, error) {
	if s.store == nil {
		return nil, errors.New("database not available")
	}

	dbUser, err := s.store.GetUserByUsername(username)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	if dbUser.Status != "active" {
		return nil, ErrUserDisabled
	}

	if !CheckPassword(password, dbUser.PasswordHash) {
		return nil, ErrInvalidCredentials
	}

	// Update last login time
	now := time.Now().UTC()
	dbUser.LastLoginAt = &now
	if err := s.store.UpdateUser(dbUser); err != nil {
		s.logger.WithError(err).WithField("user", dbUser.Username).Warn("Failed to update last login time")
	}

	return &User{
		ID:          dbUser.ID,
		Username:    dbUser.Username,
		Email:       dbUser.Email,
		DisplayName: dbUser.DisplayName,
		Role:        Role(dbUser.Role),
		Status:      dbUser.Status,
		CreatedAt:   dbUser.CreatedAt,
	}, nil
}

// GenerateToken creates a new JWT token for the given user
func (s *Service) GenerateToken(user *User) (*TokenResponse, error) {
	now := time.Now()
	expiresAt := now.Add(s.jwtExpiry)

	claims := &Claims{
		UserID:   user.ID,
		Username: user.Username,
		Email:    user.Email,
		Role:     user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "cloudai-fusion",
			Subject:   user.ID,
			ID:        uuid.New().String(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(s.jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to sign token: %w", err)
	}

	// Generate refresh token
	refreshBytes := make([]byte, 32)
	if _, err := rand.Read(refreshBytes); err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	return &TokenResponse{
		AccessToken:  tokenString,
		TokenType:    "Bearer",
		ExpiresIn:    int64(s.jwtExpiry.Seconds()),
		ExpiresAt:    expiresAt,
		RefreshToken: hex.EncodeToString(refreshBytes),
		User:         *user,
	}, nil
}

// ValidateToken parses and validates a JWT token string
func (s *Service) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.jwtSecret, nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrTokenInvalid
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrTokenInvalid
	}

	return claims, nil
}

// HasPermission checks if a role has a specific permission
func HasPermission(role Role, perm Permission) bool {
	perms, ok := rolePermissions[role]
	if !ok {
		return false
	}
	for _, p := range perms {
		if p == perm {
			return true
		}
	}
	return false
}

// ============================================================================
// Gin Middleware
// ============================================================================

// AuthMiddleware returns a Gin middleware that validates JWT tokens
func (s *Service) AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "authorization header required",
			})
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "invalid authorization format, expected 'Bearer <token>'",
			})
			return
		}

		claims, err := s.ValidateToken(parts[1])
		if err != nil {
			status := http.StatusUnauthorized
			message := "authentication failed"
			if errors.Is(err, ErrTokenExpired) {
				message = "token expired"
			}
			c.AbortWithStatusJSON(status, gin.H{
				"code":    status,
				"message": message,
			})
			return
		}

		// Set user info in context
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("email", claims.Email)
		c.Set("role", string(claims.Role))
		c.Set("claims", claims)

		c.Next()
	}
}

// RequirePermission returns middleware that checks for specific permission
func RequirePermission(perm Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		roleStr, exists := c.Get("role")
		if !exists {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "no role found in context",
			})
			return
		}

		role := Role(roleStr.(string))
		if !HasPermission(role, perm) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": fmt.Sprintf("permission '%s' required", perm),
			})
			return
		}

		c.Next()
	}
}

// RequireRole returns middleware that checks for minimum role level
func RequireRole(minRole Role) gin.HandlerFunc {
	roleHierarchy := map[Role]int{
		RoleViewer:    0,
		RoleDeveloper: 1,
		RoleOperator:  2,
		RoleAdmin:     3,
	}

	return func(c *gin.Context) {
		roleStr, exists := c.Get("role")
		if !exists {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "authentication required",
			})
			return
		}

		userRole := Role(roleStr.(string))
		if roleHierarchy[userRole] < roleHierarchy[minRole] {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": fmt.Sprintf("minimum role '%s' required", minRole),
			})
			return
		}

		c.Next()
	}
}
