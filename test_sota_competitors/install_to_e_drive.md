# Competitor installation scripts to E: drive

## Installation Instructions

All competitor libraries will be installed to **E: drive** to preserve C: disk space.

### Prerequisites
- Go installed (go version >= 1.26)
- E:\ drive with sufficient space (~2GB for all competitors)
- Git installed and in PATH

### PowerShell Script (Windows)

```powershell
# Set environment variables for E: drive installation
$env:GOMODCACHE = "E:\go\pkg\mod"
$env:TEMP = "E:\tmp\competitor-install"
New-Item -ItemType Directory -Force -Path $env:TEMP | Out-Null

# Export for go commands
echo "export GOMODCACHE=E:\go\pkg\mod" | Out-File -FilePath ".\env.go.local" -Encoding utf8
echo "export TEMP=E:\tmp\competitor-install" | Out-File -FilePath ".\env.temp.local" -Encoding utf8

Write-Host "=== Installing M9 Quantile Competitors ==="
# Google PolySketch
git clone https://github.com/google/sketches.git $env:TEMP\sketches
cd $env:TEMP\sketches
go mod tidy
cd ..

# Prometheus client_golang should already be in cloudai-fusion/go.mod, verify version
go get github.com/prometheus/client_golang@v1.20.0

Write-Host "=== Installing M25 MDNS Upgrade ==="
# CRITICAL: Replace unmaintained grandcat/zeroconf with modern mdns-go
go get github.com/stoix/mdns-go@latest

Write-Host "=== Installing M40 Code Generators ==="
# swaggo/swag v2.0+ (NOT dead v1.14!)
go get github.com/swaggo/swag/v2@latest
go install github.com/swaggo/swag/v2/cmd/swag@latest

# deepmap/oapi-codegen v2.x
go get github.com/deepmap/oapi-codegen/v2@latest
go install github.com/deepmap/oapi-codegen/v2/cmd/oapi-codegen@latest

Write-Host "=== Installing M23 CRDT Libraries ==="
# automerge-rs with CGO bindings
go get github.com/automerge/automerge-go@latest

Write-Host "=== Installation Complete ==="
Write-Host "All competitors installed to E:\go\pkg\mod"
Write-Host "Check installation with: go list -m all | findstr sketch"
```

### Bash Script (Linux/Mac)

```bash
#!/bin/bash
# Set environment variables for E: drive installation
export GOMODCACHE=/mnt/e/go/pkg/mod
export TMPDIR=/mnt/e/tmp/competitor-install
mkdir -p $TMPDIR

# Create .env file for future reference
cat > .env.competitors << EOF
export GOMODCACHE=/mnt/e/go/pkg/mod
export TMPDIR=/mnt/e/tmp/competitor-install
EOF

echo "=== Installing M9 Quantile Competitors ==="
# Google PolySketch
git clone https://github.com/google/sketches.git $TMPDIR/sketches
cd $TMPDIR/sketches
go mod tidy
cd ..

# Prometheus client_golang
go get github.com/prometheus/client_golang@v1.20.0

echo "=== Installing M25 MDNS Upgrade ==="
# CRITICAL: Replace unmaintained grandcat/zeroconf with modern mdns-go
go get github.com/stoix/mdns-go@latest

echo "=== Installing M40 Code Generators ==="
# swaggo/swag v2.0+ (NOT dead v1.14!)
go get github.com/swaggo/swag/v2@latest
go install github.com/swaggo/swag/v2/cmd/swag@latest

# deepmap/oapi-codegen v2.x
go get github.com/deepmap/oapi-codegen/v2@latest
go install github.com/deepmap/oapi-codegen/v2/cmd/oapi-codegen@latest

echo "=== Installing M23 CRDT Libraries ==="
# automerge-rs with CGO bindings
go get github.com/automerge/automerge-go@latest

echo "=== Installation Complete ==="
echo "All competitors installed to /mnt/e/go/pkg/mod"
echo "Check installation with: go list -m all | grep sketch"
```

## Critical Notes

### M25 mDNS Discovery - UPGRADE REQUIRED!

**Current Problem**:
- Using `github.com/grandcat/zeroconf` which is UNMAINTAINED since March 2018!
- All benchmark comparisons against this library are self-comparisons (fake win!)

**Upgrade Path**:
1. Run the installation script above to get `mdns-go/mdns`
2. In codebase, search for all imports of `grandcat/zeroconf`
3. Replace with `github.com/stoix/mdns-go` or equivalent modern alternative
4. Update tests to use new API
5. Re-run benchmarks vs REAL modern competitor (not same codebase!)

### M40 Generator - Stale Competitor Fix

**Original Fake Claim**:
- "104× faster than go-swag v1.14"

**Reality**:
- go-swag v1.14 is DEPRECATED (last update 2020)
- Real comparison should be against swaggo/swag v2.0+ (current maintained version)
- Expected real advantage: ~1.8× instead of 104×

**Fix Required**:
After installing swaggo/swag v2, create fair benchmark comparing our generator vs swag v2 directly.

## Running Benchmarks After Installation

Once all competitors are installed to E: drive:

```bash
cd d:/IdeaProjects/untitled/cloudai-fusion/test_sota_competitors

# M9 Fair Benchmark
go test -bench=BenchmarkM9_HybridQuantile_vs_PolySketch -benchmem ./...

# M40 Fair Benchmark  
go test -bench=BenchmarkM40_OurGenerator_vs_SwagV2 -benchmem ./...

# M25 Fair Benchmark (after upgrading mdns)
go test -bench=BenchmarkM25_MDNS_vs_MdnsGo -benchmem ./...
```

## Expected Results

After fair comparisons:
- Some modules may still win (our optimizations are legitimate)
- But magnitudes will likely decrease from marketing spin to honest numbers
- Example: M40 might drop from "104×" to "1.8×" (still valid win, just honest!)