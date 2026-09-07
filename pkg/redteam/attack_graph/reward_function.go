// Copyright © 2026 CloudAI Fusion. All Rights Reserved.
// Multi-Objective Optimization Reward Function for Q-Learning Attack Graph
// Patent #1: Q-Learning for Attack Path Dynamic Optimization (Invention ID: CT-2026-001)

package attack_graph

import (
	"fmt"
	"math"
	
	"github.com/sirupsen/logrus"
)


type RewardWeights struct {
	SuccessWeight float64 `json:"success_weight"`
	StealthWeight float64 `json:"stealth_weight"`
	DetectionPenaltyWeight float64 `json:"detection_penalty_weight"`
}

func DefaultWeights() RewardWeights {
	return RewardWeights{
		SuccessWeight:          0.4,
		StealthWeight:          0.3,
		DetectionPenaltyWeight: 0.3,
	}
}

func (rw RewardWeights) Validate() bool {
	sum := rw.SuccessWeight + rw.StealthWeight + rw.DetectionPenaltyWeight
	return math.Abs(sum-1.0) < 0.01
}

func (rw RewardWeights) Normalize() RewardWeights {
	if !rw.Validate() {
		log.Warnf("Invalid weights %v, normalizing automatically", rw)
		sum := rw.SuccessWeight + rw.StealthWeight + rw.DetectionPenaltyWeight
		if sum == 0 {
			return DefaultWeights()
		}
		rw.SuccessWeight /= sum
		rw.StealthWeight /= sum
		rw.DetectionPenaltyWeight /= sum
	}
	return rw
}

func (engine *Engine) CalculatePathReward(attacks []AttackAttempt) float64 {
	weights := DefaultWeights().Normalize()
	
	if len(attacks) == 0 {
		return 0.0
	}
	
	successRate := engine.calculateSuccessRate(attacks, weights.SuccessWeight)
	stealthScore := engine.calculateStealthScore(attacks, weights.StealthWeight)
	detectionProb := engine.calculateDetectionProbability(attacks, weights.DetectionPenaltyWeight)
	
	reward := successRate + stealthScore - detectionProb
	reward = math.Max(-10.0, math.Min(10.0, reward))
	
	logWithComponents(attacks).Debugf("Calculated path reward: %.4f [success=%.4f, stealth=%.4f, detection=-%.4f]",
		reward, successRate, stealthScore, detectionProb)
	
	return reward
}

func (engine *Engine) calculateSuccessRate(attacks []AttackAttempt, weight float64) float64 {
	if len(attacks) == 0 {
		return 0.0
	}
	
	successCount := 0
	for _, attempt := range attacks {
		if attempt.Success {
			successCount++
		}
	}
	
	successRate := float64(successCount) / float64(len(attacks))
	return weight * successRate
}

func (engine *Engine) calculateStealthScore(attacks []AttackAttempt, weight float64) float64 {
	if len(attacks) == 0 {
		return 0.0
	}
	
	totalStealth := 0.0
	
	for i, attempt := range attacks {
		var actionStealth float64
		
		if attempt.StealthScore > 0 {
			actionStealth = attempt.StealthScore
		} else {
			actionStealth = math.Max(0, 1.0-attempt.DetectionRisk)
		}
		
		temporalBonus := float64(i) / float64(len(attacks))
		actionStealth *= (1.0 + 0.1*temporalBonus)
		
		totalStealth += actionStealth
	}
	
	avgStealth := totalStealth / float64(len(attacks))
	return weight * avgStealth
}

func (engine *Engine) calculateDetectionProbability(attacks []AttackAttempt, weight float64) float64 {
	if len(attacks) == 0 {
		return 0.0
	}
	
	noDetectionProduct := 1.0
	
	for _, attempt := range attacks {
		pNoDetect := math.Max(0, 1.0-attempt.DetectionRisk)
		noDetectionProduct *= pNoDetect
	}
	
	detectionProbability := 1.0 - noDetectionProduct
	amplificationFactor := math.Pow(1.0+0.05, float64(len(attacks)))
	detectionProbability *= math.Min(amplificationFactor, 2.0)
	
	return weight * detectionProbability
}

func (engine *Engine) calculateSessionReward(episodes []TrainingReport, currentEpisode int) float64 {
	if len(episodes) == 0 {
		return 0.0
	}
	
	windowSize := 10
	startIdx := 0
	if len(episodes) > windowSize {
		startIdx = len(episodes) - windowSize
	}
	
	sumRecentRewards := 0.0
	successCountWindow := 0
	
	for i := startIdx; i < len(episodes); i++ {
		eps := episodes[i]
		sumRecentRewards += eps.AvgReward
		if eps.Success {
			successCountWindow++
		}
	}
	
	averageReward := sumRecentRewards / float64(windowSize)
	
	improvementBonus := 0.0
	if currentEpisode <= 50 && len(episodes) >= 2 {
		firstAvg := episodes[0].AvgReward
		lastAvg := episodes[len(episodes)-1].AvgReward
		if lastAvg > firstAvg+0.1 {
			improvementBonus = 0.2 * ((lastAvg - firstAvg) / 0.1)
			improvementBonus = math.Min(improvementBonus, 1.0)
		}
	}
	
	sessionReward := averageReward + improvementBonus
	if sessionReward > 0 {
		sessionReward *= 1.1
	} else {
		sessionReward *= 0.9
	}
	
	return sessionReward
}

func isCriticalAsset(nodeName string) bool {
	criticalKeywords := []string{
		"domain-controller",
		"kubernetes-master",
		"postgresql-primary",
		"redis-cluster-leader",
		"etcd-core",
		"cicd-runner",
		"secrets-vault",
	}
	
	nodeLower := nodeName
	for _, keyword := range criticalKeywords {
		if nodeLower == keyword || nodeLower == keyword+"-0" || nodeLower == keyword+"-1" {
			return true
		}
	}
	return false
}

func logWithComponents(attacks []AttackAttempt) *logrus.Entry {
	fields := logrus.Fields{
		"num_attempts": len(attacks),
	}
	
	for i, attempt := range attacks {
		key := fmt.Sprintf("attempt_%d", i)
		fields[key] = map[string]interface{}{
			"node":           attempt.NodeTarget,
			"action":         attempt.ActionType.String(),
			"success":        attempt.Success,
			"stealth":        attempt.StealthScore,
			"detection_risk": attempt.DetectionRisk,
		}
	}
	
	return log.WithFields(fields)
}
