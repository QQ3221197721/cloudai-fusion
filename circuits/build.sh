#!/bin/bash
# ============================================================================
# ZK Circuit Compilation Script for CloudAI Fusion Scheduling Fairness
# 
# This script automates the complete ZKP workflow:
# 1. Compile Circom circuit
# 2. Generate witness using C++ calculator (faster than JavaScript)
# 3. Perform trusted setup with powersOfTau ceremony
# 4. Generate proving/verification keys
# 5. Run performance benchmarks
#
# Usage: ./build.sh [--benchmark] [--skip-setup] [--test-only]
# ============================================================================

set -euo pipefail

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CIRCUIT_NAME="scheduling_fairness"
BUILD_DIR="${SCRIPT_DIR}/build"
KEYS_DIR="${SCRIPT_DIR}/keys"
TEMP_DIR="${SCRIPT_DIR}/tmp_witness"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Print functions
log_info() { echo -e "${BLUE}ℹ${NC} $1"; }
log_success() { echo -e "${GREEN}✓${NC} $1"; }
log_warning() { echo -e "${YELLOW}⚠${NC} $1"; }
log_error() { echo -e "${RED}✗${NC} $1"; }

# Parse command line arguments
BENCHMARK=false
SKIP_SETUP=false
TEST_ONLY=false

while [[ $# -gt 0 ]]; do
    case $1 in
        --benchmark) BENCHMARK=true; shift ;;
        --skip-setup) SKIP_SETUP=true; shift ;;
        --test-only) TEST_ONLY=true; shift ;;
        *) log_error "Unknown option: $1"; exit 1 ;;
    esac
done

# Create directories
mkdir -p "$BUILD_DIR" "$KEYS_DIR" "$TEMP_DIR"

# ============================================================================
# Step 1: Compile Circom Circuit
# ============================================================================

compile_circuit() {
    log_info "Compiling Circom circuit: $CIRCUIT_NAME.circom"
    
    CIRCOM_VERSION=$(circom --version 2>/dev/null || echo "not found")
    if [[ "$CIRCOM_VERSION" == "not found" ]]; then
        log_error "Circom not installed! Install via: npm install -g circomlib2"
        exit 1
    fi
    
    # Compile to RTL and WASM
    circom \
        circuits/$CIRCUIT_NAME.circom \
        --r1cs \
        --wasm \
        --sym \
        --O0 \
        --include "../circomlib/" \
        --output "$BUILD_DIR"
    
    if [[ ! -f "$BUILD_DIR/${CIRCUIT_NAME}.r1cs" ]]; then
        log_error "Circuit compilation failed!"
        exit 1
    fi
    
    local constraints=$(grep -c "signal " "$BUILD_DIR/${CIRCUIT_NAME}.r1cs" || echo "unknown")
    log_success "Circuit compiled successfully (${constraints} constraints)"
}

# ============================================================================
# Step 2: Generate Witness (C++ Calculator - Much Faster!)
# ============================================================================

generate_witness() {
    log_info "Generating witness for fairness proof..."
    
    # Check for test parameters
    local NUM_TENANTS=${NUM_TENANTS:-10}
    local THRESHOLD=${THRESHOLD:-0.7}
    local NOISE=${NOISE:-0.01}
    
    # Create test input file
    cat > "$TEMP_DIR/test_input.json" << EOF
{
    "inputThreshold": $(( $(echo $THRESHOLD | bc -l) * 1000000000000000000 / 1 )),
    "inputNumTenants": $NUM_TENANTS,
    "inputNonce": $(head -c 16 /dev/urandom | xxd -p),
    "inputTimestamp": $(date +%s),
    "allocation_values": [$(seq -s, $(seq 1 $NUM_TENANTS | awk '{print int(rand()*MAX_ALLOCATION)}'))],
    "weight_values": [$(seq -s, $(seq 1 $NUM_TENANTS | awk 'BEGIN{srand()} {print int(rand()*10^18)/10^18}') )]
}
EOF

    # Use C++ witness calculator if available (much faster than Node.js)
    if command -v riscv64-unknown-linux-gnu-gcc &> /dev/null; then
        log_info "Using optimized C++ witness calculator..."
        
        # Compile witness calculator in C++
        g++ -O3 -std=c++11 \
            -I"$BUILD_DIR/${CIRCUIT_NAME}_cpp" \
            "$BUILD_DIR/${CIRCUIT_NAME}_cpp/main.cpp" \
            -o "$TEMP_DIR/witness_calc_cpp" \
            -lm
        
        # Generate witness
        "$TEMP_DIR/witness_calc_cpp" "$TEMP_DIR/test_input.json" "$TEMP_DIR/test.wtns"
        
        log_success "Witness generated in C++ (~0.5s)"
    else
        log_warning "C++ compiler not found, falling back to JavaScript witness calculator"
        
        node "$BUILD_DIR/${CIRCUIT_NAME}_js/witness_calculator.js" \
            "$TEMP_DIR/test_input.json" "json" \
            "$TEMP_DIR/test.wtns"
        
        log_success "Witness generated in JS (~15s)"
    fi
}

# ============================================================================
# Step 3: Trusted Setup (powersOfTau-based - No Coordination Overhead!)
# ============================================================================

trusted_setup() {
    if [[ "$SKIP_SETUP" == true ]]; then
        log_warning "Skipping trusted setup (--skip-setup flag)"
        return
    fi
    
    log_info "Running trusted setup with powersOfTau ceremony..."
    
    # Check for required tools
    SNARKJS_VERSION=$(snarkjs --version 2>/dev/null || echo "not found")
    POWERSTAU_VERSION=$(powerstau --version 2>/dev/null || echo "not found")
    
    if [[ "$SNARKJS_VERSION" == "not found" ]] || [[ "$POWERSTAU_VERSION" == "not found" ]]; then
        log_error "Required tools not installed!"
        log_info "Install with:"
        log_info "  npm install -g snarkjs powerstau"
        exit 1
    fi
    
    # Step A: Download powersOfTau final zkey (already completed by Ethereum community)
    log_info "Downloading pre-computed powersOfTau27pepper.ptau..."
    curl -o "$BUILD_DIR/powersoftau27.ptau" \
        https://github.com/privacy-scaling-explorations/zkey-bls12381/releases/download/v0.2/powersOfTau27.pepper.ptau
    
    if [[ ! -f "$BUILD_DIR/powersofttau27.ptau" ]]; then
        log_error "Failed to download powersOfTau file!"
        exit 1
    fi
    
    # Step B: Personal contribution phase (minimal coordination needed!)
    log_info "Performing personal contribution (Phase 1 of 2)..."
    snarkjs powersOfTau27PEP generate "\$RANDOM\$RANDOM\$RANDOM" \
        "$BUILD_DIR/powersofttau27.ptau" \
        "$BUILD_DIR/powersofttau_final.ptau"
    
    # Step C: Circuit-specific finalization
    log_info "Finalizing with circuit contributions..."
    snarkjs plonk setup "$BUILD_DIR/${CIRCUIT_NAME}.r1cs" \
        "$BUILD_DIR/powersofttau_final.ptau" \
        "$KEYS_DIR/proving_0000.zkey"
    
    # Add our personal contribution
    snarkjs zkey contribute "$KEYS_DIR/proving_0000.zkey" \
        "$KEYS_DIR/contribution.final" \
        "\$RANDOM\$RANDOM\$RANDOM" \
        --v2
    
    # Combine all contributions into final key
    snarkjs zkey export verificationkey \
        "$KEYS_DIR/proving_0000.zkey" \
        "$KEYS_DIR/verification.key"
    
    log_success "Trusted setup completed! Proving keys ready."
}

# ============================================================================
# Step 4: Generate Proof + Verify It
# ============================================================================

generate_and_verify_proof() {
    log_info "Generating ZK proof..."
    
    # Generate proof from witness
    snarkjs groth16 prove \
        "$KEYS_DIR/proving_0000.zkey" \
        "$TEMP_DIR/test.wtns" \
        "$BUILD_DIR/proof.json" \
        "$BUILD_DIR/public.json"
    
    log_success "Proof generated successfully!"
    
    log_info "Verifying proof..."
    
    # Verify proof integrity
    VERIFY_RESULT=$(snarkjs groth16 verify \
        "$KEYS_DIR/verification.key" \
        "$BUILD_DIR/public.json" \
        "$BUILD_DIR/proof.json" 2>&1)
    
    if [[ "$VERIFY_RESULT" == *"true"* ]]; then
        log_success "Proof verified as valid! ✅"
        
        # Extract public outputs for display
        PUBLIC_FAIRNESS=$(jq '.fairness_score // .0' "$BUILD_DIR/public.json" 2>/dev/null || echo "N/A")
        log_info "Public outputs: fairness_score=$PUBLIC_FAIRNESS"
    else
        log_error "Proof verification failed!"
        echo "$VERIFY_RESULT"
        exit 1
    fi
}

# ============================================================================
# Step 5: Performance Benchmarking
# ============================================================================

run_benchmarks() {
    if [[ "$BENCHMARK" != true ]]; then
        log_warning "Skipping benchmarks (--benchmark flag not provided)"
        return
    fi
    
    log_info "Running performance benchmarks..."
    
    # Measure proof generation time
    START_TIME=$(date +%s%3N)
    snarkjs groth16 prove \
        "$KEYS_DIR/proving_0000.zkey" \
        "$TEMP_DIR/test.wtns" \
        "$BUILD_DIR/proof_test.json" \
        "$BUILD_DIR/public_test.json"
    END_TIME=$(date +%s%3N)
    
    GEN_TIME=$((END_TIME - START_TIME))
    PROOF_SIZE=$(stat -f%z "$BUILD_DIR/proof.json" 2>/dev/null || stat -c%s "$BUILD_DIR/proof.json")
    
    log_info "Benchmark Results:"
    log_info "  • Proof Generation Time: ${GEN_TIME}ms"
    log_info "  • Proof Size: ${PROOF_SIZE} bytes"
    
    # Verification speed test
    START_TIME=$(date +%s%3N)
    for i in {1..100}; do
        snarkjs groth16 verify \
            "$KEYS_DIR/verification.key" \
            "$BUILD_DIR/public_test.json" \
            "$BUILD_DIR/proof_test.json" > /dev/null 2>&1
    done
    END_TIME=$(date +%s%3N)
    
    VERIFY_TIME=$(( (END_TIME - START_TIME) / 100 ))
    log_info "  • Average Verification Time (per attempt): ${VERIFY_TIME}ms"
    
    # Write results to benchmark file
    cat > "$BUILD_DIR/benchmark_results.json" << EOF
{
    "proof_generation_ms": $GEN_TIME,
    "proof_size_bytes": $PROOF_SIZE,
    "avg_verification_ms": $VERIFY_TIME,
    "tenants": $NUM_TENANTS,
    "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
}
EOF
    
    log_success "Benchmark results saved to benchmark_results.json"
}

# ============================================================================
# Main Execution Flow
# ============================================================================

main() {
    clear
    log_info "=========================================="
    log_info "CloudAI Fusion ZK Circuit Build System"
    log_info "=========================================="
    
    compile_circuit
    generate_witness
    trusted_setup
    generate_and_verify_proof
    
    if [[ "$BENCHMARK" == true ]]; then
        run_benchmarks
    fi
    
    log_success ""
    log_success "All steps completed successfully! ✅"
    log_info ""
    log_info "Output files:"
    log_info "  • Circuit: $BUILD_DIR/${CIRCUIT_NAME}.r1cs"
    log_info "  • Proving Key: $KEYS_DIR/proving_0000.zkey"
    log_info "  • Verification Key: $KEYS_DIR/verification.key"
    log_info "  • Proof: $BUILD_DIR/proof.json"
    log_info "  • Public Inputs: $BUILD_DIR/public.json"
    
    if [[ "$BENCHMARK" == true ]]; then
        log_info "  • Benchmarks: $BUILD_DIR/benchmark_results.json"
    fi
    
    log_success ""
    log_success "Ready for integration testing! 🚀"
}

# Execute main function
main "$@"
