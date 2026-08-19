// Package modelmonitor - Unit tests for performance monitoring and alert evaluation.
package modelmonitor

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
	"github.com/cloudai-fusion/cloudai-fusion/pkg/modelregistry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestMonitor(t *testing.T, dir string) (*FSMonitor, func()) {
	t.Helper()
	mon, err := NewFSMonitor(dir, nil, nil)
	require.NoError(t, err, "must create monitor")
	return mon, func() {}
}

func newTestLedger(t *testing.T) *evidence.Ledger {
	signer, err := evidence.GenerateEphemeralSigner()
	require.NoError(t, err, "generate signer")
	store := evidence.NewMemoryStore()
	ledger, err := evidence.NewLedger(evidence.LedgerConfig{Store: store, Signer: signer, Anchorer: evidence.NewSimulatedAnchorer()})
	require.NoError(t, err, "build ledger")
	return ledger
}

func mkRec(ref string, p50, p95, p99, qps, acc, errRate float64, samples int, ts time.Time) PerformanceRecord {
	if ts.IsZero() {
		ts = time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	}
	return PerformanceRecord{
		ModelVersion:  ref,
		Timestamp:     ts,
		LatencyP50MS:  p50,
		LatencyP95MS:  p95,
		LatencyP99MS:  p99,
		ThroughputQPS: qps,
		Accuracy:      acc,
		ErrorRate:     errRate,
		SampleCount:   samples,
	}
}

// TestRecord_Persists_JSONL verifies record appends to JSONL with valid structure.
func TestRecord_Persists_JSONL(t *testing.T) {
	dir := t.TempDir()
	ledger := newTestLedger(t)
	mon, _ := NewFSMonitor(dir, ledger, nil)

	rec1 := mkRec("resnet50:1.1.0", 40, 120, 200, 850, 0.91, 0.002, 10000, time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC))
	require.NoError(t, mon.Record(context.Background(), rec1), "first record must succeed")

	rec2 := mkRec("resnet50:1.1.0", 44, 130, 220, 870, 0.92, 0.002, 10500, time.Time{}) // zero Timestamp → auto-fill
	require.NoError(t, mon.Record(context.Background(), rec2), "second record must succeed")

	// Verify file exists with 2 lines
	path := filepath.Join(dir, "resnet50_1.1.0.jsonl")
	assert.FileExists(t, path)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(string(data), "\n"), "should have 2 JSON lines (newline-terminated)")

	// Parse each line as valid JSON
	var records []PerformanceRecord
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var r PerformanceRecord
		require.NoError(t, json.Unmarshal([]byte(line), &r), "each line must be valid JSON")
		assert.Equal(t, "resnet50:1.1.0", r.ModelVersion)
		assert.GreaterOrEqual(t, r.Timestamp.Unix(), int64(1758825600)) // after Unix epoch
		records = append(records, r)
	}
	assert.Len(t, records, 2, "must parse exactly 2 records")

	// Verify attestation signed
	assert.NotNil(t, mon.LastAttestation(), "attestation must be present")
	assert.Equal(t, "monitor.record", mon.LastAttestation().Action)
	assert.NotEmpty(t, mon.LastAttestation().Hash)
}

// TestBaseline_Set_And_Report verifies baseline pinning and drift calculation.
func TestBaseline_Set_And_Report(t *testing.T) {
	dir := t.TempDir()
	mon, _ := NewFSMonitor(dir, nil, nil)

	// Baseline record: timestamp T0
	baseTs := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	base := mkRec("resnet50:1.1.0", 40, 100, 200, 1000, 0.90, 0.010, 10000, baseTs)
	require.NoError(t, mon.Record(context.Background(), base))

	// Another version's baseline for latestAcrossVersions test
	oldTs := time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC) // earlier
	oldRec := mkRec("resnet50:1.0.0", 40, 90, 180, 900, 0.88, 0.012, 9000, oldTs)
	require.NoError(t, mon.Record(context.Background(), oldRec))

	require.NoError(t, mon.SetBaseline(context.Background(), "resnet50:1.1.0"), "baseline must set")

	// Second observation: improved accuracy (+3pp), degraded latency (+10%)
	latestTs := time.Date(2026, 8, 16, 11, 0, 0, 0, time.UTC)
	latest := mkRec("resnet50:1.1.0", 44, 110, 220, 1200, 0.93, 0.008, 11000, latestTs)
	require.NoError(t, mon.Record(context.Background(), latest))

	rep, err := mon.Report(context.Background(), "resnet50", "1.1.0")
	require.NoError(t, err)
	assert.NotNil(t, rep.Baseline)
	assert.NotNil(t, rep.Latest)
	assert.Equal(t, 100.0, rep.Baseline.LatencyP95MS, "baseline p95 must equal 100")
	assert.Equal(t, 110.0, rep.Latest.LatencyP95MS, "latest p95 must equal 110")

	// Drift: +10% latency, +20% throughput, +3pp accuracy, -20% error-rate
	drift := rep.Drift
	assert.InDelta(t, 10.0, drift[MetricLatencyP50], 0.001)
	assert.InDelta(t, 10.0, drift[MetricLatencyP95], 0.001)
	assert.InDelta(t, 10.0, drift[MetricLatencyP99], 0.001)
	assert.InDelta(t, 20.0, drift[MetricThroughput], 0.001)
	assert.InDelta(t, 3.0, drift[MetricAccuracy], 0.001)
	assert.InDelta(t, -20.0, drift[MetricErrorRate], 0.001)

	// Trend should contain this version's records only (oldest first)
	assert.Len(t, rep.Trend, 2)
	assert.InDelta(t, 100.0, rep.Trend[0].LatencyP95MS, 0.001, "trend[0] should be the first 1.1.0 record")
	assert.Equal(t, "resnet50:1.1.0", rep.Trend[0].ModelVersion)

	// baselines.json exists
	baselinesPath := filepath.Join(dir, "baselines.json")
	assert.FileExists(t, baselinesPath)
	baselinesData, err := os.ReadFile(baselinesPath)
	require.NoError(t, err)
	assert.Contains(t, string(baselinesData), `"resnet50:1.1.0"`)

	// Report latest across versions selects most recent
	repLatest, err := mon.Report(context.Background(), "resnet50", "")
	require.NoError(t, err)
	assert.Equal(t, "1.1.0", repLatest.Version, "should pick newest version")
	assert.Equal(t, 110.0, repLatest.Latest.LatencyP95MS)
}

// TestDrift_Alert_Triggers evaluates all default rules against various regression scenarios.
func TestDrift_Alert_Triggers(t *testing.T) {
	ctx := context.Background()
	
	cases := []struct {
		name     string
		ref      string
		base     PerformanceRecord
		latest   PerformanceRecord
		wantRule string
		wantSev  AlertSeverity
		wantPct  float64
	}{
		{"latency p95 +60% is CRITICAL", "m1:1.0.0",
			mkRec("m1:1.0.0", 40, 100, 200, 1000, 0.90, 0.001, 10000, time.Time{}),
			mkRec("m1:1.0.0", 60, 160, 320, 1000, 0.90, 0.001, 10000, time.Time{}),
			"latency_p95_regression", SeverityCritical, 60},
		{"latency p95 +30% is WARN", "m1:1.0.0",
			mkRec("m1:1.0.0", 40, 100, 200, 1000, 0.90, 0.001, 10000, time.Time{}),
			mkRec("m1:1.0.0", 40, 130, 260, 1000, 0.90, 0.001, 10000, time.Time{}),
			"latency_p95_regression", SeverityWarn, 30},
		{"accuracy -12pp is CRITICAL", "m2:1.0.0",
			mkRec("m2:1.0.0", 40, 100, 200, 1000, 0.90, 0.001, 10000, time.Time{}),
			mkRec("m2:1.0.0", 40, 100, 200, 1000, 0.78, 0.001, 10000, time.Time{}),
			"accuracy_regression", SeverityCritical, 12},
		{"accuracy -6pp is WARN", "m2:1.0.0",
			mkRec("m2:1.0.0", 40, 100, 200, 1000, 0.90, 0.001, 10000, time.Time{}),
			mkRec("m2:1.0.0", 40, 100, 200, 1000, 0.84, 0.001, 10000, time.Time{}),
			"accuracy_regression", SeverityWarn, 6},
		{"error_rate +100% is CRITICAL", "m3:1.0.0",
			mkRec("m3:1.0.0", 40, 100, 200, 1000, 0.90, 0.001, 10000, time.Time{}),
			mkRec("m3:1.0.0", 40, 100, 200, 1000, 0.90, 0.002, 10000, time.Time{}),
			"error_rate_regression", SeverityCritical, 100},
		{"throughput -60% is CRITICAL", "m4:1.0.0",
			mkRec("m4:1.0.0", 40, 100, 200, 1000, 0.90, 0.001, 10000, time.Time{}),
			mkRec("m4:1.0.0", 40, 100, 200, 400, 0.90, 0.001, 10000, time.Time{}),
			"throughput_regression", SeverityCritical, 60},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			mon, _ := NewFSMonitor(dir, nil, nil)

			require.NoError(t, mon.Record(ctx, c.base))
			require.NoError(t, mon.SetBaseline(ctx, c.base.ModelVersion))
			require.NoError(t, mon.Record(ctx, c.latest))

			modelName, _, _ := ParseModelVersion(c.ref)
			alerts, err := mon.Alerts(ctx, modelName)
			require.NoError(t, err)

			var found *Alert
			for i := range alerts {
				if alerts[i].Rule == c.wantRule {
					found = &alerts[i]
					break
				}
			}
			require.NotNil(t, found, "alert rule %q must be triggered", c.wantRule)
			assert.Equal(t, c.wantSev, found.Severity, "severity mismatch")
			
			if math.IsInf(c.wantPct, 1) {
				assert.True(t, math.IsInf(found.RegressionPct, 1), "infinite regression expected")
			} else {
				assert.InDelta(t, c.wantPct, found.RegressionPct, 0.001, "regression percentage mismatch")
			}
		})
	}

	// Subtest: infinite regression from zero baseline
	t.Run("error_rate from zero baseline is CRITICAL +Inf", func(t *testing.T) {
		dir := t.TempDir()
		mon, _ := NewFSMonitor(dir, nil, nil)
		base := mkRec("zero:1.0.0", 40, 100, 200, 1000, 0.90, 0.0, 10000, time.Time{})
		latest := mkRec("zero:1.0.0", 40, 100, 200, 1000, 0.90, 0.01, 10000, time.Time{})
		
		require.NoError(t, mon.Record(ctx, base))
		require.NoError(t, mon.SetBaseline(ctx, base.ModelVersion))
		require.NoError(t, mon.Record(ctx, latest))

		alerts, err := mon.Alerts(ctx, "zero")
		require.NoError(t, err)

		var errAlert Alert
		for i := range alerts {
			if alerts[i].Rule == "error_rate_regression" {
				errAlert = alerts[i]
				break
			}
		}
		require.NotNil(t, errAlert)
		assert.True(t, math.IsInf(errAlert.RegressionPct, 1), "expected infinite regression from zero")
		assert.Contains(t, errAlert.Message, "infinite", "message should mention infinite")
	})
}

// TestNoDrift_NoAlert verifies metrics within thresholds produce no alerts.
func TestNoDrift_NoAlert(t *testing.T) {
	dir := t.TempDir()
	mon, _ := NewFSMonitor(dir, nil, nil)

	base := mkRec("stable:1.0.0", 40, 100, 200, 1000, 0.90, 0.010, 10000, time.Time{})
	latest := mkRec("stable:1.0.0", 42, 115, 230, 950, 0.892, 0.011, 10100, time.Time{})
	
	require.NoError(t, mon.Record(context.Background(), base))
	require.NoError(t, mon.SetBaseline(context.Background(), "stable:1.0.0"))
	require.NoError(t, mon.Record(context.Background(), latest))

	alerts, err := mon.Alerts(context.Background(), "stable")
	require.NoError(t, err)
	assert.Empty(t, alerts, "small variations below threshold should not trigger alerts")

	rep, _ := mon.Report(context.Background(), "stable", "1.0.0")
	assert.Empty(t, rep.ActiveAlerts)
}

// TestRegistry_Validation verifies optional registry check on Record/Report.
func TestRegistry_Validation(t *testing.T) {
	// Create real model registry
	regDir := t.TempDir()
	regArtifact := filepath.Join(t.TempDir(), "weights.pt")
	require.NoError(t, os.WriteFile(regArtifact, []byte("weights"), 0o644))
	reg, err := modelregistry.NewFSRegistry(regDir, nil)
	require.NoError(t, err)
	_, err = reg.Register(context.Background(), modelregistry.RegisterInput{
		Name:       "resnet50", Version: "1.0.0", ArtifactPath: regArtifact,
		DatasetRef: "sha256:ds1", CodeRef: "git:abc", CreatedBy: "test"})
	require.NoError(t, err, "register base model")

	// Monitor with registry checker
	monDir := t.TempDir()
	mon, _ := NewFSMonitor(monDir, nil, reg)

	rec := PerformanceRecord{
		ModelVersion:  "resnet50:1.0.0",
		LatencyP50MS:  40,
		LatencyP95MS:  100,
		LatencyP99MS:  200,
		ThroughputQPS: 1000,
		Accuracy:      0.90,
		ErrorRate:     0.01,
		SampleCount:   1000,
		Timestamp:     time.Now().UTC(),
	}

	// Valid version succeeds
	require.NoError(t, mon.Record(context.Background(), rec), "valid version must pass registry check")

	// Invalid version fails
	err = mon.Record(context.Background(), mkRec("resnet50:9.9.9", 100, 100, 200, 1000, 0.90, 0.01, 1000, time.Time{}))
	require.Error(t, err, "unregistered version must fail")
	assert.Contains(t, err.Error(), "no version", "error should mention missing version")

	// Unregistered version passes when registry disabled
	monNoReg, _ := NewFSMonitor(t.TempDir(), nil, nil)
	require.NoError(t, monNoReg.Record(context.Background(), mkRec("ghost:1.2.3", 100, 100, 200, 1000, 0.90, 0.01, 1000, time.Time{})), "no-registry mode allows any version")

	// Report with registry validation fails for unregistered version
	_, err = mon.Report(context.Background(), "resnet50", "8.8.8")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "registry check failed", "report should report registry validation failure")
}

// TestPathSanitization ensures model version cannot escape monitor directory.
func TestPathSanitization(t *testing.T) {
	dir := t.TempDir()
	mon, _ := NewFSMonitor(dir, nil, nil)

	// Reject path traversal attempts in name
	err := mon.Record(context.Background(), mkRec("../evil:1.0.0", 100, 100, 200, 1000, 0.90, 0.01, 1000, time.Time{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid model name", "name ../evil should be rejected")

	// Reject path traversal in version
	err = mon.Record(context.Background(), mkRec("resnet50:../evil", 100, 100, 200, 1000, 0.90, 0.01, 1000, time.Time{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid semantic version", "../evil is not semver")

	// Backslash also invalid
	err = mon.Record(context.Background(), mkRec("resnet50:1.0.0\\..\\..", 100, 100, 200, 1000, 0.90, 0.01, 1000, time.Time{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid semantic version")

	// Normal version creates only expected file
	require.NoError(t, mon.Record(context.Background(), mkRec("resnet50:1.1.0", 100, 100, 200, 1000, 0.90, 0.01, 1000, time.Time{})))
	entries, _ := os.ReadDir(dir)
	fileNames := make([]string, len(entries))
	for i, e := range entries {
		fileNames[i] = e.Name()
	}
	assert.Len(t, fileNames, 1, "only one .jsonl file created")
	assert.Contains(t, fileNames[0], "resnet50_1.1.0.jsonl")
	assert.False(t, strings.HasPrefix(fileNames[0], ".."), "filename should not start with ..")

	// No subdirectories created outside root
	subdirs := 0
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() && path != dir {
			subdirs++
		}
		return nil
	})
	assert.Zero(t, subdirs, "no extra directories created")
}
