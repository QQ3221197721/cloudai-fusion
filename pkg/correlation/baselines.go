package correlation

// baselines.go provides three baseline strategies to compare our causal approach against.

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Baseline is the common interface for all baseline implementations.
type Baseline interface {
	Name() string
	Decide(alerts []Alert) (*Decision, error)
}

// AlertmanagerGrouping emulates Prometheus Alertmanager's group_by semantics.
type AlertmanagerGrouping struct {
	GroupBy    []string
	InhibitRules []InhibitRule
}

type InhibitRule struct {
	SourceMatch         string
	TargetMatch         string
	SeveritySourceMin   Severity
	SeverityTargetMax   Severity
	EqualLabels         []string
}

func (i InhibitRule) CanInhibit(source, target Alert) bool {
	if !strings.Contains(strings.ToLower(source.Kind), strings.ToLower(i.SourceMatch)) ||
		!strings.Contains(strings.ToLower(target.Kind), strings.ToLower(i.TargetMatch)) {
		return false
	}
	if source.Severity < i.SeveritySourceMin || target.Severity > i.SeverityTargetMax {
		return false
	}
	for _, k := range i.EqualLabels {
		if source.Labels[k] != target.Labels[k] {
			return false
		}
	}
	return true
}

func (g *AlertmanagerGrouping) Name() string { return "alertmanager_grouping" }

func (g *AlertmanagerGrouping) Decide(alerts []Alert) (*Decision, error) {
	d := &Decision{Verdicts: make([]AlertVerdict, 0, len(alerts)), Total: len(alerts)}
	if len(alerts) == 0 {
		return d, nil
	}
	p := DefaultParams()
	p.SuppressThreshold = 0.5
	
	buckets := make(map[string][]int)
	for i, a := range alerts {
		var bucket []string
		for _, k := range g.GroupBy {
			v := a.Labels[k]
			bucket = append(bucket, k+"="+v)
		}
		key := fmt.Sprintf("%v", bucket)
		buckets[key] = append(buckets[key], i)
	}
	
	active := make(map[string]int)
	for key, indices := range buckets {
		maxIdx := indices[0]
		maxSev := alerts[maxIdx].Severity
		for _, idx := range indices[1:] {
			if alerts[idx].Severity > maxSev {
				maxSev = alerts[idx].Severity
				maxIdx = idx
			}
		}
		active[key] = maxIdx
	}
	
	inhibited := make(map[int]bool)
	inhibitor := make(map[int]string)
	sevOf := make(map[string]Severity, len(alerts))
	for _, a := range alerts {
		sevOf[a.ID] = a.Severity
	}
	for key, maxIdx := range active {
		source := alerts[maxIdx]
		for _, idx := range buckets[key] {
			target := alerts[idx]
			if idx == maxIdx {
				continue
			}
			for _, rule := range g.InhibitRules {
				if rule.CanInhibit(source, target) {
					inhibited[idx] = true
					inhibitor[idx] = source.ID
					break
				}
			}
		}
	}
	
	for i, a := range alerts {
		v := AlertVerdict{
			AlertID:  a.ID,
			Severity: a.Severity,
		}
		if inhibited[i] {
			v.Verdict = VerdictSuppressed
			v.Reason = ReasonCausalDerived
			v.RootAlertID = inhibitor[i]
			v.RootSeverity = sevOf[inhibitor[i]]
		} else {
			v.Verdict = VerdictEmitted
			v.Reason = ReasonUnattributed
			v.RootAlertID = a.ID
		}
		d.Verdicts = append(d.Verdicts, v)
	}
	
	sort.SliceStable(d.Verdicts, func(i, j int) bool { return d.Verdicts[i].AlertID < d.Verdicts[j].AlertID })
	for _, v := range d.Verdicts {
		if v.Suppressed() {
			d.SuppressedCount++
		} else {
			d.Emitted++
		}
	}
	d.Params = p
	d.GraphDigest = ""
	return d, nil
}

// NaiveTimeWindowDedup keeps the first alert in each time window for each (kind, service) pair.
type NaiveTimeWindowDedup struct {
	Window time.Duration
}

func (t *NaiveTimeWindowDedup) Name() string {
	if t.Window == 0 {
		t.Window = 5 * time.Minute
	}
	return fmt.Sprintf("naive_timewindow_%v", t.Window)
}

func (t *NaiveTimeWindowDedup) Decide(alerts []Alert) (*Decision, error) {
	d := &Decision{Verdicts: make([]AlertVerdict, 0, len(alerts)), Total: len(alerts)}
	if len(alerts) == 0 {
		return d, nil
	}
	w := t.Window
	if w == 0 {
		w = 5 * time.Minute
	}
	p := DefaultParams()
	p.SuppressThreshold = 0.5
	
	type key struct{ Kind, Service string }
	lastSeen := make(map[key]time.Time)
	keys := make([]key, len(alerts))
	for i, a := range alerts {
		keys[i] = key{a.Kind, a.Service}
	}
	keyStr := func(k key) string { return k.Kind + "\x00" + k.Service }
	order := make([]int, len(alerts))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool {
		ki, kj := keyStr(keys[order[i]]), keyStr(keys[order[j]])
		if ki != kj {
			return ki < kj
		}
		return alerts[order[i]].Timestamp.Before(alerts[order[j]].Timestamp)
	})
	
	for _, i := range order {
		a := alerts[i]
		k := keys[i]
		if seen, ok := lastSeen[k]; ok && a.Timestamp.Sub(seen) < w {
			d.Verdicts = append(d.Verdicts, AlertVerdict{
				AlertID:      a.ID,
				Verdict:      VerdictSuppressed,
				Reason:       ReasonCausalDerived,
				RootAlertID:  "",
				Confidence:   0,
				PathHops:     0,
				Severity:     a.Severity,
				RootSeverity: 0,
			})
		} else {
			d.Verdicts = append(d.Verdicts, AlertVerdict{
				AlertID: a.ID,
				Verdict: VerdictEmitted,
				Reason:  ReasonUnattributed,
				Severity: a.Severity,
			})
			lastSeen[k] = a.Timestamp
		}
	}
	
	sort.SliceStable(d.Verdicts, func(i, j int) bool { return d.Verdicts[i].AlertID < d.Verdicts[j].AlertID })
	for _, v := range d.Verdicts {
		if v.Suppressed() {
			d.SuppressedCount++
		} else {
			d.Emitted++
		}
	}
	d.Params = p
	d.GraphDigest = ""
	return d, nil
}

// NoDedup emits every alert unchanged.
type NoDedup struct{}

func (n *NoDedup) Name() string { return "no_dedup" }

func (n *NoDedup) Decide(alerts []Alert) (*Decision, error) {
	d := &Decision{Verdicts: make([]AlertVerdict, len(alerts)), Total: len(alerts)}
	if len(alerts) == 0 {
		return d, nil
	}
	p := DefaultParams()
	for i, a := range alerts {
		d.Verdicts[i] = AlertVerdict{
			AlertID:      a.ID,
			Verdict:      VerdictEmitted,
			Reason:       ReasonUnattributed,
			Severity:     a.Severity,
			RootAlertID:  "",
			Confidence:   0,
			PathHops:     0,
			RootSeverity: 0,
		}
	}
	d.Params = p
	d.Emitted = len(alerts)
	d.GraphDigest = ""
	return d, nil
}
