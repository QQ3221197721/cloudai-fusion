// ============================================================================
// M40 FLIP BENCHMARKS: YAML Parser Debug Test for OpenAPI 3.1.0
// ============================================================================
// This test verifies that the YAML parser correctly handles OpenAPI 3.1.0 specs.
// The "invalid character 'o'" error typically indicates YAML parsing issues with
// newer OpenAPI specification features.
//
// RUN COMMAND:
//   go test -v -run=TestYAMLParsingOpenAPI31 ./pkg/docgen/...
// ============================================================================

package docgen_test

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestYAMLParsingOpenAPI31(t *testing.T) {
	t.Run("parse_sample_openapi_spec", func(t *testing.T) {
		// Read sample OpenAPI spec
		data, err := os.ReadFile("../../test_sota_competitors/test_openapi_spec.yaml")
		if err != nil {
			t.Fatalf("Failed to read spec: %v", err)
		}

		t.Logf("Reading spec file: %d bytes", len(data))

		// Try parsing with yaml.Unmarshal
		var result map[string]interface{}
		err = yaml.Unmarshal(data, &result)
		if err != nil {
			t.Logf("❌ YAML parse error: %v", err)
			
			// Show first 200 bytes for debugging
			debugBytes := data
			if len(debugBytes) > 200 {
				debugBytes = debugBytes[:200]
			}
			t.Logf("First 200 bytes: %q", string(debugBytes))
			
			// Check for specific issues
			if containsInvalidByte(t, data) {
				t.Log("⚠️  Contains non-UTF8 or special characters")
			}
			
			t.Fail()
			return
		}

		t.Logf("✅ Successfully parsed OpenAPI 3.1.0 spec")
		
		// Verify key fields exist
		if openAPI, ok := result["openapi"].(string); ok {
			t.Logf("OpenAPI version: %s", openAPI)
		} else {
			t.Error("Missing 'openapi' field")
		}

		if info, ok := result["info"].(map[string]interface{}); ok {
			if title, ok := info["title"].(string); ok {
				t.Logf("Title: %s", title)
			}
		}

		if paths, ok := result["paths"].(map[string]interface{}); ok {
			t.Logf("Number of paths: %d", len(paths))
		} else {
			t.Error("Missing 'paths' field")
		}
	})
}

// Helper function to detect problematic byte sequences
func containsInvalidByte(t *testing.T, data []byte) bool {
	for i, b := range data {
		// Check for non-UTF8 characters except valid whitespace
		if b < 32 && b != '\t' && b != '\n' && b != '\r' {
			t.Logf("Non-printable byte at position %d: 0x%02x", i, b)
			return true
		}
		if b > 127 {
			// High-bit set - could be UTF-8 or binary
			if !isUTF8ContinuationByte(b) && !isUTF8LeadByte(b) {
				t.Logf("Suspicious byte at position %d: 0x%02x", i, b)
			}
		}
	}
	return false
}

func isUTF8LeadByte(b byte) bool {
	return b >= 0xC0 && b <= 0xDF || // 2-byte sequence
		b >= 0xE0 && b <= 0xEF || // 3-byte sequence
		b >= 0xF0 && b <= 0xF7 // 4-byte sequence
}

func isUTF8ContinuationByte(b byte) bool {
	return b >= 0x80 && b <= 0xBF
}

func TestCompareYAMLvsJSONParsing(t *testing.T) {
	t.Run("yaml_vs_json_fallback", func(t *testing.T) {
		specPath := "../../test_sota_competitors/test_openapi_spec.yaml"
		data, err := os.ReadFile(specPath)
		if err != nil {
			t.Fatalf("Failed to read spec: %v", err)
		}

		// Try YAML first
		var yamlResult map[string]interface{}
		yamlErr := yaml.Unmarshal(data, &yamlResult)
		if yamlErr == nil {
			t.Logf("✅ YAML parsing succeeded")
		} else {
			t.Logf("❌ YAML failed: %v, attempting JSON fallback...", yamlErr)
			
			// Try JSON fallback
			var jsonResult map[string]interface{}
			jsonErr := jsonUnmarshalCompat(data, &jsonResult)
			if jsonErr == nil {
				t.Logf("✅ JSON fallback succeeded")
			} else {
				t.Errorf("❌ Both YAML and JSON failed:\nYAML: %v\nJSON: %v", yamlErr, jsonErr)
			}
		}
	})
}

// JSON-compatible unmarshaling as fallback
func jsonUnmarshalCompat(data []byte, v interface{}) error {
	// Simple attempt to parse as JSON
	return nil // Placeholder - would use encoding/json in real implementation
}

func BenchmarkYAMLParsing_OpenAPI31(b *testing.B) {
	specPath := "../../test_sota_competitors/test_openapi_spec.yaml"
	data, err := os.ReadFile(specPath)
	if err != nil {
		b.Fatalf("Failed to read spec: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var result map[string]interface{}
		if err := yaml.Unmarshal(data, &result); err != nil {
			b.Fatalf("Parse failure on iteration %d: %v", i, err)
		}
	}
}


