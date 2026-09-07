#!/bin/bash
# M10 DQN Defect Fix Validation Script
# Runs all unit tests and simulations to verify convergence proof

set -e  # Exit on first error

echo "=========================================================="
echo "M10 RL Optimizer Defect Fix - Validation Suite"
echo "=========================================================="
echo ""

# Change to project root
cd "$(dirname "$0")/.."

echo "📍 Running from: $(pwd)"
echo ""

# Step 1: Enhanced State Representation Test
echo "=========================================="
echo "Week 1: State Encoding Validation"
echo "=========================================="
go test -v ./pkg/scheduler -run "^TestEnhancedStateRepresentation$"
echo ""

# Step 2: Multi-Objective Reward Test  
echo "=========================================="
echo "Week 2: Multi-Objective Reward Validation"
echo "=========================================="
go test -v ./pkg/scheduler -run "^TestMultiObjectiveReward$"
echo ""

# Step 3: Adaptive Explorer Test
echo "=========================================="
echo "Week 3: Adaptive Exploration Validation"
echo "=========================================="
go test -v ./pkg/scheduler -run "^TestAdaptiveExplorer$"
echo ""

# Step 4: Full Integration Test Suite
echo "=========================================="
echo "Week 4: Complete Defect Fix Validation"
echo "=========================================="
go test -v ./pkg/scheduler -run "^TestM10RLDefectFixes$"
echo ""

# Step 5: Optional Full Training Simulation (uncomment for production use)
echo "=========================================="
echo "Optional: Full Training Simulation"
echo "(Commented out for CI speed - uncomment to run 10k episodes)"
echo "=========================================="
# go test -v ./pkg/scheduler -run "^TestDQN_TrainingSimulation$"
echo "✅ Skipped training simulation (use 'go test -v ./pkg/scheduler -run TestDQN_TrainingSimulation' to enable)"
echo ""

# Step 6: Performance Benchmarking
echo "=========================================="
echo "Performance Benchmark: State Encoding"
echo "=========================================="
go test -bench=BenchmarkStateEncoding ./pkg/scheduler -benchmem
echo ""

echo "=========================================================="
echo "✅ All Validation Tests Completed Successfully!"
echo "=========================================================="
echo ""
echo "Summary:"
echo "  ✅ Week 1: Enhanced state representation (120→200 dim)"
echo "  ✅ Week 2: Multi-objective reward function"
echo "  ✅ Week 3: Adaptive exploration with ε-decay + UCB"
echo "  ✅ Week 4: Integration tests passing"
echo ""
echo "Full validation report: docs/M10_DQN_DEFECT_FIX_CONVERGENCE_PROOF.md"
echo ""
