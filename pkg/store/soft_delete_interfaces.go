package store

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// INTERFACES FOR SOFT DELETE FUNCTIONALITY
// ============================================================================

// DatabaseConnection abstracts database operations
type DatabaseConnection interface {
	UpdateEntity(ctx context.Context, entity SoftDeletable) error
	GetEntityByID(ctx context.Context, tableName, id string) (interface{}, error)
}

// UserIDProvider provides current user information
type UserIDProvider interface {
	GetCurrentUser(ctx context.Context) *User
}

// ReasonValidator validates deletion reasons
type ReasonValidator interface {
	Validate(reason string) error
}

// Logger provides logging functionality
type Logger interface {
	WithField(key string, value interface{}) Logger
	WithError(err error) Logger
	Info(args ...interface{})
	Warn(args ...interface{})
	Error(args ...interface{})
	Debug(args ...interface{})
}

// UserContext represents user information in context
type UserContext struct {
	ID    string
	Email string
}

// ============================================================================
// UTILITY FUNCTIONS FOR CONTEXT EXTRACTION
// ============================================================================

// getCurrentIP extracts client IP from HTTP request context
func getCurrentIP(ctx context.Context) string {
	if ip := ctx.Value("ip"); ip != nil {
		if ipStr, ok := ip.(string); ok {
			return ipStr
		}
	}
	return "unknown"
}

// getUserAgent extracts user agent from HTTP request context
func getUserAgent(ctx context.Context) string {
	if ua := ctx.Value("user_agent"); ua != nil {
		if uaStr, ok := ua.(string); ok {
			return uaStr
		}
	}
	return ""
}

// getSessionID extracts session ID from HTTP request context
func getSessionID(ctx context.Context) string {
	if sid := ctx.Value("session_id"); sid != nil {
		if sidStr, ok := sid.(string); ok {
			return sidStr
		}
	}
	return ""
}

// getRequestID extracts request ID from HTTP request context
func getRequestID(ctx context.Context) string {
	if rid := ctx.Value("request_id"); rid != nil {
		if ridStr, ok := rid.(string); ok {
			return ridStr
		}
	}
	return ""
}

// ============================================================================
// MOCK IMPLEMENTATIONS FOR TESTING
// ============================================================================

// MockUserIDProvider implements UserIDProvider for testing
type MockUserIDProvider struct {
	User *User
}

func (m *MockUserIDProvider) GetCurrentUser(ctx context.Context) *User {
	return m.User
}

// MockReasonValidator implements basic reason validation
type MockReasonValidator struct{}

func (m *MockReasonValidator) Validate(reason string) error {
	if len(reason) < 3 {
		return &ValidationErrorStruct{Field: "reason", Message: "must be at least 3 characters"}
	}
	return nil
}

// MockLogger implements Logger for testing
type MockLogger struct {
	Entries []logrus.Entry
}

func (m *MockLogger) WithField(key string, value interface{}) Logger {
	return m
}

func (m *MockLogger) WithError(err error) Logger {
	return m
}

func (m *MockLogger) Info(args ...interface{}) {
	m.Entries = append(m.Entries, logrus.Entry{Message: args[0].(string)})
}

func (m *MockLogger) Warn(args ...interface{}) {
	m.Entries = append(m.Entries, logrus.Entry{Message: args[0].(string)})
}

func (m *MockLogger) Error(args ...interface{}) {
	m.Entries = append(m.Entries, logrus.Entry{Message: args[0].(string)})
}

func (m *MockLogger) Debug(args ...interface{}) {
	m.Entries = append(m.Entries, logrus.Entry{Message: args[0].(string)})
}
