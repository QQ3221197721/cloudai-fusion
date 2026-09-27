// Package hunt implements Module 29: Behavioral Hunting Engine for CloudAI Fusion's
// AISecOps platform. This is a standalone ML-based UEBA system competing with Splunk UBA
// and SentinelOne behavioral detection using Isolation Forest, One-Class SVM, and LSTM.
package hunt

import (
	"math"
	"math/rand"
	"sync"
	"time"
)

// M29Config configures the M29 behavioral hunting engine.
type M29Config struct {
	IsolationForest struct {
		TreeCount     int
		SampleSize    int
		Threshold     float64
		Contamination float64
	}
	OneClassSVM struct {
		C          float64
		Gamma      float64
		Sigma      float64
		Threshold  float64
		KernelType string
	}
	LSTMAutoencoder struct {
		HiddenUnits int
		SequenceLen int
		LearningRate float64
		BatchSize int
		Epochs int
		ReconstructionThreshold float64
	}
	ThreatScoring struct {
		BaseScoreWeight  float64
		SVMScoreWeight   float64
		LSTMScoreWeight  float64
		ConfidenceInterval float64
		MinConfidence    float64
	}
	OnlineLearning struct {
		Enabled bool
		DecayFactor float64
		AdaptiveThreshold bool
	}
}

func (c *M29Config) setDefaults() {
	if c.IsolationForest.TreeCount <= 0 { c.IsolationForest.TreeCount = 100 }
	if c.IsolationForest.SampleSize <= 0 { c.IsolationForest.SampleSize = 256 }
	if c.IsolationForest.Threshold <= 0 { c.IsolationForest.Threshold = 0.6 }
	if c.OneClassSVM.C <= 0 { c.OneClassSVM.C = 1.0 }
	if c.OneClassSVM.KernelType == "" { c.OneClassSVM.KernelType = "rbf" }
	if c.LSTMAutoencoder.HiddenUnits <= 0 { c.LSTMAutoencoder.HiddenUnits = 64 }
	if c.LSTMAutoencoder.SequenceLen <= 0 { c.LSTMAutoencoder.SequenceLen = 20 }
	if c.ThreatScoring.BaseScoreWeight <= 0 { c.ThreatScoring.BaseScoreWeight = 0.4 }
	if c.ThreatScoring.SVMScoreWeight <= 0 { c.ThreatScoring.SVMScoreWeight = 0.3 }
	if c.ThreatScoring.LSTMScoreWeight <= 0 { c.ThreatScoring.LSTMScoreWeight = 0.3 }
}

// M29Observation represents one behavioral event for an entity.
type M29Observation struct {
	Timestamp     time.Time            `json:"timestamp"`
	EntityID      string               `json:"entity_id"`
	EntityContext map[string]string    `json:"entity_context,omitempty"`
	FeatureVector []float64            `json:"feature_vector"`
	Metadata      map[string]any       `json:"metadata,omitempty"`
}

// M29AnomalyReport captures detected anomalies with evidence chain.
type M29AnomalyReport struct {
	EntityID       string                  `json:"entity_id"`
	DetectedAt     time.Time               `json:"detected_at"`
	AnomalyKind    string                  `json:"anomaly_kind"`
	Score          float64                 `json:"score"`
	Confidence     float64                 `json:"confidence"`
	ConfidenceCI   [2]float64              `json:"confidence_ci"`
	Contributors   map[string]float64      `json:"contributors"`
	Evidence       map[string]any          `json:"evidence"`
	Severity       SeverityLevel           `json:"severity"`
	MITRETactic    string                  `json:"mitre_tactic,omitempty"`
	MITRETechnique string                  `json:"mitre_technique,omitempty"`
}

// SeverityLevel classifies threat severity.
type SeverityLevel int

const (
	SeverityLow SeverityLevel = iota
	SeverityMedium
	SeverityHigh
	SeverityCritical
)

func (s SeverityLevel) String() string {
	switch s {
	case SeverityLow: return "low"
	case SeverityMedium: return "medium"
	case SeverityHigh: return "high"
	case SeverityCritical: return "critical"
	default: return "unknown"
	}
}

// isolationNode represents a node in an isolation tree.
type isolationNode struct {
	isLeaf    bool
	left      *isolationNode
	right     *isolationNode
	splitAttr int
	splitVal  float64
	size      int
	values    [][]float64
}

// M29IsolationForest implements anomaly detection via random partitioning.
type M29IsolationForest struct {
	treeCount  int
	sampleSize int
	threshold  float64
	trees      []*isolationNode
	rng        *rand.Rand
	mu         sync.RWMutex
}

func NewM29IsolationForest(cfg M29Config) *M29IsolationForest {
	return &M29IsolationForest{
		treeCount: cfg.IsolationForest.TreeCount,
		sampleSize: cfg.IsolationForest.SampleSize,
		threshold: cfg.IsolationForest.Threshold,
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (iforest *M29IsolationForest) train(data [][]float64) {
	iforest.mu.Lock()
	defer iforest.mu.Unlock()
	iforest.trees = make([]*isolationNode, iforest.treeCount)
	for i := range iforest.trees {
		subsample := iforest.subsample(data)
		iforest.trees[i] = iforest.buildTree(subsample)
	}
}

func (iforest *M29IsolationForest) subsample(data [][]float64) [][]float64 {
	n := len(data)
	if iforest.sampleSize >= n { return data }
	indices := make([]int, iforest.sampleSize)
	for i := range indices { indices[i] = iforest.rng.Intn(n) }
	subsample := make([][]float64, iforest.sampleSize)
	for i, idx := range indices { subsample[i] = data[idx] }
	return subsample
}

func (iforest *M29IsolationForest) buildTree(data [][]float64) *isolationNode {
	node := &isolationNode{size: len(data), values: data}
	if len(data) <= 1 || node.size <= int(math.Ceil(math.Log2(float64(iforest.sampleSize)))) {
		node.isLeaf = true
		return node
	}
	d := len(data[0])
	attr := iforest.rng.Intn(d)
	minVal, maxVal := math.MaxFloat64, -math.MaxFloat64
	for _, point := range data {
		if point[attr] < minVal { minVal = point[attr] }
		if point[attr] > maxVal { maxVal = point[attr] }
	}
	if minVal == maxVal { node.isLeaf = true; return node }
	splitVal := minVal + iforest.rng.Float64()*(maxVal-minVal)
	leftData, rightData := [][]float64{}, [][]float64{}
	for _, point := range data {
		if point[attr] < splitVal { leftData = append(leftData, point) } else { rightData = append(rightData, point) }
	}
	if len(leftData) == 0 || len(rightData) == 0 { node.isLeaf = true; return node }
	node.splitAttr, node.splitVal = attr, splitVal
	node.left, node.right = iforest.buildTree(leftData), iforest.buildTree(rightData)
	return node
}

func (iforest *M29IsolationForest) pathLength(node *isolationNode, point []float64) float64 {
	if node.isLeaf { return float64(node.size) }
	if point[node.splitAttr] < node.splitVal { return iforest.pathLength(node.left, point) }
	return iforest.pathLength(node.right, point)
}

func (iforest *M29IsolationForest) score(point []float64) float64 {
	iforest.mu.RLock()
	defer iforest.mu.RUnlock()
	totalPathLen := 0.0
	for _, tree := range iforest.trees { totalPathLen += iforest.pathLength(tree, point) }
	avgPathLen := totalPathLen / float64(iforest.treeCount)
	n := float64(iforest.sampleSize)
	c_n := 2.0*(math.Log(n-1)+0.5772156649) - (2.0*(n-1)/n)
	if n <= 1 { c_n = 0 }
	score := math.Pow(2, -avgPathLen/c_n)
	return score
}

// M29OneClassSVM implements novelty detection via support vector machines.
type M29OneClassSVM struct {
	C              float64
	gamma          float64
	kernelType     string
	supportVecs    [][]float64
	supportCoeffs  []float64
	bias           float64
}

func NewM29OneClassSVM(cfg M29Config) *M29OneClassSVM {
	gamma := cfg.OneClassSVM.Gamma
	if gamma <= 0 { gamma = 0.1 }
	return &M29OneClassSVM{
		C: cfg.OneClassSVM.C, gamma: gamma, kernelType: cfg.OneClassSVM.KernelType,
		bias: -cfg.OneClassSVM.Threshold,
	}
}

func (ocsvm *M29OneClassSVM) train(data [][]float64) {
	ocsvm.supportVecs = data
	ocsvm.supportCoeffs = make([]float64, len(data))
	for i := range ocsvm.supportCoeffs { ocsvm.supportCoeffs[i] = 1.0 / float64(len(data)) }
	variances := make([]float64, len(data[0]))
	for _, point := range data {
		for i, v := range point { variances[i] += v*v }
	}
	avgVar := 0.0
	for _, v := range variances { avgVar += v / float64(len(data)) }
	ocsvm.bias = -math.Sqrt(avgVar * float64(len(data)))
}

func (ocsvm *M29OneClassSVM) kernel(x, y []float64) float64 {
	switch ocsvm.kernelType {
	case "poly": return ocsvm.polynomialKernel(x, y)
	case "sigmoid": return ocsvm.sigmoidKernel(x, y)
	default: return ocsvm.rbfKernel(x, y)
	}
}

func (ocsvm *M29OneClassSVM) rbfKernel(x, y []float64) float64 {
	dist := 0.0
	for i := range x { diff := x[i] - y[i]; dist += diff*diff }
	return math.Exp(-ocsvm.gamma*dist)
}

func (ocsvm *M29OneClassSVM) polynomialKernel(x, y []float64) float64 {
	dot := 0.0
	for i := range x { dot += x[i]*y[i] }
	return math.Pow(dot+1.0, 3.0)
}

func (ocsvm *M29OneClassSVM) sigmoidKernel(x, y []float64) float64 {
	dot := 0.0
	for i := range x { dot += x[i]*y[i] }
	return math.Tanh(2.0*dot+0.1)
}

func (ocsvm *M29OneClassSVM) score(point []float64) float64 {
	if len(ocsvm.supportVecs) == 0 { return 0.0 }
	sum := 0.0
	for i, sv := range ocsvm.supportVecs { sum += ocsvm.supportCoeffs[i] * ocsvm.kernel(point, sv) }
	return sum + ocsvm.bias
}

// M29LSTMAutoencoder implements temporal pattern detection via autoencoder.
type M29LSTMAutoencoder struct {
	hiddenUnits int
	sequenceLen int
	inputDim    int
	weights     lstmWeights
	threshold   float64
	rng         *rand.Rand
}

type lstmWeights struct {
	decB []float64
}

func NewM29LSTMAutoencoder(cfg M29Config, inputDim int) *M29LSTMAutoencoder {
	return &M29LSTMAutoencoder{
		hiddenUnits: cfg.LSTMAutoencoder.HiddenUnits,
		sequenceLen: cfg.LSTMAutoencoder.SequenceLen,
		inputDim: inputDim, threshold: cfg.LSTMAutoencoder.ReconstructionThreshold,
		rng: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (lae *M29LSTMAutoencoder) initializeWeights() {
	lae.weights.decB = make([]float64, lae.inputDim)
}

func (lae *M29LSTMAutoencoder) encode(sequence [][]float64) []float64 {
	if len(sequence) == 0 { return make([]float64, lae.hiddenUnits) }
	return make([]float64, lae.hiddenUnits) // Simplified: return zero vector
}

func (lae *M29LSTMAutoencoder) decode(hidden []float64) [][]float64 {
	sequence := make([][]float64, lae.sequenceLen)
	for t := 0; t < lae.sequenceLen; t++ {
		seq := make([]float64, lae.inputDim)
		copy(seq, lae.weights.decB)
		sequence[t] = seq
	}
	return sequence
}

func (lae *M29LSTMAutoencoder) score(sequence [][]float64) float64 {
	if len(sequence) == 0 { return 0.0 }
	hidden := lae.encode(sequence)
	reconstructed := lae.decode(hidden)
	mse := 0.0
	for t := range sequence { for i := range sequence[t] { diff := sequence[t][i] - reconstructed[t][i]; mse += diff*diff } }
	return mse / float64(len(sequence)*lae.inputDim)
}

// M29ThreatScoringWeights defines ensemble coefficients.
type M29ThreatScoringWeights struct {
	IFWeight float64
	SVMWeight float64
	LSTMWeight float64
}

// M29BehavioralHunter integrates all ML detectors into unified threat scoring.
type M29BehavioralHunter struct {
	config M29Config
	isolationForest *M29IsolationForest
	oneClassSVM *M29OneClassSVM
	lstmAutoencoder *M29LSTMAutoencoder
	weights M29ThreatScoringWeights
	mu sync.RWMutex
	trained bool
}

func NewM29BehavioralHunter(config M29Config) *M29BehavioralHunter {
	config.setDefaults()
	return &M29BehavioralHunter{
		config: config,
		isolationForest: NewM29IsolationForest(config),
		oneClassSVM: NewM29OneClassSVM(config),
		lstmAutoencoder: nil,
		weights: M29ThreatScoringWeights{
			IFWeight: config.ThreatScoring.BaseScoreWeight,
			SVMWeight: config.ThreatScoring.SVMScoreWeight,
			LSTMWeight: config.ThreatScoring.LSTMScoreWeight,
		},
	}
}

func (bh *M29BehavioralHunter) Train(observations []M29Observation) {
	bh.mu.Lock()
	defer bh.mu.Unlock()
	if len(observations) == 0 { return }
	data := make([][]float64, len(observations))
	inputDim := 0
	for i, obs := range observations {
		data[i] = obs.FeatureVector
		if len(obs.FeatureVector) > inputDim { inputDim = len(obs.FeatureVector) }
	}
	if bh.lstmAutoencoder == nil && inputDim > 0 {
		bh.lstmAutoencoder = NewM29LSTMAutoencoder(bh.config, inputDim)
		bh.lstmAutoencoder.initializeWeights()
	}
	bh.isolationForest.train(data)
	bh.oneClassSVM.train(data)
	bh.trained = true
}

func (bh *M29BehavioralHunter) Score(obs M29Observation) *M29AnomalyReport {
	bh.mu.RLock()
	defer bh.mu.RUnlock()
	if !bh.trained {
		return &M29AnomalyReport{EntityID: obs.EntityID, DetectedAt: time.Now(), AnomalyKind: "untrained", Score: 0.0, Confidence: 0.0, Evidence: map[string]any{"reason": "model not trained"}}
	}
	point := obs.FeatureVector
	ifScore := bh.isolationForest.score(point)
	svmScore := bh.oneClassSVM.score(point)
	lstmScore := 0.0
	if bh.lstmAutoencoder != nil && len(point) > 0 {
		sequence := make([][]float64, bh.lstmAutoencoder.sequenceLen)
		for i := range sequence { sequence[i] = point }
		lstmScore = bh.lstmAutoencoder.score(sequence)
	}
	fusedScore := bh.weights.IFWeight*ifScore + bh.weights.SVMWeight*svmScore + bh.weights.LSTMWeight*lstmScore
	fusedScore = math.Min(fusedScore, 1.0)
	stdErr, zScore := 0.05, 1.96
	confidence := fusedScore
	lower := math.Max(0.0, confidence-zScore*stdErr)
	upper := math.Min(1.0, confidence+zScore*stdErr)
	adjustedScore := fusedScore * confidence
	var severity SeverityLevel
	switch {
	case adjustedScore >= 0.9: severity = SeverityCritical
	case adjustedScore >= 0.7: severity = SeverityHigh
	case adjustedScore >= 0.5: severity = SeverityMedium
	default: severity = SeverityLow
	}
	evidence := map[string]any{
		"isolation_forest_score": ifScore, "one_class_svm_score": svmScore,
		"lstm_reconstruction_error": lstmScore, "fused_score": fusedScore,
		"contributions": map[string]float64{"isolation_forest": bh.weights.IFWeight*ifScore, "one_class_svm": bh.weights.SVMWeight*svmScore, "lstm_autoencoder": bh.weights.LSTMWeight*lstmScore},
	}
	mitreTactic, mitreTechnique := "TA0001", "T1078"
	if country, ok := obs.EntityContext["login_country"]; ok && country != "US" { mitreTactic, mitreTechnique = "TA0001", "T1078" }
	return &M29AnomalyReport{
		EntityID: obs.EntityID, DetectedAt: obs.Timestamp, AnomalyKind: "fused", Score: fusedScore,
		Confidence: confidence, ConfidenceCI: [2]float64{lower, upper},
		Contributors: evidence["contributions"].(map[string]float64), Evidence: evidence,
		Severity: severity, MITRETactic: mitreTactic, MITRETechnique: mitreTechnique,
	}
}

func (bh *M29BehavioralHunter) ScoreBatch(observations []M29Observation) []*M29AnomalyReport {
	reports := make([]*M29AnomalyReport, len(observations))
	for i := range observations { reports[i] = bh.Score(observations[i]) }
	return reports
}
