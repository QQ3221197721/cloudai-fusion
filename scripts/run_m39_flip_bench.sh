#!/bin/bash
# M39 FLIP Benchmark Runner - Run Merkle drift detection vs Naive baseline
# This script runs the T2 benchmarks with count=6 and produces JSON output

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
WORKSPACE_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
OUTPUT_DIR="$WORKSPACE_ROOT/output"

mkdir -p "$OUTPUT_DIR"

echo "=========================================="
echo "M39 FLIP Benchmark Runner v1.0"
echo "=========================================="
echo ""

cd "$WORKSPACE_ROOT/cloudai-fusion"

echo "Step 1: Verify go mod cache..."
go env -w GOMODCACHE=E:/go/pkg/mod

echo ""
echo "Step 2: Run build check..."
go build ./pkg/gitops/... || { echo "Build failed!"; exit 1; }

echo ""
echo "Step 3: Run vet check..."
go vet ./pkg/gitops/... || { echo "Vet found issues!"; exit 1; }

echo ""
echo "Step 4: Run correctness tests (fairness guard)..."
go test ./pkg/gitops/... -run="TestCorrectness_NaiveVsMerkle_MatchAllScenarios" -v

echo ""
echo "Step 5: Running FLIP benchmarks with count=6..."
echo "Output will be saved to: $OUTPUT_DIR/m39_flip_bench.json"
echo ""

go test ./pkg/gitops/... \
    -bench="T2_" \
    -count=6 \
    -benchtime=2s \
    -json | tee "$OUTPUT_DIR/m39_flip_bench.json"

echo ""
echo "=========================================="
echo "BENCHMARKS COMPLETE"
echo "Results saved to: $OUTPUT_DIR/m39_flip_bench.json"
echo "=========================================="
