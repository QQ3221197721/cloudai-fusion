package intelligence

import (
	"fmt"
	"time"
)

// ============================================================================
// ATTACK GRAPH ENGINE (统一编排引擎) - Complete Dual-Core System
// ============================================================================

// EngineConfig 引擎配置
type EngineConfig struct {
	MaxAttackSteps      int
	GenerateAlternatives bool
	NumAlternativePaths int
	UseQLearning        bool
	MCTSIterations      int
	GeneticGenerations  int
}

// AttackGraphEngine 攻击图优化引擎 (知己知彼系统)
type AttackGraphEngine struct {
	targetIntel   *DeepTargetIntelligence
	weaponArsenal *WeaponArsenalKnowledge
	multiPathGen  *MultiPathGenerator
	config        EngineConfig
}

// NewAttackGraphEngine 创建新引擎实例
func NewAttackGraphEngine(target *DeepTargetIntelligence, arsenal *WeaponArsenalKnowledge) *AttackGraphEngine {
	return &AttackGraphEngine{
		targetIntel:   target,
		weaponArsenal: arsenal,
		multiPathGen:  &MultiPathGenerator{},
		config: EngineConfig{
			MaxAttackSteps:       10,
			GenerateAlternatives: true,
			NumAlternativePaths:  5,
			UseQLearning:         true,
			MCTSIterations:       1000,
			GeneticGenerations:   50,
		},
	}
}

// OptimizeAttackChain 优化单条攻击链
func (ag *AttackGraphEngine) OptimizeAttackChain() *AttackPlan {
	if ag.config.UseQLearning {
		return ag.optimizeWithQLearning()
	}
	return ag.optimizeHeuristic()
}

// optimizeWithQLearning Q-Learning 优化
func (ag *AttackGraphEngine) optimizeWithQLearning() *AttackPlan {
	baseline := ag.multiPathGen.getBaselinePlan(ag.targetIntel, ag.weaponArsenal)
	
	if ag.config.GenerateAlternatives {
		variations := ag.multiPathGen.GenerateAlternatives(baseline, ag.config.NumAlternativePaths-1)
		
		best := baseline
		for _, variant := range variations {
			if variant.OverallSuccess > best.OverallSuccess {
				best = &variant
			}
		}
		
		return best
	}
	
	return baseline
}

// optimizeHeuristic 启发式优化
func (ag *AttackGraphEngine) optimizeHeuristic() *AttackPlan {
	return ag.multiPathGen.getBaselinePlan(ag.targetIntel, ag.weaponArsenal)
}

// GenerateMultipleAttackPaths 生成多条攻击路径
func (ag *AttackGraphEngine) GenerateMultipleAttackPaths(numPaths int) []AttackPlan {
	plans := ag.multiPathGen.GenerateMultiplePaths(ag.targetIntel, ag.weaponArsenal, numPaths)
	sortPlansBySuccess(plans)
	return plans
}

// GetTargetAnalysis 获取目标分析报告
func (ag *AttackGraphEngine) GetTargetAnalysis() string {
	sb := fmt.Sprintf("=== Deep Target Intelligence Analysis ===\n\n")
	
	sb += fmt.Sprintf("Target ID: %s\n", ag.targetIntel.TargetID)
	sb += fmt.Sprintf("Target Name: %s\n", ag.targetIntel.TargetName)
	sb += fmt.Sprintf("Confidence Level: %.2f%%\n", ag.targetIntel.Confidence*100)
	sb += "\n--- Operating Systems ---\n"
	
	for name, os := range ag.targetIntel.Environment.OperatingSystems {
		sb += fmt.Sprintf("%s:\n", name)
		sb += fmt.Sprintf("  Version: %s\n", os.Version)
		sb += fmt.Sprintf("  Patch Level: %.0f%%\n", os.PatchLevel*100)
		sb += fmt.Sprintf("  Known CVEs: %d\n", len(os.CVEs))
		sb += "\n"
	}
	
	if ag.targetIntel.VulnerabilitySurface != nil {
		sb += "--- Vulnerability Surface ---\n"
		sb += fmt.Sprintf("Known CVEs: %d\n", len(ag.targetIntel.VulnerabilitySurface.CVEs))
		sb += fmt.Sprintf("Hardening Score: %.2f/1.0\n", ag.targetIntel.VulnerabilitySurface.HardeningScore.OverallSecurityPosture)
		sb += "\n"
	}
	
	if ag.targetIntel.ActiveDefense != nil {
		sb += "--- Active Defense Stack ---\n"
		sb += fmt.Sprintf("Monitoring Systems: %d\n", len(ag.targetIntel.ActiveDefense.RealTimeMonitoring))
		sb += fmt.Sprintf("SIEM Platforms: %v\n", ag.targetIntel.ActiveDefense.LogAggregation.SIEMPlatforms)
		sb += "\n"
	}
	
	sb += fmt.Sprintf("Risk Assessment:\n%s", ag.targetIntel.RiskAssessment())
	
	return sb
}

// GetArsenalAnalysis 获取军火库分析
func (ag *AttackGraphEngine) GetArsenalAnalysis() string {
	sb := fmt.Sprintf("=== Weapon Arsenal Knowledge Base ===\n\n")
	sb += fmt.Sprintf("Version: %s\n", ag.weaponArsenal.Version)
	sb += fmt.Sprintf("Total Weapons: %d\n\n", len(ag.weaponArsenal.Weapons))
	
	categories := make(map[AttackCategory]int)
	for _, weapon := range ag.weaponArsenal.Weapons {
		categories[weapon.Category]++
	}
	
	sb += "--- Weapon Categories ---\n"
	for cat, count := range categories {
		sb += fmt.Sprintf("%s: %d weapons\n", cat, count)
	}
	sb += "\n"
	
	sb += "--- Top Weapons ---\n"
	for i, weapon := range ag.weaponArsenal.Weapons {
		if i >= 3 {
			break
		}
		sb += fmt.Sprintf("\n[%s]\n", weapon.ID)
		sb += fmt.Sprintf("Name: %s | Category: %s\n", weapon.Name, weapon.Category)
	}
	
	return sb
}

// GenerateFullReport 生成完整报告
func (ag *AttackGraphEngine) GenerateFullReport(includeAlternatives bool) string {
	sb := fmt.Sprintf("=== COMPLETE ATTACK GRAPH ANALYSIS REPORT ===\n")
	sb += fmt.Sprintf("Generated at: %s\n\n", time.Now().Format(time.RFC3339))
	
	sb += ag.GetTargetAnalysis()
	sb += "\n\n" + ag.GetArsenalAnalysis()
	
	sb += "\n=== Optimized Attack Plan ===\n"
	bestPlan := ag.OptimizeAttackChain()
	
	sb += fmt.Sprintf("Steps: %d\n", len(bestPlan.Steps))
	sb += fmt.Sprintf("Success Probability: %.2f%%\n", bestPlan.OverallSuccess*100)
	sb += fmt.Sprintf("Risk Score: %.2f\n", bestPlan.RiskScore)
	
	sb += "\n--- Attack Steps Detail ---\n"
	for i, step := range bestPlan.Steps {
		sb += fmt.Sprintf("\nStep %d: %s (%s)\n", i+1, step.WeaponName, step.WeaponID)
		sb += fmt.Sprintf("  Success: %.1f%% | Detection Risk: %.1f%%\n", 
			step.EstimatedSuccess*100, step.EstimatedDetection*100)
	}
	
	if includeAlternatives && ag.config.GenerateAlternatives {
		sb += "\n=== Alternative Attack Paths ===\n"
		alternatives := ag.multiPathGen.GenerateAlternatives(bestPlan, ag.config.NumAlternativePaths)
		
		for i, alt := range alternatives {
			if i == 0 && len(alternatives) > 1 {
				continue
			}
			
			sb += fmt.Sprintf("\nAlternative Path %d: Success=%.1f%% Risk=%.2f\n", i, alt.OverallSuccess*100, alt.RiskScore)
		}
	}
	
	sb += "\n=== END OF REPORT ===\n"
	
	return sb
}

// QuickAnalysis 快速分析摘要
func (ag *AttackGraphEngine) QuickAnalysis() string {
	sb := fmt.Sprintf("[QUICK ANALYSIS]\n")
	sb += fmt.Sprintf("Target: %s\n", ag.targetIntel.TargetName)
	sb += fmt.Sprintf("Risk: %s\n", ag.targetIntel.RiskAssessment())
	
	bestPlan := ag.OptimizeAttackChain()
	sb += fmt.Sprintf("Optimal Chain Success: %.1f%%\n", bestPlan.OverallSuccess*100)
	sb += fmt.Sprintf("Steps: %d\n", len(bestPlan.Steps))
	
	return sb
}

func sortPlansBySuccess(plans []AttackPlan) {
	for i := 0; i < len(plans)-1; i++ {
		for j := i + 1; j < len(plans); j++ {
			if plans[j].OverallSuccess > plans[i].OverallSuccess {
				plans[i], plans[j] = plans[j], plans[i]
			}
		}
	}
}

// Example usage pattern in main():
// func main() {
//     targetIntel := CreateSampleIntel()
//     weaponArsenal := CreateSampleArsenal()
//     
//     engine := NewAttackGraphEngine(targetIntel, weaponArsenal)
//     
//     // Full analysis with alternatives
//     report := engine.GenerateFullReport(true)
//     fmt.Println(report)
//     
//     // Quick summary
//     quick := engine.QuickAnalysis()
//     fmt.Println(quick)
// }
