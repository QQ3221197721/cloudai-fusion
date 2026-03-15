#!/usr/bin/env bash
# ============================================================================
# CloudAI Fusion - One-Click Managed Kubernetes Deployment
# Supports: AWS EKS, Alibaba Cloud ACK, Azure AKS
# ============================================================================
#
# Usage:
#   scripts/deploy-cloud.sh --provider eks   [--region us-east-1] [--cluster-name cloudai-fusion]
#   scripts/deploy-cloud.sh --provider ack   [--region cn-hangzhou]
#   scripts/deploy-cloud.sh --provider aks   [--region eastus] [--resource-group cloudai-rg]
#   scripts/deploy-cloud.sh --provider local [--context kind-cloudai]
#   scripts/deploy-cloud.sh --uninstall
#
# Prerequisites:
#   EKS:   aws-cli, eksctl, kubectl, helm
#   ACK:   aliyun-cli, kubectl, helm
#   AKS:   az-cli, kubectl, helm
#   local: kubectl, helm, kind or minikube
#
# ============================================================================
set -euo pipefail

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
HELM_CHART="$PROJECT_DIR/deploy/helm/cloudai-fusion"
NAMESPACE="cloudai-fusion"
RELEASE_NAME="cloudai-fusion"
PROVIDER=""
REGION=""
CLUSTER_NAME="cloudai-fusion"
RESOURCE_GROUP="cloudai-rg"
K8S_VERSION="1.30"
UNINSTALL=false
DRY_RUN=false
VALUES_FILE=""

# Parse arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        --provider)       PROVIDER="$2"; shift 2 ;;
        --region)         REGION="$2"; shift 2 ;;
        --cluster-name)   CLUSTER_NAME="$2"; shift 2 ;;
        --resource-group) RESOURCE_GROUP="$2"; shift 2 ;;
        --namespace)      NAMESPACE="$2"; shift 2 ;;
        --k8s-version)    K8S_VERSION="$2"; shift 2 ;;
        --values)         VALUES_FILE="$2"; shift 2 ;;
        --uninstall)      UNINSTALL=true; shift ;;
        --dry-run)        DRY_RUN=true; shift ;;
        --help|-h)
            echo "Usage: scripts/deploy-cloud.sh --provider <eks|ack|aks|local> [OPTIONS]"
            echo ""
            echo "Providers:"
            echo "  eks     AWS Elastic Kubernetes Service"
            echo "  ack     Alibaba Cloud Container Service for Kubernetes"
            echo "  aks     Azure Kubernetes Service"
            echo "  local   Local cluster (kind/minikube with existing context)"
            echo ""
            echo "Options:"
            echo "  --region          Cloud region (default: provider-specific)"
            echo "  --cluster-name    Cluster name (default: cloudai-fusion)"
            echo "  --resource-group  Azure resource group (default: cloudai-rg)"
            echo "  --namespace       K8s namespace (default: cloudai-fusion)"
            echo "  --k8s-version     Kubernetes version (default: 1.30)"
            echo "  --values          Additional Helm values file"
            echo "  --uninstall       Remove CloudAI Fusion from cluster"
            echo "  --dry-run         Show what would be done"
            exit 0 ;;
        *) echo -e "${RED}Unknown option: $1${NC}"; exit 1 ;;
    esac
done

# --------------------------------------------------------------------------
# Helper functions
# --------------------------------------------------------------------------
log_step() { echo -e "\n${YELLOW}[$1]${NC} $2"; }
log_ok()   { echo -e "  ${GREEN}✓${NC} $1"; }
log_err()  { echo -e "  ${RED}✗${NC} $1"; }
log_info() { echo -e "  ${CYAN}→${NC} $1"; }

check_cmd() {
    if ! command -v "$1" &>/dev/null; then
        log_err "$1 is not installed. $2"
        return 1
    fi
    log_ok "$1 found: $(command -v "$1")"
}

# --------------------------------------------------------------------------
# Uninstall
# --------------------------------------------------------------------------
if [[ "$UNINSTALL" == true ]]; then
    echo -e "${YELLOW}Uninstalling CloudAI Fusion from namespace $NAMESPACE...${NC}"
    helm uninstall "$RELEASE_NAME" --namespace "$NAMESPACE" 2>/dev/null || true
    kubectl delete namespace "$NAMESPACE" 2>/dev/null || true
    echo -e "${GREEN}Done.${NC}"
    exit 0
fi

# --------------------------------------------------------------------------
# Validate provider
# --------------------------------------------------------------------------
if [[ -z "$PROVIDER" ]]; then
    echo -e "${RED}Error: --provider is required (eks|ack|aks|local)${NC}"
    echo "Run with --help for usage."
    exit 1
fi

echo ""
echo -e "${CYAN}=============================================================="
echo "  CloudAI Fusion - Cloud K8s Deployment"
echo "  Provider: $PROVIDER"
echo "  Cluster:  $CLUSTER_NAME"
echo -e "==============================================================${NC}"

# --------------------------------------------------------------------------
# Step 1: Check prerequisites
# --------------------------------------------------------------------------
log_step "1/6" "Checking prerequisites..."

check_cmd kubectl "Install: https://kubernetes.io/docs/tasks/tools/"
check_cmd helm "Install: https://helm.sh/docs/intro/install/"

case "$PROVIDER" in
    eks)
        REGION="${REGION:-us-east-1}"
        check_cmd aws "Install: https://docs.aws.amazon.com/cli/latest/userguide/install-cliv2.html"
        check_cmd eksctl "Install: https://eksctl.io/installation/"
        ;;
    ack)
        REGION="${REGION:-cn-hangzhou}"
        check_cmd aliyun "Install: https://help.aliyun.com/document_detail/139508.html"
        ;;
    aks)
        REGION="${REGION:-eastus}"
        check_cmd az "Install: https://docs.microsoft.com/en-us/cli/azure/install-azure-cli"
        ;;
    local)
        log_info "Using existing kubectl context"
        ;;
    *)
        log_err "Unknown provider: $PROVIDER (use eks|ack|aks|local)"
        exit 1
        ;;
esac

# --------------------------------------------------------------------------
# Step 2: Ensure cluster exists / configure kubeconfig
# --------------------------------------------------------------------------
log_step "2/6" "Configuring cluster access ($PROVIDER / $REGION)..."

case "$PROVIDER" in
    eks)
        if eksctl get cluster --name "$CLUSTER_NAME" --region "$REGION" &>/dev/null; then
            log_ok "EKS cluster '$CLUSTER_NAME' exists"
        else
            log_info "Creating EKS cluster '$CLUSTER_NAME'... (this takes ~15 min)"
            if [[ "$DRY_RUN" == true ]]; then
                log_info "[DRY-RUN] eksctl create cluster --name $CLUSTER_NAME --region $REGION --version $K8S_VERSION --managed --nodegroup-name system --node-type m6i.xlarge --nodes 3"
            else
                eksctl create cluster \
                    --name "$CLUSTER_NAME" \
                    --region "$REGION" \
                    --version "$K8S_VERSION" \
                    --managed \
                    --nodegroup-name system \
                    --node-type m6i.xlarge \
                    --nodes 3 \
                    --nodes-min 2 \
                    --nodes-max 5
            fi
        fi
        aws eks update-kubeconfig --name "$CLUSTER_NAME" --region "$REGION" 2>/dev/null || true
        ;;

    ack)
        log_info "Configuring ACK kubeconfig..."
        if [[ "$DRY_RUN" == true ]]; then
            log_info "[DRY-RUN] aliyun cs GET /api/v1/clusters --ClusterName $CLUSTER_NAME"
        else
            # Get cluster ID
            CLUSTER_ID=$(aliyun cs GET /api/v1/clusters 2>/dev/null | \
                python3 -c "import sys,json;cs=json.load(sys.stdin);print(next((c['cluster_id'] for c in cs.get('clusters',[]) if c['name']=='$CLUSTER_NAME'),''))" 2>/dev/null || echo "")
            if [[ -n "$CLUSTER_ID" ]]; then
                log_ok "ACK cluster '$CLUSTER_NAME' found (ID: $CLUSTER_ID)"
                # Merge kubeconfig
                aliyun cs GET "/api/v1/clusters/$CLUSTER_ID/user_config" 2>/dev/null | \
                    python3 -c "import sys,json;print(json.load(sys.stdin).get('config',''))" > /tmp/ack-kubeconfig.yaml
                export KUBECONFIG="${KUBECONFIG:-$HOME/.kube/config}:/tmp/ack-kubeconfig.yaml"
                kubectl config use-context "kubernetes-admin@$CLUSTER_NAME" 2>/dev/null || true
            else
                log_err "ACK cluster '$CLUSTER_NAME' not found in region $REGION"
                log_info "Create one at: https://cs.console.aliyun.com/"
                log_info "Or use Terraform: cd terraform && terraform apply -var region=$REGION"
                exit 1
            fi
        fi
        ;;

    aks)
        log_info "Configuring AKS kubeconfig..."
        if az aks show --name "$CLUSTER_NAME" --resource-group "$RESOURCE_GROUP" &>/dev/null; then
            log_ok "AKS cluster '$CLUSTER_NAME' exists"
        else
            log_info "Creating AKS cluster '$CLUSTER_NAME'... (this takes ~10 min)"
            if [[ "$DRY_RUN" == true ]]; then
                log_info "[DRY-RUN] az aks create --resource-group $RESOURCE_GROUP --name $CLUSTER_NAME --node-count 3 --kubernetes-version $K8S_VERSION"
            else
                az group create --name "$RESOURCE_GROUP" --location "$REGION" 2>/dev/null || true
                az aks create \
                    --resource-group "$RESOURCE_GROUP" \
                    --name "$CLUSTER_NAME" \
                    --node-count 3 \
                    --node-vm-size Standard_D4s_v3 \
                    --kubernetes-version "$K8S_VERSION" \
                    --enable-managed-identity \
                    --generate-ssh-keys
            fi
        fi
        az aks get-credentials --resource-group "$RESOURCE_GROUP" --name "$CLUSTER_NAME" --overwrite-existing 2>/dev/null || true
        ;;

    local)
        log_ok "Using current kubectl context: $(kubectl config current-context 2>/dev/null || echo 'none')"
        ;;
esac

# --------------------------------------------------------------------------
# Step 3: Verify cluster connectivity
# --------------------------------------------------------------------------
log_step "3/6" "Verifying cluster connectivity..."

if ! kubectl cluster-info &>/dev/null; then
    log_err "Cannot connect to Kubernetes cluster"
    log_info "Check your kubeconfig: kubectl config current-context"
    exit 1
fi

NODE_COUNT=$(kubectl get nodes --no-headers 2>/dev/null | wc -l)
K8S_VER=$(kubectl version -o json 2>/dev/null | python3 -c "import sys,json;v=json.load(sys.stdin);print(v.get('serverVersion',{}).get('gitVersion','unknown'))" 2>/dev/null || echo "unknown")
log_ok "Connected to cluster ($NODE_COUNT nodes, $K8S_VER)"

# --------------------------------------------------------------------------
# Step 4: Create namespace & secrets
# --------------------------------------------------------------------------
log_step "4/6" "Preparing namespace and secrets..."

kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f - 2>/dev/null

# Generate secrets if not provided
JWT_SECRET=$(openssl rand -hex 32 2>/dev/null || python3 -c "import secrets;print(secrets.token_hex(32))")
DB_PASSWORD=$(openssl rand -hex 16 2>/dev/null || python3 -c "import secrets;print(secrets.token_hex(16))")
GRAFANA_PASSWORD=$(openssl rand -hex 8 2>/dev/null || python3 -c "import secrets;print(secrets.token_hex(8))")

log_ok "Namespace '$NAMESPACE' ready"
log_ok "Secrets generated"

# --------------------------------------------------------------------------
# Step 5: Helm install
# --------------------------------------------------------------------------
log_step "5/6" "Installing CloudAI Fusion via Helm..."

HELM_ARGS=(
    upgrade --install "$RELEASE_NAME" "$HELM_CHART"
    --namespace "$NAMESPACE"
    --create-namespace
    --set "database.password=$DB_PASSWORD"
    --set "auth.jwtSecret=$JWT_SECRET"
    --set "postgresql.auth.password=$DB_PASSWORD"
    --set "monitoring.grafana.adminPassword=$GRAFANA_PASSWORD"
    --timeout 10m
    --wait
)

# Add production values for non-local deployments
if [[ "$PROVIDER" != "local" ]]; then
    HELM_ARGS+=(--values "$HELM_CHART/values-production.yaml")
fi

# Add custom values file
if [[ -n "$VALUES_FILE" ]]; then
    HELM_ARGS+=(--values "$VALUES_FILE")
fi

if [[ "$DRY_RUN" == true ]]; then
    log_info "[DRY-RUN] helm ${HELM_ARGS[*]}"
else
    helm "${HELM_ARGS[@]}"
fi

log_ok "Helm release '$RELEASE_NAME' deployed"

# --------------------------------------------------------------------------
# Step 6: Verify deployment
# --------------------------------------------------------------------------
log_step "6/6" "Verifying deployment..."

if [[ "$DRY_RUN" != true ]]; then
    echo ""
    kubectl get pods -n "$NAMESPACE" -o wide
    echo ""
    kubectl get svc -n "$NAMESPACE"
fi

# --------------------------------------------------------------------------
# Summary
# --------------------------------------------------------------------------
echo ""
echo -e "${GREEN}=============================================================="
echo "  CloudAI Fusion deployed successfully!"
echo "=============================================================="
echo ""
echo "  Provider:   $PROVIDER ($REGION)"
echo "  Cluster:    $CLUSTER_NAME"
echo "  Namespace:  $NAMESPACE"
echo "  Release:    $RELEASE_NAME"
echo ""
echo "  Access:"
echo "    kubectl get pods -n $NAMESPACE"
echo "    kubectl port-forward svc/$RELEASE_NAME-apiserver 8080:8080 -n $NAMESPACE"
echo "    kubectl port-forward svc/$RELEASE_NAME-grafana 3000:3000 -n $NAMESPACE"
echo ""
echo "  Secrets:"
echo "    DB Password:    $DB_PASSWORD"
echo "    JWT Secret:     ${JWT_SECRET:0:8}..."
echo "    Grafana Admin:  admin / $GRAFANA_PASSWORD"
echo ""
echo "  Uninstall:"
echo "    scripts/deploy-cloud.sh --uninstall --namespace $NAMESPACE"
echo -e "==============================================================${NC}"
echo ""
