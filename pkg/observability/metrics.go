package observability

// metrics.go implements Module 46 (unified metric collection).
//
// Model: a Sample is a Prometheus-shaped observation — metric name, label set,
// float value, optional timestamp. Sources implement Collector; a MultiCollector
// fans out to many sources and merges the results.
//
// Exposition format support is bidirectional. WritePrometheus emits the
// text-based exposition format (# HELP / # TYPE / name{labels} value timestamp)
// and ParsePrometheus reads it back, so this package interoperates with any
// Prometheus-compatible scraper or exporter without a third-party dependency.
//
// Percentiles: p95/p99 are computed *exactly* by sorting the group's values and
// interpolating between the two nearest ranks (the same method as NumPy's
// "linear" interpolation and Prometheus' quantile over a full sample set).
// Approximation error is therefore zero. The tradeoff is O(n log n) time and
// O(n) memory per group, versus a t-digest's O(1) memory and ~1% relative error.
// Exact was chosen because aggregation here runs over a single scrape window
// (thousands of samples, not billions), where the memory cost is irrelevant and
// an honest exact number is worth more than a bounded-error estimate.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MetricType mirrors the Prometheus metric type declared in a # TYPE line.
type MetricType string

// Prometheus metric types.
const (
	MetricCounter   MetricType = "counter"
	MetricGauge     MetricType = "gauge"
	MetricHistogram MetricType = "histogram"
	MetricSummary   MetricType = "summary"
	MetricUntyped   MetricType = "untyped"
)

// Sample is a single metric observation.
type Sample struct {
	Name      string            `json:"name"`
	Labels    map[string]string `json:"labels,omitempty"`
	Value     float64           `json:"value"`
	Timestamp time.Time         `json:"timestamp,omitempty"`
	Type      MetricType        `json:"type,omitempty"`
	Help      string            `json:"help,omitempty"`
}

// ============================================================================
// Collector abstraction
// ============================================================================

// Collector is a source of metric samples.
//
// Implementations must honour ctx cancellation and must be safe for concurrent
// use if registered with a MultiCollector.
type Collector interface {
	// Name identifies the collector in logs and in CollectError.
	Name() string
	// Collect returns the current samples from this source.
	Collect(ctx context.Context) ([]Sample, error)
}

// CollectorFunc adapts a plain function to the Collector interface.
type CollectorFunc struct {
	CollectorName string
	Fn            func(ctx context.Context) ([]Sample, error)
}

// Name implements Collector.
func (c CollectorFunc) Name() string { return c.CollectorName }

// Collect implements Collector.
func (c CollectorFunc) Collect(ctx context.Context) ([]Sample, error) {
	if c.Fn == nil {
		return nil, nil
	}
	return c.Fn(ctx)
}

// CollectError reports a failure from a single named collector. MultiCollector
// joins these so one broken source never hides the others.
type CollectError struct {
	Collector string
	Err       error
}

func (e *CollectError) Error() string {
	return fmt.Sprintf("collector %q: %v", e.Collector, e.Err)
}

// Unwrap supports errors.Is / errors.As on the underlying cause.
func (e *CollectError) Unwrap() error { return e.Err }

// MultiCollector scrapes several collectors concurrently and merges their
// samples. Partial failures are returned alongside the samples that did arrive:
// a non-nil error does not mean the sample slice is empty.
type MultiCollector struct {
	mu         sync.RWMutex
	collectors []Collector
	// CommonLabels are merged into every sample that does not already define
	// the key, e.g. cluster or region identity.
	CommonLabels map[string]string
}

// NewMultiCollector returns a MultiCollector stamping commonLabels onto every
// collected sample.
func NewMultiCollector(commonLabels map[string]string) *MultiCollector {
	return &MultiCollector{CommonLabels: commonLabels}
}

// Register adds a collector. It is safe to call concurrently with Collect.
func (m *MultiCollector) Register(c ...Collector) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.collectors = append(m.collectors, c...)
}

// Len returns the number of registered collectors.
func (m *MultiCollector) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.collectors)
}

// Name implements Collector so MultiCollectors can nest.
func (m *MultiCollector) Name() string { return "multi" }

// Collect scrapes every registered collector in parallel and returns the merged
// samples plus a joined error describing any source that failed.
func (m *MultiCollector) Collect(ctx context.Context) ([]Sample, error) {
	m.mu.RLock()
	sources := make([]Collector, len(m.collectors))
	copy(sources, m.collectors)
	common := m.CommonLabels
	m.mu.RUnlock()

	if len(sources) == 0 {
		return nil, nil
	}

	type result struct {
		samples []Sample
		err     error
	}
	results := make([]result, len(sources))

	var wg sync.WaitGroup
	for i, src := range sources {
		wg.Add(1)
		go func(idx int, c Collector) {
			defer wg.Done()
			if err := ctx.Err(); err != nil {
				results[idx] = result{err: &CollectError{Collector: c.Name(), Err: err}}
				return
			}
			s, err := c.Collect(ctx)
			if err != nil {
				err = &CollectError{Collector: c.Name(), Err: err}
			}
			results[idx] = result{samples: s, err: err}
		}(i, src)
	}
	wg.Wait()

	var merged []Sample
	var errs []error
	for _, r := range results {
		merged = append(merged, r.samples...)
		if r.err != nil {
			errs = append(errs, r.err)
		}
	}

	// Stamp common labels without clobbering source-provided values.
	if len(common) > 0 {
		for i := range merged {
			if merged[i].Labels == nil {
				merged[i].Labels = make(map[string]string, len(common))
			}
			for k, v := range common {
				if _, exists := merged[i].Labels[k]; !exists {
					merged[i].Labels[k] = v
				}
			}
		}
	}

	if len(errs) > 0 {
		return merged, errors.Join(errs...)
	}
	return merged, nil
}

// ============================================================================
// Prometheus exposition format — export
// ============================================================================

// WritePrometheus writes samples in the Prometheus text exposition format.
// Samples are grouped by metric name; each group emits one # HELP and one
// # TYPE line followed by its series, with names and labels sorted so output is
// byte-stable for a given input.
func WritePrometheus(w io.Writer, samples []Sample) error {
	bw := bufio.NewWriter(w)

	byName := make(map[string][]Sample)
	names := make([]string, 0)
	for _, s := range samples {
		if _, seen := byName[s.Name]; !seen {
			names = append(names, s.Name)
		}
		byName[s.Name] = append(byName[s.Name], s)
	}
	sort.Strings(names)

	for _, name := range names {
		group := byName[name]

		// Help and type come from the first sample that declares them.
		help, mtype := "", MetricUntyped
		for _, s := range group {
			if help == "" && s.Help != "" {
				help = s.Help
			}
			if mtype == MetricUntyped && s.Type != "" {
				mtype = s.Type
			}
		}
		if help != "" {
			if _, err := fmt.Fprintf(bw, "# HELP %s %s\n", name, escapeHelp(help)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintf(bw, "# TYPE %s %s\n", name, mtype); err != nil {
			return err
		}

		for _, s := range group {
			if _, err := bw.WriteString(name); err != nil {
				return err
			}
			if len(s.Labels) > 0 {
				if _, err := bw.WriteString(formatLabels(s.Labels)); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintf(bw, " %s", formatValue(s.Value)); err != nil {
				return err
			}
			if !s.Timestamp.IsZero() {
				if _, err := fmt.Fprintf(bw, " %d", s.Timestamp.UnixMilli()); err != nil {
					return err
				}
			}
			if err := bw.WriteByte('\n'); err != nil {
				return err
			}
		}
	}
	return bw.Flush()
}

// formatLabels renders a label set as {k="v",...} with keys in sorted order.
func formatLabels(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
		b.WriteString(`="`)
		b.WriteString(escapeLabelValue(labels[k]))
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}

// escapeLabelValue applies the escaping the exposition format requires for
// label values: backslash, double quote, and newline.
func escapeLabelValue(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return r.Replace(v)
}

// escapeHelp escapes backslashes and newlines in HELP text.
func escapeHelp(v string) string {
	r := strings.NewReplacer(`\`, `\\`, "\n", `\n`)
	return r.Replace(v)
}

// formatValue renders a float in the exposition format, using the special
// tokens the spec mandates for infinities and NaN.
func formatValue(v float64) string {
	switch {
	case math.IsNaN(v):
		return "NaN"
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	default:
		return strconv.FormatFloat(v, 'g', -1, 64)
	}
}

// ============================================================================
// Prometheus exposition format — parse
// ============================================================================

// ParseError describes a malformed exposition line. Parsing is strict: rather
// than silently skipping bad input, the line number and reason are reported.
type ParseError struct {
	Line   int
	Reason string
	Text   string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("prometheus parse error at line %d: %s (%q)", e.Line, e.Reason, e.Text)
}

// ParsePrometheus reads the Prometheus text exposition format and returns the
// samples it contains, carrying across the HELP and TYPE metadata for each
// metric family.
func ParsePrometheus(r io.Reader) ([]Sample, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	helps := make(map[string]string)
	types := make(map[string]MetricType)
	var out []Sample

	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "#") {
			name, kind, rest, ok := parseComment(line)
			if !ok {
				continue // free-form comment, not metadata
			}
			switch kind {
			case "HELP":
				helps[name] = rest
			case "TYPE":
				types[name] = MetricType(rest)
			}
			continue
		}

		s, err := parseSampleLine(line, lineNo)
		if err != nil {
			return out, err
		}
		s.Help = helps[s.Name]
		if t, ok := types[s.Name]; ok {
			s.Type = t
		} else {
			s.Type = MetricUntyped
		}
		out = append(out, s)
	}
	if err := sc.Err(); err != nil {
		return out, fmt.Errorf("prometheus parse: read: %w", err)
	}
	return out, nil
}

// parseComment recognises "# HELP name text" and "# TYPE name type" lines.
func parseComment(line string) (name, kind, rest string, ok bool) {
	body := strings.TrimSpace(strings.TrimPrefix(line, "#"))
	kw, remainder, found := strings.Cut(body, " ")
	if !found {
		return "", "", "", false
	}
	kw = strings.ToUpper(kw)
	if kw != "HELP" && kw != "TYPE" {
		return "", "", "", false
	}
	remainder = strings.TrimSpace(remainder)
	name, rest, _ = strings.Cut(remainder, " ")
	return name, kw, strings.TrimSpace(rest), name != ""
}

// parseSampleLine parses `name{labels} value [timestamp]`.
func parseSampleLine(line string, lineNo int) (Sample, error) {
	var s Sample

	// Split the metric name (and optional label block) from the value section.
	var head, tail string
	if brace := strings.IndexByte(line, '{'); brace >= 0 {
		end := strings.IndexByte(line[brace:], '}')
		if end < 0 {
			return s, &ParseError{Line: lineNo, Reason: "unterminated label block", Text: line}
		}
		end += brace
		head = line[:brace]
		labels, err := parseLabels(line[brace+1:end], lineNo, line)
		if err != nil {
			return s, err
		}
		s.Labels = labels
		tail = strings.TrimSpace(line[end+1:])
	} else {
		var found bool
		head, tail, found = strings.Cut(line, " ")
		if !found {
			return s, &ParseError{Line: lineNo, Reason: "missing value", Text: line}
		}
		tail = strings.TrimSpace(tail)
	}

	s.Name = strings.TrimSpace(head)
	if s.Name == "" {
		return s, &ParseError{Line: lineNo, Reason: "empty metric name", Text: line}
	}

	fields := strings.Fields(tail)
	if len(fields) == 0 {
		return s, &ParseError{Line: lineNo, Reason: "missing value", Text: line}
	}
	if len(fields) > 2 {
		return s, &ParseError{Line: lineNo, Reason: "too many fields after labels", Text: line}
	}

	v, err := parseExpositionValue(fields[0])
	if err != nil {
		return s, &ParseError{Line: lineNo, Reason: "invalid value: " + err.Error(), Text: line}
	}
	s.Value = v

	if len(fields) == 2 {
		ms, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return s, &ParseError{Line: lineNo, Reason: "invalid timestamp: " + err.Error(), Text: line}
		}
		s.Timestamp = time.UnixMilli(ms).UTC()
	}
	return s, nil
}

// parseExpositionValue handles the NaN/+Inf/-Inf tokens plus ordinary floats.
func parseExpositionValue(tok string) (float64, error) {
	switch tok {
	case "NaN":
		return math.NaN(), nil
	case "+Inf", "Inf":
		return math.Inf(1), nil
	case "-Inf":
		return math.Inf(-1), nil
	}
	return strconv.ParseFloat(tok, 64)
}

// parseLabels parses the inside of a label block: k="v",k2="v2".
func parseLabels(inner string, lineNo int, full string) (map[string]string, error) {
	inner = strings.TrimSpace(inner)
	if inner == "" {
		return nil, nil
	}
	labels := make(map[string]string)

	i := 0
	for i < len(inner) {
		// key
		eq := strings.IndexByte(inner[i:], '=')
		if eq < 0 {
			return nil, &ParseError{Line: lineNo, Reason: "label missing '='", Text: full}
		}
		key := strings.TrimSpace(inner[i : i+eq])
		if key == "" {
			return nil, &ParseError{Line: lineNo, Reason: "empty label name", Text: full}
		}
		i += eq + 1

		// opening quote
		for i < len(inner) && inner[i] == ' ' {
			i++
		}
		if i >= len(inner) || inner[i] != '"' {
			return nil, &ParseError{Line: lineNo, Reason: "label value must be quoted", Text: full}
		}
		i++

		// value, honouring backslash escapes
		var b strings.Builder
		closed := false
		for i < len(inner) {
			c := inner[i]
			if c == '\\' && i+1 < len(inner) {
				i++
				switch inner[i] {
				case 'n':
					b.WriteByte('\n')
				case '"':
					b.WriteByte('"')
				case '\\':
					b.WriteByte('\\')
				default:
					b.WriteByte(inner[i])
				}
				i++
				continue
			}
			if c == '"' {
				closed = true
				i++
				break
			}
			b.WriteByte(c)
			i++
		}
		if !closed {
			return nil, &ParseError{Line: lineNo, Reason: "unterminated label value", Text: full}
		}
		labels[key] = b.String()

		// separator
		for i < len(inner) && (inner[i] == ' ' || inner[i] == ',') {
			i++
		}
	}
	return labels, nil
}

// ============================================================================
// Aggregation pipeline
// ============================================================================

// AggFunc names a supported aggregation.
type AggFunc string

// Supported aggregations.
const (
	AggSum   AggFunc = "sum"
	AggAvg   AggFunc = "avg"
	AggMin   AggFunc = "min"
	AggMax   AggFunc = "max"
	AggCount AggFunc = "count"
	AggP95   AggFunc = "p95"
	AggP99   AggFunc = "p99"
)

// AggregatedGroup is the result for one label-value combination.
type AggregatedGroup struct {
	// Name is the metric name the group was built from.
	Name string `json:"name"`
	// GroupLabels holds only the labels the aggregation grouped by.
	GroupLabels map[string]string `json:"group_labels"`
	// Count is the number of samples in the group.
	Count int `json:"count"`
	// Results maps each requested aggregation to its value.
	Results map[AggFunc]float64 `json:"results"`
}

// PercentileMethod documents how p95/p99 were derived, so consumers never have
// to guess whether a number is exact or estimated.
const PercentileMethod = "exact-sorted-linear-interpolation; approximation error = 0"

// Aggregate groups samples by metric name plus the given label keys and applies
// the requested aggregations to each group.
//
// Samples missing a groupBy label are bucketed under the empty string for that
// key. Output is sorted by name then by group-label values, so results are
// deterministic. Passing no funcs defaults to count+sum+avg.
func Aggregate(samples []Sample, groupBy []string, funcs ...AggFunc) []AggregatedGroup {
	if len(samples) == 0 {
		return nil
	}
	if len(funcs) == 0 {
		funcs = []AggFunc{AggCount, AggSum, AggAvg}
	}

	type bucket struct {
		name   string
		labels map[string]string
		values []float64
	}
	buckets := make(map[string]*bucket)
	order := make([]string, 0)

	for _, s := range samples {
		// Build a collision-free key from the name and the selected labels.
		var kb strings.Builder
		kb.WriteString(strconv.Itoa(len(s.Name)))
		kb.WriteByte(':')
		kb.WriteString(s.Name)
		gl := make(map[string]string, len(groupBy))
		for _, k := range groupBy {
			v := s.Labels[k]
			gl[k] = v
			kb.WriteByte('|')
			kb.WriteString(strconv.Itoa(len(k)))
			kb.WriteByte(':')
			kb.WriteString(k)
			kb.WriteByte('=')
			kb.WriteString(strconv.Itoa(len(v)))
			kb.WriteByte(':')
			kb.WriteString(v)
		}
		key := kb.String()

		b, ok := buckets[key]
		if !ok {
			b = &bucket{name: s.Name, labels: gl}
			buckets[key] = b
			order = append(order, key)
		}
		b.values = append(b.values, s.Value)
	}

	out := make([]AggregatedGroup, 0, len(buckets))
	for _, key := range order {
		b := buckets[key]
		out = append(out, AggregatedGroup{
			Name:        b.name,
			GroupLabels: b.labels,
			Count:       len(b.values),
			Results:     computeAggregations(b.values, funcs),
		})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		for _, k := range groupBy {
			if out[i].GroupLabels[k] != out[j].GroupLabels[k] {
				return out[i].GroupLabels[k] < out[j].GroupLabels[k]
			}
		}
		return false
	})
	return out
}

// computeAggregations evaluates the requested functions over values. NaN inputs
// are excluded from every statistic; a group of only NaNs yields count 0.
func computeAggregations(values []float64, funcs []AggFunc) map[AggFunc]float64 {
	clean := make([]float64, 0, len(values))
	for _, v := range values {
		if !math.IsNaN(v) {
			clean = append(clean, v)
		}
	}

	res := make(map[AggFunc]float64, len(funcs))
	if len(clean) == 0 {
		for _, f := range funcs {
			res[f] = 0
		}
		return res
	}

	var sum float64
	minV, maxV := math.Inf(1), math.Inf(-1)
	for _, v := range clean {
		sum += v
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}

	var sorted []float64
	needSorted := false
	for _, f := range funcs {
		if f == AggP95 || f == AggP99 {
			needSorted = true
			break
		}
	}
	if needSorted {
		sorted = make([]float64, len(clean))
		copy(sorted, clean)
		sort.Float64s(sorted)
	}

	for _, f := range funcs {
		switch f {
		case AggSum:
			res[f] = sum
		case AggAvg:
			res[f] = sum / float64(len(clean))
		case AggMin:
			res[f] = minV
		case AggMax:
			res[f] = maxV
		case AggCount:
			res[f] = float64(len(clean))
		case AggP95:
			res[f] = Quantile(sorted, 0.95)
		case AggP99:
			res[f] = Quantile(sorted, 0.99)
		default:
			res[f] = math.NaN()
		}
	}
	return res
}

// Quantile returns the q-quantile of an already-sorted slice using linear
// interpolation between adjacent ranks. q is clamped to [0,1]. An empty slice
// returns 0.
//
// This is exact for the supplied sample set — there is no approximation error.
func Quantile(sorted []float64, q float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	if q <= 0 {
		return sorted[0]
	}
	if q >= 1 {
		return sorted[n-1]
	}
	pos := q * float64(n-1)
	lo := int(math.Floor(pos))
	hi := lo + 1
	if hi >= n {
		return sorted[n-1]
	}
	frac := pos - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

// ToSamples flattens aggregated groups back into Samples so results can be
// re-exported through WritePrometheus. Each aggregation becomes its own metric
// named "<metric>:<func>", carrying the group labels plus a method label that
// records how percentiles were computed.
func ToSamples(groups []AggregatedGroup, ts time.Time) []Sample {
	var out []Sample
	for _, g := range groups {
		funcs := make([]AggFunc, 0, len(g.Results))
		for f := range g.Results {
			funcs = append(funcs, f)
		}
		sort.Slice(funcs, func(i, j int) bool { return funcs[i] < funcs[j] })

		for _, f := range funcs {
			labels := make(map[string]string, len(g.GroupLabels)+1)
			for k, v := range g.GroupLabels {
				labels[k] = v
			}
			if f == AggP95 || f == AggP99 {
				labels["quantile_method"] = "exact"
			}
			out = append(out, Sample{
				Name:      g.Name + ":" + string(f),
				Labels:    labels,
				Value:     g.Results[f],
				Timestamp: ts,
				Type:      MetricGauge,
			})
		}
	}
	return out
}

// ============================================================================
// Scrape timing
// ============================================================================

// ScrapeResult pairs collected samples with the wall-clock cost of collecting
// them, so scrape latency is measured rather than assumed.
type ScrapeResult struct {
	Samples  []Sample      `json:"samples"`
	Duration time.Duration `json:"duration"`
	Err      error         `json:"-"`
}

// Scrape runs a collector and records how long it took.
func Scrape(ctx context.Context, c Collector) ScrapeResult {
	start := time.Now()
	samples, err := c.Collect(ctx)
	return ScrapeResult{Samples: samples, Duration: time.Since(start), Err: err}
}
