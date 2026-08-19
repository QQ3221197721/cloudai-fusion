package plugin

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ThirdPartyWorkflow represents an external workflow integration
// This module orchestrates third-party workflow engines, handles webhook events,
// and manages state synchronization between CloudAI Fusion and external platforms.

// WorkflowEngine defines supported workflow engines
type WorkflowEngine string

const (
	Camunda    WorkflowEngine = "camunda"
	Airflow    WorkflowEngine = "airflow"
	Temporal   WorkflowEngine = "temporal"
	ArgoWorkflows WorkflowEngine = "argoworkflows"
	Prefect    WorkflowEngine = "prefect"
)

// WorkflowDefinition contains workflow specification
type WorkflowDefinition struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Version      string                 `json:"version"`
	Description  string                 `json:"description"`
	Engine       WorkflowEngine         `json:"engine"`
	Schema       map[string]interface{} `json:"schema"`
	Metadata     map[string]string      `json:"metadata,omitempty"`
	CreatedAt    time.Time              `json:"createdAt"`
	UpdatedAt    time.Time              `json:"updatedAt"`
	Status       WorkflowStatus         `json:"status"`
	Webhooks     []ThirdPartyWebhookConfig        `json:"webhooks,omitempty"`
}

// WorkflowStatus defines workflow lifecycle state
type WorkflowStatus string

const (
	WorkflowActive   WorkflowStatus = "active"
	WorkflowInactive WorkflowStatus = "inactive"
	WorkflowDraft    WorkflowStatus = "draft"
	WorkflowError    WorkflowStatus = "error"
)

// ThirdPartyWebhookConfig configures webhook endpoints for third-party workflows
type ThirdPartyWebhookConfig struct {
	URL          string            `json:"url"`
	Events       []string          `json:"events"`
	Secret       string            `json:"secret"`
	Method       string            `json:"method"` // POST/PUT
	Headers      map[string]string `json:"headers,omitempty"`
	RetryPolicy  *RetryPolicy      `json:"retryPolicy,omitempty"`
}

// RetryPolicy for webhook delivery
type RetryPolicy struct {
	MaxRetries    int           `json:"maxRetries"`
	Delay         time.Duration `json:"delay"`
	BackoffFactor float64       `json:"backoffFactor"`
}

// WorkflowInstance represents a running workflow execution
type WorkflowInstance struct {
	ID              string                `json:"id"`
	WorkflowID      string                `json:"workflowId"`
	Definition      *WorkflowDefinition   `json:"definition,omitempty"`
	Status          WorkflowInstanceState `json:"status"`
	StartedAt       time.Time             `json:"startedAt"`
	CompletedAt     *time.Time            `json:"completedAt,omitempty"`
	InputData       map[string]interface{} `json:"inputData,omitempty"`
	OutputData      map[string]interface{} `json:"outputData,omitempty"`
	Metrics         WorkflowMetrics       `json:"metrics,omitempty"`
	Errors          []string              `json:"errors,omitempty"`
	PauseState      interface{}           `json:"pauseState,omitempty"`
}

// WorkflowInstanceState defines execution state
type WorkflowInstanceState string

const (
	StateRunning    WorkflowInstanceState = "running"
	StatePaused     WorkflowInstanceState = "paused"
	StateResumed    WorkflowInstanceState = "resumed"
	StateComplete   WorkflowInstanceState = "complete"
	StateFailed     WorkflowInstanceState = "failed"
	StateTimedOut   WorkflowInstanceState = "timedout"
)

// WorkflowMetrics contains execution statistics
type WorkflowMetrics struct {
	StartTime      time.Time `json:"startTime"`
	EndTime        time.Time `json:"endTime,omitempty"`
	DurationMs     int64     `json:"durationMs"`
	NodeExecutions int       `json:"nodeExecutions"`
	EventsProcessed int      `json:"eventsProcessed"`
	Retries        int       `json:"retries"`
}

// ThirdPartyWorkflowOrchestrator manages third-party workflow integrations
type ThirdPartyWorkflowOrchestrator struct {
	mu              sync.RWMutex
	workflows       map[string]*WorkflowDefinition
	instances       map[string]*WorkflowInstance
	webhooks        map[string][]*WebhookQueue
	connectors      map[WorkflowEngine]Connector
	logger          *logrus.Logger
	ctx             context.Context
	cancel          context.CancelFunc
}

// Connector interface for workflow engine communication
type Connector interface {
	DeployWorkflow(ctx context.Context, definition *WorkflowDefinition) error
	TerminateWorkflow(ctx context.Context, instanceID string) error
	GetInstanceStatus(ctx context.Context, instanceID string) (*WorkflowInstance, error)
	PauseInstance(ctx context.Context, instanceID string) error
	ResumeInstance(ctx context.Context, instanceID string) error
	ListInstances(ctx context.Context, filters map[string]string) ([]*WorkflowInstance, error)
}

// WebhookQueue for async webhook delivery
type WebhookQueue struct {
	Config    *ThirdPartyWebhookConfig
	URL       string
	Payload   []byte
	RetryCount int
	LastAttempt time.Time
	CreatedAt   time.Time
}

// NewThirdPartyWorkflowOrchestrator creates a new orchestrator
func NewThirdPartyWorkflowOrchestrator(ctx context.Context, logger *logrus.Logger) *ThirdPartyWorkflowOrchestrator {
	fctx, cancel := context.WithCancel(ctx)
	
	return &ThirdPartyWorkflowOrchestrator{
		ctx:         fctx,
		cancel:      cancel,
		workflows:   make(map[string]*WorkflowDefinition),
		instances:   make(map[string]*WorkflowInstance),
		webhooks:    make(map[string][]*WebhookQueue),
		connectors:  make(map[WorkflowEngine]Connector),
		logger:      logger,
	}
}

// RegisterConnector registers a workflow engine connector
func (o *ThirdPartyWorkflowOrchestrator) RegisterConnector(engine WorkflowEngine, connector Connector) {
	o.mu.Lock()
	defer o.mu.Unlock()
	
	o.connectors[engine] = connector
	
	if o.logger != nil {
		o.logger.Infof("Registered connector for workflow engine: %s", engine)
	}
}

// DeployWorkflow deploys a workflow definition to the target engine
func (o *ThirdPartyWorkflowOrchestrator) DeployWorkflow(ctx context.Context, definition *WorkflowDefinition) error {
	o.mu.Lock()
	
	if _, exists := o.workflows[definition.ID]; exists {
		o.mu.Unlock()
		return fmt.Errorf("workflow %s already deployed", definition.ID)
	}

	definition.Status = WorkflowActive
	definition.CreatedAt = time.Now()
	definition.UpdatedAt = time.Now()
	
	o.workflows[definition.ID] = definition
	o.mu.Unlock()

	// Deploy to external engine
	connector := o.connectors[definition.Engine]
	if connector == nil {
		return fmt.Errorf("no connector registered for engine: %s", definition.Engine)
	}

	if err := connector.DeployWorkflow(ctx, definition); err != nil {
		definition.Status = WorkflowError
		o.mu.Lock()
		o.workflows[definition.ID] = definition
		o.mu.Unlock()
		
		return fmt.Errorf("deployment failed: %w", err)
	}

	if o.logger != nil {
		o.logger.Infof("Deployed workflow %s to %s engine", definition.Name, definition.Engine)
	}

	return nil
}

// TerminateWorkflow stops a workflow and releases resources
func (o *ThirdPartyWorkflowOrchestrator) TerminateWorkflow(ctx context.Context, workflowID string) error {
	o.mu.Lock()
	definition, exists := o.workflows[workflowID]
	o.mu.Unlock()

	if !exists {
		return fmt.Errorf("workflow %s not found", workflowID)
	}

	// Get all instances of this workflow
	o.mu.Lock()
	var instanceIDs []string
	for id, instance := range o.instances {
		if instance.WorkflowID == workflowID {
			instanceIDs = append(instanceIDs, id)
		}
	}
	o.mu.Unlock()

	// Terminate each instance
	connector := o.connectors[definition.Engine]
	for _, instanceID := range instanceIDs {
		if err := connector.TerminateWorkflow(ctx, instanceID); err != nil {
			if o.logger != nil {
				o.logger.Warnf("Failed to terminate instance %s: %v", instanceID, err)
			}
		}
	}

	// Deactivate workflow
	o.mu.Lock()
	definition.Status = WorkflowInactive
	o.workflows[workflowID] = definition
	o.mu.Unlock()

	if o.logger != nil {
		o.logger.Infof("Terminated workflow %s with %d instances", workflowID, len(instanceIDs))
	}

	return nil
}

// StartInstance starts a new workflow execution
func (o *ThirdPartyWorkflowOrchestrator) StartInstance(ctx context.Context, workflowID string, inputData map[string]interface{}) (*WorkflowInstance, error) {
	o.mu.RLock()
	definition, exists := o.workflows[workflowID]
	o.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("workflow %s not found", workflowID)
	}

	instanceID := generateInstanceID()
	now := time.Now()

	instance := &WorkflowInstance{
		ID:          instanceID,
		WorkflowID:  workflowID,
		Definition:  definition,
		Status:      StateRunning,
		StartedAt:   now,
		InputData:   inputData,
		Metrics:     WorkflowMetrics{StartTime: now},
	}

	o.mu.Lock()
	o.instances[instanceID] = instance
	o.mu.Unlock()

	// Forward to external engine
	connector := o.connectors[definition.Engine]
	if err := connector.DeployWorkflow(ctx, definition); err != nil {
		o.mu.Lock()
		instance.Status = StateFailed
		instance.Errors = append(instance.Errors, err.Error())
		o.instances[instanceID] = instance
		o.mu.Unlock()
		
		return nil, fmt.Errorf("instance creation failed: %w", err)
	}

	if o.logger != nil {
		o.logger.Infof("Started workflow instance %s (workflow: %s)", instanceID, workflowID)
	}

	return instance, nil
}

// PauseInstance pauses a running workflow
func (o *ThirdPartyWorkflowOrchestrator) PauseInstance(ctx context.Context, instanceID string) error {
	o.mu.RLock()
	instance, exists := o.instances[instanceID]
	o.mu.RUnlock()

	if !exists {
		return fmt.Errorf("instance %s not found", instanceID)
	}

	if instance.Status != StateRunning {
		return fmt.Errorf("instance is not running (current status: %s)", instance.Status)
	}

	connector := o.connectors[instance.Definition.Engine]
	if err := connector.PauseInstance(ctx, instanceID); err != nil {
		return fmt.Errorf("pause failed: %w", err)
	}

	o.mu.Lock()
	instance.Status = StatePaused
	o.instances[instanceID] = instance
	o.mu.Unlock()

	if o.logger != nil {
		o.logger.Infof("Paused workflow instance %s", instanceID)
	}

	return nil
}

// ResumeInstance resumes a paused workflow
func (o *ThirdPartyWorkflowOrchestrator) ResumeInstance(ctx context.Context, instanceID string) error {
	o.mu.RLock()
	instance, exists := o.instances[instanceID]
	o.mu.RUnlock()

	if !exists {
		return fmt.Errorf("instance %s not found", instanceID)
	}

	if instance.Status != StatePaused {
		return fmt.Errorf("instance is not paused (current status: %s)", instance.Status)
	}

	connector := o.connectors[instance.Definition.Engine]
	if err := connector.ResumeInstance(ctx, instanceID); err != nil {
		return fmt.Errorf("resume failed: %w", err)
	}

	o.mu.Lock()
	instance.Status = StateResumed
	o.instances[instanceID] = instance
	o.mu.Unlock()

	if o.logger != nil {
		o.logger.Infof("Resumed workflow instance %s", instanceID)
	}

	return nil
}

// GetInstance retrieves a workflow instance by ID
func (o *ThirdPartyWorkflowOrchestrator) GetInstance(ctx context.Context, instanceID string) (*WorkflowInstance, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	instance, exists := o.instances[instanceID]
	if !exists {
		return nil, fmt.Errorf("instance %s not found", instanceID)
	}

	return instance, nil
}

// ListInstances returns workflow instances with optional filtering
func (o *ThirdPartyWorkflowOrchestrator) ListInstances(ctx context.Context, filters map[string]string) []*WorkflowInstance {
	o.mu.RLock()
	defer o.mu.RUnlock()

	var result []*WorkflowInstance
	for _, instance := range o.instances {
		if matchesFilters(instance, filters) {
			result = append(result, instance)
		}
	}

	return result
}

// SendWebhook sends a webhook notification asynchronously
func (o *ThirdPartyWorkflowOrchestrator) SendWebhook(ctx context.Context, event string, payload []byte) {
	o.mu.RLock()
	hooks, exists := o.webhooks[event]
	o.mu.RUnlock()

	if !exists || len(hooks) == 0 {
		return
	}

	// Create queue items for each webhook
	queueItems := make([]*WebhookQueue, len(hooks))
	for i, hook := range hooks {
		queueItems[i] = &WebhookQueue{
			Config:    hook.Config,
			URL:       hook.URL,
			Payload:   payload,
			CreatedAt: time.Now(),
		}
	}

	// Process webhooks in background
	go o.processWebhooks(ctx, queueItems)
}

// processWebhooks processes webhook queue items with retry logic
func (o *ThirdPartyWorkflowOrchestrator) processWebhooks(ctx context.Context, items []*WebhookQueue) {
	for _, item := range items {
		retryCount := 0
		maxRetries := 3
		
		if item.Config.RetryPolicy != nil {
			maxRetries = item.Config.RetryPolicy.MaxRetries
		}

		for retryCount < maxRetries {
			if err := o.sendWebhookItem(item); err != nil {
				retryCount++
				item.LastAttempt = time.Now()
				item.RetryCount = retryCount
				
				if retryCount >= maxRetries {
					if o.logger != nil {
						o.logger.Errorf("Max retries exceeded for webhook %s: %v", item.URL, err)
					}
					break
				}

				// Exponential backoff
				delay := item.Config.RetryPolicy.Delay
				if item.Config.RetryPolicy.BackoffFactor > 1 {
					delay *= time.Duration(retryCount)
				}
				time.Sleep(delay)
			} else {
				if o.logger != nil {
					o.logger.Debugf("Webhook sent successfully to %s", item.URL)
				}
				break
			}
		}
	}
}

// sendWebhookItem sends a single webhook item
func (o *ThirdPartyWorkflowOrchestrator) sendWebhookItem(item *WebhookQueue) error {
	req, err := http.NewRequest("POST", item.URL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if item.Config.Headers != nil {
		for k, v := range item.Config.Headers {
			req.Header.Set(k, v)
		}
	}

	if item.Config.Secret != "" {
		signature := computeHMACSha256(item.Payload, item.Config.Secret)
		req.Header.Set("X-Webhook-Signature", signature)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	io.Copy(io.Discard, resp.Body)

	return nil
}

// ToJSON marshals to JSON
func ToJSON(v interface{}) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// Helper functions
func generateInstanceID() string {
	buffer := make([]byte, 16)
	rand.Read(buffer)
	return fmt.Sprintf("wf-%x", buffer)
}

func matchesFilters(instance *WorkflowInstance, filters map[string]string) bool {
	for key, value := range filters {
		switch key {
		case "status":
			if string(instance.Status) != value {
				return false
			}
		case "workflowId":
			if instance.WorkflowID != value {
				return false
			}
		}
	}
	return true
}

func computeHMACSha256(data []byte, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(data)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
