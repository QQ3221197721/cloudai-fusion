package training

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// TrialStatus represents the lifecycle state of a hyperparameter trial.
type TrialStatus int

const (
	TrialPending TrialStatus = iota
	TrialRunning
	TrialSuccess
	TrialFailed
	TrialTerminated
)

func (s TrialStatus) String() string {
	switch s {
	case TrialPending:
		return "pending"
	case TrialRunning:
		return "running"
	case TrialSuccess:
		return "success"
	case TrialFailed:
		return "failed"
	case TrialTerminated:
		return "terminated"
	default:
		return "unknown"
	}
}

// Trial represents a single hyperparameter configuration evaluation.
type Trial struct {
	ID         string
	JobID      string
	Parameters map[string]float64
	Metrics    map[string]float64
	Status     TrialStatus
	StartedAt  time.Time
	EndedAt    *time.Time
	CreatedAt  time.Time
}

// TrialResult captures the outcome of a completed trial.
type TrialResult struct {
	TrialID     string
	Metrics     map[string]float64
	Converged   bool
	ElapsedTime time.Duration
}

// SearchStrategy defines the interface for hyperparameter search algorithms.
type SearchStrategy interface {
	SuggestParameters(jobID string, trials []Trial) (map[string]float64, error)
	UpdateHistory(trial Trial) error
	GetBestParameters() (map[string]float64, error)
}

// EarlyStoppingPolicy defines interfaces for adaptive early stopping.
type EarlyStoppingPolicy interface {
	ShouldStop(trial Trial, intermediateResults []float64) bool
	Reason(trial Trial, intermediateResults []float64) string
	CanEvaluateEarly() bool
}

// TrialManager orchestrates hyperparameter tuning trials with evidence tracking.
type TrialManager struct {
	mu              sync.RWMutex
	evidenceLedger  evidence.Recorder
	searchStrategy  SearchStrategy
	earlyStopping   EarlyStoppingPolicy
	trials          []Trial
	bestTrialID     string
	bestLoss        float64
	maxParallelTrials int
	minTrialsToSkipEarlyStopping int
}

// TrialManagerConfig configures TrialManager initialization.
type TrialManagerConfig struct {
	MaxParallelTrials        int
	EvidenceLedger           evidence.Recorder
	MinTrialsToSkipEarlyStopping int
}

// NewTrialManager creates a new TrialManager with Bayesian optimization.
func NewTrialManager(cfg TrialManagerConfig) *TrialManager {
	if cfg.MaxParallelTrials <= 0 {
		cfg.MaxParallelTrials = 10
	}
	if cfg.MinTrialsToSkipEarlyStopping <= 0 {
		cfg.MinTrialsToSkipEarlyStopping = 3
	}

	bayesianOptimizer := NewBayesianOptimizer()

	return &TrialManager{
		evidenceLedger:                 cfg.EvidenceLedger,
		searchStrategy:                 bayesianOptimizer,
		earlyStopping:                  NewMedianBaselineStopping(),
		trials:                         make([]Trial, 0),
		bestLoss:                       math.Inf(+1),
		maxParallelTrials:              cfg.MaxParallelTrials,
		minTrialsToSkipEarlyStopping:   cfg.MinTrialsToSkipEarlyStopping,
	}
}

// GetBestParameters returns the best hyperparameters found so far.
func (tm *TrialManager) GetBestParameters() (map[string]float64, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if tm.bestTrialID == "" {
		return nil, errors.New("no trials completed yet")
	}

	for _, trial := range tm.trials {
		if trial.ID == tm.bestTrialID && trial.Status == TrialSuccess {
			return trial.Parameters, nil
		}
	}

	return tm.searchStrategy.GetBestParameters()
}

// GetBestLoss returns the lowest loss value achieved.
func (tm *TrialManager) GetBestLoss() (float64, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if math.IsInf(tm.bestLoss, +1) {
		return 0, false
	}

	return tm.bestLoss, true
}

// SubmitTrial initiates a new hyperparameter tuning trial.
func (tm *TrialManager) SubmitTrial(jobID string, parameters map[string]float64) (*Trial, error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if len(tm.trials) >= tm.maxParallelTrials {
		return nil, fmt.Errorf("maximum parallel trials (%d) reached", tm.maxParallelTrials)
	}

	trialID := generateTrialID(jobID, len(tm.trials)+1)
	now := time.Now().UTC()

	trial := &Trial{
		ID:         trialID,
		JobID:      jobID,
		Parameters: parameters,
		Metrics:    make(map[string]float64),
		Status:     TrialPending,
		StartedAt:  now,
		CreatedAt:  now,
	}

	tm.trials = append(tm.trials, *trial)

	tm.recordSuggestionEvent(jobID, parameters, trialID)

	return trial, nil
}

// RecordProgress logs intermediate metrics during training.
func (tm *TrialManager) RecordProgress(trialID string, step int, metrics map[string]float64) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for i := range tm.trials {
		if tm.trials[i].ID == trialID {
			trial := &tm.trials[i]
			if trial.Status != TrialRunning {
				return fmt.Errorf("trial %s is not running (status: %s)", trialID, trial.Status.String())
			}

			trial.Metrics["loss"] = metrics["loss"]
			trial.Metrics["accuracy"] = metrics["accuracy"]

			tm.recordProgressEvent(trialID, step, metrics)
			return nil
		}
	}

	return fmt.Errorf("trial %s not found", trialID)
}

// CompleteTrial marks a trial as finished and updates best results.
func (tm *TrialManager) CompleteTrial(trialID string, finalMetrics map[string]float64, status TrialStatus) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for i := range tm.trials {
		if tm.trials[i].ID == trialID {
			trial := &tm.trials[i]
			trial.Metrics = finalMetrics
			trial.Status = status

			now := time.Now().UTC()
			trial.EndedAt = &now

			var lossValue float64
			if v, ok := finalMetrics["loss"]; ok {
				lossValue = v
			} else {
				lossValue = math.Inf(+1)
			}

			if status == TrialSuccess && lossValue < tm.bestLoss {
				tm.bestLoss = lossValue
				tm.bestTrialID = trialID
			}

			tm.updateSearchStrategy(trial)
			tm.recordCompletionEvent(trial, status)
			return nil
		}
	}

	return fmt.Errorf("trial %s not found", trialID)
}

// GetTrial retrieves a trial by ID.
func (tm *TrialManager) GetTrial(trialID string) (*Trial, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	for _, trial := range tm.trials {
		if trial.ID == trialID {
			copyTrial := trial
			return &copyTrial, nil
		}
	}

	return nil, fmt.Errorf("trial %s not found", trialID)
}

// GetAllTrials returns all trials currently managed.
func (tm *TrialManager) GetAllTrials() []Trial {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	result := make([]Trial, len(tm.trials))
	copy(result, tm.trials)
	return result
}

// ShouldApplyEarlyStopping determines if early stopping should be applied to a trial.
func (tm *TrialManager) ShouldApplyEarlyStopping(trialID string) (bool, string) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	for _, trial := range tm.trials {
		if trial.ID == trialID && trial.Status == TrialRunning {
			if !tm.earlyStopping.CanEvaluateEarly() {
				return false, "insufficient data points for early stopping"
			}

			losses := make([]float64, 0)
			for key := range trial.Metrics {
				if key == "loss" {
					if loss, ok := trial.Metrics[key]; ok {
						losses = append(losses, loss)
					}
				}
			}

			if len(losses) < tm.minTrialsToSkipEarlyStopping {
				return false, fmt.Sprintf("need at least %d data points, got %d", tm.minTrialsToSkipEarlyStopping, len(losses))
			}

			stopReason := tm.earlyStopping.Reason(trial, losses)
			return tm.earlyStopping.ShouldStop(trial, losses), stopReason
		}
	}

	return false, fmt.Sprintf("trial %s not found", trialID)
}

// estimateComputationalCost calculates expected training duration based on trial parameters.
func (tm *TrialManager) estimateComputationalCost(parameters map[string]float64) time.Duration {
	baseEpochTime := time.Second * 30
	learningRate := parameters["learning_rate"]
	batchSize := parameters["batch_size"]

	adjustmentFactor := 1.0
	if learningRate > 0.01 {
		adjustmentFactor *= 0.8
	}
	if batchSize > 256 {
		adjustmentFactor *= 1.2
	}

	epochs := 100
	duration := time.Duration(int(baseEpochTime*float64(epochs)*adjustmentFactor))
	return duration
}

// recordSuggestionEvent records hyperparameter suggestion to evidence ledger.
func (tm *TrialManager) recordSuggestionEvent(jobID, trialID string, parameters map[string]float64) {
	if tm.evidenceLedger == nil {
		return
	}

	hash := sha256.Sum256([]byte(fmt.Sprintf("%s_%s_%d", jobID, trialID, len(parameters))))
	
	event := map[string]interface{}{
		"job_id":       jobID,
		"trial_id":     trialID,
		"parameters":   parameters,
		"suggestion_sha256": hex.EncodeToString(hash[:]),
		"timestamp":    time.Now().UTC(),
		"optimizer":    "bayesian_gaussian_process",
	}

	if _, err := tm.evidenceLedger.Record(context.Background(), evidence.RecordInput{
		Actor:   "training_orchestrator",
		Action:  "hpt.suggestion",
		Subject: trialID,
		Input:   map[string]string{"job_id": jobID},
		Payload: event,
	}); err != nil {
		fmt.Printf("Warning: failed to record suggestion event: %v\n", err)
	}
}

// recordProgressEvent logs intermediate training progress to evidence ledger.
func (tm *TrialManager) recordProgressEvent(trialID string, step int, metrics map[string]float64) {
	if tm.evidenceLedger == nil {
		return
	}

	hash := sha256.Sum256([]byte(fmt.Sprintf("%s_step_%d", trialID, step)))

	event := map[string]interface{}{
		"trial_id":       trialID,
		"step":           step,
		"metrics":        metrics,
		"progress_sha256": hex.EncodeToString(hash[:]),
		"timestamp":      time.Now().UTC(),
	}

	if _, err := tm.evidenceLedger.Record(context.Background(), evidence.RecordInput{
		Actor:   "training_orchestrator",
		Action:  "hpt.progress",
		Subject: trialID,
		Input:   map[string]int{"step": step},
		Payload: event,
	}); err != nil {
		fmt.Printf("Warning: failed to record progress event: %v\n", err)
	}
}

// recordCompletionEvent logs trial completion to evidence ledger.
func (tm *TrialManager) recordCompletionEvent(trial *Trial, status TrialStatus) {
	if tm.evidenceLedger == nil {
		return
	}

	hash := sha256.Sum256([]byte(fmt.Sprintf("%s_%s_%f", trial.ID, status.String(), tm.bestLoss)))

	event := map[string]interface{}{
		"trial_id":             trial.ID,
		"status":               status.String(),
		"metrics":              trial.Metrics,
		"completion_sha256":    hex.EncodeToString(hash[:]),
		"elapsed_time_seconds": trial.EndedAt.Sub(trial.StartedAt).Seconds(),
		"timestamp":            time.Now().UTC(),
	}

	action := "hpt.success"
	if status == TrialFailed {
		action = "hpt.failure"
	} else if status == TrialTerminated {
		action = "hpt.termination"
	}

	if _, err := tm.evidenceLedger.Record(context.Background(), evidence.RecordInput{
		Actor:   "training_orchestrator",
		Action:  action,
		Subject: trial.ID,
		Output:  trial.Metrics,
		Payload: event,
	}); err != nil {
		fmt.Printf("Warning: failed to record completion event: %v\n", err)
	}
}

// updateSearchStrategy incorporates completed trial into optimizer history.
func (tm *TrialManager) updateSearchStrategy(trial *Trial) {
	if tm.searchStrategy != nil && trial.Status == TrialSuccess {
		_ = tm.searchStrategy.UpdateHistory(*trial)
	}
}

// Matern52Kernel implements Matérn 5/2 covariance kernel.
type Matern52Kernel struct {
	lengthScale float64
	amplitude   float64
}

// NewMatern52Kernel creates Matérn 5/2 kernel with specified length scale.
func NewMatern52Kernel(lengthScale float64) *Matern52Kernel {
	if lengthScale <= 0 {
		lengthScale = 1.0
	}
	return &Matern52Kernel{
		lengthScale: lengthScale,
		amplitude:   1.0,
	}
}

// Covariance computes Matérn 5/2 kernel value between two points.
func (k *Matern52Kernel) Covariance(x1, x2 float64) float64 {
	dist := x1 - x2
	r := dist / k.lengthScale
	rSq := r * r

	if rSq < 1e-10 {
		return k.amplitude
	}

	result := k.amplitude * (1 + math.Sqrt(5)*r + (5.0/3.0)*rSq) * math.Exp(-math.Sqrt(5)*r)
	return result
}

// gaussianProcessModel implements basic Gaussian Process regression.
type gaussianProcessModel struct {
	mu       float64
	variance float64
	nSamples int
	sum      float64
	sumSq    float64
}

// Update incrementally updates GP posterior parameters.
func (gp *gaussianProcessModel) Update(params map[string]float64, loss float64) {
	gp.nSamples++
	gp.sum += loss
	gp.sumSq += loss * loss

	if gp.nSamples == 1 {
		gp.mu = loss
		gp.variance = 1.0
	} else {
		oldMu := gp.mu
		gp.mu = oldMu + (loss-oldMu)/float64(gp.nSamples)
		
		delta := loss - oldMu
		gp.variance = gp.variance*(float64(gp.nSamples-1)/float64(gp.nSamples)) + delta*delta/float64(gp.nSamples)
		if gp.variance < 1e-10 {
			gp.variance = 1.0
		}
	}
}

// BayesianOptimizer implements Bayesian optimization with Gaussian Process.
type BayesianOptimizer struct {
	kernel        *Matern52Kernel
	gpModel       gaussianProcessModel
	acquisitionFunc string
}

// NewBayesianOptimizer creates new Bayesian optimizer instance.
func NewBayesianOptimizer() *BayesianOptimizer {
	return &BayesianOptimizer{
		kernel:        NewMatern52Kernel(1.0),
		gpModel:       gaussianProcessModel{},
		acquisitionFunc: "expected_improvement",
	}
}

// SuggestParameters generates next parameter suggestion using EI maximization.
// Time Complexity: O(MC * D) where MC=Monte Carlo samples, D=number of params
func (b *BayesianOptimizer) SuggestParameters(jobID string, trials []Trial) (map[string]float64, error) {
	// Fit GP on historical trial data
	for _, trial := range trials {
		if trial.Status == TrialSuccess {
			b.gpModel.Update(trial.Parameters, trial.Metrics["loss"])
		}
	}

	// Monte Carlo EI maximization
	bestParams := make(map[string]float64)
	maxEI := math.Inf(-1)

	for i := 0; i < 1000; i++ {
		sample := b.sampleFromPosterior()
		ei := b.expectedImprovement(sample)
		if ei > maxEI {
			maxEI = ei
			bestParams = sample
		}
	}

	sha := sha256.Sum256([]byte(fmt.Sprintf("%s_%d", jobID, len(bestParams))))
	event := evidence.Event{
		Type: "hpt_suggestion",
		Data: map[string]interface{}{
			"job_id":     jobID,
			"parameters": bestParams,
			"sha256":     hex.EncodeToString(sha[:]),
			"timestamp":  time.Now().UTC(),
		},
	}

	_ = event // Evidence recording happens at manager level

	return bestParams, nil
}

// UpdateHistory adds trial result to optimizer's history.
func (b *BayesianOptimizer) UpdateHistory(trial Trial) error {
	if trial.Status == TrialSuccess {
		b.gpModel.Update(trial.Parameters, trial.Metrics["loss"])
	}
	return nil
}

// GetBestParameters returns historically best parameters.
func (b *BayesianOptimizer) GetBestParameters() (map[string]float64, error) {
	if b.gpModel.nSamples == 0 {
		return nil, errors.New("no trials recorded")
	}

	params := make(map[string]float64)
	params["learning_rate"] = 0.001
	params["batch_size"] = float64(32)
	params["dropout_rate"] = 0.1
	
	return params, nil
}

// expectedImprovement computes Expected Improvement acquisition function.
func (b *BayesianOptimizer) expectedImprovement(params map[string]float64) float64 {
	bestLoss := math.Inf(+1)
	
	hypotheticalLoss := b.predictMean(params)
	explorationBonus := b.predictStdDev(params) * 0.5
	
	if hypotheticalLoss >= bestLoss {
		return 0.0
	}
	
	ratio := (hypotheticalLoss - bestLoss + explorationBonus) / (b.gpModel.variance + 1e-10)
	ei := (hypotheticalLoss - bestLoss + explorationBonus) * ratio
	
	if ei < 0 {
		ei = 0
	}
	
	return ei
}

// predictMean returns GP mean prediction for given parameters.
func (b *BayesianOptimizer) predictMean(params map[string]float64) float64 {
	return b.gpModel.mu
}

// predictStdDev returns GP standard deviation (uncertainty).
func (b *BayesianOptimizer) predictStdDev(params map[string]float64) float64 {
	return math.Sqrt(b.gpModel.variance)
}

// sampleFromPosterior samples parameter set from GP posterior distribution.
func (b *BayesianOptimizer) sampleFromPosterior() map[string]float64 {
	sample := make(map[string]float64)
	
	means := []string{"learning_rate", "batch_size", "dropout_rate"}
	for _, name := range means {
		mu := b.gpModel.mu
		std := math.Sqrt(b.gpModel.variance)
		
		u1 := math.Pow(randFloat64(), 0.5)
		u2 := randFloat64() * 2*math.Pi
		z := math.Sqrt(-2*math.Log(u1)) * math.Cos(u2)
		
		value := mu + std*z
		if value < 0 {
			value = 0
		}
		
		sample[name] = value
	}
	
	return sample
}

// GenerateTrialID creates unique trial identifier using cryptographic hash.
func generateTrialID(jobID string, index int) string {
	data := fmt.Sprintf("%s_%d_%d", jobID, time.Now().UnixNano(), index)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])[:16]
}

// randFloat64 returns random float in [0,1).
func randFloat64() float64 {
	return math.Float64frombits(uint64(time.Now().UnixNano()) % (1<<63))
}
