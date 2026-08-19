// Package main - CLI command tests for `cafctl monitor`.
// Tests the full developer journey: record → baseline → record regression → alerts → report.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMonitorCmd_Journey walks the core workflow: record normal values, pin baseline,
// record degraded values, verify CRITICAL alerts, compute drift in report.
func TestMonitorCmd_Journey(t *testing.T) {
	store := filepath.Join(t.TempDir(), "monitor-store")

	// Record initial observation
	record := newMonitorRecordCmd()
	outBuf := wireCmd(record)
	record.SetArgs([]string{
		"resnet50:1.1.0",
		"--latency-p50", "40", "--latency-p95", "120", "--latency-p99", "200",
		"--qps", "850",
		"--accuracy", "0.91", "--errors", "0.002",
		"--samples", "10000",
		"--store", store,
	})
	require.NoError(t, record.Execute())
	s := outBuf.String()
	assert.Contains(t, s, "resnet50:1.1.0", "should show model version")
	assert.Contains(t, s, "Registry:", "should show registry status")
	assert.Contains(t, s, "recorded", "success message required")

	// Set baseline from latest
	baseline := newMonitorBaselineCmd()
	baselineBuf := wireCmd(baseline)
	baseline.SetArgs([]string{"resnet50:1.1.0", "--store", store})
	require.NoError(t, baseline.Execute())
	b := baselineBuf.String()
	assert.Contains(t, b, "baseline", "should confirm baseline set")
	assert.Contains(t, b, "p95 120.00", "should show p95 from record")

	// Record degraded observation: latency doubled, accuracy dropped
	regressed := newMonitorRecordCmd()
	regressedBuf := wireCmd(regressed)
	regressed.SetArgs([]string{
		"resnet50:1.1.0",
		"--latency-p50", "80", "--latency-p95", "240", "--latency-p99", "480", // doubled
		"--qps", "400", // reduced by ~53%
		"--accuracy", "0.84", // -7pp (WARN)
		"--errors", "0.02", // +900% (CRITICAL)
		"--samples", "10000",
		"--store", store,
	})
	require.NoError(t, regressed.Execute())
	r := regressedBuf.String()
	assert.Contains(t, r, "performance observation signed")

	// Check alerts: should have CRITICAL latency and error-rate
	alerts := newMonitorAlertsCmd()
	alertsBuf := wireCmd(alerts)
	alerts.SetArgs([]string{"resnet50", "--store", store})
	require.NoError(t, alerts.Execute())
	a := alertsBuf.String()
	assert.Contains(t, a, "CRITICAL", "must show at least one CRITICAL alert")
	assert.Contains(t, a, "+100.0%", "latency p95 regression should be +100.0%")
	assert.Contains(t, a, "latency_p95_regression", "specific rule name required")
	assert.Contains(t, a, "error_rate", "error rate alert expected")

	// Report shows drift table with +100% latency and active alerts
	report := newMonitorReportCmd()
	reportBuf := wireCmd(report)
	report.SetArgs([]string{"resnet50", "--version", "1.1.0", "--store", store})
	require.NoError(t, report.Execute())
	rep := reportBuf.String()
	assert.Contains(t, rep, "DRIFT", "drift column header required")
	assert.Contains(t, rep, "+100.0%", "latency p95 drift exactly +100%")
	assert.Contains(t, rep, "CRITICAL", "report must show active alerts")
	assert.Contains(t, rep, "accuracy", "accuracy metric in table")
	assert.Contains(t, rep, "0.84", "degraded accuracy value")
}

// TestMonitorCmd_RegistryValidation verifies optional --registry flag behavior.
func TestMonitorCmd_RegistryValidation(t *testing.T) {
	modelRegDir := filepath.Join(t.TempDir(), "models")
	storeDir := filepath.Join(t.TempDir(), "monitor-store")

	// Register valid model version using existing helper
	artPath := filepath.Join(t.TempDir(), "weights.pt")
	require.NoError(t, os.WriteFile(artPath, []byte("weights"), 0o644))
	mustRegister(t, modelRegDir, "resnet50", "1.1.0", artPath)

	// Record with --registry verified
	record := newMonitorRecordCmd()
	buf := wireCmd(record)
	record.SetArgs([]string{
		"resnet50:1.1.0",
		"--latency-p50", "40", "--latency-p95", "120", "--latency-p99", "200",
		"--accuracy", "0.91", "--errors", "0.002",
		"--store", storeDir,
		"--registry", modelRegDir,
	})
	require.NoError(t, record.Execute())
	s := buf.String()
	assert.Contains(t, s, "verified", "valid version should show verification success")
	assert.Contains(t, s, "resnet50:1.1.0 registered", "registry confirmation message required")

	// Record invalid version with --registry fails
	badRecord := newMonitorRecordCmd()
	errBuf := wireCmd(badRecord)
	badRecord.SetArgs([]string{
		"resnet50:9.9.9",
		"--latency-p50", "40", "--latency-p95", "120", "--latency-p99", "200",
		"--accuracy", "0.91", "--errors", "0.002",
		"--store", storeDir,
		"--registry", modelRegDir,
	})
	err := badRecord.Execute()
	require.Error(t, err, "unregistered version must fail when --registry provided")
	assert.Contains(t, err.Error(), "no version", "should mention missing version")
	assert.Contains(t, errBuf.String(), "no version", "stderr must contain error detail")

	// Without --registry, any version accepted
	noRegRecord := newMonitorRecordCmd()
	noRegBuf := wireCmd(noRegRecord)
	noRegRecord.SetArgs([]string{
		"ghost-model:1.2.3",
		"--latency-p50", "40", "--latency-p95", "120", "--latency-p99", "200",
		"--accuracy", "0.91", "--errors", "0.002",
		"--store", storeDir,
	})
	require.NoError(t, noRegRecord.Execute())
	nr := noRegBuf.String()
	assert.Contains(t, nr, "skipped", "no registry check means skipped status")
	assert.Contains(t, nr, "recorded", "successfully recorded")
}

// TestMonitorCmd_JSONOutput verifies machine-readable JSON for CI pipelines.
func TestMonitorCmd_JSONOutput(t *testing.T) {
	store := filepath.Join(t.TempDir(), "monitor-store")

	// Record with JSON output
	record := newMonitorRecordCmd()
	buf := wireCmd(record)
	record.SetArgs([]string{
		"jsonmodel:1.0.0",
		"--latency-p50", "40", "--latency-p95", "120", "--latency-p99", "200",
		"--qps", "850",
		"--accuracy", "0.91", "--errors", "0.002",
		"--samples", "10000",
		"--store", store,
		"--output", "json",
	})
	require.NoError(t, record.Execute())

	var result map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result), "output must be valid JSON")
	assert.Equal(t, "jsonmodel:1.0.0", result["model_version"])
	assert.InDelta(t, 120.0, result["latency_p95_ms"], 0.001)
	assert.InDelta(t, 0.91, result["accuracy"], 0.001)
	assert.NotEmpty(t, result["attestation_hash"], "attestation hash always present")
	assert.Equal(t, true, result["recorded"])
}

// TestMonitorCmd_NoBaselineHandling covers graceful degradation when baseline not set.
func TestMonitorCmd_NoBaselineHandling(t *testing.T) {
	store := filepath.Join(t.TempDir(), "monitor-store")

	// Record without setting baseline first
	record := newMonitorRecordCmd()
	wireCmd(record)
	record.SetArgs([]string{"nobase:1.0.0", "--latency-p50", "40", "--latency-p95", "120", "--latency-p99", "200", "--store", store})
	require.NoError(t, record.Execute())

	// Alerts without baseline should show INFO, not error exit
	alerts := newMonitorAlertsCmd()
	alertsBuf := wireCmd(alerts)
	alerts.SetArgs([]string{"nobase", "--store", store})
	require.NoError(t, alerts.Execute())
	s := alertsBuf.String()
	assert.Contains(t, s, "no baseline", "informative message required")
	assert.Contains(t, s, "cafctl monitor baseline", "helpful hint provided")

	// Report without baseline shows empty drift column
	report := newMonitorReportCmd()
	reportBuf := wireCmd(report)
	report.SetArgs([]string{"nobase", "--store", store})
	require.NoError(t, report.Execute())
	r := reportBuf.String()
	assert.Contains(t, r, "not set", "should indicate baseline unavailable")
	assert.Contains(t, r, "drift computation unavailable", "user-facing explanation")
}

// TestMonitorCmd_Variants tests different usage patterns and edge cases.
func TestMonitorCmd_Variants(t *testing.T) {
	store := filepath.Join(t.TempDir(), "monitor-store")

	// Test multiple versions of same model (valid semver)
	for i := range 3 {
		version := fmt.Sprintf("1.%d.0", i)
		rec := newMonitorRecordCmd()
		wireCmd(rec)
		rec.SetArgs([]string{
			fmt.Sprintf("variant:%s", version),
			"--latency-p50", fmt.Sprint(i*10+40),
			"--latency-p95", fmt.Sprint(i*10+120),
			"--latency-p99", "200",
			"--accuracy", "0.9", "--errors", "0.001",
			"--store", store,
		})
		require.NoError(t, rec.Execute(), "record variant:%s must succeed", version)
	}

	// Use --latest to automatically select newest
	reportLatest := newMonitorReportCmd()
	latestBuf := wireCmd(reportLatest)
	reportLatest.SetArgs([]string{"variant", "--latest", "--store", store})
	require.NoError(t, reportLatest.Execute())
	l := latestBuf.String()
	assert.Contains(t, l, "variant:1.2.0", "latest should resolve to newest version")

	// Explicit version override still works
	reportVersion := newMonitorReportCmd()
	explicitBuf := wireCmd(reportVersion)
	reportVersion.SetArgs([]string{"variant", "--version", "1.0.0", "--store", store})
	require.NoError(t, reportVersion.Execute())
	e := explicitBuf.String()
	assert.Contains(t, e, "1.0.0", "explicit version override works")
}
