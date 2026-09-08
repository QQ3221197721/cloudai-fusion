package intelligence

import (
	"crypto/rand"
	"math/big"
	
	"github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/models"
)

// ============================================================================
// MULTI-PATH ATTACK GENERATOR (多种攻破解法) - Alternative Attack Path Generation
// ============================================================================

// MultiPathGenerator 多路径攻击生成器
type MultiPathGenerator struct {
	baseOptimizer *QLAttackOptimizer
	rng           rand.Reader // Use crypto/rand instead of math/rand for security
}

// GenerateAlternatives 生成多个替代方案
func (mpg *MultiPathGenerator) GenerateAlternatives(primaryPlan *AttackPlan, num int) []AttackPlan {
	var alternatives []AttackPlan
	
	for i := 0; i < num; i++ {
		alternative := mpg.perturbAndGenerate(primaryPlan, float64(i))
		alternatives = append(alternatives, *alternative)
	}
	
	return alternatives
}

// perturbAndGenerate 扰动并生成新路径
func (mpg *MultiPathGenerator) perturbAndGenerate(basePlan *AttackPlan, perturbationFactor float64) *AttackPlan {
	// Perturb the base plan with controlled randomness
	newSteps := make([]AttackStep, len(basePlan.Steps))
	
	for i, step := range basePlan.Steps {
		// Apply random noise to success probability using secure RNG
		noisySuccess := step.EstimatedSuccess + float64(mpg.generateSecureInt(1000)-500)/5000.0
		
		if noisySuccess > 1.0 {
			noisySuccess = 1.0
		} else if noisySuccess < 0.1 {
			noisySuccess = 0.1
		}
		
		newSteps[i] = step
		newSteps[i].EstimatedSuccess = noisySuccess
		newSteps[i].EstimatedDetection += float64(mpg.generateSecureInt(100))/1000.0 // Add detection noise
	}
	
	return &AttackPlan{
		Steps:            newSteps,
		OverallSuccess:   calculateChainProbability(newSteps),
		EstimatedTime:    estimateTotalTime(newSteps),
		RiskScore:        calculateRiskScoreFromSteps(newSteps),
		BestPathConfidence: calculateConfidence(newSteps),
	}
}

// MonteCarloTreeSearch 蒙特卡洛树搜索
func (mpg *MultiPathGenerator) MonteCarloTreeSearch(target *DeepTargetIntelligence, arsenal *WeaponArsenalKnowledge, simulations int) *AttackPlan {
	bestPlan := &AttackPlan{}
	bestScore := -1.0
	
	// Run multiple simulation trajectories
	for sim := 0; sim < simulations; sim++ {
		simPlan := mpg.runSimulation(target, arsenal)
		
		// Score this path based on success/risk tradeoff
		score := simPlan.OverallSuccess*3.0 - simPlan.RiskScore
		
		if score > bestScore {
			bestScore = score
			bestPlan = simPlan
		}
	}
	
	return bestPlan
}

// runSimulation 运行单次模拟
func (mpg *MultiPathGenerator) runSimulation(target *DeepTargetIntelligence, arsenal *WeaponArsenalKnowledge) *AttackPlan {
	var steps []AttackStep
	
	currentState := &AttackState{
		CurrentStage:      0,
		Privileges:        models.PrivilegeNone,
		NetworkPivotCount: 0,
		DetectionLevel:    0.0,
	}
	
	for step := 0; step < 8; step++ {
		// Randomly select a weapon that hasn't been used
		weapon := mpg.getRandomUnusedWeapon(arsenal, steps)
		
		// Calculate step properties
		successProb := BuildContextualEffectivenessMap(weapon, currentState.Privileges.String())
		detectionRisk := currentState.DetectionLevel + 0.1
		
		stepData := AttackStep{
			WeaponID:          weapon.ID,
			WeaponName:        weapon.Name,
			Category:          weapon.Category,
			TargetEnvironment: "Production Environment",
			EstimatedSuccess:  successProb,
			EstimatedDetection: detectionRisk,
			ExpectedTime:      "N/A",
		}
		
		steps = append(steps, stepData)
		
		// Advance state randomly
		currentState.CurrentStage++
		currentState.Privileges = advancePrivilege(currentState.Privileges)
		currentState.NetworkPivotCount++
		currentState.DetectionLevel += 0.15
	}
	
	return &AttackPlan{
		Steps:          steps,
		OverallSuccess: calculateChainProbability(steps),
		EstimatedTime:  estimateTotalTime(steps),
		RiskScore:      calculateRiskScoreFromSteps(steps),
	}
}

// getRandomUnusedWeapon 获取未使用的随机武器
func (mpg *MultiPathGenerator) getRandomUnusedWeapon(arsenal *WeaponArsenalKnowledge, existingSteps []AttackStep) *WeaponProfile {
	usedIDs := make(map[string]bool)
	for _, step := range existingSteps {
		usedIDs[step.WeaponID] = true
	}
	
	var available []WeaponProfile
	for _, weapon := range arsenal.Weapons {
		if !usedIDs[weapon.ID] {
			available = append(available, weapon)
		}
	}
	
	if len(available) == 0 {
		return &arsenal.Weapons[0]
	}
	
	index := mpg.generateSecureInt(len(available))
	return &available[index]
}

// GeneticAlgorithmEvolution 遗传算法演化
func (mpg *MultiPathGenerator) GeneticAlgorithmEvolution(basePlan *AttackPlan, generations int, populationSize int) []AttackPlan {
	var population []*AttackPlan
	
	// Initialize population
	for i := 0; i < populationSize; i++ {
		individual := mpg.mutatePlan(basePlan, float64(i+1)/float64(populationSize))
		population = append(population, individual)
	}
	
	// Evolve for multiple generations
	for gen := 0; gen < generations; gen++ {
		population = mpg.evolveGeneration(population)
	}
	
	// Return top 3 solutions
	bestPlans := mpg.getBestPlans(population, 3)
	return bestPlans
}

// evolveGeneration 演化一代
func (mpg *MultiPathGenerator) evolveGeneration(population []*AttackPlan) []*AttackPlan {
	// Sort by fitness
	mpg.sortByFitness(population)
	
	var newPopulation []*AttackPlan
	
	// Elitism: keep top 20%
	eliteCount := len(population) / 5
	for i := 0; i < eliteCount && i < len(population); i++ {
		newPopulation = append(newPopulation, population[i])
	}
	
	// Crossover and mutation
	for len(newPopulation) < len(population) {
		parent1 := mpg.selectRoulette(population)
		parent2 := mpg.selectRoulette(population)
		
		child := mpg.crossover(parent1, parent2)
		child = mpg.mutatePlan(child, 0.1)
		
		newPopulation = append(newPopulation, child)
	}
	
	return newPopulation
}

// sortByFitness 按适应度排序
func (mpg *MultiPathGenerator) sortByFitness(population []*AttackPlan) {
	for i := 0; i < len(population)-1; i++ {
		for j := i + 1; j < len(population); j++ {
			if mpg.fitness(population[j]) > mpg.fitness(population[i]) {
				population[i], population[j] = population[j], population[i]
			}
		}
	}
}

// fitness 计算适应度分数
func (mpg *MultiPathGenerator) fitness(plan *AttackPlan) float64 {
	// Higher success, lower risk = better
	return plan.OverallSuccess*3.0 - plan.RiskScore
}

// selectRoulette 轮盘选择
func (mpg *MultiPathGenerator) selectRoulette(population []*AttackPlan) *AttackPlan {
	totalFitness := 0.0
	for _, plan := range population {
		totalFitness += mpg.fitness(plan)
	}
	
	threshold := float64(mpg.generateSecureInt(10000)) / 10000.0 * totalFitness
	cumulative := 0.0
	
	for _, plan := range population {
		cumulative += mpg.fitness(plan)
		if cumulative >= threshold {
			return plan
		}
	}
	
	return population[0]
}

// crossover 交叉操作
func (mpg *MultiPathGenerator) crossover(parent1, parent2 *AttackPlan) *AttackPlan {
	// Single-point crossover at random step
	crossoverPoint := mpg.generateSecureInt(min(len(parent1.Steps), len(parent2.Steps)))
	
	newSteps := make([]AttackStep, len(parent1.Steps))
	copy(newSteps, parent1.Steps)
	
	if crossoverPoint < len(parent2.Steps) && crossoverPoint < len(newSteps) {
		newSteps[crossoverPoint] = parent2.Steps[crossoverPoint]
	}
	
	return &AttackPlan{
		Steps:            newSteps,
		OverallSuccess:   calculateChainProbability(newSteps),
		EstimatedTime:    estimateTotalTime(newSteps),
		RiskScore:        calculateRiskScoreFromSteps(newSteps),
	}
}

// mutatePlan 变异计划
func (mpg *MultiPathGenerator) mutatePlan(base *AttackPlan, mutationRate float64) *AttackPlan {
	newPlan := &AttackPlan{
		Steps: make([]AttackStep, len(base.Steps)),
	}
	copy(newPlan.Steps, base.Steps)
	
	mutateThreshold := int(float64(1000)*mutationRate)
	
	for i := range newPlan.Steps {
		if mpg.generateSecureInt(1000) < mutateThreshold {
			// Mutate success probability significantly
			newPlan.Steps[i].EstimatedSuccess = 0.7 + float64(mpg.generateSecureInt(300))/1000.0
		}
	}
	
	newPlan.OverallSuccess = calculateChainProbability(newPlan.Steps)
	newPlan.RiskScore = calculateRiskScoreFromSteps(newPlan.Steps)
	
	return newPlan
}

// getBestPlans 获取最佳方案
func (mpg *MultiPathGenerator) getBestPlans(population []*AttackPlan, count int) []AttackPlan {
	mpg.sortByFitness(population)
	
	var best []AttackPlan
	for i := 0; i < count && i < len(population); i++ {
		planCopy := *population[i]
		best = append(best, planCopy)
	}
	
	return best
}

// generateSecureInt 生成安全的随机整数
func (mpg *MultiPathGenerator) generateSecureInt(max int) int {
	nBig, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0 // Fallback in case of error
	}
	return int(nBig.Int64())
}
