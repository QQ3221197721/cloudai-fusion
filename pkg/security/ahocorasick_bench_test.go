package security

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// ============================================================================
// Performance Benchmarks - AC Automaton vs Regex Baseline
//
// Fairness contract for the scaling benchmarks (100 / 1000 / 10000 rules):
//   - All variants search the SAME input text (benchInput); only the number
//     of patterns changes. This makes the AC vs regexp comparison and the
//     scaling curve honest and reproducible.
//   - Each automaton / regexp set is built with EXACTLY N patterns.
//   - Timers are reset after construction so we measure per-request match cost,
//     not one-time build cost (build cost is measured separately below).
// ============================================================================

// benchInput is a realistic mixed WAF inspection payload embedding several
// genuine attack vectors (SQLi, XSS, path traversal, RCE, SSRF, scanner UA)
// plus benign padding, mirroring a real HTTP request line + headers.
const benchInput = "GET /api/v1/users?id=1' OR '1'='1&next=/admin/../../etc/passwd " +
	"User-Agent: sqlmap/1.5 <script>alert(document.cookie)</script> " +
	"; cat /etc/shadow http://169.254.169.254/latest/meta-data/ " +
	"referer=https://app.internal/console some benign padding content here and there"

// generateSyntheticPatterns produces distinct non-colliding filler patterns so
// the pattern set can be scaled to an exact count for benchmarking.
func generateSyntheticPatterns(count int, prefix string) []ACPattern {
	pats := make([]ACPattern, count)
	for i := 0; i < count; i++ {
		pats[i] = ACPattern{
			Pattern:  fmt.Sprintf("%s-attack-%06d", prefix, i),
			Category: "synthetic",
			Security: "low",
			ID:       fmt.Sprintf("synth-%s-%d", prefix, i),
		}
	}
	return pats
}

// buildBenchPatterns returns EXACTLY n patterns. The real DefaultWAFPatterns
// come first (so genuine attack signatures are always present in the set),
// padded with synthetic patterns to reach n, or truncated to n.
func buildBenchPatterns(n int) []ACPattern {
	base := DefaultWAFPatterns()
	if len(base) >= n {
		return base[:n]
	}
	out := make([]ACPattern, 0, n)
	out = append(out, base...)
	out = append(out, generateSyntheticPatterns(n-len(base), "synth")...)
	return out
}

// compileLiteralRegexps compiles each pattern as a case-insensitive literal
// regexp, mirroring how a regex-based WAF scans for each signature. This is the
// honest O(N*M) linear-scan baseline that Aho-Corasick replaces.
func compileLiteralRegexps(pats []ACPattern) []*regexp.Regexp {
	res := make([]*regexp.Regexp, 0, len(pats))
	for _, p := range pats {
		if re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(p.Pattern)); err == nil {
			res = append(res, re)
		}
	}
	return res
}

// ============================================================================
// Aho-Corasick scaling benchmarks (search cost per request)
// ============================================================================

func benchmarkAhoCorasick(b *testing.B, n int) {
	ac := NewAhoCorasick()
	ac.AddPatterns(buildBenchPatterns(n))
	ac.Build()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		// Search lowercases internally, so we pass the raw request text.
		_ = ac.Search(benchInput)
	}
}

func BenchmarkAhoCorasick_100Rules(b *testing.B)   { benchmarkAhoCorasick(b, 100) }
func BenchmarkAhoCorasick_1000Rules(b *testing.B)  { benchmarkAhoCorasick(b, 1000) }
func BenchmarkAhoCorasick_10000Rules(b *testing.B) { benchmarkAhoCorasick(b, 10000) }

// ============================================================================
// Regexp baseline benchmarks (same input, same pattern count)
// ============================================================================

func benchmarkRegexp(b *testing.B, n int) {
	regexps := compileLiteralRegexps(buildBenchPatterns(n))

	b.ResetTimer()
	b.ReportAllocs()
	var matches int
	for i := 0; i < b.N; i++ {
		for _, re := range regexps {
			if re.MatchString(benchInput) {
				matches++
			}
		}
	}
	_ = matches
}

func BenchmarkRegexp_100Rules(b *testing.B)   { benchmarkRegexp(b, 100) }
func BenchmarkRegexp_1000Rules(b *testing.B)  { benchmarkRegexp(b, 1000) }
func BenchmarkRegexp_10000Rules(b *testing.B) { benchmarkRegexp(b, 10000) }

// ============================================================================
// Direct AC vs Regexp comparison on an identical multi-attack payload
// ============================================================================

func BenchmarkAhoCorasick_vs_Regexp_Comparative(b *testing.B) {
	pats := buildBenchPatterns(1000)

	ac := NewAhoCorasick()
	ac.AddPatterns(pats)
	ac.Build()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ac.Search(benchInput)
	}
}

// ============================================================================
// Build-time micro-benchmarks (one-time automaton construction cost)
// ============================================================================

func benchmarkBuild(b *testing.B, n int) {
	pats := buildBenchPatterns(n)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ac := NewAhoCorasick()
		ac.AddPatterns(pats)
		ac.Build()
	}
}

func BenchmarkAhoCorasick_BuildTime_100Rules(b *testing.B)   { benchmarkBuild(b, 100) }
func BenchmarkAhoCorasick_BuildTime_1000Rules(b *testing.B)  { benchmarkBuild(b, 1000) }
func BenchmarkAhoCorasick_BuildTime_10000Rules(b *testing.B) { benchmarkBuild(b, 10000) }

// ============================================================================
// Search micro-benchmarks (single / multiple / no match)
// ============================================================================

func BenchmarkAhoCorasick_Search_SingleMatch(b *testing.B) {
	ac := NewAhoCorasick()
	ac.AddPatterns(DefaultWAFPatterns())
	ac.Build()

	// Contains the literal signature "' or '1'='1" (after "admin").
	simplePayload := "login=admin' or '1'='1"
	if len(ac.Search(simplePayload)) == 0 {
		b.Fatal("setup error: payload should match at least one pattern")
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if len(ac.Search(simplePayload)) == 0 {
			b.Fatal("expected at least one match")
		}
	}
}

func BenchmarkAhoCorasick_Search_MultipleMatches(b *testing.B) {
	ac := NewAhoCorasick()
	ac.AddPatterns(DefaultWAFPatterns())
	ac.Build()

	multiPayload := "'; UNION SELECT username, password FROM users--" +
		"<img src=x onerror=alert(1)>" +
		"../../../etc/passwd; cat /etc/shadow" +
		"http://169.254.169.254/metadata"
	if len(ac.Search(multiPayload)) < 2 {
		b.Fatal("setup error: payload should match multiple patterns")
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if len(ac.Search(multiPayload)) == 0 {
			b.Fatal("expected multiple matches")
		}
	}
}

func BenchmarkAhoCorasick_Search_NoMatch(b *testing.B) {
	ac := NewAhoCorasick()
	ac.AddPatterns(DefaultWAFPatterns())
	ac.Build()

	cleanPayload := "this is a completely safe and normal request with no attacks"

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if results := ac.Search(cleanPayload); results != nil {
			b.Errorf("expected no matches, got %d", len(results))
		}
	}
}

// ============================================================================
// Stress: long input scanning (throughput characteristic)
// ============================================================================

func BenchmarkAhoCorasick_Stress_LongInput_1kChars(b *testing.B) {
	ac := NewAhoCorasick()
	ac.AddPatterns(buildBenchPatterns(1000))
	ac.Build()

	longInput := strings.Repeat("abcdefghijklmnopqrstuvwxyz0123456789 ", 28) // ~1k chars

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ac.Search(longInput)
	}
}

func BenchmarkAhoCorasick_Stress_LongInput_10kChars(b *testing.B) {
	ac := NewAhoCorasick()
	ac.AddPatterns(buildBenchPatterns(1000))
	ac.Build()

	longInput := strings.Repeat("long-running-web-request-with-many-parameters=value&", 200) // ~10k chars

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ac.Search(longInput)
	}
}
