#!/bin/bash
# CloudAI Fusion — M2 MIG partitioning + M3 GPU topology benchmark
# Target: real NVIDIA A100 80GB (Aliyun ecs.gn7e-c16g1.4xlarge)
# Evidence for T2 (real hardware benchmark) of modules M2 (GPU MIG sharing) and M3 (GPU topology scheduling).
set +e

echo "========================================================"
echo "CloudAI Fusion M2 MIG + M3 Topology Benchmark (Real A100)"
echo "Host: $(hostname)  Date: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo "========================================================"

echo ""
echo "########## GPU IDENTITY ##########"
nvidia-smi --query-gpu=name,driver_version,vbios_version,memory.total,pci.bus_id --format=csv
nvidia-smi -L

echo ""
echo "########## M3: TOPOLOGY (real hardware) ##########"
echo "--- nvidia-smi topo -m ---"
nvidia-smi topo -m
echo "--- NVLink status (nvidia-smi nvlink -s) ---"
nvidia-smi nvlink -s 2>&1 || echo "nvlink: no active links on single-card instance"
echo "NOTE: this instance has 1x A100. Full multi-GPU NVLink topology requires >=2 GPUs (gn7e-c16g1.8xlarge or larger)."

echo ""
echo "########## M2: MIG PARTITIONING BENCHMARK ##########"
echo "--- Current MIG mode ---"
nvidia-smi --query-gpu=mig.mode.current --format=csv

echo "--- Enabling MIG mode (timed) ---"
T0=$(date +%s.%N)
nvidia-smi -mig 1
T1=$(date +%s.%N)
awk -v a=$T0 -v b=$T1 'BEGIN{printf "MIG_ENABLE_SEC=%.3f\n", b-a}'
nvidia-smi --query-gpu=mig.mode.current --format=csv

echo "--- Available GPU Instance Profiles (real A100 80GB) ---"
nvidia-smi mig -lgip

echo "--- Creating 7x 1g.10gb GPU Instances + Compute Instances (timed, max partition density) ---"
T2=$(date +%s.%N)
nvidia-smi mig -cgi 1g.10gb,1g.10gb,1g.10gb,1g.10gb,1g.10gb,1g.10gb,1g.10gb -C
T3=$(date +%s.%N)
awk -v a=$T2 -v b=$T3 'BEGIN{printf "MIG_CREATE_7SLICE_SEC=%.3f\n", b-a}'

echo "--- Enumerate created MIG instances ---"
nvidia-smi mig -lgi
nvidia-smi mig -lci
nvidia-smi -L

echo "--- Teardown (timed) ---"
T4=$(date +%s.%N)
nvidia-smi mig -dci
nvidia-smi mig -dgi
T5=$(date +%s.%N)
awk -v a=$T4 -v b=$T5 'BEGIN{printf "MIG_TEARDOWN_SEC=%.3f\n", b-a}'

echo "--- Recreate mixed profiles: 3g.40gb + 3g.40gb (heterogeneous slicing) ---"
T6=$(date +%s.%N)
nvidia-smi mig -cgi 3g.40gb,3g.40gb -C
T7=$(date +%s.%N)
awk -v a=$T6 -v b=$T7 'BEGIN{printf "MIG_CREATE_2x3g40gb_SEC=%.3f\n", b-a}'
nvidia-smi mig -lgi
nvidia-smi -L

echo "--- Cleanup: teardown + disable MIG (restore instance) ---"
nvidia-smi mig -dci
nvidia-smi mig -dgi
nvidia-smi -mig 0
nvidia-smi --query-gpu=mig.mode.current --format=csv

echo ""
echo "___M2_M3_BENCH_DONE___"
