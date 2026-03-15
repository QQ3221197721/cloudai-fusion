package store

import (
	"testing"
	"time"
)

func TestUserTableName(t *testing.T) {
	u := User{}
	if u.TableName() != "users" {
		t.Errorf("expected 'users', got %q", u.TableName())
	}
}

func TestAuditLogTableName(t *testing.T) {
	a := AuditLog{}
	if a.TableName() != "audit_logs" {
		t.Errorf("expected 'audit_logs', got %q", a.TableName())
	}
}

func TestUserFields(t *testing.T) {
	now := time.Now()
	u := User{
		ID:           "test-uuid",
		Username:     "admin",
		Email:        "admin@example.com",
		PasswordHash: "$2a$10$hash",
		DisplayName:  "Admin User",
		Role:         "admin",
		Status:       "active",
		LastLoginAt:  &now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if u.ID != "test-uuid" {
		t.Error("ID mismatch")
	}
	if u.Username != "admin" {
		t.Error("Username mismatch")
	}
	if u.Email != "admin@example.com" {
		t.Error("Email mismatch")
	}
	if u.PasswordHash != "$2a$10$hash" {
		t.Error("PasswordHash mismatch")
	}
	if u.Role != "admin" {
		t.Error("Role mismatch")
	}
	if u.LastLoginAt == nil {
		t.Error("LastLoginAt should not be nil")
	}
}

func TestAuditLogFields(t *testing.T) {
	a := AuditLog{
		ID:           "log-uuid",
		UserID:       "user-1",
		Username:     "admin",
		Action:       "create",
		ResourceType: "cluster",
		ResourceID:   "c-1",
		IPAddress:    "192.168.1.1",
		UserAgent:    "Go-http-client/1.1",
		Status:       "success",
		Details:      `{"key":"value"}`,
		CreatedAt:    time.Now(),
	}
	if a.ID != "log-uuid" {
		t.Error("ID mismatch")
	}
	if a.Action != "create" {
		t.Error("Action mismatch")
	}
	if a.Status != "success" {
		t.Error("Status mismatch")
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := Config{}
	if cfg.MaxOpenConns != 0 {
		t.Error("expected zero default for MaxOpenConns")
	}
	if cfg.MaxIdleConns != 0 {
		t.Error("expected zero default for MaxIdleConns")
	}
	if cfg.ConnMaxLifetime != 0 {
		t.Error("expected zero default for ConnMaxLifetime")
	}
}

func TestConfigLogLevels(t *testing.T) {
	levels := []string{"silent", "error", "warn", "info"}
	for _, l := range levels {
		cfg := Config{LogLevel: l}
		if cfg.LogLevel != l {
			t.Errorf("expected %q, got %q", l, cfg.LogLevel)
		}
	}
}
