# Vault Production Deployment Guide

## Overview

This document provides step-by-step instructions for deploying HashiCorp Vault in production to support the M2 Phase 4-5 Cross-Cloud Federated Identity feature.

## Prerequisites

- Kubernetes cluster (v1.25+)
- Helm v3.10+
- PV storage class with dynamic provisioning
- TLS certificates (Let's Encrypt or internal CA)
- Cloud provider credentials (AWS/Azure/GCP permissions)

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Kubernetes Cluster                       │
│                                                             │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐  │
│  │Vault Leader  │◄──►│Vault Standby │◄──►│Vault Standby │  │
│  │Pod (Active)  │    │Pod 1          │    │Pod 2         │  │
│  └──────┬───────┘    └──────────────┘    └──────────────┘  │
│         │                                                   │
│         ▼                                                   │
│  ┌──────────────┐                                           │
│  │  Unsealed &  │                                           │
│  │   Ready      │                                           │
│  └──────┬───────┘                                           │
│         │                                                   │
│         ▼              ┌──────────────────┐                │
│  ┌──────────────┐     │   External Seals │                │
│  │   Raft       │◄────│(AWS/GCP/Azure)   │                │
│  │   Peers      │     └──────────────────┘                │
│  └──────────────┘                                           │
│                                                             │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐  │
│  │CloudAI API   │    │Scheduler      │    │Agent         │  │
│  │Server        │    │Service        │    │Service       │  │
│  └──────────────┘    └──────────────┘    └──────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

## Quick Start with Helm

### 1. Install Vault Helm Chart

```bash
helm repo add hashicorp https://helm.releases.hashicorp.com
helm repo update

helm install vault-hashicorp hashicorp/vault \
  --namespace vault \
  --create-namespace \
  --set "server.replicas=3" \
  --set "server.dataStorage.enabled=true" \
  --set "server.dataStorage.size=10Gi" \
  --set "server.ha.enabled=true" \
  --set "server.ha.peerShm.enabled=true" \
  --set "server.ui.enabled=true" \
  --set "injector.enabled=true" \
  --wait
```

### 2. Configure Vault for Production

#### Enable Required Secrets Engines

```bash
# Access Vault shell
kubectl exec -it vault-hashicorp-0 -n vault -- sh

# Run initialization script
vault status
vault operator init -key-shares=5 -key-threshold=3 > vault-unseal-keys.json
# Save unseal keys securely (offline backup required)
```

#### Initialize Vault

```bash
# Extract unseal keys and root token
kubectl get secret vault-unseal-keys -o jsonpath='{.data.UnsealKey}' | base64 -d
kubectl get secret vault-unseal-keys -o jsonpath='{.data.RootToken}' | base64 -d

# Unseal Vault (repeat N times where N=key-threshold)
for key in $(cat vault-unseal-keys.json | jq -r '.unseal_keys_b64[]'); do
  kubectl exec vault-hashicorp-0 -n vault -- vault unseal $key
done
```

#### Configure Production Settings

Apply `vault-config.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: vault-config
  namespace: vault
data:
  vault-init.sh: |
    #!/bin/sh
    
    # Set audit device
    vault secrets enable file
    vault secrets tune -path=file, max_lease_ttl=8760h file
    
    # Enable AWS Secrets Engine
    vault auth enable approle
    vault secrets enable aws
    
    # Create AWS Secrets Engine Path
    vault write aws/config/root \
        access_key=${VAULT_AWS_ACCESS_KEY} \
        secret_key=${VAULT_AWS_SECRET_KEY} \
        region=us-east-1 \
        iam_role_creation_endpoint=https://iam.us-east-1.amazonaws.com/ \
        sts_endpoint=https://sts.us-east-1.amazonaws.com/ \
        root_cert_bundle=/usr/local/share/ca-certificates/aws-ca-bundle.crt
    
    # Enable Kubernetes Auth Method
    vault auth enable kubernetes
    
    # Create Kubernetes Auth Backend Role
    vault write auth/kubernetes/config \
        token_reviewer_jwt="$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)" \
        kubernetes_host="https://$KUBERNETES_PORT_443_TCP_ADDR" \
        kubernetes_ca_cert=@/var/run/secrets/kubernetes.io/serviceaccount/ca.crt
    
    # Create Vault Policy
    vault policy put cloudai-fusion - <<EOF
    path "cloud/credentials/*" {
      capabilities = ["read", "list"]
    }
    
    path "aws/access/*" {
      capabilities = ["read", "write"]
    }
    
    path "auth/approle/login" {
      capabilities = ["create", "update"]
    }
    EOF
    
    # Bind policy to approle
    vault write auth/kubernetes/role/cloudai-role \
        bound_service_account_names=cloudai-app \
        bound_service_account_namespaces=default \
        policies=cloudai-fusion \
        ttl=1h
    
    echo "✓ Vault initialization complete"
```

### 3. Deploy Application with Vault Integration

#### Update Deployment Manifests

```yaml
# cloudai-fusion/apiserver/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: apiserver
  namespace: cloudai-fusion
spec:
  template:
    spec:
      serviceAccountName: cloudai-app
      containers:
      - name: apiserver
        image: cloudai-fusion/apiserver:v1.0.0
        env:
        - name: VAULT_ADDR
          value: "https://vault-hashicorp-vault:8200"
        - name: VAULT_NAMESPACE
          value: "cloudai-fusion"
        - name: VAULT_ROLE_ID
          valueFrom:
            secretKeyRef:
              name: vault-approle
              key: role_id
        - name: VAULT_SECRET_ID
          valueFrom:
            secretKeyRef:
              name: vault-approle
              key: secret_id
        volumeMounts:
        - name: tls-certs
          mountPath: /usr/local/share/ca-certificates
          readOnly: true
      volumes:
      - name: tls-certs
        configMap:
          name: aws-ca-bundle
```

#### Create Service Account

```yaml
# vault-service-account.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: cloudai-app
  namespace: default
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: vault-auth-binding
  namespace: default
subjects:
- kind: ServiceAccount
  name: cloudai-app
  namespace: default
roleRef:
  kind: Role
  name: vault-auth-role
  apiGroup: rbac.authorization.k8s.io
```

## Security Configuration

### Enable TLS Termination

```bash
# Generate self-signed certificates (replace with Let's Encrypt in prod)
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout tls.key \
  -out tls.crt \
  -subj "/CN=vault.vault.svc"

# Create TLS secret
kubectl create secret tls vault-tls \
  --cert=tls.crt \
  --key=tls.key \
  -n vault

# Update Helm values
helm upgrade vault-hashicorp hashicorp/vault \
  --namespace vault \
  --set "server.tls.enabled=true" \
  --set "server.tls.certSecretName=vault-tls" \
  --set "server.tls.keySecretName=vault-tls"
```

### Configure Auto-Seal (Recommended for Production)

#### AWS KMS (Example)

```yaml
# vault-auto-seal.yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: vault-auto-seal-setup
  namespace: vault
spec:
  template:
    spec:
      containers:
      - name: vault-auto-seal
        image: hashicorp/vault
        command: ["/bin/sh", "-c"]
        args:
        - |
          vault secrets disable transit
          vault secrets enable transit
          
          vault write transit/generate/key/cloudai-key \
            type=aes256-gcm96 \
            plaintext=$VAULT_ENCRYPTION_KEY_BASE64
      
      - name: configure-kms
        image: bitnami/kubectl
        env:
        - name: AWS_REGION
          value: us-east-1
        - name: AWS_ACCESS_KEY_ID
          valueFrom:
            secretKeyRef:
              name: cloud-credentials
              key: aws-access-key
        - name: AWS_SECRET_ACCESS_KEY
          valueFrom:
            secretKeyRef:
              name: cloud-credentials
              key: aws-secret-key
        command: ["/bin/sh", "-c"]
        args:
        - |
          export AWS_DEFAULT_REGION=$AWS_REGION
          aws kms create-grant \
            --grantee-principal arn:aws:iam::123456789012:role/vault-role \
            --grantor-principal arn:aws:iam::123456789012:role/vault-role \
            --operations Encrypt Decrypt \
            --key-id <your-kms-key-id>
```

### Configure Backup Strategy

#### Automated Backups to S3

```yaml
# vault-backup-schedule.yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: vault-backup
  namespace: vault
spec:
  schedule: "0 2 * * *"  # Daily at 2 AM UTC
  jobTemplate:
    spec:
      template:
        spec:
          serviceAccountName: vault-admin
          containers:
          - name: vault-backup
            image: hashicorp/vault
            env:
            - name: VAULT_ADDR
              value: "https://vault-hashicorp-vault:8200"
            - name: VAULT_TOKEN
              valueFrom:
                secretKeyRef:
                  name: vault-root-token
                  key: token
            command: ["/bin/sh", "-c"]
            args:
            - |
              vault snapshot save -format=json | \
              aws s3 cp - s3://vault-backups/$(date +%Y%m%d-%H%M%S).json
      
          restartPolicy: Never
```

## Monitoring & Alerting

### Prometheus Metrics

```yaml
# metrics-service.yaml
apiVersion: v1
kind: Service
metadata:
  name: vault-metrics
  namespace: vault
  labels:
    app: vault
    prometheus-scrape: "true"
spec:
  ports:
  - name: http-monitoring
    port: 9090
    targetPort: 9090
    protocol: TCP
  selector:
    app: vault
```

### Grafana Dashboard Template

```json
{
  "dashboard": {
    "title": "Vault Health Monitor",
    "panels": [
      {
        "title": "Unseal Progress",
        "targets": [
          {
            "expr": "vault_unseal_progress",
            "legendFormat": "{{instance}}"
          }
        ]
      },
      {
        "title": "Request Latency",
        "targets": [
          {
            "expr": "histogram_quantile(0.99, rate(vault_api_request_duration_seconds_bucket[1m]))",
            "legendFormat": "p99 latency"
          }
        ]
      }
    ]
  }
}
```

### Alert Rules

```yaml
# alertmanager-rules.yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: vault-alerts
  namespace: monitoring
spec:
  groups:
  - name: vault.rules
    rules:
    - alert: VaultIsSealed
      expr: vault_is_sealed == 1
      for: 5m
      annotations:
        summary: "Vault is sealed (instance {{ $labels.instance }})"
    - alert: VaultHighLatency
      expr: histogram_quantile(0.99, rate(vault_api_request_duration_seconds_bucket[5m])) > 0.5
      annotations:
        summary: "Vault request p99 latency > 500ms"
    - alert: VaultMemoryPressure
      expr: vault_memory_usage_bytes / vault_memory_limit_bytes > 0.9
      annotations:
        summary: "Vault memory usage > 90%"
```

## Troubleshooting

### Common Issues

#### Vault Won't Unseal

```bash
# Check seal progress
kubectl exec vault-hashicorp-0 -n vault -- vault status

# Verify node connectivity
kubectl exec vault-hashicorp-0 -n vault -- vault cluster list

# Check logs
kubectl logs vault-hashicorp-0 -n vault
```

#### Pod Keeps Crashing

```bash
# Check resource limits
kubectl describe pod vault-hashicorp-0 -n vault

# Increase resources if needed
helm upgrade vault-hashicorp hashicorp/vault \
  --namespace vault \
  --set "server.resources.requests.memory=4Gi" \
  --set "server.resources.limits.memory=8Gi"
```

#### TLS Certificate Errors

```bash
# Verify certificate chain
kubectl exec vault-hashicorp-0 -n vault -- openssl s_client -connect vault-hashicorp-vault:8200 -showcerts

# Re-create TLS secret
kubectl delete secret vault-tls -n vault
kubectl create secret tls vault-tls --cert=tls.crt --key=tls.key -n vault
```

### Support Resources

- [Vault Documentation](https://www.vaultproject.io/docs)
- [HashiCorp Developer Community](https://discuss.hashicorp.com/c/vault/)
- [Vault Slack Channel](https://slack.hashicorp.com/)
