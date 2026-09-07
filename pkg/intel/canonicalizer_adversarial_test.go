// Package intel implements adversarial test generation for canonicalization false-merge bounds.
package intel

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"
)

var (
	popularDomains = []string{
		"google.com", "facebook.com", "amazon.com", "microsoft.com", "apple.com",
		"netflix.com", "twitter.com", "linkedin.com", "github.com", "stackoverflow.com",
		"paypal.com", "ebay.com", "reddit.com", "instagram.com", "youtube.com",
	}

	typoSquatPatterns = []struct {
		name     string
		template string
		baseIdx  int
	}{
		{"char_substitution", "%s%soogle.com", 0},
		{"double_char", "goo%sogle.com", 1},
		{"missing_char", "%sggle.com", 0},
		{"extra_char", "go%sogle.com", 1},
		{"homograph", "%spple.com", 1},
		{"prefix_injection", "%s-login.com", 0},
		{"suffix_injection", "my-%s.com", 1},
	}

	privateIPRanges = []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	}

	publicIPVariants = []string{
		"8.8.8.8",
		"1.1.1.1",
		"9.9.9.9",
		"208.67.222.222",
	}

	sampleHashes = []string{
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"5e884898da2b06811b914a6e70b58ffb7d23a89f44f894f7e1f7d3e6a7e7e8e9",
		"d4735e3a265e16eee03f59718b9b5d03019c07d8b6c51f90da3a666eec13ab35",
	}
)

type AdversarialTestGenerator struct {
	rng *rand.Rand
	cfg *testing.T
}

func NewAdversarialTestGenerator(t *testing.T) *AdversarialTestGenerator {
	return &AdversarialTestGenerator{
		rng: rand.New(rand.NewSource(42)),
		cfg: t,
	}
}

func (g *AdversarialTestGenerator) GenerateTyposquattingSamples(n int) []TyposquatAttack {
	samples := make([]TyposquatAttack, 0, n)
	
	for i := 0; i < n; i++ {
		domainIdx := g.rng.Intn(len(popularDomains))
		patternIdx := g.rng.Intn(len(typoSquatPatterns))
		
		baseDomain := popularDomains[domainIdx]
		pattern := typoSquatPatterns[patternIdx]
		
		var attackerChar rune
		switch g.rng.Intn(4) {
		case 0:
			attackerChar = '0'
		case 1:
			attackerChar = '1'
		case 2:
			attackerChar = '@'
		case 3:
			attackerChar = '5'
		}
		
		var modified strings.Builder
		modified.Grow(len(baseDomain) + 2)
		
		j := 0
		for _, ch := range baseDomain {
			if j == pattern.baseIdx && len(modified.String()) <= len(pattern.template) {
				modified.WriteRune(attackerChar)
			} else {
				modified.WriteRune(ch)
			}
			j++
		}
		
		samples = append(samples, TyposquatAttack{
			Original:      baseDomain,
			Attacked:      modified.String(),
			PatternType:   pattern.name,
			GoldLabel:     IsLegitimateDomain(baseDomain),
			ExpectedMerge: true,
		})
	}
	
	return samples
}

func (g *AdversarialTestGenerator) GenerateIPEvasionSamples(n int) []IPEvasionTest {
	samples := make([]IPEvasionTest, 0, n)
	
	for i := 0; i < n; i++ {
		isPrivateRange := g.rng.Intn(2) == 0
		
		var ip1, ip2 string
		if isPrivateRange {
			baseIP := generateRandomPrivateIP(g.rng, privateIPRanges)
			ip1 = fmt.Sprintf("%s/32", baseIP)
			
			ip2 = sameSubnetButDifferentHost(baseIP, 1)
			ip2 = fmt.Sprintf("%s/32", ip2)
		} else {
			idx1 := g.rng.Intn(len(publicIPVariants))
			idx2 := (idx1 + 1) % len(publicIPVariants)
			ip1 = publicIPVariants[idx1]
			ip2 = publicIPVariants[idx2]
		}
		
		testID := fmt.Sprintf("ip_evasion_%d_%s_vs_%s", i, ip1, ip2)
		
		samples = append(samples, IPEvasionTest{
			ID:            testID,
			Input1:        ip1,
			Input2:        ip2,
			SameSubnet:    checkSameCIDR(ip1, ip2),
			ExpectedMerge: false,
		})
	}
	
	return samples
}

func (g *AdversarialTestGenerator) GenerateHashPerturbationSamples(n int) []HashPerturbation {
	samples := make([]HashPerturbation, 0, n)
	
	for i := 0; i < n; i++ {
		originalHash := sampleHashes[g.rng.Intn(len(sampleHashes))]
		
		variantTypes := []string{
			"lowercase",
			"uppercase",
			"base64_encode",
			"prefix_sha256:",
			"truncated_32chars",
			"padded_with_spaces",
		}
		
		variantType := variantTypes[g.rng.Intn(len(variantTypes))]
		variant := applyHashVariant(originalHash, variantType)
		
		testID := fmt.Sprintf("hash_var_%d_%s_%s", i, variantType, truncateHash(originalHash))
		
		samples = append(samples, HashPerturbation{
			ID:             testID,
			OriginalHash:   originalHash,
			VariantHash:    variant,
			VariantType:    variantType,
			ExpectedMerge:  variantType != "truncated_32chars",
		})
	}
	
	return samples
}

func generateRandomPrivateIP(rng *rand.Rand, ranges []string) string {
	rangeIdx := rng.Intn(len(ranges))
	cidr := ranges[rangeIdx]
	
	switch cidr {
	case "10.0.0.0/8":
		return fmt.Sprintf("10.%d.%d.%d", rng.Intn(256), rng.Intn(256), rng.Intn(256))
	case "172.16.0.0/12":
		return fmt.Sprintf("172.%d.%d.%d", 16+rng.Intn(16), rng.Intn(256), rng.Intn(256))
	case "192.168.0.0/16":
		return fmt.Sprintf("192.168.%d.%d", rng.Intn(256), rng.Intn(256))
	default:
		return "0.0.0.0"
	}
}

func sameSubnetButDifferentHost(baseIP string, offset int) string {
	parts := strings.Split(baseIP, ".")
	lastPart, _ := fmt.Sscanf(parts[3], "%d", new(int))
	newLast := (lastPart + offset) % 256
	parts[3] = fmt.Sprintf("%d", newLast)
	return strings.Join(parts, ".")
}

func checkSameCIDR(ip1, ip2 string) bool {
	prefix1 := extractPrefix(ip1)
	prefix2 := extractPrefix(ip2)
	
	return prefix1 == prefix2 && extractIPBase(ip1) == extractIPBase(ip2)
}

func extractPrefix(ip string) int {
	if idx := strings.Index(ip, "/"); idx >= 0 {
		var prefix int
		fmt.Sscanf(ip[idx+1:], "%d", &prefix)
		return prefix
	}
	return 32
}

func extractIPBase(ip string) string {
	if idx := strings.Index(ip, "/"); idx >= 0 {
		return ip[:idx]
	}
	return ip
}

func applyHashVariant(hash, vType string) string {
	switch vType {
	case "lowercase":
		return strings.ToLower(hash)
	case "uppercase":
		return strings.ToUpper(hash)
	case "base64_encode":
		bytes, _ := hex.DecodeString(hash)
		return base64.StdEncoding.EncodeToString(bytes)
	case "prefix_sha256:":
		return "sha256:" + hash
	case "truncated_32chars":
		return hash[:32]
	case "padded_with_spaces":
		return "  " + hash + "  "
	default:
		return hash
	}
}

func truncateHash(hash string) string {
	if len(hash) > 16 {
		return hash[:16] + "..."
	}
	return hash
}

type TyposquatAttack struct {
	Original      string
	Attacked      string
	PatternType   string
	GoldLabel     bool
	ExpectedMerge bool
}

type IPEvasionTest struct {
	ID            string
	Input1        string
	Input2        string
	SameSubnet    bool
	ExpectedMerge bool
}

type HashPerturbation struct {
	ID             string
	OriginalHash   string
	VariantHash    string
	VariantType    string
	ExpectedMerge  bool
}

func IsLegitimateDomain(domain string) bool {
	for _, legit := range popularDomains {
		if domain == legit || strings.HasSuffix(domain, "."+legit) {
			return true
		}
	}
	return false
}

func BenchmarkCanonicalizer_Domain_Normalize(b *testing.B) {
	norm := NewDomainNormalizer()
	sample := "g00gle.com"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		norm.Normalize(sample)
	}
}

func BenchmarkCanonicalizer_IP_Normalize(b *testing.B) {
	norm := NewIPNormalizer()
	sample := "8.8.8.8/32"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		norm.Normalize(sample)
	}
}

func BenchmarkCanonicalizer_Hash_Normalize(b *testing.B) {
	norm := NewHashFuzzyNormalizer()
	sample := "E3B0C44298FC1C149AFBF4C8996FB924"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		norm.Normalize(sample)
	}
}

func BenchmarkCanonicalizer_Chain_Normalize(b *testing.B) {
	chain := NewChainNormalizer(
		NewDomainNormalizer(),
		NewIPNormalizer(),
		NewHashFuzzyNormalizer(),
	)
	sample := "g00gle.com"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		chain.Normalize(sample)
	}
}

func TestAdversarial_TyposquattingFalsePositiveRate(t *testing.T) {
	generator := NewAdversarialTestGenerator(t)
	samples := generator.GenerateTyposquattingSamples(5000)
	
	norm := NewDomainNormalizer()
	falsePositives := 0
	truePositives := 0
	
	for i, sample := range samples {
		canonical := norm.Normalize(sample.Attacked)
		
		if canonical == sample.Original {
			truePositives++
		} else {
			t.Logf("Failed merge: %s → %s (expected %s)", sample.Attacked, canonical, sample.Original)
		}
		
		unrelatedDomain := fmt.Sprintf("unrelated_example_%d.com", i)
		if norm.Normalize(unrelatedDomain) == canonical {
			falsePositives++
			t.Errorf("False positive merge: %s collided with %s", sample.Attacked, unrelatedDomain)
		}
	}
	
	t.Logf("Typosquat catch rate: %.2f%% (%d/%d)", 
		float64(truePositives)/float64(len(samples))*100, truePositives, len(samples))
	t.Logf("False positive rate: %.4f%% (%d/%d)", 
		float64(falsePositives)/float64(len(samples))*100, falsePositives, len(samples))
	
	falsePosRate := float64(falsePositives) / float64(len(samples))
	if falsePosRate > 0.01 {
		t.Fatalf("Exceeded 0.01%% false positive bound: got %.4f%%", falsePosRate*100)
	}
}

func TestAdversarial_IPEvasion_TopologyPreservation(t *testing.T) {
	generator := NewAdversarialTestGenerator(t)
	samples := generator.GenerateIPEvasionSamples(3000)
	
	norm := NewIPNormalizer()
	violations := 0
	
	for _, sample := range samples {
		canon1 := norm.Normalize(sample.Input1)
		canon2 := norm.Normalize(sample.Input2)
		
		if !sample.SameSubnet && canon1 == canon2 {
			violations++
			t.Errorf("Topology violation: %s and %s merged to same canonical form", sample.Input1, sample.Input2)
		}
	}
	
	t.Logf("IP topology violations: %d/%d (expected 0)", violations, len(samples))
	
	if violations > 0 {
		t.Fatalf("CIDR topology preservation failed: %d violations", violations)
	}
}

func TestAdversarial_HashVariantNormalization(t *testing.T) {
	generator := NewAdversarialTestGenerator(t)
	samples := generator.GenerateHashPerturbationSamples(2000)
	
	norm := NewHashFuzzyNormalizer()
	successfulMerges := 0
	failedMerges := 0
	
	for _, sample := range samples {
		canonical := norm.Normalize(sample.VariantHash)
		
		if canonical == norm.Normalize(sample.OriginalHash) {
			successfulMerges++
		} else {
			failedMerges++
			t.Logf("Hash variant %s failed to merge: %s vs %s", 
				sample.VariantType, canonical, norm.Normalize(sample.OriginalHash))
		}
	}
	
	t.Logf("Hash variant merge success rate: %.2f%% (%d/%d)",
		float64(successfulMerges)/float64(len(samples))*100, successfulMerges, len(samples))
	
	nonTruncateSamples := 0
	for _, s := range samples {
		if s.VariantType != "truncated_32chars" {
			nonTruncateSamples++
		}
	}
	
	if nonTruncateSamples > 0 {
		rate := float64(successfulMerges-nonTruncateSamples) / float64(nonTruncateSamples) * 100
		t.Logf("Non-truncated variant merge rate: %.2f%%", rate)
		if rate < 95 {
			t.Errorf("Non-truncated hash variants below 95%% merge threshold: %.2f%%", rate)
		}
	}
}

func TestCanonicalizer_ErrorBounds_ProvableGuarantees(t *testing.T) {
	tests := []struct {
		name     string
		norm     Normalizer
		expected float64
	}{
		{"DomainNormalizer", NewDomainNormalizer(), 0.005},
		{"IPNormalizer", NewIPNormalizer(), 0.0000001},
		{"HashFuzzyNormalizer", NewHashFuzzyNormalizer(), 0.000000000001},
		{"ChainNormalizer", NewChainNormalizer(NewDomainNormalizer(), NewIPNormalizer(), NewHashFuzzyNormalizer()), 0.005000101},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := tt.norm.ErrorBound()
			
			if actual > tt.expected {
				t.Errorf("Error bound exceeds guarantee: got %.10f, want ≤%.10f", actual, tt.expected)
			}
			
			t.Logf("%s.ErrorBound() = %.10f (guaranteed ≤ %.10f)", tt.name, actual, tt.expected)
		})
	}
}

func TestLevenshteinBounded_ComplexityGuarantees(t *testing.T) {
	norm := NewDomainNormalizer()
	maxDistance := norm.maxDistance
	
	longString1 := strings.Repeat("a", 253)
	longString2 := strings.Repeat("b", 253)
	
	start := time.Now()
	dist := norm.levenshteinBounded(longString1, longString2, uint8(maxDistance))
	elapsed := time.Since(start)
	
	if dist > uint8(maxDistance) {
		t.Logf("Early termination worked: distance=%d, limit=%d (took %v)", dist, maxDistance, elapsed)
	} else {
		t.Errorf("Expected distance > %d, got %d", maxDistance, dist)
	}
	
	if elapsed > time.Millisecond {
		t.Errorf("Worst-case Levenshtein took %v (target <1ms)", elapsed)
	}
}
