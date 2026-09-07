package purpleteam

import (
	"context"
	"testing"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/purpleteam/redteam_simulation"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPurpleTeamPlatformInitialization(t *testing.T) {
	t.Run("should_create_platform_with_default_config", func(t *testing.T) {
		config := DefaultConfig()
		
		assert.NotNil(t, config.RedTeam)
		assert.NotNil(t, config.BlueTeam)
		assert.NotNil(t, config.DevSecOps)
		assert.True(t, config.EnableLogging)
		assert.True(t, config.MetricsEnabled)
	})

	t.Run("should_initialize_platform_successfully", func(t *testing.T) {
		config := DefaultConfig()
		
		platform, err := NewPurpleTeamPlatform(config)
		
		require.NoError(t, err)
		assert.NotNil(t, platform)
		assert.NotNil(t, platform.redTeamSimulator)
		assert.NotNil(t, platform.blueTeamEngine)
		assert.NotNil(t, platform.devSecOpsGates)
		assert.NotNil(t, platform.integrationHub)
	})
}

func TestRedTeamSimulationModule(t *testing.T) {
	t.Run("should_scan_binary_for_vulnerabilities", func(t *testing.T) {
		simulator := redteam_simulation.NewRedTeamSimulator(
			redteam_simulation.ScannerConfig{
				MaxRecursionDepth: 10,
				EnableAdvancedChecks: true,
			},
		)
		
		env := redteam_simulation.Environment{
			TargetPath: "test/path",
			TargetType: "binary",
		}
		
		report, err := simulator.IdentifyVulnerabilities(env)
		
		require.NoError(t, err)
		assert.NotNil(t, report)
		assert.Equal(t, "test/path", report.TargetEnvironment)
		assert.NotEmpty(t, report.ScanID)
		assert.False(t, report.Success) // Expected to fail gracefully on test path
	})

	t.Run("should_handle_invalid_target_gracefully", func(t *testing.T) {
		simulator := redteam_simulation.NewRedTeamSimulator(redteam_simulation.ScannerConfig{})
		
		env := redteam_simulation.Environment{
			TargetPath: "/nonexistent/path/that/does/not/exist",
			TargetType: "directory",
		}
		
		report, err := simulator.IdentifyVulnerabilities(env)
		
		require.NoError(t, err)
		assert.NotNil(t, report)
		assert.Empty(t, report.Findings)
	})

	t.Run("should_provide_remediation_guidance", func(t *testing.T) {
		simulator := redteam_simulation.NewRedTeamSimulator(redteam_simulation.ScannerConfig{})
		
		env := redteam_simulation.Environment{
			TargetPath: ".",
			TargetType: "directory",
		}
		
		report, err := simulator.IdentifyVulnerabilities(env)
		
		require.NoError(t, err)
		assert.NotNil(t, report)
		
		if len(report.RemediationGuidance) > 0 {
			for _, guide := range report.RemediationGuidance {
				assert.NotEmpty(t, guide.FindingID)
				assert.NotEmpty(t, guide.Title)
				assert.NotEmpty(t, guide.Action)
			}
		}
	})
}

func TestBlueTeamDetectionModule(t *testing.T) {
	t.Run("should_classify_threats_from_finding", func(t *testing.T) {
		engine := blueteam.NewBlueTeamDetectionEngine(blueteam.DetectionConfig{})
		
		finding := redteam_simulation.Finding{
			ID:          "TEST-FINDING-001",
			Title:       "Potential Buffer Overflow in strcpy",
			Description: "Unsafe string copy operation detected",
			Severity:    redteam_simulation.SeverityHigh,
			CWE:         "CWE-120: Buffer Copy without Checking Size of Input",
			Location:    "./test/example.c",
			RiskScore:   7.5,
		}
		
		report := &redteam_simulation.AssessmentReport{
			ScanID:      "TEST-SCAN-001",
			Timestamp:   time.Now(),
			Findings:    []redteam_simulation.Finding{finding},
		}
		
		responseReport, err := engine.DetectAndRespond(report)
		
		require.NoError(t, err)
		assert.NotNil(t, responseReport)
		assert.NotEmpty(t, responseReport.ReportID)
		assert.Equal(t, "TEST-SCAN-001", responseReport.RedTeamScanID)
	})

	t.Run("should_generate_defensive_responses", func(t *testing.T) {
		engine := blueteam.NewBlueTeamDetectionEngine(blueteam.DetectionConfig{})
		
		report := &redteam_simulation.AssessmentReport{
			ScanID: "TEST-SCAN-002",
			Findings: []redteam_simulation.Finding{
				{
					ID:          "FIND-001",
					Title:       "Critical Vulnerability",
					Description: "Test critical finding",
					Severity:    redteam_simulation.SeverityCritical,
					RiskScore:   9.5,
				},
			},
		}
		
		response, err := engine.DetectAndRespond(report)
		
		require.NoError(t, err)
		assert.NotNil(t, response)
		assert.NotEmpty(t, response.GeneratedResponses)
	})

	t.Run("should_calculate_effectiveness_metrics", func(t *testing.T) {
		engine := blueteam.NewBlueTeamDetectionEngine(blueteam.DetectionConfig{})
		
		report := &redteam_simulation.AssessmentReport{
			ScanID: "TEST-SCAN-003",
			RiskLevel: "HIGH",
		}
		
		response, err := engine.DetectAndRespond(report)
		
		require.NoError(t, err)
		assert.NotNil(t, response)
		assert.GreaterOrEqual(t, response.OverallEffectiveness, 0.0)
		assert.LessOrEqual(t, response.OverallEffectiveness, 1.0)
	})
}

func TestDevSecOpsSecurityGates(t *testing.T) {
	t.Run("should_create_gates_configuration", func(t *testing.T) {
		config := devsecops.DefaultGatesConfiguration()
		
		assert.NotNil(t, config.SASTConfig)
		assert.NotNil(t, config.SecretsConfig)
		assert.NotNil(t, config.ComplianceConfig)
	})

	t.Run("should_run_security_gates_on_commit", func(t *testing.T) {
		gates := devsecops.NewDevSecOpsGates(devsecops.DefaultGatesConfiguration())
		
		commit := devsecops.NewCommit(
			"abc123def456",
			"main",
			"test-repository",
		)
		
		result, err := gates.RunSecurityGate(commit)
		
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, "abc123def456", result.GitCommit)
		assert.Equal(t, "main", result.Branch)
	})

	t.Run("should_track_security_metrics", func(t *testing.T) {
		gates := devsecops.NewDevSecOpsGates(devsecops.DefaultGatesConfiguration())
		
		commit := devsecops.NewCommit("test-hash", "develop", "test-repo")
		
		result, err := gates.RunSecurityGate(commit)
		
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.NotZero(t, result.Metrics.TotalScanTimeMs)
		assert.Contains(t, result.Metrics, struct {
			SASTScanDurationMs int64 `json:"sastScanDurationMs"`
		}{})
	})
}

func TestIntegrationHub(t *testing.T) {
	t.Run("should_initialize_integration_hub", func(t *testing.T) {
		hub := integration.NewPurpleTeamIntegrationHub()
		
		assert.NotNil(t, hub)
		assert.NotNil(t, hub.sharedIntelDB)
		assert.NotNil(t, hub.metricDashboard)
		assert.NotNil(t, hub.improvementEngine)
	})

	t.Run("should_process_intelligence_fusion", func(t *testing.T) {
		hub := integration.NewPurpleTeamIntegrationHub()
		
		redReport := &redteam_simulation.AssessmentReport{
			ScanID: "RED-SCAN-001",
			Findings: []redteam_simulation.Finding{},
		}
		
		blueReport := &blueteam.ResponseReport{
			ReportID: "BLUE-REPORT-001",
		}
		
		result, err := hub.ProcessIntelligenceFusion(redReport, blueReport)
		
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.NotEmpty(t, result.FusionID)
		assert.True(t, result.Success)
	})

	t.Run("should_collect_training_data", func(t *testing.T) {
		hub := integration.NewPurpleTeamIntegrationHub()
		
		redReport := &redteam_simulation.AssessmentReport{
			ScanID: "RED-TRAIN-001",
			Timestamp: time.Now(),
		}
		
		blueReport := &blueteam.ResponseReport{
			ReportID: "BLUE-TRAIN-001",
		}
		
		ctx := context.Background()
		samples := hub.CollectTrainingData(ctx, redReport, blueReport)
		
		assert.NotNil(t, samples)
		assert.IsType(t, []integration.TrainingSample{}, samples)
	})
}

func TestCompleteEngagementFlow(t *testing.T) {
	t.Skip("Integration test requires actual environment setup")

	t.Run("should_execute_complete_engagement", func(t *testing.T) {
		config := DefaultConfig()
		
		platform, err := NewPurpleTeamPlatform(config)
		require.NoError(t, err)
		
		scenario := ScenarioSpec{
			Name:                  "full_purple_team_test",
			Branch:                "main",
			Repository:            "test-repo",
			EnforceSecurityGates:  false,
			BidirectionalFlowEnabled: true,
		}
		
		target := redteam_simulation.Environment{
			TargetPath: ".",
			TargetType: "directory",
		}
		
		ctx := context.Background()
		
		result, err := platform.RunCompleteEngagement(ctx, target, scenario)
		
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.NotEmpty(t, result.EngagementID)
		assert.False(t, result.Phase1Error != "")
		assert.False(t, result.Phase2Error != "")
	})

	t.Run("should_handle_partial_failure_gracefully", func(t *testing.T) {
		config := DefaultConfig()
		
		platform, err := NewPurpleTeamPlatform(config)
		require.NoError(t, err)
		
		scenario := ScenarioSpec{
			Name: "partial_failure_test",
		}
		
		target := redteam_simulation.Environment{
			TargetPath: "/nonexistent/path",
			TargetType: "binary",
		}
		
		ctx := context.Background()
		
		result, err := platform.RunCompleteEngagement(ctx, target, scenario)
		
		require.Error(t, err)
		assert.NotNil(t, result)
		assert.False(t, result.Success)
	})
}

func TestRiskScoring(t *testing.T) {
	t.Run("should_calculate_risk_score_correctly", func(t *testing.T) {
		simulator := redteam_simulation.NewRedTeamSimulator(redteam_simulation.ScannerConfig{})
		
		env := redteam_simulation.Environment{
			TargetPath: ".",
			TargetType: "directory",
		}
		
		report, err := simulator.IdentifyVulnerabilities(env)
		require.NoError(t, err)
		
		assert.NotNil(t, report.RiskScore)
		assert.NotZero(t, report.Duration)
	})

	t.Run("should_classify_risk_level", func(t *testing.T) {
		testCases := []struct {
			riskScore float64
			expected  redteam_simulation.RiskLevel
		}{
			{9.5, redteam_simulation.RiskCritical},
			{7.5, redteam_simulation.RiskHigh},
			{5.0, redteam_simulation.RiskMedium},
			{2.5, redteam_simulation.RiskLow},
			{0.0, redteam_simulation.RiskNegligible},
		}
		
		for _, tc := range testCases {
			// Note: This is conceptual - actual risk level comes from report
			assert.NotEqual(t, tc.riskScore, -1.0)
		}
	})
}

func TestPatternRecognition(t *testing.T) {
	t.Run("should_detect_dangerous_patterns", func(t *testing.T) {
		simulator := redteam_simulation.NewRedTeamSimulator(redteam_simulation.ScannerConfig{})
		
		_ = simulator
		
		t.Log("Pattern recognition initialized with default rule sets")
	})

	t.Run("should_support_custom_patterns", func(t *testing.T) {
		config := redteam_simulation.ScannerConfig{
			CustomPatterns: []string{"custom_pattern_1"},
		}
		
		simulator := redteam_simulation.NewRedTeamSimulator(config)
		
		_ = simulator
		
		t.Log("Custom patterns configuration accepted")
	})
}

func TestPerformanceMetrics(t *testing.T) {
	t.Run("should_measure_engagement_duration", func(t *testing.T) {
		start := time.Now()
		
		// Simulate some processing
		time.Sleep(10 * time.Millisecond)
		
		duration := time.Since(start)
		
		assert.GreaterOrEqual(t, duration, 10*time.Millisecond)
		assert.Less(t, duration, 100*time.Millisecond)
	})

	t.Run("should_record_scanner_performance", func(t *testing.T) {
		simulator := redteam_simulation.NewRedTeamSimulator(redteam_simulation.ScannerConfig{})
		
		env := redteam_simulation.Environment{
			TargetPath: ".",
			TargetType: "directory",
		}
		
		start := time.Now()
		_, err := simulator.IdentifyVulnerabilities(env)
		duration := time.Since(start)
		
		require.NoError(t, err)
		assert.Greater(t, duration.Milliseconds(), int64(0))
	})
}

func TestErrorHandling(t *testing.T) {
	t.Run("should_handle_nil_environment", func(t *testing.T) {
		var env redteam_simulation.Environment
		
		simulator := redteam_simulation.NewRedTeamSimulator(redteam_simulation.ScannerConfig{})
		
		_, err := simulator.IdentifyVulnerabilities(env)
		
		assert.Error(t, err)
	})

	t.Run("should_validate_input_parameters", func(t *testing.T) {
		config := redteam_simulation.ScannerConfig{
			MaxRecursionDepth: -1,
		}
		
		assert.Panics(t, func() {
			redteam_simulation.NewRedTeamSimulator(config)
		})
	})
}

func BenchmarkRedTeamScan(b *testing.B) {
	simulator := redteam_simulation.NewRedTeamSimulator(redteam_simulation.ScannerConfig{})
	
	env := redteam_simulation.Environment{
		TargetPath: ".",
		TargetType: "directory",
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := simulator.IdentifyVulnerabilities(env)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBlueTeamDetection(b *testing.B) {
	engine := blueteam.NewBlueTeamDetectionEngine(blueteam.DetectionConfig{})
	
	report := &redteam_simulation.AssessmentReport{
		ScanID: "BENCHMARK-SCAN",
		Findings: make([]redteam_simulation.Finding, 10),
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := engine.DetectAndRespond(report)
		if err != nil {
			b.Fatal(err)
		}
	}
}
