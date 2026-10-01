package agents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// CoordinatorAgent orchestrates multi-agent training coordination.
type CoordinatorAgent struct {
	mu            sync.RWMutex
	id            string
	registry      *Registry
	evidence      evidence.Recorder
	taskQueue     []*TrainingTask
	completedTasks map[string]*TrainingTaskResult
	runningTasks   map[string]*TaskExecutionState
	nvlinkTopology NVLinkTopology
	shutdownChan   chan struct{}
}

// TrainingTask represents unit of work assigned to agent cluster.
type TrainingTask struct {
	ID              string
	Type            TaskType
	Priority        int
	RequiredGPUs    int
	Hyperparameters map[string]float64
	CreatedAt       time.Time
	Deadline        time.Time
}

// TaskExecutionState tracks running task progress.
type TaskExecutionState struct {
	Task          *TrainingTask
	AssignedAgents []*Agent
	Progress      float64
	Status        TaskStatus
	StartedAt     time.Time
	EstimatedTime time.Duration
}

// TrainingTaskResult captures completed task outcome.
type TrainingTaskResult struct {
	TaskID        string
	Succeeded     bool
	Metrics       map[string]float64
	Duration      time.Duration
	FailedAt      *time.Time
	FailureReason string
}

// TaskType categorizes training operations.
type TaskType int

const (
	TaskHyperparameterTuning TaskType = iota
	TaskModelTraining
	TaskCheckpointSave
	TaskValidation
)

func (t TaskType) String() string {
	switch t {
	case TaskHyperparameterTuning:
		return "hyperparameter_tuning"
	case TaskModelTraining:
		return "model_training"
	case TaskCheckpointSave:
		return "checkpoint_save"
	case TaskValidation:
		return "validation"
	default:
		return "unknown"
	}
}

// TaskStatus describes execution lifecycle.
type TaskStatus int

const (
	TaskPending TaskStatus = iota
	TaskAllocating
	TaskRunning
	TaskComplete
	TaskFailed
	TaskCancelled
)

func (s TaskStatus) String() string {
	switch s {
	case TaskPending:
		return "pending"
	case TaskAllocating:
		return "allocating"
	case TaskRunning:
		return "running"
	case TaskComplete:
		return "complete"
	case TaskFailed:
		return "failed"
	case TaskCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

// NewCoordinatorAgent creates new training coordinator.
func NewCoordinatorAgent(registry *Registry, topology NVLinkTopology, evidenceRecorder evidence.Recorder) *CoordinatorAgent {
	if evidenceRecorder == nil {
		evidenceRecorder = &evidence.NopRecorder{}
	}

	return &CoordinatorAgent{
		id:             generateCoordinatorID(),
		registry:       registry,
		evidence:       evidenceRecorder,
		taskQueue:      make([]*TrainingTask, 0),
		completedTasks: make(map[string]*TrainingTaskResult),
		runningTasks:   make(map[string]*TaskExecutionState),
		nvlinkTopology: topology,
		shutdownChan:   make(chan struct{}, 1),
	}
}

// SubmitTask queues new training task for execution.
func (ca *CoordinatorAgent) SubmitTask(task *TrainingTask) error {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	if task.ID == "" {
		task.ID = generateTaskID(task.Type)
	}

	if _, exists := ca.completedTasks[task.ID]; exists {
		return fmt.Errorf("task %s already completed", task.ID)
	}

	if _, running := ca.runningTasks[task.ID]; running {
		return fmt.Errorf("task %s already running", task.ID)
	}

	now := time.Now().UTC()
	task.CreatedAt = now
	task.Deadline = now.Add(time.Hour * 24)

	ca.taskQueue = append(ca.taskQueue, task)
	ca.allocateResources(task)

	ca.recordTaskSubmitted(task)
	return nil
}

// AllocateResources assigns agents and GPUs respecting NVLink constraints.
func (ca *CoordinatorAgent) allocateResources(task *TrainingTask) {
	requiredGPUs := task.RequiredGPUs
	availableGpus := ca.registry.FindAvailableGPUs(requiredGPUs)

	if len(availableGpus) < requiredGPUs {
		task.Status = TaskFailed
		ca.recordAllocationFailure(task, fmt.Sprintf("insufficient GPUs: need %d, available %d", requiredGPUs, len(availableGpus)))
		return
	}

	assignedAgents := make([]*Agent, 0, requiredGPUs)
	for _, gpuID := range availableGpus[:requiredGPUs] {
		agentConfig := AgentConfig{
			Type:          AgentDataProcessor,
			Priority:      task.Priority,
			GPUAffinity:   []int{gpuID},
			EvidenceLedger: ca.evidence,
		}

		agent, err := ca.registry.RegisterAgent(agentConfig)
		if err != nil {
			continue
		}

		agent.AssignedGpuID = gpuID
		assignedAgents = append(assignedAgents, agent)
	}

	executionState := &TaskExecutionState{
		Task:           task,
		AssignedAgents: assignedAgents,
		Progress:       0.0,
		Status:         TaskAllocating,
		StartedAt:      time.Now().UTC(),
		EstimatedTime:  ca.estimateTaskDuration(task),
	}

	ca.runningTasks[task.ID] = executionState
	task.Status = TaskRunning
	ca.recordResourceAllocation(task, assignedAgents)
}

// estimateTaskDuration predicts execution time based on hyperparameters.
func (ca *CoordinatorAgent) estimateTaskDuration(task *TrainingTask) time.Duration {
	baseDuration := time.Minute * 30
	learningRate := task.Hyperparameters["learning_rate"]
	batchSize := task.Hyperparameters["batch_size"]

	adjustmentFactor := 1.0
	if learningRate > 0.01 {
		adjustmentFactor *= 0.8
	}
	if batchSize > 256 {
		adjustmentFactor *= 1.2
	}

	numEpochs := 100
	duration := time.Duration(int(baseDuration*float64(numEpochs)*adjustmentFactor))
	return duration
}

// getGPUBandwidth calculates aggregate NVLink bandwidth for allocated GPUs.
func (ca *CoordinatorAgent) getGPUBandwidth(gpuIDs []int) float64 {
	totalBandwidth := 0.0
	for i := 0; i < len(gpuIDs); i++ {
		for j := i + 1; j < len(gpuIDs); j++ {
			connectivity := ca.nvlinkTopology.GetConnectivityScore(gpuIDs[i])
			if connectivity > 0 {
				totalBandwidth += connectivity
			}
		}
	}
	return totalBandwidth
}

// recordTaskSubmitted logs task submission to evidence ledger.
func (ca *CoordinatorAgent) recordTaskSubmitted(task *TrainingTask) {
	if ca.evidence == nil {
		return
	}

	data := fmt.Sprintf("%s_%s_%d_%d", task.ID, task.Type, task.Priority, len(task.Hyperparameters))
	hash := sha256.Sum256([]byte(data))

	event := map[string]interface{}{
		"task_id":          task.ID,
		"task_type":        task.Type.String(),
		"priority":         task.Priority,
		"required_gpus":    task.RequiredGPUs,
		"hyperparams_hash": hex.EncodeToString(hash[:]),
		"timestamp":        time.Now().UTC(),
	}

	if _, err := ca.evidence.Record(context.Background(), evidence.RecordInput{
		Actor:   "coordinator_agent_" + ca.id,
		Action:  "task.submit",
		Subject: task.ID,
		Input:   task.Hyperparameters,
		Payload: event,
	}); err != nil {
		fmt.Printf("Warning: failed to record task submission: %v\n", err)
	}
}

// recordResourceAllocation logs GPU assignment to evidence ledger.
func (ca *CoordinatorAgent) recordResourceAllocation(task *TrainingTask, agents []*Agent) {
	if ca.evidence == nil {
		return
	}

	gpuIds := make([]int, len(agents))
	for i, agent := range agents {
		gpuIds[i] = agent.AssignedGpuID
	}

	data := fmt.Sprintf("%s_gpus_%v", task.ID, gpuIds)
	hash := sha256.Sum256([]byte(data))

	event := map[string]interface{}{
		"task_id":           task.ID,
		"allocated_gpus":    gpuIds,
		"nvlink_bandwidth":  ca.getGPUBandwidth(gpuIds),
		"allocation_hash":   hex.EncodeToString(hash[:]),
		"timestamp":         time.Now().UTC(),
	}

	if _, err := ca.evidence.Record(context.Background(), evidence.RecordInput{
		Actor:   "coordinator_agent_" + ca.id,
		Action:  "resource.allocate",
		Subject: task.ID,
		Output:  map[string]interface{}{"gpus": gpuIds},
		Payload: event,
	}); err != nil {
		fmt.Printf("Warning: failed to record resource allocation: %v\n", err)
	}
}

// recordAllocationFailure logs failed resource allocation attempt.
func (ca *CoordinatorAgent) recordAllocationFailure(task *TrainingTask, reason string) {
	if ca.evidence == nil {
		return
	}

	data := fmt.Sprintf("%s_failure_%s", task.ID, reason)
	hash := sha256.Sum256([]byte(data))

	event := map[string]interface{}{
		"task_id":      task.ID,
		"failure_reason": reason,
		"hash":         hex.EncodeToString(hash[:]),
		"timestamp":    time.Now().UTC(),
	}

	if _, err := ca.evidence.Record(context.Background(), evidence.RecordInput{
		Actor:   "coordinator_agent_" + ca.id,
		Action:  "resource.failure",
		Subject: task.ID,
		Payload: event,
	}); err != nil {
		fmt.Printf("Warning: failed to record allocation failure: %v\n", err)
	}
}

// Stop gracefully shuts down coordinator agent.
func (ca *CoordinatorAgent) Stop() {
	select {
	case ca.shutdownChan <- struct{}{}:
	default:
	}
}

// generateCoordinatorID creates unique coordinator identifier.
func generateCoordinatorID() string {
	data := fmt.Sprintf("coordinator_%d", time.Now().UnixNano())
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])[:16]
}

// generateTaskID creates unique task identifier.
func generateTaskID(taskType TaskType) string {
	data := fmt.Sprintf("%s_%d", taskType.String(), time.Now().UnixNano())
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])[:16]
}

// computeMean calculates arithmetic mean for metrics aggregation.
func computeMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sum := 0.0
	for _, v := range values {
		sum += v
	}

	return sum / float64(len(values))
}
