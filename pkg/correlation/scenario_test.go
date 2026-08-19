package correlation

// scenario_test.go builds the injected-fault corpus used to measure compression,
// root-cause accuracy and — most importantly — mis-suppression.
//
// Every scenario carries ground truth: which incident each alert belongs to and
// which alert is the true root of that incident. Alerts from different incidents
// inside the same scenario are the mis-suppression probes: an algorithm that
// folds two independent incidents together is unsafe no matter how good its
// compression looks.
//
// Five fault classes, 24 seeded instances each = 120 scenarios.
//
//	cascade      linear dependency chain failing downstream-to-upstream
//	partition    network split hitting every service in one zone at once
//	spof         one shared dependency (db/cache) fanning out to all consumers
//	concurrent   2-3 genuinely independent incidents in disjoint topologies
//	mixed        one cascade plus one independent incident elsewhere
//
// The generator is a test fixture on purpose: production code must not depend on
// math/rand.

import (
	"fmt"
	"math/rand"
	"time"
)

// truthAlert pairs an alert with the incident that produced it.
type truthAlert struct {
	Alert      Alert
	IncidentID string
	IsRoot     bool
}

// scenario is one injected-fault case with ground truth attached.
type scenario struct {
	Name      string
	Class     string
	Alerts    []Alert
	Topo      *Topology
	Incidents int
	// IncidentOf maps alert ID -> incident ID.
	IncidentOf map[string]string
	// TrueRoots maps incident ID -> the root alert ID of that incident.
	TrueRoots map[string]string
}

// independentPairs returns alert ID pairs that belong to different incidents.
// Suppressing one because of the other is a mis-suppression.
func (s *scenario) sameIncident(a, b string) bool {
	return s.IncidentOf[a] == s.IncidentOf[b]
}

// trueRootIDs returns the ground-truth root of every incident.
func (s *scenario) trueRootIDs() []string {
	out := make([]string, 0, len(s.TrueRoots))
	for _, id := range s.TrueRoots {
		out = append(out, id)
	}
	return out
}

var alertKinds = []string{
	"HighLatency", "ErrorRateSpike", "ConnectionRefused", "TimeoutExceeded",
	"SaturatedPool", "HealthCheckFailed", "QueueBacklog", "RetryStorm",
}

// scenarioBuilder accumulates alerts and ground truth for one scenario.
type scenarioBuilder struct {
	rng     *rand.Rand
	base    time.Time
	alerts  []truthAlert
	topo    *Topology
	seq     int
	roots   map[string]string
	members map[string]string
}

func newScenarioBuilder(seed int64) *scenarioBuilder {
	return &scenarioBuilder{
		rng:     rand.New(rand.NewSource(seed)),
		base:    time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
		topo:    NewTopology(),
		roots:   map[string]string{},
		members: map[string]string{},
	}
}

// add records one alert. offset is relative to the scenario start.
func (b *scenarioBuilder) add(incident, service, kind string, sev Severity, offset time.Duration, isRoot bool, labels map[string]string) string {
	b.seq++
	id := fmt.Sprintf("a%03d", b.seq)
	l := map[string]string{"cluster": "prod", "env": "production"}
	for k, v := range labels {
		l[k] = v
	}
	a := Alert{
		ID:        id,
		Service:   service,
		Instance:  fmt.Sprintf("%s-%d", service, b.rng.Intn(3)),
		Kind:      kind,
		Severity:  sev,
		Timestamp: b.base.Add(offset),
		Labels:    l,
	}
	b.alerts = append(b.alerts, truthAlert{Alert: a, IncidentID: incident, IsRoot: isRoot})
	b.members[id] = incident
	if isRoot {
		b.roots[incident] = id
	}
	return id
}

// jitter returns a sub-epsilon offset so that alerts land in the same scrape
// bucket. This is what produces genuine SCCs in the candidate graph.
func (b *scenarioBuilder) jitter() time.Duration {
	return time.Duration(b.rng.Intn(1200)) * time.Millisecond
}

func (b *scenarioBuilder) finish(name, class string, incidents int) *scenario {
	alerts := make([]Alert, len(b.alerts))
	for i, ta := range b.alerts {
		alerts[i] = ta.Alert
	}
	return &scenario{
		Name:       name,
		Class:      class,
		Alerts:     alerts,
		Topo:       b.topo,
		Incidents:  incidents,
		IncidentOf: b.members,
		TrueRoots:  b.roots,
	}
}

// chain wires svc[0] -> svc[1] -> ... in the dependency topology, meaning svc[i]
// depends on svc[i+1]. A failure in the last service propagates upstream.
func (b *scenarioBuilder) chain(svcs []string) {
	for i := 0; i+1 < len(svcs); i++ {
		b.topo.AddDependency(svcs[i], svcs[i+1])
	}
}

// fanIn makes every consumer depend on the shared provider.
func (b *scenarioBuilder) fanIn(consumers []string, provider string) {
	for _, c := range consumers {
		b.topo.AddDependency(c, provider)
	}
}

// buildCascade injects a downstream failure that walks up a dependency chain,
// plus one unrelated incident in a disjoint topology as a mis-suppression probe.
func buildCascade(seed int64, idx int) *scenario {
	b := newScenarioBuilder(seed)
	depth := 4 + b.rng.Intn(4) // 4..7
	svcs := make([]string, depth)
	for i := range svcs {
		svcs[i] = fmt.Sprintf("tier%d-svc", i)
	}
	b.chain(svcs)

	// The root is the deepest service; effects walk upstream with real lag.
	root := svcs[depth-1]
	b.add("inc-cascade", root, "ConnectionRefused", SeverityCritical, b.jitter(), true, map[string]string{"tier": "data"})
	offset := 3 * time.Second
	for i := depth - 2; i >= 0; i-- {
		kind := alertKinds[b.rng.Intn(len(alertKinds))]
		// Effects are never more severe than the root: that is what gate G5 asserts.
		sev := SeverityMajor
		if i == 0 {
			sev = SeverityWarning
		}
		b.add("inc-cascade", svcs[i], kind, sev, offset+b.jitter(), false, map[string]string{"tier": fmt.Sprintf("t%d", i)})
		// A couple of services emit two symptoms in the same scrape bucket.
		if b.rng.Intn(2) == 0 {
			b.add("inc-cascade", svcs[i], "HighLatency", SeverityWarning, offset+b.jitter(), false, map[string]string{"tier": fmt.Sprintf("t%d", i)})
		}
		offset += time.Duration(4+b.rng.Intn(6)) * time.Second
	}

	// Independent probe: a separate topology island with its own root.
	probe := []string{"iso-front", "iso-back"}
	b.chain(probe)
	b.add("inc-probe", "iso-back", "SaturatedPool", SeverityMajor, 40*time.Second+b.jitter(), true, map[string]string{"tier": "isolated"})

	return b.finish(fmt.Sprintf("cascade-%02d", idx), "cascade", 2)
}

// buildPartition injects a zone-wide network split: every service in the zone
// alerts within one scrape interval, so ordering between them is unobservable.
func buildPartition(seed int64, idx int) *scenario {
	b := newScenarioBuilder(seed)
	n := 5 + b.rng.Intn(4) // 5..8
	svcs := make([]string, n)
	for i := range svcs {
		svcs[i] = fmt.Sprintf("zone-a-svc%d", i)
	}
	// Zone services form a mesh of mutual dependencies (retry loops), which is
	// the other source of real SCCs besides scrape-bucket ties.
	for i := range svcs {
		b.topo.AddDependency(svcs[i], svcs[(i+1)%n])
	}
	gw := "zone-a-gateway"
	b.fanIn(svcs, gw)

	b.add("inc-partition", gw, "HealthCheckFailed", SeverityCritical, b.jitter(), true, map[string]string{"zone": "a"})
	for i, s := range svcs {
		kind := alertKinds[b.rng.Intn(len(alertKinds))]
		b.add("inc-partition", s, kind, SeverityMajor, time.Duration(i)*300*time.Millisecond+b.jitter(), false, map[string]string{"zone": "a"})
	}

	// Probe: a service in zone b, unreachable from zone a.
	b.topo.AddDependency("zone-b-svc0", "zone-b-gateway")
	b.add("inc-probe", "zone-b-svc0", "QueueBacklog", SeverityWarning, 90*time.Second+b.jitter(), true, map[string]string{"zone": "b"})

	return b.finish(fmt.Sprintf("partition-%02d", idx), "partition", 2)
}

// buildSPOF injects a shared-dependency failure fanning out to many consumers.
func buildSPOF(seed int64, idx int) *scenario {
	b := newScenarioBuilder(seed)
	fan := 6 + b.rng.Intn(6) // 6..11
	consumers := make([]string, fan)
	for i := range consumers {
		consumers[i] = fmt.Sprintf("consumer%d", i)
	}
	shared := "shared-postgres"
	b.fanIn(consumers, shared)

	b.add("inc-spof", shared, "ConnectionRefused", SeverityCritical, b.jitter(), true, map[string]string{"component": "db"})
	for i, c := range consumers {
		sev := SeverityMajor
		if i%3 == 0 {
			sev = SeverityWarning
		}
		b.add("inc-spof", c, "TimeoutExceeded", sev, time.Duration(2+i)*time.Second+b.jitter(), false, map[string]string{"component": "app"})
	}

	// Probe: a consumer of a *different* database.
	b.topo.AddDependency("other-consumer", "other-postgres")
	b.add("inc-probe", "other-consumer", "TimeoutExceeded", SeverityMajor, 120*time.Second+b.jitter(), true, map[string]string{"component": "app"})

	return b.finish(fmt.Sprintf("spof-%02d", idx), "spof", 2)
}

// buildConcurrent injects 2-3 genuinely independent incidents in disjoint
// topology islands. Correct behaviour is one root per incident and zero
// cross-incident suppression.
func buildConcurrent(seed int64, idx int) *scenario {
	b := newScenarioBuilder(seed)
	islands := 2 + b.rng.Intn(2) // 2..3
	for k := 0; k < islands; k++ {
		front := fmt.Sprintf("island%d-front", k)
		back := fmt.Sprintf("island%d-back", k)
		b.topo.AddDependency(front, back)
		inc := fmt.Sprintf("inc-%d", k)
		// Independent incidents deliberately overlap in time: only topology and
		// label evidence can tell them apart.
		start := time.Duration(k) * 2 * time.Second
		b.add(inc, back, "ConnectionRefused", SeverityCritical, start+b.jitter(), true, map[string]string{"island": fmt.Sprintf("i%d", k)})
		b.add(inc, front, alertKinds[b.rng.Intn(len(alertKinds))], SeverityMajor, start+5*time.Second+b.jitter(), false, map[string]string{"island": fmt.Sprintf("i%d", k)})
		b.add(inc, front, "HighLatency", SeverityWarning, start+7*time.Second+b.jitter(), false, map[string]string{"island": fmt.Sprintf("i%d", k)})
	}
	return b.finish(fmt.Sprintf("concurrent-%02d", idx), "concurrent", islands)
}

// buildMixed injects one cascade and one independent incident that fire in the
// same window, which is where naive time-window dedup does the most damage.
func buildMixed(seed int64, idx int) *scenario {
	b := newScenarioBuilder(seed)
	svcs := []string{"api", "orders", "inventory", "warehouse-db"}
	b.chain(svcs)
	b.add("inc-cascade", "warehouse-db", "SaturatedPool", SeverityCritical, b.jitter(), true, map[string]string{"tier": "data"})
	for i := len(svcs) - 2; i >= 0; i-- {
		b.add("inc-cascade", svcs[i], "TimeoutExceeded", SeverityMajor,
			time.Duration(len(svcs)-i)*3*time.Second+b.jitter(), false, map[string]string{"tier": "app"})
	}

	// Independent incident: same alert kinds, same time window, different island.
	other := []string{"billing", "billing-cache"}
	b.chain(other)
	b.add("inc-other", "billing-cache", "TimeoutExceeded", SeverityMajor, 4*time.Second+b.jitter(), true, map[string]string{"tier": "billing"})
	b.add("inc-other", "billing", "TimeoutExceeded", SeverityWarning, 9*time.Second+b.jitter(), false, map[string]string{"tier": "billing"})

	return b.finish(fmt.Sprintf("mixed-%02d", idx), "mixed", 2)
}

// buildCorpus returns the full 120-scenario corpus. Seeds are fixed so results
// are reproducible across runs and machines.
func buildCorpus() []*scenario {
	const perClass = 24
	out := make([]*scenario, 0, perClass*5)
	builders := []struct {
		name string
		fn   func(int64, int) *scenario
	}{
		{"cascade", buildCascade},
		{"partition", buildPartition},
		{"spof", buildSPOF},
		{"concurrent", buildConcurrent},
		{"mixed", buildMixed},
	}
	for bi, b := range builders {
		for i := 0; i < perClass; i++ {
			seed := int64(1_000_000*(bi+1) + i)
			out = append(out, b.fn(seed, i))
		}
	}
	return out
}
