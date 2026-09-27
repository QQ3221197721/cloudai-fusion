# ADR-001: Cross-Cloud Federated Identity with HashiCorp Vault

## Status
Accepted

## Context

We are building M2 Phase 4-5 of the Multi-Cloud Unified Interface, which requires production-grade security infrastructure across all six cloud providers (AWS/Azure/GCP/Alibaba/Tencent/Huawei). The system must support:

1. **Secure credential management** without plain text storage
2. **Automatic credential rotation** every 30 minutes with zero downtime
3. **Cross-cloud token exchange chain** following OAuth 2.0 RFC 8693
4. **Federation latency < 200ms p99** under production load
5. **Full audit trail** for all credential access and usage

The challenge is to implement this while maintaining a unified API across heterogeneous cloud providers with different authentication mechanisms.

## Decision

We choose **HashiCorp Vault + RFC 8693 OAuth 2.0 Token Exchange** as the foundation for federated identity management.

### Why Vault?

1. **Production-grade**: Industry standard for secrets management
2. **Native cloud integrations**: AWS Secrets Manager, Azure Key Vault, GCP KMS
3. **Dual-write strategy**: Built-in support for zero-downtime credential rotation
4. **Audit logging**: Comprehensive access logs built into KV engine
5. **Multi-cloud support**: Native support for all 6 target clouds
6. **TLS by default**: Security-first design

### Why RFC 8693?

1. **Industry standard**: OAuth 2.0 Token Exchange formalized by IETF
2. **Interoperability**: Works with existing OIDC providers (Okta, Auth0, Keycloak)
3. **Flexibility**: Supports various grant types and audience claims
4. **Scalability**: Stateless design enables caching and horizontal scaling

### Architecture Overview

```
┌─────────────────┐       ┌──────────────────┐       ┌─────────────────┐
│   User/SSO      │────→  │ Federation Layer │────→  │   Cloud APIs    │
│  (Okta/Auth0)   │ JWT   │                  │ Creds │   (AWS/Azure...)│
└─────────────────┘       └──────────────────┘       └─────────────────┘
                              ╱     │     ╲
                             ↓      ↓      ↓
                    ┌─────────────┬─────────────┬─────────────┐
                    │             │             │             │
              ┌─────▼─────┐ ┌─────▼─────┐ ┌─────▼─────┐ ... │
              │  AWS IAM  │ │  Azure AD │ │  GCP WF    │     │
              │Anywhere   │ │OIDC Fed   │ │Pools      │     │
              └───────────┘ └───────────┘ └───────────┘     │
                                                              │
                         ┌────────────────────────────────────┤
                         │                                    │
                          ▼                                   │
                    ┌──────────────┐                        │
                    │   Vault      │                        │
                    │  KV Engine   │◄───────────────────────┘
                    │  + Rotation  │
                    └──────────────┘
```

### Implementation Strategy

#### Step 1: Vault Integration (Priority #1)
- Deploy Vault dev environment for development
- Configure KV secrets engine v2 for cloud credentials
- Implement `VaultCredentialManager` with dual-write strategy
- Support fallback to environment variables for non-production

#### Step 2: Token Exchange Chain (Priority #2)
Implement cloud-specific STS clients implementing `STSClientInterface`:
1. **AWS IAM Roles Anywhere** (Most mature - implemented first)
2. **Azure AD OIDC Federation**
3. **GCP Workforce Pools**
4. **Alibaba Cloud RAM**
5. **Tencent Cloud STS**
6. **Huawei Cloud ISSP**

Each client follows the pattern:
```go
type STSClientInterface interface {
    ExchangeToken(ctx context.Context, idToken string, audience string, scope []string) (*FederatedCredentials, error)
}
```

#### Step 3: Caching Layer
- Use `sync.Map` for concurrent-safe cache
- TTL-based expiration with proactive cleanup
- Cache hit ratio monitoring
- Zero-latency path for repeated requests

#### Step 4: Performance Optimization
- Connection pooling for Vault HTTP client
- Batch credential refreshes to reduce lock contention
- Pre-warm cache during application startup
- Circuit breaker for Vault unavailability

### Trade-offs Considered

#### Alternative 1: Hardcoded Cloud SDKs
**Pros:** Simpler initial implementation  
**Cons:** No centralized control, no automatic rotation, plaintext in code

#### Alternative 2: Custom Secret Backend
**Pros:** Tailored to specific needs  
**Cons:** Maintenance burden, missed security updates, no audit trail

#### Alternative 3: Kubernetes Sealed Secrets
**Pros:** Native K8s integration  
**Cons:** Limited to cluster-bound, no cross-cloud federation

**Selected approach (Vault)** wins on maintainability, security, and multi-cloud coverage.

## Consequences

### Positive
✓ Centralized credential management across all 6 clouds  
✓ Automatic 30-minute rotation without service interruption  
✓ Full audit trail for compliance  
✓ Sub-millisecond cache hits for performance  
✓ Standardized RFC 8693 API  

### Negative
⚠ Requires additional operational overhead (Vault deployment)  
⚠ Learning curve for team unfamiliar with Vault  
⚠ Network hop to Vault adds latency (mitigated by caching)  

### Mitigations
- Provide comprehensive documentation and training
- Deploy Vault in high-availability mode
- Use caching aggressively (>95% cache hit rate expected)
- Implement graceful degradation if Vault unavailable

## Testing Strategy

### Unit Tests
- Mock Vault client for isolated testing
- Verify rotation sequence generation
- Test cache invalidation logic

### Integration Tests
- End-to-end workflow: User → JWT → Credentials → Workload Provisioning
- Run against real Vault dev server
- Test all 6 cloud providers

### Load Tests
- Benchmark token exchange latency (<200ms p99 requirement)
- Concurrent request handling (10+ goroutines)
- Cache hit/miss ratio analysis

### Chaos Engineering
- Simulate Vault unavailability
- Test failover to environment variable credentials
- Validate circuit breaker behavior

## References

- [HashiCorp Vault Documentation](https://www.vaultproject.io/docs)
- [RFC 8693: OAuth 2.0 Token Exchange](https://datatracker.ietf.org/doc/html/rfc8693)
- [AWS IAM Roles Anywhere](https://docs.aws.amazon.com/rolesanywhere/latest/ug/what-is.html)
- [M2 Phase 4-5 Requirements](../../docs/m2-phase-4-5-specs.md)
