#!/usr/bin/env python3
# A100 full-GPU capability benchmark: TF32 / FP16 matmul TFLOPS, HBM bandwidth, PCIe H2D/D2H.
import time, torch

assert torch.cuda.is_available()
dev = torch.device("cuda:0")
name = torch.cuda.get_device_name(0)
print(f"DEVICE {name}")

def matmul_tflops(dtype, N=16384, iters=30, tf32=False):
    torch.backends.cuda.matmul.allow_tf32 = tf32
    torch.backends.cudnn.allow_tf32 = tf32
    a = torch.randn((N, N), device=dev, dtype=dtype)
    b = torch.randn((N, N), device=dev, dtype=dtype)
    for _ in range(5):
        c = a @ b
    torch.cuda.synchronize()
    t0 = time.time()
    for _ in range(iters):
        c = a @ b
    torch.cuda.synchronize()
    dt = time.time() - t0
    return (2.0 * N**3 * iters) / dt / 1e12

# FP32 (with TF32 tensor cores on) — A100 headline TF32 matmul
tf32 = matmul_tflops(torch.float32, tf32=True)
print(f"TF32_MATMUL_TFLOPS {tf32:.1f}")

# FP16 tensor core
fp16 = matmul_tflops(torch.float16)
print(f"FP16_MATMUL_TFLOPS {fp16:.1f}")

# BF16 tensor core
bf16 = matmul_tflops(torch.bfloat16)
print(f"BF16_MATMUL_TFLOPS {bf16:.1f}")

# HBM bandwidth: large device-to-device copy
def hbm_bandwidth(bytes_n=2_000_000_000, iters=30):
    x = torch.empty(bytes_n // 2, device=dev, dtype=torch.float16)
    y = torch.empty_like(x)
    for _ in range(5):
        y.copy_(x)
    torch.cuda.synchronize()
    t0 = time.time()
    for _ in range(iters):
        y.copy_(x)
    torch.cuda.synchronize()
    dt = time.time() - t0
    # read + write = 2x bytes moved
    return (2.0 * bytes_n * iters) / dt / 1e9

print(f"HBM_BANDWIDTH_GBps {hbm_bandwidth():.1f}")

# PCIe H2D / D2H
def pcie_bw(bytes_n=1_000_000_000, iters=20, to_device=True):
    if to_device:
        src = torch.empty(bytes_n // 2, dtype=torch.float16, pin_memory=True)
        dst = torch.empty(bytes_n // 2, device=dev, dtype=torch.float16)
    else:
        src = torch.empty(bytes_n // 2, device=dev, dtype=torch.float16)
        dst = torch.empty(bytes_n // 2, dtype=torch.float16, pin_memory=True)
    torch.cuda.synchronize()
    t0 = time.time()
    for _ in range(iters):
        dst.copy_(src)
    torch.cuda.synchronize()
    dt = time.time() - t0
    return (bytes_n * iters) / dt / 1e9

print(f"PCIE_H2D_GBps {pcie_bw(to_device=True):.1f}")
print(f"PCIE_D2H_GBps {pcie_bw(to_device=False):.1f}")
print("___A100_CAPABILITY_DONE___")
