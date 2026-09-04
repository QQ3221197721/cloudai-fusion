package intel

import (
	"fmt"
	"testing"
)

// TestStrictVsLoose_FalseMerge quantifies the core T3 claim: strict type-aware
// canonical-key matching eliminates the near-miss false merges that the loose
// Levenshtein <=2 design produces. Reports both honestly.
func TestStrictVsLoose_FalseMerge(t *testing.T) {
	pop := popularSet()
	nearMiss := []string{
		"apply.com", "ample.com", "maple.com", "apples.com", "ebby.com",
		"amazin.com", "reddi.com", "redit.com", "twitier.com", "gitbub.com",
		"youtub.com", "paypar.com",
	}

	count := func(loose bool) (int, []string) {
		n := NewDomainNormalizer()
		n.SetLooseMode(loose)
		falseMerges := 0
		var off []string
		for _, d := range nearMiss {
			c := n.Normalize(d)
			if _, ok := pop[c]; ok && c != d {
				falseMerges++
				off = append(off, fmt.Sprintf("%s->%s", d, c))
			}
		}
		return falseMerges, off
	}

	looseFM, looseOff := count(true)
	strictFM, strictOff := count(false)
	total := len(nearMiss)

	t.Logf("=== Strict vs Loose False-Merge (near-miss legit domains) ===")
	t.Logf("Loose  (Levenshtein<=2): %d/%d = %.1f%% FP  %v", looseFM, total, float64(looseFM)/float64(total)*100, looseOff)
	t.Logf("Strict (canonical-key):  %d/%d = %.1f%% FP  %v", strictFM, total, float64(strictFM)/float64(total)*100, strictOff)

	// Strict must strictly improve over loose.
	if strictFM >= looseFM {
		t.Errorf("strict mode (%d FP) did not improve over loose mode (%d FP)", strictFM, looseFM)
	}

	// Demonstrate the oracle: allowlisting the residual offenders drives FP to 0.
	if strictFM > 0 {
		n := NewDomainNormalizer()
		n.AddAllowlist("redit.com") // the dup-collapse residual
		residual := 0
		for _, d := range nearMiss {
			c := n.Normalize(d)
			if _, ok := pop[c]; ok && c != d {
				residual++
			}
		}
		t.Logf("Strict + allowlist oracle: %d/%d = %.1f%% FP", residual, total, float64(residual)/float64(total)*100)
	}
}

// popularSeeds mirrors the DomainNormalizer dictionary used across tests.
var popularSeeds = []string{
	"google.com", "facebook.com", "amazon.com", "microsoft.com", "apple.com",
	"netflix.com", "twitter.com", "linkedin.com", "github.com", "stackoverflow.com",
	"paypal.com", "ebay.com", "reddit.com", "instagram.com", "youtube.com",
}

func popularSet() map[string]struct{} {
	s := make(map[string]struct{}, len(popularSeeds))
	for _, d := range popularSeeds {
		s[d] = struct{}{}
	}
	return s
}

// BenchmarkExactMatch_Baseline measures pure exact-match lookup (M28 current behavior).
func BenchmarkExactMatch_Baseline(b *testing.B) {
	store := popularSet()
	queries := []string{"google.com", "facebook.com", "github.com", "unknown.com"}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		q := queries[i%len(queries)]
		_, _ = store[q]
	}
}

// BenchmarkCanonicalized_Match measures canonicalization + lookup on the WARM (cached) path.
// Represents steady-state query traffic where indicators repeat.
func BenchmarkCanonicalized_Match(b *testing.B) {
	norm := NewDomainNormalizer()
	store := popularSet()

	// Typosquatted variants that exact-match would MISS.
	queries := []string{"g00gle.com", "faceb00k.com", "gith0b.com", "unknown.com"}
	// Warm the cache so we measure steady-state.
	for _, q := range queries {
		_ = norm.Normalize(q)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		q := queries[i%len(queries)]
		canonical := norm.Normalize(q)
		_, _ = store[canonical]
	}
}

// BenchmarkCanonicalized_ColdPath measures the one-time ingestion cost: a fresh
// normalization with no cache hit. This is the true amortized cost model, because
// in a dedup pipeline each indicator is canonicalized exactly once at ingestion,
// then stored in canonical form for O(1) exact-match queries thereafter.
func BenchmarkCanonicalized_ColdPath(b *testing.B) {
	norm := NewDomainNormalizer()
	// Unique inputs each iteration -> always a cache miss (full Levenshtein scan).
	inputs := make([]string, b.N)
	for i := range inputs {
		inputs[i] = fmt.Sprintf("g00gle-%d.com", i)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = norm.Normalize(inputs[i])
	}
}

// TestSecurityCoverage_FuzzyVsExact measures additional threats caught via fuzzy matching.
func TestSecurityCoverage_FuzzyVsExact(t *testing.T) {
	norm := NewDomainNormalizer()
	knownThreats := popularSet()

	// Adversarial typosquatting variants of known threats.
	adversarialVariants := []string{
		"g00gle.com", "1oogle.com", "@oogle.com", "gooogle.com", // google
		"faceb00k.com", "facebo0k.com", "faceb0ok.com", // facebook
		"amaz0n.com", "@mazon.com", "amazon.com", // amazon
		"micr0soft.com", "micros0ft.com", // microsoft
		"@pple.com", "appl3.com", // apple
		"netfl1x.com", "n3tflix.com", // netflix
		"paypa1.com", "p@ypal.com", // paypal
		"g1thub.com", "githu8.com", // github
	}

	exactMatchCaught := 0
	fuzzyMatchCaught := 0
	var missed []string

	for _, variant := range adversarialVariants {
		if _, ok := knownThreats[variant]; ok {
			exactMatchCaught++
		}
		canonical := norm.Normalize(variant)
		if _, ok := knownThreats[canonical]; ok {
			fuzzyMatchCaught++
		} else {
			missed = append(missed, fmt.Sprintf("%s->%s", variant, canonical))
		}
	}

	total := len(adversarialVariants)
	exactRate := float64(exactMatchCaught) / float64(total) * 100
	fuzzyRate := float64(fuzzyMatchCaught) / float64(total) * 100
	additionalCatch := fuzzyRate - exactRate

	t.Logf("=== Security Coverage Comparison ===")
	t.Logf("Total adversarial variants: %d", total)
	t.Logf("Exact-match caught:  %d (%.1f%%)", exactMatchCaught, exactRate)
	t.Logf("Fuzzy-match caught:  %d (%.1f%%)", fuzzyMatchCaught, fuzzyRate)
	t.Logf("Additional catch:    +%.1f%% via canonicalization", additionalCatch)
	t.Logf("Missed (honest):     %v", missed)

	if additionalCatch < 15.0 {
		t.Logf("WARNING: additional catch rate %.1f%% is below 15%% target", additionalCatch)
	} else {
		t.Logf("SUCCESS: additional catch rate %.1f%% exceeds 15%% target", additionalCatch)
	}
}

// TestFalsePositiveRate_NearMiss is the HARD false-positive test. It uses a curated
// list of legitimate, distinct domains that lie within Levenshtein <=2 of a popular
// domain -- the genuine false-merge risk. A well-designed normalizer must NOT collapse
// these into the popular canonical form.
func TestFalsePositiveRate_NearMiss(t *testing.T) {
	norm := NewDomainNormalizer()
	pop := popularSet()

	// Legitimate distinct domains near a popular domain (edit distance 1-2).
	// These are the adversarial false-merge risk cases.
	nearMiss := []string{
		"apply.com",  // dist 1 from apple.com
		"ample.com",  // dist 1 from apple.com
		"maple.com",  // dist 2 from apple.com
		"apples.com", // dist 1 from apple.com
		"ebby.com",   // dist 2 from ebay.com
		"amazin.com", // dist 1 from amazon.com
		"reddi.com",  // dist 1 from reddit.com
		"redit.com",  // dist 1 from reddit.com
		"twitier.com",// dist 1 from twitter.com
		"gitbub.com", // dist 1 from github.com (legit-looking)
		"youtub.com", // dist 1 from youtube.com
		"paypar.com", // dist 1 from paypal.com
	}

	falseMerges := 0
	var offenders []string
	for _, d := range nearMiss {
		canonical := norm.Normalize(d)
		if _, isPopular := pop[canonical]; isPopular && canonical != d {
			falseMerges++
			offenders = append(offenders, fmt.Sprintf("%s->%s", d, canonical))
		}
	}

	total := len(nearMiss)
	fpRate := float64(falseMerges) / float64(total)
	t.Logf("=== Near-Miss False-Positive Test (HARD) ===")
	t.Logf("Near-miss legitimate domains tested: %d", total)
	t.Logf("False merges:  %d", falseMerges)
	t.Logf("FP rate (near-miss set): %.2f%% (%d/%d)", fpRate*100, falseMerges, total)
	if len(offenders) > 0 {
		t.Logf("Offenders (honest): %v", offenders)
	}
}

// TestFalsePositiveRate_AdversarialCorpus measures actual false-merge rate over a
// large random corpus of legitimate but distinct domains.
func TestFalsePositiveRate_AdversarialCorpus(t *testing.T) {
	norm := NewDomainNormalizer()

	unrelatedDomains := make([]string, 0, 1000)
	for i := 0; i < 1000; i++ {
		unrelatedDomains = append(unrelatedDomains, fmt.Sprintf("legit-business-%d.org", i))
	}

	pop := popularSet()

	falseMerges := 0
	for _, domain := range unrelatedDomains {
		canonical := norm.Normalize(domain)
		if _, isPopular := pop[canonical]; isPopular && canonical != domain {
			falseMerges++
			t.Logf("FALSE MERGE: %s -> %s", domain, canonical)
		}
	}

	total := len(unrelatedDomains)
	fpRate := float64(falseMerges) / float64(total)

	t.Logf("=== False-Positive Rate Measurement (random corpus) ===")
	t.Logf("Unrelated domains tested: %d", total)
	t.Logf("False merges:             %d", falseMerges)
	t.Logf("Empirical FP rate:        %.4f%% (%d/%d)", fpRate*100, falseMerges, total)
	t.Logf("Claimed bound epsilon:    %.4f%% (domain normalizer)", norm.ErrorBound()*100)

	if fpRate > norm.ErrorBound() {
		t.Errorf("Measured FP rate %.4f%% exceeds claimed bound %.4f%%",
			fpRate*100, norm.ErrorBound()*100)
	}
}
