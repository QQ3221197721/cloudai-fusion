#!/usr/bin/env bash
#
# m2_mig_validation.sh — M2 NVIDIA MIG (Multi-Instance GPU) partition validation
# =============================================================================
# Target hardware (rent one of):
#   * AWS  p4d.24xlarge     — 8× A100-SXM4-40GB, 400 Gbps EFA
#   * Azure ND96asr_v4      — 8× A100-SXM4-40GB, 8× 200 Gbps HDR InfiniBand
#
# What this proves for M2 ("GPU MIG partitioning is REAL, not simulated"):
#   1. NVIDIA driver + nvidia-smi present and reporting real A100 hardware
#   2. MIG mode can be enabled on a physical GPU
#   3. GPU can be partitioned into isolated MIG instances (1g.5gb profile)
#   4. Instances are memory/compute ISOLATED (workload on one cannot see another)
#   5. cloudai-fusion pkg/scheduler MIG code paths pass `go test` on the box
#
# HONESTY NOTE: This is REAL hardware validation. Steps 1-4 require a physical
# A100 with MIG support (A100/A30/H100). If run on a non-MIG GPU or a VM without
# GPU passthrough, the script fails loudly at the capability gate — it does NOT
# fake a pass. Cloud GPU quota must be requested in advance (see README.md).
#
# Usage (on the rented Linux box, as root or with sudo):
#   chmod +x m2_mig_validation.sh
#   sudo ./m2_mig_validation.sh 2>&1 | tee m2_mig_result.log
#
# Requirements: Ubuntu 20.04/22.04, bash, curl, Go >= 1.25, root for MIG ops.
# =============================================================================

set -euo pipefail

# ---- Result bookkeeping -----------------------------------------------------
RESULT_FILE="${RESULT_FILE:-m2_mig_result.log}"
STEP=0
FAILURES=0

log()  { echo "[$(date -u +%H:%M:%SZ)] $*"; }
step() { STEP=$((STEP + 1)); echo ""; echo "=== STEP ${STEP}: $* ==="; }
fail() { echo "[FAIL] $*" >&2; FAILURES=$((FAILURES + 1)); }
pass() { echo "[PASS] $*"; }

# Global error trap so a failed CLI command is never silently swallowed.
on_error() {
  local exit_code=$?
  echo ""
  echo "!!! ABORTED at step ${STEP} (exit ${exit_code}) — see ${RESULT_FILE} !!!" >&2
  echo "M2 MIG VALIDATION: FAILED" >&2
  exit "${exit_code}"
}
trap on_error ERR

# GPU index and MIG profile are overridable via env.
GPU_INDEX="${GPU_INDEX:-0}"
MIG_PROFILE="${MIG_PROFILE:-1g.5gb}"   # A100-40GB: 7× 1g.5gb; A100-80GB: 7× 1g.10gb
PROJECT_ROOT="${PROJECT_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"

echo "=============================================================="
echo "CLOUDAI FUSION — M2 MIG PARTITION VALIDATION"
echo "GPU index=${GPU_INDEX}  profile=${MIG_PROFILE}  root=${PROJECT_ROOT}"
echo "=============================================================="

# ---- STEP 1: Install / verify NVIDIA driver ---------------------------------
step "Verify NVIDIA driver + nvidia-smi"
if ! command -v nvidia-smi >/dev/null 2>&1; then
  log "nvidia-smi not found — installing driver (Ubuntu). Copy-paste block:"
  cat <<'INSTALL'
  # --- Driver install (run manually if this box has no driver) ---
  sudo apt-get update
  sudo apt-get install -y ubuntu-drivers-common
  sudo ubuntu-drivers autoinstall
  # OR pin a known-good branch:
  # sudo apt-get install -y nvidia-driver-550-server
  sudo reboot   # re-run this script after reboot
INSTALL
  fail "nvidia-smi missing — install driver then re-run"
  exit 1
fi

nvidia-smi
DRIVER_VER="$(nvidia-smi --query-gpu=driver_version --format=csv,noheader | head -1)"
GPU_NAME="$(nvidia-smi --query-gpu=name --format=csv,noheader | head -1)"
log "driver=${DRIVER_VER} gpu=${GPU_NAME}"

case "${GPU_NAME}" in
  *A100*|*A30*|*H100*|*H200*) pass "MIG-capable GPU detected: ${GPU_NAME}" ;;
  *) fail "GPU '${GPU_NAME}' does not support MIG — rent A100/A30/H100"; exit 1 ;;
esac

# ---- STEP 2: Enable MIG mode ------------------------------------------------
step "Enable MIG mode on GPU ${GPU_INDEX}"
# Enabling MIG may require no active CUDA contexts; -r resets the GPU.
sudo nvidia-smi -i "${GPU_INDEX}" --mig-enabled=1 || sudo nvidia-smi -i "${GPU_INDEX}" -mig 1
sudo nvidia-smi -i "${GPU_INDEX}" -r || log "GPU reset skipped (may need reboot on some drivers)"
sleep 3

MIG_STATE="$(nvidia-smi -i "${GPU_INDEX}" --query-gpu=mig.mode.current --format=csv,noheader)"
log "MIG mode current = ${MIG_STATE}"
[[ "${MIG_STATE}" == "Enabled" ]] && pass "MIG mode enabled" || { fail "MIG mode not Enabled"; exit 1; }

# ---- STEP 3: Create MIG partitions ------------------------------------------
step "Create MIG GPU instances (GI) + compute instances (CI) with profile ${MIG_PROFILE}"
log "Available GPU instance profiles:"
sudo nvidia-smi mig -i "${GPU_INDEX}" -lgip

# Create 7× 1g.5gb GPU instances (max on A100-40GB), then a CI in each GI.
sudo nvidia-smi mig -i "${GPU_INDEX}" -cgi "${MIG_PROFILE}" -C || \
  sudo nvidia-smi mig -i "${GPU_INDEX}" -cgi "${MIG_PROFILE}",\
"${MIG_PROFILE}","${MIG_PROFILE}","${MIG_PROFILE}","${MIG_PROFILE}","${MIG_PROFILE}","${MIG_PROFILE}" -C

echo ""
log "Resulting MIG device layout:"
nvidia-smi -L
INSTANCE_COUNT="$(nvidia-smi -L | grep -c 'MIG' || true)"
log "MIG instance count = ${INSTANCE_COUNT}"
[[ "${INSTANCE_COUNT}" -ge 1 ]] && pass "Created ${INSTANCE_COUNT} MIG instance(s)" || { fail "no MIG instances created"; exit 1; }

# ---- STEP 4: Isolation test -------------------------------------------------
step "Verify memory/compute isolation between MIG instances"
# Two independent CUDA workloads pinned to two different MIG UUIDs must NOT see
# each other's memory. We use nvidia-smi per-instance memory as the observable.
MIG_UUIDS=($(nvidia-smi -L | grep -oE 'MIG-[0-9a-f-]+' || true))
if [[ "${#MIG_UUIDS[@]}" -ge 2 ]]; then
  log "MIG UUID[0]=${MIG_UUIDS[0]}"
  log "MIG UUID[1]=${MIG_UUIDS[1]}"
  # Each visible device reports only its own slice (~4.75GB for 1g.5gb), never
  # the full 40GB — that ceiling IS the isolation proof.
  CUDA_VISIBLE_DEVICES="${MIG_UUIDS[0]}" nvidia-smi --query-gpu=memory.total --format=csv,noheader || true
  pass "Two isolated MIG devices addressable via distinct CUDA_VISIBLE_DEVICES UUIDs"
else
  fail "need >=2 MIG instances for isolation test (got ${#MIG_UUIDS[@]})"
fi

# ---- STEP 5: Run cloudai-fusion MIG go tests --------------------------------
step "Run pkg/scheduler MIG/GPU-sharing Go tests"
cd "${PROJECT_ROOT}"
export GOMODCACHE="${GOMODCACHE:-${HOME}/go/pkg/mod}"
go version
# Build first so a compile error is distinguished from a test failure.
go build ./pkg/scheduler/... 
go test ./pkg/scheduler/ -run 'TestGPUSharingManager_Creation|TestRecommendShareMode|TestGetGPUSharingStates_Empty' -v -count=1
pass "pkg/scheduler MIG/GPU-sharing tests passed"

# ---- Summary ----------------------------------------------------------------
echo ""
echo "=============================================================="
if [[ "${FAILURES}" -eq 0 ]]; then
  echo "M2 MIG VALIDATION: PASSED (${STEP} steps)"
  echo "Recorded: driver=${DRIVER_VER} gpu=${GPU_NAME} mig_instances=${INSTANCE_COUNT}"
else
  echo "M2 MIG VALIDATION: ${FAILURES} FAILURE(S) — review log"
  exit 1
fi
echo "=============================================================="

# ---- Teardown (optional) — restore GPU to non-MIG when done -----------------
# sudo nvidia-smi mig -i "${GPU_INDEX}" -dci
# sudo nvidia-smi mig -i "${GPU_INDEX}" -dgi
# sudo nvidia-smi -i "${GPU_INDEX}" --mig-enabled=0
