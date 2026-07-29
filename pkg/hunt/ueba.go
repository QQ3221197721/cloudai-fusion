package hunt

import (
	"fmt"
	"math"
	"sort"
	"sync"
)

// ueba.go implements User & Entity Behavior Analytics (UEBA) for the L2 hunting
// well — the real statistical core that products like Splunk UBA, Exabeam, and
// Elastic ML anomaly detection are built on. Instead of only matching known
// IOCs/CVEs (which cannot catch novel or insider activity), it LEARNS a per-entity
// baseline and flags statistically anomalous behavior:
//
//   - numeric deviation: an online mean/variance baseline (Welford's algorithm)
//     per (entity, metric); a new value is scored by its Z-score (σ from mean).
//   - categorical rarity / first-seen: a per-(entity, dimension) frequency model
//     flags never-before-seen values and values below a rarity threshold (e.g. a
//     login from a new country, a host running an unusual process).
//
// The analyzer is dependency-free and deterministic, so it is fully unit-testable
// and safe in CI. It is intentionally domain-agnostic: it emits Anomaly records;
// the hunt.Engine maps them to MITRE ATT&CK findings.

// Observation is one behavioral event for an entity (a user, host, service...).
type Observation struct {
	Entity     string             `json:"entity"`               // e.g. "user:alice", "host:web-01"
	Metrics    map[string]float64 `json:"metrics,omitempty"`    // numeric features
	Categories map[string]string  `json:"categories,omitempty"` // categorical features
}

// AnomalyKind classifies a detected behavioral anomaly.
type AnomalyKind string

const (
	AnomalyNumericDeviation AnomalyKind = "numeric_deviation"
	AnomalyRareCategory     AnomalyKind = "rare_category"
	AnomalyFirstSeen        AnomalyKind = "first_seen"
)

// Anomaly is one behavioral deviation detected against an entity's baseline.
type Anomaly struct {
	Entity  string      `json:"entity"`
	Kind    AnomalyKind `json:"kind"`
	Feature string      `json:"feature"`
	Value   string      `json:"value"`
	Score   float64     `json:"score"` // Z-score for numeric; (1-frequency) for rarity
	Detail  string      `json:"detail"`
}

// AnalyzerConfig tunes the UEBA thresholds. Zero values fall back to sensible
// defaults, so NewAnalyzer(AnalyzerConfig{}) is valid.
type AnalyzerConfig struct {
	ZThreshold      float64 // σ threshold to flag a numeric deviation (default 3.0)
	MinSamples      int     // min baseline samples before numeric scoring (default 20)
	RarityThreshold float64 // relative-frequency threshold for "rare" (default 0.02)
	MinCatSamples   int     // min baseline samples before rarity scoring (default 20)
}

func (c *AnalyzerConfig) withDefaults() {
	if c.ZThreshold <= 0 {
		c.ZThreshold = 3.0
	}
	if c.MinSamples <= 0 {
		c.MinSamples = 20
	}
	if c.RarityThreshold <= 0 {
		c.RarityThreshold = 0.02
	}
	if c.MinCatSamples <= 0 {
		c.MinCatSamples = 20
	}
}

// welford maintains an online mean and variance (Welford's numerically stable
// one-pass algorithm), so baselines update in O(1) per sample with no history.
type welford struct {
	n    int
	mean float64
	m2   float64
}

func (w *welford) update(x float64) {
	w.n++
	delta := x - w.mean
	w.mean += delta / float64(w.n)
	w.m2 += delta * (x - w.mean)
}

func (w *welford) variance() float64 {
	if w.n < 2 {
		return 0
	}
	return w.m2 / float64(w.n-1)
}

func (w *welford) stddev() float64 { return math.Sqrt(w.variance()) }

// catCounter tracks observed frequencies of categorical values.
type catCounter struct {
	total  int
	counts map[string]int
}

// Analyzer learns per-entity behavioral baselines and scores new observations.
// It is concurrency-safe.
type Analyzer struct {
	mu          sync.Mutex
	numeric     map[string]*welford
	categorical map[string]*catCounter
	cfg         AnalyzerConfig
}

// NewAnalyzer builds an analyzer with the given config (zero value = defaults).
func NewAnalyzer(cfg AnalyzerConfig) *Analyzer {
	cfg.withDefaults()
	return &Analyzer{
		numeric:     make(map[string]*welford),
		categorical: make(map[string]*catCounter),
		cfg:         cfg,
	}
}

func baselineKey(entity, feature string) string { return entity + "\x00" + feature }

// Train updates the baselines from obs WITHOUT scoring — used to warm up a model
// from known-good historical activity before detection begins.
func (a *Analyzer) Train(obs Observation) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.learn(obs)
}

// Observe scores obs against the CURRENT baseline (returning any anomalies), then
// folds obs into the baseline. Scoring-before-learning ensures an anomalous value
// cannot mask itself by polluting its own baseline.
func (a *Analyzer) Observe(obs Observation) []Anomaly {
	a.mu.Lock()
	defer a.mu.Unlock()
	anomalies := a.score(obs)
	a.learn(obs)
	return anomalies
}

// score evaluates obs against existing baselines (no mutation).
func (a *Analyzer) score(obs Observation) []Anomaly {
	var out []Anomaly

	for _, m := range sortedFloatKeys(obs.Metrics) {
		x := obs.Metrics[m]
		w := a.numeric[baselineKey(obs.Entity, m)]
		if w == nil || w.n < a.cfg.MinSamples {
			continue // still learning; cannot honestly score yet
		}
		sd := w.stddev()
		var z float64
		switch {
		case sd == 0 && x == w.mean:
			z = 0
		case sd == 0:
			z = 10 // zero-variance baseline; any different value is a strong outlier
		default:
			z = math.Abs(x-w.mean) / sd
		}
		if z >= a.cfg.ZThreshold {
			out = append(out, Anomaly{
				Entity: obs.Entity, Kind: AnomalyNumericDeviation, Feature: m,
				Value: fmt.Sprintf("%.4g", x), Score: z,
				Detail: fmt.Sprintf("%s=%.4g deviates %.1fσ from baseline mean %.4g (stddev %.4g, n=%d)",
					m, x, z, w.mean, sd, w.n),
			})
		}
	}

	for _, dim := range sortedStringKeys(obs.Categories) {
		v := obs.Categories[dim]
		cc := a.categorical[baselineKey(obs.Entity, dim)]
		if cc == nil || cc.total < a.cfg.MinCatSamples {
			continue
		}
		cnt := cc.counts[v]
		if cnt == 0 {
			out = append(out, Anomaly{
				Entity: obs.Entity, Kind: AnomalyFirstSeen, Feature: dim, Value: v, Score: 1.0,
				Detail: fmt.Sprintf("first-ever %s=%q for %s (baseline %d obs, %d distinct)",
					dim, v, obs.Entity, cc.total, len(cc.counts)),
			})
			continue
		}
		if freq := float64(cnt) / float64(cc.total); freq < a.cfg.RarityThreshold {
			out = append(out, Anomaly{
				Entity: obs.Entity, Kind: AnomalyRareCategory, Feature: dim, Value: v, Score: 1 - freq,
				Detail: fmt.Sprintf("rare %s=%q for %s (%.2f%% of %d obs)",
					dim, v, obs.Entity, freq*100, cc.total),
			})
		}
	}
	return out
}

// learn folds obs into the baselines.
func (a *Analyzer) learn(obs Observation) {
	for m, x := range obs.Metrics {
		k := baselineKey(obs.Entity, m)
		w := a.numeric[k]
		if w == nil {
			w = &welford{}
			a.numeric[k] = w
		}
		w.update(x)
	}
	for dim, v := range obs.Categories {
		k := baselineKey(obs.Entity, dim)
		cc := a.categorical[k]
		if cc == nil {
			cc = &catCounter{counts: make(map[string]int)}
			a.categorical[k] = cc
		}
		cc.total++
		cc.counts[v]++
	}
}

func sortedFloatKeys(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedStringKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
