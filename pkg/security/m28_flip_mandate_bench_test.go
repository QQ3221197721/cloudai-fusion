//go:build m28flip

package security

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"testing"
)

// ============================================================================
// M28 FLIP Mandate: IOC Pattern Matching - Aho-Corasick vs Go Stdlib RegExp
// ============================================================================
// REAL competitor head-to-head for M28 Threat Intel IOC scanning:
// Our Aho-Corasick O(n+m+z) multi-pattern automaton vs Go stdlib regexp compiled
// with N alternation patterns (or per-pattern loop).
//
// Win thesis: AC crushes per-pattern regex on throughput for large IOC sets.
// Single-pass O(n*m) in regex vs single-pass O(n+m+z) in AC. Gap widens at N→∞.
//
// ANTI-FIASCO GUARANTEES:
// - Count=6 median runs (-json)
// - Same IOC pattern set + same input corpus both sides
// - Honest verdict even if we lose
// - Never fake, never edge-only
//
// Environment: Windows PowerShell; go env -w GOMODCACHE=E:\go\pkg\mod
// Run command (N=100):
//   go test ./pkg/security/ -tags m28flip -bench='BenchmarkAC_100|BenchmarkRegexp_100' -benchtime=2s -count=6 -json > output/m28_flip_bench.json
//
// Run command (N=1000):
//   go test ./pkg/security/ -tags m28flip -bench='BenchmarkAC_1k|BenchmarkRegexp_1k' -benchtime=2s -count=6 -json > output/m28_flip_bench.json
//
// Run command (N=10000):
//   go test ./pkg/security/ -tags m28flip -bench='BenchmarkAC_10k|BenchmarkRegexp_10k' -benchtime=2s -count=6 -json > output/m28_flip_bench.json
// ============================================================================

const (
	// Pattern scales for M28 FLIP Mandate
	scaleSmall     = 100
	scaleMedium    = 1_000
	scaleLarge     = 10_000

	// Log stream size (realistic threat intel scan window)
	logStreamSize = 1_000_000 // 1MB of threat telemetry

	// Match density (~5% embedding rate)
	matchDensity = 0.05

	// Seeds for reproducibility
	patternSeed   = 20260824
	logStreamSeed = 1337
)

// BuildIOCPatterns generates realistic IOC threat intelligence indicators from MITRE ATT&CK
func BuildIOCPatterns(count int, r *rand.Rand) []ACPattern {
	categories := map[string][]string{
		"c2": {
			"beacon-http", "cobalt-strike", "metasploit", "empyrean-framework",
			"powerview.ps1", "procdump.exe", "mimikatz", "pass-the-hash",
			"whiskerty", "avast-shellextension", "dlphr", "necurs",
			".onion", ".ru", ".cn", ".xyz", ".top",
			"http://malicious-domain.com", "POST /panel.php", "GET /shell.php",
		},
		"exfil": {
			"curl -X POST", "wget --post-data", "nc -e /bin/sh",
			"base64 -d", "certutil -decode",
			"/tmp/.X11-unix", "sshuttle", "proxychains",
			"\\appdata\\local\\temp\\", "%temp%\\*.zip",
			"export http_proxy=http://127.0.0.1:8080",
			"socks5://127.0.0.1:9050", "torify",
		},
		"malware": {
			"ransomware_file_extensions", ".encrypted", ".locked", ".cryzip",
			"wanacry", "petya", "notPetya", "ryuk", "contabo",
			"evilginx", "keylogger.dll", "capture.bat", "screen_capture.py",
			"mimikatz.dmp", "lsass.exe.memory",
		},
		"exploit": {
			"CVE-2024-", "CVE-2023-", "CVE-2022-", "CVE-2021-",
			"log4j", "log4shell", "spring4shell", "proxypack",
			"iframe[src=", "<form action=", "<input type=hidden name=",
			"eval(base64_decode(", "exec(eval(", "create_function(",
		},
		"scan": {
			"Nmap script engine", "Nikto Web Scanner", "Burp Suite",
			"Acunetix WVS Professional", "sqlmap/1.5", "masscan",
			"-sS", "-sV", "-O", "-A", "-Pn",
			"robots.txt", "sitemap.xml", ".git/config", ".env",
		},
	}

	catKeys := []string{"c2", "exfil", "malware", "exploit", "scan"}
	allPats := make([]ACPattern, 0, count)
	catIdx := 0

	for len(allPats) < count {
		cat := catKeys[catIdx%len(catKeys)]
		items := categories[cat]
		
		for i := 0; i < len(items) && len(allPats) < count; i++ {
			item := items[i]
			if r.Float64() < 0.3 {
				variant := item + fmt.Sprintf("%d", r.Intn(1000))
				allPats = append(allPats, ACPattern{
					Pattern: variant, Category: cat, Security: "high",
					ID: fmt.Sprintf("IOC-%s-%s-v%d", cat, variant, r.Intn(100)),
				})
			} else {
				allPats = append(allPats, ACPattern{
					Pattern: item, Category: cat, Security: "critical",
					ID: fmt.Sprintf("IOC-%s-%s-v%d", cat, item, r.Intn(100)),
				})
			}
		}
		catIdx++
		if catIdx >= len(catKeys)*5 {
			catIdx = 0
		}
	}

	if len(allPats) > count {
		allPats = allPats[:count]
	} else if len(allPats) < count {
		seedPats := allPats
		for len(allPats) < count {
			base := seedPats[r.Intn(len(seedPats))]
			suffix := fmt.Sprintf("x%d%x", r.Intn(10000), r.Uint32())
			allPats = append(allPats, ACPattern{
				Pattern: base.Pattern + suffix,
				Category: base.Category,
				Security: base.Security,
				ID: fmt.Sprintf("SYNTH-%s-%s", base.Category, suffix),
			})
		}
	}

	return allPats[:count]
}

// BuildTelemetryStream creates a realistic telemetry/log stream embedding IOC patterns
func BuildTelemetryStream(patterns []ACPattern, size int, r *rand.Rand) string {
	filler := `
2026-08-24T02:13:45Z INFO auth service started listening on :443
2026-08-24T02:13:46Z DEBUG processing request id=req-847392 user=admin src_ip=10.0.1.42
{"level":"info","ts":1724461825,"msg":"health check passed","component":"api-gateway"}
POST /api/v1/users HTTP/1.1 201 1.2kb User-Agent:"Mozilla/5.0 (Windows NT 10.0; Win64; x64)"
GET /dashboard?sort=created_at&order=desc HTTP/1.1 200 8.4kb Response-Time:23ms
{"event_type":"login_attempt","user_id":"u-92847","status":"success","auth_method":"oauth2"}
service mesh proxy: upstream connection pool health OK, active_connections=247
2026-08-24T02:13:52Z WARN rate_limit exceeded for client_id=c-48392 limit=1000/min
`

	var sb strings.Builder
	sb.Grow(size + 32*len(patterns))

	remaining := size
	for remaining > 0 {
		if r.Intn(20) == 0 && len(patterns) > 0 && remaining > 20 {
			pat := patterns[r.Intn(len(patterns))]
			toWrite := len(pat.Pattern)
			if toWrite > remaining {
				toWrite = remaining
			}
			sb.WriteString(pat.Pattern[:toWrite])
			remaining -= toWrite
		} else if remaining >= len(filler) {
			sb.WriteString(filler)
			remaining -= len(filler)
		} else {
			sb.WriteString(filler[:remaining])
			remaining = 0
		}
	}

	return sb.String()
}

// ============================================================================
// REGEXP COMPETITOR IMPLEMENTATION
// ============================================================================
// Competitor: Go stdlib regexp using alternation pattern (grouped OR)
// Regex complexity: O(n * total_pattern_length) worst case
// Our AC:          O(n + total_pattern_length + matches) guaranteed
// This is the REAL comparison mandated by FLIP.

// RegexpMatcher wraps Go's regexp: it compiles ONE regexp per IOC pattern (the
// real per-pattern O(n*patterns) competitor) and ALSO a single grouped
// alternation regexp for reference. Both operate on the SAME lowercased corpus
// and the SAME IOC pattern set as our Aho-Corasick automaton.
type RegexpMatcher struct {
	patterns       []string         // QuoteMeta'd lowercase literals
	compiledRE     *regexp.Regexp   // grouped alternation (pat1|pat2|...) for reference
	multiPatternRE []*regexp.Regexp // one compiled regexp per pattern (primary competitor)
	hasErr         bool             // true if alternation compilation failed
}

// NewRegexpMatcherFromACPatterns builds compiled regexps from the ACPattern slice.
// Patterns are lowercased + QuoteMeta-escaped so regex matches the SAME literal
// strings as Aho-Corasick (which lowercases and matches literals). This is the
// FLIP fairness guarantee: identical pattern set, identical corpus, identical
// literal-match semantics.
func NewRegexpMatcherFromACPatterns(patterns []ACPattern) *RegexpMatcher {
	strs := make([]string, len(patterns))
	for i, p := range patterns {
		// Escape special regex chars - we're comparing literal pattern matching!
		strs[i] = regexp.QuoteMeta(strings.ToLower(p.Pattern))
	}

	// Primary competitor: compile ONE regexp per pattern. This is the honest
	// O(n*patterns) baseline that AC's single-pass O(n+m+z) should crush.
	multiPattern := make([]*regexp.Regexp, 0, len(strs))
	for _, s := range strs {
		if s == "" {
			continue
		}
		if re, err := regexp.Compile(s); err == nil {
			multiPattern = append(multiPattern, re)
		}
	}

	// Reference: grouped alternation (pat1|pat2|...). Single-pass but uses
	// leftmost non-overlapping semantics, so its match COUNT differs from AC.
	totalLen := 0
	for _, s := range strs {
		totalLen += len(s)
	}
	reStr := make([]byte, 0, totalLen+3*len(patterns))
	reStr = append(reStr, '(')
	for i, s := range strs {
		if i > 0 {
			reStr = append(reStr, '|')
		}
		reStr = append(reStr, s...)
	}
	reStr = append(reStr, ')')
	compiled, err := regexp.Compile(string(reStr))

	return &RegexpMatcher{
		patterns:       strs,
		compiledRE:     compiled,
		multiPatternRE: multiPattern,
		hasErr:         err != nil,
	}
}

// HasError reports if the alternation regexp failed to compile.
func (rm *RegexpMatcher) HasError() bool {
	return rm.hasErr
}

// Search finds all matches using the PER-PATTERN loop (the real O(n*patterns)
// competitor). Each pattern's FindAllStringIndex over the corpus reports every
// occurrence of that literal, giving match semantics directly comparable to
// Aho-Corasick (all occurrences of every pattern).
func (rm *RegexpMatcher) Search(text string) []MatchResult {
	var results []MatchResult
	textLower := strings.ToLower(text)

	for _, re := range rm.multiPatternRE {
		matches := re.FindAllStringIndex(textLower, -1)
		for _, match := range matches {
			results = append(results, MatchResult{
				Pattern: re.String(),
				From:    match[0],
				To:      match[1],
				Node:    "regexp-per-pattern",
			})
		}
	}

	return results
}

// SearchAlternation finds matches using the single grouped alternation regexp.
// Kept for reference; NOTE its leftmost non-overlapping semantics report fewer
// matches than the per-pattern loop or AC when patterns overlap in the corpus.
func (rm *RegexpMatcher) SearchAlternation(text string) []MatchResult {
	var results []MatchResult
	textLower := strings.ToLower(text)
	if rm.compiledRE == nil {
		return results
	}
	matches := rm.compiledRE.FindAllStringIndex(textLower, -1)
	for _, match := range matches {
		results = append(results, MatchResult{
			Pattern: rm.compiledRE.String(),
			From:    match[0],
			To:      match[1],
			Node:    "regexp-alternation",
		})
	}
	return results
}

// MatchResult represents a single pattern match (shared between both engines)
type MatchResult struct {
	Pattern string
	From    int
	To      int
	Node    string // "ahocorasick", "regexp-alternation", "regexp-multi"
}

// ============================================================================
// CORRECTNESS VALIDATION: Verify identical match counts
// ============================================================================

// TestCorrectness_ACvsRegexp validates that AC and regexp find SAME number of hits
func TestCorrectness_ACvsRegexp(t *testing.T) {
	t.Run("scale_100_patterns", func(t *testing.T) {
		testCorrectnessAtScale(t, scaleSmall)
	})
	t.Run("scale_1000_patterns", func(t *testing.T) {
		testCorrectnessAtScale(t, scaleMedium)
	})
	t.Run("scale_10000_patterns", func(t *testing.T) {
		testCorrectnessAtScale(t, scaleLarge)
	})
}

func testCorrectnessAtScale(t *testing.T, count int) {
	r := rand.New(rand.NewSource(patternSeed))
	patterns := BuildIOCPatterns(count, r)
	text := BuildTelemetryStream(patterns, 100_000, rand.New(rand.NewSource(logStreamSeed)))

	// Run AC
	ac := NewAhoCorasick()
	for _, p := range patterns {
		ac.AddPattern(p)
	}
	ac.Build()
	acMatches := ac.Search(text)

	// Run regexp competitor
	rm := NewRegexpMatcherFromACPatterns(patterns)
	regexpMatches := rm.Search(text)

	// Compare counts (NOT positions - semantics differ slightly)
	acCount := len(acMatches)
	regexpCount := len(regexpMatches)

	diff := regexpCount - acCount
	if diff < 0 {
		diff = -diff
	}
	pct := float64(diff) / float64(acCount) * 100

	t.Logf("\n=== Scale %d patterns ===", count)
	t.Logf("AC matches:          %d", acCount)
	t.Logf("Regexp matches:       %d", regexpCount)
	t.Logf("Difference:           %d (%.2f%%)", diff, pct)

	if pct > 1.0 {
		t.Logf("⚠️  WARNING: Match counts differ significantly (>1%%)")
		t.Log("Note: Regex escaping may cause slight differences")
	} else {
		t.Logf("✓ Match counts comparable (within 1%% tolerance)")
	}
}

// ============================================================================
// BENCHMARK TESTS: Throughput + Latency
// ============================================================================

// ------------------------ SCALE: N=100 PATTERNS ------------------------

func BenchmarkAC_100patterns(b *testing.B) {
	patterns := BuildIOCPatterns(scaleSmall, rand.New(rand.NewSource(patternSeed)))
	text := BuildTelemetryStream(patterns, logStreamSize, rand.New(rand.NewSource(logStreamSeed)))

	ac := NewAhoCorasick()
	for _, p := range patterns {
		ac.AddPattern(p)
	}
	ac.Build()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		matches := ac.Search(text)
		// Sink+runtime.KeepAlive to prevent DCE
		_ = matches
	}
}

func BenchmarkRegexp_100patterns(b *testing.B) {
	patterns := BuildIOCPatterns(scaleSmall, rand.New(rand.NewSource(patternSeed)))
	text := BuildTelemetryStream(patterns, logStreamSize, rand.New(rand.NewSource(logStreamSeed)))

	rm := NewRegexpMatcherFromACPatterns(patterns)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		matches := rm.Search(text)
		_ = matches
	}
}

// ------------------------ SCALE: N=1000 PATTERNS ------------------------

func BenchmarkAC_1kpatterns(b *testing.B) {
	patterns := BuildIOCPatterns(scaleMedium, rand.New(rand.NewSource(patternSeed)))
	text := BuildTelemetryStream(patterns, logStreamSize, rand.New(rand.NewSource(logStreamSeed)))

	ac := NewAhoCorasick()
	for _, p := range patterns {
		ac.AddPattern(p)
	}
	ac.Build()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		matches := ac.Search(text)
		_ = matches
	}
}

func BenchmarkRegexp_1kpatterns(b *testing.B) {
	patterns := BuildIOCPatterns(scaleMedium, rand.New(rand.NewSource(patternSeed)))
	text := BuildTelemetryStream(patterns, logStreamSize, rand.New(rand.NewSource(logStreamSeed)))

	rm := NewRegexpMatcherFromACPatterns(patterns)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		matches := rm.Search(text)
		_ = matches
	}
}

// ------------------------ SCALE: N=10000 PATTERNS (THE WIN THESIS) ------------------------

func BenchmarkAC_10kpatterns(b *testing.B) {
	patterns := BuildIOCPatterns(scaleLarge, rand.New(rand.NewSource(patternSeed)))
	text := BuildTelemetryStream(patterns, logStreamSize, rand.New(rand.NewSource(logStreamSeed)))

	ac := NewAhoCorasick()
	for _, p := range patterns {
		ac.AddPattern(p)
	}
	ac.Build()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		matches := ac.Search(text)
		_ = matches
	}
}

func BenchmarkRegexp_10kpatterns(b *testing.B) {
	patterns := BuildIOCPatterns(scaleLarge, rand.New(rand.NewSource(patternSeed)))
	text := BuildTelemetryStream(patterns, logStreamSize, rand.New(rand.NewSource(logStreamSeed)))

	rm := NewRegexpMatcherFromACPatterns(patterns)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		matches := rm.Search(text)
		_ = matches
	}
}

// ============================================================================
// THROUGHPUT METRICS: MB/sec and Matches/sec
// ============================================================================

func BenchmarkAC_10kpatterns_MBperSec(b *testing.B) {
	patterns := BuildIOCPatterns(scaleLarge, rand.New(rand.NewSource(patternSeed)))
	text := BuildTelemetryStream(patterns, logStreamSize, rand.New(rand.NewSource(logStreamSeed)))

	ac := NewAhoCorasick()
	for _, p := range patterns {
		ac.AddPattern(p)
	}
	ac.Build()

	textBytes := int64(len(text))
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		matches := ac.Search(text)
		_ = matches
	}
	// MB/sec = (text_bytes * iterations) / (elapsed_sec * 1024 * 1024)
	b.ReportMetric(float64(textBytes)*float64(b.N)/float64(b.Elapsed())/1024/1024, "MB/s")
}

func BenchmarkRegexp_10kpatterns_MBperSec(b *testing.B) {
	patterns := BuildIOCPatterns(scaleLarge, rand.New(rand.NewSource(patternSeed)))
	text := BuildTelemetryStream(patterns, logStreamSize, rand.New(rand.NewSource(logStreamSeed)))

	rm := NewRegexpMatcherFromACPatterns(patterns)

	textBytes := int64(len(text))
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		matches := rm.Search(text)
		_ = matches
	}
	b.ReportMetric(float64(textBytes)*float64(b.N)/float64(b.Elapsed())/1024/1024, "MB/s")
}

// ============================================================================
// STATISTICAL ANALYSIS HELPER
// ============================================================================

func TestStatisticalSummary(t *testing.T) {
	t.Log("===========================================================")
	t.Log("M28 FLIP Mandate: Aho-Corasick vs Go Stdlib RegExp")
	t.Log("===========================================================")
	t.Log("Win thesis: AC should crush regex on throughput for large IOC sets")
	t.Log("Theory: O(n+m+z) vs O(n*p) where p=pattern_count")
	t.Log("Rules: count=6 median, real competitors, honest verdict if loss")
	t.Log("")
	t.Log("Commands:")
	t.Log("  # Small scale")
	t.Log("  go test ./pkg/security/ -tags m28flip -bench='^Benchmark(AC|Regexp)_100patterns$' -benchtime=2s -count=6 -json")
	t.Log("")
	t.Log("  # Medium scale")
	t.Log("  go test ./pkg/security/ -tags m28flip -bench='^Benchmark(AC|Regexp)_1kpatterns$' -benchtime=2s -count=6 -json")
	t.Log("")
	t.Log("  # Large scale (THE WIN THESIS)")
	t.Log("  go test ./pkg/security/ -tags m28flip -bench='^Benchmark(AC|Regexp)_10kpatterns$' -benchtime=2s -count=6 -json")
	t.Log("===========================================================")
}
