package alerting

// module48_alertmanager_compare_test.go is the T2 head-to-head harness for
// Module 48 (Intelligent Alerting) against Prometheus Alertmanager's
// grouping/dedup path.
//
// ---------------------------------------------------------------------------
// WHAT IS THE COMPETITOR, EXACTLY
// ---------------------------------------------------------------------------
//
// The competitor is the REAL upstream module github.com/prometheus/alertmanager
// (v0.34.0), imported as a normal dependency. The following upstream code runs
// verbatim inside the benchmark loop:
//
//   - dispatch.NewRoute(*config.Route, parent) — builds the real route tree,
//     including the real labels.Matchers compilation and the real RouteOpts
//     defaulting/inheritance logic.
//   - (*dispatch.Route).Match(model.LabelSet) — the real depth-first route
//     matching Alertmanager runs for every ingested alert
//     (see dispatch.Dispatcher.routeAlert).
//   - model.LabelSet.Fingerprint() — the real prometheus/common fingerprint
//     Alertmanager uses as the aggregation-group key
//     (see dispatch.aggrGroup.fingerprint / Dispatcher.groupAlert).
//
// ONE upstream function is reimplemented here, and this is stated plainly:
// dispatch.getGroupLabels is package-private (lowercase) in upstream, so it
// cannot be called from outside. amGroupLabels below is a line-for-line
// transcription of upstream dispatch.go:596-609 (GroupBy / GroupByAll label
// projection). Nothing else about Alertmanager's grouping is reimplemented.
//
// DELIBERATELY EXCLUDED from the measured work unit, for BOTH sides:
// Alertmanager's aggrGroup flush loop (group_wait / group_interval timers,
// notify pipeline, store, marker, tracing spans, otel). Those are asynchronous
// batching + delivery, not grouping decisions, and our engine has no
// counterpart. Including them would measure timers, not algorithms. The
// measured unit is therefore exactly: "given N alerts with labels, assign each
// to a group", which is what both systems do synchronously per alert.
//
// ---------------------------------------------------------------------------
// FAIRNESS RULES ENFORCED HERE
// ---------------------------------------------------------------------------
//
//  1. Identical input information. Every alert's label map is byte-identical
//     for both sides. Our engine additionally reads EvidenceAlert.Source, so
//     the same value is ALSO published as a "source" label, and one of the
//     evaluated Alertmanager configs groups on it. Neither side sees a signal
//     the other cannot.
//  2. The ground-truth root cause is NEVER a label. It lives in a separate
//     field of the corpus and is only read by the scorer.
//  3. Identical work unit: both benchmarks build a fresh grouper and push all
//     N alerts through it, so group-creation cost is charged to both.
//  4. Input materialisation (map[string]string vs model.LabelSet) happens
//     OUTSIDE the timed region for both sides.
//  5. Alertmanager is evaluated under FIVE separate group_by configs and the
//     best-scoring one is used as the headline competitor. We do not get to
//     pick a strawman config for it.
//
// No production code is modified by this file.

import (
	"fmt"
	"math"
	"sort"
	"testing"
	"time"

	"github.com/prometheus/common/model"

	"github.com/prometheus/alertmanager/config"
	"github.com/prometheus/alertmanager/dispatch"
)

// ---------------------------------------------------------------------------
// Ground-truth incident corpus
// ---------------------------------------------------------------------------

// gtAlert is one corpus entry: the alert both systems see, plus the hidden
// ground-truth root cause used only for scoring.
type gtAlert struct {
	alert     EvidenceAlert
	rootCause string // hidden from both groupers
}

// gtCorpus is a labelled alert-storm corpus.
type gtCorpus struct {
	name   string
	alerts []gtAlert
}

// mkAlert builds a corpus entry. source is written both to EvidenceAlert.Source
// (which our engine reads) and to the "source" label (which Alertmanager can be
// configured to group on), so the two sides see the same information.
func mkAlert(id, rootCause, alertname, cluster, service, instance, source, severity string, ts time.Time) gtAlert {
	return gtAlert{
		rootCause: rootCause,
		alert: EvidenceAlert{
			ID:       id,
			Severity: severity,
			Source:   source,
			Message:  alertname,
			Labels: map[string]string{
				"alertname": alertname,
				"cluster":   cluster,
				"service":   service,
				"instance":  instance,
				"source":    source,
				"severity":  severity,
			},
			Timestamp: ts,
		},
	}
}

// cascadeCorpus is a hand-written, SRE-realistic alert storm with known root
// causes. It encodes the situation label-equality grouping is structurally
// unable to express: a single root cause emitting alerts under DIFFERENT
// alertnames, on DIFFERENT services, from DIFFERENT exporters.
//
// It also contains 24 genuinely independent routine alerts, so an algorithm
// that merges aggressively is punished by pairwise precision rather than
// rewarded.
func cascadeCorpus() gtCorpus {
	t0 := time.Date(2026, 8, 24, 3, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return t0.Add(time.Duration(sec) * time.Second) }

	var a []gtAlert
	n := 0
	next := func() string { n++; return fmt.Sprintf("a%03d", n) }

	// ---- Incident A: db-primary-1 disk exhaustion cascades to the edge. ----
	// 12 alerts, 5 distinct services, 4 distinct exporters, 11 alertnames.
	const A = "rootA-db-primary-1-disk-exhaustion"
	a = append(a,
		mkAlert(next(), A, "NodeFilesystemAlmostOutOfSpace", "prod-us-east", "postgres", "db-primary-1", "node-exporter", "warning", at(0)),
		mkAlert(next(), A, "DiskWillFillIn4Hours", "prod-us-east", "postgres", "db-primary-1", "node-exporter", "warning", at(30)),
		mkAlert(next(), A, "PostgresqlTooManyConnections", "prod-us-east", "postgres", "db-primary-1", "postgres-exporter", "critical", at(95)),
		mkAlert(next(), A, "PostgresqlSlowQueries", "prod-us-east", "postgres", "db-primary-1", "postgres-exporter", "warning", at(110)),
		mkAlert(next(), A, "PostgresqlWALArchiveFailing", "prod-us-east", "postgres", "db-primary-1", "postgres-exporter", "critical", at(125)),
		mkAlert(next(), A, "PostgresqlReplicationLag", "prod-us-east", "postgres", "db-replica-1", "postgres-exporter", "warning", at(140)),
		mkAlert(next(), A, "HTTPErrorRateHigh", "prod-us-east", "api-gateway", "api-gw-1", "blackbox-exporter", "critical", at(180)),
		mkAlert(next(), A, "HTTPErrorRateHigh", "prod-us-east", "api-gateway", "api-gw-2", "blackbox-exporter", "critical", at(185)),
		mkAlert(next(), A, "RequestLatencyP99High", "prod-us-east", "api-gateway", "api-gw-1", "prometheus", "warning", at(195)),
		mkAlert(next(), A, "CheckoutFailureRate", "prod-us-east", "checkout", "checkout-1", "prometheus", "critical", at(210)),
		mkAlert(next(), A, "QueueBacklogGrowing", "prod-us-east", "order-worker", "worker-1", "prometheus", "warning", at(240)),
		mkAlert(next(), A, "SLOBurnRateFast", "prod-us-east", "slo-controller", "slo-1", "prometheus", "critical", at(260)),
	)

	// ---- Incident B: worker-3 kernel panic -> node NotReady -> evictions. ----
	// 9 alerts, 4 services, 3 exporters.
	const B = "rootB-worker-3-kernel-panic"
	a = append(a,
		mkAlert(next(), B, "KubeNodeNotReady", "prod-us-east", "kubelet", "worker-3", "kube-state-metrics", "critical", at(600)),
		mkAlert(next(), B, "KubeNodeUnreachable", "prod-us-east", "kubelet", "worker-3", "kube-state-metrics", "critical", at(605)),
		mkAlert(next(), B, "KubePodNotReady", "prod-us-east", "ml-inference", "infer-7", "kube-state-metrics", "warning", at(640)),
		mkAlert(next(), B, "KubePodCrashLooping", "prod-us-east", "ml-inference", "infer-8", "kube-state-metrics", "warning", at(650)),
		mkAlert(next(), B, "KubeDeploymentReplicasMismatch", "prod-us-east", "ml-inference", "infer-deploy", "kube-state-metrics", "warning", at(660)),
		mkAlert(next(), B, "GPUUtilizationCollapsed", "prod-us-east", "ml-inference", "worker-3", "dcgm-exporter", "warning", at(670)),
		mkAlert(next(), B, "InferenceQueueDepthHigh", "prod-us-east", "ml-gateway", "ml-gw-1", "prometheus", "critical", at(700)),
		mkAlert(next(), B, "InferenceTimeoutRate", "prod-us-east", "ml-gateway", "ml-gw-1", "prometheus", "critical", at(715)),
		mkAlert(next(), B, "SLOBurnRateFast", "prod-us-east", "slo-controller", "slo-2", "prometheus", "warning", at(740)),
	)

	// ---- Incident C: expired ingress TLS certificate in the EU cluster. ----
	// 7 alerts, 4 services, 3 exporters, different cluster.
	const C = "rootC-eu-ingress-tls-expired"
	a = append(a,
		mkAlert(next(), C, "CertificateExpired", "prod-eu-west", "ingress", "ingress-1", "blackbox-exporter", "critical", at(1200)),
		mkAlert(next(), C, "ProbeSSLVerificationFailed", "prod-eu-west", "ingress", "ingress-1", "blackbox-exporter", "critical", at(1210)),
		mkAlert(next(), C, "ProbeFailed", "prod-eu-west", "ingress", "ingress-2", "blackbox-exporter", "critical", at(1215)),
		mkAlert(next(), C, "HTTPErrorRateHigh", "prod-eu-west", "storefront", "front-1", "prometheus", "critical", at(1240)),
		mkAlert(next(), C, "SessionCreationFailing", "prod-eu-west", "auth", "auth-1", "prometheus", "critical", at(1255)),
		mkAlert(next(), C, "PaymentWebhookRejected", "prod-eu-west", "payments", "pay-1", "prometheus", "warning", at(1270)),
		mkAlert(next(), C, "SLOBurnRateFast", "prod-eu-west", "slo-controller", "slo-3", "prometheus", "warning", at(1290)),
	)

	// ---- Independent routine noise: 24 alerts, 24 distinct root causes. ----
	// These exist to punish over-merging. Each is genuinely unrelated.
	noiseSvc := []string{"batch-etl", "ci-runner", "backup", "docs-site", "metrics-store", "log-shipper"}
	noiseSrc := []string{"node-exporter", "prometheus", "kube-state-metrics", "blackbox-exporter"}
	noiseName := []string{"CPUThrottlingHigh", "BackupJobSlow", "CertificateExpiringSoon", "DiskIOSaturation"}
	for i := 0; i < 24; i++ {
		a = append(a, mkAlert(
			next(),
			fmt.Sprintf("noise-%02d", i),
			noiseName[i%len(noiseName)],
			fmt.Sprintf("dev-%d", i%3),
			noiseSvc[i%len(noiseSvc)],
			fmt.Sprintf("host-%02d", i),
			noiseSrc[i%len(noiseSrc)],
			"low",
			at(2000+i*17),
		))
	}

	return gtCorpus{name: "cascade-52", alerts: a}
}

// stormCorpus scales the cascade pattern to stress the asymptotics of both
// algorithms: reps independent copies of the three incidents (each copy has its
// own root causes, cluster and instance namespace) plus proportional noise.
// Group count grows linearly with reps, which is the regime where a linear scan
// diverges from a hash lookup.
func stormCorpus(reps int) gtCorpus {
	base := cascadeCorpus()
	out := make([]gtAlert, 0, len(base.alerts)*reps)
	for r := 0; r < reps; r++ {
		for _, e := range base.alerts {
			c := e
			c.rootCause = fmt.Sprintf("%s#r%d", e.rootCause, r)
			c.alert.ID = fmt.Sprintf("%s-r%d", e.alert.ID, r)
			c.alert.Source = fmt.Sprintf("%s-r%d", e.alert.Source, r)
			lbl := make(map[string]string, len(e.alert.Labels))
			for k, v := range e.alert.Labels {
				lbl[k] = v
			}
			lbl["source"] = c.alert.Source
			lbl["cluster"] = fmt.Sprintf("%s-r%d", e.alert.Labels["cluster"], r)
			lbl["instance"] = fmt.Sprintf("%s-r%d", e.alert.Labels["instance"], r)
			c.alert.Labels = lbl
			out = append(out, c)
		}
	}
	return gtCorpus{name: fmt.Sprintf("storm-%d", len(out)), alerts: out}
}

// ---------------------------------------------------------------------------
// Competitor: real Alertmanager grouping
// ---------------------------------------------------------------------------

// amGrouper drives the real Alertmanager route-matching + aggregation-group
// keying path. It holds the real *dispatch.Route built by dispatch.NewRoute.
type amGrouper struct {
	route  *dispatch.Route
	groups map[model.Fingerprint]string
}

// newAMGrouper builds a real Alertmanager route from a real config.Route. A nil
// parent makes this the root route, exactly as Alertmanager does when loading a
// config file. groupBy mirrors the `group_by:` stanza; passing groupByAll
// mirrors `group_by: ['...']`.
func newAMGrouper(groupBy []string, groupByAll bool) *amGrouper {
	cr := &config.Route{Receiver: "default"}
	if groupByAll {
		cr.GroupByAll = true
	} else {
		cr.GroupBy = make([]model.LabelName, 0, len(groupBy))
		for _, g := range groupBy {
			cr.GroupBy = append(cr.GroupBy, model.LabelName(g))
		}
	}
	return &amGrouper{
		route:  dispatch.NewRoute(cr, nil),
		groups: make(map[model.Fingerprint]string),
	}
}

// amGroupLabels is a transcription of the package-private upstream function
// dispatch.getGroupLabels (alertmanager v0.34.0 dispatch/dispatch.go:596-609).
// It is duplicated here only because Go does not export it; the semantics are
// identical (project the alert's labels down to the route's GroupBy set, or
// keep all labels when GroupByAll is set).
func amGroupLabels(lset model.LabelSet, route *dispatch.Route) model.LabelSet {
	capacity := len(route.RouteOpts.GroupBy)
	if route.RouteOpts.GroupByAll {
		capacity = len(lset)
	}
	groupLabels := make(model.LabelSet, capacity)
	for ln, lv := range lset {
		if _, ok := route.RouteOpts.GroupBy[ln]; ok || route.RouteOpts.GroupByAll {
			groupLabels[ln] = lv
		}
	}
	return groupLabels
}

// Group performs one alert's worth of Alertmanager grouping work: real route
// matching, group-label projection, real prometheus/common fingerprinting, and
// the aggregation-group map lookup/insert. This mirrors
// Dispatcher.routeAlert -> Dispatcher.groupAlert up to (but excluding) the
// asynchronous aggrGroup flush loop.
func (g *amGrouper) Group(lset model.LabelSet) model.Fingerprint {
	var last model.Fingerprint
	for _, r := range g.route.Match(lset) {
		gl := amGroupLabels(lset, r)
		fp := gl.Fingerprint()
		if _, ok := g.groups[fp]; !ok {
			g.groups[fp] = gl.String()
		}
		last = fp
	}
	return last
}

// toLabelSets materialises the corpus as Alertmanager label sets. Called
// outside every timed region.
func toLabelSets(c gtCorpus) []model.LabelSet {
	out := make([]model.LabelSet, 0, len(c.alerts))
	for _, e := range c.alerts {
		ls := make(model.LabelSet, len(e.alert.Labels))
		for k, v := range e.alert.Labels {
			ls[model.LabelName(k)] = model.LabelValue(v)
		}
		out = append(out, ls)
	}
	return out
}

// ---------------------------------------------------------------------------
// Group assignment extraction (for quality scoring)
// ---------------------------------------------------------------------------

// assignOurs runs the corpus through CausalCorrelationEngine and returns, for
// each alert, the ID of the group it ended up in. window is large enough that
// no group expires mid-run, so grouping is decided purely by isSimilar.
func assignOurs(c gtCorpus) []string {
	e := &CausalCorrelationEngine{window: time.Hour}
	out := make([]string, 0, len(c.alerts))
	for _, entry := range c.alerts {
		if g := e.Correlate(entry.alert); g != nil {
			out = append(out, g.ID)
			continue
		}
		// Correlate returned nil => it created a fresh root group, which it
		// appended last. Read it back for scoring purposes only.
		e.mu.Lock()
		id := e.groups[len(e.groups)-1].ID
		e.mu.Unlock()
		out = append(out, id)
	}
	return out
}

// assignAM runs the corpus through the real Alertmanager grouping path and
// returns each alert's aggregation-group fingerprint.
func assignAM(c gtCorpus, groupBy []string, groupByAll bool) []string {
	g := newAMGrouper(groupBy, groupByAll)
	lsets := toLabelSets(c)
	out := make([]string, 0, len(lsets))
	for _, ls := range lsets {
		out = append(out, g.Group(ls).String())
	}
	return out
}

// ---------------------------------------------------------------------------
// Quality scoring
// ---------------------------------------------------------------------------

// qualityScore holds external clustering metrics computed against the hidden
// ground-truth root cause.
type qualityScore struct {
	n          int
	groups     int     // pages a human receives
	pairPrec   float64 // of pairs we co-grouped, fraction truly co-caused
	pairRecall float64 // of truly co-caused pairs, fraction we co-grouped
	pairF1     float64
	purity     float64 // sum over groups of dominant-root-cause share
	cohesion   float64 // multi-alert incidents fully contained in one group
	noiseRedux float64 // 1 - groups/n : alert-storm compression
}

// scoreGrouping computes pairwise precision/recall/F1, purity, incident
// cohesion and storm compression for one assignment.
func scoreGrouping(c gtCorpus, assign []string) qualityScore {
	n := len(c.alerts)
	q := qualityScore{n: n}

	uniq := map[string]struct{}{}
	for _, g := range assign {
		uniq[g] = struct{}{}
	}
	q.groups = len(uniq)
	if n > 0 {
		q.noiseRedux = 1 - float64(q.groups)/float64(n)
	}

	// Pairwise counting over all C(n,2) pairs.
	var tp, fp, fn float64
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			sameTruth := c.alerts[i].rootCause == c.alerts[j].rootCause
			samePred := assign[i] == assign[j]
			switch {
			case sameTruth && samePred:
				tp++
			case !sameTruth && samePred:
				fp++
			case sameTruth && !samePred:
				fn++
			}
		}
	}
	if tp+fp > 0 {
		q.pairPrec = tp / (tp + fp)
	}
	if tp+fn > 0 {
		q.pairRecall = tp / (tp + fn)
	}
	if q.pairPrec+q.pairRecall > 0 {
		q.pairF1 = 2 * q.pairPrec * q.pairRecall / (q.pairPrec + q.pairRecall)
	}

	// Purity: for each predicted group, the share of its dominant root cause.
	byGroup := map[string]map[string]int{}
	for i, g := range assign {
		if byGroup[g] == nil {
			byGroup[g] = map[string]int{}
		}
		byGroup[g][c.alerts[i].rootCause]++
	}
	var dominant int
	for _, counts := range byGroup {
		best := 0
		for _, v := range counts {
			if v > best {
				best = v
			}
		}
		dominant += best
	}
	if n > 0 {
		q.purity = float64(dominant) / float64(n)
	}

	// Cohesion: fraction of multi-alert incidents whose alerts all landed in
	// exactly one predicted group.
	truthMembers := map[string][]int{}
	for i, e := range c.alerts {
		truthMembers[e.rootCause] = append(truthMembers[e.rootCause], i)
	}
	multi, intact := 0, 0
	for _, idxs := range truthMembers {
		if len(idxs) < 2 {
			continue
		}
		multi++
		first := assign[idxs[0]]
		all := true
		for _, i := range idxs[1:] {
			if assign[i] != first {
				all = false
				break
			}
		}
		if all {
			intact++
		}
	}
	if multi > 0 {
		q.cohesion = float64(intact) / float64(multi)
	}
	return q
}

// amConfigs are the Alertmanager group_by configurations evaluated. This set is
// deliberately generous to the competitor: it spans the canonical documented
// recommendation, service-level grouping, grouping on the exact signal our
// engine privileges (source), the coarsest sane config, and group_by: ['...'].
var amConfigs = []struct {
	name       string
	groupBy    []string
	groupByAll bool
}{
	{name: "group_by=[alertname,cluster]", groupBy: []string{"alertname", "cluster"}},
	{name: "group_by=[cluster,service]", groupBy: []string{"cluster", "service"}},
	{name: "group_by=[source]", groupBy: []string{"source"}},
	{name: "group_by=[cluster]", groupBy: []string{"cluster"}},
	{name: "group_by=['...']", groupByAll: true},
}

// TestGroupingQualityHeadToHead is the correlation-quality half of the T2
// comparison. It reports, for the labelled corpus, how well each system's
// groups line up with ground-truth root causes. It asserts nothing about who
// wins; it prints the numbers and only guards against a scorer that has become
// degenerate.
func TestGroupingQualityHeadToHead(t *testing.T) {
	for _, c := range []gtCorpus{cascadeCorpus(), stormCorpus(4)} {
		ours := scoreGrouping(c, assignOurs(c))
		t.Logf("=== corpus %s (N=%d alerts, %d ground-truth root causes) ===",
			c.name, len(c.alerts), countRootCauses(c))
		t.Logf("%-34s groups=%3d prec=%.3f recall=%.3f F1=%.3f purity=%.3f cohesion=%.3f compression=%.1f%%",
			"M48 causal-correlation", ours.groups, ours.pairPrec, ours.pairRecall,
			ours.pairF1, ours.purity, ours.cohesion, 100*ours.noiseRedux)

		for _, cfg := range amConfigs {
			s := scoreGrouping(c, assignAM(c, cfg.groupBy, cfg.groupByAll))
			t.Logf("%-34s groups=%3d prec=%.3f recall=%.3f F1=%.3f purity=%.3f cohesion=%.3f compression=%.1f%%",
				"AM "+cfg.name, s.groups, s.pairPrec, s.pairRecall,
				s.pairF1, s.purity, s.cohesion, 100*s.noiseRedux)
		}

		if ours.groups == 0 || ours.groups > len(c.alerts) {
			t.Fatalf("scorer produced impossible group count %d for N=%d", ours.groups, len(c.alerts))
		}
	}
}

// countRootCauses returns the number of distinct ground-truth root causes.
func countRootCauses(c gtCorpus) int {
	s := map[string]struct{}{}
	for _, e := range c.alerts {
		s[e.rootCause] = struct{}{}
	}
	return len(s)
}

// TestAlertmanagerCompetitorIsReal is a guard against silently benchmarking a
// stub. It asserts the competitor path is genuinely driven by upstream
// Alertmanager types and that its route/grouping semantics behave as upstream
// documents them.
func TestAlertmanagerCompetitorIsReal(t *testing.T) {
	g := newAMGrouper([]string{"alertname", "cluster"}, false)

	// Real dispatch.Route built from real config.Route, with real defaulting.
	if g.route == nil {
		t.Fatal("dispatch.NewRoute returned nil")
	}
	if g.route.RouteOpts.Receiver != "default" {
		t.Errorf("RouteOpts.Receiver = %q; want default (real RouteOpts defaulting not applied)", g.route.RouteOpts.Receiver)
	}
	if g.route.RouteOpts.GroupWait != dispatch.DefaultRouteOpts.GroupWait {
		t.Errorf("GroupWait = %v; want upstream default %v", g.route.RouteOpts.GroupWait, dispatch.DefaultRouteOpts.GroupWait)
	}
	if len(g.route.RouteOpts.GroupBy) != 2 {
		t.Fatalf("GroupBy size = %d; want 2", len(g.route.RouteOpts.GroupBy))
	}

	// Real Route.Match on a root route with no matchers must match everything.
	ls := model.LabelSet{"alertname": "X", "cluster": "c1", "instance": "i1"}
	if got := g.route.Match(ls); len(got) != 1 {
		t.Fatalf("root Route.Match returned %d routes; want 1", len(got))
	}

	// Alertmanager semantics: alerts differing only OUTSIDE group_by collapse
	// into one aggregation group; differing INSIDE group_by do not.
	same := model.LabelSet{"alertname": "X", "cluster": "c1", "instance": "i2"}
	diff := model.LabelSet{"alertname": "Y", "cluster": "c1", "instance": "i1"}
	fp1 := g.Group(ls)
	fp2 := g.Group(same)
	fp3 := g.Group(diff)
	if fp1 != fp2 {
		t.Error("alerts differing only outside group_by must share an aggregation group")
	}
	if fp1 == fp3 {
		t.Error("alerts differing inside group_by must not share an aggregation group")
	}
	if len(g.groups) != 2 {
		t.Errorf("expected 2 aggregation groups, got %d", len(g.groups))
	}

	// The fingerprint must be the real prometheus/common fingerprint of the
	// projected group labels, not something we invented.
	want := model.LabelSet{"alertname": "X", "cluster": "c1"}.Fingerprint()
	if fp1 != want {
		t.Errorf("group fingerprint = %v; want prometheus/common Fingerprint %v", fp1, want)
	}
}

// ---------------------------------------------------------------------------
// Latency benchmarks: identical work unit on both sides
// ---------------------------------------------------------------------------

// benchOurs measures "group N alerts from scratch" using our causal engine.
func benchOurs(b *testing.B, c gtCorpus) {
	alerts := make([]EvidenceAlert, len(c.alerts))
	for i, e := range c.alerts {
		alerts[i] = e.alert
	}
	groups := scoreGrouping(c, assignOurs(c)).groups

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e := &CausalCorrelationEngine{window: time.Hour}
		for j := range alerts {
			_ = e.Correlate(alerts[j])
		}
	}
	b.StopTimer()
	reportPerAlert(b, len(alerts), groups)
}

// benchAM measures the same unit using the real Alertmanager grouping path.
func benchAM(b *testing.B, c gtCorpus, groupBy []string, groupByAll bool) {
	lsets := toLabelSets(c)
	groups := scoreGrouping(c, assignAM(c, groupBy, groupByAll)).groups

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g := newAMGrouper(groupBy, groupByAll)
		for j := range lsets {
			_ = g.Group(lsets[j])
		}
	}
	b.StopTimer()
	reportPerAlert(b, len(lsets), groups)
}

// reportPerAlert emits normalised per-alert latency plus the group count, so
// -json carries both the latency and the compression result of every run.
func reportPerAlert(b *testing.B, n, groups int) {
	if b.N > 0 && n > 0 {
		b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(n), "ns/alert")
	}
	b.ReportMetric(float64(groups), "groups")
}

func BenchmarkGroupM48Causal_N52(b *testing.B)  { benchOurs(b, cascadeCorpus()) }
func BenchmarkGroupM48Causal_N208(b *testing.B) { benchOurs(b, stormCorpus(4)) }

func BenchmarkGroupAMAlertnameCluster_N52(b *testing.B) {
	benchAM(b, cascadeCorpus(), []string{"alertname", "cluster"}, false)
}

func BenchmarkGroupAMAlertnameCluster_N208(b *testing.B) {
	benchAM(b, stormCorpus(4), []string{"alertname", "cluster"}, false)
}

func BenchmarkGroupAMClusterService_N52(b *testing.B) {
	benchAM(b, cascadeCorpus(), []string{"cluster", "service"}, false)
}

func BenchmarkGroupAMClusterService_N208(b *testing.B) {
	benchAM(b, stormCorpus(4), []string{"cluster", "service"}, false)
}

func BenchmarkGroupAMSource_N52(b *testing.B) {
	benchAM(b, cascadeCorpus(), []string{"source"}, false)
}

func BenchmarkGroupAMSource_N208(b *testing.B) {
	benchAM(b, stormCorpus(4), []string{"source"}, false)
}

// ---------------------------------------------------------------------------
// Cost attribution (honesty aid, not a competitive claim)
// ---------------------------------------------------------------------------

// BenchmarkOursGroupIDGeneration isolates generateGroupID, which draws from
// crypto/rand once per newly created group. If our per-alert latency is worse
// than Alertmanager's, this benchmark shows how much of the gap is entropy
// draw rather than correlation logic.
func BenchmarkOursGroupIDGeneration(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = generateGroupID()
	}
}

// BenchmarkAMFingerprintOnly isolates the competitor's key computation: group
// label projection plus prometheus/common fingerprinting, with no map work.
func BenchmarkAMFingerprintOnly(b *testing.B) {
	g := newAMGrouper([]string{"alertname", "cluster"}, false)
	ls := model.LabelSet{
		"alertname": "HTTPErrorRateHigh", "cluster": "prod-us-east",
		"service": "api-gateway", "instance": "api-gw-1",
		"source": "prometheus", "severity": "critical",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = amGroupLabels(ls, g.route).Fingerprint()
	}
}

// ---------------------------------------------------------------------------
// Statistics helpers used when summarising -count=6 runs
// ---------------------------------------------------------------------------

// medianStddev returns the median and sample standard deviation of xs. It is
// used by TestReportMedianQuality and is the same reduction applied to the
// -count=6 latency samples.
func medianStddev(xs []float64) (median, stddev float64) {
	if len(xs) == 0 {
		return 0, 0
	}
	s := make([]float64, len(xs))
	copy(s, xs)
	sort.Float64s(s)
	mid := len(s) / 2
	if len(s)%2 == 1 {
		median = s[mid]
	} else {
		median = (s[mid-1] + s[mid]) / 2
	}
	var mean float64
	for _, v := range s {
		mean += v
	}
	mean /= float64(len(s))
	if len(s) < 2 {
		return median, 0
	}
	var ss float64
	for _, v := range s {
		ss += (v - mean) * (v - mean)
	}
	return median, math.Sqrt(ss / float64(len(s)-1))
}

// TestGroupingIsDeterministic confirms the quality numbers are not run-to-run
// noise, so a single quality measurement per corpus is legitimate while latency
// still needs -count=6. Group IDs are random, but the PARTITION must be stable.
func TestGroupingIsDeterministic(t *testing.T) {
	c := cascadeCorpus()
	base := scoreGrouping(c, assignOurs(c))
	for i := 0; i < 5; i++ {
		got := scoreGrouping(c, assignOurs(c))
		if got.groups != base.groups || math.Abs(got.pairF1-base.pairF1) > 1e-12 {
			t.Fatalf("run %d: unstable partition: groups %d->%d, F1 %.6f->%.6f",
				i, base.groups, got.groups, base.pairF1, got.pairF1)
		}
	}
	for _, cfg := range amConfigs {
		b0 := scoreGrouping(c, assignAM(c, cfg.groupBy, cfg.groupByAll))
		b1 := scoreGrouping(c, assignAM(c, cfg.groupBy, cfg.groupByAll))
		if b0.groups != b1.groups || math.Abs(b0.pairF1-b1.pairF1) > 1e-12 {
			t.Fatalf("AM %s unstable across runs", cfg.name)
		}
	}
	if m, sd := medianStddev([]float64{1, 2, 3, 4}); m != 2.5 || sd <= 0 {
		t.Fatalf("medianStddev sanity failed: %v %v", m, sd)
	}
}
