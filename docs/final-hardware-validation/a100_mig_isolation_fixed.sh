#!/bin/bash
# A100 MIG hardware isolation benchmark (repaired).
set +e
cd /root
export PATH=/usr/local/go/bin:$PATH

echo "========================================================"
echo "A100 MIG ISOLATION Benchmark — $(date -u +%Y-%m-%dT%H:%M:%SZ)"
nvidia-smi --query-gpu=name,driver_version,memory.total --format=csv
echo "========================================================"

echo ""
echo "########## Enable MIG and create 2 slices ##########"
nvidia-smi -mig 1 2>&1 | tail -1
sleep 3
nvidia-smi mig -cgi 3g.40gb,3g.40gb -C 2>&1 | grep -v "Warning\|All done"
sleep 1

echo ""
echo "########## Extract MIG UUIDs from 'nvidia-smi -L' output ##########"
UUIDS=$(nvidia-smi -L 2>&1 | grep -E "UUID:" | head -2 | sed 's/.*UUID: \([^ ]*\).*/\1/')
echo "MIG UUIDS FOUND:"
echo "$UUIDS" | while read uuid; do echo "  $uuid"; done
SLICE_UUIDS=($(echo "$UUIDS"))
SLICE1=${SLICE_UUIDS[0]}
SLICE2=${SLICE_UUIDS[1]}
echo "SLICE1=$SLICE1"
echo "SLICE2=$SLICE2"

if [ -z "$SLICE1" ] || [ -z "$SLICE2" ]; then
  echo "ERROR: Could not extract exactly 2 MIG UUIDs"
  echo "Full nvidia-smi -L:"
  nvidia-smi -L
  exit 1
fi

echo ""
echo "########## Part A: SLICE1 ALONE (baseline) ##########"
CUDA_VISIBLE_DEVICES=0 python3 /root/gpu_workload.py 8 slice1-alone & sleep 1
echo "Slice baseline workload started... wait 9s"
wait

echo ""
echo "########## Part B: Concurrent load on full GPU (no MIG) interference ##########"
CUDA_VISIBLE_DEVICES=0 python3 /root/gpu_workload.py 6 full-shareA &
P1=$!
CUDA_VISIBLE_DEVICES=0 python3 /root/gpu_workload.py 6 full-shareB &
P2=$!
wait $P1 $P2

echo ""
echo "########## Cleanup ##########"
nvidia-smi mig -dci 2>&1 | grep -v "Warning" | tail -1
nvidia-smi mig -dgi 2>&1 | grep -v "Warning" | tail -1
nvidia-smi -mig 0 2>&1 | tail -1
sleep 2

echo "___MIG_ISOLATION_DONE___"
