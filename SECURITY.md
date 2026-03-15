# Security Policy

## Supported Versions

| Version | Supported          |
|---------|--------------------|
| 0.1.x   | :white_check_mark: |
| < 0.1   | :x:                |

We provide security patches for the latest minor release. Users are encouraged to
upgrade to the latest version to receive all security fixes.

## Reporting a Vulnerability

We take security seriously at CloudAI Fusion. If you discover a security
vulnerability, please report it responsibly.

### How to Report

**DO NOT** open a public GitHub issue for security vulnerabilities.

Instead, please use one of the following methods:

1. **GitHub Security Advisory** (Preferred):
   - Go to [Security Advisories](https://github.com/cloudai-fusion/cloudai-fusion/security/advisories)
   - Click "Report a vulnerability"
   - Fill in the details

2. **Email**:
   - Send an email to **security@cloudai-fusion.io**
   - Use our [PGP key](https://cloudai-fusion.io/.well-known/pgp-key.asc) to encrypt sensitive information
   - Subject line: `[SECURITY] Brief description`

### What to Include

Please include as much of the following information as possible:

- **Type of vulnerability** (e.g., RCE, SQL injection, XSS, privilege escalation)
- **Affected component** (e.g., API Server, Scheduler, AI Engine, Helm Chart)
- **Affected versions**
- **Steps to reproduce** the vulnerability
- **Proof of concept** (if available)
- **Impact assessment** — what an attacker could achieve
- **Suggested fix** (if you have one)

### Response Timeline

| Action                     | Timeline          |
|----------------------------|-------------------|
| Acknowledgment of report   | Within 48 hours   |
| Initial triage & severity  | Within 5 days     |
| Fix development            | Within 30 days    |
| Public disclosure           | Within 90 days    |

We follow a 90-day disclosure policy. If a fix is available sooner, we will
coordinate public disclosure with the reporter.

### Severity Classification

We use [CVSS v3.1](https://www.first.org/cvss/v3.1/specification-document) for
severity scoring:

| Severity | CVSS Score | Response Time |
|----------|------------|---------------|
| Critical | 9.0 – 10.0 | 72 hours     |
| High     | 7.0 – 8.9  | 7 days       |
| Medium   | 4.0 – 6.9  | 30 days      |
| Low      | 0.1 – 3.9  | 90 days      |

## Security Measures

### Authentication & Authorization

- JWT-based authentication with configurable token expiration
- **Production JWT secret enforcement**: minimum 32 characters, high entropy required; API server **refuses to start** in production with weak/default secrets
- Role-Based Access Control (RBAC) with four roles: `admin`, `operator`, `viewer`, `auditor`
- OIDC cross-cloud identity federation with:
  - JWKS caching with configurable TTL and automatic rotation
  - Token Refresh flow (automatic re-authentication)
  - JIT (Just-In-Time) user provisioning from OIDC claims
  - Token Revocation / Logout endpoint
- bcrypt password hashing with configurable cost factor

### Network Security

- TLS 1.2+ required for all external communications
- Kubernetes NetworkPolicy for zero-trust microsegmentation
- Ingress with TLS termination
- Internal gRPC communication with mTLS support

### Debug Endpoint Security

- Debug endpoints (`/debug/*`) are **disabled by default** (`CLOUDAI_DEBUG_ENABLED=false`)
- When enabled, protected by 6 defense layers:
  1. **Environment gate**: `CLOUDAI_DEBUG_ENABLED=true` required
  2. **JWT Authentication**: same `AuthMiddleware()` as production API
  3. **Admin-only RBAC**: `role=admin` required in JWT claims
  4. **IP Allowlist**: optional `CLOUDAI_DEBUG_ALLOWED_IPS` CIDR/IP restriction
  5. **Audit Logging**: all debug access logged with user identity and IP
  6. **Rate Limiting**: pprof/trace endpoints rate-limited (~2 req/min) to prevent DoS
- Exposes: runtime info, pprof profiles, dynamic log level, cross-service health
- Does **not** expose: database credentials, JWT secrets, or user data

### Container Security

- Non-root container execution (`runAsNonRoot: true`)
- Read-only root filesystem (`readOnlyRootFilesystem: true`)
- All capabilities dropped (`drop: ["ALL"]`)
- Seccomp profile: `RuntimeDefault`
- No privilege escalation (`allowPrivilegeEscalation: false`)

### Supply Chain Security

- Signed container images (Cosign)
- SBOM generation for each release
- Dependency vulnerability scanning (Trivy, Grype)
- GitHub Dependabot enabled for automated dependency updates

### Secrets Management

- Kubernetes Secrets with encryption at rest
- Environment-based configuration (no hardcoded secrets)
- **JWT secret validation on startup**: rejects insecure defaults (`changeme`, `secret`, `.env.example` placeholders) and enforces Shannon entropy threshold in production
- `.env` files excluded from version control via `.gitignore`
- `make setup` / `scripts/env-generate.sh` generates cryptographically random secrets automatically
- Helm chart supports external secret stores (Vault, AWS Secrets Manager)

### Audit & Compliance

- CIS Kubernetes Benchmark compliance checking
- Structured audit logging with trace correlation
- Real-time threat detection engine
- Security event alerting via monitoring stack

## Security-Related Configuration

### Recommended Production Settings

```yaml
# values.yaml (Helm)
networkPolicy:
  enabled: true

podDisruptionBudget:
  enabled: true

apiserver:
  autoscaling:
    enabled: true
    minReplicas: 2
```

### Environment Variables

```bash
# Strong JWT secret (minimum 32 characters)
JWT_SECRET=<cryptographically-random-secret>

# Database with TLS
DATABASE_URL=postgresql://user:pass@host:5432/db?sslmode=require

# Redis with authentication
REDIS_URL=redis://:password@host:6379/0
```

## Vulnerability Disclosure Hall of Fame

We recognize security researchers who help keep CloudAI Fusion secure.
Contributors will be acknowledged here (with permission).

| Researcher | Vulnerability | Date | Severity |
|------------|---------------|------|----------|
| *Be the first!* | — | — | — |

## Contact

- **Security Team Email**: security@cloudai-fusion.io
- **GitHub Security Advisories**: [Report](https://github.com/cloudai-fusion/cloudai-fusion/security/advisories)
- **PGP Key Fingerprint**: `(To be published)`

## References

- [CNCF Security Best Practices](https://www.cncf.io/blog/2022/02/17/10-best-practices-for-securing-your-cloud-native-environment/)
- [Kubernetes Security Checklist](https://kubernetes.io/docs/concepts/security/security-checklist/)
- [OWASP Top 10](https://owasp.org/www-project-top-ten/)
- [CIS Kubernetes Benchmark](https://www.cisecurity.org/benchmark/kubernetes)
