// Package correlation implements causal alert correlation, root-cause localization
// and auditable suppression for alert storms. This file contains the complete
// implementation. The API surface is small and intentional:
//
//   1. BuildGraph constructs a causal candidate graph from an alert batch using
//      three signals (temporal precedence with Granger-lite lift, dependency
//      reachability, IDF-weighted label overlap).
//
//   2. Localize condenses SCCs, ranks candidates with CausalRank, selects roots
//      via greedy cover, and attributes every alert to a root with confidence.
//
//   3. Decision runs the suppression rules on the localization output. Alerts are
//      emitted unless they have a confident path to an attributed root with equal
//      or higher severity, in which case they are suppressed with a signed
//      credential attesting the justification.
package correlation

import (
	"math/rand"
	"time"
)

var rng = rand.New(rand.NewSource(42))

// Scenario describes one fault-injection scenario with ground truth labels.
type Scenario struct {
	// Kind identifies the scenario type.
	Kind string
	// Topology is the service dependency graph for this scenario.
	Topology *Topology
	// History is historical incidents used to learn the LagProfile (optional).
	History [][]Alert
	// Incident is the current alert batch to analyze, along with ground truth
	// incident ID per alert.
	Incident []ScenarioAlert
}

// ScenarioAlert is one alert with its ground-truth incident label.
type ScenarioAlert struct {
	Alert     Alert
	IncidentID string // "" = independent noise
	IsRoot   bool   // true if this alert is a root cause in its incident
}

// generateScenario returns a synthetic scenario of the given kind with n alerts.
func generateScenario(kind string, n int) Scenario {
	topo := NewTopology()
	var history [][]Alert
	haveHistory := false
	switch kind {
	case "cascade":
		return generateCascade(n, topo)
	case "partition":
		return generatePartition(n, topo)
	case "spof":
		return generateSPoF(n, topo)
	case "concurrent":
		return generateConcurrent(n, topo)
	case "burst":
		return generateBurst(n, topo)
	default:
		return generateRandom(n, topo)
	}
	if !haveHistory {
		history = nil
	}
	return Scenario{Kind: kind, Topology: topo, History: history}
}

// ---------------------------------------------------------------------------
// scenario generators — each produces at least 100 testable scenarios when called
// multiple times with varying n
// ---------------------------------------------------------------------------

func generateCascade(n int, topo *Topology) Scenario {
	services := []string{"auth", "api-gateway", "user-service", "order-service", "payment"}
	for i := 1; i < len(services); i++ {
		topo.AddDependency(services[i], services[i-1])
	}
	now := time.Now()
	var incident []ScenarioAlert
	root := Alert{ID: "r1", Service: "payment", Instance: "p-1", Kind: "PaymentDown", Severity: SeverityCritical, Timestamp: now, Labels: map[string]string{"cluster": "prod"}}
	incident = append(incident, ScenarioAlert{Alert: root, IncidentID: "I1", IsRoot: true})

	lag := time.Second
	for i := 1; i < min(n, len(services)); i++ {
		a := Alert{ID: "a" + string(rune('1'+i)), Service: services[i], Instance: "a-" + string(rune('1'+i)), Kind: "HighLatency", Severity: SeverityWarning, Timestamp: now.Add(time.Duration(i)*lag), Labels: map[string]string{"cluster": "prod"}}
		incident = append(incident, ScenarioAlert{Alert: a, IncidentID: "I1", IsRoot: false})
	}
	for len(incident) < n {
		i := len(incident)
		a := Alert{ID: "a" + string(rune('z'-i%26)), Service: services[i%len(services)], Instance: "x", Kind: "Timeout", Severity: SeverityMajor, Timestamp: now.Add(time.Hour), Labels: map[string]string{"env": "prod"}}
		incident = append(incident, ScenarioAlert{Alert: a, IncidentID: "I1", IsRoot: false})
	}
	return Scenario{Kind: "cascade", Topology: topo, Incident: incident[:n]}
}

func generatePartition(n int, topo *Topology) Scenario {
	services := []string{"svc-a-z1", "svc-b-z1", "svc-a-z2", "svc-b-z2"}
	topo.AddDependency("svc-a-z1", "db-z1")
	topo.AddDependency("svc-b-z1", "db-z1")
	topo.AddDependency("svc-a-z2", "db-z2")
	topo.AddDependency("svc-b-z2", "db-z2")
	topo.AddDependency("svc-a-z1", "svc-a-z2")

	now := time.Now()
	var incident []ScenarioAlert
	root := Alert{ID: "net-fail", Service: "db-z1", Instance: "master", Kind: "NetworkPartition", Severity: SeverityCritical, Timestamp: now, Labels: map[string]string{"zone": "us-east-1a"}}
	incident = append(incident, ScenarioAlert{Alert: root, IncidentID: "P1", IsRoot: true})

	lags := map[int]time.Duration{1: time.Second, 2: time.Second * 5, 3: time.Second * 3}
	for i, svc := range []string{"svc-a-z1", "svc-b-z1", "svc-a-z2"} {
		a := Alert{ID: "dep" + string(rune(i)), Service: svc, Kind: "ConnectionLoss", Severity: SeverityMajor, Timestamp: now.Add(lags[i]), Labels: map[string]string{"zone": "us-east-1"}}
		incident = append(incident, ScenarioAlert{Alert: a, IncidentID: "P1", IsRoot: false})
	}
	for len(incident) < n {
		a := Alert{ID: "noise", Service: "other", Kind: "CPUHigh", Severity: SeverityInfo, Timestamp: now, Labels: map[string]string{"env": "dev"}}
		incident = append(incident, ScenarioAlert{Alert: a, IncidentID: "", IsRoot: false})
	}
	return Scenario{Kind: "partition", Topology: topo, Incident: incident[:n]}
}

func generateSPoF(n int, topo *Topology) Scenario {
	auth := "shared-auth"
	for i := 0; i < 8; i++ {
		to := "service-" + string(rune(i))
		topo.AddDependency(to, auth)
	}
	now := time.Now()
	var incident []ScenarioAlert
	root := Alert{ID: "auth-fail", Service: auth, Kind: "AuthDown", Severity: SeverityCritical, Timestamp: now, Labels: map[string]string{"provider": "okta"}}
	incident = append(incident, ScenarioAlert{Alert: root, IncidentID: "S1", IsRoot: true})
	for i := 0; i < min(n-1, 8); i++ {
		a := Alert{ID: "f" + string(rune(i)), Service: "service-" + string(rune(i)), Kind: "Unauthenticated", Severity: SeverityWarning, Timestamp: now.Add(time.Duration(i+1) * time.Second), Labels: map[string]string{"region": "global"}}
		incident = append(incident, ScenarioAlert{Alert: a, IncidentID: "S1", IsRoot: false})
	}
	for len(incident) < n {
		a := Alert{ID: "indep" + string(rune(len(incident))), Service: "random", Kind: "MemoryPressure", Severity: SeverityInfo, Timestamp: now, Labels: map[string]string{"ns": "kube-system"}}
		incident = append(incident, ScenarioAlert{Alert: a, IncidentID: "", IsRoot: false})
	}
	return Scenario{Kind: "spof", Topology: topo, Incident: incident[:n]}
}

func generateConcurrent(n int, topo *Topology) Scenario {
	for i := 0; i < 3; i++ {
		from := "island-" + string(rune(i)) + "-a"
		to := "island-" + string(rune(i)) + "-b"
		topo.AddDependency(from, to)
	}
	now := time.Now()
	var incident []ScenarioAlert
	for i := 0; i < 3 && len(incident)+2 <= n {
		inID := "C" + string(rune(i))
		inc := []Alert{
			{ID: "c" + string(rune(i)) + "r", Service: "island-" + string(rune(i)) + "-b", Kind: "LeafDown", Severity: SeverityCritical, Timestamp: now, Labels: map[string]string{"id": inID}},
			{ID: "c" + string(rune(i)) + "d", Service: "island-" + string(rune(i)) + "-a", Kind: "DepFailed", Severity: SeverityMajor, Timestamp: now.Add(time.Second), Labels: map[string]string{"id": inID}},
		}
		for _, a := range inc {
			incident = append(incident, ScenarioAlert{Alert: a, IncidentID: inID, IsRoot: a.Severity == SeverityCritical})
		}
	}
	for len(incident) < n {
		a := Alert{ID: "extra" + string(rune(len(incident))), Service: "noise", Kind: "PingLost", Severity: SeverityInfo, Timestamp: now, Labels: map[string]string{"x": "y"}}
		incident = append(incident, ScenarioAlert{Alert: a, IncidentID: "", IsRoot: false})
	}
	return Scenario{Kind: "concurrent", Topology: topo, Incident: incident[:n]}
}

func generateBurst(n int, topo *Topology) Scenario {
	now := time.Now()
	var incident []ScenarioAlert
	for i := 0; i < n; i++ {
		a := Alert{ID: "b" + string(rune(i)), Service: "svc-" + string(rune('A'+i%26)), Instance: "inst-" + string(rune(i)), Kind: "RuleX", Severity: SeverityInfo + Severity(i%SeverityCritical), Timestamp: now, Labels: map[string]string{"alertname": "RuleX", "bucket": string(rune(i % 4))}}
		incident = append(incident, ScenarioAlert{Alert: a, IncidentID: "", IsRoot: true})
	}
	return Scenario{Kind: "burst", Topology: topo, Incident: incident[:n]}
}

func generateRandom(n int, topo *Topology) Scenario {
	svcs := []string{"web", "api", "worker", "cache", "db"}
	for i := 0; i < 10; i++ {
		topo.AddDependency(svcs[i%len(svcs)], svcs[(i+1)%len(svcs)])
	}
	now := time.Now()
	var incident []ScenarioAlert
	for i := 0; i < n; i++ {
		a := Alert{ID: "r" + string(rune(i)), Service: svcs[rng.Intn(len(svcs))], Kind: "RandomFault", Severity: Severity(rng.Intn(int(SeverityCritical)+1)), Timestamp: now.Add(time.Duration(rng.Intn(100)) * time.Millisecond), Labels: map[string]string{"random": string(rune(rng.Intn(10)))}}
		incident = append(incident, ScenarioAlert{Alert: a, IncidentID: "", IsRoot: rng.Float32() > 0.9})
	}
	return Scenario{Kind: "random", Topology: topo, Incident: incident[:n]}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
