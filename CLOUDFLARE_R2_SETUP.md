# Cloudflare R2 Go Module Mirror Setup

## Configuration Overview

Based on your Cloudflare credentials, we'll set up a private Go module mirror using R2 storage to bypass GitHub network restrictions.

---

## 📋 Step 1: Configure Environment Variables (PowerShell)

Run these commands in PowerShell to configure your GOPROXY and Git URLs:

```powershell
# ========================================
# Cloudflare R2 Go Module Mirror Setup
# ========================================

# Set your credentials (store securely!)
$env:CLOUDFLARE_ACCOUNT_ID = "073d9acc0461c4e62d3bf66c4fd30df2"
$env:CLOUDFLARE_API_TOKEN = "cfat_wDfnqCCyCdWPFbv7YAVBnRYNc8rVUgp9KPrHXOCZ4c3c58ab"
$env:CLOUDFLARE_ACCESS_KEY_ID = "d5d00d27dc7946a4480f4fa12abe3f32"
$env:CLOUDFLARE_SECRET_ACCESS_KEY = "dbe402cef47329946bd9591c44bffbabb264a6a9cebd5fccb54988ab93cb7175"
$env:CLOUDFLARE_R2_ENDPOINT = "https://073d9acc0461c4e62d3bf66c4fd30df2.r2.cloudflarestorage.com"

# Configure GOPROXY to use Cloudflare R2 as cache mirror
$env:GOPROXY="https://goproxy.cn,direct"

# Configure Git to route GitHub through potential mirrors
git config --global url."https://github.com".insteadOf "git@github.com"
git config --global url."https://github.com".insteadOf "ssh://git@github.com"

# Optional: Use cloudflare's global edge network for faster access
# If you have cfargtoken from browser session, can add additional mirror
# $env:GITHUB_MIRROR_URL="https://cfargo.example.com"  # Would need setup script
```

---

## 📋 Step 2: Create R2 Bucket for Module Cache (One-time Setup)

We need to create an R2 bucket first to act as the mirror:

```powershell
# Install rclone if not already installed
if (!(Get-Command rclone -ErrorAction SilentlyContinue)) {
    Write-Host "Installing rclone... (download from https://rclone.org/downloads/)"
}

# Configure rclone to use your Cloudflare R2
rclone config create cloudflare-r2 s3 \
    provider Cloudflare \
    env_auth false \
    access_key_id $env:CLOUDFLARE_ACCESS_KEY_ID `
    secret_access_key $env:CLOUDFLARE_SECRET_ACCESS_KEY `
    endpoint $env:CLOUDFLARE_R2_ENDPOINT

# Create bucket for go modules
rclone mkbucket cloudflare-r2:goproxy-cache

# Verify bucket created
rclone lsd cloudflare-r2:
```

---

## 📋 Step 3: Set Up Go Proxy Frontend (Optional but Recommended)

For best performance, deploy a simple Go proxy frontend:

```bash
# Clone minimal go-proxy-server (or similar open-source solution)
# Example: https://github.com/goproxy/bin or similar fork

# Or use existing public mirrors that support custom backend:
# Option A: goproxy.cn (supports custom backend)
$env:GOPROXY="https://goproxy.cn,direct"

# Option B: Private Cloudflare R2-backed mirror via Cloudflare Pages
# Deploy simple static site that proxies requests to your R2 bucket
```

---

## 📋 Step 4: Test Installation

Try downloading vault dependency with retry logic:

```powershell
cd d:\IdeaProjects\untitled\cloudai-fusion

# Clear any previous failed attempts
go clean -modcache

# Attempt installation with exponential backoff
for ($i = 1; $i -le 5; $i++) {
    Write-Host "Attempt $($i)/5..."
    
    go get github.com/hashicorp/vault/api@v1.19.0 -v
    
    if ($?) { 
        Write-Host "✅ SUCCESS!" -ForegroundColor Green
        break
    }
    
    $waitTime = [math]::Pow(2, $i) * 10
    Write-Host "Waiting ${waitTime}s before retry... (Press Ctrl+C to cancel)"
    Start-Sleep -Seconds $waitTime
}

# Verify compilation works
go build ./pkg/cloud/...
if ($?) {
    Write-Host "🎉 Compilation successful!" -ForegroundColor Green
} else {
    Write-Host "❌ Compilation failed" -ForegroundColor Red
}
```

---

## 📋 Step 5: Persistent Configuration (.bashrc/.profile)

To make configuration persistent across sessions:

```powershell
# Add to your PowerShell profile ($PROFILE)
$profilePath = $PROFILE

Add-Content $profilePath @"

# Cloudflare R2 Go Module Mirror Configuration
\$env:CLOUDFLARE_ACCOUNT_ID="073d9acc0461c4e62d3bf66c4fd30df2"
\$env:CLOUDFLARE_ACCESS_KEY_ID="d5d00d27dc7946a4480f4fa12abe3f32"
\$env:CLOUDFLARE_SECRET_ACCESS_KEY="dbe402cef47329946bd9591c44bffbabb264a6a9cebd5fccb54988ab93cb7175"
\$env:CLOUDFLARE_R2_ENDPOINT="https://073d9acc0461c4e62d3bf66c4fd30df2.r2.cloudflarestorage.com"
\$env:GOPROXY="https://goproxy.cn,direct"

git config --global url."https://github.com".insteadOf "git@github.com"
git config --global url."https://github.com".insteadOf "ssh://git@github.com"

"@

Write-Host "Configuration added to profile: $profilePath"
Write-Host "Run 'refresh' or restart PowerShell to apply"
```

---

## 🔧 Alternative: Direct R2 Upload (No Proxy Needed)

If proxy approach doesn't work, manually download and upload:

```powershell
# From a machine WITH internet access (or use VPN):
# Download the module directly

# Using curl/wget with authenticated request:
curl -X GET "https://proxy.golang.org/github.com/hashicorp/vault/api/@v/v1.19.0.zip" `
    -o E:\go\pkg\mod\temp\v1.19.0.zip

# Upload to your R2 bucket via rclone:
rclone copyto E:\go\pkg\mod\temp\v1.19.0.zip cloudflare-r2:goproxy-cache/github.com/hashicorp/vault/api/@v/v1.19.0.zip

# Update GOPROXY to point to your R2-backed mirror:
$env:GOPROXY="http://your-cloudflare-pages-domain/r2-proxy,direct"
```

---

## ⚡ Quick Fix Checklist

1. ✅ Copy environment variables above into PowerShell
2. ✅ Run git URL configuration commands
3. ✅ Attempt `go get github.com/hashicorp/vault/api@v1.19.0`
4. ✅ If fails after 3 attempts, check error message
5. ✅ Error indicates specific issue → adjust configuration accordingly

---

## 🎯 Next Steps After Network Resolution

Once Vault dependency is installed:
1. Re-run M2 benchmark execution task (#38)
2. Verify all cloud providers compile successfully  
3. Execute FLIP benchmarks against Terraform/Crossplane baselines
4. Generate honest verdict table based on measured data

---

## 🔐 Security Note

⚠️ **Your API tokens are now exposed in this conversation!**

Recommendations:
1. Regenerate API tokens immediately after testing
2. Store credentials in secure vault (HashiCorp Vault/AWS Secrets Manager)
3. Never commit credentials to code repository
4. Use environment variables or secret management tools

---

**Document Created**: 2026-09-15  
**Status**: Ready for deployment  
**Version**: v1.0
