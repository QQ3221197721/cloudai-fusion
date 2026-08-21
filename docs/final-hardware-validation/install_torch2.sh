#!/bin/bash
# Reinstall PyTorch via Aliyun PyPI mirror (fast within Aliyun network).
set -x
pkill -f "pip3 install" 2>/dev/null
pkill -f "pip install" 2>/dev/null
sleep 3
pip3 install torch -i https://mirrors.aliyun.com/pypi/simple/ --timeout 120 2>&1 | tail -12
python3 - <<'PY'
import torch
print("TORCH", torch.__version__, "CUDA_AVAIL", torch.cuda.is_available())
if torch.cuda.is_available():
    print("DEVICE", torch.cuda.get_device_name(0))
    print("CAP", torch.cuda.get_device_capability(0))
PY
echo ___TORCH_SETUP_DONE2___
