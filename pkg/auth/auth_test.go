package auth

import (
	"testing"
	"time"
)

// ============================================================================
// Password Hashing Tests
// ============================================================================

func TestHashPassword(t *testing.T) {
	password := "SecureP@ssw0rd!"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	if hash == "" {
		t.Fatal("hash should not be empty")
	}

	if hash == password {
		t.Fatal("hash should not equal plaintext password")
	}

	// bcrypt hashes start with "$2a$" or "$2b$"
	if hash[0] != '$' {
		t.Fatalf("expected bcrypt hash prefix, got: %s", hash[:10])
	}
}

func TestCheckPassword(t *testing.T) {
	password := "MyTestP@ssword123"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	// Correct password
	if !CheckPassword(password, hash) {
		t.Error("CheckPassword should return true for correct password")
	}

	// Wrong password
	if CheckPassword("WrongPassword", hash) {
		t.Error("CheckPassword should return false for incorrect password")
	}

	// Empty password
	if CheckPassword("", hash) {
		t.Error("CheckPassword should return false for empty password")
	}
}

func TestHashPasswordUniqueness(t *testing.T) {
	password := "SamePassword123!"
	hash1, _ := HashPassword(password)
	hash2, _ := HashPassword(password)

	if hash1 == hash2 {
		t.Error("two hashes of the same password should differ (bcrypt salt)")
	}

	// Both should still verify
	if !CheckPassword(password, hash1) || !CheckPassword(password, hash2) {
		t.Error("both hashes should verify against the original password")
	}
}

// ============================================================================
// JWT Token Tests
// ============================================================================

func TestNewService(t *testing.T) {
	// Valid config
	svc, err := NewService(Config{
		JWTSecret: "test-secret-key-for-unit-tests",
		JWTExpiry: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	if svc == nil {
		t.Fatal("service should not be nil")
	}

	// Missing secret
	_, err = NewService(Config{JWTSecret: ""})
	if err == nil {
		t.Error("NewService should fail with empty JWT secret")
	}

	// Default expiry
	svc, err = NewService(Config{JWTSecret: "test"})
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}
	if svc.jwtExpiry != 24*time.Hour {
		t.Errorf("expected default expiry 24h, got %v", svc.jwtExpiry)
	}
}

func TestGenerateAndValidateToken(t *testing.T) {
	svc, err := NewService(Config{
		JWTSecret: "unit-test-secret-key-32bytes!!!!",
		JWTExpiry: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	user := &User{
		ID:       "test-user-001",
		Username: "testadmin",
		Email:    "admin@test.io",
		Role:     RoleAdmin,
		Status:   "active",
	}

	// Generate token
	tokenResp, err := svc.GenerateToken(user)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	if tokenResp.AccessToken == "" {
		t.Error("access token should not be empty")
	}
	if tokenResp.TokenType != "Bearer" {
		t.Errorf("expected token type 'Bearer', got '%s'", tokenResp.TokenType)
	}
	if tokenResp.RefreshToken == "" {
		t.Error("refresh token should not be empty")
	}
	if tokenResp.User.Username != "testadmin" {
		t.Errorf("expected username 'testadmin', got '%s'", tokenResp.User.Username)
	}

	// Validate token
	claims, err := svc.ValidateToken(tokenResp.AccessToken)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}

	if claims.UserID != "test-user-001" {
		t.Errorf("expected user ID 'test-user-001', got '%s'", claims.UserID)
	}
	if claims.Username != "testadmin" {
		t.Errorf("expected username 'testadmin', got '%s'", claims.Username)
	}
	if claims.Role != RoleAdmin {
		t.Errorf("expected role 'admin', got '%s'", claims.Role)
	}
}

func TestValidateToken_Invalid(t *testing.T) {
	svc, _ := NewService(Config{
		JWTSecret: "test-secret",
		JWTExpiry: time.Hour,
	})

	// Completely invalid token
	_, err := svc.ValidateToken("not-a-valid-token")
	if err == nil {
		t.Error("should fail with invalid token")
	}

	// Token signed with different secret
	otherSvc, _ := NewService(Config{
		JWTSecret: "different-secret",
		JWTExpiry: time.Hour,
	})
	user := &User{ID: "1", Username: "test", Role: RoleViewer, Status: "active"}
	otherToken, _ := otherSvc.GenerateToken(user)
	_, err = svc.ValidateToken(otherToken.AccessToken)
	if err == nil {
		t.Error("should fail when validating token signed with different secret")
	}
}

func TestValidateToken_Expired(t *testing.T) {
	svc, _ := NewService(Config{
		JWTSecret: "test-secret",
		JWTExpiry: -time.Second, // already expired
	})

	user := &User{ID: "1", Username: "test", Role: RoleViewer, Status: "active"}
	tokenResp, _ := svc.GenerateToken(user)

	_, err := svc.ValidateToken(tokenResp.AccessToken)
	if err != ErrTokenExpired {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}

// ============================================================================
// RBAC Tests
// ============================================================================

func TestHasPermission(t *testing.T) {
	tests := []struct {
		role     Role
		perm     Permission
		expected bool
	}{
		{RoleAdmin, PermClusterCreate, true},
		{RoleAdmin, PermClusterDelete, true},
		{RoleAdmin, PermUserManage, true},
		{RoleOperator, PermClusterCreate, false},
		{RoleOperator, PermClusterRead, true},
		{RoleOperator, PermWorkloadCreate, true},
		{RoleDeveloper, PermClusterCreate, false},
		{RoleDeveloper, PermClusterRead, true},
		{RoleDeveloper, PermWorkloadCreate, true},
		{RoleViewer, PermClusterRead, true},
		{RoleViewer, PermClusterCreate, false},
		{RoleViewer, PermWorkloadCreate, false},
		{Role("unknown"), PermClusterRead, false},
	}

	for _, tc := range tests {
		result := HasPermission(tc.role, tc.perm)
		if result != tc.expected {
			t.Errorf("HasPermission(%s, %s) = %v, expected %v", tc.role, tc.perm, result, tc.expected)
		}
	}
}

func TestRolePermissionCounts(t *testing.T) {
	// Admin should have the most permissions
	adminPerms := rolePermissions[RoleAdmin]
	viewerPerms := rolePermissions[RoleViewer]

	if len(adminPerms) <= len(viewerPerms) {
		t.Errorf("admin should have more permissions than viewer: admin=%d, viewer=%d", len(adminPerms), len(viewerPerms))
	}

	// All roles should have at least one permission
	for role, perms := range rolePermissions {
		if len(perms) == 0 {
			t.Errorf("role %s should have at least one permission", role)
		}
	}
}
