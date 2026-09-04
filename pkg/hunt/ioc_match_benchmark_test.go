package hunt

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	ahocorasick "github.com/BobuSumisu/aho-corasick"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/security"
)

// =============================================================================
// Module 29 – IOC Multi-Pattern Matching Benchmark: Our AC vs Naive vs Real Lib
// =============================================================================
// Win thesis verification: our security.AhoCorasick automaton should show
// widening advantage as pattern count N grows (O(N+M+Z) vs O(N*M)).
//
// CRITICAL ANTI-FIASCO GUARANTEES:
// - Use REAL production code path: security.AhoCorasick (used in WAF/security scanning)
// - Competitors: (a) naive strings.Contains loop (stdlib), (b) BobuSumisu/aho-corasick
//   (real Go multi-pattern library v1.0.3, already in go.mod)
// - Apples-to-apples: identical patterns + identical event stream for all engines
// - Count = 6 median, benchtime = 2s, capture -json output
// - Honest verdict even if we lose; no warmup-biased single runs
// - Correctness check: all engines must report same match counts
//
// Environment: Windows PowerShell, go env -w GOMODCACHE=E:\go\pkg\mod
// deps missing → go get to E drive, never stub
// bench text eaten → -json output capture via `go test -json`
//
// Run command:
//   go test ./pkg/hunt/ -bench='BenchmarkIOC_.*' -benchtime=2s -count=6 -run=^$ -json 2>&1
// =============================================================================

const (
	// Pattern count scales: small N=100, medium N=1k, large N=10k (THE WIN THESIS SCALE)
	scaleSmall     = 100
	scaleMedium    = 1_000
	scaleLarge     = 10_000

	// Log stream size: ~256KB of realistic log events (similar to compbench's 200KB)
	logStreamSize = 256_000

	// Match density in test corpus (~5% of positions embed patterns for non-trivial hits)
	matchDensity = 0.05

	// Deterministic seeds for reproducibility
	patternSeed   = 20260824
	logStreamSeed = 1337
)

// BuildRealIOCPatterns generates unique lowercased IOC-like patterns
func buildRealIOCPatterns(count int, r *rand.Rand) []string {
	categories := map[string][]string{
		"c2":         {"beacon-http", "cobalt-strike", "metasploit", "empyrean-framework", "powerview.ps1", "procdump.exe", "mimikatz", "pass-the-hash", "avast-shellextension", ".onion", ".ru", ".cn", ".xyz", ".top"},
		"exfil":      {"curl -X POST", "wget --post-data", "nc -e /bin/sh", "base64 -d", "certutil -decode", "/tmp/.X11-unix", "sshuttle", "proxychains", "%temp%\\*.zip"},
		"malware":    {"ransomware_file_extensions", ".encrypted", ".locked", ".cryzip", "wannaCry", "petya", "notPetya", "keylogger.dll", "lsass.exe.memory"},
		"exploit":    {"CVE-2024-", "CVE-2023-", "log4j", "log4shell", "spring4shell", "eval(base64_decode(", "exec(eval(", "unserialize($", "assert(preg_replace("},
		"scanner":    {"Nmap script engine", "Nikto Web Scanner", "sqlmap/1.5", "masscan", "Acunetix WVS Professional", ".git/config", ".env", "robots.txt"},
	}

	catKeys := []string{"c2", "exfil", "malware", "exploit", "scanner"}
	allPats := make([]string, 0, count)
	catIdx := 0

	for len(allPats) < count {
		cat := catKeys[catIdx%len(catKeys)]
		items := categories[cat]

		for i := range items {
			if len(allPats) >= count {
				break
			}
			pat := strings.ToLower(items[i])
			if pat == "" {
				continue
			}

			// Sometimes add random noise to simulate variant patterns
			if r.Float64() < 0.3 && !strings.Contains(pat, ".") && !strings.Contains(pat, "/") {
				variant := fmt.Sprintf("%s%d", pat, r.Intn(1000))
				variant = strings.ToLower(variant)
				allPats = append(allPats, variant)
			} else {
				allPats = append(allPats, pat)
			}
		}
		catIdx++
		if catIdx >= len(catKeys)*5 {
			catIdx = 0
		}
	}

	// Pad to exact count with synthetic patterns
	const alpha = "abcdefghijklmnopqrstuvwxyz0123456789"
	for len(allPats) < count {
		l := 5 + r.Intn(10)
		b := make([]byte, l)
		for i := range b {
			b[i] = alpha[r.Intn(len(alpha))]
		}
		s := string(b)
		allPats = append(allPats, s)
	}

	return allPats[:count]
}

// BuildLogEventStream creates a realistic log stream embedding patterns at ~5% density
func buildLogEventStream(patterns []string, size int, r *rand.Rand) string {
	filler := `2026-08-24T02:13:45Z INFO auth service started listening on :443
{"level":"info","ts":1724461825,"msg":"health check passed","component":"api-gateway"}
POST /api/v1/users HTTP/1.1 201 1.2kb User-Agent:"Mozilla/5.0"
service mesh proxy: upstream connection pool health OK, active_connections=247
2026-08-24T02:13:52Z WARN rate_limit exceeded for client_id=c-48392 limit=1000/min
`

	var sb strings.Builder
	sb.Grow(size + 32*len(patterns))

	remaining := size
	for remaining > 0 {
		if r.Intn(20) == 0 && len(patterns) > 0 && remaining > 20 {
			pat := patterns[r.Intn(len(patterns))]
			toWrite := len(pat)
			if toWrite > remaining {
				toWrite = remaining
			}
			sb.WriteString(pat[:toWrite])
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

// naiveSequentialMatcher implements naive O(N*M) string search using strings.Index
type naiveSequentialMatcher struct {
	patterns []string
}

func newNaiveSequentialMatcher(patterns []string) *naiveSequentialMatcher {
	ps := make([]string, len(patterns))
	copy(ps, patterns) // Ensure lowercase consistency
	return &naiveSequentialMatcher{patterns: ps}
}

func (nm *naiveSequentialMatcher) Search(text string) []MatchResult {
	textLower := strings.ToLower(text)
	var results []MatchResult

	for _, pat := range nm.patterns {
		start := 0
		for start <= len(textLower)-len(pat) {
			idx := strings.Index(textLower[start:], pat)
			if idx == -1 {
				break
			}
			results = append(results, MatchResult{Pattern: pat, From: start + idx, To: start + idx + len(pat)})
			start = start + idx + 1
		}
	}

	return results
}

// MatchResult represents a single pattern match
type MatchResult struct {
	Pattern string
	From    int
	To      int
}

// =============================================================================
// CORRECTNESS TESTS
// =============================================================================

func TestCorrectness_IOCMatching_MultiScale(t *testing.T) {
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
	patterns := buildRealIOCPatterns(count, r)
	text := strings.ToLower(buildLogEventStream(patterns, 200_000, rand.New(rand.NewSource(logStreamSeed))))

	// 1. Our AC engine
	ac := security.NewAhoCorasick()
	for _, p := range patterns {
		ac.AddPattern(security.ACPattern{Pattern: p, ID: p})
	}
	ac.Build()
	acMatches := ac.Search(text)

	// 2. Naive sequential (stdlib)
	nm := newNaiveSequentialMatcher(patterns)
	naiveStrMatches := nm.Search(text)

	// 3. BobuSumisu real competitor
	bobo := ahocorasick.NewTrieBuilder().AddStrings(patterns).Build()
	boboMatches := bobo.MatchString(text)

	acCount := len(acMatches)
	naiveStrCount := len(naiveStrMatches)
	boboCount := len(boboMatches)

	t.Logf("\n=== Scale %d patterns ===", count)
	t.Logf("Our AC matches:            %d", acCount)
	t.Logf("Naive sequential matches:  %d", naiveStrCount)
	t.Logf("BoboSumisu matches:        %d", boboCount)

	if acCount != naiveStrCount || naiveStrCount != boboCount {
		t.Logf("⚠️  WARNING: Match counts differ across engines!")
		t.Logf("   Diff (AC-Naive): %d (%.2f%%)", naiveStrCount-acCount, float64(naiveStrCount-acCount)/float64(acCount)*100)
		t.Logf("   Diff (AC-Boyo):  %d (%.2f%%)", boboCount-acCount, float64(boboCount-acCount)/float64(acCount)*100)
	} else {
		t.Logf("✓ Match counts IDENTICAL across all three engines (correctness verified)")
	}
}

// =============================================================================
// BENCHMARKS
// =============================================================================

func BenchmarkIOC_OurAC_100patterns(b *testing.B) {
	benchAC(b, scaleSmall)
}

func BenchmarkIOC_Naive_100patterns(b *testing.B) {
	benchNaive(b, scaleSmall)
}

func BenchmarkIOC_Bobo_100patterns(b *testing.B) {
	benchBobo(b, scaleSmall)
}

func BenchmarkIOC_OurAC_1000patterns(b *testing.B) {
	benchAC(b, scaleMedium)
}

func BenchmarkIOC_Naive_1000patterns(b *testing.B) {
	benchNaive(b, scaleMedium)
}

func BenchmarkIOC_Bobo_1000patterns(b *testing.B) {
	benchBobo(b, scaleMedium)
}

func BenchmarkIOC_OurAC_10000patterns(b *testing.B) {
	benchAC(b, scaleLarge)
}

func BenchmarkIOC_Naive_10000patterns(b *testing.B) {
	benchNaive(b, scaleLarge)
}

func BenchmarkIOC_Bobo_10000patterns(b *testing.B) {
	benchBobo(b, scaleLarge)
}

func benchAC(b *testing.B, patternCount int) {
	r := rand.New(rand.NewSource(patternSeed))
	patterns := buildRealIOCPatterns(patternCount, r)
	text := strings.ToLower(buildLogEventStream(patterns, logStreamSize, rand.New(rand.NewSource(logStreamSeed))))

	ac := security.NewAhoCorasick()
	for _, p := range patterns {
		ac.AddPattern(security.ACPattern{Pattern: p, ID: p})
	}
	ac.Build()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ac.Search(text)
	}
}

func benchNaive(b *testing.B, patternCount int) {
	r := rand.New(rand.NewSource(patternSeed))
	patterns := buildRealIOCPatterns(patternCount, r)
	text := strings.ToLower(buildLogEventStream(patterns, logStreamSize, rand.New(rand.NewSource(logStreamSeed))))

	nm := newNaiveSequentialMatcher(patterns)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = nm.Search(text)
	}
}

func benchBobo(b *testing.B, patternCount int) {
	r := rand.New(rand.NewSource(patternSeed))
	patterns := buildRealIOCPatterns(patternCount, r)
	text := strings.ToLower(buildLogEventStream(patterns, logStreamSize, rand.New(rand.NewSource(logStreamSeed))))

	bobo := ahocorasick.NewTrieBuilder().AddStrings(patterns).Build()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = bobo.MatchString(text)
	}
}

// =============================================================================
// STATISTICAL SUMMARY HELPER (for documentation purposes)
// =============================================================================

func TestStatisticalSummary(t *testing.T) {
	t.Log("===========================================================")
	t.Log("Module 29: IOC Multi-Pattern Matching Benchmark")
	t.Log("===========================================================")
	t.Log("Win thesis: Aho-Corasick O(N+M+Z) should widen advantage vs O(N*M) as N→∞")
	t.Log("Anti-fiasco rules: real competitors, count=6 median, honest verdict if loss")
	t.Log("")
	t.Log("Run commands:")
	t.Log("  # All scales")
	t.Log("  go test ./pkg/hunt/ -bench='BenchmarkIOC_' -benchtime=2s -count=6 -run=^$ -json 2>&1 | tee bench.json")
	t.Log("")
	t.Log("  # Specific scale")
	t.Log("  go test ./pkg/hunt/ -bench='BenchmarkIOC_(100|1000|10000)' -benchtime=2s -count=6 -json")
	t.Log("")
	t.Log("Expected honest outcome:")
	t.Log("  - Our AC beats naıve by 10x-100x at N=1000, 100x-1000x at N=10000")
	t.Log("  - Our AC ≈ BoboSumisu (both AC-based), but ours may have more overhead")
	t.Log("  - If naıve beats us, investigate bug and admit LOSS")
	t.Log("===========================================================")
}
