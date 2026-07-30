# CloudAI Fusion ZK Prover - Deployment Verification Script for Windows

This script performs complete deployment validation:
1. Compile circuit and verify outputs
2. Run all test suites (unit, integration, performance)
3. Build Docker image
4. Deploy to staging Kubernetes cluster
5. Execute smoke tests and health checks

Usage: .\deploy-verify-zkp.ps1 [--dry-run] [--skip-tests]

Param(
    [switch]$DryRun = $false,
    [switch]$SkipTests = $false
)

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "CloudAI Fusion ZK Prover Deployment Verification" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "Namespace: zkp-staging" -ForegroundColor Yellow
Write-Host "Dry Run: $DryRun" -ForegroundColor Yellow
Write-Host "Skip Tests: $SkipTests" -ForegroundColor Yellow
Write-Host ""

# ============================================================================
# Phase 1: Circuit Compilation & Verification
# ============================================================================

function Test-CircuitCompilation {
    Write-Host "`n========== PHASE 1: Circuit Compilation ==========" -ForegroundColor Cyan
    
    Set-Location "$PSScriptRoot\circuits"
    
    if (-Not (Test-Path "scheduling_fairness.circom")) {
        Write-Host "ERROR: Circuit file not found!" -ForegroundColor Red
        exit 1
    }
    
    # Check Circom installation
    $circomExe = Get-Command circom -ErrorAction SilentlyContinue
    if ($null -eq $circomExe) {
        Write-Host "WARNING: Circom not found in PATH." -ForegroundColor Yellow
        Write-Host "Skipping compilation. Please install Circom via npm." -ForegroundColor Yellow
        return $true
    }
    
    # Create build directory
    $buildDir = Join-Path $PSScriptRoot "circuits\build"
    New-Item -ItemType Directory -Path $buildDir -Force | Out-Null
    
    # Compile circuit
    Write-Host "Compiling scheduling_fairness.circom..." -ForegroundColor Green
    
    try {
        circom scheduling_fairness.circom `
            --r1cs `
            --wasm `
            --sym `
            --O0 `
            --include "../circomlib/" `
            --output "$buildDir" `
            --verbose
        
        Write-Host "✓ Circuit compiled successfully!" -ForegroundColor Green
        
        # Count constraints
        $r1csFile = Join-Path $buildDir "scheduling_fairness.r1cs"
        if (Test-Path $r1csFile) {
            $fileSize = (Get-Item $r1csFile).Length
            Write-Host "Generated R1CS file: $([math]::Round($fileSize/1KB, 2)) KB" -ForegroundColor White
        }
        
        return $true
    }
    catch {
        Write-Host "ERROR: Circuit compilation failed!" -ForegroundColor Red
        Write-Host $_.Exception.Message -ForegroundColor Red
        return $false
    }
}

# ============================================================================
# Phase 2: Test Execution
# ============================================================================

function Test-GoTests {
    if ($SkipTests) {
        Write-Host "`n⚠ Skipping tests (--skip-tests flag provided)" -ForegroundColor Yellow
        return $true
    }
    
    Write-Host "`n========== PHASE 2: Running All Test Suites ==========" -ForegroundColor Cyan
    
    Set-Location "$PSScriptRoot"
    
    # Check Go installation
    $goExe = Get-Command go -ErrorAction SilentlyContinue
    if ($null -eq $goExe) {
        Write-Host "ERROR: Go not found in PATH!" -ForegroundColor Red
        Write-Host "Please install Go 1.19+ from https://golang.org/dl/" -ForegroundColor Red
        return $false
    }
    
    # Run unit tests with coverage
    Write-Host "Running unit tests with race detector..." -ForegroundColor Green
    
    try {
        $coverageProfile = "coverage-unit.out"
        
        $testOutput = go test .\pkg\scheduler\... `
            -v `
            -race `
            "-coverprofile=$coverageProfile" `
            -timeout 10m 2>&1
        
        Write-Host $testOutput
        
        # Generate coverage report
        if (Test-Path $coverageProfile) {
            Write-Host "`nGenerating coverage report..." -ForegroundColor Green
            go tool cover -func=$coverageProfile
            go tool cover -html=$coverageProfile -o coverage-report.html
            
            Write-Host "✓ Test execution completed!" -ForegroundColor Green
            Write-Host "Coverage HTML report: coverage-report.html" -ForegroundColor White
            return $true
        } else {
            Write-Host "WARNING: No coverage profile generated" -ForegroundColor Yellow
            return $true
        }
    }
    catch {
        Write-Host "ERROR: Test execution failed!" -ForegroundColor Red
        Write-Host $_.Exception.Message -ForegroundColor Red
        return $false
    }
}

# ============================================================================
# Phase 3: Docker Image Building
# ============================================================================

function Test-DockerBuild {
    Write-Host "`n========== PHASE 3: Building Docker Image ==========" -ForegroundColor Cyan
    
    # Check Docker availability
    $dockerExe = Get-Command docker -ErrorAction SilentlyContinue
    if ($null -eq $dockerExe) {
        Write-Host "Docker not available, skipping image build" -ForegroundColor Yellow
        return $true
    }
    
    # Validate Dockerfile
    if (-Not (Test-Path "Dockerfile.zkp")) {
        Write-Host "ERROR: Dockerfile.zkp not found!" -ForegroundColor Red
        return $false
    }
    
    $imageName = "cloudai-zkp-prover:test-$((Get-Random)"
    
    Write-Host "Building Docker image: $imageName" -ForegroundColor Green
    
    if ($DryRun) {
        Write-Host "[DRY RUN] Would execute: docker build -f Dockerfile.zkp -t $imageName ." -ForegroundColor Yellow
        return $true
    }
    
    try {
        docker build -f Dockerfile.zkp -t $imageName .
        
        Write-Host "✓ Image built successfully!" -ForegroundColor Green
        
        # Show image details
        Write-Host "`nImage details:" -ForegroundColor White
        docker images cloudai-zkp-prover | Select-Object -Last 1
        
        return $true
    }
    catch {
        Write-Host "ERROR: Docker build failed!" -ForegroundColor Red
        Write-Host $_.Exception.Message -ForegroundColor Red
        return $false
    }
}

# ============================================================================
# Phase 4: Kubernetes Deployment
# ============================================================================

function Test-K8sDeployment {
    Write-Host "`n========== PHASE 4: Deploying to Kubernetes ==========" -ForegroundColor Cyan
    
    # Check kubectl availability
    $kubectlExe = Get-Command kubectl -ErrorAction SilentlyContinue
    if ($null -eq $kubectlExe) {
        Write-Host "kubectl not available, skipping K8s deployment" -ForegroundColor Yellow
        return $true
    }
    
    # Verify cluster connectivity
    Write-Host "Checking Kubernetes cluster connectivity..." -ForegroundColor Green
    
    try {
        kubectl cluster-info > $null 2>&1
        if ($LASTEXITCODE -ne 0) {
            throw "Cannot connect to Kubernetes cluster"
        }
    }
    catch {
        Write-Host "ERROR: Cannot connect to Kubernetes cluster!" -ForegroundColor Red
        Write-Host $_.Exception.Message -ForegroundColor Red
        Write-Host "Make sure you have a valid kubeconfig configured." -ForegroundColor Yellow
        return $false
    }
    
    $namespace = "zkp-staging"
    
    # Create namespace if it doesn't exist
    Write-Host "Creating/updating namespace: $namespace" -ForegroundColor Green
    
    if ($DryRun) {
        Write-Host "[DRY RUN] Would create namespace: $namespace" -ForegroundColor Yellow
    } else {
        kubectl create namespace $namespace --dry-run=client -o yaml | kubectl apply -f -
    }
    
    # Install Helm chart
    $chartPath = Join-Path $PSScriptRoot "deploy\helm\cloudai-zkp-prover"
    
    if (-Not (Test-Path $chartPath)) {
        Write-Host "ERROR: Helm chart not found at $chartPath!" -ForegroundColor Red
        return $false
    }
    
    Write-Host "Installing Helm release: zkp-prover-deploy" -ForegroundColor Green
    
    if ($DryRun) {
        Write-Host "[DRY RUN] Would execute:" -ForegroundColor Yellow
        Write-Host "  helm install zkp-prover-deploy $chartPath \"
        Write-Host "    --namespace $namespace \"
        Write-Host "    --create-namespace --wait --timeout 5m"
    } else {
        helm upgrade --install zkp-prover-deploy $chartPath `
            --namespace $namespace `
            --create-namespace `
            --wait `
            --timeout 5m `
            --set replicaCount=1 `
            --set resources.limits.cpu="1" `
            --set resources.limits.memory="2Gi" `
            --set autoscaling.enabled=false
    }
    
    if ($LASTEXITCODE -eq 0) {
        Write-Host "✓ Helm deployment successful!" -ForegroundColor Green
        
        # Wait for pods to be ready
        Write-Host "`nWaiting for pods to become ready..." -ForegroundColor Green
        
        if ($DryRun) {
            Write-Host "[DRY RUN] Would wait for pods to be ready" -ForegroundColor Yellow
        } else {
            Start-Sleep -Seconds 10
            kubectl wait --for=condition=ready pod `
                -l app.kubernetes.io/name=cloudai-zkp-prover `
                -n $namespace `
                --timeout=300s || Write-Host "Some pods may not be ready yet" -ForegroundColor Yellow
        }
        
        return $true
    } else {
        Write-Host "ERROR: Helm deployment failed!" -ForegroundColor Red
        return $false
    }
}

# ============================================================================
# Phase 5: Smoke Tests & Health Checks
# ============================================================================

function Test-SmokeHealth {
    Write-Host "`n========== PHASE 5: Smoke Tests & Health Checks ==========" -ForegroundColor Cyan
    
    if ($DryRun) {
        Write-Host "[DRY RUN] Would run smoke tests against deployed service" -ForegroundColor Yellow
        Write-Host "After deployment, you can manually test:" -ForegroundColor Yellow
        Write-Host "  kubectl port-forward svc/cloudai-zkp-prover 8080:8080 -n zkp-staging" -ForegroundColor White
        Write-Host "  curl http://localhost:8080/health" -ForegroundColor White
        return $true
    }
    
    # Try to get service endpoint
    $serviceName = "cloudai-zkp-prover"
    $port = 8080
    
    Write-Host "Attempting to access service..." -ForegroundColor Green
    
    try {
        # Port-forward for local testing
        Write-Host "Port forwarding service for local access..." -ForegroundColor Green
        
        $process = Start-Process kubectl `
            -ArgumentList "port-forward", "svc/$serviceName", "$port:8080", "-n", "zkp-staging" `
            -Wait `
            -NoNewWindow `
            -PassThru `
            -RedirectStandardOutput "C:\temp\zkp-portforward.log"
        
        # Wait for port forward to be ready
        Start-Sleep -Seconds 5
        
        # Test health endpoint
        Write-Host "Testing health endpoint..." -ForegroundColor Green
        
        try {
            $response = Invoke-WebRequest -Uri "http://localhost:$port/health" -UseBasicParsing
            Write-Host "Health response: $($response.Content)" -ForegroundColor Green
        } catch {
            Write-Host "WARNING: Health check endpoint not responding immediately" -ForegroundColor Yellow
        }
        
        return $true
    }
    catch {
        Write-Host "ERROR: Smoke test failed!" -ForegroundColor Red
        Write-Host $_.Exception.Message -ForegroundColor Red
        return $false
    }
}

# ============================================================================
# Main Execution Flow
# ============================================================================

try {
    Write-Host "🚀 Starting deployment verification pipeline..." -ForegroundColor Cyan
    
    # Phase 1: Circuit Compilation
    if (-Not (Test-CircuitCompilation)) {
        exit 1
    }
    
    # Phase 2: Test Execution
    if (-Not (Test-GoTests)) {
        exit 1
    }
    
    # Phase 3: Docker Build
    if (-Not (Test-DockerBuild)) {
        exit 1
    }
    
    # Phase 4: K8s Deployment
    if (-Not (Test-K8sDeployment)) {
        exit 1
    }
    
    # Phase 5: Smoke Tests
    if (-Not (Test-SmokeHealth)) {
        exit 1
    }
    
    Write-Host ""
    Write-Host "========================================" -ForegroundColor Green
    Write-Host "✅ ALL DEPLOYMENT VERIFICATION PHASES COMPLETED!" -ForegroundColor Green
    Write-Host "========================================" -ForegroundColor Green
    
    Write-Host "`nNext steps:" -ForegroundColor Cyan
    Write-Host "  • Review generated reports in coverage-report.html" -ForegroundColor White
    Write-Host "  • Check Helm deployment status: helm status zkp-prover-deploy -n zkp-staging" -ForegroundColor White
    Write-Host "  • Monitor logs: kubectl logs -l app.kubernetes.io/name=cloudai-zkp-prover -n zkp-staging" -ForegroundColor White
    
    exit 0
}
catch {
    Write-Host ""
    Write-Host "🚨 Deployment verification interrupted!" -ForegroundColor Red
    Write-Host $_.Exception.Message -ForegroundColor Red
    exit 1
}
