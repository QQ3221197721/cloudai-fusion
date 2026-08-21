#!/bin/bash
# A100 "to the extreme" real-hardware benchmark driver.
# Part 1: full-GPU capability (TFLOPS/bandwidth)
# Part 2: MIG hardware isolation (2 slices run concurrently, measure per-slice stability)
# Part 3: full-GPU contention (2 workloads share whole GPU, measure interference)
set +e
cd /root
export PATH=/usr/local/go/bin:$PATH

echo "========================================================"
echo "A100 EXTREME real-hardware benchmark — $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo "Host: $(hostname)"
nvidia-smi --query-gpu=name,driver_version,memory.total --format=csv
echo "========================================================"

echo ""
echo "########## PART 1: A100 full-GPU capability ##########"
python3 /root/a100_capability.py

echo ""
echo "########## PART 2: MIG hardware ISOLATION (2x 3g.40gb concurrent) ##########"
nvidia-smi -mig 1 2>&1 | tail -1
sleep 2
nvidia-smi mig -cgi 3g.40gb,3g.40gb -C 2>&1 | grep -i "created GPU instance"
MIGS=$(nvidia-smi -L | grep -oP 'MIG-[0-9a-f-]+')
M1=$(echo "$MIGS" | sed -n 1p)
M2=$(echo "$MIGS" | sed -n 2p)
echo "SLICE1=$M1"
echo "SLICE2=$M2"
echo "--- baseline: slice1 ALONE (no neighbor load) ---"
CUDA_VISIBLE_DEVICES=$M1 python3 /root/gpu_workload.py 6 slice1-alone
echo "--- concurrent: slice1 + slice2 BOTH loaded (isolation test) ---"
CUDA_VISIBLE_DEVICES=$M1 python3 /root/gpu_workload.py 8 slice1-concurrent &
P1=$!
CUDA_VISIBLE_DEVICES=$M2 python3 /root/gpu_workload.py 8 slice2-concurrent &
P2=$!
wait $P1 $P2
echo "--- teardown MIG ---"
nvidia-smi mig -dci 2>&1 | tail -1
nvidia-smi mig -dgi 2>&1 | tail -1
nvidia-smi -mig 0 2>&1 | tail -1
sleep 2

echo ""
echo "########## PART 3: full-GPU CONTENTION (2 workloads share whole GPU, no MIG) ##########"
echo "--- baseline: single workload ALONE on full GPU ---"
CUDA_VISIBLE_DEVICES=0 python3 /root/gpu_workload.py 6 full-alone
echo "--- concurrent: 2 workloads share the SAME full GPU (interference test) ---"
CUDA_VISIBLE_DEVICES=0 python3 /root/gpu_workload.py 8 full-shareA &
CUDA_VISIBLE_DEVICES=0 python3 /root/gpu_workload.py 8 full-shareB &
wait

echo ""
echo "___A100_EXTREME_DONE___"
