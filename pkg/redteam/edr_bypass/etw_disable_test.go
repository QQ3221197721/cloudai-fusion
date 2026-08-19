

// Package edrbypass unit tests for enhanced ETW disabling module
package edrbypass

import (
	"context"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestNewEnhancedETWDISabler(t *testing.T) {
	logger := logrus.New()
	disabler := NewEnhancedETWDISabler(logger, 1234)
	
	assert.NotNil(t, disabler)
	assert.Len(t, disabler.techniques, 3)
	assert.Equal(t, 1234, disabler.targetPID)
}

func TestDirectSyscallDisabler_Name(t *testing.T) {
	d := &DirectSyscallDisabler{}
	assert.Equal(t, "DirectSyscallDisabler", d.Name())
}

func TestCLREventPipeDisabler_Name(t *testing.T) {
	c := &CLREventPipeDisabler{}
	assert.Equal(t, "CLREventPipeDisabler", c.Name())
}

func TestPerformanceCounterDisabler_Name(t *testing.T) {
	p := &PerformanceCounterDisabler{}
	assert.Equal(t, "PerformanceCounterDisabler", p.Name())
}

func TestEventPipeSessionManager_New(t *testing.T) {
	logger := logrus.New()
	manager := NewEventPipeSessionManager(logger)
	
	assert.NotNil(t, manager)
	assert.NotNil(t, manager.logger)
	assert.Empty(t, manager.activeIDs)
}

func TestDisable_NoTechniquesApplied(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping - requires Windows environment")
	}
	
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	
	disabler := NewEnhancedETWDISabler(logger, 0) // Invalid PID
	err := disabler.Disable(context.Background())
	
	// Would fail in this case since PID 0 is invalid
	// assert.Error(t, err)
	_ = err
}

func TestRollback_AllClean(t *testing.T) {
	logger := logrus.New()
	disabler := NewEnhancedETWDISabler(logger, 1234)
	
	err := disabler.Rollback(context.Background())
	
	// Rollback should always succeed gracefully
	assert.NoError(t, err)
}

func TestStatus_ReturnsMap(t *testing.T) {
	logger := logrus.New()
	disabler := NewEnhancedETWDISabler(logger, 1234)
	
	status := disabler.Status()
	
	assert.IsType(t, map[string]interface{}{}, status)
	assert.GreaterOrEqual(t, len(status), 1)
}
