# Final Hardware Validation — M2 / M3 / M5

**Stage B goal:** drive M2 (GPU MIG partitioning), M3 (CRIU + InfiniBand cross-node
migration), and M5 (SGX confidential-compute enclaves) from "code-complete /
locally-simulated" to **all four verification criteria passing on real hardware**.

These three modules cannot be fully verified on a Windows developer laptop — they
require physical A100 GPUs with MIG support, an RDMA (InfiniBand / EFA) fabric, and
an Intel SGX-enabled CPU. This directory contains **runnable, copy-paste-ready
validation scripts** to execute on rented cloud hardware, plus the cost budget and
expected runtimes.

> **Honesty statement.** Every script `set -euo pipefail` + traps errors and
> **fails loudly** at the capability gate when the required hardware is absent — it
> never fabricates a "pass". Legs that need a second physical node or an external
> attestation service are explicitly **SKIPPED** (counted and reported), not faked.
> Cloud GPU/SGX capacity is quota-gated; **request quota 2–5 business days ahead**
> (see the quota table below).

---

## Files

| File | Module | Runs on | What it verifies |
|------|--------|---------|------------------|
| `m2_mig_validation.sh` | **M2 MIG** | 1× A100 node (p4d.24xlarge / ND96asr_v4) | driver → MIG enable → partition → isolation → `go test ./pkg/scheduler` |
| `m3_migration_validation.sh` | **M3 CRIU+IB** | 2× A100 nodes in one placement group | criu check → RDMA BW → checkpoint → cross-node restore → measure → `go test` |
| `m5_sgx_validation.sh` | **M5 SGX** | 1× SGX node (Azure DCsv3 / Intel Dev Cloud) | /dev/sgx_enclave → cpuid → load enclave → attest → `go test ./pkg/capability` |
| `README.md` | — | — | this file: rental instructions, cost budget, runtimes |

Each script writes a `*_result.log` (via `tee`) that is the artifact to attach to
the module's four-goal audit entry.

---

## How to rent the hardware

### M2 — single A100 node (MIG)
**AWS `p4d.24xlarge`** (8× A100-40GB) or **Azure `Standard_ND96asr_v4`** (8× A100-40GB + IB).

```bash
# --- AWS (us-east-1), on-demand ---
aws ec2 run-instances \
  --image-id ami-0abcdef_ubuntu2204_dlami \
  --instance-type p4d.24xlarge \
  --key-name my-key --count 1 \
  --tag-specifications 'ResourceType=instance,Tags=[{Key=purpose,Value=m2-mig-validation}]'
# then: ssh in, git clone the repo, run the script (see below)
```

```bash
# --- Azure ---
az vm create -g rg-hwval -n m2-a100 \
  --image Canonical:0001-com-ubuntu-server-jammy:22_04-lts-gen2:latest \
  --size Standard_ND96asr_v4 --admin-username azureuser --generate-ssh-keys
```

### M3 — two A100 nodes on one RDMA fabric (CRIU + InfiniBand)
Provision **two** nodes in the **same placement group** (AWS) or **proximity
placement group** (Azure) so EFA/InfiniBand is usable node-to-node.

```bash
# --- AWS: cluster placement group + EFA ---
aws ec2 create-placement-group --group-name m3-pg --strategy cluster
aws ec2 run-instances --instance-type p4d.24xlarge --count 2 \
  --placement GroupName=m3-pg \
  --network-interfaces '[{"DeviceIndex":0,"InterfaceType":"efa","SubnetId":"subnet-xxxx"}]'
```

```bash
# --- Azure: proximity placement group ---
az ppg create -g rg-hwval -n m3-ppg
az vm create -g rg-hwval -n m3-a100-a --size Standard_ND96asr_v4 --ppg m3-ppg ...
az vm create -g rg-hwval -n m3-a100-b --size Standard_ND96asr_v4 --ppg m3-ppg ...
```

### M5 — SGX node
**Azure `Standard_DC4s_v3`** (or larger DCsv3) exposes SGX with a large EPC. Intel
Tiber Developer Cloud also offers bare-metal SGX nodes (often free-tier eligible).

```bash
# --- Azure DCsv3 (confidential compute) ---
az vm create -g rg-hwval -n m5-sgx \
  --image Canonical:0001-com-ubuntu-confidential-vm-jammy:22_04-lts-cvm:latest \
  --size Standard_DC4s_v3 --admin-username azureuser --generate-ssh-keys
```

### Run a script (all modules, same pattern)
```bash
# on the rented Linux box
sudo apt-get update && sudo apt-get install -y git golang-go
git clone <your-repo-url> && cd <repo>/cloudai-fusion
chmod +x docs/final-hardware-validation/*.sh
# M2:
sudo docs/final-hardware-validation/m2_mig_validation.sh 2>&1 | tee m2_mig_result.log
# M3 (from NODE_A, set NODE_B first):
export NODE_B_HOST=10.0.1.23 NODE_B_SSH="ubuntu@10.0.1.23"
sudo -E docs/final-hardware-validation/m3_migration_validation.sh 2>&1 | tee m3_migration_result.log
# M5:
sudo docs/final-hardware-validation/m5_sgx_validation.sh 2>&1 | tee m5_sgx_result.log
```

---

## Quota — request ahead of time

| Cloud | Resource | Default quota | Lead time |
|-------|----------|---------------|-----------|
| AWS | `p4d.24xlarge` (Running On-Demand P instances vCPUs) | often **0** | 2–5 business days |
| Azure | `Standard_ND96asr_v4` (NDASv4 family vCPUs) | often **0** | 1–3 business days |
| Azure | `Standard_DCsv3` (DCSv3 family) | low/0 | 1–2 business days |
| Intel Tiber Dev Cloud | SGX bare-metal | account approval | 1–2 days |

Submit the quota increase **before** planning the validation window, or the
`run-instances` / `vm create` call will fail with a capacity/quota error.

---

## Budget estimate

On-demand list prices (USD, mid-2026; verify at run time — spot can cut 60–70%).

| Module | Instance(s) | $/h (each) | Nodes | Est. wall-clock | Est. cost |
|--------|-------------|-----------:|------:|----------------:|----------:|
| **M2 MIG** | AWS p4d.24xlarge | ~$32.77 | 1 | ~1.0 h (setup 40m + run 20m) | **~$33** |
| **M3 CRIU+IB** | AWS p4d.24xlarge ×2 | ~$32.77 | 2 | ~1.5 h (setup 60m + run 30m) | **~$98** |
| **M5 SGX** | Azure DC4s_v3 | ~$0.38 | 1 | ~1.0 h (setup 45m + run 15m) | **~$0.40** |
| | | | | **Subtotal** | **~$131** |

Notes / cost controls:
- Add ~20 % buffer for reruns after a first-attempt quota/driver hiccup → **budget ~$160**.
- **Use Spot/low-priority** for M2/M3 (stateless validation, tolerant of interruption) to
  land closer to **~$45–55** for both.
- **Terminate immediately** after `*_result.log` is captured — the A100 nodes dominate cost.
- Azure DCsv3 for M5 is trivially cheap; keep it only long enough to capture the SGX log.
- Cheapest M5 path: **Intel Tiber Developer Cloud SGX free tier** → **$0**.

### Expected per-script runtime (compute only, after box is provisioned)

| Script | Setup (driver/tooling install) | Active validation | `go test` |
|--------|-------------------------------:|------------------:|----------:|
| `m2_mig_validation.sh` | 30–40 min (driver, possible reboot) | 5–10 min (MIG ops) | 1–3 min |
| `m3_migration_validation.sh` | 45–60 min (criu, OFED/EFA, podman on 2 nodes) | 10–20 min (ckpt+xfer+restore) | 1–3 min |
| `m5_sgx_validation.sh` | 30–45 min (DCAP/PSW, Gramine) | 5–10 min (load+attest) | 1–2 min |

---

## Mapping to the four verification criteria

Each module's four-goal audit expects: **(1) real (not simulated) execution,
(2) measured numbers, (3) `go test` green on-hardware, (4) reproducible artifact.**

| Criterion | M2 evidence | M3 evidence | M5 evidence |
|-----------|-------------|-------------|-------------|
| Real execution | `nvidia-smi -L` shows MIG UUIDs | `podman restore` on NODE_B | `/dev/sgx_enclave` + cpuid |
| Measured numbers | instance count, per-slice mem | checkpoint/xfer/restore seconds | EPC size, quote parse |
| `go test` on HW | `pkg/scheduler` MIG tests | `pkg/scheduler` topology tests | `pkg/capability` evidence tests |
| Artifact | `m2_mig_result.log` | `m3_migration_result.log` | `m5_sgx_result.log` |

Attach the three `*_result.log` files to the audit to close M2/M3/M5 at four-goal.
