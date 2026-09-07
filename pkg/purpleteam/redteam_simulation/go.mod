# Red Team Simulation Module

Provides safe, defensive vulnerability assessment capabilities for OBCE3 red team simulation without actual exploitation.

## Components

### BufferOverflowScanner
- Static analysis of C/C++/Rust source code for buffer overflow vulnerabilities  
- Safe string extraction from compiled binaries (no execution)
- Pattern-based detection using regex rulesets
- CWE mapping and severity classification

### ADSecurityAuditor
- Active Directory security posture assessment
- Policy compliance checking (password policies, account lockout)
- Group policy validation
- Audit logging verification

## Usage

```go
package main

import (
    "github.com/cloudai-fusion/cloudai-fusion/pkg/purpleteam/redteam_simulation"
)

func main() {
    // Initialize scanner with configuration
    simulator := redteam_simulation.NewRedTeamSimulator(
        redteam_simulation.ScannerConfig{
            MaxRecursionDepth: 15,
            EnableAdvancedChecks: true,
        },
    )
    
    // Define target environment
    env := redteam_simulation.Environment{
        TargetPath: "/path/to/target",
        TargetType: "directory",
    }
    
    // Run vulnerability assessment (DEFENSIVE - NO EXPLOITATION!)
    report, err := simulator.IdentifyVulnerabilities(env)
    if err != nil {
        panic(err)
    }
    
    // Process results
    fmt.Printf("Found %d vulnerabilities\n", len(report.Findings))
    for _, finding := range report.Findings {
        fmt.Printf("[%s] %s: %s\n", 
            finding.Severity, finding.Title, finding.Mitigation)
    }
    
    // Get remediation guidance
    for _, guide := range report.RemediationGuidance {
        fmt.Printf("Priority %d: %s -> %s\n", 
            guide.Priority, guide.Title, guide.Action)
    }
}
```

## Safety Guarantees

✓ **Static Analysis Only** - No binary execution or exploitation attempts  
✓ **Read-Only Scanning** - Does not modify target systems  
✓ **Safe Patterns** - All detection patterns use regex matching, no payload injection  
✓ **Defensive Focus** - Emphasizes identification and remediation over exploitation  

## Output Format

Returns comprehensive `AssessmentReport` containing:
- Structured findings with metadata
- Risk scores (0.0-10.0)
- Severity levels (Critical/High/Medium/Low/Info)
- CWE mappings
- Remediation guidance with code examples
- Strategic recommendations

See complete examples in `test_data/` directory.
