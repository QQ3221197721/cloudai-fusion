package qa

import (
	"sort"
)

// regression.go is the Performance Regressor: comparing a stored baseline benchmark
// run against a current run and flagging any sample that regressed beyond configurable
// time/allocation budgets. Zero means "no check", positive numbers are maximum %
// degradation allowed in that dimension.

// RegressorResult reports the outcome of comparing baseline against current under
// a given config. Budgets expressed as percentages: 10.0 means up to 10% worse is OK.
type RegressorResult struct {
	Pass       bool
	Violations []RegressionViolation
}

// RegressConfig controls the sensitivity of the performance regressor. Zero-budgets
// mean "no check". Positive values are maximum % degradation allowed.
type RegressConfig struct {
	MaxTimePct     float64 // max % increase for ns/op
	MaxAllocBytesPct float64 // max % increase for bytes/op
	MaxAllocOpsPct   float64 // max % increase for allocs/op
}

// RegressionViolation describes one exceeded budget when moving from baseline to
// current. Fields report the absolute baseline/current values along with the
// percentage change and the configured budget.
type RegressionViolation struct {
	Sample string // Benchmark name (e.g. "Parse/8")
	Metric string // "time", "alloc_bytes", or "alloc_ops"
	Baseline float64
	Cur      float64
	PctDiff  float64 // percent worse relative to baseline
	Budget   float64 // max allowed %
}

func (v RegressionViolation) String() string {
	return v.Sample + "/" + v.Metric + ":" + itof(v.Baseline) + "→" + itof(v.Cur) +
		" diff=" + itof(v.PctDiff) + "% (budget " + itof(v.Budget) + "%)"
}

// Regress compares baseline to current under cfg and returns violations sorted by
// Sample/Metric deterministically. If either input is nil/empty, pass is true.
func Regress(baseline, current *BenchRun, cfg RegressConfig) RegressorResult {
	if baseline == nil || current == nil || len(baseline.Samples) == 0 {
		return RegressorResult{Pass: true, Violations: nil}
	}
	m := make(map[string]BenchSample, len(baseline.Samples))
	for _, s := range baseline.Samples {
		m[s.Name] = s
	}
	var vio []RegressionViolation
	for _, cur := range current.Samples {
		base, ok := m[cur.Name]
		if !ok {
			continue // new benchmarks have no history; treat as acceptable
		}
		budget := cfg.MaxTimePct
		if baseTime(base.NsPerOp) && curTime(cur.NsPerOp) {
			pct := percentDelta(base.NsPerOp, cur.NsPerOp)
			if pct >= budget && budget > 0 {
				vio = append(vio, RegressionViolation{
					Sample: cur.Name, Metric: "time", Baseline: base.NsPerOp, Cur: cur.NsPerOp, PctDiff: pct, Budget: budget,
				})
			}
		}
		if baseAlloc(base.BytesPerOp) && curAlloc(cur.BytesPerOp) {
			pct := percentDelta(float64(base.BytesPerOp), float64(cur.BytesPerOp))
			bgt := cfg.MaxAllocBytesPct
			if bgt > 0 && pct >= bgt {
				vio = append(vio, RegressionViolation{
					Sample: cur.Name, Metric: "alloc_bytes", Baseline: float64(base.BytesPerOp), Cur: float64(cur.BytesPerOp), PctDiff: pct, Budget: bgt,
				})
			}
		}
		if baseAlloc(base.AllocsPerOp) && curAlloc(cur.AllocsPerOp) {
			pct := percentDelta(float64(base.AllocsPerOp), float64(cur.AllocsPerOp))
			bgt := cfg.MaxAllocOpsPct
			if bgt > 0 && pct >= bgt {
				vio = append(vio, RegressionViolation{
					Sample: cur.Name, Metric: "alloc_ops", Baseline: float64(base.AllocsPerOp), Cur: float64(cur.AllocsPerOp), PctDiff: pct, Budget: bgt,
				})
			}
		}
	}
	sort.SliceStable(vio, func(i, j int) bool {
		if vio[i].Sample != vio[j].Sample { return vio[i].Sample < vio[j].Sample }
		return vio[i].Metric < vio[j].Metric
	})
	return RegressorResult{Pass: len(vio) == 0, Violations: vio}
}

func baseTime(t float64) bool { return t > 0 }
func curTime(t float64) bool { return t > 0 }
func baseAlloc(a int64) bool { return a >= 0 }
func curAlloc(a int64) bool { return a >= 0 }
func percentDelta(old, new float64) float64 {
	if old == 0 { return 0 }
	return (new-old)/old*100
}
func itof(f float64) string { 
	s := ""
	if f < 0 { s = "-" ; f = -f }
	i, frac := int64(f), int64((f-float64(int64(f)))*1000)
	s += formatInt64(i) + "." + formatInt32(int32(frac))
	return s
}
func formatInt64(x int64) string {
	if x == 0 { return "0" }
	const digits = "0123456789"
	s := [20]byte{}
	i := 0
	for x > 0 {
		s[i] = digits[int(x%10)]
		x /= 10
		i++
	}
	r := [20]byte{}
	for j := 0; j < i; j++ {
		r[j] = s[i-j-1]
	}
	return string(r[:i])
}
func formatInt32(x int32) string {
	digs := "0123456789"
	s := [10]byte{}
	i := 0
	if x == 0 {
		return "0"
	}
	for x > 0 {
		s[i] = digs[x%10]
		x /= 10
		i++
	}
	for j := 0; j < i; j++ {
		s[i-j-1], s[j] = s[j], s[i-j-1]
	}
	return string(s[:i])
}
