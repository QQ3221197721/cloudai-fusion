package main

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/m48alert"
)

func main() {
	alerts, gold := genIncidentAlerts(7, 7, 42)
	fmt.Printf("Total alerts: %d\n", len(alerts))
	fmt.Printf("Gold clusters: %d incidents\n", len(gold))
	for incID, fps := range gold {
		if len(fps) > 0 {
			fmt.Printf("  %s: %d members (%v...)\n", incID, len(fps), fps[:min(3, len(fps))])
		}
	}

	cfg := m48alert.DefaultConfig()
	icl := m48alert.NewIntelligentAlertClustering(cfg)
	result := icl.ClusterSync(nil, alerts)

	fmt.Printf("\nSingle-linkage result:\n")
	fmt.Printf("  Clusters: %d\n", len(result.Clusters))
	for i, c := range result.Clusters {
		fps := make([]string, len(c.Alerts))
		for j, a := range c.Alerts {
			fps[j] = m48alert.Fingerprint(a)
		}
		fmt.Printf("  Cluster %d: %d alerts", i, len(c.Alerts))
		if len(c.Alerts) <= 10 {
			fmt.Printf(" (%v)\n", fps)
		} else {
			fmt.Printf(" (%v...)\n", fps[:min(3, len(fps))])
		}
		if len(c.Alerts) > 0 {
			labels := c.Alerts[0].Labels
			fmt.Printf("    Labels: incident=%s region=%s team=%s severity=%s\n",
				labels["incident"], labels["region"], labels["team"], labels["severity"])
		}
	}
}

func genIncidentAlerts(nIncidents, membersPer int, seed int64) ([]*m48alert.Alert, map[string][]string) {
	alertRoles := []struct{ alertname, instance string }{
		{"HighLatency", "edge-1"}, {"DBSlowQuery", "pg-1"}, {"CacheEviction", "redis-1"},
		{"QueueBacklog", "mq-1"}, {"ErrorRateSpike", "api-1"}, {"CPUThrottle", "node-1"},
		{"MemPressure", "node-2"}, {"DiskIOWait", "nfs-1"},
	}
	regions := []string{"us-east", "us-west", "eu-central"}

	rng := rand.New(rand.NewSource(seed))
	alerts := make([]*m48alert.Alert, 0, nIncidents*membersPer)
	gold := make(map[string][]string)
	base := time.Now()

	for inc := 0; inc < nIncidents; inc++ {
		incID := fmt.Sprintf("incident-%d", inc)
		region := fmt.Sprintf("%s-r%d", regions[inc%len(regions)], inc)
		severity := "critical"
		if inc%2 == 1 { severity = "warning" }
		team := fmt.Sprintf("team-%d", inc)
		onset := base.Add(time.Duration(inc*90) * time.Second)

		var fps []string
		for m := 0; m < membersPer; m++ {
			role := alertRoles[m%len(alertRoles)]
			labels := map[string]string{
				"incident":  incID,
				"region":    region,
				"severity":  severity,
				"team":      team,
				"alertname": role.alertname,
				"instance":  role.instance,
			}
			a := &m48alert.Alert{Labels: labels, Value: rng.Float64(), StartsAt: onset.Add(time.Duration(m)*time.Second), EndsAt: onset.Add(time.Duration(m+30)*time.Second)}
			alerts = append(alerts, a)
			fps = append(fps, m48alert.Fingerprint(a))
		}
		gold[incID] = fps
	}
	rng.Shuffle(len(alerts), func(i, j int) { alerts[i], alerts[j] = alerts[j], alerts[i] })
	return alerts, gold
}

func min(a, b int) int {
	if a < b { return a }
	return b
}
