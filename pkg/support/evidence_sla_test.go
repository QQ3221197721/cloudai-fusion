package support

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func newTestSLATracker(t *testing.T) *EvidenceSLATracker {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return NewEvidenceSLATracker(priv)
}

// TestTrackSLA_MetAndBreachedProofs verifies SLA proofs are signed and correctly
// classify met vs breached against the priority policy.
func TestTrackSLA_MetAndBreachedProofs(t *testing.T) {
	tracker := newTestSLATracker(t)

	// High priority: response<=30m, resolution<=2h. This one is met.
	met, err := tracker.TrackSLA("TKT-1", SLAMeasurement{
		Priority:       PriorityHigh,
		ResponseTime:   10 * time.Minute,
		ResolutionTime: 90 * time.Minute,
		Resolved:       true,
	})
	if err != nil {
		t.Fatalf("track met: %v", err)
	}
	if !met.ResponseMet || !met.ResolutionMet {
		t.Fatalf("expected SLA met, got %+v", met)
	}
	if met.Receipt == nil || !met.Receipt.Verify() {
		t.Fatal("SLA proof must carry a verifiable receipt")
	}

	// This one breaches both.
	breached, err := tracker.TrackSLA("TKT-2", SLAMeasurement{
		Priority:       PriorityHigh,
		ResponseTime:   2 * time.Hour,
		ResolutionTime: 5 * time.Hour,
		Resolved:       true,
	})
	if err != nil {
		t.Fatalf("track breach: %v", err)
	}
	if breached.ResponseMet || breached.ResolutionMet {
		t.Fatalf("expected SLA breach, got %+v", breached)
	}
	if !breached.Receipt.Verify() {
		t.Fatal("breach receipt must verify")
	}
}

// TestContextRouter_RoutesToExpert trains two engineers on distinct topics and
// verifies TF-IDF routing sends each new ticket to the right specialist.
func TestContextRouter_RoutesToExpert(t *testing.T) {
	tracker := newTestSLATracker(t)
	router := tracker.Router()
	router.AddEngineer("db-expert")
	router.AddEngineer("net-expert")

	// db-expert resolves database tickets quickly.
	dbTickets := []Ticket{
		{Title: "postgres database connection pool exhausted", Description: "database replica postgres timeout query slow"},
		{Title: "postgres index bloat vacuum", Description: "database postgres autovacuum table bloat query planner"},
		{Title: "database migration deadlock postgres", Description: "postgres transaction deadlock migration schema"},
	}
	for _, tk := range dbTickets {
		router.Learn(tk, "db-expert", 30*time.Minute)
	}

	// net-expert resolves networking tickets quickly.
	netTickets := []Ticket{
		{Title: "kubernetes ingress network latency", Description: "network kubernetes ingress packet loss routing"},
		{Title: "network firewall dropping packets", Description: "firewall network packet drop iptables routing rules"},
		{Title: "kubernetes service mesh network policy", Description: "network kubernetes mesh policy routing sidecar"},
	}
	for _, tk := range netTickets {
		router.Learn(tk, "net-expert", 30*time.Minute)
	}

	// A new database ticket must route to the db expert.
	dbTicket := Ticket{Title: "postgres database slow query", Description: "postgres database query planner index slow"}
	if id, conf := router.Route(dbTicket); id != "db-expert" || conf <= 0 {
		t.Fatalf("database ticket should route to db-expert, got %q (conf=%.3f)", id, conf)
	}

	// A new network ticket must route to the net expert.
	netTicket := Ticket{Title: "kubernetes network routing broken", Description: "network kubernetes routing ingress packet"}
	if id, conf := router.Route(netTicket); id != "net-expert" || conf <= 0 {
		t.Fatalf("network ticket should route to net-expert, got %q (conf=%.3f)", id, conf)
	}
}

// TestContextRouter_EmptyReturnsNoMatch verifies routing with no learned
// expertise returns no engineer.
func TestContextRouter_EmptyReturnsNoMatch(t *testing.T) {
	router := NewAIContextRouter()
	router.AddEngineer("e1")
	if id, conf := router.Route(Ticket{Title: "anything", Description: "unseen terms here"}); id != "" || conf != 0 {
		t.Fatalf("expected no match with no learned expertise, got %q/%.3f", id, conf)
	}
}

// TestCosine exercises the cosine-similarity helper directly.
func TestCosine(t *testing.T) {
	a := map[string]float64{"x": 1, "y": 1}
	if got := cosine(a, a); got < 0.999 {
		t.Fatalf("identical vectors should have cosine ~1, got %.4f", got)
	}
	orth := map[string]float64{"z": 1}
	if got := cosine(a, orth); got != 0 {
		t.Fatalf("orthogonal vectors should have cosine 0, got %.4f", got)
	}
}
