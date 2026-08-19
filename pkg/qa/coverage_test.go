package qa

import (
	"strings"
	"testing"
)

// coverage_test.go exercises coverage parser and gate with table-driven tests.

func TestCoverageParse(t *testing.T) {
	tests := []struct {
		input string
		want  CoverageReport
		ok    bool
	}{
		{
			input: "github.com/acme/foo.go:10: Parse 85.7%\ngithub.com/acme/foo.go:20: Marshal 90.0%\ntotal:\t(statements)\t88.0%\n",
			ok: true, want: CoverageReport{
				Funcs: []FuncCoverage{{Package: "github.com/acme", File: "github.com/acme/foo.go", Function: "Parse", Percent: 85.7}, {Package: "github.com/acme", File: "github.com/acme/foo.go", Function: "Marshal", Percent: 90.0}},
				Packages: map[string]float64{"github.com/acme": 87.85}, Total: 88.0,
			},
		},
	}
	for _, tt := range tests {
		report, err := ParseFuncCoverage(strings.NewReader(tt.input))
		if (err == nil) != tt.ok { t.Fatalf("Parse(%v): ok=%v err=%v", tt.input, tt.ok, err); continue }
		if !tt.ok { break }
		if len(report.Funcs) != len(tt.want.Funcs) { t.Fatalf("funcs len: got %d want %d", len(report.Funcs), len(tt.want.Funcs)); continue }
		for i := range report.Funcs { if report.Funcs[i].Percent != tt.want.Funcs[i].Percent { t.Errorf("pct mismatch at %d: got %.1f want %.1f", i, report.Funcs[i].Percent, tt.want.Funcs[i].Percent) } }
		if report.Total != tt.want.Total { t.Errorf("total: got %.1f want %.1f", report.Total, tt.want.Total) }
	}
}

func TestGateCoverage(t *testing.T) {
	report := &CoverageReport{
		Funcs:      []FuncCoverage{{Function: "Foo", Percent: 50.0}, {Function: "Bar", Percent: 80.0}},
		Packages:   map[string]float64{"github.com/acme": 70.0},
		Total:      65.0,
	}
	tests := []struct {
		cfg       CoverageThreshold
		wantPass  bool
		failCount int
	}{
		{cfg: CoverageThreshold{MinTotal: 100.0, MinFunc: 100.0, MinPackage: map[string]float64{"github.com/acme": 100.0}}, wantPass: false, failCount: 4},
		{cfg: CoverageThreshold{MinTotal: 50.0}, wantPass: true, failCount: 0},
	}
	for _, tt := range tests {
		r := Gate(report, tt.cfg)
		if r.Pass != tt.wantPass { t.Errorf("pass: got %v want %v", r.Pass, tt.wantPass) }
		if len(r.Failures) != tt.failCount { t.Errorf("fail count: got %d want %d", len(r.Failures), tt.failCount) }
	}
}
