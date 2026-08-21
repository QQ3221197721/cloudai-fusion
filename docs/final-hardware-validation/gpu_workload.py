#!/usr/bin/env python3
# Sustained FP16 matmul workload. Reports per-interval achieved TFLOPS + stability stats.
# The visible GPU/MIG-slice is controlled externally via CUDA_VISIBLE_DEVICES.
# Usage: python3 gpu_workload.py <duration_sec> <tag>
import sys, time, statistics
import torch

dur = float(sys.argv[1]) if len(sys.argv) > 1 else 8.0
tag = sys.argv[2] if len(sys.argv) > 2 else "wl"

assert torch.cuda.is_available(), "CUDA not available"
dev = torch.device("cuda:0")
N = 8192
a = torch.randn((N, N), device=dev, dtype=torch.float16)
b = torch.randn((N, N), device=dev, dtype=torch.float16)
flops_per_matmul = 2.0 * (N ** 3)

# warmup
for _ in range(5):
    c = a @ b
torch.cuda.synchronize()

samples = []
t_end = time.time() + dur
while time.time() < t_end:
    n_iter = 20
    torch.cuda.synchronize()
    t0 = time.time()
    for _ in range(n_iter):
        c = a @ b
    torch.cuda.synchronize()
    dt = time.time() - t0
    tflops = (flops_per_matmul * n_iter) / dt / 1e12
    samples.append(tflops)

mean = statistics.mean(samples)
mn = min(samples)
mx = max(samples)
sd = statistics.pstdev(samples) if len(samples) > 1 else 0.0
cv = (sd / mean * 100) if mean else 0.0
print(f"RESULT tag={tag} samples={len(samples)} mean_TFLOPS={mean:.2f} min={mn:.2f} max={mx:.2f} stddev={sd:.3f} CV%={cv:.2f}")
