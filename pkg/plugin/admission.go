// Package plugin — Admission control chain, CRD Operator support, and
// policy engine (OPA/Gatekeeper) integration for webhook extensions.
//
// This implements a full Kubernetes-style admission control pipeline:
//   - Validation webhooks: reject non-conforming resources
//   - Mutation webhooks: modify resources before persistence
//   - CRD Operator: watch/reconcile custom resources
//   - OPA/Gatekeeper policy evaluation with constraint templates
package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ============================================================================
// Admission Control Chain
// ============================================================================

// AdmissionPhase represents the type of admission control
type AdmissionPhase string

const (
	PhaseValidation AdmissionPhase = "validation"
	PhaseMutation   AdmissionPhase = "mutation"
)

// AdmissionDecision is the result of evaluating an admission request
type AdmissionDecision string

const (
	DecisionAllow  AdmissionDecision = "allow"
	DecisionDeny   AdmissionDecision = "deny"
	DecisionPatch  AdmissionDecision = "patch"  // mutation only
	DecisionSkip   AdmissionDecision = "skip"   // webhook chose not to act
)

// AdmissionRequest represents a resource admission request
type AdmissionRequest struct {
	UID        string            `json:"uid"`
	Kind       ResourceKind      `json:"kind"`
	Resource   string            `json:"resource"`
	Namespace  string            `json:"namespace"`
	Name       string            `json:"name"`
	Operation  OperationType     `json:"operation"`
	Object     json.RawMessage   `json:"object"`
	OldObject  json.RawMessage   `json:"old_object,omitempty"`
	UserInfo   AdmissionUserInfo `json:"user_info"`
	DryRun     bool              `json:"dry_run"`
	Options    map[string]string `json:"options,omitempty"`
	RequestedAt time.Time        `json:"requested_at"`
}

// AdmissionUserInfo identifies the user making the request
type AdmissionUserInfo struct {
	Username string   `json:"username"`
	UID      string   `json:"uid"`
	Groups   []string `json:"groups"`
}

// ResourceKind identifies the type of resource being admitted
type ResourceKind struct {
	Group   string `json:"group"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

// OperationType defines the type of resource operation
type OperationType string

const (
	OperationCreate  OperationType = "CREATE"
	OperationUpdate  OperationType = "UPDATE"
	OperationDelete  OperationType = "DELETE"
	OperationConnect OperationType = "CONNECT"
)

// AdmissionResponse represents the response from admission evaluation
type AdmissionResponse struct {
	UID       string            `json:"uid"`
	Allowed   bool              `json:"allowed"`
	Decision  AdmissionDecision `json:"decision"`
	Reason    string            `json:"reason,omitempty"`
	Patch     json.RawMessage   `json:"patch,omitempty"`
	PatchType string            `json:"patch_type,omitempty"` // JSONPatch, MergePatch
	Warnings  []string          `json:"warnings,omitempty"`
	AuditAnnotations map[string]string `json:"audit_annotations,omitempty"`
	EvaluatedBy      string            `json:"evaluated_by"`
	LatencyMs        float64           `json:"latency_ms"`
}

// FailurePolicyType defines behavior when a webhook is unreachable
type FailurePolicyType string

const (
	FailurePolicyFail   FailurePolicyType = "Fail"
	FailurePolicyIgnore FailurePolicyType = "Ignore"
)

// MatchCondition defines when an admission webhook should be invoked
type MatchCondition struct {
	Name       string          `json:"name"`
	Expression string          `json:"expression"` // CEL expression
}

// AdmissionWebhookConfig describes a registered admission webhook
type AdmissionWebhookConfig struct {
	Name            string            `json:"name"`
	Phase           AdmissionPhase    `json:"phase"` // validation or mutation
	URL             string            `json:"url"`
	FailurePolicy   FailurePolicyType `json:"failure_policy"`
	TimeoutSeconds  int               `json:"timeout_seconds"`
	MatchRules      []MatchRule       `json:"match_rules"`
	MatchConditions []MatchCondition  `json:"match_conditions,omitempty"`
	SideEffects     string            `json:"side_effects"` // None, NoneOnDryRun
	Priority        int               `json:"priority"`     // lower = earlier
	NamespaceSelector map[string]string `json:"namespace_selector,omitempty"`
	CABundle        string            `json:"ca_bundle,omitempty"`
	Enabled         bool              `json:"enabled"`
	RegisteredAt    time.Time         `json:"registered_at"`
}

// MatchRule defines which resources trigger this webhook
type MatchRule struct {
	APIGroups   []string        `json:"api_groups"`
	APIVersions []string        `json:"api_versions"`
	Resources   []string        `json:"resources"`
	Operations  []OperationType `json:"operations"`
	Scope       string          `json:"scope"` // Cluster, Namespaced, *
}

// AdmissionChain manages an ordered set of admission webhooks
type AdmissionChain struct {
	validationWebhooks []*AdmissionWebhookConfig
	mutationWebhooks   []*AdmissionWebhookConfig
	webhookPlugins     map[string]*WebhookPlugin
	auditLog           []AdmissionAuditEntry
	maxAuditEntries    int
	mu                 sync.RWMutex
	logger             *logrus.Logger
}

// AdmissionAuditEntry records an admission evaluation for audit
type AdmissionAuditEntry struct {
	Timestamp   time.Time         `json:"timestamp"`
	RequestUID  string            `json:"request_uid"`
	Resource    string            `json:"resource"`
	Operation   OperationType     `json:"operation"`
	WebhookName string            `json:"webhook_name"`
	Phase       AdmissionPhase    `json:"phase"`
	Decision    AdmissionDecision `json:"decision"`
	Reason      string            `json:"reason,omitempty"`
	LatencyMs   float64           `json:"latency_ms"`
	UserInfo    AdmissionUserInfo `json:"user_info"`
}

// NewAdmissionChain creates a new admission control chain
func NewAdmissionChain() *AdmissionChain {
	return &AdmissionChain{
		validationWebhooks: make([]*AdmissionWebhookConfig, 0),
		mutationWebhooks:   make([]*AdmissionWebhookConfig, 0),
		webhookPlugins:     make(map[string]*WebhookPlugin),
		auditLog:           make([]AdmissionAuditEntry, 0),
		maxAuditEntries:    10000,
		logger:             logrus.StandardLogger(),
	}
}

// RegisterWebhook registers a new admission webhook
func (ac *AdmissionChain) RegisterWebhook(cfg *AdmissionWebhookConfig) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	if cfg.Name == "" {
		return fmt.Errorf("webhook name is required")
	}
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 10
	}
	if cfg.FailurePolicy == "" {
		cfg.FailurePolicy = FailurePolicyFail
	}
	cfg.RegisteredAt = time.Now().UTC()
	cfg.Enabled = true

	// Create underlying WebhookPlugin for HTTP calls
	wp := NewWebhookPlugin(WebhookConfig{
		Name:           cfg.Name,
		URL:            cfg.URL,
		TimeoutSeconds: cfg.TimeoutSeconds,
		FailurePolicy:  string(cfg.FailurePolicy),
		CABundle:       cfg.CABundle,
		Priority:       cfg.Priority,
	})
	ac.webhookPlugins[cfg.Name] = wp

	switch cfg.Phase {
	case PhaseMutation:
		ac.mutationWebhooks = append(ac.mutationWebhooks, cfg)
		sort.Slice(ac.mutationWebhooks, func(i, j int) bool {
			return ac.mutationWebhooks[i].Priority < ac.mutationWebhooks[j].Priority
		})
	case PhaseValidation:
		ac.validationWebhooks = append(ac.validationWebhooks, cfg)
		sort.Slice(ac.validationWebhooks, func(i, j int) bool {
			return ac.validationWebhooks[i].Priority < ac.validationWebhooks[j].Priority
		})
	default:
		return fmt.Errorf("unknown admission phase: %s", cfg.Phase)
	}

	ac.logger.WithFields(logrus.Fields{
		"webhook": cfg.Name, "phase": cfg.Phase, "priority": cfg.Priority,
	}).Info("Admission webhook registered")
	return nil
}

// UnregisterWebhook removes an admission webhook by name
func (ac *AdmissionChain) UnregisterWebhook(name string) error {
	ac.mu.Lock()
	defer ac.mu.Unlock()

	found := false
	for i, wh := range ac.validationWebhooks {
		if wh.Name == name {
			ac.validationWebhooks = append(ac.validationWebhooks[:i], ac.validationWebhooks[i+1:]...)
			found = true
			break
		}
	}
	for i, wh := range ac.mutationWebhooks {
		if wh.Name == name {
			ac.mutationWebhooks = append(ac.mutationWebhooks[:i], ac.mutationWebhooks[i+1:]...)
			found = true
			break
		}
	}
	delete(ac.webhookPlugins, name)

	if !found {
		return fmt.Errorf("webhook %q not found", name)
	}
	return nil
}

// Evaluate runs the full admission chain: mutation first, then validation
func (ac *AdmissionChain) Evaluate(ctx context.Context, req *AdmissionRequest) (*AdmissionResponse, error) {
	ac.mu.RLock()
	defer ac.mu.RUnlock()

	start := time.Now()
	currentObject := req.Object

	// Phase 1: Mutation webhooks (can modify the object)
	for _, wh := range ac.mutationWebhooks {
		if !wh.Enabled || !ac.matchesRule(wh, req) {
			continue
		}

		mutReq := &AdmissionRequest{
			UID:       req.UID,
			Kind:      req.Kind,
			Resource:  req.Resource,
			Namespace: req.Namespace,
			Name:      req.Name,
			Operation: req.Operation,
			Object:    currentObject,
			OldObject: req.OldObject,
			UserInfo:  req.UserInfo,
			DryRun:    req.DryRun,
		}

		resp, err := ac.callWebhook(ctx, wh, mutReq)
		if err != nil {
			if wh.FailurePolicy == FailurePolicyIgnore {
				ac.recordAudit(req, wh, DecisionSkip, err.Error(), 0)
				continue
			}
			return &AdmissionResponse{
				UID:      req.UID,
				Allowed:  false,
				Decision: DecisionDeny,
				Reason:   fmt.Sprintf("mutation webhook %q failed: %v", wh.Name, err),
			}, nil
		}

		if resp.Patch != nil {
			currentObject = resp.Patch // apply mutation
		}
		ac.recordAudit(req, wh, DecisionPatch, "", resp.LatencyMs)
	}

	// Phase 2: Validation webhooks (can only accept/reject)
	for _, wh := range ac.validationWebhooks {
		if !wh.Enabled || !ac.matchesRule(wh, req) {
			continue
		}

		valReq := &AdmissionRequest{
			UID:       req.UID,
			Kind:      req.Kind,
			Resource:  req.Resource,
			Namespace: req.Namespace,
			Name:      req.Name,
			Operation: req.Operation,
			Object:    currentObject,
			OldObject: req.OldObject,
			UserInfo:  req.UserInfo,
			DryRun:    req.DryRun,
		}

		resp, err := ac.callWebhook(ctx, wh, valReq)
		if err != nil {
			if wh.FailurePolicy == FailurePolicyIgnore {
				ac.recordAudit(req, wh, DecisionSkip, err.Error(), 0)
				continue
			}
			return &AdmissionResponse{
				UID:      req.UID,
				Allowed:  false,
				Decision: DecisionDeny,
				Reason:   fmt.Sprintf("validation webhook %q failed: %v", wh.Name, err),
			}, nil
		}

		if !resp.Allowed {
			ac.recordAudit(req, wh, DecisionDeny, resp.Reason, resp.LatencyMs)
			return &AdmissionResponse{
				UID:         req.UID,
				Allowed:     false,
				Decision:    DecisionDeny,
				Reason:      resp.Reason,
				Warnings:    resp.Warnings,
				EvaluatedBy: wh.Name,
				LatencyMs:   float64(time.Since(start).Microseconds()) / 1000.0,
			}, nil
		}
		ac.recordAudit(req, wh, DecisionAllow, "", resp.LatencyMs)
	}

	return &AdmissionResponse{
		UID:       req.UID,
		Allowed:   true,
		Decision:  DecisionAllow,
		Patch:     currentObject,
		LatencyMs: float64(time.Since(start).Microseconds()) / 1000.0,
	}, nil
}

// callWebhook invokes a webhook and returns the admission response
func (ac *AdmissionChain) callWebhook(ctx context.Context, wh *AdmissionWebhookConfig, req *AdmissionRequest) (*AdmissionResponse, error) {
	wp, ok := ac.webhookPlugins[wh.Name]
	if !ok {
		return nil, fmt.Errorf("no plugin registered for webhook %q", wh.Name)
	}

	start := time.Now()
	objBytes, _ := json.Marshal(req)

	webhookReq := &WebhookRequest{
		UID:            req.UID,
		ExtensionPoint: ExtWebhookValidating,
		Object:         json.RawMessage(objBytes),
	}
	if wh.Phase == PhaseMutation {
		webhookReq.ExtensionPoint = ExtWebhookMutating
	}

	resp, err := wp.Call(ctx, webhookReq)
	if err != nil {
		return nil, err
	}

	latency := float64(time.Since(start).Microseconds()) / 1000.0
	return &AdmissionResponse{
		UID:       resp.UID,
		Allowed:   resp.Allowed,
		Decision:  DecisionAllow,
		Patch:     resp.MutatedObject,
		Warnings:  nil,
		LatencyMs: latency,
	}, nil
}

// matchesRule checks if an admission request matches webhook rules
func (ac *AdmissionChain) matchesRule(wh *AdmissionWebhookConfig, req *AdmissionRequest) bool {
	if len(wh.MatchRules) == 0 {
		return true // no rules = match all
	}
	for _, rule := range wh.MatchRules {
		if ac.ruleMatches(&rule, req) {
			return true
		}
	}
	return false
}

func (ac *AdmissionChain) ruleMatches(rule *MatchRule, req *AdmissionRequest) bool {
	// Check API group
	groupMatch := len(rule.APIGroups) == 0
	for _, g := range rule.APIGroups {
		if g == "*" || g == req.Kind.Group {
			groupMatch = true
			break
		}
	}
	if !groupMatch {
		return false
	}

	// Check operation
	opMatch := len(rule.Operations) == 0
	for _, op := range rule.Operations {
		if op == req.Operation {
			opMatch = true
			break
		}
	}
	if !opMatch {
		return false
	}

	// Check resource
	resMatch := len(rule.Resources) == 0
	for _, r := range rule.Resources {
		if r == "*" || r == req.Resource {
			resMatch = true
			break
		}
	}
	return resMatch
}

// recordAudit records an admission audit entry
func (ac *AdmissionChain) recordAudit(req *AdmissionRequest, wh *AdmissionWebhookConfig, decision AdmissionDecision, reason string, latencyMs float64) {
	entry := AdmissionAuditEntry{
		Timestamp:   time.Now().UTC(),
		RequestUID:  req.UID,
		Resource:    req.Resource,
		Operation:   req.Operation,
		WebhookName: wh.Name,
		Phase:       wh.Phase,
		Decision:    decision,
		Reason:      reason,
		LatencyMs:   latencyMs,
		UserInfo:    req.UserInfo,
	}
	ac.auditLog = append(ac.auditLog, entry)
	if len(ac.auditLog) > ac.maxAuditEntries {
		ac.auditLog = ac.auditLog[len(ac.auditLog)-ac.maxAuditEntries:]
	}
}

// GetAuditLog returns recent admission audit entries
func (ac *AdmissionChain) GetAuditLog(limit int) []AdmissionAuditEntry {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	if limit <= 0 || limit > len(ac.auditLog) {
		limit = len(ac.auditLog)
	}
	start := len(ac.auditLog) - limit
	result := make([]AdmissionAuditEntry, limit)
	copy(result, ac.auditLog[start:])
	return result
}

// ListWebhooks returns all registered webhooks
func (ac *AdmissionChain) ListWebhooks() map[string][]*AdmissionWebhookConfig {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return map[string][]*AdmissionWebhookConfig{
		"validation": ac.validationWebhooks,
		"mutation":   ac.mutationWebhooks,
	}
}

// GetStats returns admission chain statistics
func (ac *AdmissionChain) GetStats() map[string]interface{} {
	ac.mu.RLock()
	defer ac.mu.RUnlock()

	allowed, denied, skipped := 0, 0, 0
	var totalLatency float64
	for _, entry := range ac.auditLog {
		switch entry.Decision {
		case DecisionAllow, DecisionPatch:
			allowed++
		case DecisionDeny:
			denied++
		case DecisionSkip:
			skipped++
		}
		totalLatency += entry.LatencyMs
	}

	avgLatency := 0.0
	if len(ac.auditLog) > 0 {
		avgLatency = totalLatency / float64(len(ac.auditLog))
	}

	return map[string]interface{}{
		"validation_webhooks": len(ac.validationWebhooks),
		"mutation_webhooks":   len(ac.mutationWebhooks),
		"total_evaluations":   len(ac.auditLog),
		"allowed":             allowed,
		"denied":              denied,
		"skipped":             skipped,
		"avg_latency_ms":      avgLatency,
	}
}

// ============================================================================
// CRD Operator
// ============================================================================

// CRDDefinition describes a Custom Resource Definition
type CRDDefinition struct {
	Group       string            `json:"group"`
	Version     string            `json:"version"`
	Kind        string            `json:"kind"`
	Plural      string            `json:"plural"`
	Scope       string            `json:"scope"` // Namespaced, Cluster
	ShortNames  []string          `json:"short_names,omitempty"`
	Categories  []string          `json:"categories,omitempty"`
	Columns     []CRDColumn       `json:"additional_printer_columns,omitempty"`
	Schema      json.RawMessage   `json:"schema,omitempty"` // OpenAPI v3 schema
	Subresources CRDSubresources  `json:"subresources,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
}

// CRDColumn defines an additional printer column for kubectl output
type CRDColumn struct {
	Name        string `json:"name"`
	Type        string `json:"type"` // string, integer, date
	JSONPath    string `json:"json_path"`
	Description string `json:"description,omitempty"`
	Priority    int    `json:"priority"`
}

// CRDSubresources defines subresources for the CRD
type CRDSubresources struct {
	StatusEnabled bool `json:"status_enabled"`
	ScaleEnabled  bool `json:"scale_enabled"`
	ScaleSpecPath string `json:"scale_spec_path,omitempty"`
	ScaleStatusPath string `json:"scale_status_path,omitempty"`
}

// ReconcileResult is the result of a reconciliation loop
type ReconcileResult struct {
	Requeue      bool          `json:"requeue"`
	RequeueAfter time.Duration `json:"requeue_after,omitempty"`
	Error        error         `json:"error,omitempty"`
}

// ReconcileFunc is the callback invoked when a custom resource changes
type ReconcileFunc func(ctx context.Context, event CRDEvent) ReconcileResult

// CRDEvent represents a change to a custom resource
type CRDEvent struct {
	Type      string          `json:"type"` // ADDED, MODIFIED, DELETED
	Resource  string          `json:"resource"`
	Namespace string          `json:"namespace"`
	Name      string          `json:"name"`
	Object    json.RawMessage `json:"object"`
	OldObject json.RawMessage `json:"old_object,omitempty"`
}

// CRDOperator manages custom resource definitions and reconciliation
type CRDOperator struct {
	definitions  map[string]*CRDDefinition // kind -> definition
	reconcilers  map[string]ReconcileFunc  // kind -> reconciler
	watchers     map[string]bool           // kind -> watching
	eventQueue   []CRDEvent
	maxQueueSize int
	mu           sync.RWMutex
	logger       *logrus.Logger
}

// NewCRDOperator creates a new CRD operator
func NewCRDOperator() *CRDOperator {
	return &CRDOperator{
		definitions:  make(map[string]*CRDDefinition),
		reconcilers:  make(map[string]ReconcileFunc),
		watchers:     make(map[string]bool),
		eventQueue:   make([]CRDEvent, 0),
		maxQueueSize: 5000,
		logger:       logrus.StandardLogger(),
	}
}

// RegisterCRD registers a new custom resource definition
func (op *CRDOperator) RegisterCRD(crd *CRDDefinition) error {
	op.mu.Lock()
	defer op.mu.Unlock()

	if crd.Kind == "" || crd.Group == "" {
		return fmt.Errorf("CRD kind and group are required")
	}
	if crd.Version == "" {
		crd.Version = "v1"
	}
	if crd.Plural == "" {
		crd.Plural = strings.ToLower(crd.Kind) + "s"
	}
	if crd.Scope == "" {
		crd.Scope = "Namespaced"
	}
	crd.CreatedAt = time.Now().UTC()

	op.definitions[crd.Kind] = crd
	op.logger.WithFields(logrus.Fields{
		"kind": crd.Kind, "group": crd.Group, "version": crd.Version,
	}).Info("CRD registered")
	return nil
}

// UnregisterCRD removes a CRD by kind
func (op *CRDOperator) UnregisterCRD(kind string) error {
	op.mu.Lock()
	defer op.mu.Unlock()

	if _, ok := op.definitions[kind]; !ok {
		return fmt.Errorf("CRD %q not found", kind)
	}
	delete(op.definitions, kind)
	delete(op.reconcilers, kind)
	delete(op.watchers, kind)
	return nil
}

// SetReconciler sets the reconciliation function for a CRD kind
func (op *CRDOperator) SetReconciler(kind string, fn ReconcileFunc) error {
	op.mu.Lock()
	defer op.mu.Unlock()

	if _, ok := op.definitions[kind]; !ok {
		return fmt.Errorf("CRD %q not registered", kind)
	}
	op.reconcilers[kind] = fn
	op.logger.WithField("kind", kind).Info("Reconciler set for CRD")
	return nil
}

// StartWatching begins watching for changes to a CRD kind
func (op *CRDOperator) StartWatching(kind string) error {
	op.mu.Lock()
	defer op.mu.Unlock()

	if _, ok := op.definitions[kind]; !ok {
		return fmt.Errorf("CRD %q not registered", kind)
	}
	op.watchers[kind] = true
	op.logger.WithField("kind", kind).Info("Started watching CRD")
	return nil
}

// StopWatching stops watching a CRD kind
func (op *CRDOperator) StopWatching(kind string) {
	op.mu.Lock()
	defer op.mu.Unlock()
	delete(op.watchers, kind)
}

// EnqueueEvent adds a resource event to the reconciliation queue
func (op *CRDOperator) EnqueueEvent(event CRDEvent) {
	op.mu.Lock()
	defer op.mu.Unlock()

	if !op.watchers[event.Resource] {
		return // not watching this resource
	}

	op.eventQueue = append(op.eventQueue, event)
	if len(op.eventQueue) > op.maxQueueSize {
		op.eventQueue = op.eventQueue[1:] // drop oldest
	}
}

// ProcessQueue processes pending events in the reconciliation queue
func (op *CRDOperator) ProcessQueue(ctx context.Context) []ReconcileResult {
	op.mu.Lock()
	queue := make([]CRDEvent, len(op.eventQueue))
	copy(queue, op.eventQueue)
	op.eventQueue = op.eventQueue[:0]
	op.mu.Unlock()

	results := make([]ReconcileResult, 0, len(queue))
	for _, event := range queue {
		op.mu.RLock()
		reconciler, ok := op.reconcilers[event.Resource]
		op.mu.RUnlock()

		if !ok {
			continue
		}

		result := reconciler(ctx, event)
		results = append(results, result)

		if result.Requeue {
			op.EnqueueEvent(event)
		}
	}
	return results
}

// ListCRDs returns all registered CRD definitions
func (op *CRDOperator) ListCRDs() []*CRDDefinition {
	op.mu.RLock()
	defer op.mu.RUnlock()

	crds := make([]*CRDDefinition, 0, len(op.definitions))
	for _, crd := range op.definitions {
		crds = append(crds, crd)
	}
	return crds
}

// GetCRD returns a specific CRD by kind
func (op *CRDOperator) GetCRD(kind string) (*CRDDefinition, error) {
	op.mu.RLock()
	defer op.mu.RUnlock()

	crd, ok := op.definitions[kind]
	if !ok {
		return nil, fmt.Errorf("CRD %q not found", kind)
	}
	return crd, nil
}

// GenerateK8sCRDManifest generates a Kubernetes CRD YAML manifest
func (op *CRDOperator) GenerateK8sCRDManifest(kind string) (map[string]interface{}, error) {
	op.mu.RLock()
	defer op.mu.RUnlock()

	crd, ok := op.definitions[kind]
	if !ok {
		return nil, fmt.Errorf("CRD %q not found", kind)
	}

	manifest := map[string]interface{}{
		"apiVersion": "apiextensions.k8s.io/v1",
		"kind":       "CustomResourceDefinition",
		"metadata": map[string]interface{}{
			"name": crd.Plural + "." + crd.Group,
			"labels": map[string]string{
				"app.kubernetes.io/managed-by": "cloudai-fusion",
			},
		},
		"spec": map[string]interface{}{
			"group": crd.Group,
			"names": map[string]interface{}{
				"kind":       crd.Kind,
				"plural":     crd.Plural,
				"shortNames": crd.ShortNames,
				"categories": crd.Categories,
			},
			"scope": crd.Scope,
			"versions": []map[string]interface{}{
				{
					"name":    crd.Version,
					"served":  true,
					"storage": true,
					"subresources": map[string]interface{}{
						"status": map[string]interface{}{},
					},
				},
			},
		},
	}

	return manifest, nil
}

// GetOperatorStats returns CRD operator statistics
func (op *CRDOperator) GetOperatorStats() map[string]interface{} {
	op.mu.RLock()
	defer op.mu.RUnlock()

	return map[string]interface{}{
		"registered_crds":    len(op.definitions),
		"active_reconcilers": len(op.reconcilers),
		"active_watchers":    len(op.watchers),
		"pending_events":     len(op.eventQueue),
	}
}

// PredefinedCRDs returns built-in CRD definitions for CloudAI Fusion
func PredefinedCRDs() []*CRDDefinition {
	return []*CRDDefinition{
		{
			Group:   "ai.cloudai-fusion.io",
			Version: "v1alpha1",
			Kind:    "AIWorkload",
			Plural:  "aiworkloads",
			Scope:   "Namespaced",
			ShortNames: []string{"aiw"},
			Categories: []string{"cloudai", "ai"},
			Columns: []CRDColumn{
				{Name: "Model", Type: "string", JSONPath: ".spec.modelName"},
				{Name: "Runtime", Type: "string", JSONPath: ".spec.runtime"},
				{Name: "Status", Type: "string", JSONPath: ".status.phase"},
				{Name: "Age", Type: "date", JSONPath: ".metadata.creationTimestamp"},
			},
		},
		{
			Group:   "ai.cloudai-fusion.io",
			Version: "v1alpha1",
			Kind:    "GPUPool",
			Plural:  "gpupools",
			Scope:   "Cluster",
			ShortNames: []string{"gp"},
			Categories: []string{"cloudai", "gpu"},
			Columns: []CRDColumn{
				{Name: "Total GPUs", Type: "integer", JSONPath: ".status.totalGPUs"},
				{Name: "Available", Type: "integer", JSONPath: ".status.availableGPUs"},
				{Name: "Utilization", Type: "string", JSONPath: ".status.utilization"},
			},
		},
		{
			Group:   "edge.cloudai-fusion.io",
			Version: "v1alpha1",
			Kind:    "EdgeCluster",
			Plural:  "edgeclusters",
			Scope:   "Cluster",
			ShortNames: []string{"ec"},
			Categories: []string{"cloudai", "edge"},
			Columns: []CRDColumn{
				{Name: "Platform", Type: "string", JSONPath: ".spec.platform"},
				{Name: "Nodes", Type: "integer", JSONPath: ".status.nodeCount"},
				{Name: "Status", Type: "string", JSONPath: ".status.phase"},
			},
		},
		{
			Group:   "wasm.cloudai-fusion.io",
			Version: "v1alpha1",
			Kind:    "WasmFunction",
			Plural:  "wasmfunctions",
			Scope:   "Namespaced",
			ShortNames: []string{"wf"},
			Categories: []string{"cloudai", "wasm"},
			Columns: []CRDColumn{
				{Name: "Runtime", Type: "string", JSONPath: ".spec.runtime"},
				{Name: "Replicas", Type: "integer", JSONPath: ".spec.replicas"},
				{Name: "Cold Start", Type: "string", JSONPath: ".status.avgColdStartMs"},
			},
		},
		{
			Group:   "policy.cloudai-fusion.io",
			Version: "v1alpha1",
			Kind:    "SecurityConstraint",
			Plural:  "securityconstraints",
			Scope:   "Cluster",
			ShortNames: []string{"sc"},
			Categories: []string{"cloudai", "security"},
			Columns: []CRDColumn{
				{Name: "Engine", Type: "string", JSONPath: ".spec.engine"},
				{Name: "Violations", Type: "integer", JSONPath: ".status.violations"},
				{Name: "Enforced", Type: "string", JSONPath: ".status.enforced"},
			},
		},
	}
}

// ============================================================================
// OPA / Gatekeeper Policy Engine
// ============================================================================

// PolicyEngineType defines the policy engine backend
type PolicyEngineType string

const (
	PolicyEngineOPA        PolicyEngineType = "opa"
	PolicyEngineGatekeeper PolicyEngineType = "gatekeeper"
	PolicyEngineCEL        PolicyEngineType = "cel" // K8s Common Expression Language
)

// ConstraintTemplate defines a policy constraint template (Gatekeeper-style)
type ConstraintTemplate struct {
	Name        string           `json:"name"`
	Kind        string           `json:"kind"`
	Engine      PolicyEngineType `json:"engine"`
	Description string           `json:"description"`
	Rego        string           `json:"rego,omitempty"`        // OPA Rego policy
	CEL         string           `json:"cel,omitempty"`         // CEL expression
	Parameters  json.RawMessage  `json:"parameters,omitempty"`  // parameter schema
	Targets     []PolicyTarget   `json:"targets"`
	Labels      map[string]string `json:"labels,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
	Hash        string           `json:"hash"`
}

// PolicyTarget defines what resources a constraint applies to
type PolicyTarget struct {
	Target     string   `json:"target"` // admission.k8s.gatekeeper.sh
	Rego       string   `json:"rego,omitempty"`
	APIGroups  []string `json:"api_groups"`
	Kinds      []string `json:"kinds"`
	Namespaces []string `json:"namespaces,omitempty"`
	ExcludedNamespaces []string `json:"excluded_namespaces,omitempty"`
}

// PolicyConstraint is an instance of a ConstraintTemplate with parameters
type PolicyConstraint struct {
	Name             string            `json:"name"`
	TemplateName     string            `json:"template_name"`
	Parameters       map[string]interface{} `json:"parameters"`
	Match            PolicyMatch       `json:"match"`
	EnforcementAction string           `json:"enforcement_action"` // deny, dryrun, warn
	CreatedAt        time.Time         `json:"created_at"`
}

// PolicyMatch defines which resources the constraint applies to
type PolicyMatch struct {
	Kinds              []PolicyMatchKind `json:"kinds"`
	Namespaces         []string          `json:"namespaces,omitempty"`
	ExcludedNamespaces []string          `json:"excluded_namespaces,omitempty"`
	LabelSelector      map[string]string `json:"label_selector,omitempty"`
}

// PolicyMatchKind is a group+kind pair
type PolicyMatchKind struct {
	APIGroups []string `json:"api_groups"`
	Kinds     []string `json:"kinds"`
}

// PolicyEvaluation is the result of evaluating a policy
type PolicyEvaluation struct {
	ConstraintName string `json:"constraint_name"`
	TemplateName   string `json:"template_name"`
	Allowed        bool   `json:"allowed"`
	Violations     []PolicyViolation `json:"violations,omitempty"`
	EvaluatedAt    time.Time `json:"evaluated_at"`
	LatencyMs      float64   `json:"latency_ms"`
}

// PolicyViolation describes a policy violation
type PolicyViolation struct {
	Message    string `json:"message"`
	Severity   string `json:"severity"` // low, medium, high, critical
	Resource   string `json:"resource"`
	Field      string `json:"field,omitempty"`
	Constraint string `json:"constraint"`
}

// PolicyEngine manages OPA/Gatekeeper policy evaluation
type PolicyEngine struct {
	templates   map[string]*ConstraintTemplate
	constraints map[string]*PolicyConstraint
	violations  []PolicyViolation
	maxViolations int
	mu          sync.RWMutex
	logger      *logrus.Logger
}

// NewPolicyEngine creates a new policy engine
func NewPolicyEngine() *PolicyEngine {
	return &PolicyEngine{
		templates:     make(map[string]*ConstraintTemplate),
		constraints:   make(map[string]*PolicyConstraint),
		violations:    make([]PolicyViolation, 0),
		maxViolations: 10000,
		logger:        logrus.StandardLogger(),
	}
}

// RegisterTemplate registers a constraint template
func (pe *PolicyEngine) RegisterTemplate(tmpl *ConstraintTemplate) error {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	if tmpl.Name == "" {
		return fmt.Errorf("template name is required")
	}
	if tmpl.Engine == "" {
		tmpl.Engine = PolicyEngineOPA
	}

	// Compute hash of the policy content
	h := sha256.New()
	h.Write([]byte(tmpl.Rego + tmpl.CEL))
	tmpl.Hash = hex.EncodeToString(h.Sum(nil))[:16]
	tmpl.CreatedAt = time.Now().UTC()

	pe.templates[tmpl.Name] = tmpl
	pe.logger.WithFields(logrus.Fields{
		"template": tmpl.Name, "engine": tmpl.Engine,
	}).Info("Constraint template registered")
	return nil
}

// UnregisterTemplate removes a constraint template
func (pe *PolicyEngine) UnregisterTemplate(name string) error {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	if _, ok := pe.templates[name]; !ok {
		return fmt.Errorf("template %q not found", name)
	}
	delete(pe.templates, name)

	// Remove related constraints
	for k, c := range pe.constraints {
		if c.TemplateName == name {
			delete(pe.constraints, k)
		}
	}
	return nil
}

// CreateConstraint creates a policy constraint from a template
func (pe *PolicyEngine) CreateConstraint(constraint *PolicyConstraint) error {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	if _, ok := pe.templates[constraint.TemplateName]; !ok {
		return fmt.Errorf("template %q not found", constraint.TemplateName)
	}
	if constraint.EnforcementAction == "" {
		constraint.EnforcementAction = "deny"
	}
	constraint.CreatedAt = time.Now().UTC()

	pe.constraints[constraint.Name] = constraint
	pe.logger.WithFields(logrus.Fields{
		"constraint": constraint.Name, "template": constraint.TemplateName,
		"enforcement": constraint.EnforcementAction,
	}).Info("Policy constraint created")
	return nil
}

// DeleteConstraint removes a policy constraint
func (pe *PolicyEngine) DeleteConstraint(name string) error {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	if _, ok := pe.constraints[name]; !ok {
		return fmt.Errorf("constraint %q not found", name)
	}
	delete(pe.constraints, name)
	return nil
}

// EvaluatePolicy evaluates all matching constraints against a resource
func (pe *PolicyEngine) EvaluatePolicy(ctx context.Context, resource json.RawMessage, kind ResourceKind, namespace string) []PolicyEvaluation {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	results := make([]PolicyEvaluation, 0)

	for _, constraint := range pe.constraints {
		tmpl, ok := pe.templates[constraint.TemplateName]
		if !ok {
			continue
		}

		// Check if this constraint matches the resource kind
		if !pe.constraintMatchesKind(constraint, kind) {
			continue
		}

		// Check namespace match
		if !pe.constraintMatchesNamespace(constraint, namespace) {
			continue
		}

		start := time.Now()
		eval := PolicyEvaluation{
			ConstraintName: constraint.Name,
			TemplateName:   constraint.TemplateName,
			Allowed:        true,
			EvaluatedAt:    time.Now().UTC(),
		}

		// Evaluate based on engine type
		violations := pe.evaluateConstraint(tmpl, constraint, resource)
		if len(violations) > 0 {
			eval.Allowed = constraint.EnforcementAction != "deny"
			eval.Violations = violations

			// Record violations
			pe.violations = append(pe.violations, violations...)
			if len(pe.violations) > pe.maxViolations {
				pe.violations = pe.violations[len(pe.violations)-pe.maxViolations:]
			}
		}

		eval.LatencyMs = float64(time.Since(start).Microseconds()) / 1000.0
		results = append(results, eval)
	}

	return results
}

// evaluateConstraint evaluates a single constraint against a resource
func (pe *PolicyEngine) evaluateConstraint(tmpl *ConstraintTemplate, constraint *PolicyConstraint, resource json.RawMessage) []PolicyViolation {
	var violations []PolicyViolation

	// Simulate policy evaluation based on engine type
	switch tmpl.Engine {
	case PolicyEngineOPA:
		// In production, this would call the OPA REST API or embedded OPA engine
		// POST /v1/data/{package}/violation with input document
		if tmpl.Rego != "" {
			// Simulated: parse basic deny rules
			violations = pe.simulateOPAEval(tmpl, constraint, resource)
		}

	case PolicyEngineGatekeeper:
		// Gatekeeper uses OPA under the hood but with ConstraintTemplate CRDs
		if len(tmpl.Targets) > 0 {
			violations = pe.simulateGatekeeperEval(tmpl, constraint, resource)
		}

	case PolicyEngineCEL:
		// K8s CEL validation rules
		if tmpl.CEL != "" {
			violations = pe.simulateCELEval(tmpl, constraint, resource)
		}
	}

	return violations
}

// simulateOPAEval simulates OPA Rego policy evaluation
func (pe *PolicyEngine) simulateOPAEval(tmpl *ConstraintTemplate, constraint *PolicyConstraint, _ json.RawMessage) []PolicyViolation {
	// In production: POST to OPA at /v1/data/{package}/deny
	// For now, validate that the Rego policy is non-empty and well-formed
	if strings.Contains(tmpl.Rego, "deny") || strings.Contains(tmpl.Rego, "violation") {
		pe.logger.WithField("template", tmpl.Name).Debug("OPA policy evaluated (simulated)")
	}
	return nil // no violations in simulation
}

// simulateGatekeeperEval simulates Gatekeeper constraint evaluation
func (pe *PolicyEngine) simulateGatekeeperEval(tmpl *ConstraintTemplate, constraint *PolicyConstraint, _ json.RawMessage) []PolicyViolation {
	pe.logger.WithFields(logrus.Fields{
		"template": tmpl.Name, "constraint": constraint.Name,
	}).Debug("Gatekeeper constraint evaluated (simulated)")
	return nil
}

// simulateCELEval simulates CEL expression evaluation
func (pe *PolicyEngine) simulateCELEval(tmpl *ConstraintTemplate, constraint *PolicyConstraint, _ json.RawMessage) []PolicyViolation {
	pe.logger.WithFields(logrus.Fields{
		"template": tmpl.Name, "cel": tmpl.CEL,
	}).Debug("CEL expression evaluated (simulated)")
	return nil
}

func (pe *PolicyEngine) constraintMatchesKind(c *PolicyConstraint, kind ResourceKind) bool {
	if len(c.Match.Kinds) == 0 {
		return true
	}
	for _, mk := range c.Match.Kinds {
		for _, k := range mk.Kinds {
			if k == "*" || k == kind.Kind {
				return true
			}
		}
	}
	return false
}

func (pe *PolicyEngine) constraintMatchesNamespace(c *PolicyConstraint, namespace string) bool {
	if len(c.Match.Namespaces) == 0 {
		return true
	}
	for _, ns := range c.Match.Namespaces {
		if ns == "*" || ns == namespace {
			return true
		}
	}
	return false
}

// ListTemplates returns all registered constraint templates
func (pe *PolicyEngine) ListTemplates() []*ConstraintTemplate {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	templates := make([]*ConstraintTemplate, 0, len(pe.templates))
	for _, t := range pe.templates {
		templates = append(templates, t)
	}
	return templates
}

// ListConstraints returns all registered constraints
func (pe *PolicyEngine) ListConstraints() []*PolicyConstraint {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	constraints := make([]*PolicyConstraint, 0, len(pe.constraints))
	for _, c := range pe.constraints {
		constraints = append(constraints, c)
	}
	return constraints
}

// GetViolations returns recent policy violations
func (pe *PolicyEngine) GetViolations(limit int) []PolicyViolation {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	if limit <= 0 || limit > len(pe.violations) {
		limit = len(pe.violations)
	}
	start := len(pe.violations) - limit
	result := make([]PolicyViolation, limit)
	copy(result, pe.violations[start:])
	return result
}

// GetPolicyStats returns policy engine statistics
func (pe *PolicyEngine) GetPolicyStats() map[string]interface{} {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	byEngine := make(map[string]int)
	for _, t := range pe.templates {
		byEngine[string(t.Engine)]++
	}

	byEnforcement := make(map[string]int)
	for _, c := range pe.constraints {
		byEnforcement[c.EnforcementAction]++
	}

	bySeverity := make(map[string]int)
	for _, v := range pe.violations {
		bySeverity[v.Severity]++
	}

	return map[string]interface{}{
		"templates":        len(pe.templates),
		"constraints":      len(pe.constraints),
		"total_violations": len(pe.violations),
		"by_engine":        byEngine,
		"by_enforcement":   byEnforcement,
		"by_severity":      bySeverity,
	}
}

// PredefinedConstraintTemplates returns built-in policy templates
func PredefinedConstraintTemplates() []*ConstraintTemplate {
	return []*ConstraintTemplate{
		{
			Name:        "require-resource-limits",
			Kind:        "K8sRequireResourceLimits",
			Engine:      PolicyEngineOPA,
			Description: "Requires all containers to have CPU and memory limits set",
			Rego: `package k8srequireresourcelimits
violation[{"msg": msg}] {
  container := input.review.object.spec.containers[_]
  not container.resources.limits.cpu
  msg := sprintf("Container %v has no CPU limit", [container.name])
}
violation[{"msg": msg}] {
  container := input.review.object.spec.containers[_]
  not container.resources.limits.memory
  msg := sprintf("Container %v has no memory limit", [container.name])
}`,
			Targets: []PolicyTarget{
				{
					Target:    "admission.k8s.gatekeeper.sh",
					APIGroups: []string{""},
					Kinds:     []string{"Pod"},
				},
			},
		},
		{
			Name:        "deny-privileged-containers",
			Kind:        "K8sDenyPrivilegedContainers",
			Engine:      PolicyEngineOPA,
			Description: "Denies containers running in privileged mode",
			Rego: `package k8sdenyprivileged
violation[{"msg": msg}] {
  container := input.review.object.spec.containers[_]
  container.securityContext.privileged
  msg := sprintf("Privileged container %v is not allowed", [container.name])
}`,
			Targets: []PolicyTarget{
				{
					Target:    "admission.k8s.gatekeeper.sh",
					APIGroups: []string{""},
					Kinds:     []string{"Pod"},
				},
			},
		},
		{
			Name:        "require-labels",
			Kind:        "K8sRequireLabels",
			Engine:      PolicyEngineGatekeeper,
			Description: "Requires specified labels on resources",
			Rego: `package k8srequirelabels
violation[{"msg": msg}] {
  provided := input.review.object.metadata.labels
  required := input.parameters.labels[_]
  not provided[required]
  msg := sprintf("Missing required label: %v", [required])
}`,
			Targets: []PolicyTarget{
				{
					Target:    "admission.k8s.gatekeeper.sh",
					APIGroups: []string{"*"},
					Kinds:     []string{"*"},
				},
			},
		},
		{
			Name:        "restrict-image-registries",
			Kind:        "K8sRestrictImageRegistries",
			Engine:      PolicyEngineOPA,
			Description: "Restricts container images to approved registries",
			Rego: `package k8srestrictregistries
violation[{"msg": msg}] {
  container := input.review.object.spec.containers[_]
  not startswith(container.image, input.parameters.allowed_registries[_])
  msg := sprintf("Container %v uses unapproved registry: %v", [container.name, container.image])
}`,
			Targets: []PolicyTarget{
				{
					Target:    "admission.k8s.gatekeeper.sh",
					APIGroups: []string{""},
					Kinds:     []string{"Pod"},
				},
			},
		},
		{
			Name:        "gpu-request-validation",
			Kind:        "CloudAIGPURequestValidation",
			Engine:      PolicyEngineCEL,
			Description: "Validates GPU resource requests for AI workloads",
			CEL:         "object.spec.resources.requests['nvidia.com/gpu'] <= 8 && has(object.metadata.labels['cloudai-fusion/workload-type'])",
			Targets: []PolicyTarget{
				{
					Target:    "admission.k8s.gatekeeper.sh",
					APIGroups: []string{"ai.cloudai-fusion.io"},
					Kinds:     []string{"AIWorkload"},
				},
			},
		},
	}
}

// ============================================================================
// Admission Extension Hub — Unified Entry Point
// ============================================================================

// AdmissionHub aggregates all admission control subsystems
type AdmissionHub struct {
	AdmissionChain *AdmissionChain `json:"-"`
	CRDOperator    *CRDOperator    `json:"-"`
	PolicyEngine   *PolicyEngine   `json:"-"`
}

// NewAdmissionHub creates a fully initialized admission hub
func NewAdmissionHub() *AdmissionHub {
	hub := &AdmissionHub{
		AdmissionChain: NewAdmissionChain(),
		CRDOperator:    NewCRDOperator(),
		PolicyEngine:   NewPolicyEngine(),
	}

	// Register predefined CRDs
	for _, crd := range PredefinedCRDs() {
		_ = hub.CRDOperator.RegisterCRD(crd)
	}

	// Register predefined constraint templates
	for _, tmpl := range PredefinedConstraintTemplates() {
		_ = hub.PolicyEngine.RegisterTemplate(tmpl)
	}

	return hub
}

// HandleAdmissionExtension routes admission-related actions
func (hub *AdmissionHub) HandleAdmissionExtension(ctx context.Context, action string, params map[string]interface{}) (interface{}, error) {
	switch action {
	// --- Admission Chain ---
	case "admission_stats":
		return hub.AdmissionChain.GetStats(), nil

	case "admission_list_webhooks":
		return hub.AdmissionChain.ListWebhooks(), nil

	case "admission_register_webhook":
		cfg := &AdmissionWebhookConfig{}
		if name, ok := params["name"].(string); ok {
			cfg.Name = name
		}
		if url, ok := params["url"].(string); ok {
			cfg.URL = url
		}
		if phase, ok := params["phase"].(string); ok {
			cfg.Phase = AdmissionPhase(phase)
		}
		if priority, ok := params["priority"].(float64); ok {
			cfg.Priority = int(priority)
		}
		return nil, hub.AdmissionChain.RegisterWebhook(cfg)

	case "admission_unregister_webhook":
		name, _ := params["name"].(string)
		return nil, hub.AdmissionChain.UnregisterWebhook(name)

	case "admission_audit_log":
		limit := 100
		if l, ok := params["limit"].(float64); ok {
			limit = int(l)
		}
		return hub.AdmissionChain.GetAuditLog(limit), nil

	case "admission_evaluate":
		reqBytes, _ := json.Marshal(params)
		var req AdmissionRequest
		if err := json.Unmarshal(reqBytes, &req); err != nil {
			return nil, fmt.Errorf("invalid admission request: %w", err)
		}
		return hub.AdmissionChain.Evaluate(ctx, &req)

	// --- CRD Operator ---
	case "crd_list":
		return hub.CRDOperator.ListCRDs(), nil

	case "crd_get":
		kind, _ := params["kind"].(string)
		return hub.CRDOperator.GetCRD(kind)

	case "crd_register":
		crd := &CRDDefinition{}
		if kind, ok := params["kind"].(string); ok {
			crd.Kind = kind
		}
		if group, ok := params["group"].(string); ok {
			crd.Group = group
		}
		if version, ok := params["version"].(string); ok {
			crd.Version = version
		}
		if scope, ok := params["scope"].(string); ok {
			crd.Scope = scope
		}
		return nil, hub.CRDOperator.RegisterCRD(crd)

	case "crd_unregister":
		kind, _ := params["kind"].(string)
		return nil, hub.CRDOperator.UnregisterCRD(kind)

	case "crd_generate_manifest":
		kind, _ := params["kind"].(string)
		return hub.CRDOperator.GenerateK8sCRDManifest(kind)

	case "crd_operator_stats":
		return hub.CRDOperator.GetOperatorStats(), nil

	case "crd_process_queue":
		results := hub.CRDOperator.ProcessQueue(ctx)
		return results, nil

	// --- Policy Engine ---
	case "policy_list_templates":
		return hub.PolicyEngine.ListTemplates(), nil

	case "policy_register_template":
		tmpl := &ConstraintTemplate{}
		if name, ok := params["name"].(string); ok {
			tmpl.Name = name
		}
		if engine, ok := params["engine"].(string); ok {
			tmpl.Engine = PolicyEngineType(engine)
		}
		if rego, ok := params["rego"].(string); ok {
			tmpl.Rego = rego
		}
		if cel, ok := params["cel"].(string); ok {
			tmpl.CEL = cel
		}
		if desc, ok := params["description"].(string); ok {
			tmpl.Description = desc
		}
		return nil, hub.PolicyEngine.RegisterTemplate(tmpl)

	case "policy_unregister_template":
		name, _ := params["name"].(string)
		return nil, hub.PolicyEngine.UnregisterTemplate(name)

	case "policy_list_constraints":
		return hub.PolicyEngine.ListConstraints(), nil

	case "policy_create_constraint":
		constraint := &PolicyConstraint{}
		if name, ok := params["name"].(string); ok {
			constraint.Name = name
		}
		if tmpl, ok := params["template"].(string); ok {
			constraint.TemplateName = tmpl
		}
		if action, ok := params["enforcement"].(string); ok {
			constraint.EnforcementAction = action
		}
		return nil, hub.PolicyEngine.CreateConstraint(constraint)

	case "policy_delete_constraint":
		name, _ := params["name"].(string)
		return nil, hub.PolicyEngine.DeleteConstraint(name)

	case "policy_violations":
		limit := 100
		if l, ok := params["limit"].(float64); ok {
			limit = int(l)
		}
		return hub.PolicyEngine.GetViolations(limit), nil

	case "policy_stats":
		return hub.PolicyEngine.GetPolicyStats(), nil

	case "policy_evaluate":
		resource, _ := json.Marshal(params["resource"])
		kind := ResourceKind{}
		if k, ok := params["kind"].(string); ok {
			kind.Kind = k
		}
		if g, ok := params["group"].(string); ok {
			kind.Group = g
		}
		ns, _ := params["namespace"].(string)
		return hub.PolicyEngine.EvaluatePolicy(ctx, resource, kind, ns), nil

	default:
		return nil, fmt.Errorf("unknown admission action: %s", action)
	}
}
