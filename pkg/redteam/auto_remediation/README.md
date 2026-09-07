# Auto Remediation Agent Module

## Overview

Production-grade LLM-based auto remediation agent module for the Red Team platform, integrating DashScope Qwen3 API as primary backend with OpenAI-compatible fallback support.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Orchestrator                              │
│  ┌──────────┐  ┌─────────────┐  ┌──────────────────────┐   │
│  │  LLM Client │ │ Prompt Engine │  │   Report Generator   │   │
│  └──────────┘  └─────────────┘  └──────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
                          │
          ┌───────────────┼───────────────┐
          │               │               │
┌─────────▼──────┐ ┌──────▼───────┐ ┌────▼────────┐
│ Findings       │ │ Compliance   │ │Evidence     │
│ Aggregator     │ │ Enforcer     │ │Ledger       │
└────────────────┘ └──────────────┘ └─────────────┘
```

## Components

### 1. LLM Client (`llm_client.go`)

**Features:**
- Multi-provider support: DashScope (Qwen3), OpenAI (DeepSeek), Ollama, vLLM
- Graceful fallback between providers
- Circuit breaker pattern for resilience
- Automatic retry with exponential backoff
- JSON schema-enforced structured output parsing

**Usage:**
```go
// Initialize client
config := NewDefaultConfig()
client, err := NewLLMClient(config, capability.Real)
if err != nil {
    return err
}

// Simple completion
response, err := client.Complete(ctx, prompt, &CompletionOptions{
    Model: "qwen3.5-256b",
    MaxTokens: 8192,
    Temperature: 0.7,
})

// Chat with messages
messages := []Message{
    {Role: "user", Content: "Analyze this vulnerability..."},
}
response, err := client.Chat(ctx, messages, opts)

// Structured JSON response
var result RemediationResponse
_, err := client.ChatJSON(ctx, messages, &result, opts)
```

**Fallback Strategy:**
1. Try DashScope first (primary Alibaba Cloud backend)
2. Fall back to DeepSeek/OpenAI-compatible APIs
3. Retry failed requests 3 times with backoff
4. Mark unhealthy providers as unavailable

### 2. Prompt Engine (`prompt_engine.go`)

**Template System:**
- `remediation_recommendation` - Generate actionable fixes
- `compliance_verification` - Verify against policies  
- `risk_assessment` - Calculate risk scores
- `executive_summary` - Business-focused summaries
- `poc_generation` - Create proof-of-concept code
- `mitre_mapping` - Map to ATT&CK framework

**Usage:**
```go
engine := NewPromptEngine()

// Generate remediation
data := RemediationData{
    Vulnerability: vuln,
    Findings: findings,
    BusinessContext: "Production environment",
}
remediation, err := engine.GenerateRemediation(ctx, client, data)

// Generate PoC
poc, err := engine.GeneratePoC(ctx, client, vuln)

// Map to MITRE ATT&CK
mapping, err := engine.MapToMitRE(ctx, client, vuln)
```

### 3. Findings Aggregator (`findings_aggregator.go`)

**Capabilities:**
- Group findings by CVE, type, severity, location
- Calculate aggregate risk scores (0-100)
- Identify top-priority remediation targets
- Infer attachment levels (network/application/data/system)

**Aggregation Logic:**
```go
aggregator := NewFindingsAggregator(ctx)

// Add findings from scanners
aggregator.AddMultiple(findings)

// Perform analysis
result := aggregator.Aggregate(ctx)

// Get prioritized items
priorities := result.TopPriorities // Top 5 highest-risk

// Get grouped findings
groups := aggregator.GroupByCVE() // map[string][]Finding
```

### 4. Compliance Enforcer (`compliance_enforcer.go`)

**OBE3 Compliance Rules:**
- OBE3-001: No unvalidated user input
- OBE3-002: Authentication required for sensitive ops
- OBE3-003: Audit logging mandatory
- OBE3-004: Evidence chain integrity
- OBE3-005: No hardcoded secrets
- OBE3-006: Rate limiting on APIs

**Validation Process:**
```go
enforcer := NewComplianceEnforcer("real") // or "simulation"

report, err := enforcer.ValidateRemediation(ctx, remediation)

// Check compliance status
if report.Summary.OverallStatus == StatusCritical {
    // Block deployment
} else if report.Summary.CompliantRate < 90 {
    // Require manual review
}
```

**Decision Modes:**
- `real`: Deny critical violations, warn high severity
- `simulation`: Always warn, never block

### 5. Report Generator (`report_generator.go`)

**Report Types:**
- Full technical remediation report
- Executive summary (business-focused)
- Proof-of-concept code generation
- Markdown export for documentation
- MITRE ATT&CK mapping

**Generate Complete Report:**
```go
generator := NewReportGenerator(client, engine, mode)

report, err := generator.GenerateFullReport(ctx, data)
// Contains:
// - Remmediation recommendations
// - PoC exploitation code
// - MITRE ATT&CK mappings
// - Executive summary
```

### 6. Input Sanitizer (`input_sanitizer.go`)

**Security Features:**
- Blocklist pattern matching (eval, exec, shell commands)
- Null byte injection prevention
- Code block extraction protection
- Prompt instruction override detection
- HTML/script tag filtering

**Sanitization Pipeline:**
1. Remove null bytes
2. Truncate oversized inputs (>10KB)
3. Clean code injection patterns
4. Remove prompt instructions ("ignore previous...")
5. Validate against danger patterns

### 7. Orchestrator (`orchestrator.go`)

**Main Coordination Point:**
```go
orchestrator := NewOrchestrator(
    llmClient,
    promptEngine,
    enforcer,
    reportGenerator,
    evidenceLedger,
    mode,
)

// Process single vulnerability
report, err := orchestrator.ProcessVulnerability(ctx, vuln, findings)

// Bulk processing
reports, err := orchestrator.ProcessBulkVulnerabilities(ctx, vulns)

// Quick report for rapid response
quickReport, err := orchestrator.GenerateQuickReport(ctx, vuln, topFindings)
```

## Integration with Red Team Platform

### Configuration

Add to `.env`:
```bash
DASHSCOPE_API_KEY=your_dashscope_key_here
OPENAI_API_KEY=your_deepseek_key_here

# Optional fallback endpoints
DASHSCOPE_API_BASE=https://dashscope.aliyuncs.com/compatible-mode/v1
OPENAI_API_BASE=https://api.deepseek.com/v1
```

### Plug into Red Team Controller

```go
// In pkg/redteam/controller.go or similar

func initAutoRemediation(r *redTeamController) error {
    // Initialize LLM client
    config := NewDefaultConfig()
    llmClient, err := NewLLMClient(config, r.capMode)
    if err != nil {
        return err
    }
    
    // Initialize components
    promptEngine := NewPromptEngine()
    enforcer := NewComplianceEnforcer(string(r.capMode))
    reportGen := NewReportGenerator(llmClient, promptEngine, string(r.capMode))
    
    // Create orchestrator
    r.autoRemediation = NewOrchestrator(
        llmClient,
        promptEngine,
        enforcer,
        reportGen,
        r.evidenceLedger,
        string(r.capMode),
    )
    
    return nil
}

// Expose via API endpoint
func (r *redTeamController) RemediateVulnerability(c *gin.Context) {
    var req RemediateRequest
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(400, gin.H{"error": err.Error()})
        return
    }
    
    ctx, cancel := context.WithTimeout(c.Request.Context(), time.Minute*2)
    defer cancel()
    
    report, err := r.autoRemediation.ProcessVulnerability(
        ctx, 
        req.Vulnerability,
        req.Findings,
    )
    
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }
    
    c.JSON(200, report)
}
```

## Testing

Run unit tests:
```bash
cd cloudai-fusion/pkg/redteam/auto_remediation
go test -v -coverprofile=coverage.out ./...
go tool cover -html=coverage.out  # View coverage report
```

Expected coverage: ≥80%

Test scenarios:
- LLM client initialization and provider fallback
- Prompt injection prevention
- Finding aggregation accuracy
- Compliance rule validation
- Report generation quality

## Usage Examples

### Example 1: Process SQL Injection Vulnerability

```go
vuln := Vulnerability{
    CVE:         "CVE-2024-1234",
    Type:        "SQL Injection",
    Description: "User input not sanitized in login form",
    CVSSScore:   9.5,
    Component:   "auth-service",
}

findings := []Finding{
    {
        ID: "F-001",
        Type: "SQL Injection",
        Severity: Critical,
        CVSSScore: 9.5,
        Location: "/api/auth/login",
        Evidence: "POST /login?user=admin' OR '1'='1",
    },
}

ctx := context.Background()
report, err := orchestrator.ProcessVulnerability(ctx, vuln, findings)

// Output includes:
// - Root cause analysis
// - Immediate fixes with code snippets
// - Long-term prevention strategies
// - PoC exploit code
// - MITRE ATT&CK mapping (T1190: Exploit Public-Facing Application)
// - Executive summary
```

### Example 2: Executive Summary Generation

```go
summaryData := SummaryData{
    VulnCount:              15,
    HighCriticalCount:      8,
    AssetCount:             5,
    BusinessRisk:           "Customer data exposure risk",
    KeyFindings:            []string{"Critical SQLi in payment gateway"},
    AttackScenarios:        []string{"Attacker → SQLi → Database → Customer PII"},
    BudgetEstimate:         "$80,000 - $100,000 USD",
    Timeline:               "3-5 weeks",
}

summary, err := promptEngine.GenerateExecutiveSummary(ctx, client, summaryData)

/* Output example:

**Security Vulnerability Assessment Summary**

Eight (8) critical vulnerabilities identified across five production systems,
posing significant customer data exposure risk. Primary concern is a critical
SQL injection flaw in the payment gateway that could allow unauthorized access
to sensitive financial information.

**Recommended Actions:**
1. Immediate patching of SQL injection within 24 hours
2. Conduct security audit of all API endpoints
3. Implement Web Application Firewall (WAF) rules
4. Initiate incident response procedures

**Resource Requirements:**
- Budget: $80,000-$100,000 USD
- Timeline: 3-5 weeks
- Team: Security Engineers + DevOps

*/
```

## Production Deployment Checklist

- [ ] Configure DashScope API key (required)
- [ ] Set up DeepSeek/OpenAI fallback key (recommended)
- [ ] Integrate with existing evidence ledger
- [ ] Enable circuit breakers (default: 3 failures before skip)
- [ ] Configure rate limits (default: 120s timeout)
- [ ] Set up monitoring for LLM latency/costs
- [ ] Run compliance validation in simulation mode first
- [ ] Establish human review workflow for critical decisions
- [ ] Document prompt templates and customization points
- [ ] Test fallback behavior manually

## Performance Characteristics

- **Latency**: 2-10 seconds per vulnerability (depends on model)
- **Throughput**: ~5-10 vulnerabilities/minute (sequential)
- **Token Usage**: ~4K tokens per remediation request
- **Memory Footprint**: ~50MB base + overhead

## Security Considerations

1. **Input Validation**: All user-provided text goes through sanitizer
2. **Prompt Protection**: Instruction override attempts detected
3. **Output Filtering**: Potential unsafe code patterns reviewed
4. **Evidence Chain**: All decisions recorded for audit trail
5. **API Security**: Credentials stored in secure vault/environment variables

## Future Enhancements

- [ ] Fine-tuned models on historical remediation data
- [ ] Multi-turn dialogue for clarification questions
- [ ] Automated fix application (not just recommendations)
- [ ] Integration with dependency managers (npm, pip, go mod)
- [ ] Real-time threat intelligence enrichment
- [ ] Cost optimization (token budgeting, caching)
- [ ] A/B testing different model configurations
- [ ] Explainable AI (why this specific fix recommended)

## Contact & Support

For issues or questions:
- Review logs in `auto_remediation/logger.go`
- Check dash scope API availability
- Verify fallback configuration
- Consult SKILL.md for best practices

## License

Proprietary - CloudAI Fusion Red Team Platform
Internal use only until further notice
