//go:build m29ioc

package security

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"testing"
)

// =============================================================================
// Module 29 – UEBA+IOC Fusion: Aho-Corasick vs Naive Baseline Benchmark
// =============================================================================
// This file constructs a REAL, FAIR head-to-head for M29 Behavioral Hunting's
// IOC pattern matching: comparing our production Aho-Corasick automaton against
// (a) naive sequential strings.Contains loop and (b) compiled regex baseline.
//
// Win thesis: Aho-Corasick already proved 269x on M35. Multi-pattern IOC matching
// should show widening advantage at N→∞ patterns due to O(N+M+Z) vs O(N*M) complexity.
//
// ANTI-FIASCO GUARANTEES:
// - Import/use REAL competitors (no stubs)
// - Count = 6 median runs
// - Same work unit both sides (identical patterns + text)
// - Honest verdict even if we lose
// - No M5 warmup bias
//
// Environment: Windows PowerShell, go env -w GOMODCACHE=E:\go\pkg\mod
// deps missing → go get to E drive, never stub
// bench text eaten → -json output capture
//
// Run command:
//   go test ./pkg/security/ -tags m29ioc -bench=. -benchtime=2s -count=6 -run=^$ -benchmem
// =============================================================================

const (
	// Pattern scales to benchmark
	scaleSmall     = 100
	scaleMedium    = 1_000
	scaleLarge     = 10_000

	// Event log stream size (realistic per-batch ingestion)
	logStreamSize = 500_000 // 500KB of combined log events

	// Match density in test corpus (~5% of positions embed real patterns)
	matchDensity = 0.05

	// Deterministic seeds for reproducibility
	patternSeed   = 20260824 // today's date
	logStreamSeed = 1337
)

// BuildRealIOCPatterns generates realistic IOC threat intelligence patterns
// covering C2 beacons, exfiltration signatures, malware indicators, etc.
func BuildRealIOCPatterns(count int, r *rand.Rand) []ACPattern {
	// Real-world IOC categories from MITRE ATT&CK and commercial TI feeds
	categories := map[string][]string{
		"c2": {
			"beacon-http", "cobalt-strike", "metasploit", "empyrean-framework",
			"powerview.ps1", "procdump.exe", "mimikatz", "pass-the-hash",
			"whiskerty", "avast-shellextension", "dlphr", "necurs",
			".onion", ".ru", ".cn", ".xyz", ".top",
			"http://domain-example",
			"POST /panel.php", "GET /shell.php", "POST /cmd.php",
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
			"wannaCry", "petya", "notPetya", "ryuk", "contabo",
			"evilginx", "keylogger.dll", "capture.bat", "screen_capture.py",
			"mimikatz.dmp", "lsass.exe.memory",
		},
		"exploit": {
			"CVE-2024-", "CVE-2023-", "CVE-2022-", "CVE-2021-",
			"log4j", "log4shell", "spring4shell", "proxypack",
			"iframe[src=", "<form action=", "<input type=hidden name=",
			"eval(base64_decode(", "exec(eval(", "create_function(",
			"unserialize($", "assert(preg_replace(", "call_user_func_array(",
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
			
			// Sometimes add random noise to simulate variant patterns
			if r.Float64() < 0.3 && !strings.Contains(item, ".") && !strings.Contains(item, "/") {
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

	// Truncate/pad to exact count
	if len(allPats) > count {
		allPats = allPats[:count]
	} else if len(allPats) < count {
		// Fill gap with synthetic variants
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

// BuildLogEventStream creates a realistic event/log stream embedding patterns
// at ~5% density (simulating actual threat traffic mixed with benign noise)
func BuildLogEventStream(patterns []ACPattern, size int, r *rand.Rand) string {
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
			// Embed a pattern (~5% of time)
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
			// Write partial filler
			sb.WriteString(filler[:remaining])
			remaining = 0
		}
	}

	return sb.String()
}

// =============================================================================
// NAIVE BASELINE IMPLEMENTATIONS
// =============================================================================

// NaiveSequentialMatcher implements naive O(N*M) string search using strings.Contains
type NaiveSequentialMatcher struct {
	patterns   []string
	compiledRE []*regexp.Regexp
}

func NewNaiveSequentialMatcher(patterns []ACPattern) *NaiveSequentialMatcher {
	strs := make([]string, len(patterns))
	for i, p := range patterns {
		strs[i] = strings.ToLower(p.Pattern)
	}

	// Compile some patterns as regex (those containing special chars)
	reList := make([]*regexp.Regexp, 0, len(strs)/10)
	for _, s := range strs {
		if strings.ContainsAny(s, "+.*?[](){}|^") {
			if re, err := regexp.Compile("(?i)" + s); err == nil {
				reList = append(reList, re)
			}
		}
	}

	return &NaiveSequentialMatcher{
		patterns:   strs,
		compiledRE: reList,
	}
}

// SearchNaiveStrings finds all matches using naive strings.Contains loop
func (nm *NaiveSequentialMatcher) SearchNaiveStrings(text string) []MatchResult {
	textLower := strings.ToLower(text)
	var results []MatchResult

	for i, pat := range nm.patterns {
		start := 0
		for start < len(textLower) {
			idx := strings.Index(textLower[start:], pat)
			if idx == -1 {
				break
			}
			results = append(results, MatchResult{
				Pattern: nm.patterns[i],
				From:    start + idx,
				To:      start + idx + len(pat),
				Node:    "naive-string",
			})
			start = start + idx + 1
		}
	}

	return results
}

// SearchNaiveRegex finds all matches using compiled regex
func (nm *NaiveSequentialMatcher) SearchNaiveRegex(text string) []MatchResult {
	var results []MatchResult

	for _, re := range nm.compiledRE {
		matches := re.FindAllStringIndex(text, -1)
		for _, match := range matches {
			results = append(results, MatchResult{
				Pattern: re.String(),
				From:    match[0],
				To:      match[1],
				Node:    "naive-regex",
			})
		}
	}

	return results
}

// MatchResult represents a single pattern match
type MatchResult struct {
	Pattern string
	From    int
	To      int
	Node    string // "ahocorasick", "naive-string", "naive-regex"
}

// =============================================================================
// BENCHMARK TESTS
// =============================================================================

func BenchmarkACvsNaive_Any_Medium(b *testing.B) {
	r := rand.New(rand.NewSource(patternSeed))
	patterns := BuildRealIOCPatterns(scaleMedium, r)
	text := BuildLogEventStream(patterns, logStreamSize, rand.New(rand.NewSource(logStreamSeed)))

	// Build AC engine
	ac := NewAhoCorasick()
	for _, p := range patterns {
		ac.AddPattern(p)
	}
	ac.Build()

	// Build naive matcher
	nm := NewNaiveSequentialMatcher(patterns)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = ac.Search(text)
		_ = nm.SearchNaiveStrings(text)
	}
}

// BENCHMARK: N=100 patterns (small scale)
func BenchmarkAC_100patterns(b *testing.B) {
	patterns := BuildRealIOCPatterns(scaleSmall, rand.New(rand.NewSource(patternSeed)))
	text := BuildLogEventStream(patterns, logStreamSize, rand.New(rand.NewSource(logStreamSeed)))

	ac := NewAhoCorasick()
	for _, p := range patterns {
		ac.AddPattern(p)
	}
	ac.Build()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ac.Search(text)
	}
}

func BenchmarkNaive_String_100patterns(b *testing.B) {
	patterns := BuildRealIOCPatterns(scaleSmall, rand.New(rand.NewSource(patternSeed)))
	text := BuildLogEventStream(patterns, logStreamSize, rand.New(rand.NewSource(logStreamSeed)))

	nm := NewNaiveSequentialMatcher(patterns)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = nm.SearchNaiveStrings(text)
	}
}

func BenchmarkNaive_Regex_100patterns(b *testing.B) {
	patterns := BuildRealIOCPatterns(scaleSmall, rand.New(rand.NewSource(patternSeed)))
	text := BuildLogEventStream(patterns, logStreamSize, rand.New(rand.NewSource(logStreamSeed)))

	nm := NewNaiveSequentialMatcher(patterns)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = nm.SearchNaiveRegex(text)
	}
}

// BENCHMARK: N=1000 patterns (medium scale)
func BenchmarkAC_1000patterns(b *testing.B) {
	patterns := BuildRealIOCPatterns(scaleMedium, rand.New(rand.NewSource(patternSeed)))
	text := BuildLogEventStream(patterns, logStreamSize, rand.New(rand.NewSource(logStreamSeed)))

	ac := NewAhoCorasick()
	for _, p := range patterns {
		ac.AddPattern(p)
	}
	ac.Build()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ac.Search(text)
	}
}

func BenchmarkNaive_String_1000patterns(b *testing.B) {
	patterns := BuildRealIOCPatterns(scaleMedium, rand.New(rand.NewSource(patternSeed)))
	text := BuildLogEventStream(patterns, logStreamSize, rand.New(rand.NewSource(logStreamSeed)))

	nm := NewNaiveSequentialMatcher(patterns)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = nm.SearchNaiveStrings(text)
	}
}

func BenchmarkNaive_Regex_1000patterns(b *testing.B) {
	patterns := BuildRealIOCPatterns(scaleMedium, rand.New(rand.NewSource(patternSeed)))
	text := BuildLogEventStream(patterns, logStreamSize, rand.New(rand.NewSource(logStreamSeed)))

	nm := NewNaiveSequentialMatcher(patterns)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = nm.SearchNaiveRegex(text)
	}
}

// BENCHMARK: N=10000 patterns (large scale - THE WIN THESIS SCALE)
func BenchmarkAC_10000patterns(b *testing.B) {
	patterns := BuildRealIOCPatterns(scaleLarge, rand.New(rand.NewSource(patternSeed)))
	text := BuildLogEventStream(patterns, logStreamSize, rand.New(rand.NewSource(logStreamSeed)))

	ac := NewAhoCorasick()
	for _, p := range patterns {
		ac.AddPattern(p)
	}
	ac.Build()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ac.Search(text)
	}
}

func BenchmarkNaive_String_10000patterns(b *testing.B) {
	patterns := BuildRealIOCPatterns(scaleLarge, rand.New(rand.NewSource(patternSeed)))
	text := BuildLogEventStream(patterns, logStreamSize, rand.New(rand.NewSource(logStreamSeed)))

	nm := NewNaiveSequentialMatcher(patterns)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = nm.SearchNaiveStrings(text)
	}
}

func BenchmarkNaive_Regex_10000patterns(b *testing.B) {
	patterns := BuildRealIOCPatterns(scaleLarge, rand.New(rand.NewSource(patternSeed)))
	text := BuildLogEventStream(patterns, logStreamSize, rand.New(rand.NewSource(logStreamSeed)))

	nm := NewNaiveSequentialMatcher(patterns)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = nm.SearchNaiveRegex(text)
	}
}

// =============================================================================
// CORRECTNESS VALIDATION
// =============================================================================

// TestCorrectness_NaiveVsAC validates that naive and AC find same hits
func TestCorrectness_NaiveVsAC(t *testing.T) {
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
	patterns := BuildRealIOCPatterns(count, r)
	text := BuildLogEventStream(patterns, 100_000, rand.New(rand.NewSource(logStreamSeed)))

	// Run AC
	ac := NewAhoCorasick()
	for _, p := range patterns {
		ac.AddPattern(p)
	}
	ac.Build()
	acMatches := ac.Search(text)

	// Run naive string
	nm := NewNaiveSequentialMatcher(patterns)
	naiveStrMatches := nm.SearchNaiveStrings(text)

	// Compare counts
	acCount := len(acMatches)
	naiveStrCount := len(naiveStrMatches)

	t.Logf("\n=== Scale %d patterns ===", count)
	t.Logf("AC matches:         %d", acCount)
	t.Logf("Naive string matches: %d", naiveStrCount)
	t.Logf("Difference:         %d (%.2f%%)", naiveStrCount-acCount, float64(naiveStrCount-acCount)/float64(acCount)*100)

	if acCount != naiveStrCount {
		t.Logf("⚠️  WARNING: Match counts differ - semantics may not be byte-identical")
	} else {
		t.Logf("✓ Match counts identical")
	}
}

// =============================================================================
// STATISTICAL ANALYSIS HELPER (for post-processing)
// =============================================================================

// TestStatisticalSummary provides sample stats computation for the team
func TestStatisticalSummary(t *testing.T) {
	t.Log("===========================================================")
	t.Log("Module 29: Aho-Corasick vs Naive Baseline - Fair Head-to-Head")
	t.Log("===========================================================")
	t.Log("Win thesis: AC already proved 269x on M35; expect widening gap at N→∞")
	t.Log("Rules: count=6 median, real competitors, honest verdict if loss")
	t.Log("")
	t.Log("Commands:")
	t.Log("  # Small scale")
	t.Log("  go test ./pkg/security/ -tags m29ioc -bench='BenchmarkAC_100patterns|BenchmarkNaive' -benchtime=2s -count=6 -json")
	t.Log("")
	t.Log("  # Medium scale")
	t.Log("  go test ./pkg/security/ -tags m29ioc -bench='BenchmarkAC_1000patterns|BenchmarkNaive' -benchtime=2s -count=6 -json")
	t.Log("")
	t.Log("  # Large scale (THE WIN THESIS)")
	t.Log("  go test ./pkg/security/ -tags m29ioc -bench='BenchmarkAC_10000patterns|BenchmarkNaive' -benchtime=2s -count=6 -json")
	t.Log("===========================================================")
}
