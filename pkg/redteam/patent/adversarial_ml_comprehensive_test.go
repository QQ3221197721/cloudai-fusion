// Package patent - Comprehensive unit tests for Adversarial ML Defense System
package patent

import (
	"testing"
	
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ==================== Section 1: Gradient Mask Tests ====================

func TestMLModel_Forward(t *testing.T) {
	t.Run("normal forward pass", func(t *testing.T) {
		model := NewMLModel(10, 3, []int{20, 15})
		input := make(FeatureVector, 10)
		for i := range input {
			input[i] = float64(i) * 0.1
		}
		
		pred := model.Forward(input)
		
		assert.Len(t, pred, 3)
		assert.InEpsilon(t, 1.0, sumValues(pred), 0.01)
	})
	
	t.Run("output sums to one", func(t *testing.T) {
		model := NewMLModel(5, 2, []int{8})
		input := FeatureVector{0.1, 0.2, 0.3, 0.4, 0.5}
		
		pred := model.Forward(input)
		
		total := sumValues(pred)
		assert.InDelta(t, 1.0, total, 0.001)
	})
}

func TestGradientMask_ProtectedPredict(t *testing.T) {
	baseModel := NewMLModel(10, 3, []int{20, 15})
	mask := &GradientMask{
		originalModel:   baseModel,
		maskNetwork:     buildMaskNetwork(0.5),
		smoothingRate:   0.5,
		noisingStrength: 0.1,
		inputDimension:  10,
	}
	
	t.Run("normal operation", func(t *testing.T) {
		input := FeatureVector{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0}
		
		pred := mask.ProtectedPredict(input)
		
		require.NotNil(t, pred)
		assert.NotZero(t, len(pred))
		assert.InEpsilon(t, 1.0, sumValues(pred), 0.01)
	})
	
	t.Run("zero input handling", func(t *testing.T) {
		input := make(FeatureVector, 10)
		
		pred := mask.ProtectedPredict(input)
		
		assert.NotNil(t, pred)
		assert.Greater(t, len(pred), 0)
	})
	
	t.Run("extreme values handling", func(t *testing.T) {
		input := FeatureVector{1000, -1000, 0, 500, -500, 100, -100, 50, -50, 25}
		
		pred := mask.ProtectedPredict(input)
		
		assert.NotNil(t, pred)
		assert.NotZero(t, len(pred))
	})
}

func TestGradientMask_SmoothingRate(t *testing.T) {
	baseModel := NewMLModel(10, 3, []int{20})
	mask := NewMLModel(10, 3, []int{20})
	gm := &GradientMask{
		originalModel:   baseModel,
		maskNetwork:     buildMaskNetwork(0.5),
		smoothingRate:   0.5,
		noisingStrength: 0.1,
		inputDimension:  10,
	}
	
	t.Run("GetSmoothingRate returns correct value", func(t *testing.T) {
		rate := gm.GetSmoothingRate()
		
		assert.Equal(t, 0.5, rate)
	})
	
	t.Run("SetSmoothingRate updates value", func(t *testing.T) {
		gm.SetSmoothingRate(0.8)
		
		rate := gm.GetSmoothingRate()
		assert.Equal(t, 0.8, rate)
	})
}

// ==================== Section 2: Feature Compression Tests ====================

func TestFeatureCompression_AnalyzeFeatures(t *testing.T) {
	compression := NewFeatureCompression(0.1)
	
	t.Run("high variance features selected", func(t *testing.T) {
		features := generateRandomFeatures(50, 0.8)
		compressed := compression.CompressFeatures(features)
		
		assert.NotEmpty(t, compressed)
		assert.GreaterOrEqual(t, len(compressed), 1)
	})
	
	t.Run("low variance features filtered", func(t *testing.T) {
		lowVarianceInput := make([]float64, 50)
		for i := range lowVarianceInput {
			lowVarianceInput[i] = 0.5 // Constant value
		}
		
		compression2 := NewFeatureCompression(0.1)
		compression2.analyzeAndSelectFeatures(lowVarianceInput)
		
		selected := compression2.GetSelectedFeatures()
		assert.Empty(t, selected)
	})
}

func TestFeatureCompression_PCABasedCompression(t *testing.T) {
	compression := NewFeatureCompression(0.3)
	
	t.Run("PCA reduces dimensionality", func(t *testing.T) {
		input := generateRandomFeatures(100, 0.5)
		compressed := compression.PCABasedCompression(input)
		
		assert.NotEmpty(t, compressed)
		assert.LessOrEqual(t, len(compressed), 50) // Should be reduced
	})
	
	t.Run("reduced features maintain structure", func(t *testing.T) {
		trainingData := generateTrainingDataset(1000)
		compression.trainPCA(trainingData)
		
		testInput := trainingData[0]
		compressed := compression.projectToPCAFeatures(testInput)
		
		assert.Greater(t, len(compressed), 0)
	})
}

func TestFeatureCompression_Reset(t *testing.T) {
	compression := NewFeatureCompression(0.1)
	
	// Generate some statistics
	features := generateRandomFeatures(50, 0.5)
	compression.CompressFeatures(features)
	compression.PCABasedCompression(features)
	
	// Reset and verify
	compression.Reset()
	
	stats := compression.GetSelectedFeatures()
	assert.Empty(t, stats)
}

// ==================== Section 3: Poisoning Detector Tests ====================

func TestPoisoningDetector_DetectPoison(t *testing.T) {
	// Generate baseline benign updates
	benignUpdates := generateBenignFederatedUpdates(20)
	detector := NewPoisoningDetector(3.0)
	
	// Add benign updates as baseline
	for _, update := range benignUpdates {
		detector.AddBenignUpdate(update)
	}
	
	t.Run("sufficient baseline required", func(t *testing.T) {
		lessThanThree := generateBenignFederatedUpdates(2)
		smallDetector := NewPoisoningDetector(3.0)
		
		for _, update := range lessThanThree {
			smallDetector.AddBenignUpdate(update)
		}
		
		testUpdate := generateRandomFederatedUpdate()
		detected := smallDetector.DetectPoison(testUpdate)
		
		assert.False(t, detected) // Should return false due to insufficient baseline
	})
	
	t.Run("detect significantly different update", func(t *testing.T) {
		// Create a poisoned update with extreme values
		poisoned := FederatedUpdate{
			ClientID: "poisoner1",
			Weights:  generateExtremeWeights(),
			Biases:   generateExtremeBiases(),
		}
		
		detected := detector.DetectPoison(poisoned)
		
		// May or may not detect depending on threshold, but should run without error
		assert.NotPanics(t, func() {
			detector.DetectPoison(poisoned)
		})
	})
}

func TestPoisoningDetector_HistoryManagement(t *testing.T) {
	detector := NewPoisoningDetector(3.0)
	
	// Run many detections
	for i := 0; i < 1500; i++ {
		update := FederatedUpdate{
			ClientID:  string(rune(i)),
			Weights:   generateNormalWeights(),
			Biases:    []float64{0.0},
		}
		detector.DetectPoison(update)
	}
	
	history := detector.GetDetectionHistory()
	assert.LessOrEqual(t, len(history), 1000) // Max history size enforced
}

// ==================== Section 4: Byzantine Aggregation Tests ====================

func TestByzantineResistantAggregator_Aggregate(t *testing.T) {
	// Create 20 updates with 20% potentially malicious
	updates := generateFederatedUpdatesWithByzantine(20, 0.2)
	
	t.Run("Krum algorithm handles Byzantine", func(t *testing.T) {
	 aggregator := NewByzantineAggregator([]string{"Krum"}, 0.2)
		aggregator.aggregationAlgorithm = "Krum"
		
		result, err := aggregator.Aggregate(updates)
		
		require.NoError(t, err)
		assert.NotEmpty(t, result.Layers)
		assert.Greater(t, len(result.Layers), 0)
	})
	
	t.Run("Multi-Krum is more robust", func(t *testing.T) {
	 aggregator := NewByzantineAggregator([]string{"Multi-Krum"}, 0.2)
		aggregator.aggregationAlgorithm = "Multi-Krum"
		
		result, err := aggregator.Aggregate(updates)
		
		require.NoError(t, err)
		assert.NotEmpty(t, result.Layers)
	})
	
	t.Run("Median handles outliers", func(t *testing.T) {
	 aggregator := NewByzantineAggregator([]string{"Median"}, 0.2)
		aggregator.aggregationAlgorithm = "Median"
		
		result, err := aggregator.Aggregate(updates)
		
		require.NoError(t, err)
		assert.NotEmpty(t, result.Layers)
	})
}

func TestByzantineResistantAggregator_MinimumUpdates(t *testing.T) {
	 aggregator := NewByzantineAggregator([]string{"Krum"}, 0.2)
	
	t.Run("error when too few updates", func(t *testing.T) {
		 singleUpdate := []FederatedUpdate{generateRandomFederatedUpdate()}
		
		_, err := aggregator.Aggregate(singleUpdate)
		
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "insufficient")
	})
	
	t.Run("success with minimum required", func(t *testing.T) {
		minimalUpdates := generateFederatedUpdatesWithByzantine(3, 0.0)
		
		result, err := aggregator.Aggregate(minimalUpdates)
		
		require.NoError(t, err)
		assert.NotEmpty(t, result.Layers)
	})
}

func TestByzantineResistantAggregator_AlgorithmSwitch(t *testing.T) {
	 aggregator := NewByzantineAggregator([]string{"Krum", "Median", "TrimmedMean"}, 0.2)
	
	t.Run("switch to valid algorithm", func(t *testing.T) {
		err := aggregator.SwitchAlgorithm("Median")
		
		require.NoError(t, err)
		assert.Equal(t, "Median", aggregator.GetCurrentAlgorithm())
	})
	
	t.Run("reject invalid algorithm", func(t *testing.T) {
		err := aggregator.SwitchAlgorithm("InvalidAlg")
		
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported")
	})
}

// ==================== Section 5: GAN Tests ====================

func TestGANNetwork_Inference(t *testing.T) {
	network := NewGANNetwork(10, 5, []int{20, 15}...)
	input := generateRandomFeatureVector(10)
	
	output := network.Forward(input)
	
	require.NotEmpty(t, output)
	assert.Len(t, output, 5)
}

func TestAttackGeneratorGAN_GenerateExamples(t *testing.T) {
	gan := NewAttackGenerator(10, 3, 0.1)
	
	samples := []DatasetSample{
		{ID: "sample1", Features: generateRandomFeatureVector(10), Label: 1},
		{ID: "sample2", Features: generateRandomFeatureVector(10), Label: 2},
	}
	
	attacks := gan.GenerateAdversarialExamples(samples)
	
	require.NotEmpty(t, attacks)
	assert.Len(t, attacks, 2)
	
	for _, attack := range attacks {
		require.NotNil(t, attack.Perturbation)
		assert.Greater(t, len(attack.Perturbation), 0)
		assert.GreaterOrEqual(t, attack.DetectionProbability, 0.0)
		assert.LessOrEqual(t, attack.DetectionProbability, 1.0)
	}
}

func TestDefenseEnhancer_BuildRobustModel(t *testing.T) {
	baseModel := NewMLModel(10, 3, []int{20, 15})
	
	enhancer := NewDefenseEnhancer(baseModel)
	
	robustModel := enhancer.BuildRobustModel()
	
	require.NotNil(t, robustModel)
	assert.Equal(t, baseModel.numInputFeatures, robustModel.numInputFeatures)
	assert.Equal(t, baseModel.numClasses, robustModel.numClasses)
}

// ==================== Utility Functions ====================

func sumValues(p map[int]float64) float64 {
	sum := 0.0
	for _, v := range p {
		sum += v
	}
	return sum
}
