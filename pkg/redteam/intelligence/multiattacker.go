package intelligence

import (
	"crypto/rand"
	"math/big"
	
	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/models"
)

// ============================================================================
// Q-LEARNING ATTACK OPTIMIZER (运筹帷幄) - Simplified RL Core
// ============================================================================

// PrivilegeLevel 权限级别枚举
type PrivilegeLevel int

const (
	PrivilegeNone PrivilegeLevel = iota
	PrivilegeUser
	PrivilegeLimited
	PrivilegeAdmin
	PrivilegeRoot
	PrivilegeSystem
)

// String 字符串表示
func (p PrivilegeLevel) String() string {
	switch p {
	case PrivilegeNone:
		return "none"
	case PrivilegeUser:
		return "user"
	case PrivilegeLimited:
		return "limited"
	case PrivilegeAdmin:
		return "admin"
	case PrivilegeRoot:
		return "root"
	case PrivilegeSystem:
		return "system"
	default:
		return "unknown"
	}
}

// ============================================================================
// MULTI-PATH ATTACK GENERATOR (多种攻破解法)
// ============================================================================

// MultiPathGenerator 多路径攻击生成器
type MultiPathGenerator struct{}

// GenerateAlternatives 生成多个替代方案
func (mpg *MultiPathGenerator) GenerateAlternatives(primaryPlan *AttackPlan, num int) []AttackPlan {
	var alternatives []AttackPlan
	
	for i := 0; i < num; i++ {
		alternative := mpg.generateAlternative(*primaryPlan, i)
		alternatives = append(alternatives, alternative)
	}
	
	return alternatives
}

// generateAlternative 生成替代方案
func (mpg *MultiPathGenerator) generateAlternative(base AttackPlan, seed int) AttackPlan {
	newSteps := make([]AttackStep, len(base.Steps))
	copy(newSteps, base.Steps)
	
	// Apply deterministic perturbation based on seed
	perturbation := float64(seed) / 100.0
	
	for i := range newSteps {
		newSteps[i].EstimatedSuccess = base.Steps[i].EstimatedSuccess + perturbation*0.1
		if newSteps[i].EstimatedSuccess > 0.95 {
			newSteps[i].EstimatedSuccess = 0.95
		}
	}
	
	return AttackPlan{
		Steps:         newSteps,
		OverallSuccess: calculateChainProbability(newSteps),
		RiskScore:     calculateRiskScoreFromSteps(newSteps),
	}
}

// GenerateMultiplePaths 生成多条路径
func (mpg *MultiPathGenerator) GenerateMultiplePaths(target *DeepTargetIntelligence, arsenal *WeaponArsenalKnowledge, numPaths int) []AttackPlan {
	var plans []AttackPlan
	
	// First, get a baseline plan using simple heuristic
	baseline := mpg.getBaselinePlan(target, arsenal)
	plans = append(plans, *baseline)
	
	// Generate variations
	variations := mpg.GenerateAlternatives(baseline, numPaths-1)
	plans = append(plans, variations...)
	
	return plans
}

// getBaselinePlan 获取基线计划
func (mpg *MultiPathGenerator) getBaselinePlan(target *DeepTargetIntelligence, arsenal *WeaponArsenalKnowledge) *AttackPlan {
	var steps []AttackStep
	
	// Start with reconnaissance
	steps = append(steps, AttackStep{
		WeaponID:          "RECON-001",
		WeaponName:        "Network Reconnaissance",
		Category:          CATEGORY_NETWORK,
		EstimatedSuccess:  0.95,
		EstimatedDetection: 0.2,
	})
	
	// Find best weapon against target environment
	bestWeapon := arsenal.BestAgainstTarget("Production Environment")
	if bestWeapon != nil && !hasWeapon(steps, bestWeapon.ID) {
		steps = append(steps, AttackStep{
			WeaponID:          bestWeapon.ID,
			WeaponName:        bestWeapon.Name,
			Category:          bestWeapon.Category,
			EstimatedSuccess:  BuildContextualEffectivenessMap(bestWeapon, "Production Server"),
			EstimatedDetection: 0.3,
		})
	}
	
	// Add privilege escalation if needed
	privescWeapons := arsenal.GetWeaponsByCategory(CATEGORY_PRIVESC)
	for _, weapon := range privescWeapons {
		if !hasWeapon(steps, weapon.ID) {
			success := BuildContextualEffectivenessMap(&weapon, "Linux Server")
			if success > 0.7 {
				steps = append(steps, AttackStep{
					WeaponID:          weapon.ID,
					WeaponName:        weapon.Name,
					Category:          weapon.Category,
					EstimatedSuccess:  success,
					EstimatedDetection: 0.4,
				})
				break
			}
		}
	}
	
	return &AttackPlan{
		Steps:         steps,
		OverallSuccess: calculateChainProbability(steps),
		RiskScore:     calculateRiskScoreFromSteps(steps),
	}
}

// MonteCarloTreeSearch 蒙特卡洛树搜索
func (mpg *MultiPathGenerator) MonteCarloTreeSearch(target *DeepTargetIntelligence, arsenal *WeaponArsenalKnowledge, simulations int) *AttackPlan {
	bestPlan := &AttackPlan{}
	bestScore := -1.0
	
	for sim := 0; sim < simulations; sim++ {
		simPlan := mpg.runMonteCarloSimulation(target, arsenal, sim)
		score := simPlan.OverallSuccess*3.0 - simPlan.RiskScore
		
		if score > bestScore {
			bestScore = score
			bestPlan = simPlan
		}
	}
	
	return bestPlan
}

// runMonteCarloSimulation 运行蒙特卡洛模拟
func (mpg *MultiPathGenerator) runMonteCarloSimulation(target *DeepTargetIntelligence, arsenal *WeaponArsenalKnowledge, seed int) *AttackPlan {
	rng := createSecureRNG(seed)
	
	var steps []AttackStep
	currentStage := 0
	
	for stage := 0; stage < 5; stage++ {
		// Randomly pick an unused weapon
		available := getAvailableWeapons(arsenal, steps)
		if len(available) == 0 {
			break
		}
		
		index := big.Int{}
		rng.Read([]byte(index.Bytes()))
	_weapon := available[index.Int64()%int64(len(available))]
		
	success := BuildContextualEffectivenessMap(_weapon, currentEnvironment(stage))
	
		steps = append(steps, AttackStep{
			WeaponID:          _weapon.ID,
			WeaponName:        _weapon.Name,
			Category:          _weapon.Category,
			EstimatedSuccess:  success,
			EstimatedDetection: 0.3 + float64(stage)*0.1,
		})
		
		currentStage++
	}
	
	return &AttackPlan{
		Steps:         steps,
		OverallSuccess: calculateChainProbability(steps),
		RiskScore:     calculateRiskScoreFromSteps(steps),
	}
}

// GeneticAlgorithmEvolution 遗传算法演化
func (mpg *MultiPathGenerator) GeneticAlgorithmEvolution(basePlan *AttackPlan, generations int) []AttackPlan {
	// Initialize population
	populationSize := 10
	population := make([]*AttackPlan, populationSize)
	
	for i := 0; i < populationSize; i++ {
		population[i] = mpg.mutatePlan(basePlan, float64(i+1)/float64(populationSize))
	}
	
	// Evolve
	for gen := 0; gen < generations; gen++ {
		population = mpg.evolveGeneration(population)
	}
	
	// Return top 3
	mpg.sortByFitness(population)
	
	var bestPlans []AttackPlan
	for i := 0; i < min(3, len(population)); i++ {
		planCopy := *population[i]
		bestPlans = append(bestPlans, planCopy)
	}
	
	return bestPlans
}

// evolveGeneration 演化一代
func (mpg *MultiPathGenerator) evolveGeneration(population []*AttackPlan) []*AttackPlan {
	var newPopulation []*AttackPlan
	
	// Keep top performers
	topCount := len(population) / 3
	for i := 0; i < topCount; i++ {
		clone := *population[i]
		newPopulation = append(newPopulation, &clone)
	}
	
	// Create offspring through crossover and mutation
	for len(newPopulation) < len(population) {
		parent1 := population[len(newPopulation)%len(population)]
		parent2 := population[(len(newPopulation)+1)%len(population)]
		
		child := mpg.crossover(parent1, parent2)
		child = mpg.mutatePlan(child, 0.1)
		
		newPopulation = append(newPopulation, child)
	}
	
	return newPopulation
}

// mutatePlan 变异计划
func (mpg *MultiPathGenerator) mutatePlan(base *AttackPlan, mutationRate float64) *AttackPlan {
	newPlan := &AttackPlan{
		Steps: make([]AttackStep, len(base.Steps)),
	}
	copy(newPlan.Steps, base.Steps)
	
	for i := range newPlan.Steps {
		// Mutate success probability slightly
		delta := float64(randInt(100))/500.0 - 0.1 // -0.1 to 0.1
		if randomFloat64() < mutationRate {
			newPlan.Steps[i].EstimatedSuccess += delta
			if newPlan.Steps[i].EstimatedSuccess > 0.9 {
				newPlan.Steps[i].EstimatedSuccess = 0.9
			}
		}
	}
	
	newPlan.OverallSuccess = calculateChainProbability(newPlan.Steps)
	newPlan.RiskScore = calculateRiskScoreFromSteps(newPlan.Steps)
	
	return newPlan
}

// crossover 交叉
func (mpg *MultiPathGenerator) crossover(parent1, parent2 *AttackPlan) *AttackPlan {
	crossoverPoint := len(parent1.Steps) / 2
	
	newSteps := make([]AttackStep, len(parent1.Steps))
	copy(newSteps, parent1.Steps[:crossoverPoint])
	
	if crossoverPoint < len(parent2.Steps) {
		if crossoverPoint < len(newSteps) {
			newSteps[crossoverPoint] = parent2.Steps[crossoverPoint]
		}
	}
	
	return &AttackPlan{
		Steps:         newSteps,
		OverallSuccess: calculateChainProbability(newSteps),
		RiskScore:     calculateRiskScoreFromSteps(newSteps),
	}
}

// sortByFitness 按适应度排序
func (mpg *MultiPathGenerator) sortByFitness(population []*AttackPlan) {
	for i := 0; i < len(population)-1; i++ {
		for j := i + 1; j < len(population); j++ {
			fitness1 := mpg.fitness(population[i])
			fitness2 := mpg.fitness(population[j])
			
			if fitness2 > fitness1 {
				population[i], population[j] = population[j], population[i]
			}
		}
	}
}

// fitness 计算适应度
func (mpg *MultiPathGenerator) fitness(plan *AttackPlan) float64 {
	return plan.OverallSuccess*3.0 - plan.RiskScore
}

// Utility Functions

func hasWeapon(steps []AttackStep, weaponID string) bool {
	for _, step := range steps {
		if step.WeaponID == weaponID {
			return true
		}
	}
	return false
}

func calculateChainProbability(steps []AttackStep) float64 {
	product := 1.0
	for _, step := range steps {
		product *= step.EstimatedSuccess
		if product < 0.01 {
			break
		}
	}
	return product
}

func calculateRiskScoreFromSteps(steps []AttackStep) float64 {
	if len(steps) == 0 {
		return 0.0
	}
	
	total := 0.0
	for _, step := range steps {
		total += step.EstimatedDetection
	}
	return total / float64(len(steps))
}

func createSecureRNG(seed int) rand.Reader {
	seedBytes := make([]byte, 8)
	for i := 0; i < 8 && i < len(string(rune(seed))); i++ {
		seedBytes[i] = byte(seed >> uint(i*8)) & 0xFF
	}
	
	seededReader, _ := rand.NewCryptographicReader(seedBytes)
	return seededReader
}

func getAvailableWeapons(arsenal *WeaponArsenalKnowledge, used []AttackStep) []WeaponProfile {
	usedIDs := make(map[string]bool)
	for _, step := range used {
		usedIDs[step.WeaponID] = true
	}
	
	var available []WeaponProfile
	for _, weapon := range arsenal.Weapons {
		if !usedIDs[weapon.ID] {
			available = append(available, weapon)
		}
	}
	
	return available
}

func currentEnvironment(stage int) string {
	switch stage {
	case 0:
		return "Web Server"
	case 1:
		return "Application Server"
	case 2:
		return "Database Server"
	case 3:
		return "Internal Network"
	default:
		return "Production Server"
	}
}

func randInt(max int) int {
	nBig, _ := rand.Int(rand.Reader, big.NewInt(int64(max)))
	return int(nBig.Int64())
}

func randomFloat64() float64 {
	nBig, _ := rand.Int(rand.Reader, big.NewInt(1000000))
	return float64(nBig.Int64()) / 1000000.0
}
