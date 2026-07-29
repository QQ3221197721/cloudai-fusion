package soc

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/intel"
)

// identity.go implements the L6 identity-governance detector: brute-force and
// impossible-travel anomalies over authentication events. It is deterministic
// given its input, so results are reproducible in CI.

// AuthEvent is one authentication attempt.
type AuthEvent struct {
	User      string    `json:"user"`
	SourceIP  string    `json:"source_ip"`
	Country   string    `json:"country"`
	Success   bool      `json:"success"`
	Timestamp time.Time `json:"timestamp"`
}

// IdentityConfig tunes the L6 thresholds.
type IdentityConfig struct {
	FailureThreshold int           // failures within Window that flag brute force
	Window           time.Duration // correlation window
}

// DefaultIdentityConfig returns sensible defaults (5 failures / 10 minutes).
func DefaultIdentityConfig() IdentityConfig {
	return IdentityConfig{FailureThreshold: 5, Window: 10 * time.Minute}
}

// IdentityDetector (L6) flags brute-force and impossible-travel patterns.
type IdentityDetector struct{ cfg IdentityConfig }

// NewIdentityDetector builds an L6 detector. A zero-value config uses defaults.
func NewIdentityDetector(cfg IdentityConfig) *IdentityDetector {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 5
	}
	if cfg.Window <= 0 {
		cfg.Window = 10 * time.Minute
	}
	return &IdentityDetector{cfg: cfg}
}

func (*IdentityDetector) Well() Well   { return WellIdentity }
func (*IdentityDetector) Name() string { return "identity-anomaly" }
func (*IdentityDetector) IsReal() bool { return false }

// Analyze groups events by user and emits findings for brute force (T1110) and
// impossible travel (T1078).
func (d *IdentityDetector) Analyze(_ context.Context, events []AuthEvent) ([]Finding, error) {
	byUser := make(map[string][]AuthEvent)
	for _, e := range events {
		byUser[e.User] = append(byUser[e.User], e)
	}
	out := make([]Finding, 0)
	for user, evs := range byUser {
		sort.SliceStable(evs, func(i, j int) bool { return evs[i].Timestamp.Before(evs[j].Timestamp) })
		if f, ok := d.bruteForce(user, evs); ok {
			out = append(out, f)
		}
		if f, ok := d.impossibleTravel(user, evs); ok {
			out = append(out, f)
		}
	}
	return out, nil
}

// bruteForce flags >= FailureThreshold failures inside any sliding Window.
func (d *IdentityDetector) bruteForce(user string, evs []AuthEvent) (Finding, bool) {
	fails := make([]time.Time, 0, len(evs))
	for _, e := range evs {
		if !e.Success {
			fails = append(fails, e.Timestamp)
		}
	}
	for i := range fails {
		count := 1
		for j := i + 1; j < len(fails); j++ {
			if fails[j].Sub(fails[i]) <= d.cfg.Window {
				count++
			}
		}
		if count >= d.cfg.FailureThreshold {
			return newFinding(WellIdentity, "T1110", user,
				fmt.Sprintf("%d failed logins for %s within %s", count, user, d.cfg.Window),
				intel.SeverityHigh,
				map[string]any{"user": user, "failures": count, "window": d.cfg.Window.String()}), true
		}
	}
	return Finding{}, false
}

// impossibleTravel flags two successful logins from different countries inside
// the correlation window (a proxy for credential theft / T1078).
func (d *IdentityDetector) impossibleTravel(user string, evs []AuthEvent) (Finding, bool) {
	var last *AuthEvent
	for i := range evs {
		e := evs[i]
		if !e.Success || e.Country == "" {
			continue
		}
		if last != nil && last.Country != e.Country && e.Timestamp.Sub(last.Timestamp) <= d.cfg.Window {
			return newFinding(WellIdentity, "T1078", user,
				fmt.Sprintf("impossible travel for %s: %s then %s within %s",
					user, last.Country, e.Country, d.cfg.Window),
				intel.SeverityCritical,
				map[string]any{
					"user": user, "from_country": last.Country, "to_country": e.Country,
					"from_ip": last.SourceIP, "to_ip": e.SourceIP,
				}), true
		}
		cur := e
		last = &cur
	}
	return Finding{}, false
}
