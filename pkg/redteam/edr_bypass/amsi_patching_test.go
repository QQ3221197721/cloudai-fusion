

// Package edrbypass unit tests for AMSI patching module
package edrbypass

import (
	"context"
	"runtime"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"golang.org/x/sys/windows"
)

// requireAdminPrivileges skips the test when the process lacks the Windows
// privileges needed to open/patch another process. AMSI work requires
// OpenProcess with PROCESS_VM_OPERATION|PROCESS_VM_READ|PROCESS_VM_WRITE,
// which non-elevated tokens cannot obtain for arbitrary target processes.
func requireAdminPrivileges(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		return // POSIX paths use different mechanisms; keep original behavior
	}
	if !hasElevatedToken() {
		t.Skipf("requires elevated Windows token for AMSI patching; run as admin for real coverage")
	}
}

// hasElevatedToken reports whether the current process token is elevated
// (UAC-admin), probed via OpenProcessToken + TokenElevation from
// golang.org/x/sys/windows.
func hasElevatedToken() bool {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return false
	}
	defer token.Close()
	return token.IsElevated()
}

func TestNewAMSIPatcher_Defaults(t *testing.T) {
	logger := logrus.New()
	patcher := NewAMSIPatcher(logger, 1234)
	
	assert.NotNil(t, patcher)
	assert.Equal(t, uintptr(1234), patcher.targetProcess)
	assert.False(t, patcher.restoreNeeded)
}

func TestInitialize_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}
	requireAdminPrivileges(t)
	
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	
	patcher := NewAMSIPatcher(logger, 1234)
	err := patcher.Initialize(context.Background())
	
	// This test would require actual Windows process - skipping in demo
	assert.NoError(t, err) // or expected error depending on environment
}

func TestAMSIResult_String(t *testing.T) {
	tests := []struct {
		result   AMSIResult
		expected string
	}{
		{AMSI_RESULT_UNKNOWN, "UNKNOWN"},
		{AMSI_RESULT_NOT_DETECTED, "NOT_DETECTED"},
		{AMSI_RESULT_DETECTED, "DETECTED"},
		{AMSI_RESULT_ABORTED_BY_USER, "ABORTED_BY_USER"},
		{999, "RESULT_999"},
	}
	
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			str := tt.result.String()
			assert.Equal(t, tt.expected, str)
		})
	}
}

func TestIsPatched_Uninitialized(t *testing.T) {
	logger := logrus.New()
	patcher := NewAMSIPatcher(logger, 0)
	
	isPatched := patcher.IsPatched()
	assert.False(t, isPatched)
}

func TestGetAMSIStatus_Uninitialized(t *testing.T) {
	logger := logrus.New()
	patcher := NewAMSIPatcher(logger, 0)
	
	status := patcher.GetAMSIStatus()
	assert.Equal(t, "UNINITIALIZED", status)
}

func TestErrWin32_ErrorFormat(t *testing.T) {
	err := ErrWin32(1234)
	
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "0x4d2")
}

func TestEqualBytes_Equal(t *testing.T) {
	a := []byte{0x01, 0x02, 0x03}
	b := []byte{0x01, 0x02, 0x03}
	
	assert.True(t, equalBytes(a, b))
}

func TestEqualBytes_NotEqual(t *testing.T) {
	a := []byte{0x01, 0x02, 0x03}
	b := []byte{0x01, 0x02, 0x04}
	
	assert.False(t, equalBytes(a, b))
}

func TestNewAMSIDisableViaCOM(t *testing.T) {
	logger := logrus.New()
	disabler := NewAMSIDisableViaCOM(logger)
	
	assert.NotNil(t, disabler)
	assert.Equal(t, logger.WithField("component", "amsi_com_unloader"), disabler.logger)
}

func TestNewAMSIMemorySanitizer(t *testing.T) {
	logger := logrus.New()
	sanitizer := NewAMSIMemorySanitizer(logger)
	
	assert.NotNil(t, sanitizer)
	assert.Equal(t, logger.WithField("component", "amsi_sanitizer"), sanitizer.logger)
}

func TestSanitize_MemoryPatternRemoval(t *testing.T) {
	logger := logrus.New()
	sanitizer := NewAMSIMemorySanitizer(logger)
	
	testData := []byte{0x60, 0x8B, 0xEC, 0x83, 0x90, 0xCC}
	result, err := sanitizer.Sanitize(testData)
	
	assert.NoError(t, err)
	assert.Len(t, result, len(testData))
}

func TestMatchesSignature(t *testing.T) {
	tests := []struct {
		bytes    []byte
		expected bool
	}{
		{[]byte{0x60, 0x8B, 0xEC, 0x83}, true},
		{[]byte{0xCC, 0xCC, 0xCC, 0xCC}, true},
		{[]byte{0x00, 0x00, 0x00, 0x00}, false},
	}
	
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := matchesSignature(tt.bytes)
			assert.Equal(t, tt.expected, result)
		})
	}
}
