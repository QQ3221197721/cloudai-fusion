# CloudAI Fusion Red Team Platform - Documentation Index

**Version**: v1.0.0  
**Last Updated**: September 2026  

---

## Welcome

This index provides navigation to the complete documentation suite for the **CloudAI Fusion Red Team Platform**, an enterprise-grade offensive security framework designed for **OBCE3 Expert certification**.

---

## Complete Documentation Suite

### 1. Setup Guide
**File**: [REDTEAM_PLATFORM_SETUP.md](REDTEAM_PLATFORM_SETUP.md)

Comprehensive installation and configuration guide covering:
- Prerequisites and system requirements
- Repository cloning and building from source
- Environment variable configuration
- Docker deployment strategies
- First-time setup walkthrough
- Common troubleshooting scenarios

**When to use**: Before running any scans or creating campaigns

---

### 2. API Reference
**File**: [REDTEAM_API_REFERENCE.md](REDTEAM_API_REFERENCE.md)

Complete REST API specification with:
- Authentication (JWT) endpoints
- Dashboard statistics queries
- Work order management
- Campaign lifecycle operations
- Finding CRUD operations
- Error handling patterns
- SDK examples (Python, JavaScript, Bash)

**When to use**: For programmatic integration or building custom clients

---

### 3. User Manual
**File**: [REDTEAM_USER_MANUAL.md](REDTEAM_USER_MANUAL.md)

End-user guide covering day-to-day operations:
- Getting started workflow
- Target registration process
- Creating and managing attack campaigns
- Running automated scans
- Viewing real-time results
- Generating compliance reports
- Exporting cryptographic evidence
- Best practices and security hygiene

**When to use**: For learning how to operate the platform

---

### 4. Developer Guide
**File**: [REDTEAM_DEVELOPER_GUIDE.md](REDTEAM_DEVELOPER_GUIDE.md)

Technical deep-dive for contributors:
- Architecture overview and data flow
- Code structure explanation
- Core component implementation details
- How to add new attack vectors
- Testing guidelines and patterns
- Performance optimization tips
- Deployment strategies

**When to use**: For extending platform capabilities or debugging internals

---

## Additional Resources

### Architecture Documents

| Document | Description | Location |
|----------|-------------|----------|
| Verifiable Moat Specification | Cryptographic evidence chain design | `docs/verifiable-moat-spec.md` |
| MITRE ATT&CK Mapping | Technique coverage matrix | `docs/mitre-attack-mapping.md` |
| OBCE3 Alignment Report | Certification gap analysis | `docs/obce3-alignment.md` |
| Evidence Ledger Design | Hash-chain implementation details | `pkg/evidence/README.md` |

### Configuration Reference

| Config File | Purpose | Example Location |
|-------------|---------|------------------|
| `config/redteam.yaml` | Platform-wide settings | Project root |
| `config/scanners.yaml` | Tool configurations | `config/scanners/` |
| `k8s/redteam-config.yaml` | Kubernetes deployment | Helm charts |

### Testing Resources

| Resource | Description |
|----------|-------------|
| Unit Test Templates | Mock-based testing patterns | `test/unit/templates.go` |
| Integration Test Suite | Component integration tests | `test/integration/` |
| E2E Test Bed | Full system test environment | `test/e2e/fixtures/` |

---

## Quick Navigation by Role

### For Security Managers
1. Start with **User Manual** → "Introduction" chapter
2. Review **Setup Guide** → "Prerequisites" for team onboarding
3. Refer to **API Reference** → "Dashboard Statistics" for executive reporting

### For Red Team Operators
1. Begin with **User Manual** → "Creating Attack Campaigns"
2. Consult **Setup Guide** → "Environment Configuration" for tool setup
3. Use **API Reference** → "Running Scans" for programmatic control

### For Developers
1. Read **Developer Guide** → "Architecture Overview" first
2. Study core components in **Developer Guide** → "Core Components"
3. Follow **Developer Guide** → "Adding New Features" for extensions

### For Compliance Officers
1. Review **User Manual** → "Generating Reports"
2. Understand evidence export in **User Manual** → "Exporting Evidence"
3. Verify blockchain anchoring in **Setup Guide** → "Blockchain Anchoring"

---

## Key Concepts Glossary

| Term | Definition | Related Docs |
|------|------------|--------------|
| **Engagement** | Authorized red team operation with defined scope | User Manual §4 |
| **Campaign** | Multi-stage attack orchestration with phases | User Manual §5 |
| **Evidence Chain** | Cryptographic hash chain of all actions | Developer Guide §3.3 |
| **Scope Gate** | Authorization boundary preventing out-of-scope actions | Developer Guide §3.2 |
| **MITRE ATT&CK** | Framework for categorizing adversary tactics/techniques | API Reference §FINDINGS |
| **OBCE3** | Offensive Security Certified Expert certification standard | Setup Guide §PREREQUISITES |

---

## File Structure Map

```
cloudai-fusion/
├── REDTEAM_PLATFORM_SETUP.md        ← Installation & configuration
├── REDTEAM_API_REFERENCE.md         ← REST API specification
├── REDTEAM_USER_MANUAL.md           ← End-user operations guide
├── REDTEAM_DEVELOPER_GUIDE.md       ← Technical development reference
├── REDTEAM_DOCUMENTATION_INDEX.md   ← This document
│
├── cmd/apiserver/redteam/           ← HTTP API layer
│   ├── routes.go                    ← Endpoint definitions
│   └── handlers.go                  ← Request processing
│
├── pkg/redteam/                     ← Core business logic
│   ├── engagement.go                ← Lifecycle state machine
│   ├── evidence.go                  ← Cryptographic receipts
│   ├── exploit_engine.go            ← Payload generation
│   └── models/                      ← Data structures
│
├── docs/                            ← Architecture documents
│   ├── verifiable-moat-spec.md
│   └── mitre-attack-mapping.md
│
└── test/                            ← Test suites
    ├── unit/                        ← Component tests
    ├── integration/                 ← System tests
    └── e2e/                         ← End-to-end validation
```

---

## Getting Help

### Documentation Issues
- Report broken links: GitHub Issue with label `documentation`
- Suggest improvements: Pull Request to relevant `.md` file
- Request clarification: Community Forum thread

### Technical Support
- **General Usage**: Check "Common Troubleshooting" sections in Setup/User guides
- **API Questions**: Consult API Reference examples
- **Development**: Read Developer Guide testing section first
- **Critical Issues**: Contact security@cloudai-fusion.io

---

## Version Compatibility

| Documentation Version | Platform Version | Compatible Until |
|-----------------------|------------------|------------------|
| v1.0.0 (current) | v1.0.0 | Ongoing |
| v0.9.x (legacy) | v0.9.x | Deprecated after Dec 2026 |

All documentation is version-controlled. Historical versions available via Git tags.

---

## Contributing to Documentation

We welcome documentation improvements! Follow these guidelines:

### Style Guidelines
- Use professional, clear language
- Include code examples for all API endpoints
- Provide screenshots for UI workflows
- Keep examples current with latest features

### Contribution Process
1. Fork repository
2. Create feature branch (`feature/improve-setup-guide`)
3. Make changes following markdown style
4. Test all links and code examples
5. Submit PR with detailed description

### Review Checklist
- ☑️ Accuracy against current codebase
- ☑️ Completeness (no missing steps)
- ☑️ Clarity (readable by target audience)
- ☑️ Consistency (formatting, terminology)
- ☑️ Examples tested and working

---

## Maintenance Schedule

Documentation review cycle:
- **Monthly**: Link health check, broken link detection
- **Quarterly**: Feature parity update with releases
- **Annually**: Major restructure based on user feedback

Last comprehensive review: September 2026

---

## License

All documentation is licensed under:
**Creative Commons Attribution 4.0 International (CC BY 4.0)**

For commercial licensing inquiries: contact sales@cloudai-fusion.io

---

*Thank you for using CloudAI Fusion Red Team Platform!*  
*For questions about this documentation, email docs@cloudai-fusion.io*
