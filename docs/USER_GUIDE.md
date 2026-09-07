# CloudAI Fusion Red Team Platform - User Guide

**Complete End-to-End Usage Manual for Enterprise Security Testing**

---

<div align="center">

## 📋 Table of Contents

1. [Getting Started](#section1-getting-started)
2. [Sandbox Mode: Automated Vulnerability Discovery](#section2-sandbox-mode)
3. [Production Mode: Authorized Penetration Testing](#section3-production-mode)
4. [Frontend Console Navigation](#section4-frontend-console)
5. [API Reference](#section5-api-reference)
6. [Troubleshooting FAQ](#section6-troubleshooting)

---

## <a name="section1-getting-started"></a>Section 1: Getting Started

### Prerequisites Checklist ✅

Before beginning any assessment, verify your environment meets these minimum requirements:

#### Required Software Versions

| Component | Minimum Version | Recommended Version | Verification Command |
|-----------|-----------------|---------------------|----------------------|
| **Vagrant** | 3.1.0 | 4.x | `vagrant --version` |
| **VirtualBox** | 7.0.x | 7.2+ | `VBoxManage --version` |
| **Go Runtime** | 1.25.0 | 1.25.7+ | `go version` |
| **Node.js** | 18.0.0 | 20.x LTS | `node --version` |
| **npm** | 9.0.0 | 10.x | `npm --version` |
| **Docker** | 24.0.0 (optional) | 26.0+ | `docker --version` |

#### System Resource Requirements

```bash
# Check available disk space (minimum 50GB required)
df -h /  # macOS/Linux
Get-Volume C:  # Windows PowerShell

# Verify RAM availability (minimum 8GB for sandbox mode)
free -h  # Linux
systeminfo  # Windows
top -mem  # macOS

# Ensure CPU cores sufficient (minimum 4 cores recommended)
nproc  # Linux/macOS
Get-CimInstance Win32_Processor  # Windows
```

#### Network Connectivity

Ensure outbound access to the following endpoints during installation:

| Hostname | Port | Protocol | Purpose |
|----------|------|----------|---------|
| `raw.githubusercontent.com` | 443 | HTTPS | Go module downloads |
| `releases.vagrantup.com` | 443 | HTTPS | Vagrant updates |
| `download.virtualbox.com` | 443 | HTTPS | VirtualBox Guest Additions ISO |
| `registry.npmjs.org` | 443 | HTTPS | Frontend dependencies |

### First-Time Setup Walkthrough

Follow these steps to prepare your development environment:

#### Step 1: Install Go Development Environment

**Windows:**

```powershell
# Download latest Go installer from https://go.dev/dl/
# Run installer and follow wizard prompts

# After installation, configure module cache location
$env:GOMODCACHE = "E:\go\pkg\mod"
Add-Content $env:USERPROFILE\.bash_profile "`nexport GOMODCACHE=`"E:\go\pkg\mod`""

# Verify successful installation
go version
# Expected output: go version go1.25.x windows/amd64
```

**Linux (Ubuntu):**

```bash
# Install Go 1.25 from official repository
wget https://go.dev/dl/go1.25.linux-amd64.tar.gz
sudo rm -rf /usr/local/go  # Remove old version if exists
sudo tar -C /usr/local -xzvf go1.25.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
echo 'export GOMODCACHE=/path/to/cache/go/pkg/mod' >> ~/.bashrc
source ~/.bashrc

# Verify
go version
```

#### Step 2: Install Virtualization Tools

```powershell
# Windows - Chocolatey package manager (recommended)
choco install vagrant virtualbox git -y

# Linux - Ubuntu APT repository
wget -q https://www.virtualbox.org/download/oracle_vbox_2016.asc -O- | sudo apt-key add -
echo "deb http://download.virtualbox.com/virtualbox/debian $(lsb_release -cs) contrib" | sudo tee /etc/apt/sources.list.d/virtualbox.list
sudo apt update
sudo apt install virtualbox-7.0 vagrant -y

# macOS - Homebrew
brew install --cask vagrant virtualbox
```

#### Step 3: Clone Repository and Initialize Modules

```bash
git clone https://github.com/cloudai-fusion/cloudai-fusion.git
cd cloudai-fusion

# Download all Go dependencies
go mod download
go mod tidy  # Resolves transitive dependencies

# Build core binaries (verifies compilation success)
go build -o bin/redteam ./cmd/redteam/main.go
go build -o bin/api-server ./cmd/apiserver/main.go

# Verify builds produced executables
ls -lh bin/
```

#### Step 4: Install Web Console Dependencies

```bash
cd cloudai-fusion-web
npm install

# This will download React framework, Axios HTTP client, Zustand state manager
# Typical duration: 3-5 minutes depending on network speed

npm run dev
# Expected output:
#   Ready on http://localhost:3000
#   Open browser to see management console
```

### Understanding the Two Modes

CloudAI Fusion operates in **two distinct modes** with strict operational boundaries:

#### Sandbox Mode (Development & Training)

**Purpose**: Safe environment for learning exploitation techniques and testing new CVEs without risking production systems.

| Characteristic | Details |
|---------------|---------|
| **Network Isolation** | Air-gapped subnet `192.168.200.0/24` managed by Vagrant |
| **Execution Limit** | No destructive payloads; reconnaissance-only probing |
| **Authorization** | None required - inherently isolated by design |
| **Rate Limiting** | Unlimited scan concurrency (maximize throughput) |
| **Use Case** | Training exercises, tool validation, vulnerability research |
| **Duration** | As long as VMs remain running (`vagrant up`) |

#### Production Mode (Authorized Engagements)

**Purpose**: Conduct penetration tests against real target systems owned by clients.

| Characteristic | Details |
|---------------|---------|
| **Authorization Required** | Multi-level approval workflow (PM → Security Officer → CEO) |
| **Legal Documents** | Client contract, NDA signed, IP range scope validated |
| **Time Window** | Business hours only (9AM-6PM local timezone enforced) |
| **Rate Limiting** | Max 50 probes/sec per target to avoid service disruption |
| **Audit Trail** | Every action cryptographically hashed and stored in Merkle tree |
| **Post-Engagement** | Mandatory cleanup: delete temp files, rotate credentials |
| **Use Case** | Customer security assessments, compliance testing |

⚠️ **Critical Warning**: Attempting to execute production scans without valid authorization will trigger automatic alerts to platform administrators and may result in account termination and legal action.

---

## <a name="section2-sandbox-mode"></a>Section 2: Sandbox Mode: Automated Vulnerability Discovery

### Step-by-Step Vagrant Deployment

Deploy pre-configured vulnerable VMs for safe exploitation practice:

#### Step 1: Navigate to Vagrant Configuration Directory

```powershell
cd pkg/redteam/vagrant
dir
# Expected contents:
#   Vagrantfile                          # Master VM orchestration config
#   boxes/                               # Prebuilt VM image references
#     ├── metasploitable-3.box
#     ├── cve-target-web.box
#     └── cve-target-db.box
```

#### Step 2: Review Vagrantfile Settings

Open `Vagrantfile` in your preferred editor and verify network configuration:

```ruby
# Extract from Vagrantfile showing air-gap network setup
config.vm.network "private_network", ip: "192.168.200.10"  # Metasploitable
config.vm.network "private_network", ip: "192.168.200.20"  # Web CVE Target
config.vm.network "private_network", ip: "192.168.200.30"  # Database CVE Target
```

This private NAT network ensures no external connectivity during testing.

#### Step 3: Launch Vulnerable Targets

```powershell
# Provision all VMs simultaneously
vagrant up

# Monitor progress output:
# ==> metasploitable-3: Box not found in local cache, downloading...
# ==> metasploitable-3: Downloading: https://.../metasploitable-3.box
# ==> metasploitable-3: Importing box into VirtualBox...
# ==> metasploitable-3: The guest additions on this VM do not match the installed version...
# ==> metasploitable-3: Updating VirtualBox Guest Additions...
# ==> metasploitable-3: Checking for host interconnectedness...
# ==> metasploitable-3: Waiting for machine to boot...
# ==> metasploitable-3: Machine booted successfully!
# ==> cve-target-web: Similar provisioning sequence...
# ==> cve-target-db: Similar provisioning sequence...
```

Expected total duration: **15-20 minutes** depending on download speed.

#### Step 4: Verify VM Health

```powershell
# List all active VMs
vagrant status

# Expected output:
# Current    State     Machine
# running    running   metasploitable-3 (192.168.200.10)
# running    running   cve-target-web (192.168.200.20)
# running    running   cve-target-db (192.168.200.30)

# Test SSH connectivity to first target
vagrant ssh metasploitable-3

# You should login to the guest OS:
# ubuntu@metasploitable-3's password: vagrant
# Welcome to Ubuntu 20.04.4 LTS (GNU/Linux 5.4.0-128-generic x86_64)
```

Press `Ctrl+D` or type `exit` to return to host.

### Running Automated Vulnerability Scans

Execute comprehensive CVE discovery using Q-Learning optimized attack paths:

#### Basic Scan Command

```powershell
# Full system enumeration against metasploitable-3
go run cmd/redteam/main.go `
    --mode sandbox `
    --target 192.168.200.10 `
    --output-format json `
    --output-path ./reports/sandbox-scan.json
```

**Expected Terminal Output:**

```text
[INFO] Initializing CloudAI Fusion Red Team Platform v1.0.0
[SANDBOX] Operational mode: UNRESTRICTED_CONCURRENCY (safe isolation verified)
[QLEARN] Loading exploit knowledge base from exploits/db.jsonl (950 entries)
[QLEARN] Initializing Q-table with ε-greedy exploration rate: 0.1

[SCAN] Starting port sweep on target 192.168.200.10...
[SCAN] Probing ports 21,22,23,25,53,80,110,111,135,139,143,161,443,445,993,995,1433,3306,3389,5432,8080...
[SERVICE DISCOVERY] Detected open services:
  ├─ Port 21/tcp: FTP (vsftpd 2.3.4)
  ├─ Port 22/tcp: SSH (OpenSSH 5.1p1)
  ├─ Port 80/tcp: HTTP (Apache Tomcat 6.0.20)
  ├─ Port 139/tcp: NetBIOS (Samba 3.0.25a)
  └─ Port 3306/tcp: MySQL (5.1.38)

[CVE MATCHING] Correlating service signatures against CVE database...
[CVE DETECTED] Apache Struts2 S2-045 Remote Code Execution → CVE-2017-5638 (CVSS: 9.8 CRITICAL)
[CVE DETECTED] vsftpd 2.3.4 Backdoor User List → CVE-2011-2523 (CVSS: 10.0 CRITICAL)
[CVE DETECTED] Samba MS06-040 Buffer Overflow → CVE-2006-5804 (CVSS: 9.3 HIGH)
[CVE DETECTED] MySQL 5.1 Authentication Bypass → CVE-2008-5832 (CVSS: 7.5 MEDIUM)

[Q-LEARNING] Iteration 1/500: State=[FTP,HTTP,SSH], Action=probe_SMB, Reward=-0.1 (no useful data)
[Q-LEARNING] Iteration 100/500: Converging... Best path score: 0.423
[Q-LEARNING] Iteration 347/500: Optimal policy discovered! Final reward: 0.942
[ATTACK PATH] Highest-confidence exploitation chain:
  1. Exploit vsftpd 2.3.4 (CVE-2011-2523) → Obtain shell
  2. Privilege escalation via Samba (CVE-2006-5804) → Root access
  3. Persistence via cron job modification

[EVIDENCE] Computing SHA-256 hash chain for findings integrity...
[REPORT] Generating risk assessment report: reports/sandbox-scan.json
[COMPLETE] Scan finished in 12.3s. Reviewed 4 critical CVEs.
```

#### Advanced Scan Options

```powershell
# Specify custom timeout settings (default: 5 seconds per probe)
go run cmd/redteam/main.go `
    --target 192.168.200.10 `
    --timeout-per-port 3s `
    --scan-timeout 600s `
    --threads 500

# Filter CVE detection by severity threshold (e.g., only show HIGH+)
go run cmd/redteam/main.go `
    --target 192.168.200.10 `
    --severity-filter high

# Enable detailed logging for debugging
go run cmd/redteam/main.go `
    --target 192.168.200.10 `
    --log-level debug

# Export results in multiple formats simultaneously
go run cmd/redteam/main.go `
    --target 192.168.200.10 `
    --output-format json,csv,html `
    --output-dir ./reports
```

### Interpreting Scan Results

After a scan completes, analyze the generated JSON report:

#### Viewing Raw JSON Report

```powershell
# Pretty-print JSON report
cat reports/sandbox-scan.json | jq '.'

# Sample excerpt from findings structure:
{
  "scan_id": "7f3b9e2a-1c4d-4e8b-9f6a-2d5c8b1e3a4f",
  "timestamp": "2024-06-15T14:32:17Z",
  "target": "192.168.200.10",
  "status": "complete",
  "findings": [
    {
      "id": "f1a2b3c4-d5e6-7f8g-9h0i-j1k2l3m4n5o6",
      "cve_id": "CVE-2011-2523",
      "service": "vsftpd 2.3.4",
      "port": 21,
      "cvss_score": 10.0,
      "severity": "critical",
      "description": "Hidden .rhosts file exposed sensitive information...",
      "mitre_attack": ["TA0001:Initial Access", "T1190:Exploit Public-Facing Application"],
      "remediation": "Upgrade to vsftpd >= 2.3.5"
    }
  ],
  "summary": {
    "total_findings": 4,
    "by_severity": {
      "critical": 2,
      "high": 1,
      "medium": 1,
      "low": 0
    }
  }
}
```

#### Severity Classification Standard

The platform uses CVSS v3.1 scoring to classify vulnerabilities:

| Severity | CVSS Score Range | Color Code | Response Priority |
|----------|------------------|------------|-------------------|
| **CRITICAL** | 9.0–10.0 | 🔴 Red | Immediate (within 24 hours) |
| **HIGH** | 7.0–8.9 | 🟠 Orange | Urgent (within 7 days) |
| **MEDIUM** | 4.0–6.9 | 🟡 Yellow | Scheduled (within 30 days) |
| **LOW** | 0.1–3.9 | 🟢 Green | Consider in next maintenance window |
| **INFO** | 0.0 | ⚪ Gray | Informational only (no action required) |

#### MITRE ATT&CK Mapping Interpretation

Each finding links to tactics/techniques within MITRE ATT&CK framework:

```json
"mitre_attack": [
  "TA0001:Initial Access",
  "TA0002:Execution",
  "T1190:Exploit Public-Facing Application",
  "T1068:Exploitation of Weaponized Vulnerabilities"
]
```

This mapping helps security teams understand the attacker kill chain stage where each CVE fits.

### Generating Risk Assessments

Filter and aggregate scan findings into actionable intelligence:

#### By Severity Threshold

```powershell
# Show only critical and high-severity issues
jq '[.findings[] | select(.severity == "critical" or .severity == "high")]' \
   reports/sandbox-scan.json > reports/critical-findings.json

# Count items per severity level
jq '.summary.by_severity' reports/sandbox-scan.json
```

#### By Service Type

```powershell
# Filter all HTTP-related CVEs
jq '[.findings[] | select(.port == 80 or .port == 443)]' reports/sandbox-scan.json

# Group findings by port number
jq 'group_by(.port) | map({port: .[0].port, count: length, cves: [.[] | .cve_id]})' \
   reports/sandbox-scan.json
```

#### Compliance Gap Analysis Template

Generate a report comparing findings against CIS Benchmark controls:

```javascript
// Custom script to cross-reference CVEs with CIS controls
const fs = require('fs');
const data = JSON.parse(fs.readFileSync('./reports/sandbox-scan.json'));

// Map CVEs to CIS Control IDs (example mapping logic)
const controlMapping = {
  'CVE-2011-2523': ['CIS 3.3: Secure Configuration'],
  'CVE-2017-5638': ['CIS 4.1: Input Validation'],
  // ... extend with full matrix
};

const gaps = data.findings.map(f => ({
  cve: f.cve_id,
  affected_control: controlMapping[f.cve_id] || ['Unknown'],
  remediation_status: 'Not Compliant'
}));

fs.writeFileSync('./reports/cis-gaps.json', JSON.stringify(gaps, null, 2));
```

---

## <a name="section3-production-mode"></a>Section 3: Production Mode Workflow

Executing penetration tests in production requires strict adherence to legal and safety protocols. Follow this end-to-end workflow:

### Submitting Work Order Request Form

Initiate an authorized engagement by completing the web-based work order submission:

#### Step 1: Access Authorization Portal

Navigate to the Red Team console: `https://your-instance.cloudai-fusion.io/auth/production-request`

You must be logged in with **Role-Based Access Control (RBAC)** elevation to "Red Team Operator" role.

#### Step 2: Fill Out Required Fields

**Work Order Request Form:**

| Field Name | Data Type | Valid Values | Validation Rules |
|------------|-----------|--------------|------------------|
| **Target Organization** | Text | Free text | Must match signed contract entity name |
| **Target IP Ranges** | CIDR list | e.g., `203.0.113.0/24` | Maximum 10 CIDRs per request; must be owned by client |
| **Scan Start Time** | ISO 8601 datetime | `2024-06-15T09:00:00-07:00` | Must fall within business hours (9AM–6PM local time) |
| **Scan Duration** | Duration string | e.g., `4h`, `30m` | Maximum 8-hour continuous operation |
| **Assessment Scope** | Checkbox list | ✓ Port scanning<br/>✓ Service fingerprinting<br/>✓ CVE exploitation simulation<br/>❌ Do not test denial-of-service | Cannot include DoS unless explicitly approved |
| **Emergency Contact** | Email + Phone | Valid format | Required for abort procedures |

**Important**: All fields are mandatory. Incomplete forms will be rejected with error code `ERR_VALIDATION_FAILED`.

#### Step 3: Upload Supporting Documentation

Attach the following PDF documents (each ≤ 10MB):

1. **Client Contract Copy**: Signed agreement authorizing red team activities
2. **Non-Disclosure Agreement (NDA)**: Dated and executed by both parties
3. **Scope Validation Certificate**: Third-party confirmation that IP ranges belong to client
4. **Legal Authorization Letter**: Issued by client's general counsel office

Upload via drag-and-drop interface or use "Select Files" button. Files are encrypted at rest using AES-256.

### Multi-Level Approval Process Visualization

Once submitted, your request enters a hierarchical approval workflow:

```mermaid
graph LR
    A[Operator Submits Request] --> B[Project Manager Review]
    B -->|Reject| Z[Notify Operator: Reason]
    B -->|Approve→C[Security Officer Validation]
    C -->|Reject| Z
    C -->|Approve→D[Legal Compliance Check]
    D -->|Reject| Z
    D -->|Pass→E[CEO Executive Approval]
    E -->|Reject| Z
    E -->|Approved→F[Queue for Execution]
    F --> G[Auto-Schedule Based on Time Window]
    G --> H[Notification Sent to Operator]
    
    style A fill:#fff3cd
    style F fill:#d4edda
    style E fill:#cce5ff
```

**Approval SLAs (Service Level Agreements):**

- **PM Review**: Within 4 business hours
- **Security Officer**: Within 8 business hours
- **Legal Compliance**: Within 12 business hours
- **CEO Approval**: Within 24 business hours

Total expected turnaround: **2–5 business days** depending on organizational policies.

### Legal Compliance Checklist

Before executing any production scan, confirm you have completed ALL checklist items:

```markdown
## Production Engagement Pre-Flight Checklist

### Authorization Documentation
- [ ] Signed statement of work (SOW) explicitly mentions penetration testing
- [ ] NDA current and covers all jurisdictions involved
- [ ] IP address ownership proof matches scan targets
- [ ] Emergency contact verified and reachable during window

### Technical Safeguards
- [ ] Rate limit configured to ≤50 probes/sec per target
- [ ] Kill switch command documented and tested
- [ ] Temporary storage encryption enabled (AES-256)
- [ ] Audit log destination verified (SIEM integration check)

### Operational Controls
- [ ] Scan scheduled within business hours window
- [ ] Change advisory board (CAB) ticket created (if enterprise environment)
- [ ] Rollback plan prepared (who to call if services degrade)
- [ ] Post-engagement cleanup procedure reviewed

### Acknowledgment
By clicking "Begin Assessment", I affirm:
- I am authorized under written contract to perform these activities
- I understand violations may result in civil/criminal penalties
- I will cease immediately if unauthorized errors or service disruptions occur
```

### Executing Authorized Scan

Once approval granted, receive email notification and proceed with controlled execution:

#### Step 1: Retrieve Approved Token

```bash
# Log in to get JWT token
curl -X POST https://your-instance.cloudai-fusion.io/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"redteam-operator","password":"***"}' | jq .access_token

# Decode token payload to verify expiration
echo "<TOKEN>" | cut -d. -f2 | base64 -d
# Contains: {"role":"operator","scope":["203.0.113.0/24"],"expires":1718462400,...}
```

#### Step 2: Initiate Production Scan

```powershell
$PROD_TOKEN = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."

go run cmd/redteam/main.go `
    --mode production `
    --auth-token $PROD_TOKEN `
    --work-order-id wo_7f3b9e2a1c4d `
    --target 203.0.113.45 `
    --rate-limit 50 `
    --time-window "2024-06-15T09:00:00-07:00/2024-06-15T17:00:00-07:00" \
    --output-format json,human-readable `
    --enable-kill-switch
```

**Enforcement Checks Performed:**

1. **Time Window Validation**: Confirms current timestamp falls within specified range; otherwise exits immediately
2. **Rate Limiting**: Uses token bucket algorithm to throttle requests to 50 probes/sec
3. **Scope Validation**: Verifies target IP exists in approved CIDR list; rejects out-of-bounds attempts
4. **Kill Switch Activation**: Monitors for emergency signal sent via webhook/SMS if operator invokes abort command

**Sample Output:**

```text
[PRODUCTION] Operational mode: RESTRICTED_EXECUTION
[AUTH] Validating JWT signature... OK
[WORK ORDER] WO-7f3b9e2a1c4d: Status=APPROVED, Scope=[203.0.113.45], Expires=2024-06-15T17:00:00Z
[RATE LIMIT] Throttling to 50 requests/sec (token bucket refill rate: 1/20ms)
[TIME WINDOW] Current: 2024-06-15T10:23:17-07:00 → Within allowed window (09:00–17:00)
[KILL SWITCH] Armed and listening for emergency abort signals via HTTPS webhook

[SCAN] Beginning assessment on 203.0.113.45...
[SCAN] Rate-limited port sweep started...
[Q-LEARNING] Initializing exploitation model...
[... scan proceeds normally with audit logging every 100 probes ...]
```

### Post-Engagement Cleanup Procedures

Within 1 hour of scan completion, execute cleanup tasks to maintain compliance:

#### Step 1: Delete Temporary Files

All intermediate artifacts stored in `/tmp/cloudai-fusion/<workorder-id>/` must be removed:

```bash
rm -rf /tmp/cloudai-fusion/wo_7f3b9e2a1c4d/
# Verify deletion
ls /tmp/cloudai-fusion/  # Should show empty directory or missing path
```

#### Step 2: Rotate Credentials

If scan required temporary credential creation (e.g., SSH keys, API tokens), delete them from target systems:

```bash
# Example: Remove authorized_keys entry added during privilege escalation phase
ssh root@203.0.113.45 "grep -v 'TEMPORARY-WO-7f3b9e2a1c4d' ~/.ssh/authorized_keys > ~/.ssh/authorized_keys.new && mv ~/.ssh/authorized_keys.new ~/.ssh/authorized_keys"
```

#### Step 3: Archive Evidence Chain

Retain final findings report for client delivery but purge raw logs:

```bash
# Compress audit trail before deletion (for 90-day retention requirement)
tar czf /backup/evidence-wo_7f3b9e2a1c4d.tar.gz --exclude '*.log' evidence/

# Then delete originals
rm -rf evidence/
```

#### Step 4: Notify Stakeholders

Send closure email to all participants confirming cleanup completion:

```email
Subject: [COMPLETED] Red Team Engagement WO-7f3b9e2a1c4d – Cleanup Confirmed

Dear Client Stakeholders,

This message confirms that the authorized penetration test for 203.0.113.45 has been completed and all cleanup procedures executed successfully:

✅ Temporary files deleted from host systems  
✅ Credential rotation verified  
✅ Evidence archived securely per retention policy  
✅ No residual backdoors or malicious artifacts detected  

Final risk assessment report is attached separately via secure file transfer portal.

If you have questions about any findings, please contact our incident response team at ir@cloudai-fusion.io.

Best regards,  
CloudAI Fusion Red Team  
OBCE3 Certified
```

---

## <a name="section4-frontend-console"></a>Section 4: Frontend Console Usage

The React-based management console provides real-time monitoring and interactive analysis capabilities. Accessed at `http://localhost:3000` after starting `npm run dev`.

### Navigating Dashboard: Q-Learning Attack Path Visualization

The main dashboard displays three panels:

#### Panel 1: Real-Time Attack Graph

Located top-left corner, shows live visualization of Q-Learning agent exploring states:

```
Nodes = Discovered Services
Edges = Exploitation paths scored by Q-values
Colors = Node severity (red=critical CVE, yellow=high, green=low)
Animations = Q-value updates during learning iterations
```

**Interactive Features:**

- **Hover over node**: See service details, CVSS score, list of matching CVEs
- **Click edge**: Expand popover with optimal action sequence (exploit chain)
- **Zoom/Pan**: Mouse wheel zoom, left-click-drag to reposition graph
- **Export Image**: "Download PNG" button saves visual to local filesystem

#### Panel 2: Q-Value Convergence Plot

Top-right area contains line chart tracking cumulative reward over iterations:

```yaml
Axes:
  X-axis: Iteration count (0–500)
  Y-axis: Average reward score (-1.0 to +1.0)
  Line color: Gradient blue (#3B82F6 primary)
  Shaded region: 95% confidence interval around mean
  
Interpretation:
  Upward trend indicates learning progress
  Plateau near iteration 300 suggests convergence
  Oscillations indicate suboptimal action selection requiring hyperparameter tuning
```

#### Panel 3: Live Finding Feed

Bottom section lists newest CVE detections in chronological order:

```typescript
interface FindingCard {
  timestamp: string;          // When detected
  cveId: string;             // e.g., "CVE-2017-5638"
  severity: Critical│High│Medium│Low;  // Determines border color
  service: string;           // Detected software/version
  cvssScore: number;         // 0–10 float
  mitreTags: string[];       // MITRE ATT&CK technique names
  actions: ['View Details'] │['Mark as False Positive'];  // Interactive buttons
}
```

**Filter Controls Above Feed:**

- **Severity Dropdown**: Select "All", "Critical+", "High+", etc.
- **Search Bar**: Text search across CVE descriptions and service names
- **Time Range Picker**: Show only last 10min, 1hour, 5hours, or all results
- **Bulk Actions Button**: Applies selected operation to checked items

### Using Vulnerability Scanner Page

Accessible via sidebar menu item **"Scanner"** — primary interface for launching assessments:

#### Interface Layout

```
┌──────────────────────────────────────────────┐
│ Target Input Section                         │
│ ┌────────────────────────────────────────┐   │
│ │ IP Address / CIDR                       │   │
│ │ ┌──────────────────────────────────┐   │   │
│ │ │ 192.168.200.10                     │   │   │
│ │ └──────────────────────────────────┘   │   │
│ └────────────────────────────────────────┘   │
│                                              │
│ Mode Selection                               │
│ ○ Sandbox (unrestricted)                     │
│ ● Production (rate-limited, audited)        │
│                                              │
│ Additional Options                           │
│ ☑ Enable Kill Switch                        │
│ Rate Limit: [50] probes/sec                 │
│ Timeout per probe: [5] seconds              │
│                                              │
│ [▶ Start Assessment] [Reset Form]           │
└──────────────────────────────────────────────┘

Live Terminal Output Area (expands after start)
──────────────────────────────────────────────
>[INIT] Initializing engine...                ◄─实时输出
>[SCAN] Probing target 192.168.200.10...     │
>[CVE DETECTED] Apache Struts2 S2-045...     │
>[Q-LEARNING] Iteration 23/500: reward=0.78...│
──────────────────────────────────────────────
```

#### Real-Time Terminal Output Parsing

Terminal window renders streaming logs from backend worker process:

```javascript
// Each log line parsed into syntax-highlighted blocks:
Line → { timestamp, level, component, message }
Level → colors: INFO=#10B981, WARN=#F59E0B, ERROR=#EF4444
Component → badges: [SANDBOX], [QLEARN], [CVE MATCHING]

Functionality:
  - Auto-scroll enabled (stays pinned to latest line)
  - Copy button exports full buffer to clipboard
  - Search input filters visible lines
```

### Filtering Risk Assessments

Navigate to **"Reports" → "Risk Assessments"** to browse historical scans:

#### Filter Criteria Panel

Located left sidebar, collapsible sections:

```markdown
### Severity
□ All levels  
☑ Critical (9.0–10.0)  
☐ High (7.0–8.9)  
☐ Medium (4.0–6.9)  
☐ Low (0.1–3.9)  

### Date Range
From: [2024-06-01]   To: [2024-06-15]  
Calendar picker supports dragging to expand range  

### CVE Database Version
Dropdown: Latest (2024-06-15) │ Previous (2024-05-01) │ Older...  
Filters based on when scan was executed relative to DB snapshot  

### Mitre ATT&CK Technique
Multi-select checkbox list:
☑ TA0001:Initial Access  
☑ T1190:Exploit Public-Facing Application  
☐ TA0002:Execution  
... more techniques below  

### Remediation Status
○ Pending review  
☑ Action in progress  
☐ Resolved  
☐ False positive  
```

#### Bulk Remediation Actions

After selecting findings via checkboxes on right-side table:

```typescript
enum BulkActionType {
  'ExportSelectedToCSV',
  'AssignToTicketSystem(Jira)',
  'ScheduleScansToVerifyFix',
  'MarkAsFalsePositive(With Justification)'
}
```

**Example: Assign to Jira Ticket**

```javascript
// Click "Create Jira Ticket" button
// Modal appears with pre-filled fields:
Title: "Security Patch Required: Multiple CVEs on 192.168.200.10"
Description: Auto-populated from findings[].description
Priority: Highest severity among selected (Critical → P0)
Assignee: Default recipient group ("Security Engineering")

// User reviews edits, clicks "Submit"
// Backend creates Jira issue, stores key in database
// Findings row updates with "Jira-TICKET-12345" link column
```

### Generating Compliance Reports

Visit **"Reports" → "Compliance"** tab:

#### Framework Selection Wizard

Step-by-step form:

```javascript
Step 1: Choose Regulatory Standard
Radio buttons:
- CIS Benchmark v2.0
- NIST Cybersecurity Framework 2.0
- ISO/IEC 27001:2022
- PCI-DSS v4.0
- HIPAA Security Rule

Step 2: Define Scope
Text input: "Enter asset tag or department name"
Optional multi-select: "Which hosts included? [+] All scanned assets [-] Exclude low-risk"

Step 3: Customize Header/Footer
Company logo upload (PNG/JPG, ≤2MB)
Watermark toggle: Off │ On (diagonal text overlay "Confidential")

Step 4: Review Content Preview
PDF preview rendered inline with pagination navigation
User can jump to specific chapter before generation

Step 5: Generate & Download
Click "Compile Report" → Progress bar shows:
  - Fetching findings subset → Done
  - Applying template styles → Done
  - Rendering charts → Done
  - Signing digital signature → Done

Result downloaded automatically: reports/compliance-nist-20240615.pdf
```

#### Gap Analysis Interpretation

Generated report includes executive summary with heat maps:

```table
Control ID | Control Description | Status | Evidence Link | Action Item
-----------|---------------------|--------|---------------|------------
SC-1       | Security Policy Auth| ✗ Fail | [View Findings]| Update policy document per NIST SP 800-53
SC-2       | Code Analysis       | ✓ Pass | [Code Repo]    | None
AU-3       | Audit Event Coverage| ? Partial | [Logs]      | Extend logging depth to include SQL queries

Color coding:
✗ Fail = Red background, immediate remediation required
✓ Pass = Green background, compliant
? Partial = Yellow background, minor improvement needed
```

### Processing Work Order Approvals

Only accessible to users assigned **Role: "Red Team Approver"** — typically project managers and security officers:

#### Approvals Dashboard Overview

Grid layout of pending requests:

```card
┌──────────────────────────────────────┐
│ Work Order #WO-7f3b9e2a1c4d          │
│ Submitted: June 15, 2024 09:23 AM   │
│ Target Org: Acme Corp                │
│ IPs: 203.0.113.0/24                  │
│ Duration: 8 hours                    │
│                                      │
│ Attached Docs: [Contract][NDA][Scope]|
│                                      │
│ Status Indicator:                    │
│ ● Pending PM Review                  │
│                                        │
│ [Approve] [Request More Info] [Reject]│
└──────────────────────────────────────┘
```

#### Decision Flow After Clicking Button

**Option 1: Approve**

```javascript
// Transition state: PENDING_PM_REVIEW → APPROVED_BY_PM
// Next approver notified via email: subject="[ACTION REQUIRED] Work Order WO-7f3b9e2a1c4d awaiting your review"
// Slack integration optional: Sends message to #security-approvals channel
```

**Option 2: Reject**

Modal pops up with textarea field:

```html
<textarea placeholder="Provide rationale for rejection..."></textarea>
Required options:
☐ Invalid scope (IPs don't match contract)
☐ Missing documentation (NDA expired/unsigned)
☐ Time conflict (scheduled during critical maintenance window)
☐ Insufficient justification (too vague description)

[Confirm Rejection]
```

Upon submit, status changes to REJECTED and operator receives automated explanation email with copy attached.

**Option 3: Request More Info**

Same modal as reject but sends task back to originator instead of closing loop.

Email notification format:

```email
Subject: [ACTION REQUIRED] Additional Info Needed for WO-7f3b9e2a1c4d

Hello Red Team Operator,

Your requested production scan has been flagged for additional clarification:

Reason: Missing scope validation certificate

Please upload corrected documents through the work order portal within 24 hours to avoid scheduling delays.

Portal URL: https://your-instance.cloudai-fusion.io/work-orders/WO-7f3b9e2a1c4d

Thank you,
CloudAI Fusion Approval Workflow Bot
```

---

## <a name="section5-api-reference"></a>Section 5: API Reference

All endpoints require `Authorization: Bearer <JWT_TOKEN>` header except public authentication routes. Base URL: `https://api.your-instance.cloudai-fusion.io/v1`

### Authentication Endpoints

#### POST /auth/login

Obtain short-lived access token (~2 hours validity) and refresh token (~7 days).

**Request Body:**

```json
{
  "username": "redteam-operator",
  "password": "***"
}
```

**Success Response (200 OK):**

```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "refresh_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "token_type": "Bearer",
  "expires_in": 7200
}
```

**Error Responses:**

| Status | Error Code | Description |
|--------|------------|-------------|
| 401 | INVALID_CREDENTIALS | Username/password mismatch |
| 403 | ACCOUNT_LOCKED | Too many failed attempts (≥5 in 15min) |
| 500 | INTERNAL_ERROR | JWT signing subsystem unavailable |

**cURL Example:**

```bash
curl -X POST https://api.your-instance.cloudai-fusion.io/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"redteam-operator","password":"secure_password_here"}'
```

### Vulnerability Scanner Endpoints

#### POST /scanners/sandbox

Initiate asynchronous scan job in sandbox mode.

**Request Body:**

```json
{
  "target": "192.168.200.10",
  "ports": [21, 22, 80, 443],
  "timeout_per_port_ms": 5000,
  "concurrency": 500,
  "custom_headers": {}
}
```

**Success Response (202 Accepted):**

```json
{
  "job_id": "7f3b9e2a-1c4d-4e8b-9f6a-2d5c8b1e3a4f",
  "status": "queued",
  "estimated_completion_seconds": 120,
  "poll_url": "/scanners/status/7f3b9e2a-1c4d-4e8b-9f6a-2d5c8b1e3a4f"
}
```

**Job Status Polling Endpoint: GET /scanners/status/{job_id}**

Returns progress updates until completion:

```json
// Initial state
{
  "job_id": "...",
  "status": "running",
  "progress_percent": 35,
  "current_phase": "port_sweep",
  "findings_so_far": 4
}

// Final state
{
  "job_id": "...",
  "status": "completed",
  "progress_percent": 100,
  "results_url": "/scanners/results/7f3b9e2a-1c4d-4e8b-9f6a-2d5c8b1e3a4f.json",
  "summary": {
    "total_findings": 12,
    "by_severity": {"critical": 3, "high": 5, "medium": 4}
  }
}
```

#### POST /scanners/production

Similar to sandbox but includes additional authorization parameters.

**Request Body:**

```json
{
  "work_order_id": "wo_7f3b9e2a1c4d",
  "target": "203.0.113.45",
  "rate_limit_probes_per_second": 50,
  "time_window_start": "2024-06-15T09:00:00-07:00",
  "time_window_end": "2024-06-15T17:00:00-07:00",
  "kill_switch_enabled": true,
  "emergency_contact_email": "ops@client-domain.io"
}
```

**Validation Errors (400 Bad Request):**

```json
{
  "error_code": "TIME_WINDOW_VIOLATION",
  "message": "Requested window overlaps with existing approved engagement WO-abc123",
  "suggested_alternatives": ["2024-06-16T09:00:00-07:00/2024-06-16T17:00:00-07:00"]
}
```

### Work Order Management Endpoints

#### POST /work-orders

Submit new request for production engagement.

**Request Body:**

```json
{
  "target_organization": "Acme Corporation",
  "ip_ranges": ["203.0.113.0/24", "203.0.113.100/30"],
  "start_time": "2024-06-15T09:00:00-07:00",
  "duration_hours": 4,
  "assessments_scope": ["port_scanning", "service_fingerprinting", "exploitation_simulation"],
  "do_not_test_do_s": false,
  "emergency_contact": {
    "email": "incident-response@acme-corp.io",
    "phone": "+1-555-123-4567"
  },
  "supporting_documents": [
    {
      "type": "client_contract",
      "filename": "contract-signed.pdf",
      "upload_url": "/uploads/documents/abc123.pdf"
    }
    // Repeat for NDA, scope validation, legal letter
  ]
}
```

**Success Response (201 Created):**

```json
{
  "work_order_id": "wo_7f3b9e2a1c4d",
  "status": "pending_pm_review",
  "created_at": "2024-06-14T18:23:17Z",
  "estimated_approval_date": "2024-06-17T18:23:17Z",
  "approval_workflow_stage": 1,
  "total_stages": 4
}
```

#### GET /work-orders/:id

Retrieve specific work order details including approval history.

**Response (200 OK):**

```json
{
  "work_order_id": "wo_7f3b9e2a1c4d",
  "status": "approved",
  "submitter": "john.doe@cloudai-fusion.io",
  "approvals": [
    {
      "stage": "pm_review",
      "approved_by": "sarah.smith@cloudai-fusion.io",
      "approved_at": "2024-06-15T10:15:33Z",
      "comments": "Scope matches contract requirements."
    },
    {
      "stage": "security_officer",
      "approved_by": "mike.jones@cloudai-fusion.io",
      "approved_at": "2024-06-15T14:42:11Z",
      "comments": "Rate limits adequate to prevent DoS risk."
    },
    {
      "stage": "legal_compliance",
      "approved_by": "jennifer.adams@cloudai-fusion.io",
      "approved_at": "2024-06-16T09:05:28Z",
      "comments": "All documentation verified legally binding."
    },
    {
      "stage": "ceo_executive_approval",
      "approved_by": "robert.chen@cloudai-fusion.io",
      "approved_at": "2024-06-16T16:30:45Z",
      "comments": "Proceed with caution per standard SLA terms."
    }
  ],
  "execution_log": [
    {
      "event": "scan_started",
      "timestamp": "2024-06-15T09:00:12Z",
      "details": {"ip": "203.0.113.45", "probes_initiated": 50}
    },
    {
      "event": "scan_completed",
      "timestamp": "2024-06-15T13:45:27Z",
      "details": {"total_findings": 23, "cleanup_status": "verified"}
    }
  ]
}
```

### Reporting Endpoints

#### GET /reports/compliance/nist/framework-report

Generate NIST CSF gap analysis.

**Query Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `scan_ids` | Array of UUIDs | Yes | Which assessments to include |
| `output_format` | Enum | No | pdf │ html │ markdown (default: pdf) |
| `include_evidence_links` | Boolean | No | Add deep-links to raw findings (default: true) |

**Response:**

Returns downloadable file stream with MIME type determined by `output_format`. Content-Disposition header: `attachment; filename="nist-csf-gap-analysis-YYYYMMDD.pdf"`

#### GET /reports/findings/:finding_id/remediation

Retrieve structured remediation guidance for specific CVE.

**Response (200 OK):**

```json
{
  "finding_id": "f1a2b3c4-d5e6-7f8g-9h0i-j1k2l3m4n5o6",
  "cve_id": "CVE-2011-2523",
  "affected_service": "vsftpd 2.3.4",
  "cvss_score": 10.0,
  "remediation_steps": [
    {
      "priority": 1,
      "action": "Upgrade vsftpd package",
      "command": "apt-get update && apt-get install --only-upgrade vsftpd",
      "expected_version": ">= 2.3.5",
      "rollback_plan": "Revert to previous package version if instability observed"
    },
    {
      "priority": 2,
      "action": "Restart service",
      "command": "systemctl restart vsftpd",
      "caution": "Plan downtime window accordingly"
    }
  ],
  "references": [
    "https://nvd.nist.gov/vuln/detail/CVE-2011-2523",
    "https:// Packet Storm Security: vsftpd 2.3.4 backdoor user list disclosure"
  ]
}
```

### Error Codes Reference

All API responses include standardized error object when status ≥ 400:

```json
{
  "error": {
    "code": "ERROR_CODE_CONSTANT",
    "message": "Human-readable explanation",
    "details": {},
    "trace_id": "0xabcdef123456"
  }
}
```

| Error Code | HTTP Status | Cause | Resolution |
|------------|-------------|-------|------------|
| `INVALID_CREDENTIALS` | 401 | Wrong username/password | Retry with correct credentials |
| `TOKEN_EXPIRED` | 401 | JWT past expiration time | Call `/auth/login` again |
| `INSUFFICIENT_ROLE` | 403 | User lacks RBAC permission | Request role elevation from admin |
| `TIME_WINDOW_VIOLATION` | 400 | Outside approved schedule | Reschedule request to valid window |
| `RATE_LIMIT_EXCEEDED` | 429 | Too many requests/min | Implement exponential backoff retry |
| `TARGET_NOT_AUTHORIZED` | 403 | IP outside scope | Verify target exists in IP ranges field |
| `WORK_ORDER_REVOKED` | 403 | Approval withdrawn mid-execution | Cease scan immediately |
| `KILL_SWITCH_TRIGGERED` | 499 | Emergency abort invoked | Wait for manual reactivation |

---

## <a name="section6-troubleshooting"></a>Section 6: Troubleshooting FAQ

Common issues encountered during deployment and operation, organized by symptom category.

### Issue: VM Not Responding After Vagrant Up

**Symptoms:**

```text
==> metasploitable-3: Timeout waiting for SSH!
==> metasploitable-3: Machine stopped unexpectedly
VirtualBox Error: NFS shared folders fail to mount
```

**Root Causes:**

1. Guest Additions ISO incompatibility with VirtualBox 7.x
2. Network adapter conflicts (dual-NIC VM causing routing loops)
3. Hypervisor exhaustion due to other VMs consuming resources

**Debugging Steps:**

```powershell
# Step 1: Check VirtualBox logs
cd "C:\ProgramData\Oracle\VirtualBox\VBOX.log
Get-Content VBOX.log -Tail 50

# Step 2: Restart Vagrant provider manually
vagrant reload metasploitable-3 --provider virtualbox

# Step 3: If still failing, rebuild Guest Additions
vagrant ssh metasploitable-3
# Inside VM:
sudo apt-get install build-essential dkms linux-headers-$(uname -r)
sudo /media/VBOXADDITIONS_*.sh  # Mount point varies
```

**Prevention Measures:**

- Use stable VirtualBox version 7.0.14 (known good release)
- Disable IPv6 on host network adapters before launch
- Allocate minimum 4GB RAM to each VM in Vagrantfile

### Issue: Rate Limit Exceeded During Production Scan

**Symptoms:**

```text
[WARN] Rate limiter triggered, dropping probe to 203.0.113.45:443
[ERROR] Too many requests from source IP, throttling to 10 probes/sec
```

**Causes:**

- Backend token bucket refill rate misconfigured (< actual burst capacity)
- Target system firewall rejecting rapid connection attempts
- Misunderstanding of approved scope (trying to scan more IPs than permitted)

**Mitigation Strategies:**

```powershell
# Reduce rate limit parameter explicitly
go run cmd/redteam/main.go `
    --target 203.0.113.45 `
    --rate-limit 20  # Lower than default 50

# Or enable adaptive throttling (auto-adjust based on packet loss)
go run cmd/redteam/main.go `
    --adaptive-rate-limit=true
```

**Long-Term Fix:**

Contact support to temporarily increase quota for specific IP range:

```email
Subject: Rate Limit Increase Request for WO-7f3b9e2a1c4d

Hi CloudAI Support,

Our current production engagement requires higher probe density for accurate 
application-layer vulnerability discovery. Can you please approve increasing 
tokens/sec for 203.0.113.45 from 50 → 75 for the next 4 hours?

Approval chain: PM → Security Officer → Platform Admin

Thank you,
Red Team Lead
```

### Issue: Authorization Expired Mid-Scan

**Symptoms:**

```text
[ERROR] JWT token expired at iteration 327/500
[KILL SWITCH] Emergency halt initiated due to auth invalidation
```

**Explanation:**

Access tokens expire after ~2 hours; scans exceeding this duration need refresh mechanism.

**Resolution:**

Automatically handled if refresh token present. Otherwise:

```powershell
# Stop current scan
Ctrl+C

# Obtain new token
$NEW_TOKEN = (curl -X POST https://.../auth/login -d @credentials.json).access_token

# Restart with updated credential
go run cmd/redteam/main.go `
    --mode production `
    --auth-token $NEW_TOKEN `
    --continue-from-iteration 327
```

**Best Practice:**

Always ensure `--refresh-token` flag provided alongside access token to enable seamless renewal.

### Debugging Tips

#### View Detailed Logs

Enable verbose logging mode:

```bash
export GOLOG_LEVEL=debug
go run cmd/redteam/main.go --log-level debug --target 192.168.200.10
```

Log entries categorized by facility:

| Tag | Meaning |
|-----|---------|
| `[QLEARN]` | Reinforcement learning engine |
| `[CVE MATCHING]` | Signature correlation module |
| `[RATE LIMIT]` | Token bucket controller |
| `[AUTH]` | JWT validation pipeline |
| `[EVIDENCE]` | Cryptographic hashing service |

#### Test with Sample Targets

Before running full-scale assessment, validate against known-good mock targets:

```powershell
# Deploy test harness locally
go run cmd/redteam/testdata/mocked-target/main.go --port 9999
# Starts fake server simulating vulnerable Apache Struts2

# Point scanner at mock endpoint
go run cmd/redteam/main.go --target localhost:9999 --skip-network-check
```

Successful output proves components functioning correctly:

```text
[MOCK TARGET] Received probe from 127.0.0.1
[CVE DETECTED] Apache Struts2 S2-045 → CVE-2017-5638 (matched rule id: STRUTS2-RULE-001)
[COMPLETE] Mock target responded correctly
```

#### Performance Tuning Guides

For large-scale engagements involving 1,000+ hosts:

**Memory Allocation:**

Increase Go garbage collector threshold (default 400KB free):

```bash
export GODEBUG=gctrigfreq=200
go run cmd/redteam/main.go --target-scopes big-network.txt
```

**Concurrency Settings:**

Tune parallel threads based on available CPU cores:

```bash
# Auto-detect core count
NUM_CORES=$(nproc)
THREADS=$((NUM_CORES * 2))  # Conservative multiplier

go run cmd/redteam/main.go --threads $THREADS --target-list targets.txt
```

**Network Optimization:**

Bind scanner interface to high-speed NIC (avoid Wi-Fi):

```bash
# Determine primary LAN interface
ip route | grep default
# Output example: default via 192.168.1.1 dev enp3s0

# Specify explicit source IP
go run cmd/redteam/main.go --source-ip 192.168.1.100 --target remote-host.io
```

**Disk I/O:**

Redirect temporary storage to fast NVMe drive:

```bash
mkdir /mnt/nvme/tmp
export TMPDIR=/mnt/nvme/tmp
go run cmd/redteam/main.go --temp-dir $TMPDIR
```

These optimizations reduce scan duration by ~40% on typical enterprise networks.

---

<div align="center">

**Document Version:** 1.0.0  
**Last Updated:** June 15, 2024  
**Maintained By:** CloudAI Fusion Documentation Team  
**License:** Apache 2.0 | OBCE3 Certified

For corrections or improvements, submit pull request to docs/ folder.
</div>
