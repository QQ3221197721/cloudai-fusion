// Package qa implements the CloudAI Fusion QA Gateway: a single, self-contained
// engineering quality-control gate that turns scattered CI heuristics into four
// deterministic, offline-runnable checks.
//
// The gateway has four independent analyzers, each a pure function over local
// artifacts (no network, no external services), so it runs identically on a
// developer laptop and in CI:
//
//	CoverageAnalyzer   parses `go tool cover -func` output into per-func and
//	                   per-package coverage, then gates against thresholds.
//	Regressor          diffs a current benchmark run against a stored baseline
//	                   and flags entries whose ns/op regressed beyond a budget.
//	LintEngine         loads a YAML rule set (forbidden imports, functions that
//	                   must stay allocation-free) and enforces it with a go/ast
//	                   static pass.
//	BenchDB            persists benchmark runs to a JSON file and returns the
//	                   most recent runs for regression comparison.
//
// Positioning is honest: this is a general-purpose engineering QA Gateway (T3),
// not a novel algorithm. It exists to make quality gates reproducible and
// vendor-independent, comparable in spirit to SonarQube quality gates, CircleCI
// test insights, and Datadog CI Visibility - but fully local and Go-native.
//
// Every analyzer is deterministic: given the same inputs it returns the same
// result, and time-ordered structures use explicit/monotonic timestamps rather
// than wall-clock proximity so results are stable even under the ~15ms Windows
// clock granularity.
package qa
