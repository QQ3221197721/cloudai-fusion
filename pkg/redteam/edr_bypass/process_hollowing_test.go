// Package edrbypass unit tests for process hollowing module
package edrbypass

import (
	"context"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestNewRobustProcessHollower(t *testing.T) {
	logger := logrus.New()
	shellcode := []byte{0xCC, 0xCC}
	hollower := NewRobustProcessHollower(shellcode, logger)
	
	assert.NotNil(t, hollower)
	assert.Equal(t, shellcode, hollower.shellcode)
	assert.False(t, hollower.hollowed)
}

func TestHollow_RequiresShellcode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping - requires Windows environment")
	}
	
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	
	hollower := NewRobustProcessHollower(nil, logger) // No shellcode
	
	err := hollower.Hollow(context.Background())
	// Would fail due to missing shellcode
	assert.Error(t, err)
}

func TestIsHollowed_NotStarted(t *testing.T) {
	logger := logrus.New()
	hollower := NewRobustProcessHollower([]byte{0x90}, logger)
	
	isHollowed := hollower.IsHollowed()
	assert.False(t, isHollowed)
}

func TestFixPEHeadersForX64_AppliesAlignment(t *testing.T) {
	logger := logrus.New()
	hollower := NewRobustProcessHollower([]byte{0x90}, logger)
	
	// Setup mock data
	hollower.targetInfo = TargetInfo{
		Sections: []Section{
			{VirtualAddress: 0x1000, SizeOfRawData: 0x200},
		},
	}
	
	err := hollower.fixPEHeadersForX64()
	
	assert.NoError(t, err)
	// After alignment: should be aligned to 0x1000 boundary
	assert.Equal(t, uint32(0x1000), hollower.targetInfo.Sections[0].VirtualAddress)
}

func TestWriteShellcode_TooLarge(t *testing.T) {
	logger := logrus.New()
	hollower := NewRobustProcessHollower(make([]byte, 1000000), logger) // Very large shellcode
	
	hollower.targetInfo = TargetInfo{
		ImageSize:   1024, // Small target
	}
	
	err := hollower.writeShellcode()
	assert.Error(t, err)
}

func TestFixImportTable_Empty(t *testing.T) {
	logger := logrus.New()
	hollower := NewRobustProcessHollower([]byte{0x90}, logger)
	
	hollower.targetInfo = TargetInfo{
		ImportTable: nil,
	}
	
	err := hollower.fixImportTable()
	assert.NoError(t, err)
	// Should create minimal IAT
	assert.NotNil(t, hollower.targetInfo.IAT)
}

func TestRevertRemovals_CleansState(t *testing.T) {
	logger := logrus.New()
	hollower := NewRobustProcessHollower([]byte{0x90}, logger)
	
	// Pretend hollowing was successful
	hollowerr := hollower
	hollowerr.hollowed = true
	
	err := hollower.RevertRemovals()
	assert.NoError(t, err)
	
	// State should be reset
	assert.False(t, hollower.hollowed)
}
