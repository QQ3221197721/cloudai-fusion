// Package kerberos_asn1 unit tests for encoding/decoding operations
package kerberos_asn1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestEncoder_New(t *testing.T) {
	enc := NewEncoder()
	
	assert.NotNil(t, enc)
	assert.NotNil(t, enc.buffer)
	assert.Nil(t, enc.error)
	assert.Equal(t, 0, len(enc.buffer))
}

func TestEncodeLength_ShortForm(t *testing.T) {
	tests := []struct {
		length int
		expect byte
	}{
		{0x00, 0x00},
		{0x42, 0x42},
		{0x7F, 0x7F},
	}
	
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			enc := NewEncoder()
			enc.encodeLength(tt.length)
			
			data, _ := enc.GetBuffer()
			assert.Len(t, data, 1)
			assert.Equal(t, tt.expect, data[0])
		})
	}
}

func TestEncodeLength_LongForm(t *testing.T) {
	tests := []struct {
		length int
		minLen int
	}{
		{256, 2},   // Requires 1+1 bytes
		{1000, 3},  // Requires 1+2 bytes  
		{1000000, 5}, // Requires 1+4 bytes
	}
	
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			enc := NewEncoder()
			enc.encodeLength(tt.length)
			
			data, _ := enc.GetBuffer()
			assert.GreaterOrEqual(t, len(data), tt.minLen)
		})
	}
}

func TestDecodeInteger_Positive(t *testing.T) {
	tests := []struct {
		input    []byte
		expected int64
	}{
		{[]byte{0x00}, 0},
		{[]byte{0x01}, 1},
		{[]byte{0xFF}, 255},
		{[]byte{0x01, 0x00}, 256},
	}
	
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result, err := DecodeInteger(tt.input)
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDecodeInteger_Negative(t *testing.T) {
	// MSB set = negative number
	input := []byte{0x80} // -128 in signed byte
	result, err := DecodeInteger(input)
	assert.NoError(t, err)
	assert.Equal(t, int64(-128), result)
}

func TestEncodeUTCTime_Valid(t *testing.T) {
	t := time.Date(2026, 8, 5, 14, 30, 45, 0, time.UTC)
	result := EncodeUTCTime(t)
	
	// Should be exactly 13 bytes: YYMMDDHHMMSSZ
	assert.Len(t, result, 13)
	assert.Contains(t, string(result), "260805") // Date check
	assert.Contains(t, string(result), "143045Z") // Time check with Z
}

func TestDecodeUTCTime_Valid(t *testing.T) {
	input := []byte("260805143045Z")
	result, err := DecodeUTCTime(input)
	
	assert.NoError(t, err)
	assert.Equal(t, 2026, result.Year())
	assert.Equal(t, 8, int(result.Month()))
	assert.Equal(t, 5, result.Day())
	assert.Equal(t, 14, result.Hour())
	assert.Equal(t, 30, result.Minute())
	assert.Equal(t, 45, result.Second())
}

func TestEncodeInteger_BigEndianConversion(t *testing.T) {
	input := int64(0x123456789ABCDEF0)
	result := EncodeInteger(input)
	
	// Result should be big-endian
	assert.Len(t, result, 8)
	assert.Equal(t, byte(0x12), result[0])
	assert.Equal(t, byte(0x34), result[1])
}

func TestEncodeOID_Simple(t *testing.T) {
	oid := []int{1, 2, 840, 113549} // iso.org.dod
	
	result := EncodeOID(oid)
	
	assert.NotEmpty(t, result)
	assert.Len(t, result, 5) // Minimum length for this OID
}

func TestEncodeOID_Complex(t *testing.T) {
	oid := []int{1, 2, 840, 113549, 1, 1, 1} // pkcs-1
	
	result := EncodeOID(oid)
	
	assert.NotEmpty(t, result)
	// First two components: 1*40+2 = 42
	assert.Equal(t, byte(42), result[0])
}

func TestDecodeBitString(t *testing.T) {
	input := []byte{0x00, 0xDE, 0xAD, 0xBE, 0xEF} // 0 unused bits
	
	result, err := DecodeBitString(input)
	
	assert.NoError(t, err)
	assert.Equal(t, []byte{0xDE, 0xAD, 0xBE, 0xEF}, result)
}

func TestDecodeBitString_WithUnused(t *testing.T) {
	input := []byte{0x03, 0xFF} // 3 unused bits
	// After unmasking: 0b11100000 & 0xFF = 0xE0
	
	result, err := DecodeBitString(input)
	
	assert.NoError(t, err)
	assert.Len(t, result, 1)
	// Check that unused bits are removed
	assert.Equal(t, byte(0xE0&0b00111111), result[0])
}

func TestEncodePrintableString_Valid(t *testing.T) {
	input := "John-Doe@Example.COM"
	result := EncodePrintableString(input)
	
	assert.NotNil(t, result)
	assert.Equal(t, input, string(result))
}

func TestEncodePrintableString_Invalid(t *testing.T) {
	// Unicode character not allowed in PrintableString
	input := "Testñ"
	result := EncodePrintableString(input)
	
	assert.Nil(t, result) // Invalid characters should return nil
}

func TestEncodeUTF8String_AllCharacters(t *testing.T) {
	input := "Hello 世界 🌍"
	result := EncodeUTF8String(input)
	
	assert.NotNil(t, result)
	assert.Equal(t, []byte(input), result)
}

func TestEncodeIA5String_ASCII(t *testing.T) {
	input := "test@example.com"
	result := EncodeIA5String(input)
	
	assert.NotNil(t, result)
	assert.Equal(t, input, string(result))
}

func TestEncodeIA5String_NotASCII(t *testing.T) {
	input := "testñexample.com" // ñ is not ASCII
	result := EncodeIA5String(input)
	
	assert.Nil(t, result) // Non-ASCII not allowed
}
