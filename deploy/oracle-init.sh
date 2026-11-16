#!/bin/bash
# Oracle Cloud "Initialization script" (cloud-init). Paste into:
#   Create instance -> Advanced options -> Management -> Paste cloud-init script
# Runs once as root on first boot: installs Go + git, builds quicadd, and publishes
# it at https://<public-ip-with-dashes>.sslip.io. Log: /var/log/quicadd-setup.log
exec > /var/log/quicadd-setup.log 2>&1
set -euxo pipefail

GO_VERSION=1.27.1
ARCH=$(uname -m); case "$ARCH" in x86_64) GOARCH=amd64;; aarch64) GOARCH=arm64;; esac

(command -v dnf && dnf install -y git tar) || (apt-get update && apt-get install -y git tar curl)

curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${GOARCH}.tar.gz" | tar -C /usr/local -xz
export PATH=/usr/local/go/bin:$PATH HOME=/root GOCACHE=/root/.cache/go-build GOPATH=/root/go

# 1 GB micro instances need swap to compile reliably.
if [ ! -f /swapfile ]; then
  fallocate -l 2G /swapfile && chmod 600 /swapfile && mkswap /swapfile && swapon /swapfile
  echo '/swapfile none swap sw 0 0' >> /etc/fstab
fi

git clone https://github.com/adsachin15-ops/quic.git /opt/quicadd
cd /opt/quicadd
go build -o quicadd-server ./cmd/server

QUICADD_YES=1 bash deploy/setup-permanent-url.sh
