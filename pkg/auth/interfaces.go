package auth

import "github.com/gin-gonic/gin"

// AuthService defines the contract for authentication and authorization.
// The API layer depends on this interface rather than the concrete *Service,
// enabling mock implementations for handler unit testing.
type AuthService interface {
	// AuthenticateUser validates credentials and returns the authenticated user.
	AuthenticateUser(username, password string) (*User, error)

	// GenerateToken creates a JWT token pair for the given user.
	GenerateToken(user *User) (*TokenResponse, error)

	// RegisterUser creates a new user account.
	RegisterUser(req *RegisterRequest) (*User, error)

	// ValidateToken parses and validates a JWT token string.
	ValidateToken(tokenString string) (*Claims, error)

	// AuthMiddleware returns a Gin middleware that enforces JWT authentication.
	AuthMiddleware() gin.HandlerFunc
}

// Compile-time interface satisfaction check.
var _ AuthService = (*Service)(nil)
