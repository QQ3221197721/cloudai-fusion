package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// ============================================================================
// Gatekeeper Integration - Admission Control Controller
// ============================================================================

// KubernetesAdmissionController manages admission control with Gatekeeper integration
type KubernetesAdmissionController struct {
	mu                   sync.RWMutex
	kubeClient           *kubernetes.Clientset
	webhookServer        *webhook.Server
	admissionHandler     *admission.Handler
	policyEngine         *Engine
	namespaceManager     *NamespaceManager
	resourceValidator    *ResourceValidator
	aclRuleResolver      *ACLRuleResolver
	eventLogger          *AuditEventLogger
	defaultDenyPolicy    *PolicyBundle
	namespaceScopes      map[string]*NamespacePolicyScope
	cache                *AdmissionCache
	reconciliationTicker *time.Ticker
	ctx                  context.Context
	cancel               context.CancelFunc
	isRunning            bool
}

// NamespacePolicyScope defines namespace-level policy inheritance
type NamespacePolicyScope struct {
	Namespace         string
	Policies          []*PolicyBundle
	InheritFrom       string
	OpaquePolicies    []runtime.RawExtension
	LabelSelectors    map[string]string
	AnnotationFilters map[string]string
	CreatedAt         time.Time
	LastEvaluatedAt   time.Time
}

// ============================================================================
// Kubernetes Resource Lifecycle Handlers
// ============================================================================

// ResourceEventHandler handles kubernetes resource lifecycle events
type ResourceEventHandler struct {
	controller       *KubernetesAdmissionController
	serializer       runtime.Codec
	handlersMap      map[WatchEventType]ResourceEventHandlerFunc
	errorHandler    func(error)
	cacheSyncedChan chan struct{}
}

type WatchEventType string

const (
	EventCreate WatchEventType = "CREATE"
	EventUpdate WatchEventType = "UPDATE"
	EventDelete WatchEventType = "DELETE"
)

type ResourceEventHandlerFunc func(ctx context.Context, event *WatchEvent) error

type WatchEvent struct {
	Type      WatchEventType              `json:"type"`
	Resource  metav1.TypeMeta             `json:"resource"`
	Object    interface{}                 `json:"object"`
	Namespace string                      `json:"namespace"`
	RequestID types.UID                   `json:"request_id"`
	Timestamp time.Time                   `json:"timestamp"`
	Metadata  map[string]interface{}      `json:"metadata,omitempty"`
}

// ============================================================================
// Webhook Server & Validation Patterns
// ============================================================================

// ValidatingWebhookConfiguration configures Kubernetes webhook
type ValidatingWebhookConfiguration struct {
	Name              string
	Labels            map[string]string
	Annotations       map[string]string
	Rules             []RuleWithOperations
	FailurePolicy     *string
	SideEffects     *string
	NamespaceSelector *metav1.LabelSelector
	ObjectSelector  *metav1.LabelSelector
	ClientConfig     WebhookClientConfig
}

type RuleWithOperations struct {
	Operations []admissionv1.OperationType
	Rule       metav1.Rule
}

type WebhookClientConfig struct {
	URL      *string
	CABundle []byte
	Service  *ServiceReference
}

type ServiceReference struct {
	Namespace string
	Name      string
	Path      *string
	Port      *int32
}

// ============================================================================
// Audit Logging - Structured JSON Output
// ============================================================================

// AuditEvent represents a single audit log entry
type AuditEvent struct {
	AuditID        string                 `json:"audit_id"`
	Timestamp      time.Time              `json:"timestamp"`
	Action         string                 `json:"action"`
	Decision       string                 `json:"decision"`
	UserInfo       UserInfo               `json:"user_info"`
	Resource       ResourceContext        `json:"resource"`
	Namespace      string                 `json:"namespace"`
	PolicyRefs     []string               `json:"policy_refs"`
	EvaluationTime time.Duration          `json:"evaluation_time"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	ErrorMessage   string                 `json:"error_message,omitempty"`
	Remediation    string                 `json:"remediation,omitempty"`
}

// UserInfo captures authenticated user details
type UserInfo struct {
 Username   string            `json:"username"`
 Groups     []string          `json:"groups"`
 UID        string            `json:"uid"`
 Extra      map[string][]string `json:"extra,omitempty"`
}

// ResourceContext describes the audited resource
type ResourceContext struct {
 APIVersion string              `json:"api_version"`
 Kind       string              `json:"kind"`
 Name       string              `json:"name"`
 Namespace  string              `json:"namespace"`
 UID        types.UID           `json:"uid"`
 Labels     map[string]string   `json:"labels,omitempty"`
 Annotations map[string]string `json:"annotations,omitempty"`
}

// ============================================================================
// Controller Implementation
// ============================================================================

// NewKubernetesAdmissionController creates new admission controller
func NewKubernetesAdmissionController(
	config *KubernetesAdmissionConfig,
	kubeClient *kubernetes.Clientset,
	policyEngine *Engine,
) (*KubernetesAdmissionController, error) {
	if config == nil {
		config = DefaultKubernetesAdmissionConfig()
	}

	controller := &KubernetesAdmissionController{
		kubeClient:        kubeClient,
		webhookServer:     nil,
		admissionHandler:  admission.NewHandler(nil),
		policyEngine:      policyEngine,
		namespaceManager:  NewNamespaceManager(),
		resourceValidator: NewResourceValidator(config.ResourceTypes),
		aclRuleResolver:   NewACLRuleResolver(),
		eventLogger:       NewAuditEventLogger(),
		defaultDenyPolicy: nil,
		namespaceScopes:   make(map[string]*NamespacePolicyScope),
		cache:             NewAdmissionCache(10000),
	}

	var err error
	if err = controller.initializeDefaultDeny(); err != nil {
		return nil, fmt.Errorf("failed to initialize default deny: %w", err)
	}

	if err = controller.setupWebhookServer(); err != nil {
		return nil, fmt.Errorf("webhook setup failed: %w", err)
	}

	if err = controller.registerLifecycleHandlers(); err != nil {
		return nil, fmt.Errorf("handler registration failed: %w", err)
	}

	return controller, nil
}

// Start initializes all background reconciliation loops
func (c *KubernetesAdmissionController) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ctx, c.cancel = context.WithCancel(ctx)
	c.isRunning = true

	// Start namespace synchronization
	go c.namespaceReconciler(c.ctx)

	// Start cache cleanup
	go c.cacheCleanupLoop(c.ctx)

	// Register webhook endpoints
	if err := c.registerWebhookEndpoints(); err != nil {
		return fmt.Errorf("webhook endpoint registration failed: %w", err)
	}

	c.eventLogger.Info("admission controller started")
	return nil
}

// Stop gracefully shuts down admission controller
func (c *KubernetesAdmissionController) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	if c.reconciliationTicker != nil {
		c.reconciliationTicker.Stop()
	}

	c.isRunning = false
	c.eventLogger.Info("admission controller stopped")
}

// ValidateResource evaluates resource against all applicable policies
func (c *KubernetesAdmissionController) ValidateResource(
	ctx context.Context,
	obj runtime.Object,
	operation admissionv1.Operation,
) *admission.Response {
	startTime := time.Now()
	
	resourceCtx := c.extractResourceContext(obj)
	namespace := resourceCtx.Namespace
	
	nsScope, exists := c.getNamespaceScope(namespace)
	if !exists {
		nsScope = c.buildNamespaceScope(namespace)
		c.cacheNamespaceScope(namespace, nsScope)
	}

	policies := c.collectApplicablePolicies(nsScope, obj)
	if len(policies) == 0 {
		return admission.Allowed("no_policies_found")
	}

	input := c.prepareEvaluationInput(obj, operation)
	queries := c.generatePolicyQueries(policies)
	
	evalResult, err := c.policyEngine.Evaluate(ctx, queries, input)
	if err != nil {
		c.logError(resourceCtx, operation, err)
		return admission.Errored(http.StatusInternalServerError, err)
	}

	evaluationTime := time.Since(startTime)
	response := c.buildAdmissionResponse(evalResult, resourceCtx, evaluationTime)
	
	c.auditEvaluation(response, evalResult, startTime)
	return response
}

// ============================================================================
// Namespace Scoping & Inheritance Management
// ============================================================================

// buildNamespaceScope constructs namespace policy scope with inheritance
func (c *KubernetesAdmissionController) buildNamespaceScope(namespace string) *NamespacePolicyScope {
	scope := &NamespacePolicyScope{
		Namespace:       namespace,
		Policies:        make([]*PolicyBundle, 0),
		InheritFrom:     "",
		OpaquePolicies:  make([]runtime.RawExtension, 0),
		LabelSelectors:  make(map[string]string),
		AnnotationFilters: make(map[string]string),
		CreatedAt:       time.Now(),
	}

	// Get namespace object
	nsObj, err := c.kubeClient.CoreV1().Namespaces().Get(c.ctx, namespace, metav1.GetOptions{})
	if err != nil {
		c.eventLogger.Warnf("failed to get namespace '%s': %v", namespace, err)
		return scope
	}

	// Apply label selectors from annotations
	if labels := nsObj.GetLabels(); labels != nil {
		for key, value := range labels {
			if isPolicyLabel(key) {
				scope.LabelSelectors[key] = value
			}
		}
	}

	// Check parent inheritance annotation
	if parentNs := nsObj.Annotations["policy.cloudai-fusion.io/parent"]; parentNs != "" {
		scope.InheritFrom = parentNs
		parentScope := c.getOrBuildNamespaceScope(parentNs)
		scope.Policies = append(scope.Policies, parentScope.Policies...)
	}

	return scope
}

// namespaceReconciler syncs namespace scopes periodically
func (c *KubernetesAdmissionController) namespaceReconciler(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.reconcileNamespaceScopes()
		}
	}
}

// reconcileNamespaceScores synchronizes all namespace scopes
func (c *KubernetesAdmissionController) reconcileNamespaceScopes() {
	namespaces, err := c.kubeClient.CoreV1().Namespaces().List(c.ctx, metav1.ListOptions{})
	if err != nil {
		c.eventLogger.Errorf("failed to list namespaces: %v", err)
		return
	}

	for _, ns := range namespaces.Items {
		scope, exists := c.getNamespaceScope(ns.Name)
		if !exists || scope.LastEvaluatedAt.IsZero() {
			newScope := c.buildNamespaceScope(ns.Name)
			c.cacheNamespaceScope(ns.Name, newScope)
			c.eventLogger.Infof("new namespace scope created: %s", ns.Name)
		}
	}
}

// ============================================================================
// Resource Lifecycle Event Processing
// ============================================================================

// registerLifecycleHandlers registers Kubernetes watch event handlers
func (c *KubernetesAdmissionController) registerLifecycleHandlers() error {
	eventHandler := &ResourceEventHandler{
		controller:      c,
		serializer:      scheme.Codecs.LegacyCodec(scheme.SchemeGroupVersion),
		handlersMap:     make(map[WatchEventType]ResourceEventHandlerFunc),
		errorHandler:    func(err error) { c.eventLogger.Errorf("event handler error: %v", err) },
		cacheSyncedChan: make(chan struct{}),
	}

	eventHandler.handlersMap[EventCreate] = c.handleCreate
	eventHandler.handlersMap[EventUpdate] = c.handleUpdate
	eventHandler.handlersMap[EventDelete] = c.handleDelete

	c.registerEventHandler(eventHandler)
	return nil
}

// handleCreate processes CREATE operations
func (c *KubernetesAdmissionController) handleCreate(ctx context.Context, event *WatchEvent) error {
	resourceCtx := ResourceContext{
		APIVersion: event.Resource.APIVersion,
		Kind:       event.Resource.Kind,
		Name:       event.Object.(metav1.Object).GetName(),
		Namespace:  event.Namespace,
		UID:        event.Object.(metav1.Object).GetUID(),
	}

	response := c.ValidateResource(ctx, event.Object, admissionv1.Create)
	if response.Allowed {
		c.eventLogger.Infof("create allowed: %s/%s", event.Namespace, resourceCtx.Name)
	} else {
		c.eventLogger.Warnf("create denied: %s/%s - reason: %s", 
			event.Namespace, resourceCtx.Name, response.Result.Message)
	}

	return nil
}

// handleUpdate processes UPDATE operations
func (c *KubernetesAdmissionController) handleUpdate(ctx context.Context, event *WatchEvent) error {
	oldObj := event.Metadata["old_object"].(runtime.Object)
	newObj := event.Object

	// Check if spec changed
	oldSpec, _ := extractSpec(oldObj)
	newSpec, _ := extractSpec(newObj)

	if oldSpec != newSpec {
		response := c.ValidateResource(ctx, newObj, admissionv1.Update)
		if !response.Allowed {
			return fmt.Errorf("update denied by policy: %s", response.Result.Message)
		}
	}

	return nil
}

// handleDelete processes DELETE operations
func (c *KubernetesAdmissionController) handleDelete(ctx context.Context, event *WatchEvent) error {
	// Pre-delete validation for sensitive resources
	resourceKind := event.Resource.Kind
	sensitiveResources := []string{"Secret", "ConfigMap", "RoleBinding"}
	
	for _, kind := range sensitiveResources {
		if resourceKind == kind {
			response := c.ValidateResource(ctx, event.Object, admissionv1.Delete)
			if !response.Allowed {
				c.eventLogger.Warnf("delete denied: %s/%s", event.Namespace, event.Object.(metav1.Object).GetName())
				break
			}
		}
	}

	return nil
}

// ============================================================================
// Admission Response Builders
// ============================================================================

// buildAdmissionResponse constructs Kubernetes admission response
func (c *KubernetesAdmissionController) buildAdmissionResponse(
	evalResult *EvaluationResult,
	resourceCtx ResourceContext,
	executionTime time.Duration,
) *admission.Response {
	if evalResult.Error != nil {
		return admission.Errored(http.StatusInternalServerError, evalResult.Error)
	}

	deniedDecisions := 0
	denyReasons := make([]string, 0)

	for _, decision := range evalResult.Decisions {
		if decision.DecisionType == "deny" {
			deniedDecisions++
			denyReasons = append(denyReasons, decision.Message)
		}
	}

	if deniedDecisions > 0 {
		return admission.Denied(fmt.Sprintf("policy violation: %s", 
			joinStrings(denyReasons, "; ")))
	}

	if len(evalResult.Decisions) > 0 && evalResult.Decisions[0].DecisionType == "warn" {
		return admission.Acknowledgement().
			WithWarning("policy warning").
			WithWarningMessage(joinStrings(denyReasons, "; "))
	}

	return admission.Allowed("all policies passed")
}

// auditEvaluation logs admission decision to audit trail
func (c *KubernetesAdmissionController) auditEvaluation(
	response *admission.Response,
	evalResult *EvaluationResult,
	startTime time.Time,
) {
	event := &AuditEvent{
		AuditID:        generateAuditID(),
		Timestamp:      time.Now(),
		Action:         "evaluate",
		Decision:       getDecisionStatus(response),
		Resource:       ResourceContext{},
		PolicyRefs:     make([]string, 0),
		EvaluationTime: time.Since(startTime),
		Metadata:       make(map[string]interface{}),
	}

	for _, decision := range evalResult.Decisions {
		event.PolicyRefs = append(event.PolicyRefs, decision.PolicyRef)
	}

	c.eventLogger.Audit(event)
}

// ============================================================================
// Webhook Registration & Configuration
// ============================================================================

// setupWebhookServer initializes webhook server
func (c *KubernetesAdmissionController) setupWebhookServer() error {
	serverConfig := &webhook.ServerConfig{
		CertDir: "/var/run/secrets/kubernetes.io/serviceaccount",
	}

	c.webhookServer = &webhook.Server{
		Config: serverConfig,
		Host:   "0.0.0.0",
		Port:   9443,
	}

	return nil
}

// registerWebhookEndpoints registers HTTP webhook handlers
func (c *KubernetesAdmissionController) registerWebhookEndpoints() error {
	http.HandleFunc("/validate", c.handleAdmissionRequest)
	http.HandleFunc("/ready", c.handleReadyCheck)
	return nil
}

// handleAdmissionRequest processes incoming admission review requests
func (c *KubernetesAdmissionController) handleAdmissionRequest(w http.ResponseWriter, r *http.Request) {
	admReq, err := decodeAdmissionRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := context.Background()
	response := c.processAdmissionReview(ctx, admReq)
	
	responseBytes, _ := json.Marshal(response)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(responseBytes)
}

// processAdmissionReview converts request to admission.Review
func (c *KubernetesAdmissionController) processAdmissionReview(
	ctx context.Context,
	req *admissionv1.AdmissionRequest,
) *admissionv1.AdmissionResponse {
	obj, err := decodeObject(req.Object.Raw)
	if err != nil {
		return &admissionv1.AdmissionResponse{
			Result: &metav1.Status{
				Code:    http.StatusBadRequest,
				Message: err.Error(),
			},
		}
	}

	admissionResp := c.ValidateResource(ctx, obj, req.Operation)
	
	return &admissionv1.AdmissionResponse{
		Allowed: admissionResp.Allowed,
		Result: &metav1.Status{
			Code:    int32(http.StatusOK),
			Message: admissionResp.Result.Message,
		},
		PatchType: admissionResp.Patch,
	}
}
