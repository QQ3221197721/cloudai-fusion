# CloudAI Fusion Red Team Platform - Developer Guide

**Version**: v1.0.0  
**Last Updated**: September 2026  

---

## Table of Contents

1. [Architecture Overview](#architecture-overview)
2. [Code Structure](#code-structure)
3. [Core Components](#core-components)
4. [Adding New Features](#adding-new-features)
5. [Testing Guidelines](#testing-guidelines)
6. [Performance Optimization](#performance-optimization)
7. [Deployment Strategies](#deployment-strategies)
8. [API Integration Patterns](#api-integration-patterns)

---

## Architecture Overview

### System Design Principles

The CloudAI Fusion Red Team Platform follows these core principles:

1. **Authorization First**: Every action requires cryptographic scope validation
2. **Evidence Chain**: All operations recorded in tamper-proof hash chain
3. **Modular Attack Vectors**: Pluggable exploit modules with standardized interfaces
4. **AI-Enhanced Planning**: LLM integration for intelligent attack path generation
5. **Compliance by Design**: Reports generated automatically from structured data

### High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    Presentation Layer                           │
├──────────────┬────────────────┬────────────────┬────────────────┤
│   Web UI     │   REST API     │   gRPC API     │   CLI Tool     │
└──────────────┴────────────────┴────────────────┴────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────┐
│                   Application Layer                             │
├──────────────┬────────────────┬────────────────┬────────────────┤
│   Engagement │  Campaign Mgr  │ Evidence Gen   │ Reporting Svc  │
│   Manager    │                │                │                │
└──────────────┴────────────────┴────────────────┴────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────┐
│                     Core Layer                                  │
├──────────────┬────────────────┬────────────────┬────────────────┤
│ Authorization│ Exploit Orch.  │Attack Graph    │ MITRE Mapping  │
│   Gate       │                │ Builder        │ Engine         │
└──────────────┴────────────────┴────────────────┴────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────┐
│                  Infrastructure Layer                           │
├──────────────┬────────────────┬────────────────┬────────────────┤
│  PostgreSQL  │ Redis Cache    │ Evidence Ledger│ External Tools │
│  (State)     │ (Session)      │ (Hash Chain)   │ (Metasploit,   │
│              │                │                │  Nmap, etc.)   │
└──────────────┴────────────────┴────────────────┴────────────────┘
```

### Data Flow Pattern

```
User Request → API Handler → Validation → Business Logic → DB Write
     ↓                                                              ↓
Event Emission ← State Transition ← Evidence Recording ← Response Build
```

---

## Code Structure

### Repository Layout

```
cloudai-fusion/
├── pkg/redteam/                      # Core red team logic
│   ├── engagement.go                 # Engagement lifecycle management
│   ├── evidence.go                   # Cryptographic evidence emission
│   ├── exploit_engine.go             # Binary exploitation framework
│   ├── attack_chain_orchestrator.go  # Multi-stage attack orchestration
│   ├── authorization_gate.go         # Scope-based access control
│   ├── cex3_engine.go               # CEX3 certification engine
│   ├── ad_attack_integration_tests.go  # Active Directory attacks
│   ├── mitre_attandck.go            # MITRE ATT&CK mapping
│   ├── evidence_campaign.go         # Campaign-level evidence aggregation
│   └── models/                       # Data models
│       └── types_core_modules.go    # Core type definitions
│
├── cmd/apiserver/redteam/            # API server integration
│   ├── routes.go                     # REST endpoint routing
│   ├── handlers.go                   # HTTP request handlers
│   └── middleware.go                 # JWT auth & rate limiting
│
├── cmd/redteam-cli/                  # Command-line interface
│   ├── main.go                       # CLI entry point
│   ├── commands/                     # Subcommands
│   │   ├── engage.go                # Run engagements
│   │   ├── report.go                # Generate reports
│   │   └── verify.go                # Verify evidence chains
│
├── api/openapi.yaml                  # OpenAPI 3.0 specification
└── test/
    ├── e2e/                          # End-to-end tests
    ├── integration/                  # Component integration tests
    └── unit/                         # Unit tests
```

### Key Directories

#### `pkg/redteam/`

Contains all core business logic:

| File | Responsibility | Complexity |
|------|----------------|------------|
| `engagement.go` | Lifecycle state machine | High |
| `evidence.go` | Cryptographic receipt emission | Medium |
| `exploit_engine.go` | Payload generation | Medium |
| `attack_chain_orchestrator.go` | Multi-vector coordination | High |
| `authorization_gate.go` | Permission checking | Medium |

#### `cmd/apiserver/redteam/`

HTTP integration layer:

- Routes define REST endpoints
- Handlers convert HTTP requests to domain calls
- Middleware applies cross-cutting concerns

#### `test/`

Test hierarchy:

```
test/
├── e2e/           # Full system tests with real DB
├── integration/   # Mocked external dependencies
└── unit/          # Isolated component tests
```

---

## Core Components

### 1. Engagement Manager

Manages red team engagement lifecycle using a finite state machine.

**Responsibilities**:
- Create engagements with cryptographic scope grants
- Enforce state transitions (pending → running → completed)
- Record all actions to evidence ledger
- Prevent unauthorized state changes

**Key Types**:

```go
// Status represents engagement lifecycle states
type Status string

const (
    StatusPending   Status = "pending"
    StatusRunning   Status = "running"
    StatusPaused    Status = "paused"
    StatusAborted   Status = "aborted"
    StatusCompleted Status = "completed"
)

// Engagement holds engagement state and authorization gate
type Engagement struct {
    ID        string     `json:"id"`
    TenantID  string     `json:"tenant_id,omitempty"`
    Scope     Scope      `json:"scope"`
    Status    Status     `json:"status"`
    CreatedBy string     `json:"created_by"`
    CreatedAt time.Time  `json:"created_at"`
    Findings  []*Finding `json:"findings,omitempty"`
    
    gate *Gate       // Authorization boundary
    mu   sync.Mutex // Thread safety
}

// Manager orchestrates multiple engagements
type Manager struct {
    recorder    evidence.Recorder
    logger      *logrus.Logger
    mu          sync.RWMutex
    engagements map[string]*Engagement
}
```

**Usage Example**:

```go
// Initialize manager
recorder := evidence.NewLedger(db)
mgr := redteam.NewManager(recorder, logrus.StandardLogger())

// Create new engagement
scope := redteam.Scope{
    Targets: []string{"example.com"},
    MaxRiskTier: redteam.RiskTierMedium,
}
engagement, err := mgr.Create(ctx, scope, "analyst@example.com")
if err != nil {
    log.Fatal(err)
}

// Start engagement
if err := mgr.Start(engagement.ID); err != nil {
    log.Fatal(err)
}

// Add finding during execution
finding := &redteam.Finding{
    ID:        common.NewUUID(),
    Title:     "SQL Injection Vulnerability",
    Severity:  "critical",
    Technique: "T1190",
}
if err := mgr.AddFinding(ctx, engagement.ID, finding); err != nil {
    log.Fatal(err)
}

// Complete engagement when finished
if err := mgr.Complete(engagement.ID); err != nil {
    log.Fatal(err)
}
```

### 2. Authorization Gate

Enforces scope-based access control for all attack actions.

**Responsibilities**:
- Validate that targets are in-scope
- Check risk tier permissions
- Require human approval for high-risk actions
- Log authorization decisions to evidence chain

**Key Methods**:

```go
// Gate validates action against engagement scope
type Gate struct {
    engagementID string
    scope        Scope
    recorder     evidence.Recorder
    logger       *logrus.Logger
}

// Authorize checks if an action is permitted
func (g *Gate) Authorize(ctx context.Context, action Action) (*AuthorizationResult, error)
    
// RequireApproval returns true for actions needing human oversight
func (g *Gate) RequireApproval(action Action) bool
    
// Abort terminates engagement immediately
func (g *Gate) Abort(ctx context.Context, reason string)
```

**Validation Flow**:

```go
// 1. Check if target is in scope
if !contains(g.scope.Targets, action.Target) {
    return recordScopeDeny(reason: "target out-of-scope")
}

// 2. Check risk tier
if action.RiskTier > g.scope.MaxRiskTier {
    if g.RequireApproval(action) {
        // Wait for human approval
        select {
        case approved := <-approvalChannel:
            if !approved {
                return recordScopeDeny(reason: "human denied")
            }
        case <-ctx.Done():
            return recordScopeDeny(reason: "timeout")
        }
    } else {
        return recordScopeDeny(reason: "risk tier exceeded")
    }
}

// 3. Record authorization and proceed
return recordActionAuthorized(needsApproval: false)
```

### 3. Evidence System

Cryptographic recording of all actions for tamper-proof auditing.

**Design**:
- Each action creates a signed receipt
- Receipts form a hash chain (blockchain-like)
- Verifiers can independently validate completeness
- Zero-knowledge proofs enable privacy-preserving verification

**Receipt Schema**:

```go
// RecordInput defines structured evidence emission
type RecordInput struct {
    Actor   string                 // Who performed action
    Action  string                 // What action type
    Subject string                 // What was acted upon
    Input   map[string]interface{} // Request parameters
    Output  map[string]interface{} // Response result
    Payload map[string]interface{} // Domain-specific details
    Backends []evidence.BackendFact // Backend configuration facts
}
```

**Emission Pattern**:

```go
// Helper function for consistent evidence emission
func emit(ctx context.Context, rec evidence.Recorder, logger *logrus.Logger, input evidence.RecordInput) {
    if rec == nil {
        return // NopRecorder silently ignores
    }
    
    if _, err := rec.Record(ctx, input); err != nil {
        // Log but don't fail - evidence loss shouldn't block operations
        if logger != nil {
            logger.WithError(err).WithField("action", input.Action).Warn("failed to emit evidence")
        }
    }
}

// Usage in authorization
emit(ctx, m.recorder, m.logger, evidence.RecordInput{
    Action:  redteam.ActionActionAuthorized,
    Subject: action.Target,
    Payload: map[string]any{
        "engagement_id": engagementID,
        "technique": action.Technique,
        "allowed": true,
    },
})
```

### 4. Exploit Engine

Framework for generating and executing attack payloads.

**Components**:

```
ExploitEngine
├── ShellcodeGenerator    // Platform-specific payload construction
├── EvasionToolkit        // AV/EDR bypass techniques
├── PostExploitationKit   // Persistence, lateral movement
└── AttackOrchestrator    // Multi-step attack coordination
```

**Extending Exploit Libraries**:

Implement the `ExploitModule` interface:

```go
type ExploitModule interface {
    Name() string
    Description() string
    SupportedPlatforms() []string
    
    // Execute runs the exploit against target
    Execute(ctx context.Context, target Target) (*ExploitResult, error)
    
    // RequiresPrerequisites returns tools/binaries needed
    RequiresPrerequisites() []string
}

// Example implementation
type SQLInjectionExploit struct {}

func (s *SQLInjectionExploit) Name() string { 
    return "sql_injection_auto_detect"
}

func (s *SQLInjectionExploit) Execute(ctx context.Context, target Target) (*ExploitResult, error) {
    // 1. Enumerate endpoints
    endpoints := s.enumerateEndpoints(target.URL)
    
    // 2. Test each parameter
    for _, param := range endpoints.Parameters {
        if s.testParameter(ctx, target.URL, param) {
            return &ExploitResult{
                Success: true,
                Finding: Finding{
                    Title:     "SQL Injection in " + param.Name,
                    Severity:  "critical",
                    Technique: "T1190",
                },
                Evidence: s.captureEvidence(target.URL, param),
            }, nil
        }
    }
    
    return &ExploitResult{Success: false}, nil
}
```

---

## Adding New Features

### Adding a New Attack Vector

**Step 1: Define Module Interface**

```go
// pkg/redteam/exploits/custom_vector.go
package exploits

import (
    "context"
    "github.com/cloudai-fusion/cloudai-fusion/pkg/redteam"
)

// CustomVector implements redteam.ExploitModule interface
type CustomVector struct {
    config CustomVectorConfig
}

type CustomVectorConfig struct {
    TimeoutSeconds int
    Parallelism    int
}

func (c *CustomVector) Name() string {
    return "custom_vector_attack"
}

func (c *CustomVector) Execute(ctx context.Context, target redteam.Target) (*redteam.ExploitResult, error) {
    // Implement your custom attack logic here
    return &redteam.ExploitResult{}, nil
}

func (c *CustomVector) RequiresPrerequisites() []string {
    return []string{"python3", "curl"}
}
```

**Step 2: Register in Registry**

```go
// pkg/redteam/engine.go
func init() {
    registry.Register(&CustomVector{})
}
```

**Step 3: Add Configuration Support**

```yaml
# config/redteam/custom_vector.yaml
custom_vector:
  enabled: true
  timeout_seconds: 30
  parallelism: 5
  targets:
    include: ["*.example.com"]
    exclude: ["staging.example.com"]
```

### Extending Report Generation

**Create New Template**:

```go
// pkg/reporting/custom_template.go
package reporting

type CustomTemplate struct {
    findings []redteam.Finding
    metadata ReportMetadata
}

func (t *CustomTemplate) Generate(ctx context.Context) ([]byte, error) {
    // Use template engine (Go html/template or mustache)
    tmpl := template.Must(template.ParseFiles("templates/custom.tmpl"))
    
    var buf bytes.Buffer
    err := tmpl.Execute(&buf, map[string]interface{}{
        "findings": t.findings,
        "metadata": t.metadata,
        "timestamp": time.Now().UTC(),
    })
    
    return buf.Bytes(), err
}
```

**Register Output Format**:

```go
func init() {
    Formats["custom-markdown"] = func(findings []redteam.Finding) (format.Output, error) {
        return &CustomTemplate{
            findings: findings,
            metadata: extractMetadata(findings),
        }, nil
    }
}
```

### Integrating External Tools

**Wrapper Pattern**:

```go
// pkg/tools/nmap_wrapper.go
package tools

import (
    "os/exec"
    "context"
)

type NmapWrapper struct{}

func (n *NmapWrapper) Scan(ctx context.Context, targets []string) (*NmapResult, error) {
    cmd := exec.CommandContext(ctx, "nmap", "-sS", "-sV", "-oX", "-", strings.Join(targets, " "))
    
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr
    
    if err := cmd.Run(); err != nil {
        return nil, fmt.Errorf("nmap failed: %w, stderr: %s", err, stderr.String())
    }
    
    return parseNmapXML(stdout.Bytes())
}

func parseNmapXML(xmlData []byte) (*NmapResult, error) {
    var result NmapResult
    if err := xml.Unmarshal(xmlData, &result); err != nil {
        return nil, err
    }
    return &result, nil
}
```

**Tool Discovery Protocol**:

```go
// Auto-detect available tools
func DetectAvailableTools() map[string]bool {
    tools := map[string]string{
        "nmap": "/usr/bin/nmap",
        "metasploit": "/opt/metasploit-framework/msfconsole",
        "burpsuite": "/opt/burpsuite/burpsuite",
    }
    
    available := make(map[string]bool)
    for name, path := range tools {
        if _, err := exec.LookPath(path); err == nil {
            available[name] = true
        }
    }
    
    return available
}
```

---

## Testing Guidelines

### Testing Pyramid Strategy

```
         /¯¯¯¯¯¯¯¯¯¯¯¯¯¯¯\
        /  E2E Tests (5%) \
       /___________________\
      /¯¯¯¯¯¯¯¯¯¯¯¯¯¯¯¯¯¯¯¯\
     /  Integration (20%)   \
    /________________________\
   /¯¯¯¯¯¯¯¯¯¯¯¯¯¯¯¯¯¯¯¯¯¯¯¯¯¯\
  /    Unit Tests (75%)       \
 /_____________________________\
```

### Unit Testing Patterns

**Mock-Based Testing**:

```go
// pkg/redteam/engagement_test.go
func TestEngagementLifecycle(t *testing.T) {
    // Arrange
    mockRecorder := &MockRecorder{}
    mgr := redteam.NewManager(mockRecorder, testLogger)
    
    scope := redteam.Scope{
        Targets: []string{"example.com"},
        MaxRiskTier: redteam.RiskTierLow,
    }
    
    engagement, err := mgr.Create(context.Background(), scope, "analyst@test.com")
    assert.NoError(t, err)
    assert.Equal(t, redteam.StatusPending, engagement.Status)
    
    // Act
    err = mgr.Start(engagement.ID)
    
    // Assert
    assert.NoError(t, err)
    assert.Equal(t, redteam.StatusRunning, engagement.Status)
    
    // Verify evidence emitted
    mockRecorder.AssertCalled(t, "Record", mock.MatchedBy(func(record evidence.RecordInput) bool {
        return record.Action == redteam.ActionScopeGrant
    }))
}
```

**Table-Driven Tests**:

```go
func TestStatusTransitions(t *testing.T) {
    tests := []struct {
        name         string
        fromStatus   redteam.Status
        toStatus     redteam.Status
        shouldSucceed bool
    }{
        {"pending_to_running", redteam.StatusPending, redteam.StatusRunning, true},
        {"running_to_completed", redteam.StatusRunning, redteam.StatusCompleted, true},
        {"running_to_pending", redteam.StatusRunning, redteam.StatusPending, false},
        {"completed_to_running", redteam.StatusCompleted, redteam.StatusRunning, false},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            mgr := createTestManager()
            
            engagement := &redteam.Engine{
                Status: tt.fromStatus,
            }
            
            // Simulate transition
            err := simulateTransition(engagement, tt.toStatus)
            
            if tt.shouldSucceed {
                assert.NoError(t, err)
                assert.Equal(t, tt.toStatus, engagement.Status)
            } else {
                assert.Error(t, err)
            }
        })
    }
}

// Mock implementations for testing
type MockRecorder struct {
    mock.Mock
}

func (m *MockRecorder) Record(ctx context.Context, input evidence.RecordInput) (*evidence.Evidence, error) {
    args := m.Called(ctx, input)
    return args.Get(0).(*evidence.Evidence), args.Error(1)
}

func (m *MockRecorder) Store() evidence.Store {
    return nil
}