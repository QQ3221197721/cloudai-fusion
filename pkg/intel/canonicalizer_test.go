package intel

import (
	"testing"
)

func TestDomainNormalizer_DictionaryBased(t *testing.T) {
	norm := NewDomainNormalizer()
	
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"leet_zero", "g00gle.com", "google.com"},        // 0->o homoglyph, canonical keys match
		{"double_o", "gooogle.com", "google.com"},         // duplicate-char collapse
		{"leet_at_apple", "@pple.com", "apple.com"},        // @->a homoglyph
		{"leet_three", "appl3.com", "apple.com"},           // 3->e homoglyph
		{"already_clean", "google.com", "google.com"},      // exact, unchanged
		{"unrelated_domain", "random.site", "random.site"}, // must NOT merge
		// Honest negatives: strict mode does NOT merge non-homoglyph edits.
		{"non_homoglyph_1", "1oogle.com", "1oogle.com"},    // 1->i, key != google key; no merge
		{"near_miss_legit", "apply.com", "apply.com"},      // legit near-miss; must NOT merge to apple.com
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := norm.Normalize(tt.input)
			
			if result != tt.expected {
				t.Errorf("Normalize(%q) = %q, want %q", tt.input, result, tt.expected)
			} else {
				t.Logf("✓ Normalize(%q) → %q", tt.input, result)
			}
		})
	}
}

func BenchmarkCanonicalizer_Normalize(b *testing.B) {
	norm := NewDomainNormalizer()
	samples := []string{
		"g00gle.com",
		"8.8.8.8/32",
		"E3B0C44298FC1C149AFBF4C8996FB924",
	}
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		_ = norm.Normalize(samples[i%len(samples)])
	}
}
