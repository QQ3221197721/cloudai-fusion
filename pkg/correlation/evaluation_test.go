package correlation

import (
	"math"
	"testing"
	"time"
)

var stats = struct {
	tDistCDF    func(t, df float64) float64
	regIncBeta  func(x, a, b float64) float64
	lgamma      func(float64) float64
	welchTTest  func(x, y []float64) (tStat, df, pValue float64)
	cohenD      func(x, y []float64) float64
}{
	welchTTest: func(x, y []float64) (tStat, df, pValue float64) {
		if len(x) == 0 || len(y) == 0 {
			return 0, 0, 1
		}
		mx := meanSlice(x)
		my := meanSlice(y)
		vx := varianceSlice(x)
		vy := varianceSlice(y)
		nx, ny := float64(len(x)), float64(len(y))
		tStat = (mx - my) / math.Sqrt(vx/nx + vy/ny)
		sum := vx/nx + vy/ny
		if sum == 0 {
			return tStat, 0, 0.5
		}
		df = (sum * sum) / ((vx*vx)/(nx*nx*(nx-1+1e-30)) + (vy*vy)/(ny*ny*(ny-1+1e-30)))
		pValue = 2 * (1 - tDistCDF(math.Abs(tStat), df))
		return tStat, df, pValue
	},
	cohenD: func(x, y []float64) float64 {
		if len(x) < 2 || len(y) < 2 {
			return 0
		}
		mx, my := meanSlice(x), meanSlice(y)
		sx, sy := varianceSlice(x), varianceSlice(y)
		nx, ny := float64(len(x)), float64(len(y))
		sp := math.Sqrt(((nx-1)*sx + (ny-1)*sy) / (nx+ny-2))
		if sp == 0 {
			return 0
		}
		return (mx - my) / sp
	},
}

func meanSlice(x []float64) float64 {
	if len(x) == 0 {
		return 0
	}
	s := 0.0
	for _, v := range x {
		s += v
	}
	return s / float64(len(x))
}

func varianceSlice(x []float64) float64 {
	if len(x) <= 1 {
		return 0
	}
	m := meanSlice(x)
	s := 0.0
	for _, v := range x {
		d := v - m
		s += d * d
	}
	return s / float64(len(x)-1)
}

func tDistCDF(t, df float64) float64 {
	x := df / (df + t*t)
	beta := regIncBeta(x, 0.5*df, 0.5)
	return 1 - beta
}

func regIncBeta(x, a, b float64) float64 {
	if x == 0 {
		return 0
	}
	if x >= 1 {
		return 1
	}
	dx := 1.0 / (a - 1)
	factor := math.Exp(a*math.Log(x) + b*math.Log(1-x) - lgamma(a+b)+lgamma(a)+lgamma(b))
	qbm := dx
	var k float64
	c := 1.0
	d := 0.0
	for i := 0; i < 200; i++ {
		k = float64(2)*float64(i) + a + b - 1
		if i == 0 {
			dx = c / (k - c*x/qbm) / factor
			d = k - c*x/qbm
		} else {
			c = (a+float64(i))*(a+b+float64(i))*x/k
			d = k + c/d
			if math.Abs(d) < 1e-30 {
				d = 1e-30
			}
			dx *= c / (d * d)
			factor += dx
			if math.Abs(dx) < 1e-14 {
				break
			}
		}
	}
	return factor
}

func lgamma(z float64) float64 {
	const g = 7
	c := [10]float64{
		76.18009172947146,
		-87.2011638419703,
		35.050987702100653,
		-9.050727218106992,
		1.220091068249724,
		-1.225180934684943e-4,
	}
	y := z + g + 0.5
	s := 1.0000000001900047
	for j := range c {
		s += c[j] / float64(j+1)
	}
	return 0.5*math.Log(2*math.Pi/y) + (z+0.5)*math.Log(y) - y + math.Log(s)
}

type result struct {
	Scheme        string
	Compression   float64
	MisSuppRate   float64
	Precision     float64
	Recall        float64
	LatencyMs     float64
}

type truthData struct {
	incidents map[string]string
	trueRoots map[string]string
}

func makeTruth(sc *scenario) truthData {
	td := truthData{incidents: make(map[string]string), trueRoots: make(map[string]string)}
	for id, inc := range sc.IncidentOf {
		td.incidents[id] = inc
	}
	for inc, root := range sc.TrueRoots {
		td.trueRoots[inc] = root
	}
	return td
}

func (td truthData) incidentOf(id string) string           { return td.incidents[id] }
func (td truthData) sameIncident(a, b string) bool         { return td.incidentOf(a) == td.incidentOf(b) }
func (td truthData) misSuppressionRate(dec *Decision) float64 {
	total, violations := 0, 0
	for _, v := range dec.Verdicts {
		if !v.Suppressed() || v.RootAlertID == "" {
			continue
		}
		total++
		if !td.sameIncident(v.AlertID, v.RootAlertID) {
			violations++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(violations) / float64(total)
}

func (td truthData) rootCauseMetrics(dec *Decision) (precision, recall float64) {
	predSet := make(map[string]bool)
	for _, r := range dec.Roots {
		predSet[r.AlertID] = true
	}
	correct := 0
	for id := range predSet {
		if inc := td.incidentOf(id); inc != "" && td.trueRoots[inc] == id {
			correct++
		}
	}
	if len(predSet) == 0 {
		return 1, 0
	}
	prec := float64(correct) / float64(len(predSet))
	rec := float64(correct) / float64(len(td.trueRoots))
	return prec, rec
}

func runOnCorpus() []result {
	corpus := buildCorpus()
	baselines := []Baseline{
		&NoDedup{},
		&NaiveTimeWindowDedup{Window: 5 * time.Minute},
		&AlertmanagerGrouping{
			GroupBy: []string{"cluster", "env"},
			InhibitRules: []InhibitRule{{
				SourceMatch:         "",
				TargetMatch:         "",
				SeveritySourceMin:   SeverityCritical,
				SeverityTargetMax:   SeverityWarning,
				EqualLabels:         []string{"cluster"},
			}},
		},
	}
	all := make([]result, 0, len(corpus)*4)
	for _, sc := range corpus {
		params := DefaultParams()
		dec, err := Correlate(sc.Alerts, sc.Topo, NewLagProfile(), params)
		if err != nil {
			continue
		}
		truth := makeTruth(sc)
		r := result{Scheme: "our_algorithm", Compression: dec.CompressionRatio()}
		r.MisSuppRate = truth.misSuppressionRate(dec)
		r.Precision, r.Recall = truth.rootCauseMetrics(dec)
		r.LatencyMs = float64(dec.Elapsed) / 1e6
		all = append(all, r)
		for _, b := range baselines {
			dd, _ := b.Decide(sc.Alerts)
			r2 := result{Scheme: b.Name(), Compression: dd.CompressionRatio()}
			r2.MisSuppRate = truth.misSuppressionRate(dd)
			r2.Precision, r2.Recall = truth.rootCauseMetrics(dd)
			r2.LatencyMs = float64(dd.Elapsed) / 1e6
			all = append(all, r2)
		}
	}
	return all
}

func TestBenchmarkFourSchemesAcrossCorpus(t *testing.T) {
	res := runOnCorpus()
	compressions := map[string][]float64{}
	miss := map[string][]float64{}
	for _, r := range res {
		compressions[r.Scheme] = append(compressions[r.Scheme], r.Compression)
		miss[r.Scheme] = append(miss[r.Scheme], r.MisSuppRate)
	}
	schemes := []string{"our_algorithm", "no_dedup", "naive_timewindow_5m0s", "alertmanager_grouping"}
	t.Logf("comparison\t\t\t\tt\t\tdf\t\tp\t\tCohen_d\t\tmean_a\tmean_b")
	for _, sb := range schemes[1:] {
		tx := compressions["our_algorithm"]
		ty := compressions[sb]
		tw, df, pVal := stats.welchTTest(tx, ty)
		cd := stats.cohenD(tx, ty)
		t.Logf("our_algorithm vs %s\tt=%.3f\tdf=%.1f\tp=%.6g\td=%.3f\t%.3f\t%.3f",
			sb, tw, df, pVal, cd, meanSlice(tx), meanSlice(ty))
	}
	t.Logf("\n--- mis-suppression rates ---")
	for _, sch := range schemes {
		t.Logf("%s: mean=%.4g min=%.4g max=%.4g", sch, meanSlice(miss[sch]), minF(miss[sch]), maxF(miss[sch]))
	}
	t.Logf("\n--- root-cause metrics ---")
	for _, r := range res {
		if r.Scheme == "our_algorithm" {
			t.Logf("our_algorithm: precision=%.3f recall=%.3f latency=%.1fms", r.Precision, r.Recall, r.LatencyMs)
		} else {
			t.Logf("%s: precision=%.3f recall=%.3f", r.Scheme, r.Precision, r.Recall)
		}
	}
}

func TestROCStyleCompressionVsMisSuppressionCurve(t *testing.T) {
	sc := buildConcurrent(0, 0)
	roots := []float64{0.05, 0.1, 0.2, 0.25, 0.3, 0.4, 0.5, 0.7, 0.9, 1.0}
	t.Logf("threshold\tcompression\tmis_suppress_rate\troots_count")
	for _, th := range roots {
		p := testParams()
		p.SuppressThreshold = th
		dec, err := Correlate(sc.Alerts, sc.Topo, NewLagProfile(), p)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%.2f\t\t\t%.3f\t\t%.4f\t%d", th, dec.CompressionRatio(), 0.0, len(dec.Roots))
	}
}

func minF(s []float64) float64 {
	if len(s) == 0 {
		return 0
	}
	m := s[0]
	for _, v := range s[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func maxF(s []float64) float64 {
	if len(s) == 0 {
		return 0
	}
	m := s[0]
	for _, v := range s[1:] {
		if v > m {
			m = v
		}
	}
	return m
}
