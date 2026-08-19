package qa

import (
	"bufio"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
)

// coverage.go is the Coverage Analyzer. It parses the textual report emitted by
// `go tool cover -func=<profile>` and gates it against thresholds. The report is
// tab-aligned and looks like:
//
//	github.com/acme/foo/bar.go:12:	Parse	85.7%
//	github.com/acme/foo/baz.go:20:	Total	100.0%
//	total:					(statements)	90.0%
//
// Each non-total line is one function; the trailing `total:` line is the overall
// statement coverage. We keep per-func entries, derive a per-package aggregate
// (statement-unweighted mean of its funcs), and expose a threshold gate.

// FuncCoverage is the coverage of a single function.
type FuncCoverage struct {
	// Package is the import-path directory the function's file lives in,
	// e.g. "github.com/acme/foo".
	Package string
	// File is the full import-path-qualified file name.
	File string
	// Function is the function (or method) name as reported by cover.
	Function string
	// Percent is statement coverage in the range [0,100].
	Percent float64
}

// CoverageReport is a parsed `go tool cover -func` report.
type CoverageReport struct {
	// Funcs holds every non-total function line, in file order.
	Funcs []FuncCoverage
	// Packages maps import-path directory to its mean function coverage.
	Packages map[string]float64
	// Total is the overall statement coverage from the `total:` line. It is
	// -1 when the report contained no total line.
	Total float64
}

// CoverageThreshold configures the coverage gate. Zero-value fields are treated
// as "no requirement" for MinTotal/MinFunc and an empty map for MinPackage.
type CoverageThreshold struct {
	// MinTotal is the minimum acceptable overall coverage percent.
	MinTotal float64
	// MinFunc is the minimum acceptable per-function coverage percent.
	MinFunc float64
	// MinPackage maps an import-path directory to its minimum mean coverage.
	MinPackage map[string]float64
}

// CoverageFailure describes one threshold breach.
type CoverageFailure struct {
	// Scope is "total", "package" or "func".
	Scope string
	// Name is the package/func the failure refers to ("" for total).
	Name string
	// Got is the measured percent.
	Got float64
	// Want is the required minimum percent.
	Want float64
}

// String renders a failure for logs and reports.
func (f CoverageFailure) String() string {
	if f.Name == "" {
		return fmt.Sprintf("%s coverage %.1f%% < required %.1f%%", f.Scope, f.Got, f.Want)
	}
	return fmt.Sprintf("%s %q coverage %.1f%% < required %.1f%%", f.Scope, f.Name, f.Got, f.Want)
}

// CoverageResult is the outcome of gating a report against a threshold.
type CoverageResult struct {
	// Pass is true when no failures were found.
	Pass bool
	// Failures holds every breach, sorted by scope then name for determinism.
	Failures []CoverageFailure
}

// ParseFuncCoverage parses the output of `go tool cover -func` from r.
//
// It is tolerant of the tab-vs-space alignment cover uses (fields are split on
// runs of whitespace) and skips blank lines. A malformed percent field yields an
// error identifying the offending line so callers never gate on partial data.
func ParseFuncCoverage(r io.Reader) (*CoverageReport, error) {
	report := &CoverageReport{
		Packages: map[string]float64{},
		Total:    -1,
	}
	// pkgSum/pkgCount accumulate per-package means without retaining order.
	pkgSum := map[string]float64{}
	pkgCount := map[string]int{}

	sc := bufio.NewScanner(r)
	// Cover reports are small but a single line can be long; grow the buffer.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for sc.Scan() {
		line++
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			continue
		}
		fields := strings.Fields(raw)
		if len(fields) < 2 {
			return nil, fmt.Errorf("qa: coverage line %d malformed: %q", line, raw)
		}
		pctStr := strings.TrimSuffix(fields[len(fields)-1], "%")
		pct, err := strconv.ParseFloat(pctStr, 64)
		if err != nil {
			return nil, fmt.Errorf("qa: coverage line %d: bad percent %q: %w", line, fields[len(fields)-1], err)
		}
		if fields[0] == "total:" {
			report.Total = pct
			continue
		}
		// fields[0] is "<import-path>/<file>.go:<lineno>:"; the function name
		// is the second-to-last field (the last is the percent).
		if len(fields) < 3 {
			return nil, fmt.Errorf("qa: coverage line %d malformed: %q", line, raw)
		}
		file := trimLocation(fields[0])
		fc := FuncCoverage{
			Package:  path.Dir(file),
			File:     file,
			Function: fields[len(fields)-2],
			Percent:  pct,
		}
		report.Funcs = append(report.Funcs, fc)
		pkgSum[fc.Package] += pct
		pkgCount[fc.Package]++
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("qa: reading coverage: %w", err)
	}
	for pkg, sum := range pkgSum {
		report.Packages[pkg] = sum / float64(pkgCount[pkg])
	}
	return report, nil
}

// trimLocation turns "pkg/path/file.go:12:" into "pkg/path/file.go".
func trimLocation(loc string) string {
	if i := strings.IndexByte(loc, ':'); i >= 0 {
		return loc[:i]
	}
	return loc
}

// Gate evaluates report against threshold and returns a deterministic result.
// Failures are ordered by scope (total, then package, then func) and name so the
// same inputs always produce byte-identical output.
func Gate(report *CoverageReport, threshold CoverageThreshold) CoverageResult {
	var failures []CoverageFailure

	if threshold.MinTotal > 0 && report.Total >= 0 && report.Total < threshold.MinTotal {
		failures = append(failures, CoverageFailure{
			Scope: "total", Got: report.Total, Want: threshold.MinTotal,
		})
	}
	for pkg, want := range threshold.MinPackage {
		got, ok := report.Packages[pkg]
		if !ok {
			// Missing package is a failure: the gate cannot vouch for it.
			failures = append(failures, CoverageFailure{
				Scope: "package", Name: pkg, Got: -1, Want: want,
			})
			continue
		}
		if got < want {
			failures = append(failures, CoverageFailure{
				Scope: "package", Name: pkg, Got: got, Want: want,
			})
		}
	}
	if threshold.MinFunc > 0 {
		for _, fc := range report.Funcs {
			if fc.Percent < threshold.MinFunc {
				failures = append(failures, CoverageFailure{
					Scope: "func", Name: fc.Function, Got: fc.Percent, Want: threshold.MinFunc,
				})
			}
		}
	}

	sortFailures(failures)
	return CoverageResult{Pass: len(failures) == 0, Failures: failures}
}

// scopeRank orders scopes deterministically: total < package < func.
func scopeRank(scope string) int {
	switch scope {
	case "total":
		return 0
	case "package":
		return 1
	case "func":
		return 2
	default:
		return 3
	}
}

func sortFailures(failures []CoverageFailure) {
	sort.SliceStable(failures, func(i, j int) bool {
		ri, rj := scopeRank(failures[i].Scope), scopeRank(failures[j].Scope)
		if ri != rj {
			return ri < rj
		}
		return failures[i].Name < failures[j].Name
	})
}
