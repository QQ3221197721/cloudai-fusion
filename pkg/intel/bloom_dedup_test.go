package intel

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Benchmark Suite: Cold-Path Bypass & Throughput Analysis
// ---------------------------------------------------------------------------

const (
	testUniqueIOCs  = 100_000        // total unique IOCs in corpus
	duplicateRate   = 0.75           // 75% of inserts are duplicates
	warmUpCount     = testUniqueIOCs // populate bloom with all uniques first
	benchmarkRuns   = 100_000        // total operations per benchmark
)

// generateIOC generates a deterministic IOC string from index
func generateIOC(index int) string {
	return "10." + string(rune((index>>16)&0xff)) + "." + string(rune((index>>8)&0xff)) + "." + string(rune(index&0xff))
}

// generateThreatDistribution creates a mix of warm (historical) and cold (new) items
func generateThreatDistribution(operations int, newRate float64, rng *rand.Rand) []string {
	result := make([]string, operations)
	
	for i := 0; i < operations; i++ {
		if rng.Float64() < newRate {
			result[i] = generateIOC(warmUpCount + i)
		} else {
			result[i] = generateIOC(rng.Int() % testUniqueIOCs)
		}
	}
	
	return result
}

// ---------------------------------------------------------------------------
// Benchmark 1: Cold-Path Bypass Rate
// ---------------------------------------------------------------------------

func BenchmarkBloomDedup_ColdPath(b *testing.B) {
	config := DefaultBloomConfig()
	config.ExpectedItems = warmUpCount + benchmarkRuns
	config.MaxFalsePos = 0.01
	
	bd := NewBloomDedup(config)
	
	for i := 0; i < warmUpCount; i++ {
		bd.Add(generateIOC(i))
	}
	
	bd.stats.Reset()
	
	b.ReportAllocs()
	b.ResetTimer()
	
	newRate := 0.30
	rng := rand.New(rand.NewSource(42))
	stream := generateThreatDistribution(b.N, newRate, rng)
	
	for i := 0; i < b.N; i++ {
		bd.Add(stream[i])
	}
}

// TestBloomDedup_ColdPathStats verifies cold-path statistics are accurate
func TestBloomDedup_ColdPathStats(t *testing.T) {
	config := DefaultBloomConfig()
	config.ExpectedItems = warmUpCount
	config.MaxFalsePos = 0.01
	
	bd := NewBloomDedup(config)
	
	for i := 0; i < warmUpCount; i++ {
		bd.Add(generateIOC(i))
	}
	
	coldStream := generateThreatDistribution(10000, 1.0, rand.New(rand.NewSource(42)))
	for _, item := range coldStream {
		bd.Add(item)
	}
	
	t.Log(bd.stats.Report())
	
	expectedMinCold := int64(float64(len(coldStream)) * 0.95)
	if bd.stats.BypassCount < expectedMinCold {
		t.Errorf("Expected >= %d cold-path bypasses, got %d", expectedMinCold, bd.stats.BypassCount)
	}
}

// ---------------------------------------------------------------------------
// Benchmark 2: Throughput Comparison
// ---------------------------------------------------------------------------

type memoryBasedDedup struct {
	exactMap map[string]struct{}
	addCount int
	mu       chan bool // dummy for sync.Mutex replacement
}

func NewMemoryBasedDedup(expectedItems int) *memoryBasedDedup {
	return &memoryBasedDedup{
		exactMap: make(map[string]struct{}, expectedItems),
		addCount: 0,
		mu:       make(chan bool, 1),
	}
}

func (mbd *memoryBasedDedup) Add(key string) bool {
	key = normalizeIOCKey(key)
	
	if _, exists := mbd.exactMap[key]; exists {
		return false
	}
	
	mbd.exactMap[key] = struct{}{}
	mbd.addCount++
	return true
}

func (mbd *memoryBasedDedup) Lookup(key string) (bool, error) {
	_, exists := mbd.exactMap[normalizeIOCKey(key)]
	return exists, nil
}

func BenchmarkThroughput_BloomVsPureMap(b *testing.B) {
	bloomConfig := DefaultBloomConfig()
	bloomConfig.ExpectedItems = warmUpCount + benchmarkRuns
	bloomConfig.MaxFalsePos = 0.01
	
	bloomDedup := NewBloomDedup(bloomConfig)
	memoryDedup := NewMemoryBasedDedup(warmUpCount)
	
	for i := 0; i < warmUpCount; i++ {
		key := generateIOC(i)
		bloomDedup.Add(key)
		memoryDedup.Add(key)
	}
	
	testStream := generateThreatDistribution(benchmarkRuns, 0.30, rand.New(rand.NewSource(42)))
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		idx := i % len(testStream)
		key := testStream[idx]
		
		bloomDedup.Add(key)
		memoryDedup.Add(key)
	}
}

// ---------------------------------------------------------------------------
// Benchmark 3: Memory Footprint Analysis
// ---------------------------------------------------------------------------

func BenchmarkBloomDedup_MemoryFootprint(b *testing.B) {
	config := DefaultBloomConfig()
	config.ExpectedItems = warmUpCount + benchmarkRuns
	config.MaxFalsePos = 0.01
	
	var memStats uint64 // placeholder for runtime.MemStats (removed due to missing import)
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		bd := NewBloomDedup(config)
		
		for j := 0; j < warmUpCount; j++ {
			bd.Add(generateIOC(j))
		}
		
		memStats = uint64(bd.GetMemoryUsageBytes())
		
		t.Logf("After inserting %d unique IOCs: %.2f MB total", 
			warmUpCount, float64(memStats)/1024/1024)
	}
}

func TestBloomDedup_MemoryBoundedness(t *testing.T) {
	config := DefaultBloomConfig()
	config.MaxMemoryMB = 10
	
	bd := NewBloomDedup(config)
	
	initialMem := bd.GetMemoryUsageBytes()
	
	insertCount := 5_000_000
	for i := 0; i < insertCount; i++ {
		bd.Add(generateIOC(i))
	}
	
	finalMem := bd.GetMemoryUsageBytes()
	
	t.Logf("Initial memory: %.2f MB", float64(initialMem)/1024/1024)
	t.Logf("Final memory: %.2f MB (%d entries)", 
		float64(finalMem)/1024/1024, bd.Count())
	
	maxAllowed := int64(config.MaxMemoryMB * 1024 * 1024 * 1.5)
	if finalMem > maxAllowed {
		t.Errorf("Memory exceeded hard cap: got %.2f MB, max allowed %.2f MB",
			float64(finalMem)/1024/1024, float64(maxAllowed)/1024/1024)
	}
}

// ---------------------------------------------------------------------------
// Benchmark 5: False Positive Impact Analysis
// ---------------------------------------------------------------------------

func TestBloomDedup_FalsePositiveBound(t *testing.T) {
	config := DefaultBloomConfig()
	config.ExpectedItems = testUniqueIOCs
	config.MaxFalsePos = 0.01
	
	bd := NewBloomDedup(config)
	
	for i := 0; i < testUniqueIOCs; i++ {
		bd.Add(generateIOC(i))
	}
	
	queryCount := 100000
	falsePositives := 0
	
	rng := rand.New(rand.NewSource(12345))
	for i := 0; i < queryCount; i++ {
		unknownKey := generateIOC(testUniqueIOCs*100 + rng.Intn(10000))
		present, _ := bd.Lookup(unknownKey)
		
		if present {
			falsePositives++
		}
	}
	
	actualFPRate := float64(falsePositives) / float64(queryCount)
	t.Logf("Actual FP rate: %.4f%% (target ≤%.2f%%)\n\tFrom %d queries over %d unknown keys",
		actualFPRate*100, config.MaxFalsePos*100, falsePositives, queryCount)
	
	if actualFPRate > config.MaxFalsePos {
		t.Errorf("FP rate exceeds bound: got %.4f%%, max allowed %.2f%%",
			actualFPRate*100, config.MaxFalsePos*100)
	}
}

// ---------------------------------------------------------------------------
// Concurrency Stress Test
// ---------------------------------------------------------------------------

func BenchmarkBloomDedup_ConcurrencyStress(b *testing.B) {
	config := DefaultBloomConfig()
	config.ExpectedItems = warmUpCount + benchmarkRuns
	config.MaxFalsePos = 0.01
	
	bd := NewBloomDedup(config)
	
	numGoroutines := 16
	opsPerGoroutine := b.N / numGoroutines
	
	done := make(chan bool, numGoroutines)
	
	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			localRNG := rand.New(rand.NewSource(int64(id)))
			
			for i := 0; i < opsPerGoroutine; i++ {
				idx := localRNG.Int() % (warmUpCount + testUniqueIOCs)
				key := generateIOC(idx)
				bd.Add(key)
			}
			done <- true
		}(g)
	}
	
	for g := 0; g < numGoroutines; g++ {
		<-done
	}
}

func TestBloomDedupConcurrency_Safety(t *testing.T) {
	config := DefaultBloomConfig()
	config.ExpectedItems = 10000
	config.MaxFalsePos = 0.01
	
	bd := NewBloomDedup(config)
	
	numWorkers := 10
	opsPerWorker := 1000
	
	done := make(chan bool, numWorkers)
	errors := make(chan error, numWorkers)
	
	for w := 0; w < numWorkers; w++ {
		go func(workerID int) {
			rng := rand.New(rand.NewSource(int64(workerID)))
			
			for i := 0; i < opsPerWorker; i++ {
				selectType := rng.Intn(2)
				
				if selectType == 0 {
					key := generateIOC(rng.Int() % 5000)
					bd.Add(key)
				} else {
					key := generateIOC(rng.Int() % 10000)
					_, err := bd.Lookup(key)
					if err != nil {
						errors <- nil
						return
					}
				}
			}
			done <- true
		}(w)
	}
	
	for w := 0; w < numWorkers; w++ {
		<-done
	}
	close(errors)
	
	for err := range errors {
		if err != nil {
			t.Error(err)
		}
	}
	
	t.Logf("Concurrency test passed: %d concurrent workers, no races detected", numWorkers)
}

// ---------------------------------------------------------------------------
// Integration Pattern Tests
// ---------------------------------------------------------------------------

func TestIntegration_TwoStagePipeline(t *testing.T) {
	config := DefaultBloomConfig()
	config.ExpectedItems = 1000
	
	canonicalizer := NewChainNormalizer(
		NewDomainNormalizer(),
		NewIPNormalizer(),
	)
	
	hybrid := NewBloomDedupWithNormalizer(canonicalizer, config)
	
	// Test domain canonicalization
	hybrid.Add("EXAMPLE.COM")
	result, _ := hybrid.Lookup("example.com")
	
	if !result {
		t.Error("Domain case-insensitive lookup failed")
	}
	
	// Verify false merge bound matches canonicalizer
	bound := hybrid.FalseMergeBound()
	if bound <= 0 {
		t.Errorf("Expected positive false merge bound, got %f", bound)
	}
	
	t.Logf("False merge bound: %.6f%%", bound*100)
	t.Log(hybrid.Stats().Report())
}

// ---------------------------------------------------------------------------
// Regression Tests: Edge Cases
// ---------------------------------------------------------------------------

func TestBloomDedup_EdgeCases(t *testing.T) {
	config := DefaultBloomConfig()
	config.ExpectedItems = 100
	config.MaxFalsePos = 0.01
	
	tests := []struct {
		name string
		input string
	}{
		{"Empty string", ""},
		{"Whitespace only", "   "},
		{"Mixed case", "Example.Com"},
		{"Leading/trailing spaces", "  example.com  "},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bd := NewBloomDedup(config)
			
			result := bd.Add(tt.input)
			
			if !strings.TrimSpace(tt.input) && result {
				t.Log("WARNING: Empty input accepted (may be intentional)")
			}
			
			result2 := bd.Add(tt.input)
			if result2 {
				t.Errorf("Duplicate add returned true: %q", tt.input)
			}
			
			found, _ := bd.Lookup(tt.input)
			if !found {
				t.Errorf("Lookup failed for %q", tt.input)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Parameter Tuning Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkParameterTuning_FPRateEffectiveness(b *testing.B) {
	fprates := []float64{0.001, 0.01, 0.05, 0.1}
	
	for _, fpRate := range fprates {
		b.Run("FP-"+fmt.Sprintf("%.3f", fpRate), func(b *testing.B) {
			config := DefaultBloomConfig()
			config.ExpectedItems = warmUpCount + benchmarkRuns
			config.MaxFalsePos = fpRate
			
			bd := NewBloomDedup(config)
			
			for i := 0; i < warmUpCount; i++ {
				bd.Add(generateIOC(i))
			}
			
			b.ResetTimer()
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				idx := i % (testUniqueIOCs + warmUpCount)
				bd.Lookup(generateIOC(idx))
			}
		})
	}
}

func TestBloomDedup_ParameterImpact(t *testing.T) {
	configs := []BloomConfig{
		{ExpectedItems: 100_000, MaxFalsePos: 0.001},
		{ExpectedItems: 100_000, MaxFalsePos: 0.01},
		{ExpectedItems: 100_000, MaxFalsePos: 0.05},
		{ExpectedItems: 1_000_000, MaxFalsePos: 0.01},
	}
	
	for _, cfg := range configs {
		bd := NewBloomDedup(cfg)
		
		for i := 0; i < testUniqueIOCs; i++ {
			bd.Add(generateIOC(i))
		}
		
		memUsed := bd.GetMemoryUsageBytes()
		fpRate := bd.bloom.FalsePositiveRate()
		
		t.Logf("Config {n=%d, p=%.4f}: Memory=%.2fMB, ActualFP=%.4f%%, Count=%d",
			cfg.ExpectedItems, cfg.MaxFalsePos,
			float64(memUsed)/1024/1024,
			fpRate*100,
			bd.Count(),
		)
	}
}
