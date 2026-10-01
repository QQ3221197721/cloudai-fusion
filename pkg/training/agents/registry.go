package agents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/cloudai-fusion/cloudai-fusion/pkg/evidence"
)

// AgentType categorizes different multi-agent roles.
type AgentType int

const (
	AgentCoordinator AgentType = iota
	AgentFaultMonitoring
	AgentMetricsCollection
	AgentResourceOptimizer
	AgentDataProcessor
)

func (t AgentType) String() string {
	switch t {
	case AgentCoordinator:
		return "coordinator"
	case AgentFaultMonitoring:
		return "fault_monitoring"
	case AgentMetricsCollection:
		return "metrics_collection"
	case AgentResourceOptimizer:
		return "resource_optimizer"
	case AgentDataProcessor:
		return "data_processor"
	default:
		return "unknown"
	}
}

// AgentStatus represents lifecycle state.
type AgentStatus int

const (
	AgentInitializing AgentStatus = iota
	AgentRunning
	AgentPaused
	AgentTerminated
	AgentFailed
)

func (s AgentStatus) String() string {
	switch s {
	case AgentInitializing:
		return "initializing"
	case AgentRunning:
		return "running"
	case AgentPaused:
		return "paused"
	case AgentTerminated:
		return "terminated"
	case AgentFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Agent describes a single orchestrator component.
type Agent struct {
	ID              string
	Type            AgentType
	Status          AgentStatus
	AssignedGpuID   int
	Priority        int
	StartedAt       time.Time
	LastHeartbeat   time.Time
	Metadata        map[string]string
	CreatedAt       time.Time
}

// AgentConfig holds initialization parameters.
type AgentConfig struct {
	ID                string
	Type              AgentType
	Priority          int
	GPUAffinity       []int
	ResourceLimits    ResourceConstraints
	Metadata          map[string]string
	EvidenceLedger    evidence.Recorder
}

// ResourceConstraints defines hardware limits.
type ResourceConstraints struct {
	MaxGPUmemoryGB float64
	MaxCPUCores    int
	MaxMemoryGB    float64
}

// Registry manages agent lifecycle and discovery.
type Registry struct {
	mu           sync.RWMutex
	agents       map[string]*Agent
	evidence     evidence.Recorder
	nvlinkTopology NVLinkTopology
}

// NewRegistry creates new agent registry with NVLink awareness.
func NewRegistry(evidenceRecorder evidence.Recorder, topology NVLinkTopology) *Registry {
	if evidenceRecorder == nil {
		evidenceRecorder = &evidence.NopRecorder{}
	}

	return &Registry{
		agents:       make(map[string]*Agent),
		evidence:     evidenceRecorder,
		nvlinkTopology: topology,
	}
}

// RegisterAgent enrolls new agent in registry.
func (reg *Registry) RegisterAgent(config AgentConfig) (*Agent, error) {
	reg.mu.Lock()
	defer reg.mu.Unlock()

	if config.ID == "" {
		config.ID = generateAgentID(config.Type)
	}

	if _, exists := reg.agents[config.ID]; exists {
		return nil, fmt.Errorf("agent %s already registered", config.ID)
	}

	now := time.Now().UTC()
	agent := &Agent{
		ID:            config.ID,
		Type:          config.Type,
		Status:        AgentInitializing,
		Priority:      config.Priority,
		AssignedGpuID: -1,
		Metadata:      config.Metadata,
		StartedAt:     now,
		LastHeartbeat: now,
		CreatedAt:     now,
	}

	if len(config.GPUAffinity) > 0 {
	 optimalGPU := reg.findOptimalGPU(config.GPUAffinity)
		if optimalGPU >= 0 {
			agent.AssignedGpuID = optimalGPU
		}
	}

	agent.Status = AgentRunning
	reg.agents[config.ID] = agent

	reg.recordRegistrationEvent(agent)

	return agent, nil
}

// UnregisterAgent removes agent from registry gracefully.
func (reg *Registry) UnregisterAgent(agentID string) error {
	reg.mu.Lock()
	defer reg.mu.Unlock()

	agent, exists := reg.agents[agentID]
	if !exists {
		return fmt.Errorf("agent %s not found", agentID)
	}

	agent.Status = AgentTerminated
	delete(reg.agents, agentID)

	reg.recordUnregistrationEvent(agentID)
	return nil
}

// GetAgent retrieves agent by ID.
func (reg *Registry) GetAgent(agentID string) (*Agent, error) {
	reg.mu.RLock()
	defer reg.mu.RUnlock()

	agent, exists := reg.agents[agentID]
	if !exists {
		return nil, fmt.Errorf("agent %s not found", agentID)
	}

	agentCopy := *agent
	return &agentCopy, nil
}

// GetAllAgents returns all registered agents.
func (reg *Registry) GetAllAgents() []*Agent {
	reg.mu.RLock()
	defer reg.mu.RUnlock()

	result := make([]*Agent, 0, len(reg.agents))
	for _, agent := range reg.agents {
		agentCopy := *agent
		result = append(result, &agentCopy)
	}

	return result
}

// UpdateHeartbeat refreshes last heartbeat timestamp.
func (reg *Registry) UpdateHeartbeat(agentID string) error {
	reg.mu.Lock()
	defer reg.mu.Unlock()

	agent, exists := reg.agents[agentID]
	if !exists {
		return fmt.Errorf("agent %s not found", agentID)
	}

	agent.LastHeartbeat = time.Now().UTC()
	return nil
}

// FindAvailableGPUs identifies free GPUs for new agent.
func (reg *Registry) FindAvailableGPUs(count int) []int {
	reg.mu.RLock()
	defer reg.mu.RUnlock()

	assignedGpus := make(map[int]bool)
	for _, agent := range reg.agents {
		if agent.AssignedGpuID >= 0 && agent.Status == AgentRunning {
			assignedGpus[agent.AssignedGpuID] = true
		}
	}

	available := make([]int, 0)
	for gpuID := range assignedGpus {
		if !assignedGpus[gpuID] {
			available = append(available, gpuID)
			if len(available) >= count {
				break
			}
		}
	}

	return available
}

// findOptimalGPU selects best GPU respecting NVLink constraints.
func (reg *Registry) findOptimalGPU(affinities []int) int {
	bestGPU := affinities[0]
	bestScore := reg.nvlinkTopology.GetConnectivityScore(bestGPU)

	for _, gpuID := range affinities[1:] {
		score := reg.nvlinkTopology.GetConnectivityScore(gpuID)
		if score > bestScore {
			bestScore = score
			bestGPU = gpuID
		}
	}

	return bestGPU
}

// recordRegistrationEvent logs agent creation to evidence ledger.
func (reg *Registry) recordRegistrationEvent(agent *Agent) {
	if reg.evidence == nil {
		return
	}

	data := fmt.Sprintf("%s_%d_%s", agent.ID, agent.Type, agent.AssignedGpuID)
	hash := sha256.Sum256([]byte(data))

	event := map[string]interface{}{
		"agent_id":       agent.ID,
		"agent_type":     agent.Type.String(),
		"gpu_assigned":   agent.AssignedGpuID,
		"priority":       agent.Priority,
		"event_hash":     hex.EncodeToString(hash[:]),
		"timestamp":      time.Now().UTC(),
	}

	if _, err := reg.evidence.Record(context.Background(), evidence.RecordInput{
		Actor:   "multi_agent_coordinator",
		Action:  "agent.register",
		Subject: agent.ID,
		Payload: event,
	}); err != nil {
		fmt.Printf("Warning: failed to record agent registration: %v\n", err)
	}
}

// recordUnregistrationEvent logs agent removal to evidence ledger.
func (reg *Registry) recordUnregistrationEvent(agentID string) {
	if reg.evidence == nil {
		return
	}

	hash := sha256.Sum256([]byte(fmt.Sprintf("unregister_%s_%d", agentID, time.Now().UnixNano())))

	event := map[string]interface{}{
		"agent_id":      agentID,
		"action":        "unregister",
		"event_hash":    hex.EncodeToString(hash[:]),
		"timestamp":     time.Now().UTC(),
	}

	if _, err := reg.evidence.Record(context.Background(), evidence.RecordInput{
		Actor:   "multi_agent_coordinator",
		Action:  "agent.unregister",
		Subject: agentID,
		Payload: event,
	}); err != nil {
		fmt.Printf("Warning: failed to record unregistration: %v\n", err)
	}
}

// generateAgentID creates unique identifier using cryptographic hash.
func generateAgentID(agentType AgentType) string {
	data := fmt.Sprintf("%s_%d", agentType.String(), time.Now().UnixNano())
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])[:16]
}
