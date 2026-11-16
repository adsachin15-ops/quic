#!/usr/bin/env bash
# Give quicadd a permanent, browser-trusted HTTPS URL.
# Run ON THE VPS from the repo root:   sudo bash deploy/setup-permanent-url.sh [domain]
#
# With no argument the domain is <public-ip-with-dashes>.sslip.io, a free hostname
# that always resolves to your IP. Pass your own domain (A record -> VPS IP) to use it instead.
set -euo pipefail

APP_DIR="$(cd "$(dirname "$0")/.." && pwd)"
APP_USER="${SUDO_USER:-$(whoami)}"
IP="$(curl -4 -s https://ifconfig.me)"
DOMAIN="${1:-${IP//./-}.sslip.io}"

echo "==> Public IP of this machine: $IP"
echo "==> Domain: https://$DOMAIN"
read -r -p "Is this the VPS you want to publish? [y/N] " ok </dev/tty || ok=n
[[ "$ok" =~ ^[Yy]$ ]] || { echo "Aborted. Run this script on the VPS, not your laptop."; exit 1; }

# 1. Build the server and make sure backend certs exist.
cd "$APP_DIR"
[ -f certs/server.crt ] || sudo -u "$APP_USER" bash scripts/generate-certs.sh "$IP"
if sudo -u "$APP_USER" bash -lc 'command -v go' >/dev/null 2>&1; then
  sudo -u "$APP_USER" bash -lc "cd '$APP_DIR' && go build -o quicadd-server ./cmd/server"
fi
[ -x quicadd-server ] || { echo "quicadd-server binary missing (install Go or copy it here)"; exit 1; }

# 2. Run quicadd as a service so it survives reboots/crashes.
sed -e "s#__APP_DIR__#$APP_DIR#g" -e "s#__APP_USER__#$APP_USER#g" \
    deploy/quicadd.service > /etc/systemd/system/quicadd.service
systemctl daemon-reload
systemctl enable --now quicadd
systemctl restart quicadd

# 3. Install Caddy (official apt repo).
if ! command -v caddy >/dev/null; then
  apt-get update
  apt-get install -y debian-keyring debian-archive-keyring apt-transport-https curl gnupg
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' > /etc/apt/sources.list.d/caddy-stable.list
  apt-get update && apt-get install -y caddy
fi

# 4. Open ports 80/443 in the host firewall (Oracle Cloud images block them via iptables).
for p in 80 443; do
  iptables -C INPUT -p tcp --dport $p -j ACCEPT 2>/dev/null || iptables -I INPUT 1 -p tcp --dport $p -j ACCEPT
done
iptables -C INPUT -p udp --dport 4433 -j ACCEPT 2>/dev/null || iptables -I INPUT 1 -p udp --dport 4433 -j ACCEPT
command -v netfilter-persistent >/dev/null && netfilter-persistent save || true
command -v ufw >/dev/null && ufw status | grep -q active && ufw allow 80,443/tcp && ufw allow 4433/udp || true

# 5. Configure and start Caddy.
cp deploy/Caddyfile /etc/caddy/Caddyfile
mkdir -p /etc/systemd/system/caddy.service.d
printf '[Service]\nEnvironment=QUICADD_DOMAIN=%s\n' "$DOMAIN" > /etc/systemd/system/caddy.service.d/domain.conf
systemctl daemon-reload
systemctl enable caddy
systemctl restart caddy

echo
echo "Done. Your permanent URL:  https://$DOMAIN"
echo "If it doesn't load, open TCP 80 and 443 in your cloud provider's firewall / security list."
