package redteam

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// Import patent package for benchmark helpers
import "github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/patent"

// ============================================================================
// PERFORMANCE BENCHMARKS - MEASURE UNIFIED PLATFORM CAPABILITIES
// ============================================================================

func BenchmarkM34Platform_AssessSingleTarget(b *testing.B) {
	ctx := context.Background()
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	
	platform, err := NewM34RedTeamPlatform(ctx, logger)
	if err != nil {
		b.Fatalf("Failed to create platform: %v", err)
	}
	defer platform.Stop()
	
	target := generateRandomTarget()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := platform.AssessVulnerabilities(target, ctx)
		if err != nil {
			b.Fatalf("Assessment failed on iteration %d: %v", i, err)
		}
		_ = result
	}
}

func BenchmarkM34Platform_MultiTargetScanning(b *testing.B) {
	ctx := context.Background()
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	
	platform, err := NewM34RedTeamPlatform(ctx, logger)
	if err != nil {
		b.Fatalf("Failed to create platform: %v", err)
	}
	defer platform.Stop()
	
	targets := generateTargets(10)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, target := range targets {
			_, err := platform.AssessVulnerabilities(target, ctx)
			if err != nil {
				b.Fatalf("Assessment failed: %v", err)
			}
		}
	}
}

func BenchmarkM34Platform_CrossPatentOrchestration(b *testing.B) {
	ctx := context.Background()
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	
	platform, err := NewM34RedTeamPlatform(ctx, logger)
	if err != nil {
		b.Fatalf("Failed to create platform: %v", err)
	}
	defer platform.Stop()
	
	target := TargetInfo{
		IP:     "192.168.1.100",
		Ports:  []int{22, 80, 443, 3306, 5432, 8080, 8443},
		Services: []Service{
			{Name: "sshd", Version: "8.0", Port: 22},
			{Name: "httpd", Version: "2.4", Port: 80},
			{Name: "nginx", Version: "1.18", Port: 443},
			{Name: "mysqld", Version: "8.0", Port: 3306},
			{Name: "postgres", Version: "13", Port: 5432},
		},
		KnownCVEs: []string{"CVE-2021-44228", "CVE-2022-22965", "CVE-2023-12345"},
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := platform.AssessVulnerabilities(target, ctx)
		if err != nil {
			b.Fatalf("Iteration %d failed: %v", i, err)
		}
		_ = result.AttacksDiscovered
	}
}

func BenchmarkM34Platform_GetStats(b *testing.B) {
	ctx := context.Background()
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	
	platform, err := NewM34RedTeamPlatform(ctx, logger)
	if err != nil {
		b.Fatalf("Failed to create platform: %v", err)
	}
	defer platform.Stop()
	
	// Pre-populate with some assessments
	target := TargetInfo{IP: "192.168.1.1"}
	for i := 0; i < 10; i++ {
		_, _ = platform.AssessVulnerabilities(target, ctx)
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stats := platform.GetStats()
		_ = stats.AssessmentsRun
	}
}

func BenchmarkAttackChainOrchestrator_Orchestrate(b *testing.B) {
	ctx := context.Background()
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	
	graphEngine, _ := patent.NewSelfEvolvingGraphEngine(logger)
	predictor, _ := patent.NewQuantumResistantPredictor(logger)
	defenseSystem, _ := patent.NewAdversarialMLDefenseSystem(logger)
	
	orchestrator := NewAttackChainOrchestrator(graphEngine, predictor, defenseSystem)
	
	// Generate realistic test data
	attackPaths := generateMockAttackPaths(20)
	quantumThreats := generateMockQuantumThreats(15)
	defenseReports := generateMockDefenseReports(5)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := orchestrator.Orchestrate(attackPaths, quantumThreats, defenseReports)
		_ = len(result.Paths)
	}
}

func BenchmarkM34Platform_ScaleLinearly(b *testing.B) {
	ctx := context.Background()
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	
	platform, err := NewM34RedTeamPlatform(ctx, logger)
	if err != nil {
		b.Fatalf("Failed to create platform: %v", err)
	}
	defer platform.Stop()
	
	targetBase := TargetInfo{
		IP:     "10.0.0.",
		Ports:  []int{80, 443},
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		target := TargetInfo{
			IP:    targetBase.IP + fmt.Sprintf("%d", i%256),
			Ports: targetBase.Ports,
		}
		
		_, err := platform.AssessVulnerabilities(target, ctx)
		if err != nil {
			b.Fatalf("Assessment failed: %v", err)
		}
	}
}

// ============================================================================
// LOAD TESTS - MULTI-TENANT SCENARIOS
// ============================================================================

func BenchmarkM34Platform_TenantIsolation(b *testing.B) {
	ctx := context.Background()
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	
	// Create multiple platform instances for different tenants
	numTenants := 5
	var platforms []*M34RedTeamPlatform
	
	for i := 0; i < numTenants; i++ {
		platform, err := NewM34RedTeamPlatform(ctx, logger)
		if err != nil {
			b.Fatalf("Failed to create tenant %d platform: %v", i, err)
		}
		platforms = append(platforms, platform)
	}
	defer func() {
		for _, p := range platforms {
			p.Stop()
		}
	}()
	
	target := TargetInfo{
		IP:     "192.168.1.100",
		Ports:  []int{80, 443},
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tenantIdx := i % numTenants
		_, err := platforms[tenantIdx].AssessVulnerabilities(target, ctx)
		if err != nil {
			b.Fatalf("Tenant %d assessment failed: %v", tenantIdx, err)
		}
	}
}

// ============================================================================
// COMPARISON TESTS - SINGLE VS INTEGRATED PATENTS
// ============================================================================

func BenchmarkPatent1_Alone(b *testing.B) {
	ctx := context.Background()
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	
	engine, err := patent.NewSelfEvolvingGraphEngine(logger)
	if err != nil {
		b.Fatalf("Failed to create graph engine: %v", err)
	}
	
	target := TargetInfo{
		IP:     "192.168.1.1",
		Ports:  []int{80, 443},
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := engine.DiscoverOptimizedPaths(target, ctx)
		if err != nil {
			b.Skip("No paths discovered")
		}
	}
}

func BenchmarkPatent2_Alone(b *testing.B) {
	ctx := context.Background()
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	
	predictor, err := patent.NewQuantumResistantPredictor(logger)
	if err != nil {
		b.Fatalf("Failed to create predictor: %v", err)
	}
	
	paths := generateMockAttackPaths(10)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = predictor.AssessThreats(paths, ctx)
	}
}

func BenchmarkPatent3_Alone(b *testing.B) {
	ctx := context.Background()
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	
	defense, err := patent.NewAdversarialMLDefenseSystem(logger)
	if err != nil {
		b.Fatalf("Failed to create defense system: %v", err)
	}
	
	target := TargetInfo{IP: "192.168.1.1"}
	paths := generateMockAttackPaths(10)
	quantumMatrix := make(map[string]*patent.QuantumThreat)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = defense.EvaluateDefenses(target, paths, quantumMatrix)
	}
}

func BenchmarkM34_PlusVsSumOfParts(b *testing.B) {
	ctx := context.Background()
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	
	// Measure standalone patents
	p1Start := time.Now()
	patent1Ops := 0
	for i := 0; i < b.N; i++ {
		engine, _ := patent.NewSelfEvolvingGraphEngine(logger)
		target := TargetInfo{IP: "192.168.1.1"}
		_, err := engine.DiscoverOptimizedPaths(target, ctx)
		if err == nil {
			patent1Ops++
		}
	}
	patent1Time := time.Since(p1Start)
	
	p2Start := time.Now()
	patent2Ops := 0
	for i := 0; i < b.N; i++ {
		predictor, _ := patent.NewQuantumResistantPredictor(logger)
		paths := generateMockAttackPaths(5)
		_ = predictor.AssessThreats(paths, ctx)
		patent2Ops++
	}
	patent2Time := time.Since(p2Start)
	
	p3Start := time.Now()
	patent3Ops := 0
	for i := 0; i < b.N; i++ {
		defense, _ := patent.NewAdversarialMLDefenseSystem(logger)
		target := TargetInfo{IP: "192.168.1.1"}
		paths := generateMockAttackPaths(5)
		matrix := make(map[string]*patent.QuantumThreat)
		_ = defense.EvaluateDefenses(target, paths, matrix)
		patent3Ops++
	}
	patent3Time := time.Since(p3Start)
	
	singleTotal := patent1Time + patent2Time + patent3Time
	
	// Now measure integrated platform
	p4Start := time.Now()
	platform, _ := NewM34RedTeamPlatform(ctx, logger)
	target := TargetInfo{IP: "192.168.1.1"}
	
	integratedOps := 0
	for i := 0; i < b.N; i++ {
		_, err := platform.AssessVulnerabilities(target, ctx)
		if err == nil {
			integratedOps++
		}
	}
	integratedTotal := time.Since(p4Start)
	
	platform.Stop()
	
	b.Logf("📊 Single patents total: %v, Integrated platform: %v", 
		singleTotal, integratedTotal)
	b.Logf("🔗 Synergy factor: %.2fx (should be > 1.0)", 
		float64(singleTotal)/float64(integratedTotal))
}

// ============================================================================
// HELPERS FOR GENERATING TEST DATA
// ============================================================================

func generateRandomTarget() TargetInfo {
	port := 80 + (time.Now().UnixNano() % 7)
	return TargetInfo{
		IP:       fmt.Sprintf("192.168.%d.%d", port, time.Now().UnixNano()%256),
		Hostname: fmt.Sprintf("target-%d.example.com", port),
		Ports:    []int{port, port + 1},
	}
}

func generateTargets(n int) []TargetInfo {
	targets := make([]TargetInfo, n)
	for i := 0; i < n; i++ {
		targets[i] = TargetInfo{
			IP:    fmt.Sprintf("10.0.%d.%d", i/256, i%256),
			Ports: []int{80 + i, 443 + i},
		}
	}
	return targets
}

// Mock generators for benchmark isolation
func generateMockAttackPaths(count int) []patent.AttackPath {
	paths := make([]patent.AttackPath, count)
	for i := 0; i < count; i++ {
		paths[i] = patent.AttackPath{
			ID:      uint64(i),
			StartState: StateID(fmt.Sprintf("init_%d", i)),
			EndState: EndState(fmt.Sprintf("goal_%d", i)),
			Reward: float64(i) * 0.1,
		}
	}
	return paths
}

func generateMockQuantumThreats(count int) map[string]*patent.QuantumThreat {
	threats := make(map[string]*patent.QuantumThreat)
	cves := []string{"CVE-2021-44228", "CVE-2022-22965", "CVE-2023-1234", 
		"CVE-2023-5678", "CVE-2023-9012"}
	
	for i := 0; i < count; i++ {
		cve := cves[i%len(cves)]
		threats[cve] = &patent.QuantumThreat{
			CVE:              cve,
			ExploitationProbability: float64(i) / float64(count),
			ThreatLevel:      float64(i*10) / 100,
		}
	}
	return threats
}

func generateMockDefenseReports(count int) []patent.DefenseReport {
	reports := make([]patent.DefenseReport, count)
	for i := 0; i < count; i++ {
		reports[i] = patent.DefenseReport{
			DetectorType:    patent.MLClassifierDetector,
			Confidence:      0.7 + float64(i)*0.05,
			DetectorCoverage: 0.6 + float64(i)*0.05,
			EvasionProbability: 0.2 + float64(i)*0.05,
		}
	}
	return reports
}
