// Package patent - Performance benchmarks for Adversarial ML Defense System
package patent

import (
	"fmt"
	"testing"
)

// ==================== Gradient Mask Benchmarks ====================

func BenchmarkGradientMask_ProtectedPredict(b *testing.B) {
	baseModel := NewMLModel(50, 5, []int{100, 75})
	mask := &GradientMask{
		originalModel:   baseModel,
		maskNetwork:     buildMaskNetwork(0.5),
		smoothingRate:   0.5,
		noisingStrength: 0.1,
		inputDimension:  50,
	}
	input := generateRandomFeatureVector(50)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pred := mask.ProtectedPredict(input)
		_ = pred
	}
}

func BenchmarkGradientMask_SmoothInput(b *testing.B) {
	baseModel := NewMLModel(50, 5, []int{100})
	mask := &GradientMask{
		originalModel:   baseModel,
		maskNetwork:     buildMaskNetwork(0.5),
		smoothingRate:   0.5,
		noisingStrength: 0.1,
		inputDimension:  50,
	}
	input := generateRandomFeatureVector(50)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		smoothed := mask.smoothInput(input)
		_ = smoothed
	}
}

func BenchmarkGradientMask_AddGradientNoise(b *testing.B) {
	baseModel := NewMLModel(50, 5, []int{100})
	mask := &GradientMask{
		originalModel:   baseModel,
		maskNetwork:     buildMaskNetwork(0.5),
		smoothingRate:   0.5,
		noisingStrength: 0.1,
		inputDimension:  50,
	}
	input := generateRandomFeatureVector(50)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		perturbed := mask.addGradientNoise(input)
		_ = perturbed
	}
}

func BenchmarkGradientMask_LocalGradient(b *testing.B) {
	baseModel := NewMLModel(50, 5, []int{100})
	mask := &GradientMask{
		originalModel:   baseModel,
		maskNetwork:     buildMaskNetwork(0.5),
		smoothingRate:   0.5,
		noisingStrength: 0.1,
		inputDimension:  50,
	}
	input := generateRandomFeatureVector(50)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		gradients, _ := mask.getLocalGradient(input, 0.01)
		_ = gradients
	}
}

// ==================== Feature Compression Benchmarks ====================

func BenchmarkFeatureCompression_CompressFeatures(b *testing.B) {
	compression := NewFeatureCompression(0.1)
	features := generateRandomFeatures(100, 0.5)
	compression.analyzeAndSelectFeatures(features)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compressed := compression.CompressFeatures(generateRandomFeatures(100, 0.5))
		_ = compressed
	}
}

func BenchmarkFeatureCompression_PCABasedCompression(b *testing.B) {
	compression := NewFeatureCompression(0.1)
	trainingData := generateTrainingDataset(1000)
	compression.trainPCA(trainingData)
	input := generateRandomFeatures(100, 0.5)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compressed := compression.projectToPCAFeatures(input)
		_ = compressed
	}
}

func BenchmarkFeatureCompression_AnalyzeFeatures(b *testing.B) {
	compression := NewFeatureCompression(0.1)
	features := generateRandomFeatures(100, 0.5)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compression.analyzeAndSelectFeatures(features)
	}
}

// ==================== Poisoning Detector Benchmarks ====================

func BenchmarkPoisoningDetector_DetectPoison(b *testing.B) {
	benignUpdates := generateBenignFederatedUpdates(50)
	detector := NewPoisoningDetector(3.0)
	
	for _, update := range benignUpdates {
		detector.AddBenignUpdate(update)
	}
	sampleUpdate := generateRandomFederatedUpdate()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		detected := detector.DetectPoison(sampleUpdate)
		_ = detected
	}
}

func BenchmarkPoisoningDetector_MahalanobisDistance(b *testing.B) {
	detector := &PoisoningDetector{}
	update := generateRandomFederatedUpdate()
	baseline := generateBenignFederatedUpdates(50)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		distance := detector.computeDeviation(update, baseline)
		_ = distance
	}
}

func BenchmarkPoisoningDetector_FlattenUpdate(b *testing.B) {
	detector := &PoisoningDetector{}
	update := generateRandomFederatedUpdate()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		flat := detector.flattenUpdate(update)
		_ = flat
	}
}

// ==================== Byzantine Aggregation Benchmarks ====================

func BenchmarkByzantineAggregator_Aggregate(b *testing.B) {
	updates := generateFederatedUpdatesWithByzantine(20, 0.2)
	aggregator := NewByzantineAggregator([]string{"Krum"}, 0.2)
	aggregator.aggregationAlgorithm = "Krum"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := aggregator.Aggregate(updates)
		if err != nil {
			b.Fatal(err)
		}
		_ = result
	}
}

func BenchmarkByzantineAggregator_KrumAlgorithm(b *testing.B) {
	updates := generateFederatedUpdatesWithByzantine(20, 0.2)
	aggregator := NewByzantineAggregator([]string{"Krum"}, 0.2)
	aggregator.aggregationAlgorithm = "Krum"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := aggregator.krumAggregate(updates)
		if err != nil {
			b.Fatal(err)
		}
		_ = result
	}
}

func BenchmarkByzantineAggregator_MultiKrumAlgorithm(b *testing.B) {
	updates := generateFederatedUpdatesWithByzantine(20, 0.2)
	aggregator := NewByzantineAggregator([]string{"Multi-Krum"}, 0.2)
	aggregator.aggregationAlgorithm = "Multi-Krum"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := aggregator.multiKrumAggregate(updates)
		if err != nil {
			b.Fatal(err)
		}
		_ = result
	}
}

func BenchmarkByzantineAggregator_MedianAlgorithm(b *testing.B) {
	updates := generateFederatedUpdatesWithByzantine(20, 0.2)
	aggregator := NewByzantineAggregator([]string{"Median"}, 0.2)
	aggregator.aggregationAlgorithm = "Median"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := aggregator.medianAggregate(updates)
		if err != nil {
			b.Fatal(err)
		}
		_ = result
	}
}

func BenchmarkByzantineAggregator_WeightedDistance(b *testing.B) {
	u1 := generateRandomFederatedUpdate()
	u2 := generateRandomFederatedUpdate()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dist := computeWeightedDistance(u1, u2)
		_ = dist
	}
}

// ==================== GAN Benchmarks ====================

func BenchmarkGANNetwork_Forward(b *testing.B) {
	network := NewGANNetwork(50, 25, 100, 75)
	input := generateRandomFeatureVector(50)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		output := network.Forward(input)
		_ = output
	}
}

func BenchmarkAttackGeneratorGAN_GenerateBatch(b *testing.B) {
	gan := NewAttackGenerator(50, 5, 0.1)
	samples := []DatasetSample{
		{ID: "test", Features: generateRandomFeatureVector(50), Label: 1},
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		attacks := gan.GenerateAdversarialExamples(samples)
		_ = attacks
	}
}

func BenchmarkDefenseEnhancer_BuildRobustModel(b *testing.B) {
	baseModel := NewMLModel(50, 5, []int{100, 75})
	enhancer := NewDefenseEnhancer(baseModel)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model := enhancer.BuildRobustModel()
		_ = model
	}
}

func BenchmarkDefenseEnhancer_IncrementalRetrain(b *testing.B) {
	baseModel := NewMLModel(50, 5, []int{100, 75})
	enhancer := NewDefenseEnhancer(baseModel)
	trainingData := generateTrainingDataset(500)
	enhancer.trainingSamples = trainingData
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model := enhancer.incrementalRetrain()
		_ = model
	}
}

// ==================== Model Forward Pass Benchmarks ====================

func BenchmarkMLModel_Forward(b *testing.B) {
	model := NewMLModel(50, 5, []int{100, 75})
	input := generateRandomFeatureVector(50)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pred := model.Forward(input)
		_ = pred
	}
}

func BenchmarkMLModel_Predict(b *testing.B) {
	model := NewMLModel(50, 5, []int{100, 75})
	input := generateRandomFeatureVector(50)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		label := model.Predict(input)
		_ = label
	}
}

func BenchmarkSoftmax(b *testing.B) {
	scores := make([]float64, 10)
	for i := range scores {
		scores[i] = rand.NormFloat64()
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pred := Softmax(scores)
		_ = pred
	}
}

// ==================== Helper Utilities ====================

func generateRandomFeatureVector(size int) []float64 {
	vec := make([]float64, size)
	for i := range vec {
		vec[i] = rand.NormFloat64() * 0.5
	}
	return vec
}

func generateRandomFeatures(count int, variance float64) []float64 {
	features := make([]float64, count)
	for i := range features {
		features[i] = rand.NormFloat64() * variance
	}
	return features
}

func generateTrainingDataset(size int) [][]float64 {
	data := make([][]float64, size)
	for i := range data {
		data[i] = generateRandomFeatureVector(50)
	}
	return data
}

func generateBenignFederatedUpdates(count int) []FederatedUpdate {
	updates := make([]FederatedUpdate, count)
	for i := range updates {
		updates[i] = FederatedUpdate{
			ClientID:  fmt.Sprintf("benign_%d", i),
			Weights:   generateNormalWeights(),
			Biases:    []float64{0.0},
		}
	}
	return updates
}

func generateFederatedUpdatesWithByzantine(totalCount int, byzantineRatio float64) []FederatedUpdate {
	updates := make([]FederatedUpdate, totalCount)
	
	numByzantine := int(float64(totalCount)*byzantineRatio)
	for i := 0; i < totalCount; i++ {
		if i < numByzantine {
			updates[i] = FederatedUpdate{
				ClientID:  fmt.Sprintf("byzantine_%d", i),
				Weights:   generateExtremeWeights(),
				Biases:    generateExtremeBiases(),
			}
		} else {
			updates[i] = FederatedUpdate{
				ClientID:  fmt.Sprintf("benign_%d", i-numByzantine),
				Weights:   generateNormalWeights(),
				Biases:    []float64{0.0},
			}
		}
	}
	return updates
}

func generateExtremeWeights() [][][]float64 {
	return [][][]float64{{
		{100.0, -100.0, 50.0, -50.0, 25.0, -25.0, 10.0, -10.0, 5.0, -5.0},
	}}
}

func generateExtremeBiases() []float64 {
	return []float64{100.0, -100.0, 50.0, -50.0, 25.0}
}

func generateNormalWeights() [][][]float64 {
	return [][][]float64{{
		{0.1, -0.1, 0.05, -0.05, 0.02, -0.02, 0.01, -0.01, 0.005, -0.005},
	}}
}

func generateRandomFederatedUpdate() FederatedUpdate {
	return FederatedUpdate{
		ClientID:  fmt.Sprintf("client_%d", rand.Intn(1000)),
		Weights:   generateNormalWeights(),
		Biases:    []float64{0.0},
	}
}
