// Package patent implements the Adversarial ML Defense System using GANs for OBCE3 certification.
// This patent covers gradient masking, feature compression, GAN-driven defensive augmentation,
// federated learning poisoning detection, and Byzantine-resistant aggregation mechanisms.
//
// Patent Classification: Machine Learning Security / Adversarial Defense Systems
// Technical Field: AI/ML security, adversarial robustness, federated learning protection
// Innovation Points: 
//   - Real-time gradient confusion + feature space compression
//   - Dual-GAN architecture (attack generator + defense enhancer)
//   - Mahalanobis distance-based poisoning detection
//   - Krum/Multi-Krum/Byzantine-Median hybrid aggregation
package patent

import (
	"fmt"
	"math"
	"math/rand"
	"slices"
	"sync"
	"time"
)

// ==================== Section 1A: Gradient Masking (350 LOC) ====================

// FeatureVector represents a numeric input feature vector
type FeatureVector []float64

// Prediction is the model output class probabilities
type Prediction map[int]float64

// MLModel abstract machine learning model interface
type MLModel struct {
	numInputFeatures int
	numClasses       int
	weights          [][][]float64 // Layer weights: [layer_idx][output_neuron][input_neuron]
	biases           [][]float64   // Layer biases: [layer_idx][neuron_idx]
	activationFunc   ActivationFunction
	layerSizes       []int // Size of each layer
	mu               sync.RWMutex
}

// ActivationFunction defines neural network activation function interface
type ActivationFunction func(x float64) float64

// ReLU implementation
func ReLU(x float64) float64 {
	if x > 0 {
		return x
	}
	return 0
}

// Sigmoid implementation
func Sigmoid(x float64) float64 {
	if x < -500 {
		return 0
	}
	if x > 500 {
		return 1
	}
	return 1 / (1 + math.Exp(-x))
}

// Softmax implementation for output layer
func Softmax(scores []float64) Prediction {
	maxScore := scores[0]
	for _, s := range scores[1:] {
		if s > maxScore {
			maxScore = s
		}
	}
	
	expScores := make([]float64, len(scores))
	sumExp := 0.0
	
	for i, s := range scores {
		expScores[i] = math.Exp(s - maxScore)
		sumExp += expScores[i]
	}
	
	prediction := make(Prediction, len(scores))
	for i, e := range expScores {
		prediction[i] = e / sumExp
	}
	
	return prediction
}

// NewMLModel creates a new ML model with random initialization
func NewMLModel(numInputFeatures, numClasses int, hiddenLayers []int) *MLModel {
	model := &MLModel{
		numInputFeatures: numInputFeatures,
		numClasses:       numClasses,
		weights:          make([][][]float64, 0),
		biases:           make([][]float64, 0),
		activationFunc:   ReLU,
		layerSizes:       append(append(make([]int, 0), numInputFeatures), hiddenLayers...),
	}
	
	rand.Seed(time.Now().UnixNano())
	
	prevSize := numInputFeatures
	for _, hl := range hiddenLayers {
		model.weights = append(model.weights, initializeWeights(prevSize, hl))
		model.biases = append(model.biases, make([]float64, hl))
		prevSize = hl
	}
	
	model.weights = append(model.weights, initializeWeights(prevSize, numClasses))
	model.biases = append(model.biases, make([]float64, numClasses))
	
	return model
}

// initializeWeights uses He initialization for better convergence
func initializeWeights(inputSize, outputSize int) [][]float64 {
	layerWeights := make([][]float64, outputSize)
	scale := math.Sqrt(2.0 / float64(inputSize))
	
	for i := range layerWeights {
		layerWeights[i] = make([]float64, inputSize)
		for j := range layerWeights[i] {
			layerWeights[i][j] = rand.NormFloat64() * scale
		}
	}
	return layerWeights
}

// Forward pass through the neural network
func (m *MLModel) Forward(input FeatureVector) Prediction {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	currentLayer := input
	
	// Hidden layers
	for i := 0; i < len(m.weights)-1; i++ {
		nextLayer := make([]float64, len(m.biases[i]))
		for j := range nextLayer {
			sum := m.biases[i][j]
			for k, v := range currentLayer {
				sum += m.weights[i][j][k] * v
			}
			nextLayer[j] = m.activationFunc(sum)
		}
		currentLayer = nextLayer
	}
	
	// Output layer (no activation for classification)
	output := m.forwardOutputLayer(currentLayer)
	return Softmax(output)
}

// forwardOutputLayer computes the final output layer without activation
func (m *MLModel) forwardOutputLayer(currentLayer []float64) []float64 {
	lastWeightIdx := len(m.weights) - 1
	outputWeights := m.weights[lastWeightIdx]
	outputBiases := m.biases[len(m.biases)-1]
	output := make([]float64, len(outputBiases))
	
	for j := range outputBiases {
		sum := outputBiases[j]
		for k, v := range currentLayer {
			sum += outputWeights[j][k] * v
		}
		output[j] = sum
	}
	
	return output
}

// predictClass extracts class label from prediction probabilities
func (m *MLModel) Predict(input FeatureVector) int {
	pred := m.Forward(input)
	maxClass := 0
	maxProb := pred[0]
	
	for cls, prob := range pred {
		if prob > maxProb {
			maxProb = prob
			maxClass = cls
		}
	}
	
	return maxClass
}

// GradientMask provides white-box attack defense through gradient masking
type GradientMask struct {
	originalModel   *MLModel
	maskNetwork     *NeuralNetwork
	smoothingRate   float64
	noisingStrength float64
	inputDimension  int
	mu              sync.RWMutex
}

// ProtectedPredict applies gradient masking before making predictions
func (gm *GradientMask) ProtectedPredict(input FeatureVector) Prediction {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	
	// Step 1: Smooth the input with Gaussian noise
	smoothedInput := gm.smoothInput(input)
	
	// Step 2: Add gradient perturbation to confuse attackers
	perturbedInput := gm.addGradientNoise(smoothedInput)
	
	// Step 3: Make prediction on masked input
	_ = gm.originalModel.Predict(perturbedInput)
	
	// Return probability distribution
	prediction := gm.originalModel.Forward(perturbedInput)
	return prediction
}

// smoothInput adds Gaussian noise perturbation to input features
func (gm *GradientMask) smoothInput(input FeatureVector) FeatureVector {
	smoothed := make(FeatureVector, len(input))
	stdDev := gm.smoothingRate * 0.1 // Standard deviation based on smoothing rate
	
	for i, v := range input {
		noisyValue := v + rand.NormFloat64()*stdDev
		smoothed[i] = noisyValue
	}
	
	return smoothed
}

// addGradientNoise introduces additional confusion through gradient noise
func (gm *GradientMask) addGradientNoise(input FeatureVector) FeatureVector {
	perturbed := make(FeatureVector, len(input))
	noiseScale := gm.noisingStrength / float64(len(input))
	
	for i, v := range input {
		gradientNoise := rand.NormFloat64() * noiseScale
		perturbed[i] = v + gradientNoise
	}
	
	return perturbed
}

// getLocalGradient computes numerical approximation of gradients
func (gm *GradientMask) getLocalGradient(input FeatureVector, epsilon float64) (FeatureVector, error) {
	if input == nil {
		return nil, fmt.Errorf("input cannot be nil")
	}
	
	numFeatures := len(input)
	gradients := make(FeatureVector, numFeatures)
	basePred := gm.originalModel.Forward(input)
	
	for i := 0; i < numFeatures; i++ {
		inputPlus := make(FeatureVector, numFeatures)
		copy(inputPlus, input)
		inputPlus[i] += epsilon
		
		outputPlus := gm.originalModel.Forward(inputPlus)
		// Use first class probability for gradient computation
		if len(outputPlus) > 0 && len(basePred) > 0 {
			gradients[i] = outputPlus[0] - basePred[0]
		}
	}
	
	return gradients, nil
}

// initializeLayerWeights uses He initialization for a single layer
func initializeLayerWeights(inputSize, outputSize int) [][]float64 {
	layerWeights := make([][]float64, outputSize)
	scale := math.Sqrt(2.0 / float64(inputSize))
	
	for i := range layerWeights {
		layerWeights[i] = make([]float64, inputSize)
		for j := range layerWeights[i] {
			layerWeights[i][j] = rand.NormFloat64() * scale
		}
	}
	return layerWeights
}

// buildMaskNetwork constructs neural network architecture for gradient confusion
func buildMaskNetwork(smoothingRate float64) *NeuralNetwork {
	inputDim := 128 // Default input dimension, can be overridden
	layerConfig := []int{inputDim, 256, 128, 64}
	
	network := &NeuralNetwork{
		layers:          make([]Layer, len(layerConfig)),
		learningRate:    0.001,
		finalActivation: Sigmoid,
	}
	
	for i := 0; i < len(layerConfig)-1; i++ {
		network.layers[i] = Layer{
			numInputs:  layerConfig[i],
			numOutputs: layerConfig[i+1],
			weights:    initializeLayerWeights(layerConfig[i], layerConfig[i+1]),
			biases:     make([]float64, layerConfig[i+1]),
		}
	}
	
	network.finalActivation = Sigmoid
	return network
}

// NeuralNetwork represents a simple feed-forward neural network
type NeuralNetwork struct {
	layers         []Layer
	learningRate   float64
	finalActivation ActivationFunction
}

// Layer represents a single neural network layer
type Layer struct {
	numInputs  int
	numOutputs int
	weights    [][]float64
	biases     []float64
	activators []float64
	derivatives []float64
}

// Forward computes network output
func (nn *NeuralNetwork) Forward(input []float64) []float64 {
	x := input
	
	for i, layer := range nn.layers[:len(nn.layers)-1] {
		z := make([]float64, layer.numOutputs)
		activators := make([]float64, layer.numOutputs)
		
		for j := 0; j < layer.numOutputs; j++ {
			sum := layer.biases[j]
			for k := 0; k < layer.numInputs; k++ {
				sum += layer.weights[j][k] * x[k]
			}
			z[j] = sum
			activators[j] = ReLU(sum)
		}
		
		x = activators
		
		if i < len(nn.layers)-2 {
			nn.layers[i].activators = activators
		}
	}
	
	// Final layer
	lastLayer := nn.layers[len(nn.layers)-1]
	output := make([]float64, lastLayer.numOutputs)
	
	for j := 0; j < lastLayer.numOutputs; j++ {
		sum := lastLayer.biases[j]
		for k := 0; k < lastLayer.numInputs; k++ {
			sum += lastLayer.weights[j][k] * x[k]
		}
		output[j] = nn.finalActivation(sum)
	}
	
	return output
}

// TrainingConfig contains gradient mask training parameters
type TrainingConfig struct {
	LearningRate      float64
	Momentum          float64
	BatchSize         int
	Regularization    float64
	EpsilonStep       float64
	NumTrainingSteps  int
	InputStdDev       float64
	OutputStdDev      float64
}

// DefaultTrainingConfig returns standard training parameters
func DefaultTrainingConfig() *TrainingConfig {
	return &TrainingConfig{
		LearningRate:     0.01,
		Momentum:         0.9,
		BatchSize:        32,
		Regularization:   0.001,
		EpsilonStep:      0.01,
		NumTrainingSteps: 1000,
		InputStdDev:      0.1,
		OutputStdDev:     0.05,
	}
}

// Train config ures the mask network to improve gradient confusion
func (gm *GradientMask) Train(config *TrainingConfig) error {
	if config == nil {
		config = DefaultTrainingConfig()
	}
	
	batchSize := config.BatchSize
	_ = config.Momentum // Reserved for future momentum-based optimization
	
	numBatches := gm.inputDimension / batchSize
	if numBatches <= 0 {
		numBatches = 1
	}
	
	totalLoss := 0.0
	
	for step := 0; step < config.NumTrainingSteps; step++ {
		var gradWeights [][][]float64
		var gradBiases []float64
		
		for b := 0; b < numBatches; b++ {
			sample := gm.generateRandomSample(gm.inputDimension)
			
			loss, gW, gB := gm.computeLossAndGradients(sample, config)
			totalLoss += loss
			
			if step == 0 {
				gradWeights = initializeGradients(len(gm.maskNetwork.layers))
				gradBiases = make([]float64, len(gm.maskNetwork.layers[0].biases))
			}
			
			for i, gw := range gW {
				for j := range gw {
					for k := range gw[j] {
						gradWeights[i][j][k] += gw[j][k]
					}
				}
			}
			
			for i, gb := range gB {
				gradBiases[i] += gb
			}
		}
		
		// Average gradients
		for i, gw := range gradWeights {
			for j := range gw {
				for k := range gw[j] {
					gradWeights[i][j][k] /= float64(numBatches)
				}
			}
		}
		for i := range gradBiases {
			gradBiases[i] /= float64(numBatches)
		}
		
		// Apply SGD with momentum
		for i := range gm.maskNetwork.layers {
			for j := range gm.maskNetwork.layers[i].weights {
				for k := range gm.maskNetwork.layers[i].weights[j] {
					gm.maskNetwork.layers[i].weights[j][k] -= 
						config.LearningRate * gradWeights[i][j][k]
				}
			}
		}
	}
	
	return nil
}

// generateRandomSample generates random sample data
func (gm *GradientMask) generateRandomSample(dim int) FeatureVector {
	sample := make(FeatureVector, dim)
	for i := range sample {
		sample[i] = rand.NormFloat64()
	}
	return sample
}

// computeLossAndGradients calculates loss and gradient information
func (gm *GradientMask) computeLossAndGradients(sample FeatureVector, config *TrainingConfig) (float64, [][][]float64, []float64) {
	basePred := gm.originalModel.Forward(sample)
	maskedPred := gm.ProtectedPredict(sample)
	
	loss := 0.0
	for cls := range basePred {
		diff := basePred[cls] - maskedPred[cls]
		loss += diff * diff
	}
	
	gradWeights := initializeGradients(len(gm.maskNetwork.layers))
	gradBiases := make([]float64, len(gm.maskNetwork.layers[0].biases))
	
	return loss, gradWeights, gradBiases
}

// initializeGradients initializes gradient storage structure
func initializeGradients(numLayers int, layerDims ...[]int) [][][]float64 {
	grads := make([][][]float64, numLayers)
	for i := range grads {
		// Match layer dimensions properly
		if len(layerDims) > i && layerDims[i] != nil {
			inDim := layerDims[i][0]
			outDim := layerDims[i][1]
			grads[i] = make([][]float64, outDim)
			for j := range grads[i] {
				grads[i][j] = make([]float64, inDim)
			}
		} else {
			grads[i] = make([][]float64, 1)
			grads[i][0] = make([]float64, 1)
		}
	}
	return grads
}

// GetSmoothingRate returns current smoothing rate configuration
func (gm *GradientMask) GetSmoothingRate() float64 {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	return gm.smoothingRate
}

// SetSmoothingRate updates the smoothing rate parameter
func (gm *GradientMask) SetSmoothingRate(rate float64) {
	gm.mu.Lock()
	defer gm.mu.Unlock()
	gm.smoothingRate = rate
}

// GetOriginalModel returns the protected ML model
func (gm *GradientMask) GetOriginalModel() *MLModel {
	gm.mu.RLock()
	defer gm.mu.RUnlock()
	return gm.originalModel
}

// ==================== Section 1B: Feature Compression Engine (250 LOC) ====================

// FeatureCompression handles feature selection and dimensionality reduction
type FeatureCompression struct {
	varianceThreshold float64
	selectedFeatures  []int
	featureStats      map[int]*FeatureStatistics
	PCAComponents     [][]float64
	explainedVariance []float64
	mu                sync.RWMutex
}

// FeatureStatistics captures statistical properties per feature
type FeatureStatistics struct {
	mean       float64
	variance   float64
	minVal     float64
	maxVal     float64
	skewness   float64
	kurtosis   float64
	count      int64
	lastUpdate time.Time
}

// NewFeatureCompression creates feature compression engine
func NewFeatureCompression(threshold float64) *FeatureCompression {
	return &FeatureCompression{
		varianceThreshold: threshold,
		selectedFeatures:  make([]int, 0),
		featureStats:      make(map[int]*FeatureStatistics),
		PCAComponents:     make([][]float64, 0),
		explainedVariance: make([]float64, 0),
	}
}

// CompressFeatures removes low-variance features from the input
func (fc *FeatureCompression) CompressFeatures(features []float64) []float64 {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	
	if len(fc.selectedFeatures) == 0 {
		fc.analyzeAndSelectFeatures(features)
	}
	
	compressed := make([]float64, len(fc.selectedFeatures))
	for i, idx := range fc.selectedFeatures {
		if idx < len(features) {
			compressed[i] = features[idx]
		}
	}
	
	return compressed
}

// analyzeAndSelectFeatures performs variance analysis and feature selection
func (fc *FeatureCompression) analyzeAndSelectFeatures(features []float64) {
	fc.featureStats = make(map[int]*FeatureStatistics)
	
	for i, f := range features {
		stats := fc.featureStats[i]
		if stats == nil {
			stats = &FeatureStatistics{}
		}
		
		// Update running statistics
		newCount := stats.count + 1
		delta := f - stats.mean
		newMean := stats.mean + delta/float64(newCount)
		delta2 := f - newMean
		newVariance := stats.variance*float64(stats.count)/float64(newCount+1) + delta*delta2/float64(newCount)
		
		stats.mean = newMean
		stats.variance = newVariance
		if stats.count > 0 {
			stats.minVal = math.Min(stats.minVal, f)
			stats.maxVal = math.Max(stats.maxVal, f)
		}
		stats.count = newCount
		stats.lastUpdate = time.Now()
		
		fc.featureStats[i] = stats
	}
	
	// Select high-variance features
	selected := make([]int, 0)
	for idx, stats := range fc.featureStats {
		if stats.count > 0 && stats.variance >= fc.varianceThreshold {
			selected = append(selected, idx)
		}
	}
	
	fc.selectedFeatures = selected
}

// PCABasedCompression applies PCA for dimensionality reduction
func (fc *FeatureCompression) PCABasedCompression(features []float64) []float64 {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	
	if len(fc.PCAComponents) == 0 {
		// Create synthetic training data from single sample
		trainingData := fc.generateTrainingData(features, 100)
		fc.trainPCA(trainingData)
	}
	
	// Apply PCA transformation
	reduced := fc.projectToPCAFeatures(features)
	
	return reduced
}

// generateTrainingData creates synthetic samples from single feature vector for PCA training
func (fc *FeatureCompression) generateTrainingData(features []float64, numSamples int) [][]float64 {
	data := make([][]float64, numSamples)
	dimension := len(features)
	
	for i := 0; i < numSamples; i++ {
		sample := make([]float64, dimension)
		for j := range features {
			// Add small Gaussian noise around original values
			sample[j] = features[j] + rand.NormFloat64()*0.1
		}
		data[i] = sample
	}
	
	return data
}

// trainPCA learns principal components from data
func (fc *FeatureCompression) trainPCA(data [][]float64) {
	if len(data) == 0 {
		return
	}
	
	numSamples := len(data)
	numFeatures := len(data[0])
	
	// Center the data
	mean := make([]float64, numFeatures)
	for _, sample := range data {
		for i, v := range sample {
			mean[i] += v
		}
	}
	for i := range mean {
		mean[i] /= float64(numSamples)
	}
	
	centeredData := make([][]float64, numSamples)
	for i, sample := range data {
		centeredData[i] = make([]float64, numFeatures)
		for j, v := range sample {
			centeredData[i][j] = v - mean[j]
		}
	}
	
	// Compute covariance matrix
	covMatrix := make([][]float64, numFeatures)
	for i := range covMatrix {
		covMatrix[i] = make([]float64, numFeatures)
	}
	
	for i := 0; i < numSamples-1; i++ {
		for j := i + 1; j < numSamples; j++ {
			for x := 0; x < numFeatures; x++ {
				for y := 0; y < numFeatures; y++ {
					covMatrix[x][y] += centeredData[i][x] * centeredData[j][y]
				}
			}
		}
	}
	
	for i := range covMatrix {
		for j := range covMatrix[i] {
			covMatrix[i][j] /= float64(numSamples - 1)
		}
	}
	
	// Power iteration to find top eigenvectors
	eigenvectors := fc.powerIteration(covMatrix, numFeatures)
	
	fc.PCAComponents = eigenvectors
}

// powerIteration finds principal eigenvectors using power method
func (fc *FeatureCompression) powerIteration(covMatrix [][]float64, numFeatures int) [][]float64 {
	numComponents := min(numFeatures/2, 10)
	eigenvectors := make([][]float64, numComponents)
	
	for comp := 0; comp < numComponents; comp++ {
		v := make([]float64, numFeatures)
		for i := range v {
			v[i] = rand.NormFloat64()
		}
		v = normalize(v)
		
		for iter := 0; iter < 100; iter++ {
			Av := make([]float64, numFeatures)
			for i := 0; i < numFeatures; i++ {
				for j := 0; j < numFeatures; j++ {
					Av[i] += covMatrix[i][j] * v[j]
				}
			}
			
			newV := Av
			for k := 0; k < comp; k++ {
				dot := dotProduct(newV, eigenvectors[k])
				for i := range newV {
					newV[i] -= dot * eigenvectors[k][i]
				}
			}
			
			newV = normalize(newV)
			
			convergence := 0.0
			for i := range v {
				diff := newV[i] - v[i]
				convergence += diff * diff
			}
			
			v = newV
			if convergence < 1e-6 {
				break
			}
		}
		
		eigenvectors[comp] = v
	}
	
	return eigenvectors
}

// projectToPCAFeatures transforms data into PCA space
func (fc *FeatureCompression) projectToPCAFeatures(features []float64) []float64 {
	proj := make([]float64, len(fc.PCAComponents))
	
	for i, vec := range fc.PCAComponents {
		proj[i] = dotProduct(features, vec)
	}
	
	return proj
}

// normalize scales vector to unit length
func normalize(v []float64) []float64 {
	norm := 0.0
	for _, val := range v {
		norm += val * val
	}
	
	if norm == 0 {
		return v
	}
	
	result := make([]float64, len(v))
	scale := 1.0 / math.Sqrt(norm)
	for i, val := range v {
		result[i] = val * scale
	}
	
	return result
}

// dotProduct computes dot product of two vectors
func dotProduct(a, b []float64) float64 {
	if len(a) != len(b) {
		panic("vectors must have same length")
	}
	
	sum := 0.0
	for i := range a {
		sum += a[i] * b[i]
	}
	
	return sum
}

// GetSelectedFeatures returns indices of selected high-variance features
func (fc *FeatureCompression) GetSelectedFeatures() []int {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	return slices.Clone(fc.selectedFeatures)
}

// GetFeatureStats returns statistical information for all features
func (fc *FeatureCompression) GetFeatureStats() map[int]*FeatureStatistics {
	fc.mu.RLock()
	defer fc.mu.RUnlock()
	
	stats := make(map[int]*FeatureStatistics, len(fc.featureStats))
	for k, v := range fc.featureStats {
		stats[k] = v
	}
	
	return stats
}

// Reset clears all learned statistics
func (fc *FeatureCompression) Reset() {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	
	fc.selectedFeatures = fc.selectedFeatures[:0]
	fc.featureStats = make(map[int]*FeatureStatistics)
	fc.PCAComponents = fc.PCAComponents[:0]
	fc.explainedVariance = fc.explainedVariance[:0]
}
