#!/bin/bash
# Install PyTorch (CUDA) on the A100 box for real-hardware compute/bandwidth/MIG-isolation benchmarks.
set -x
export DEBIAN_FRONTEND=noninteractive
apt-get install -y python3-pip 2>&1 | tail -3
pip3 install --upgrade pip 2>&1 | tail -2
pip3 install torch --index-url https://download.pytorch.org/whl/cu124 2>&1 | tail -8
python3 - <<'PY'
import torch
print("TORCH", torch.__version__, "CUDA_AVAIL", torch.cuda.is_available())
if torch.cuda.is_available():
    print("DEVICE", torch.cuda.get_device_name(0))
    print("CAP", torch.cuda.get_device_capability(0))
PY
echo ___TORCH_SETUP_DONE___
