package errors

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
)

// ============================================================================
// AppError basics
// ============================================================================

func TestAppError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *AppError
		want string
	}{
		{
			name: "without cause",
			err:  &AppError{Code: CodeNotFound, Message: "cluster not found"},
			want: "[NOT_FOUND] cluster not found",
		},
		{
			name: "with cause",
			err:  &AppError{Code: CodeInternal, Message: "db failure", Cause: fmt.Errorf("connection refused")},
			want: "[INTERNAL_ERROR] db failure: connection refused",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAppError_Unwrap(t *testing.T) {
	cause := fmt.Errorf("root cause")
	appErr := Internal("failed", cause)
	if !errors.Is(appErr, cause) {
		t.Error("Unwrap should expose the cause for errors.Is chain")
	}
}

func TestAppError_WithDetails(t *testing.T) {
	err := NotFound("cluster", "abc")
	err.WithDetail("region", "us-east-1")

	if err.Details["region"] != "us-east-1" {
		t.Errorf("WithDetail did not set key; details = %v", err.Details)
	}
	if err.Details["resource_type"] != "cluster" {
		t.Errorf("WithDetail overwrote existing details; details = %v", err.Details)
	}
}

func TestAppError_WithDetails_Map(t *testing.T) {
	err := New(CodeInternal, 500, "oops")
	err.WithDetails(map[string]interface{}{"foo": "bar"})
	if err.Details["foo"] != "bar" {
		t.Errorf("WithDetails(map) failed; details = %v", err.Details)
	}
}

func TestAppError_WithCause(t *testing.T) {
	cause := fmt.Errorf("io timeout")
	err := ErrTimeout.WithCause(cause)
	if err.Cause != cause {
		t.Error("WithCause did not attach cause")
	}
}

// ============================================================================
// Sentinel errors — errors.Is() comparison
// ============================================================================

func TestSentinelErrors_Is(t *testing.T) {
	tests := []struct {
		name   string
		err    *AppError
		target *AppError
		want   bool
	}{
		{"NotFound matches NotFound", NotFound("cluster", "x"), ErrNotFound, true},
		{"AlreadyExists matches", AlreadyExists("user", "john"), ErrAlreadyExists, true},
		{"Conflict matches", Conflict("busy"), ErrConflict, true},
		{"Unauthorized matches", Unauthorized("bad cred"), ErrUnauthorized, true},
		{"PermissionDenied matches", PermissionDenied("admin"), ErrPermissionDenied, true},
		{"NotFound != Conflict", NotFound("x", "1"), ErrConflict, false},
		{"Internal != NotFound", Internal("oops", nil), ErrNotFound, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errors.Is(tt.err, tt.target); got != tt.want {
				t.Errorf("errors.Is(%v, %v) = %v, want %v", tt.err.Code, tt.target.Code, got, tt.want)
			}
		})
	}
}

// ============================================================================
// Constructors
// ============================================================================

func TestConstructors(t *testing.T) {
	t.Run("NotFound", func(t *testing.T) {
		err := NotFound("pod", "abc-123")
		assertCode(t, err, CodeNotFound)
		assertHTTP(t, err, http.StatusNotFound)
		assertContains(t, err.Message, "pod")
		assertContains(t, err.Message, "abc-123")
	})

	t.Run("AlreadyExists", func(t *testing.T) {
		err := AlreadyExists("user", "alice")
		assertCode(t, err, CodeAlreadyExists)
		assertHTTP(t, err, http.StatusConflict)
	})

	t.Run("Conflict", func(t *testing.T) {
		err := Conflict("resource locked")
		assertCode(t, err, CodeConflict)
		assertHTTP(t, err, http.StatusConflict)
	})

	t.Run("InvalidStateTransition", func(t *testing.T) {
		err := InvalidStateTransition("workload", "running", "pending")
		assertCode(t, err, CodeInvalidState)
		assertHTTP(t, err, http.StatusConflict)
		if err.Details["from"] != "running" || err.Details["to"] != "pending" {
			t.Errorf("details missing from/to; got %v", err.Details)
		}
	})

	t.Run("Validation", func(t *testing.T) {
		err := Validation("invalid input", map[string]string{"name": "required"})
		assertCode(t, err, CodeValidation)
		assertHTTP(t, err, http.StatusBadRequest)
	})

	t.Run("Unauthorized", func(t *testing.T) {
		err := Unauthorized("bad token")
		assertCode(t, err, CodeUnauthorized)
		assertHTTP(t, err, http.StatusUnauthorized)
	})

	t.Run("PermissionDenied", func(t *testing.T) {
		err := PermissionDenied("clusters:delete")
		assertCode(t, err, CodePermissionDenied)
		assertHTTP(t, err, http.StatusForbidden)
	})

	t.Run("Internal", func(t *testing.T) {
		cause := fmt.Errorf("nil pointer")
		err := Internal("crash", cause)
		assertCode(t, err, CodeInternal)
		assertHTTP(t, err, http.StatusInternalServerError)
		if err.Cause != cause {
			t.Error("cause not attached")
		}
	})

	t.Run("Database", func(t *testing.T) {
		err := Database("insert", fmt.Errorf("duplicate key"))
		assertCode(t, err, CodeDatabaseError)
		assertHTTP(t, err, http.StatusInternalServerError)
	})

	t.Run("ServiceUnavailable", func(t *testing.T) {
		err := ServiceUnavailable("auth-service")
		assertCode(t, err, CodeServiceUnavail)
		assertHTTP(t, err, http.StatusServiceUnavailable)
	})

	t.Run("Timeout", func(t *testing.T) {
		err := Timeout("fetchMetrics")
		assertCode(t, err, CodeTimeout)
		assertHTTP(t, err, http.StatusGatewayTimeout)
	})

	t.Run("Upstream", func(t *testing.T) {
		err := Upstream("aws-eks", fmt.Errorf("503"))
		assertCode(t, err, CodeUpstreamError)
		assertHTTP(t, err, http.StatusBadGateway)
	})

	t.Run("New", func(t *testing.T) {
		err := New(CodeRateLimited, http.StatusTooManyRequests, "slow down")
		assertCode(t, err, CodeRateLimited)
		assertHTTP(t, err, http.StatusTooManyRequests)
	})

	t.Run("Newf", func(t *testing.T) {
		err := Newf(CodeQuotaExceeded, http.StatusForbidden, "quota %d exceeded", 100)
		assertCode(t, err, CodeQuotaExceeded)
		assertContains(t, err.Message, "100")
	})
}

// ============================================================================
// Wrap / IsCode / HTTPStatusFromError
// ============================================================================

func TestWrap(t *testing.T) {
	t.Run("nil returns nil", func(t *testing.T) {
		if got := Wrap(nil); got != nil {
			t.Errorf("Wrap(nil) = %v, want nil", got)
		}
	})

	t.Run("AppError passthrough", func(t *testing.T) {
		original := NotFound("cluster", "x")
		wrapped := Wrap(original)
		if wrapped != original {
			t.Error("Wrap should return the same *AppError")
		}
	})

	t.Run("plain error wraps as internal", func(t *testing.T) {
		plain := fmt.Errorf("something failed")
		wrapped := Wrap(plain)
		assertCode(t, wrapped, CodeInternal)
		if wrapped.Cause != plain {
			t.Error("Wrap should preserve the original error as Cause")
		}
	})
}

func TestIsCode(t *testing.T) {
	if !IsCode(NotFound("x", "1"), CodeNotFound) {
		t.Error("IsCode should return true for matching code")
	}
	if IsCode(NotFound("x", "1"), CodeConflict) {
		t.Error("IsCode should return false for non-matching code")
	}
	if IsCode(fmt.Errorf("plain"), CodeInternal) {
		t.Error("IsCode should return false for non-AppError")
	}
}

func TestHTTPStatusFromError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"AppError 404", NotFound("x", "1"), http.StatusNotFound},
		{"AppError 503", ServiceUnavailable("svc"), http.StatusServiceUnavailable},
		{"plain error defaults to 500", fmt.Errorf("oops"), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HTTPStatusFromError(tt.err); got != tt.want {
				t.Errorf("HTTPStatusFromError() = %d, want %d", got, tt.want)
			}
		})
	}
}

// ============================================================================
// Helpers
// ============================================================================

func assertCode(t *testing.T, err *AppError, code ErrorCode) {
	t.Helper()
	if err.Code != code {
		t.Errorf("Code = %s, want %s", err.Code, code)
	}
}

func assertHTTP(t *testing.T, err *AppError, status int) {
	t.Helper()
	if err.HTTPStatus != status {
		t.Errorf("HTTPStatus = %d, want %d", err.HTTPStatus, status)
	}
}

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if len(s) == 0 || len(substr) == 0 {
		t.Errorf("assertContains: empty string")
		return
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return
		}
	}
	t.Errorf("%q does not contain %q", s, substr)
}
