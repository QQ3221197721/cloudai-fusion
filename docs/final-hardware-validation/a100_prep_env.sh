#!/bin/bash
# Prepare A100 box for real-hardware validation of the CloudAI Fusion MIG scheduler.
# Installs Go toolchain + clones the repo + pre-downloads deps.
set -x
cd /root

# --- Install Go (latest stable; fallback to go1.26.0) ---
GOVER=$(curl -fsSL "https://go.dev/VERSION?m=text" 2>/dev/null | head -1)
[ -z "$GOVER" ] && GOVER=go1.26.0
echo "GO_VERSION_TARGET=$GOVER"
curl -fsSL "https://go.dev/dl/${GOVER}.linux-amd64.tar.gz" -o /root/go.tgz
rm -rf /usr/local/go
tar -C /usr/local -xzf /root/go.tgz
export PATH=/usr/local/go/bin:$PATH
echo 'export PATH=/usr/local/go/bin:$PATH' >> /root/.bashrc
go version || echo GO_INSTALL_FAIL

# --- Clone / update repo (public) ---
if [ -d /root/cloudai-fusion/.git ]; then
  cd /root/cloudai-fusion && git pull --ff-only 2>&1 | tail -3
else
  git clone https://github.com/QQ3221197721/cloudai-fusion.git /root/cloudai-fusion 2>&1 | tail -3
fi

# --- Pre-download deps for the scheduler package ---
cd /root/cloudai-fusion
export GOFLAGS=-mod=mod
go mod download 2>&1 | tail -8 || echo GO_MOD_DOWNLOAD_WARN
go build ./pkg/scheduler/ 2>&1 | tail -8 || echo SCHEDULER_BUILD_WARN

echo ___A100_PREP_DONE___
