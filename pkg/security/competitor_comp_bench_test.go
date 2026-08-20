//go:build compbench

package security

import (
	"math/rand"
	"strings"
	"testing"

	ahocorasick "github.com/BobuSumisu/aho-corasick"
)

// competitor_comp_bench_test.go benchmarks our Aho-Corasick automaton against the
// real third-party pure-Go library github.com/BobuSumisu/aho-corasick v1.0.3.
//
// Both engines are built from the SAME 10k pattern set and run over the SAME
// text corpus. Patterns and text are lowercased up-front so the two engines do
// byte-identical matching on identical input; ns/op is then the only variable.
//
// Deterministic seeds guarantee both benchmarks see identical patterns & text.
//
// Run: go test ./pkg/security/ -tags compbench -bench=. -benchmem -count=6 -run=^$

const (
	benchTextSize    = 200_000
	benchNumPatterns = 10_000
	patternSeed      = 42
	textSeed         = 1337
)

// buildPatternSet returns 10k unique lowercased pattern strings: the real WAF
// library first, then synthetic tokens padded up to benchNumPatterns. Uses a
// fixed seed so every caller gets the identical set.
func buildPatternSet() []string {
	r := rand.New(rand.NewSource(patternSeed))
	seen := make(map[string]struct{}, benchNumPatterns)
	out := make([]string, 0, benchNumPatterns)

	for _, p := range DefaultWAFPatterns() {
		s := strings.ToLower(p.Pattern)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
		if len(out) >= benchNumPatterns {
			return out
		}
	}

	const alpha = "abcdefghijklmnopqrstuvwxyz0123456789"
	for len(out) < benchNumPatterns {
		l := 4 + r.Intn(8)
		b := make([]byte, l)
		for i := range b {
			b[i] = alpha[r.Intn(len(alpha))]
		}
		s := string(b)
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// buildText builds a lowercased corpus that embeds real patterns at ~5% density
// so both engines find non-trivial match counts. Fixed seed => identical text.
func buildText(patterns []string, size int) string {
	r := rand.New(rand.NewSource(textSeed))
	var sb strings.Builder
	sb.Grow(size + 32)
	const filler = "abcdefghijklmnopqrstuvwxyz0123456789 /?&=.-_"
	for sb.Len() < size {
		if r.Intn(20) == 0 && len(patterns) > 0 {
			sb.WriteString(patterns[r.Intn(len(patterns))])
		} else {
			sb.WriteByte(filler[r.Intn(len(filler))])
		}
	}
	return strings.ToLower(sb.String()[:size])
}

// BenchmarkOurAC_Search runs our automaton on the shared 10k patterns / text.
func BenchmarkOurAC_Search(b *testing.B) {
	patterns := buildPatternSet()
	text := buildText(patterns, benchTextSize)
	ac := NewAhoCorasick()
	for _, p := range patterns {
		ac.AddPattern(ACPattern{Pattern: p, ID: p})
	}
	ac.Build()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ac.Search(text)
	}
}

// BenchmarkBobuSumisuAC_Search runs the real competitor library on the same input.
func BenchmarkBobuSumisuAC_Search(b *testing.B) {
	patterns := buildPatternSet()
	text := buildText(patterns, benchTextSize)
	trie := ahocorasick.NewTrieBuilder().AddStrings(patterns).Build()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = trie.MatchString(text)
	}
}

// BenchmarkOurAC_Build measures our automaton construction over 10k patterns.
func BenchmarkOurAC_Build(b *testing.B) {
	patterns := buildPatternSet()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ac := NewAhoCorasick()
		for _, p := range patterns {
			ac.AddPattern(ACPattern{Pattern: p, ID: p})
		}
		ac.Build()
	}
}

// BenchmarkBobuSumisuAC_Build measures competitor trie construction over 10k patterns.
func BenchmarkBobuSumisuAC_Build(b *testing.B) {
	patterns := buildPatternSet()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ahocorasick.NewTrieBuilder().AddStrings(patterns).Build()
	}
}

// TestMatchCount_OurACVsBobuSumisu sanity-checks both engines find comparable
// match counts on identical input. Semantics may differ (dedup / overlap
// handling), so this logs rather than asserts strict equality.
func TestMatchCount_OurACVsBobuSumisu(t *testing.T) {
	patterns := buildPatternSet()
	text := buildText(patterns, 20_000)

	ac := NewAhoCorasick()
	for _, p := range patterns {
		ac.AddPattern(ACPattern{Pattern: p, ID: p})
	}
	ac.Build()
	ourN := len(ac.Search(text))

	trie := ahocorasick.NewTrieBuilder().AddStrings(patterns).Build()
	theirN := len(trie.MatchString(text))

	t.Logf("Patterns:             %d", len(patterns))
	t.Logf("Text size:            %d bytes", len(text))
	t.Logf("Our AC matches:       %d", ourN)
	t.Logf("BobuSumisu matches:   %d", theirN)
	if theirN != 0 {
		t.Logf("Ratio (Us/Them):      %.4f", float64(ourN)/float64(theirN))
	}
}
