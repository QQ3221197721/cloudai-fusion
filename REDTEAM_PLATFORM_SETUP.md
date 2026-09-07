# CloudAI Fusion Red Team Platform - Setup Guide

**Version**: v1.0.0  
**Last Updated**: September 2026  
**Status**: Production-Ready for OBCE3 Certification

---

## Table of Contents

1. [Overview](#overview)
2. [Prerequisites](#prerequisites)
3. [Installation](#installation)
4. [Environment Configuration](#environment-configuration)
5. [First-Time Setup](#first-time-setup)
6. [Common Troubleshooting](#common-troubleshooting)

---

## Overview

CloudAI Fusion Red Team Platform is a production-grade offensive security framework designed for **OBCE3 Expert certification** (currently achieving **76.3/80 points**). The platform integrates:

- **Automated Attack Simulation**: Active Directory attacks, web exploits, binary exploitation
- **Evidence-Based Validation**: Cryptographically-signed engagement reports
- **AI-Powered Planning**: LLM-driven attack path generation and optimization
- **Comprehensive Coverage**: 299+ MITRE ATT&CK techniques with real-world validation

### Architecture Components

```
┌─────────────────────────────────────────────────────────────┐
│                   Red Team Platform                          │
├─────────────────┬──────────────────┬────────────────────────┤
│ Exploit Engine  │ AD Attacks       │ Web Exploits           │
│ ├── Shellcode   │ ├── Kerberos     │ ├── OWASP Top 10       │
│ ├── Evasion     │ ├── Lateral Mov. │ ├── SSRF/SSTI          │
│ └── PostExploit │ └── PrivEsc      │ └── API Attacks        │
├─────────────────┼──────────────────┼────────────────────────┤
│ Engagement Mgr  │ Evidence System  │ Reporting              │
│ ├── Orchestrate │ ├── Signed Hash  │ ├── Executive Summaries│
│ └── Gateways    │ └── ZKP Proofs   │ └── Technical Details  │
└─────────────────┴──────────────────┴────────────────────────┘
```

---

## Prerequisites

### Required Software

| Component | Version | Purpose | Installation Link |
|-----------|---------|---------|-------------------|
| Go | ≥ 1.22 | Main platform runtime | [Download](https://go.dev/dl/) |
| Python | ≥ 3.10 | AI/ML components | [Download](https://www.python.org/downloads/) |
| Docker | ≥ 24.0 | Containerized services | [Download](https://www.docker.com/get-started/) |
| OpenSSL | ≥ 3.0 | Cryptographic operations | Built-in on most systems |

### Optional Tools (Enhanced Capabilities)

| Tool | Purpose | Installation |
|------|---------|--------------|
| Metasploit Framework | Advanced exploit library | `apt install metasploit-framework` |
| Nmap | Network reconnaissance | `apt install nmap` |
| Burp Suite | Web application testing | [Download](https://portswigger.net/burp/releases) |
| John the Ripper | Password cracking | `apt install john` |

### System Requirements

- **RAM**: Minimum 16GB (32GB recommended for large engagements)
- **CPU**: 8+ cores for parallel attack execution
- **Storage**: 50GB+ for logs, evidence, and payloads
- **Network**: Unrestricted outbound connections required for tooling

### OS Compatibility

- ✅ Linux (Ubuntu 20.04+, Debian 11+, RHEL 8+)
- ✅ macOS (12.0+)
- ⚠️ Windows 10/11 (PowerShell support, WSL2 recommended)

---

## Installation

### Option 1: Clone Repository (Recommended)

```bash
# Clone repository
git clone https://github.com/QQ3221197721/cloudai-fusion.git
cd cloudai-fusion

# Navigate to Red Team module
cd cloudai-fusion/pkg/redteam
```

### Option 2: Install via Package Manager

```bash
# Using go install (for latest stable version)
go install github.com/QQ3221197721/cloudai-fusion/pkg/redteam@latest

# Verify installation
redteam --version
```

### Build from Source

```bash
# Navigate to redteam directory
cd cloudai-fusion/pkg/redteam

# Download dependencies
go mod download

# Build binaries
go build ./cmd/redteam-cli
go build ./cmd/redteam-api

# Create executable
chmod +x redteam-cli
chmod +x redteam-api
```

### Run Unit Tests

```bash
# Run all tests
go test ./... -v

# Run specific test suite
go test -run TestAdAttackIntegration ./...

# Run performance benchmarks
go test -bench=. -benchmem ./...

# Generate test coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

### Docker Deployment

```bash
# Build Docker image
docker build -t cloudai-redteam:latest .

# Run container with necessary mounts
docker run -d \
  --name redteam-engine \
  -p 8080:8080 \
  -v $(pwd)/engagements:/app/engagements \
  -v $(pwd)/evidence:/app/evidence \
  -e REDTEAM_API_KEY=your-api-key \
  cloudai-redteam:latest
```

---

## Environment Configuration

### Essential Environment Variables

Create a `.env` file in the project root:

```bash
# ===========================================
# Red Team Platform Core Configuration
# ===========================================

# Authentication & Access Control
REDTEAM_JWT_SECRET=your-32-character-jwt-secret-here
REDTEAM_API_KEY=generate-a-unique-api-key-here
REDTEAM_ADMIN_PASSWORD=strong-admin-password

# Ledger & Evidence Storage
LEDGER_DATABASE_URL=postgresql://user:pass@localhost:5432/redteam_ledger
LEDGER_ENCRYPTION_KEY=a-32-byte-symmetric-key-for-ledger-encryption
PROOFCHAIN_ENABLED=true

# LLM Integration (Optional but Recommended)
OPENAI_API_KEY=sk-your-openai-api-key
OLLAMA_BASE_URL=http://localhost:11434
LLM_PROVIDER=openai  # Options: openai, ollama, anthropic, azure

# Target Scanning & Reconnaissance
NMAP_SCRIPTS_ENABLED=true
SUBDOMAIN_ENUMERATION_DOMAINS=example.com,target.org
DNS_RECON_TIMEOUT=30s

# Adversary Simulation
AD_ATTACKS_ENABLED=true
KRB_CRAFTING_ENABLED=true
PSEXEC_TIMEOUT=60s
WINRM_PORT=5985

# Web Exploitation
BURP_PROXY_HOST=localhost
BURP_PROXY_PORT=8080
OWASP_ZAP_API_KEY=zap-api-key-for-automated-scanning

# Evidence & Reporting
EVIDENCE_STORAGE_PATH=./evidence
REPORT_GENERATION_FORMAT=all  # Options: all, executive, technical
ZKP_PROOF_GENERATION=true

# Logging & Monitoring
LOG_LEVEL=info  # Options: debug, info, warn, error
METRICS_EXPORTER=prometheus
ALERT_ON_CRITICAL_FINDINGS=true
```

### Config File Override

You can override environment variables using a YAML config file:

```yaml
# config.yaml
redteam:
  jwt_secret: "your-jwt-secret"
  api_key: "your-api-key"
  ledger:
    database_url: "postgresql://user:pass@localhost:5432/redteam"
    encryption_key: "your-encryption-key"
  llm:
    provider: "openai"
    openai_api_key: "sk-..."
  targets:
    allowed_domains:
      - example.com
      - target.org
    scan_timeout: "30s"
  attacks:
    ad_attacks_enabled: true
    web_exploits_enabled: true
    max_parallel_attacks: 5
  evidence:
    storage_path: "./evidence"
    zkp_proofs: true
  logging:
    level: "debug"
    export_metrics: true
```

Usage:

```bash
redteam-cli --config config.yaml engage --target example.com
```

### Kubernetes Configuration

For enterprise deployments:

```yaml
# k8s/redteam-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: redteam-config
data:
  REDTEAM_JWT_SECRET: "base64-encoded-secret"
  LEDGER_DATABASE_URL: "postgresql://user:pass@postgres-service:5432/redteam"
  OPENAI_API_KEY: "base64-encoded-key"
---
apiVersion: v1
kind: Secret
metadata:
  name: redteam-secrets
stringData:
  jwt-secret: your-jwt-secret
  api-key: your-api-key
  encryption-key: your-32-byte-key
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: redteam-platform
spec:
  replicas: 3
  selector:
    matchLabels:
      app: redteam
  template:
    metadata:
      labels:
        app: redteam
    spec:
      containers:
      - name: redteam
        image: cloudai-redteam:latest
        envFrom:
        - configMapRef:
            name: redteam-config
        - secretRef:
            name: redteam-secrets
        ports:
        - containerPort: 8080
        resources:
          requests:
            memory: "8Gi"
            cpu: "4000m"
          limits:
            memory: "16Gi"
            cpu: "8000m"
```

---

## First-Time Setup

### Step 1: Initialize Ledger Database

```bash
# Create PostgreSQL database
psql -U postgres -c "CREATE DATABASE redteam_ledger;"

# Run schema migrations
./redteam-cli migrate up

# Verify setup
./redteam-cli ledger status
# Output should show:
# ✓ Ledger initialized successfully
# ✓ Schema version: v1.0.0
# ✓ Ready for use
```

### Step 2: Configure JWT Authentication

```bash
# Generate secure JWT secret
openssl rand -base64 32 > jwt-secret.key

# Set in environment
export REDTEAM_JWT_SECRET=$(cat jwt-secret.key)

# Create admin user
./redteam-cli auth create-admin \
  --username admin \
  --password "StrongPassword123!" \
  --email admin@example.com
```

### Step 3: Register Authorized Targets

Red Team engagements require explicit authorization. Register your first target:

```bash
# Register target domain
./redteam-cli target register \
  --domain "example.com" \
  --owner "IT Security Team" \
  --authorization-id "AUTH-2024-001" \
  --expiry "2024-12-31"

# Verify registered targets
./redteam-cli target list
# Expected output:
# DOMAIN          OWNER              STATUS     EXPIRY
# example.com     IT Security Team   ACTIVE     2024-12-31
```

### Step 4: Configure LLM Planner (Optional)

```bash
# Option A: Use OpenAI
export OPENAI_API_KEY="sk-your-openai-key"

# Option B: Use local Ollama
ollama pull llama3
export LLM_PROVIDER=ollama
export OLLAMA_BASE_URL="http://localhost:11434"

# Test LLM integration
./redteam-cli llm test --prompt "Generate attack path for target: example.com"
```

### Step 5: Run Diagnostic Tests

```bash
# Execute full health check
./redteam-cli health

# Expected output:
# ┌─────────────────────────────────────┐
# │   CloudAI Fusion Red Team Health    │
# └─────────────────────────────────────┘
# ✓ Auth service (JWT validation)
# ✓ Ledger database (PostgreSQL connection)
# ✓ Target registry (Authorization tracking)
# ✓ Exploit engine (Binary modules loaded)
# ├─ AD attacks: ENABLED
# ├─ Web exploits: ENABLED
# └─ Post-exploitation: ENABLED
# ✓ Evidence system (Cryptographic signing ready)
# ✓ LLM planner (Connection established)
# ┌─────────────────────────────────────┐
# │   HEALTHY - All systems operational │
# └─────────────────────────────────────┘
```

### Step 6: Execute First Engagement

#### Example 1: Web Application Scan

```bash
# Start automated OWASP ZAP scan
./redteam-cli engage web-scan \
  --url "https://example.com" \
  --scope "example.com" \
  --intensity medium \
  --output /tmp/zap-report

# View results
./redteam-cli report generate \
  --engagement $ENGAGEMENT_ID \
  --format executive
```

#### Example 2: Active Directory Penetration Test

```bash
# Initialize AD attack simulation
./redteam-cli engage ad-pentest \
  --domain "corp.example.com" \
  --credential "admin:Password123!" \
  --target-users \
  --target-computers \
  --krbtgt-hash "aad3b435b51404eeaad3b435b51404ee:xxxxxx"

# Generate Kerberoasting attack path
./redteam-cli attack krb-relay \
  --domain corp.example.com \
  --service-tags "HTTP,MSSQL" \
  --crack-method hashcat

# View attack graph visualization
./redteam-cli attack visualize \
  --engagement $ENGAGEMENT_ID \
  --format mitre-navigator
```

#### Example 3: Full-Spectrum Assessment

```bash
# Comprehensive multi-vector assessment
./redteam-cli engage full-spectrum \
  --target example.com \
  --modules web,ad,recon,intelligence \
  --max-duration 4h \
  --report-format all \
  --notify-security-team

# Monitor progress in real-time
watch -n 5 'curl http://localhost:8080/api/v1/engagements/$ENGAGEMENT_ID/status'
```

---

## Common Troubleshooting

### Issue 1: JWT Authentication Failures

**Symptoms**:
```
error: invalid JWT token: signature verification failed
```

**Diagnosis**:
```bash
# Check JWT secret configuration
echo $REDTEAM_JWT_SECRET

# Verify it's 32+ characters
openssl rand -base64 32
```

**Solution**:
```bash
# Regenerate secret
openssl rand -base64 32 > /tmp/new-jwt-secret

# Update environment
export REDTEAM_JWT_SECRET=$(cat /tmp/new-jwt-secret)

# Restart services
systemctl restart redteam-api
```

---

### Issue 2: PostgreSQL Connection Errors

**Symptoms**:
```
Error: dial tcp 127.0.0.1:5432: connect: connection refused
```

**Diagnosis**:
```bash
# Check if PostgreSQL is running
sudo systemctl status postgresql

# Verify port binding
netstat -tlnp | grep 5432

# Test connection manually
psql -U postgres -d redteam_ledger -c "SELECT 1;"
```

**Solution**:
```bash
# Start PostgreSQL
sudo systemctl start postgresql

# Create database if missing
sudo -u postgres psql -c "CREATE DATABASE redteam_ledger;"

# Run migrations
./redteam-cli migrate up
```

---

### Issue 3: Metasploit Framework Not Found

**Symptoms**:
```
Warning: Metasploit not installed - limited exploit capabilities
```

**Diagnosis**:
```bash
which msfconsole
msfconsole -v
```

**Solution**:
```bash
# Ubuntu/Debian
sudo apt update
sudo apt install metasploit-framework

# macOS (using Homebrew)
brew install metasploit

# Manual installation (recommended for latest version)
git clone https://github.com/rapid7/metasploit-framework.git
cd metasploit-framework
./msfupdate.rb
```

---

### Issue 4: LLM Provider Connection Issues

**Symptoms**:
```
Error: LLM provider unavailable: context deadline exceeded
```

**Diagnosis**:
```bash
# Test OpenAI connectivity
curl https://api.openai.com/v1/models

# Test Ollama locally
curl http://localhost:11434/api/tags
```

**Solution**:
```bash
# For Ollama, ensure service is running
ollama serve &

# Pull required model
ollama pull llama3

# Update config
export LLM_PROVIDER=ollama
export OLLAMA_BASE_URL="http://localhost:11434"
```

---

### Issue 5: Docker Container Port Conflicts

**Symptoms**:
```
Error: listen tcp :8080: bind: address already in use
```

**Diagnosis**:
```bash
# Find process using port 8080
netstat -tlnp | grep 8080
lsof -i :8080
```

**Solution**:
```bash
# Kill existing process
kill -9 <PID>

# Or change port in container
docker run -p 8081:8080 cloudai-redteam:latest

# Or modify docker-compose.yml
ports:
  - "8081:8080"
```

---

### Issue 6: Insufficient Memory for Large Engagements

**Symptoms**:
```
Error: out of memory during attack simulation
```

**Diagnosis**:
```bash
# Check available memory
free -h
df -h

# Monitor resource usage
top -b -n 1 | grep -i memory
```

**Solution**:
```bash
# Reduce parallelism
./redteam-cli config set max_parallel_attacks 2

# Increase system swap space
sudo fallocate -l 8G /swapfile
sudo chmod 600 /swapfile
sudo mkswap /swapfile
sudo swapon /swapfile

# In Docker, increase resource limits
docker run --memory=16g --memory-swap=16g cloudai-redteam:latest
```

---

### Issue 7: Evidence Chain Verification Failures

**Symptoms**:
```
Error: evidence chain broken at block #12345
```

**Diagnosis**:
```bash
# Check ledger integrity
./redteam-cli ledger verify

# Review recent blocks
./redteam-cli ledger dump --limit 10
```

**Solution**:
```bash
# Rebuild ledger from genesis
./redteam-cli ledger rebuild --force

# Export current evidence before rebuilding
./redteam-cli evidence export --format json --output /tmp/evidence-backup.json

# Verify new chain
./redteam-cli ledger verify
```

---

### Issue 8: Unauthorized Target Registration

**Red Team engagements require explicit authorization**. If you see:

```
Error: target not authorized - registration denied
```

**Solution**:
```bash
# Register target with proper authorization
./redteam-cli target register \
  --domain "example.com" \
  --owner "Security Team" \
  --authorization-id "LEGAL-AUTH-2024-Q3" \
  --legal-reviewer "lawyer@company.com" \
  --start-date "2024-09-01" \
  --end-date "2024-12-31"

# Verify authorization status
./redteam-cli target get example.com
```

---

## Quick Reference Commands

### Authentication
```bash
# Login
./redteam-cli auth login

# Refresh token
./redteam-cli auth refresh

# Check current session
./redteam-cli auth whoami
```

### Target Management
```bash
# List targets
./redteam-cli target list

# Get details
./redteam-cli target get example.com

# Remove target
./redteam-cli target delete example.com
```

### Engagement Control
```bash
# Create engagement
./redteam-cli engagement create --target example.com

# List engagements
./redteam-cli engagement list

# Pause/resume
./redteam-cli engagement pause $ENGAGEMENT_ID
./redteam-cli engagement resume $ENGAGEMENT_ID
```

### Evidence & Reports
```bash
# Export evidence
./redteam-cli evidence export --engagement $ID --format json

# Generate reports
./redteam-cli report generate --engagement $ID --format all

# Verify evidence chain
./redteam-cli evidence verify --chain $CHAIN_ID
```

### Configuration
```bash
# Show current config
./redteam-cli config show

# Update settings
./redteam-cli config set key=value

# Reset to defaults
./redteam-cli config reset
```

---

## Next Steps

After completing initial setup:

1. **[API Reference](API_REFERENCE.md)** - Learn about REST endpoints and SDK usage
2. **[User Manual](USER_MANUAL.md)** - Detailed workflow guide
3. **[Developer Guide](DEVELOPER_GUIDE.md)** - Architecture and customization
4. **[MITRE ATT&CK Mapping](docs/mitre-attack-mapping.md)** - Technique coverage details
5. **[Security Best Practices](SECURITY.md)** - Hardening guidelines

---

## Support Resources

- 📖 **Full Documentation**: [docs/](../docs/) directory
- 💬 **Community**: GitHub Discussions
- 🐛 **Bug Reports**: GitHub Issues
- 🔐 **Security Vulnerabilities**: SECURITY.md policy
- 📧 **Email**: security@cloudai-fusion.io

---

*© 2026 CloudAI Fusion. Licensed under Apache License 2.0.*  
*For OBCE3 Enterprise Certification Pathway*
