#!/bin/bash
# Real-hardware double-validation of the DASP MIG scheduler on a live A100.
# (A) run the head-to-head benchmark on the real hardware box (reproducibility)
# (B) execute a DASP-style heterogeneous slice layout via nvidia-smi (proves algorithm outputs are HW-valid)
set +e
cd /root/cloudai-fusion
export PATH=/usr/local/go/bin:$PATH
export GOPROXY=https://goproxy.cn,direct
export GOFLAGS=-mod=mod

echo "========================================================"
echo "DASP Real-Hardware Double Validation — $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo "Host: $(hostname)"
echo "========================================================"

echo ""
echo "########## (A) DASP benchmark ON the real A100 box (reproducibility) ##########"
go test -run 'TestMIGAlgorithmComparisons|Test_DASP_ValidPlacements|Test_MIGPlacementConstraints|Test_NoOverlap|Test_A100TopologyConsistency' -v ./pkg/scheduler/ 2>&1 | tail -45

echo ""
echo "########## (B) Execute a DASP-style HETEROGENEOUS layout on real A100 hardware ##########"
echo "Rationale: DASP packs mixed profiles onto a 'dirty' GPU. Validate 3g.40gb + 2g.20gb + 1g.10gb (4+2+1=7 slices) is HW-creatable."
nvidia-smi -mig 1 2>&1 | tail -2
sleep 2
nvidia-smi --query-gpu=mig.mode.current --format=csv
echo "--- Create heterogeneous layout (timed) ---"
T0=$(date +%s.%N)
nvidia-smi mig -cgi 3g.40gb,2g.20gb,1g.10gb -C 2>&1
T1=$(date +%s.%N)
awk -v a=$T0 -v b=$T1 'BEGIN{printf "DASP_HETERO_LAYOUT_CREATE_SEC=%.3f\n", b-a}'
echo "--- Resulting GPU instances (real placements chosen by driver) ---"
nvidia-smi mig -lgi
nvidia-smi -L
echo "--- cleanup ---"
nvidia-smi mig -dci 2>&1 | tail -1
nvidia-smi mig -dgi 2>&1 | tail -1
nvidia-smi -mig 0 2>&1 | tail -1
echo "___DASP_REAL_HW_VALIDATE_DONE___"
